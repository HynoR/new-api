package deepseek

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

func preserveDeepSeekClaudeThinking(claudeRequest *dto.ClaudeRequest, openAIRequest *dto.GeneralOpenAIRequest) error {
	if claudeRequest == nil || openAIRequest == nil {
		return nil
	}

	assistantThinking := make([]*string, 0)
	for _, claudeMessage := range claudeRequest.Messages {
		if claudeMessage.Role != "assistant" {
			continue
		}

		var thinking *string
		if !claudeMessage.IsStringContent() {
			contents, err := claudeMessage.ParseContent()
			if err != nil {
				return err
			}
			for _, mediaMessage := range contents {
				if mediaMessage.Type == "thinking" {
					thinking = appendDeepSeekThinking(thinking, mediaMessage.Thinking)
				}
			}
		}
		assistantThinking = append(assistantThinking, thinking)
	}

	assistantIndex := 0
	for i := range openAIRequest.Messages {
		if openAIRequest.Messages[i].Role != "assistant" {
			continue
		}
		if assistantIndex >= len(assistantThinking) {
			break
		}
		if assistantThinking[assistantIndex] != nil {
			appendDeepSeekOpenAIReasoningContent(&openAIRequest.Messages[i], assistantThinking[assistantIndex])
		}
		assistantIndex++
	}
	return nil
}

func appendDeepSeekThinking(current *string, next *string) *string {
	if next == nil {
		return current
	}
	combined := ""
	if current != nil {
		combined = *current
	}
	combined += *next
	return &combined
}

func appendDeepSeekOpenAIReasoningContent(message *dto.Message, thinking *string) {
	if message == nil || thinking == nil {
		return
	}
	combined := ""
	if message.ReasoningContent != nil {
		combined = *message.ReasoningContent
	}
	combined += *thinking
	message.ReasoningContent = &combined
}

func applyDeepSeekV4OpenAIThinkingSuffix(info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) error {
	modelName := request.Model
	if info != nil && info.ChannelMeta != nil && info.UpstreamModelName != "" {
		modelName = info.UpstreamModelName
	}
	baseModel, thinkingType, effort, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(modelName)
	if !ok {
		return nil
	}
	thinking, err := common.Marshal(map[string]string{
		"type": thinkingType,
	})
	if err != nil {
		return fmt.Errorf("error marshalling thinking: %w", err)
	}
	request.Model = baseModel
	request.THINKING = thinking
	request.ReasoningEffort = effort
	if info != nil {
		if info.ChannelMeta != nil {
			info.UpstreamModelName = baseModel
		}
		info.ReasoningEffort = effort
	}
	return nil
}

func applyDeepSeekV4ClaudeThinkingSuffix(info *relaycommon.RelayInfo, request *dto.ClaudeRequest) error {
	modelName := request.Model
	if info != nil && info.ChannelMeta != nil && info.UpstreamModelName != "" {
		modelName = info.UpstreamModelName
	}
	baseModel, thinkingType, effort, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(modelName)
	if !ok {
		return nil
	}
	request.Model = baseModel
	request.Thinking = &dto.Thinking{Type: thinkingType}
	if effort == "" {
		request.OutputConfig = nil
	} else {
		outputConfig, err := common.Marshal(map[string]string{
			"effort": effort,
		})
		if err != nil {
			return fmt.Errorf("error marshalling output_config: %w", err)
		}
		request.OutputConfig = outputConfig
	}
	if info != nil {
		if info.ChannelMeta != nil {
			info.UpstreamModelName = baseModel
		}
		info.ReasoningEffort = effort
	}
	return nil
}

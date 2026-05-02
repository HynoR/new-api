package deepseek

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	if useDeepSeekAnthropicUpstream(info) {
		if err := applyDeepSeekV4ClaudeThinkingSuffix(info, req); err != nil {
			return nil, err
		}
		return req, nil
	}

	openAIRequest, err := service.ClaudeToOpenAIRequest(*req, info)
	if err != nil {
		return nil, err
	}
	if err := preserveDeepSeekClaudeThinking(req, openAIRequest); err != nil {
		return nil, err
	}
	if info.SupportStreamOptions && info.IsStream {
		openAIRequest.StreamOptions = &dto.StreamOptions{
			IncludeUsage: true,
		}
	}
	return a.ConvertOpenAIRequest(c, info, openAIRequest)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := strings.TrimRight(info.ChannelBaseUrl, "/")
	switch info.GetFinalRequestRelayFormat() {
	case types.RelayFormatClaude:
		if isDeepSeekAnthropicBaseURL(baseURL) {
			return fmt.Sprintf("%s/v1/messages", baseURL), nil
		}
		return fmt.Sprintf("%s/anthropic/v1/messages", baseURL), nil
	default:
		openAIBaseURL := deepSeekOpenAIBaseURL(baseURL)
		fimBaseUrl := openAIBaseURL
		if !strings.HasSuffix(openAIBaseURL, "/beta") {
			fimBaseUrl += "/beta"
		}
		switch info.RelayMode {
		case constant.RelayModeCompletions:
			return fmt.Sprintf("%s/completions", fimBaseUrl), nil
		default:
			return fmt.Sprintf("%s/v1/chat/completions", openAIBaseURL), nil
		}
	}
}

func useDeepSeekAnthropicUpstream(info *relaycommon.RelayInfo) bool {
	return info != nil && isDeepSeekAnthropicBaseURL(info.ChannelBaseUrl)
}

func isDeepSeekAnthropicBaseURL(baseURL string) bool {
	return strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/anthropic")
}

func deepSeekOpenAIBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	return strings.TrimSuffix(baseURL, "/anthropic")
}

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

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if err := applyDeepSeekV4OpenAIThinkingSuffix(info, request); err != nil {
		return nil, err
	}

	return request, nil
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

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	// TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.GetFinalRequestRelayFormat() {
	case types.RelayFormatClaude:
		adaptor := claude.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	default:
		adaptor := openai.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

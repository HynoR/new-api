package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesCompactionRequestUnmarshalPreservesCodexFields(t *testing.T) {
	t.Parallel()

	request := &OpenAIResponsesCompactionRequest{}
	err := common.Unmarshal([]byte(`{
		"model":"gpt-5-codex-compact",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],
		"instructions":"system",
		"reasoning":{"effort":"medium","summary":"auto"},
		"service_tier":"priority",
		"prompt_cache_key":"pc_123",
		"text":{"format":{"type":"text"}},
		"tools":[{"type":"function","name":"shell"}],
		"parallel_tool_calls":false
	}`), request)
	require.NoError(t, err)

	require.NotNil(t, request.Reasoning)
	require.Equal(t, "medium", request.Reasoning.Effort)
	require.Equal(t, "priority", request.ServiceTier)
	require.JSONEq(t, `"pc_123"`, string(request.PromptCacheKey))
	require.JSONEq(t, `{"format":{"type":"text"}}`, string(request.Text))
	require.JSONEq(t, `[{"type":"function","name":"shell"}]`, string(request.Tools))
	require.JSONEq(t, `false`, string(request.ParallelToolCalls))
}

func TestOpenAIResponsesCompactionRequestToResponsesRequestPreservesCodexFields(t *testing.T) {
	t.Parallel()

	request := &OpenAIResponsesCompactionRequest{
		Model:              "gpt-5-codex-compact",
		Input:              []byte(`[{"type":"message"}]`),
		Instructions:       []byte(`"system"`),
		PreviousResponseID: "resp_123",
		Reasoning: &Reasoning{
			Effort:  "high",
			Summary: "detailed",
		},
		ServiceTier:       "priority",
		PromptCacheKey:    []byte(`"pc_123"`),
		Text:              []byte(`{"format":{"type":"text"}}`),
		Tools:             []byte(`[{"type":"function","name":"shell"}]`),
		ParallelToolCalls: []byte(`false`),
	}

	responsesRequest := request.ToResponsesRequest()

	require.Equal(t, request.Model, responsesRequest.Model)
	require.JSONEq(t, string(request.Input), string(responsesRequest.Input))
	require.JSONEq(t, string(request.Instructions), string(responsesRequest.Instructions))
	require.Equal(t, request.PreviousResponseID, responsesRequest.PreviousResponseID)
	require.Equal(t, request.Reasoning, responsesRequest.Reasoning)
	require.Equal(t, request.ServiceTier, responsesRequest.ServiceTier)
	require.JSONEq(t, string(request.PromptCacheKey), string(responsesRequest.PromptCacheKey))
	require.JSONEq(t, string(request.Text), string(responsesRequest.Text))
	require.JSONEq(t, string(request.Tools), string(responsesRequest.Tools))
	require.JSONEq(t, string(request.ParallelToolCalls), string(responsesRequest.ParallelToolCalls))
}

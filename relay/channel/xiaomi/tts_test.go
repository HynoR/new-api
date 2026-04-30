package xiaomi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertAudioRequestBuildsXiaomiTTSRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	adaptor := &Adaptor{}

	reader, err := adaptor.ConvertAudioRequest(c, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAudioSpeech,
	}, dto.AudioRequest{
		Model:          "mimo-v2.5-tts",
		Input:          "hello",
		Instructions:   "bright tone",
		Voice:          "Chloe",
		ResponseFormat: "pcm",
	})
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload xiaomiTTSRequest
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "mimo-v2.5-tts", payload.Model)
	require.Equal(t, []xiaomiTTSMessage{
		{Role: "user", Content: "bright tone"},
		{Role: "assistant", Content: "hello"},
	}, payload.Messages)
	require.Equal(t, xiaomiTTSAudio{Voice: "Chloe", Format: "pcm16"}, payload.Audio)
	require.Equal(t, "pcm16", c.GetString(contextKeyAudioFormat))
}

func TestConvertAudioRequestUsesXiaomiTTSDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	adaptor := &Adaptor{}

	reader, err := adaptor.ConvertAudioRequest(c, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAudioSpeech,
	}, dto.AudioRequest{
		Model: "mimo-v2-tts",
		Input: "hello",
	})
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload xiaomiTTSRequest
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, []xiaomiTTSMessage{{Role: "assistant", Content: "hello"}}, payload.Messages)
	require.Equal(t, xiaomiTTSAudio{Voice: defaultMimoVoice, Format: "wav"}, payload.Audio)
}

func TestConvertOpenAIRequestKeepsNativeOpenAIFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	adaptor := &Adaptor{}
	req := &dto.GeneralOpenAIRequest{
		Model:         "mimo-v2.5",
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
		Audio:         []byte(`{"format":"wav","voice":"Chloe"}`),
	}

	converted, err := adaptor.ConvertOpenAIRequest(c, &relaycommon.RelayInfo{}, req)
	require.NoError(t, err)
	require.Same(t, req, converted)
	require.NotNil(t, req.StreamOptions)
	require.JSONEq(t, `{"format":"wav","voice":"Chloe"}`, string(req.Audio))
}

func TestHandleTTSResponseDecodesAudioAndReturnsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"choices":[{"message":{"audio":{"data":"QUJD"}}}],
			"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}
		}`)),
	}

	usageAny, apiErr := handleTTSResponse(c, resp, &relaycommon.RelayInfo{}, "wav")
	require.Nil(t, apiErr)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "ABC", recorder.Body.String())
	require.Contains(t, recorder.Header().Get("Content-Type"), "audio/wav")

	usage := usageAny.(*dto.Usage)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
}

func TestHandleTTSResponseDoesNotEstimateMissingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(12)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"audio":{"data":"QUJD"}}}]}`)),
	}

	usageAny, apiErr := handleTTSResponse(c, resp, info, "wav")
	require.Nil(t, apiErr)

	usage := usageAny.(*dto.Usage)
	require.Equal(t, 0, usage.PromptTokens)
	require.Equal(t, 0, usage.CompletionTokens)
	require.Equal(t, 0, usage.TotalTokens)
}

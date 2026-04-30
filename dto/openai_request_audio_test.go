package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenAIMessageAudioIsNotRoundTripped(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model":"mimo-v2.5-tts",
		"audio":{"format":"wav","voice":"Chloe"},
		"messages":[{
			"role":"assistant",
			"content":"hello",
			"audio":{"data":"QUJD"}
		}]
	}`)

	var req GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal(body, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.Unmarshal(encoded, &out))
	require.Contains(t, out, "audio")

	messages := out["messages"].([]any)
	message := messages[0].(map[string]any)
	require.NotContains(t, message, "audio")
}

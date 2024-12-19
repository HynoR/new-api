package common

import (
	"encoding/json"
	"strings"
	"sync"
)

// from songquanpeng/one-api
const (
	USD2RMB = 7.3 // 暂定 1 USD = 7.3 RMB
	USD     = 500 // $0.002 = 1 -> $1 = 500
	RMB     = USD / USD2RMB
)

// modelRatio
// https://platform.openai.com/docs/models/model-endpoint-compatibility
// https://cloud.baidu.com/doc/WENXINWORKSHOP/s/Blfmc9dlf
// https://openai.com/pricing
// TODO: when a new api is enabled, check the pricing here
// 1 === $0.002 / 1K tokens
// 1 === ￥0.014 / 1k tokens

var defaultModelPrice = map[string]float64{
	"suno_music":        0.1,
	"suno_lyrics":       0.01,
	"dall-e-3":          0.04,
	"gpt-4-gizmo-*":     0.1,
	"mj_imagine":        0.1,
	"mj_variation":      0.1,
	"mj_reroll":         0.1,
	"mj_blend":          0.1,
	"mj_modal":          0.1,
	"mj_zoom":           0.1,
	"mj_shorten":        0.1,
	"mj_high_variation": 0.1,
	"mj_low_variation":  0.1,
	"mj_pan":            0.1,
	"mj_inpaint":        0,
	"mj_custom_zoom":    0,
	"mj_describe":       0.05,
	"mj_upscale":        0.05,
	"swap_face":         0.05,
	"mj_upload":         0.05,
}

var (
	modelPriceMap      map[string]float64 = nil
	modelPriceMapMutex                    = sync.RWMutex{}
)
var (
	modelRatioMap      map[string]float64 = nil
	modelRatioMapMutex                    = sync.RWMutex{}
)

var CompletionRatio map[string]float64 = nil
var defaultCompletionRatio = map[string]float64{
	"gpt-4-gizmo-*":  2,
	"gpt-4o-gizmo-*": 3,
	"gpt-4-all":      2,
}
var defaultModelRatio = initModelRatio()

func GetModelPriceMap() map[string]float64 {
	modelPriceMapMutex.Lock()
	defer modelPriceMapMutex.Unlock()
	if modelPriceMap == nil {
		modelPriceMap = defaultModelPrice
	}
	return modelPriceMap
}

func ModelPrice2JSONString() string {
	GetModelPriceMap()
	jsonBytes, err := json.Marshal(modelPriceMap)
	if err != nil {
		SysError("error marshalling model price: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelPriceByJSONString(jsonStr string) error {
	modelPriceMapMutex.Lock()
	defer modelPriceMapMutex.Unlock()
	modelPriceMap = make(map[string]float64)
	return json.Unmarshal([]byte(jsonStr), &modelPriceMap)
}

// GetModelPrice 返回模型的价格，如果模型不存在则返回-1，false
func GetModelPrice(name string, printErr bool) (float64, bool) {
	GetModelPriceMap()
	if strings.HasPrefix(name, "gpt-4-gizmo") {
		name = "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(name, "gpt-4o-gizmo") {
		name = "gpt-4o-gizmo-*"
	}
	price, ok := modelPriceMap[name]
	if !ok {
		if printErr {
			SysError("model price not found: " + name)
		}
		return -1, false
	}
	return price, true
}

func GetModelRatioMap() map[string]float64 {
	modelRatioMapMutex.Lock()
	defer modelRatioMapMutex.Unlock()
	if modelRatioMap == nil {
		modelRatioMap = defaultModelRatio
	}
	return modelRatioMap
}

func ModelRatio2JSONString() string {
	GetModelRatioMap()
	jsonBytes, err := json.Marshal(modelRatioMap)
	if err != nil {
		SysError("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRatioByJSONString(jsonStr string) error {
	modelRatioMapMutex.Lock()
	defer modelRatioMapMutex.Unlock()
	modelRatioMap = make(map[string]float64)
	return json.Unmarshal([]byte(jsonStr), &modelRatioMap)
}

func GetModelRatio(name string) float64 {
	GetModelRatioMap()
	if strings.HasPrefix(name, "gpt-4-gizmo") {
		name = "gpt-4-gizmo-*"
	}
	ratio, ok := modelRatioMap[name]
	if !ok {
		SysError("model ratio not found: " + name)
		return 30
	}
	return ratio
}

func DefaultModelRatio2JSONString() string {
	jsonBytes, err := json.Marshal(defaultModelRatio)
	if err != nil {
		SysError("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func GetDefaultModelRatioMap() map[string]float64 {
	return defaultModelRatio
}

func CompletionRatio2JSONString() string {
	if CompletionRatio == nil {
		CompletionRatio = defaultCompletionRatio
	}
	jsonBytes, err := json.Marshal(CompletionRatio)
	if err != nil {
		SysError("error marshalling completion ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateCompletionRatioByJSONString(jsonStr string) error {
	CompletionRatio = make(map[string]float64)
	return json.Unmarshal([]byte(jsonStr), &CompletionRatio)
}

var OpenAICompletionRatioDataDiff = map[string]float64{
	"gpt-4o-2024-05-13":  3, // 第一版4o 为 3倍
	"chatgpt-4o-latest":  3,
	"gpt-3.5-turbo-0125": 3, // 最后一版3.5 turbo 为 3倍
	"gpt-3.5-turbo-1106": 2, // 1106 为 2倍
}

func getOpenAICompletionRatioData(name string) float64 {
	// 特殊倍率
	if v, ok := OpenAICompletionRatioDataDiff[name]; ok {
		return v
	}

	if strings.HasPrefix(name, "gpt-4-gizmo") {
		name = "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(name, "gpt-4o-gizmo") {
		name = "gpt-4o-gizmo-*"
	}

	// 逆向大手子
	if strings.HasSuffix(name, "-all") || strings.HasSuffix(name, "-gizmo-*") {
		return 1
	}

	// 4o 大家族基本上为4倍 , 05-13和chatgpt-4o-latest为3倍
	if strings.HasPrefix(name, "gpt-4o") {
		return 4
	}

	// o1
	if strings.HasPrefix(name, "o1-") {
		return 4
	}

	// gpt-4 / gpt-4-turbo 大家族基本上为2倍 带preview的为3倍, turbo为3倍
	if strings.HasPrefix(name, "gpt-4") {
		if strings.HasSuffix(name, "-preview") {
			return 3
		}
		if strings.HasSuffix(name, "-turbo") {
			return 3
		}
		return 2
	}

	// gpt-3.5 老版本均为1.33 (3/4)
	if strings.HasPrefix(name, "gpt-3.5") {
		return 3.0 / 4.0
	}

	// 默认倍率
	return 1

}

func GetCompletionRatio(name string) float64 {
	// Custom Completion Ratio
	if ratio, ok := CompletionRatio[name]; ok && ratio > 0 {
		return ratio
	}

	// OpenAI Models
	if strings.HasPrefix(name, "gpt") || strings.HasPrefix(name, "chatgpt") || strings.HasPrefix(name, "o1") {
		return getOpenAICompletionRatioData(name)
	}

	// Anthropic Models
	if strings.Contains(name, "claude-3") {
		return 5
	}

	if strings.Contains(name, "claude-2") || strings.Contains(name, "claude-instant-1") {
		return 3
	}

	if strings.HasPrefix(name, "mistral-") {
		return 3
	}
	if strings.HasPrefix(name, "gemini-") {
		return 4
	}
	if strings.HasPrefix(name, "command") {
		switch name {
		// 淘汰模型暂不考虑倍率设置
		case "command-r":
			return 3
		case "command-r-plus":
			return 5
		// 2024 后推出的新模型目前都是4倍
		default:
			return 4
		}
	}
	if strings.HasPrefix(name, "deepseek") {
		return 2
	}
	if strings.HasPrefix(name, "ERNIE-Speed-") {
		return 2
	} else if strings.HasPrefix(name, "ERNIE-Lite-") {
		return 2
	} else if strings.HasPrefix(name, "ERNIE-Character") {
		return 2
	} else if strings.HasPrefix(name, "ERNIE-Functions") {
		return 2
	}
	switch name {
	case "llama2-70b-4096":
		return 0.8 / 0.64
	case "llama3-8b-8192":
		return 2
	case "llama3-70b-8192":
		return 0.79 / 0.59
	}

	return 1
}

func GetAudioRatio(name string) float64 {
	if strings.HasPrefix(name, "gpt-4o-realtime") {
		return 20
	} else if strings.HasPrefix(name, "gpt-4o-audio") {
		return 40
	}
	return 20
}

func GetAudioCompletionRatio(name string) float64 {
	if strings.HasPrefix(name, "gpt-4o-realtime") {
		return 2
	}
	return 2
}

//func GetAudioPricePerMinute(name string) float64 {
//	if strings.HasPrefix(name, "gpt-4o-realtime") {
//		return 0.06
//	}
//	return 0.06
//}
//
//func GetAudioCompletionPricePerMinute(name string) float64 {
//	if strings.HasPrefix(name, "gpt-4o-realtime") {
//		return 0.24
//	}
//	return 0.24
//}

func GetCompletionRatioMap() map[string]float64 {
	if CompletionRatio == nil {
		CompletionRatio = defaultCompletionRatio
	}
	return CompletionRatio
}

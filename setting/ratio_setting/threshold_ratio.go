package ratio_setting

import (
	"encoding/json"
	"one-api/common"
	"sync"
)

type ThresholdConfig struct {
	Threshold       int     `json:"threshold"`
	ModelRatio      float64 `json:"model_ratio"`
	CompletionRatio float64 `json:"completion_ratio"`
}

var defaultThresholdRatio = map[string]ThresholdConfig{
	"claude-sonnet-4": {
		Threshold:       200000,
		ModelRatio:      10.0, // $10/1M input tokens
		CompletionRatio: 30.0, // $30/1M output tokens
	},
}

var thresholdRatioMap map[string]ThresholdConfig
var thresholdRatioMapMutex sync.RWMutex

// GetThresholdRatioMap returns the threshold ratio map
func GetThresholdRatioMap() map[string]ThresholdConfig {
	thresholdRatioMapMutex.RLock()
	defer thresholdRatioMapMutex.RUnlock()
	return thresholdRatioMap
}

// ThresholdRatio2JSONString converts the threshold ratio map to a JSON string
func ThresholdRatio2JSONString() string {
	thresholdRatioMapMutex.RLock()
	defer thresholdRatioMapMutex.RUnlock()
	jsonBytes, err := json.Marshal(thresholdRatioMap)
	if err != nil {
		common.SysLog("error marshalling threshold ratio: " + err.Error())
	}
	return string(jsonBytes)
}

// UpdateThresholdRatioByJSONString updates the threshold ratio map from a JSON string
func UpdateThresholdRatioByJSONString(jsonStr string) error {
	thresholdRatioMapMutex.Lock()
	defer thresholdRatioMapMutex.Unlock()
	thresholdRatioMap = make(map[string]ThresholdConfig)
	err := json.Unmarshal([]byte(jsonStr), &thresholdRatioMap)
	if err == nil {
		InvalidateExposedDataCache()
	}
	return err
}

// GetThresholdConfig returns the threshold configuration for a model
func GetThresholdConfig(name string) (ThresholdConfig, bool) {
	thresholdRatioMapMutex.RLock()
	defer thresholdRatioMapMutex.RUnlock()

	name = FormatMatchingModelName(name)

	config, ok := thresholdRatioMap[name]
	if !ok {
		return ThresholdConfig{}, false
	}
	return config, true
}

// GetThresholdRatioCopy returns a copy of the threshold ratio map
func GetThresholdRatioCopy() map[string]ThresholdConfig {
	thresholdRatioMapMutex.RLock()
	defer thresholdRatioMapMutex.RUnlock()
	copyMap := make(map[string]ThresholdConfig, len(thresholdRatioMap))
	for k, v := range thresholdRatioMap {
		copyMap[k] = v
	}
	return copyMap
}

// InitThresholdRatio initializes the threshold ratio map
func InitThresholdRatio() {
	thresholdRatioMapMutex.Lock()
	defer thresholdRatioMapMutex.Unlock()
	thresholdRatioMap = make(map[string]ThresholdConfig)
	for k, v := range defaultThresholdRatio {
		thresholdRatioMap[k] = v
	}
}

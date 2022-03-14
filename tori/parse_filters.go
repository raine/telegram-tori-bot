package tori

import (
	"encoding/json"
)

func ParseNewadFilters(jsonData []byte) (FiltersNewad, error) {
	var filters FiltersNewad
	err := json.Unmarshal(jsonData, &filters)
	return filters, err
}

func ParseOneSettingsParam(jsonData []byte) (SettingsParam, error) {
	var settingsParam SettingsParam
	err := json.Unmarshal(jsonData, &settingsParam)
	return settingsParam, err
}

func ParseMultipleSettingsParams(jsonData []byte) ([]SettingsParam, error) {
	var settingsParams []SettingsParam
	err := json.Unmarshal(jsonData, &settingsParams)
	return settingsParams, err
}

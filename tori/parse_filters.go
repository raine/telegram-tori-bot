package tori

import (
	"encoding/json"

	tf "github.com/raine/go-telegram-bot/tori_filters"
)

func ParseNewadFilters(jsonData []byte) (tf.FiltersNewad, error) {
	var filters tf.FiltersNewad
	err := json.Unmarshal(jsonData, &filters)
	return filters, err
}

func ParseOneSettingsParam(jsonData []byte) (tf.SettingsParam, error) {
	var settingsParam tf.SettingsParam
	err := json.Unmarshal(jsonData, &settingsParam)
	return settingsParam, err
}

func ParseMultipleSettingsParams(jsonData []byte) ([]tf.SettingsParam, error) {
	var settingsParams []tf.SettingsParam
	err := json.Unmarshal(jsonData, &settingsParams)
	return settingsParams, err
}

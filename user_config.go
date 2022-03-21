package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

type (
	UserConfig struct {
		Token         string `json:"token"`
		ToriAccountId string `json:"toriAccountId"`
	}
	UserConfigMap map[int64]UserConfig
)

func readUserConfigMap() (UserConfigMap, error) {
	var userConfigMap UserConfigMap

	userConfigPath, ok := os.LookupEnv("USER_CONFIG_PATH")
	if !ok {
		return userConfigMap, fmt.Errorf("USER_CONFIG_PATH env var not defined")
	}

	bytes, err := ioutil.ReadFile(userConfigPath)
	if err != nil {
		return userConfigMap, fmt.Errorf("could not read auth config: %w", err)
	}

	err = json.Unmarshal(bytes, &userConfigMap)
	if err != nil {
		return userConfigMap, fmt.Errorf("failed to unmarshal user config: %w", err)
	}

	return userConfigMap, nil
}

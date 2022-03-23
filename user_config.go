package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
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
		return userConfigMap, errors.Errorf("USER_CONFIG_PATH env var not defined")
	}

	bytes, err := ioutil.ReadFile(userConfigPath)
	if err != nil {
		return userConfigMap, errors.Wrap(err, "could not read auth config")
	}

	err = json.Unmarshal(bytes, &userConfigMap)
	if err != nil {
		return userConfigMap, errors.Wrap(err, "failed to unmarshal user config")
	}

	return userConfigMap, nil
}

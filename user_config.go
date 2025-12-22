package main

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type (
	UserConfigItem struct {
		TelegramUserId int64
		Token          string
		ToriAccountId  string
	}
	UserConfig struct {
		Users []UserConfigItem
	}
	UserConfigMap map[int64]UserConfigItem
)

func readUserConfigMap() (UserConfigMap, error) {
	userConfigPath, ok := os.LookupEnv("USER_CONFIG_PATH")
	if !ok {
		return nil, fmt.Errorf("USER_CONFIG_PATH env var not defined")
	}

	bytes, err := os.ReadFile(userConfigPath)
	if err != nil {
		return nil, fmt.Errorf("could not read auth config: %w", err)
	}

	var userConfig UserConfig

	if err := toml.Unmarshal(bytes, &userConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user config: %w", err)
	}

	userConfigMap := make(UserConfigMap)

	for _, configUser := range userConfig.Users {
		userConfigMap[configUser.TelegramUserId] = configUser
	}

	return userConfigMap, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

type AuthMap map[int64]string

func readAuthMap() (AuthMap, error) {
	var authMap AuthMap

	authConfigPath, ok := os.LookupEnv("AUTH_CONFIG")
	if !ok {
		return authMap, fmt.Errorf("AUTH_CONFIG env var not defined")
	}

	bytes, err := ioutil.ReadFile(authConfigPath)
	if err != nil {
		return authMap, fmt.Errorf("could not read auth config: %w", err)
	}

	err = json.Unmarshal(bytes, &authMap)
	if err != nil {
		return authMap, fmt.Errorf("failed to unmarshal auth config: %w", err)
	}

	return authMap, nil
}

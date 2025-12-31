package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

const (
	AppName     = "telegram-tori-bot"
	EnvFileName = "config.env"
)

// LoadEnvFile loads environment variables from the config file in the user's
// config directory. Errors are ignored since the file may not exist.
func LoadEnvFile() {
	configBase, err := os.UserConfigDir()
	if err != nil {
		return
	}
	configPath := filepath.Join(configBase, AppName, EnvFileName)
	_ = godotenv.Load(configPath)
}

package tori

import (
	"os"
	"path/filepath"
)

const appName = "telegram-tori-bot"

// ConfigDir returns the XDG config directory for the app.
// Uses $XDG_CONFIG_HOME/telegram-tori-bot or ~/.config/telegram-tori-bot
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", appName)
}

// ConfigPath returns the full path to a config file.
func ConfigPath(filename string) string {
	return filepath.Join(ConfigDir(), filename)
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	return os.MkdirAll(ConfigDir(), 0700)
}

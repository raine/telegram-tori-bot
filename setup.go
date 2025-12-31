package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
	"golang.org/x/term"
)

const (
	appName     = "telegram-tori-bot"
	envFileName = "config.env"
)

// getConfigDir returns the application's config directory path.
// Creates the directory if it doesn't exist.
func getConfigDir() (string, error) {
	configBase, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}

	configDir := filepath.Join(configBase, appName)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	return configDir, nil
}

// getConfigFilePath returns the full path to the config file.
func getConfigFilePath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, envFileName), nil
}

// requiredEnvVars lists all environment variables that must be set for the bot to run.
var requiredEnvVars = []string{"BOT_TOKEN", "GEMINI_API_KEY", "TORI_TOKEN_KEY", "ADMIN_TELEGRAM_ID"}

// checkRequiredConfig checks if all required environment variables are set.
// Returns the names of any missing variables.
func checkRequiredConfig() []string {
	var missing []string
	for _, v := range requiredEnvVars {
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	return missing
}

// loadEnvFile attempts to load environment variables from the config file.
// Errors are ignored since the file may not exist.
func loadEnvFile() {
	configPath, err := getConfigFilePath()
	if err != nil {
		return
	}
	_ = godotenv.Load(configPath)
}

// isInteractiveTerminal returns true if both stdin and stdout are TTYs.
// This is used to determine if we can run the interactive setup wizard.
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// runSetupWizard runs an interactive wizard to collect required configuration.
// Returns true if setup was successful and the bot should continue starting.
func runSetupWizard() bool {
	// Header style
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		MarginBottom(1)

	fmt.Println()
	fmt.Println(titleStyle.Render("🤖 Telegram Tori Bot - First-time Setup"))
	fmt.Println()

	var botToken, geminiKey, adminID string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Telegram Bot Token").
				Description("Message @BotFather on Telegram → /newbot → copy token").
				Value(&botToken).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("token is required")
					}
					return validateTelegramToken(s)
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Gemini API Key").
				Description("Get yours at https://aistudio.google.com/apikey").
				Value(&geminiKey).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("API key is required")
					}
					return validateGeminiKey(s)
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Your Telegram User ID").
				Description("Message @userinfobot to get your ID: https://t.me/userinfobot").
				Value(&adminID).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("user ID is required")
					}
					if _, err := strconv.ParseInt(s, 10, 64); err != nil {
						return errors.New("must be a number")
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeBase16())

	err := form.Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("\nSetup cancelled.")
			return false
		}
		fmt.Printf("\nError: %v\n", err)
		return false
	}

	// Generate security key automatically
	tokenKey := generateTokenKey()

	// Write configuration to .env file
	config := map[string]string{
		"BOT_TOKEN":         botToken,
		"GEMINI_API_KEY":    geminiKey,
		"ADMIN_TELEGRAM_ID": adminID,
		"TORI_TOKEN_KEY":    tokenKey,
	}

	configPath, err := writeEnvFile(config)
	if err != nil {
		fmt.Printf("\nError saving configuration: %v\n", err)
		waitOnWindows()
		return false
	}

	// Set values in current process
	for k, v := range config {
		os.Setenv(k, v)
	}

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	fmt.Println()
	fmt.Println(successStyle.Render("✓ Configuration saved"))
	fmt.Println(pathStyle.Render("  " + configPath))
	fmt.Println()
	fmt.Println("Starting bot...")
	fmt.Println()

	return true
}

func generateTokenKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based if crypto/rand fails (unlikely)
		return fmt.Sprintf("tori-%d", time.Now().UnixNano())
	}
	return base64.URLEncoding.EncodeToString(b)
}

// validateTelegramToken validates a Telegram bot token by calling the getMe API.
func validateTelegramToken(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return errors.New("connection timed out - check your internet")
		}
		return errors.New("connection failed - check your internet")
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("invalid response: %w", err)
	}

	if !result.OK {
		if result.Description != "" {
			return errors.New(result.Description)
		}
		return errors.New("token rejected by Telegram")
	}

	return nil
}

// validateGeminiKey validates a Gemini API key by making a simple API call.
func validateGeminiKey(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the models list endpoint which is lightweight and validates the key
	// URL-encode the key to handle any special characters
	q := url.Values{}
	q.Add("key", key)
	reqURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?%s", q.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("connection failed - check your internet")
	}
	defer resp.Body.Close()

	if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 {
		var result struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Error.Message != "" {
			return errors.New(result.Error.Message)
		}
		return fmt.Errorf("API key rejected (HTTP %d)", resp.StatusCode)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}

	return nil
}

// writeEnvFile writes the configuration to the config file.
// Uses restrictive permissions (0600) since the file contains secrets.
// Returns the path where the config was written.
func writeEnvFile(config map[string]string) (string, error) {
	configPath, err := getConfigFilePath()
	if err != nil {
		return "", err
	}

	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	// Write in a consistent order, quoting values to handle special characters
	order := []string{"BOT_TOKEN", "GEMINI_API_KEY", "ADMIN_TELEGRAM_ID", "TORI_TOKEN_KEY"}
	for _, key := range order {
		if val, ok := config[key]; ok {
			if _, err := fmt.Fprintf(f, "%s=%q\n", key, val); err != nil {
				return "", fmt.Errorf("failed to write %s: %w", key, err)
			}
		}
	}

	return configPath, nil
}

// waitOnWindows pauses execution on Windows so users can see error messages
// before the console window closes.
func waitOnWindows() {
	if runtime.GOOS == "windows" {
		fmt.Println()
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
	}
}

// fatalWithWait logs a fatal error and waits on Windows before exiting.
// Uses zerolog for structured logging if available, falls back to stderr otherwise.
func fatalWithWait(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	// Use zerolog for consistent logging (it's always initialized, even if default)
	log.Error().Msg(msg)
	waitOnWindows()
	os.Exit(1)
}

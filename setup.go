package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
	"golang.org/x/term"
)

const envFileName = ".env"

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

// loadEnvFile attempts to load environment variables from .env file.
// Errors are ignored since the file may not exist.
func loadEnvFile() {
	_ = godotenv.Load()
}

// isInteractiveTerminal returns true if both stdin and stdout are TTYs.
// This is used to determine if we can run the interactive setup wizard.
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// runSetupWizard runs an interactive wizard to collect required configuration.
// Returns true if setup was successful and the bot should continue starting.
func runSetupWizard() bool {
	fmt.Println()
	fmt.Println("Telegram Tori Bot - First-time Setup")
	fmt.Println("=====================================")
	fmt.Println()
	fmt.Println("Configuration file not found. Let's set up your bot!")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Step 1: Telegram Bot Token
	botToken := promptBotToken(reader)
	if botToken == "" {
		return false
	}

	// Step 2: Gemini API Key
	geminiKey := promptGeminiKey(reader)
	if geminiKey == "" {
		return false
	}

	// Step 3: Admin Telegram ID
	adminID := promptAdminID(reader)
	if adminID == "" {
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

	if err := writeEnvFile(config); err != nil {
		fmt.Printf("\nError saving configuration: %v\n", err)
		waitOnWindows()
		return false
	}

	// Set values in current process
	for k, v := range config {
		os.Setenv(k, v)
	}

	fmt.Println()
	fmt.Println("Configuration saved to .env")
	fmt.Println("Starting bot...")
	fmt.Println()

	return true
}

func promptBotToken(reader *bufio.Reader) string {
	fmt.Println("Step 1: Telegram Bot Token")
	fmt.Println("--------------------------")
	fmt.Println("1. Open Telegram and message @BotFather")
	fmt.Println("2. Send /newbot and follow the prompts")
	fmt.Println("3. Copy the token you receive")
	fmt.Println()

	for {
		fmt.Print("Enter your bot token: ")
		token, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nSetup cancelled.")
				return ""
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}
		token = strings.TrimSpace(token)

		if token == "" {
			fmt.Println("Token cannot be empty. Please try again.")
			continue
		}

		fmt.Print("Validating token... ")
		if err := validateTelegramToken(token); err != nil {
			fmt.Printf("\nInvalid token: %v\n", err)
			fmt.Println("Please check your token and try again.")
			fmt.Println()
			continue
		}

		fmt.Println("OK")
		fmt.Println()
		return token
	}
}

func promptGeminiKey(reader *bufio.Reader) string {
	fmt.Println("Step 2: Gemini API Key")
	fmt.Println("----------------------")
	fmt.Println("1. Visit https://aistudio.google.com/apikey")
	fmt.Println("2. Create a new API key")
	fmt.Println()

	for {
		fmt.Print("Enter your Gemini API key: ")
		key, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nSetup cancelled.")
				return ""
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}
		key = strings.TrimSpace(key)

		if key == "" {
			fmt.Println("API key cannot be empty. Please try again.")
			continue
		}

		fmt.Print("Validating API key... ")
		if err := validateGeminiKey(key); err != nil {
			fmt.Printf("\nInvalid API key: %v\n", err)
			fmt.Println("Please check your key and try again.")
			fmt.Println()
			continue
		}

		fmt.Println("OK")
		fmt.Println()
		return key
	}
}

func promptAdminID(reader *bufio.Reader) string {
	fmt.Println("Step 3: Your Telegram User ID")
	fmt.Println("-----------------------------")
	fmt.Println("1. Message @userinfobot on Telegram")
	fmt.Println("2. It will reply with your user ID")
	fmt.Println()

	for {
		fmt.Print("Enter your Telegram user ID: ")
		id, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nSetup cancelled.")
				return ""
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}
		id = strings.TrimSpace(id)

		if id == "" {
			fmt.Println("User ID cannot be empty. Please try again.")
			continue
		}

		// Validate it's a valid integer
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			fmt.Println("User ID must be a number. Please try again.")
			continue
		}

		fmt.Println()
		return id
	}
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
			return fmt.Errorf("connection timed out - check your internet connection")
		}
		return fmt.Errorf("connection failed - check your internet connection: %w", err)
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
			return fmt.Errorf("%s", result.Description)
		}
		return fmt.Errorf("token rejected by Telegram")
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
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 {
		var result struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Error.Message != "" {
			return fmt.Errorf("%s", result.Error.Message)
		}
		return fmt.Errorf("API key rejected (HTTP %d)", resp.StatusCode)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}

	return nil
}

// writeEnvFile writes the configuration to a .env file.
// Uses restrictive permissions (0600) since the file contains secrets.
func writeEnvFile(config map[string]string) error {
	f, err := os.OpenFile(envFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create .env file: %w", err)
	}
	defer f.Close()

	// Write in a consistent order, quoting values to handle special characters
	order := []string{"BOT_TOKEN", "GEMINI_API_KEY", "ADMIN_TELEGRAM_ID", "TORI_TOKEN_KEY"}
	for _, key := range order {
		if val, ok := config[key]; ok {
			if _, err := fmt.Fprintf(f, "%s=%q\n", key, val); err != nil {
				return fmt.Errorf("failed to write %s: %w", key, err)
			}
		}
	}

	return nil
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

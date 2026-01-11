package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/raine/telegram-tori-bot/config"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
)

func main() {
	var userID int64
	var adID string

	flag.Int64Var(&userID, "user", 0, "Telegram user ID")
	flag.StringVar(&adID, "id", "", "Ad/listing ID to fetch")
	flag.Parse()

	// Accept ad ID as positional argument
	if adID == "" && flag.NArg() > 0 {
		adID = flag.Arg(0)
	}

	if adID == "" {
		fmt.Fprintf(os.Stderr, "Usage: get-listing -user <telegram_id> -id <ad_id>\n")
		fmt.Fprintf(os.Stderr, "       get-listing -user <telegram_id> <ad_id>\n")
		os.Exit(1)
	}

	config.LoadEnvFile()

	tokenKey := os.Getenv("TORI_TOKEN_KEY")
	if tokenKey == "" {
		fmt.Fprintf(os.Stderr, "TORI_TOKEN_KEY not set\n")
		os.Exit(1)
	}

	encryptionKey, err := storage.DeriveKey(tokenKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deriving encryption key: %v\n", err)
		os.Exit(1)
	}

	dbPath := os.Getenv("TORI_DB_PATH")
	if dbPath == "" {
		dbPath = "sessions.db"
	}

	store, err := storage.NewSQLiteStore(dbPath, encryptionKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database at %s: %v\n", dbPath, err)
		os.Exit(1)
	}
	defer store.Close()

	// If no user specified, try to find one
	if userID == 0 {
		sessions, err := store.GetAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
			os.Exit(1)
		}
		if len(sessions) == 0 {
			fmt.Fprintf(os.Stderr, "No sessions found in database\n")
			os.Exit(1)
		}
		if len(sessions) == 1 {
			userID = sessions[0].TelegramID
		} else {
			fmt.Fprintf(os.Stderr, "Multiple users found. Please specify -user:\n")
			for _, s := range sessions {
				fmt.Fprintf(os.Stderr, "  %d\n", s.TelegramID)
			}
			os.Exit(1)
		}
	}

	session, err := store.Get(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting session: %v\n", err)
		os.Exit(1)
	}
	if session == nil {
		fmt.Fprintf(os.Stderr, "No session found for user %d\n", userID)
		os.Exit(1)
	}

	// Get or create installation ID for the user
	installationID, err := store.GetInstallationID(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting installation ID: %v\n", err)
		os.Exit(1)
	}
	if installationID == "" {
		installationID = uuid.New().String()
		if err := store.SetInstallationID(userID, installationID); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving installation ID: %v\n", err)
			os.Exit(1)
		}
	}

	client := tori.NewAdinputClient(session.Tokens.BearerToken, installationID)
	ad, err := client.GetAdWithModel(context.Background(), adID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching ad %s: %v\n", adID, err)
		os.Exit(1)
	}

	// Pretty print as JSON
	output, err := json.MarshalIndent(ad, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(output))
}

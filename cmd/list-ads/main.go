package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/google/uuid"
	"github.com/raine/telegram-tori-bot/config"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
)

func main() {
	var userID int64
	var limit, offset int
	var facet string

	flag.Int64Var(&userID, "user", 0, "Telegram user ID (if omitted, lists all users)")
	flag.IntVar(&limit, "limit", 50, "Page size")
	flag.IntVar(&offset, "offset", 0, "Pagination offset")
	flag.StringVar(&facet, "facet", "", "Filter: ACTIVE, DRAFT, PENDING, EXPIRED, DISPOSED, or ALL")
	flag.Parse()

	// Also accept user ID as positional argument
	if userID == 0 && flag.NArg() > 0 {
		if id, err := strconv.ParseInt(flag.Arg(0), 10, 64); err == nil {
			userID = id
		}
	}

	// Load env file from user config directory (same as main bot)
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

	if userID == 0 {
		// List all users
		sessions, err := store.GetAll()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
			os.Exit(1)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions found in database")
			os.Exit(0)
		}

		fmt.Println("Available users:")
		for _, s := range sessions {
			fmt.Printf("  Telegram ID: %d (Tori User ID: %s)\n", s.TelegramID, s.ToriUserID)
		}
		fmt.Println("\nUse --user <telegram_id> or pass ID as argument to list ads for a specific user")
		os.Exit(0)
	}

	// Get specific user's session
	session, err := store.Get(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting session: %v\n", err)
		os.Exit(1)
	}
	if session == nil {
		fmt.Fprintf(os.Stderr, "No session found for user %d\n", userID)
		os.Exit(1)
	}

	fmt.Printf("User: %d (Tori User ID: %s)\n\n", session.TelegramID, session.ToriUserID)

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

	// Use shared client logic from tori package
	client := tori.NewAdinputClient(session.Tokens.BearerToken, installationID)
	result, err := client.GetAdSummaries(context.Background(), limit, offset, facet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching ads: %v\n", err)
		os.Exit(1)
	}

	if len(result.Summaries) == 0 {
		fmt.Println("No ads found")
		os.Exit(0)
	}

	// Show available facets
	if len(result.Facets) > 0 {
		fmt.Print("Facets: ")
		for i, f := range result.Facets {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s (%d)", f.Name, f.Total)
		}
		fmt.Print("\n\n")
	}

	fmt.Printf("Found %d ads (showing %d-%d of %d):\n\n", len(result.Summaries), offset+1, offset+len(result.Summaries), result.Total)

	for _, ad := range result.Summaries {
		fmt.Printf("ID: %d\n", ad.ID)
		fmt.Printf("  Title: %s\n", ad.Data.Title)
		fmt.Printf("  Price: %s\n", ad.Data.Subtitle)
		if ad.State.Label != "" {
			fmt.Printf("  State: %s\n", ad.State.Label)
		}
		if ad.ExternalData.Clicks.Value != "" {
			fmt.Printf("  Views: %s\n", ad.ExternalData.Clicks.Value)
		}
		if ad.ExternalData.Favorites.Value != "" {
			fmt.Printf("  Favorites: %s\n", ad.ExternalData.Favorites.Value)
		}
		if ad.DaysUntilExpires > 0 {
			fmt.Printf("  Expires in: %d days\n", ad.DaysUntilExpires)
		}
		fmt.Println()
	}
}

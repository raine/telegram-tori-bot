package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/bot"
	"github.com/raine/telegram-tori-bot/internal/llm"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/watcher"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const logFileName = "telegram-tori-bot.log"

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Try to load existing .env file
	bot.LoadEnvFile()

	// Check if required config is missing
	if missing := bot.CheckRequiredConfig(); len(missing) > 0 {
		if bot.IsInteractiveTerminal() {
			// Interactive terminal - run setup wizard
			if !bot.RunSetupWizard() {
				bot.WaitOnWindows()
				os.Exit(1)
			}
		} else {
			// Non-interactive (systemd, k8s, etc.) - fail with clear error
			bot.FatalWithWait("missing required config: %s", strings.Join(missing, ", "))
		}
	}

	// JOURNAL_STREAM is set by systemd when running as a service.
	// Skip file logging under systemd (journald handles it, and ProtectSystem=strict
	// makes the working directory read-only).
	if _, underSystemd := os.LookupEnv("JOURNAL_STREAM"); underSystemd {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		// Local development: log to both stderr and file
		logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			bot.FatalWithWait("failed to open log file: %v", err)
		}
		defer logFile.Close()

		consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}
		fileWriter := zerolog.ConsoleWriter{Out: logFile, NoColor: true}
		multiWriter := io.MultiWriter(consoleWriter, fileWriter)
		log.Logger = log.Output(multiWriter)

		log.Info().Str("logFile", logFileName).Msg("logging to file")
	}

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		bot.FatalWithWait("BOT_TOKEN is not set")
	}

	// Token encryption key (required)
	tokenKey := os.Getenv("TORI_TOKEN_KEY")
	if tokenKey == "" {
		bot.FatalWithWait("TORI_TOKEN_KEY is not set")
	}

	// Admin Telegram ID (required)
	adminIDStr := os.Getenv("ADMIN_TELEGRAM_ID")
	if adminIDStr == "" {
		bot.FatalWithWait("ADMIN_TELEGRAM_ID is not set")
	}
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		bot.FatalWithWait("ADMIN_TELEGRAM_ID must be a valid integer: %v", err)
	}

	// Database path (optional, defaults to sessions.db)
	dbPath := os.Getenv("TORI_DB_PATH")
	if dbPath == "" {
		dbPath = "sessions.db"
	}

	tg, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		bot.FatalWithWait("failed to initialize telegram bot: %v", err)
	}
	tg.Debug = false
	log.Info().Str("username", tg.Self.UserName).Msg("authorized on account")

	// Register bot commands for Telegram's command menu
	bot.RegisterCommands(tg)

	// Derive encryption key from passphrase
	encryptionKey, err := storage.DeriveKey(tokenKey)
	if err != nil {
		bot.FatalWithWait("failed to derive encryption key: %v", err)
	}

	// Initialize session store
	sessionStore, err := storage.NewSQLiteStore(dbPath, encryptionKey)
	if err != nil {
		bot.FatalWithWait("failed to initialize session store: %v", err)
	}
	defer sessionStore.Close()
	log.Info().Str("dbPath", dbPath).Msg("session store initialized")

	// Initialize listing log (writes to listing.log in current directory)
	if err := bot.InitListingLog("."); err != nil {
		log.Warn().Err(err).Msg("failed to initialize listing log")
	}

	// Create context that cancels on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize vision analyzer (GEMINI_API_KEY is required)
	if os.Getenv("GEMINI_API_KEY") == "" {
		bot.FatalWithWait("GEMINI_API_KEY environment variable is required")
	}
	geminiAnalyzer, err := llm.NewGeminiAnalyzer(ctx)
	if err != nil {
		bot.FatalWithWait("failed to initialize gemini vision analyzer: %v", err)
	}
	log.Info().Msg("gemini vision analyzer initialized")

	// Wrap with cache
	visionAnalyzer := llm.NewCachedAnalyzer(geminiAnalyzer, sessionStore)
	log.Info().Msg("vision analysis caching enabled")

	g, ctx := errgroup.WithContext(ctx)

	// Run bot update loop
	g.Go(func() error {
		return runBot(ctx, tg, sessionStore, visionAnalyzer, adminID)
	})

	// Run watcher service for search watch notifications
	watcherService := watcher.NewService(sessionStore, tg)
	g.Go(func() error {
		watcherService.Run(ctx)
		return nil
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Error().Err(err).Msg("shutdown with error")
	} else {
		log.Info().Msg("shutdown complete")
	}
}

func runBot(ctx context.Context, tg *tgbotapi.BotAPI, sessionStore storage.SessionStore, visionAnalyzer llm.Analyzer, adminID int64) error {
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := tg.GetUpdatesChan(updateConfig)

	b := bot.NewBot(tg, sessionStore, adminID)
	if visionAnalyzer != nil {
		var editParser llm.EditIntentParser
		if gemini := llm.GetGeminiAnalyzer(visionAnalyzer); gemini != nil {
			editParser = gemini
		}
		b.SetLLMClients(visionAnalyzer, editParser)
	}

	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("stopping bot update loop")
			tg.StopReceivingUpdates()
			log.Info().Msg("waiting for active handlers to finish")
			wg.Wait()
			return ctx.Err()
		case update, ok := <-updates:
			if !ok {
				log.Warn().Msg("updates channel closed")
				wg.Wait()
				return nil
			}
			wg.Add(1)
			go func(u tgbotapi.Update) {
				defer wg.Done()
				b.HandleUpdate(ctx, u)
			}(update)
		}
	}
}

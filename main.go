package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/storage"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/raine/telegram-tori-bot/vision"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	botToken, ok := os.LookupEnv("BOT_TOKEN")
	if !ok {
		log.Fatal().Msg("BOT_TOKEN is not set")
	}

	// Token encryption key (required)
	tokenKey, ok := os.LookupEnv("TORI_TOKEN_KEY")
	if !ok {
		log.Fatal().Msg("TORI_TOKEN_KEY is not set")
	}

	// Database path (optional, defaults to sessions.db)
	dbPath := os.Getenv("TORI_DB_PATH")
	if dbPath == "" {
		dbPath = "sessions.db"
	}

	tg, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize telegram bot; bad token?")
	}
	tg.Debug = false
	log.Info().Str("username", tg.Self.UserName).Msg("authorized on account")

	// Derive encryption key from passphrase
	encryptionKey, err := storage.DeriveKey(tokenKey)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to derive encryption key")
	}

	// Initialize session store
	sessionStore, err := storage.NewSQLiteStore(dbPath, encryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize session store")
	}
	defer sessionStore.Close()
	log.Info().Str("dbPath", dbPath).Msg("session store initialized")

	// Create context that cancels on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize vision analyzer (GEMINI_API_KEY is required)
	if os.Getenv("GEMINI_API_KEY") == "" {
		log.Fatal().Msg("GEMINI_API_KEY environment variable is required")
	}
	visionAnalyzer, err := vision.NewGeminiAnalyzer(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize Gemini vision analyzer")
	}
	log.Info().Msg("Gemini vision analyzer initialized")

	g, ctx := errgroup.WithContext(ctx)

	// Run bot update loop
	g.Go(func() error {
		return runBot(ctx, tg, sessionStore, visionAnalyzer)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Error().Err(err).Msg("shutdown with error")
	} else {
		log.Info().Msg("shutdown complete")
	}
}

func runBot(ctx context.Context, tg *tgbotapi.BotAPI, sessionStore storage.SessionStore, visionAnalyzer vision.Analyzer) error {
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := tg.GetUpdatesChan(updateConfig)

	bot := NewBot(tg, tori.ApiBaseUrl, sessionStore)
	if visionAnalyzer != nil {
		bot.SetVisionAnalyzer(visionAnalyzer)
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
				bot.handleUpdate(ctx, u)
			}(update)
		}
	}
}

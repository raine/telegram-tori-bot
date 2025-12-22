package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
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

	tg, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize telegram bot; bad token?")
	}
	tg.Debug = false
	log.Info().Str("username", tg.Self.UserName).Msg("authorized on account")

	userConfigMap, err := readUserConfigMap()
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	// Create context that cancels on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// Run session keep-alive in background
	g.Go(func() error {
		return keepSessionsAlive(ctx, tori.ApiBaseUrl, userConfigMap)
	})

	// Run bot update loop
	g.Go(func() error {
		return runBot(ctx, tg, userConfigMap)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Error().Err(err).Msg("shutdown with error")
	} else {
		log.Info().Msg("shutdown complete")
	}
}

func runBot(ctx context.Context, tg *tgbotapi.BotAPI, userConfigMap UserConfigMap) error {
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := tg.GetUpdatesChan(updateConfig)

	bot := NewBot(tg, userConfigMap, tori.ApiBaseUrl)

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
				bot.handleUpdate(u)
			}(update)
		}
	}
}

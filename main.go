package main

import (
	"os"
	"os/signal"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

func main() {
	handleGracefulExit()
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	botToken, ok := os.LookupEnv("BOT_TOKEN")
	if !ok {
		log.Fatal().Msg("BOT_TOKEN is not set")
	}

	tg, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal().Err(err).Send()
		os.Exit(1)
	}
	tg.Debug = false
	log.Info().Str("username", tg.Self.UserName).Msg("authorized on account")

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := tg.GetUpdatesChan(updateConfig)

	userConfigMap, err := readUserConfigMap()
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	bot := NewBot(tg, userConfigMap, tori.ApiBaseUrl)

	for update := range updates {
		go bot.handleUpdate(update)
	}
}

func handleGracefulExit() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGINT,
		syscall.SIGTERM)

	go func() {
		s := <-sigc
		log.Info().Msgf("got %s, exiting", s)
		// The program doesn't really have anything to clean up so this should be fine
		os.Exit(1)
	}()
}

package main

import (
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/go-telegram-bot/tori"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

func main() {
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

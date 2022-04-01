package main

import (
	"fmt"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/lithammer/dedent"
	"github.com/pkg/errors"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

type UserSession struct {
	userId               int64
	client               *tori.Client
	listing              *tori.Listing
	toriAccountId        string
	bot                  *Bot
	mu                   sync.Mutex
	pendingPhotos        *[]tgbotapi.PhotoSize
	photos               []tgbotapi.PhotoSize
	categories           []tori.Category
	userSubjectMessageId int
	userBodyMessageId    int
	botSubjectMessageId  int
	botBodyMessageId     int
}

func (s *UserSession) reset() {
	log.Info().Int64("userId", s.userId).Msg("reset user session")
	s.listing = nil
	s.pendingPhotos = nil
	s.photos = nil
	s.categories = nil
	s.userSubjectMessageId = 0
}

func (s *UserSession) replyWithError(err error) {
	log.Error().Err(err).Send()
	msg := tgbotapi.NewMessage(0, fmt.Sprintf("Virhe: %s\n", err))
	s.replyWithMessage(msg)
}

func (s *UserSession) replyWithMessage(msg tgbotapi.MessageConfig) tgbotapi.Message {
	msg.ChatID = s.userId
	sent, err := s.bot.tg.Send(msg)
	if err != nil {
		log.Error().Stack().
			Interface("msg", msg).
			Err(errors.Wrap(err, "failed to send reply message")).Send()
	}

	return sent
}

func (s *UserSession) reply(text string, a ...any) tgbotapi.Message {
	msg := tgbotapi.NewMessage(0, fmt.Sprintf(strings.TrimSpace(dedent.Dedent(text)), a...))
	msg.ParseMode = tgbotapi.ModeMarkdown
	return s.replyWithMessage(msg)
}

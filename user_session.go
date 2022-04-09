package main

import (
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pkg/errors"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

type PendingPhoto struct {
	messageId int
	photoSize tgbotapi.PhotoSize
}

type UserSession struct {
	userId               int64
	client               *tori.Client
	listing              *tori.Listing
	toriAccountId        string
	bot                  *Bot
	mu                   sync.Mutex
	pendingPhotos        *[]PendingPhoto
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

func (s *UserSession) replyWithError(err error) tgbotapi.Message {
	log.Error().Stack().Err(errors.WithStack(err)).Send()
	return s._reply(formatReplyText(unexpectedErrorText, err), false)
}

func (s *UserSession) replyWithMessage(msg tgbotapi.MessageConfig) tgbotapi.Message {
	msg.ChatID = s.userId
	sent, err := s.bot.tg.Send(msg)
	if err != nil {
		log.Error().Stack().
			Interface("msg", msg).
			Err(errors.Wrap(err, "failed to send reply message")).Send()
	} else {
		log.Info().Interface("msg", msg).Interface("sent", sent).Msg("sent message")
	}

	return sent
}

func (s *UserSession) _reply(text string, removeReplyKeyboard bool) tgbotapi.Message {
	msg := tgbotapi.MessageConfig{
		Text:      text,
		ParseMode: tgbotapi.ModeMarkdown,
	}

	if removeReplyKeyboard {
		msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(false)
	}

	return s.replyWithMessage(msg)
}

func (s *UserSession) reply(text string, a ...any) tgbotapi.Message {
	return s._reply(formatReplyText(text, a...), false)
}

// replyAndRemoveCustomKeyboard sends a text as reply while removing any
// existing custom reply keyboard. In telegram, bot's custom keyboards will
// remain as long as a new one is sent or the current one is removed. If
// not removed manually, you will often see custom keyboards that are no
// longer valid in the context.
func (s *UserSession) replyAndRemoveCustomKeyboard(text string, a ...any) tgbotapi.Message {
	return s._reply(formatReplyText(text, a...), true)
}

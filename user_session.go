package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/go-telegram-bot/tori"
	"github.com/rs/zerolog/log"
)

type UserSession struct {
	userId        int64
	client        *tori.Client
	listing       *tori.Listing
	bot           *Bot
	mu            sync.Mutex
	pendingPhotos *[]tgbotapi.PhotoSize
	photos        []tgbotapi.PhotoSize
	categories    []tori.Category
}

func (s *UserSession) reset() {
	log.Info().Int64("userId", s.userId).Msg("reset user session")
	s.listing = nil
	s.photos = nil
	s.categories = nil
}

func (s *UserSession) replyWithError(err error) {
	msg := tgbotapi.NewMessage(0, fmt.Sprintf("Virhe: %s\n", err))
	s.replyWithMessage(msg)
}

func (s *UserSession) replyWithMessage(msg tgbotapi.MessageConfig) {
	msg.ChatID = s.userId
	_, err := s.bot.tg.Send(msg)
	if err != nil {
		log.Error().Stack().Err(err).Interface("msg", msg).Msg("failed to send reply message")
	}
}

func (s *UserSession) handlePhoto(message *tgbotapi.Message) {
	// When photos are sent as a "media group" that appear like a single message
	// with multiple photos, the photos are in fact sent one by one in separate
	// messages. To give feedback like "n photos added", we have to wait a bit
	// after the first photo is sent and keep track of photos since then
	if s.pendingPhotos == nil {
		s.pendingPhotos = new([]tgbotapi.PhotoSize)

		go func() {
			env, _ := os.LookupEnv("GO_ENV")
			if env == "test" {
				time.Sleep(100 * time.Microsecond)
			} else {
				time.Sleep(1 * time.Second)
			}
			s.photos = append(s.photos, *s.pendingPhotos...)
			s.reply("%s lisätty", pluralize("kuva", "kuvaa", len(*s.pendingPhotos)))
			s.pendingPhotos = nil
		}()
	}

	// message.Photo is an array of PhotoSizes and the last one is the largest size
	largestPhoto := message.Photo[len(message.Photo)-1]
	log.Info().Interface("photo", largestPhoto).Msg("added photo")
	pendingPhotos := append(*s.pendingPhotos, largestPhoto)
	s.pendingPhotos = &pendingPhotos
}

func (s *UserSession) reply(text string, a ...any) {
	msg := tgbotapi.NewMessage(0, fmt.Sprintf(text, a...))
	msg.ParseMode = tgbotapi.ModeMarkdown
	s.replyWithMessage(msg)
}

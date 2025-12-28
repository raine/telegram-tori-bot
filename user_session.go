package main

import (
	"fmt"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"

	"github.com/raine/telegram-tori-bot/tori"
)

// isLoggedIn returns true if the user has a valid bearer token (internal, no lock)
func (s *UserSession) isLoggedIn() bool {
	return s.bearerToken != ""
}

// escapeMarkdown escapes special characters for Telegram Markdown V1
func escapeMarkdown(text string) string {
	text = strings.ReplaceAll(text, "*", "\\*")
	text = strings.ReplaceAll(text, "_", "\\_")
	text = strings.ReplaceAll(text, "`", "\\`")
	text = strings.ReplaceAll(text, "[", "\\[")
	return text
}

// MessageSender abstracts the ability to send Telegram messages.
// This interface decouples UserSession from the full Bot struct,
// improving testability.
type MessageSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

type PendingPhoto struct {
	messageId int
	photoSize tgbotapi.PhotoSize
}

type UserSession struct {
	userId        int64
	bearerToken   string
	toriAccountId string
	refreshToken  string
	deviceID      string
	sender        MessageSender
	mu            sync.Mutex

	// Photo collection
	pendingPhotos *[]PendingPhoto
	photos        []tgbotapi.PhotoSize

	// Auth flow state for login
	authFlow *AuthFlow

	// Adinput API state for ad creation
	adInputClient    *tori.AdinputClient
	draftID          string
	etag             string
	adAttributes     *tori.AttributesResponse
	currentDraft     *AdInputDraft
	isCreatingDraft  bool // Prevents concurrent draft creation from album photos
}

// --- Thread-safe accessors ---

// IsLoggedIn returns true if the user has an authenticated session.
func (s *UserSession) IsLoggedIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bearerToken != ""
}

// GetAdInputClient returns the adinput client (creates if needed).
func (s *UserSession) GetAdInputClient() *tori.AdinputClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initAdInputClient()
	return s.adInputClient
}

// HasActiveDraft returns true if there's an active draft being created.
func (s *UserSession) HasActiveDraft() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draftID != ""
}

// GetDraftState returns the current draft's state, or AdFlowStateNone if no draft.
func (s *UserSession) GetDraftState() AdFlowState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentDraft == nil {
		return AdFlowStateNone
	}
	return s.currentDraft.State
}

// GetDraftInfo returns draft ID and etag for API calls.
func (s *UserSession) GetDraftInfo() (draftID, etag string, client *tori.AdinputClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draftID, s.etag, s.adInputClient
}

// UpdateETag updates the etag after API operations.
func (s *UserSession) UpdateETag(newETag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.etag = newETag
}

// SetAdAttributes stores the category attributes.
func (s *UserSession) SetAdAttributes(attrs *tori.AttributesResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adAttributes = attrs
}

// PhotoCount returns the number of photos in the current listing.
func (s *UserSession) PhotoCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.photos)
}

// AddPhoto adds a photo to the current listing.
func (s *UserSession) AddPhoto(photo tgbotapi.PhotoSize) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.photos = append(s.photos, photo)
}

// --- Auth flow accessors ---

// IsAuthFlowActive returns true if an auth flow is in progress.
func (s *UserSession) IsAuthFlowActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authFlow != nil && s.authFlow.IsActive()
}

// IsAuthFlowTimedOut returns true if the auth flow has timed out.
func (s *UserSession) IsAuthFlowTimedOut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authFlow != nil && s.authFlow.IsTimedOut()
}

// GetAuthFlowState returns the current auth flow state.
func (s *UserSession) GetAuthFlowState() AuthState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authFlow == nil {
		return AuthStateNone
	}
	return s.authFlow.State
}

func (s *UserSession) reset() {
	log.Info().Int64("userId", s.userId).Msg("reset user session")
	s.pendingPhotos = nil
	s.photos = nil
	if s.authFlow != nil {
		s.authFlow.Reset()
	}
	// Reset adinput API state
	s.adInputClient = nil
	s.draftID = ""
	s.etag = ""
	s.adAttributes = nil
	s.currentDraft = nil
	s.isCreatingDraft = false
}

func (s *UserSession) replyWithError(err error) tgbotapi.Message {
	log.Error().Stack().Err(err).Send()
	return s._reply(formatReplyText(unexpectedErrorText, err), false)
}

func (s *UserSession) replyWithMessage(msg tgbotapi.MessageConfig) tgbotapi.Message {
	msg.ChatID = s.userId
	sent, err := s.sender.Send(msg)
	if err != nil {
		log.Error().Stack().
			Interface("msg", msg).
			Err(fmt.Errorf("failed to send reply message: %w", err)).Send()
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

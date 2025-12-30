package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"

	"github.com/raine/telegram-tori-bot/tori"
)

// SessionMessage represents a message to be processed by the session worker.
type SessionMessage struct {
	Type  string
	Ctx   context.Context
	Done  chan struct{} // Closed when processing is complete (for synchronous dispatch)
	Error chan error    // Optional: for returning errors

	// Message data (only one is set based on Type)
	Message       *tgbotapi.Message
	CallbackQuery *tgbotapi.CallbackQuery
	Text          string       // For auth flow messages
	AlbumBuffer   *AlbumBuffer // For album_timeout messages

	// Bulk mode message data
	BulkAnalysisResult *BulkAnalysisResult // For bulk_analysis_complete messages
	BulkDraftError     *BulkDraftError     // For bulk_draft_error messages
}

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

// AlbumPhoto holds a photo from an album with its Telegram data.
type AlbumPhoto struct {
	FileID string
	Width  int
	Height int
}

// AlbumBuffer collects photos from a Telegram album (MediaGroup) before processing.
type AlbumBuffer struct {
	MediaGroupID  string
	Photos        []AlbumPhoto
	Timer         *time.Timer
	FirstReceived time.Time
}

// MessageHandler is the interface for processing session messages.
// This allows the session to dispatch to external handlers without circular dependencies.
type MessageHandler interface {
	HandleSessionMessage(ctx context.Context, session *UserSession, msg SessionMessage)
}

// UserSession represents a user's session with the bot.
//
// Threading model:
//   - Each session has a dedicated worker goroutine that processes messages sequentially
//   - Message handlers (HandlePhoto, HandleInput, etc.) are called only from the worker
//     and can access session state without locks
//   - Public accessors (IsLoggedIn, GetDraftState, etc.) use mutex for external callers
//   - TryRefreshTokens is an exception: it may be called externally and uses mutex
//
// Note: There is a potential race between TryRefreshTokens (with lock) writing
// bearerToken and the worker (without lock) reading it. In practice, token refresh
// happens infrequently and the race is benign (stale read of old valid token).
type UserSession struct {
	userId        int64
	bearerToken   string
	toriAccountId string
	refreshToken  string
	deviceID      string
	sender        MessageSender
	mu            sync.Mutex // For thread-safe accessors and TryRefreshTokens

	// Worker channel for sequential message processing
	inbox   chan SessionMessage
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	handler MessageHandler // Set after construction to avoid circular deps

	// Photo collection
	pendingPhotos *[]PendingPhoto
	photos        []tgbotapi.PhotoSize
	albumBuffer   *AlbumBuffer // Buffer for collecting album photos

	// Auth flow state for login
	authFlow *AuthFlow

	// Adinput API state for ad creation
	adInputClient   tori.AdService
	draftID         string
	etag            string
	adAttributes    *tori.AttributesResponse
	currentDraft    *AdInputDraft
	isCreatingDraft bool // Prevents concurrent draft creation from album photos

	// Postal code command state
	awaitingPostalCodeInput bool

	// Bulk listing mode
	bulkSession *BulkSession
}

// --- Thread-safe accessors ---

// IsLoggedIn returns true if the user has an authenticated session.
func (s *UserSession) IsLoggedIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bearerToken != ""
}

// GetAdInputClient returns the adinput client (creates if needed).
func (s *UserSession) GetAdInputClient() tori.AdService {
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
func (s *UserSession) GetDraftInfo() (draftID, etag string, client tori.AdService) {
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
	// Stop album timer if running
	if s.albumBuffer != nil && s.albumBuffer.Timer != nil {
		s.albumBuffer.Timer.Stop()
	}
	s.albumBuffer = nil
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
	s.awaitingPostalCodeInput = false
	// Note: bulk session is NOT reset here - use EndBulkSession() explicitly
}

// deleteCurrentDraft deletes the current draft ad from Tori API.
// Called from session worker - no locking needed.
func (s *UserSession) deleteCurrentDraft(ctx context.Context) {
	if s.draftID != "" && s.adInputClient != nil {
		if err := s.adInputClient.DeleteAd(ctx, s.draftID); err != nil {
			log.Warn().Err(err).Str("draftID", s.draftID).Msg("failed to delete draft ad on cancel")
		} else {
			log.Info().Str("draftID", s.draftID).Msg("deleted draft ad on cancel")
		}
	}
}

// --- Bulk session methods ---

// IsInBulkMode returns true if the session is in bulk listing mode.
// Called from session worker - no locking needed.
func (s *UserSession) IsInBulkMode() bool {
	return s.bulkSession != nil && s.bulkSession.Active
}

// GetBulkSession returns the current bulk session, or nil if not in bulk mode.
// Called from session worker - no locking needed.
func (s *UserSession) GetBulkSession() *BulkSession {
	return s.bulkSession
}

// StartBulkSession starts a new bulk listing session.
// Called from session worker - no locking needed.
func (s *UserSession) StartBulkSession() {
	// Clean up any existing single listing state
	s.reset()
	s.bulkSession = NewBulkSession()
	log.Info().Int64("userId", s.userId).Msg("started bulk session")
}

// EndBulkSession ends the current bulk listing session.
// Called from session worker - no locking needed.
func (s *UserSession) EndBulkSession() {
	if s.bulkSession == nil {
		return
	}

	// Cancel any ongoing analyses
	for _, draft := range s.bulkSession.Drafts {
		if draft.CancelAnalysis != nil {
			draft.CancelAnalysis()
		}
	}

	// Stop update timer
	if s.bulkSession.UpdateTimer != nil {
		s.bulkSession.UpdateTimer.Stop()
	}

	// Stop album buffer timer
	if s.bulkSession.AlbumBuffer != nil && s.bulkSession.AlbumBuffer.Timer != nil {
		s.bulkSession.AlbumBuffer.Timer.Stop()
	}

	s.bulkSession = nil
	log.Info().Int64("userId", s.userId).Msg("ended bulk session")
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

// --- Worker methods ---

// StartWorker starts the session's message processing worker goroutine.
// Must be called after setting the handler.
func (s *UserSession) StartWorker() {
	s.wg.Add(1)
	go s.runWorker()
}

// SetHandler sets the message handler for this session.
func (s *UserSession) SetHandler(handler MessageHandler) {
	s.handler = handler
}

// runWorker is the main worker loop that processes messages sequentially.
func (s *UserSession) runWorker() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			// Drain any remaining messages and signal completion
			for {
				select {
				case msg := <-s.inbox:
					if msg.Done != nil {
						close(msg.Done)
					}
				default:
					return
				}
			}
		case msg := <-s.inbox:
			s.processMessage(msg)
		}
	}
}

// processMessage handles a single message from the inbox.
func (s *UserSession) processMessage(msg SessionMessage) {
	defer func() {
		// Recover from any panics to keep the worker running
		if r := recover(); r != nil {
			log.Error().
				Int64("userId", s.userId).
				Interface("panic", r).
				Msg("recovered from panic in session worker")
		}
		if msg.Done != nil {
			close(msg.Done)
		}
	}()

	if s.handler == nil {
		log.Error().Int64("userId", s.userId).Msg("session handler not set")
		return
	}

	s.handler.HandleSessionMessage(msg.Ctx, s, msg)
}

// Send queues a message for processing by the worker.
// This is non-blocking - it returns immediately after queuing.
func (s *UserSession) Send(msg SessionMessage) {
	select {
	case s.inbox <- msg:
	case <-s.ctx.Done():
		if msg.Done != nil {
			close(msg.Done)
		}
	}
}

// SendSync queues a message and waits for it to be processed.
// Returns when the message has been fully processed by the worker.
func (s *UserSession) SendSync(msg SessionMessage) {
	msg.Done = make(chan struct{})
	s.Send(msg)
	<-msg.Done
}

// Stop stops the worker and waits for it to finish.
func (s *UserSession) Stop() {
	s.cancel()
	s.wg.Wait()
}

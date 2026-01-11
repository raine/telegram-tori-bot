package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog/log"

	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
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

	// Draft expiration data
	ExpiredTimer *time.Timer // For draft_expired messages - used to validate the timer is still current

	// Bulk mode message data
	BulkAnalysisResult *BulkAnalysisResult // For bulk_analysis_complete messages
	BulkDraftError     *BulkDraftError     // For bulk_draft_error messages

	// Background publish data
	PublishResult *PublishResult // For publish_complete messages
}

// PublishResult contains the result of a background publish operation.
type PublishResult struct {
	Title   string // Title of the published listing
	Price   int    // Price of the listing
	DraftID string // Tori draft ID
	Error   error  // Error if publish failed
}

// isLoggedIn returns true if the user has a valid bearer token (internal, no lock)
func (s *UserSession) isLoggedIn() bool {
	return s.auth.BearerToken != ""
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
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
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

// AlbumBufferConfig holds configuration for album buffering behavior.
type AlbumBufferConfig struct {
	// GetBuffer returns the current album buffer (may be nil).
	GetBuffer func() *AlbumBuffer
	// SetBuffer sets the album buffer.
	SetBuffer func(buffer *AlbumBuffer)
	// OnFlush is called when a buffer needs to be processed (different album arrived).
	OnFlush func(ctx context.Context, photos []AlbumPhoto)
	// OnTimeout is called when the timer fires.
	OnTimeout func(buffer *AlbumBuffer)
	// Timeout duration for waiting for more photos.
	Timeout time.Duration
	// MaxPhotos is the maximum number of photos to buffer.
	MaxPhotos int
}

// AuthContext holds authentication-related state for a user session.
type AuthContext struct {
	BearerToken   string
	RefreshToken  string
	ToriAccountID string
	DeviceID      string
}

// DraftState holds ad draft creation state.
type DraftState struct {
	AdInputClient   tori.AdService
	DraftID         string
	Etag            string
	AdAttributes    *tori.AttributesResponse
	CurrentDraft    *AdInputDraft
	IsCreatingDraft bool // Prevents concurrent draft creation from album photos
}

// ListingBrowserState holds state for browsing user's existing listings.
type ListingBrowserState struct {
	MenuMsgID        int              // Message ID to edit for navigation
	BrowsePage       int              // Current pagination page (1-based)
	CachedListings   []tori.AdSummary // Cache of the current page's listings
	ActiveListingID  int64            // ID of listing being viewed (for detail view)
	ShowOldListings  bool             // Show expired/sold listings
	DeletedListingID string           // ID of listing just deleted (to filter from stale API)
}

// PhotoCollectionState holds state for collecting photos for a listing.
type PhotoCollectionState struct {
	PendingPhotos *[]PendingPhoto
	Photos        []tgbotapi.PhotoSize
	AlbumBuffer   *AlbumBuffer // Buffer for collecting album photos
}

// SearchWatchState holds state for search watch feature.
type SearchWatchState struct {
	PendingQuery string // Stores the query from the last /haku command for callback
}

// BufferAlbumPhoto adds a photo to the album buffer and schedules processing.
// This is a shared implementation used by both ListingHandler and BulkHandler.
func BufferAlbumPhoto(ctx context.Context, photo AlbumPhoto, mediaGroupID string, config AlbumBufferConfig) {
	buffer := config.GetBuffer()

	// Initialize or update album buffer
	if buffer == nil || buffer.MediaGroupID != mediaGroupID {
		// If there's an existing buffer with photos from a different album, flush it first
		if buffer != nil && len(buffer.Photos) > 0 {
			if buffer.Timer != nil {
				buffer.Timer.Stop()
			}
			config.OnFlush(ctx, buffer.Photos)
		}
		buffer = &AlbumBuffer{
			MediaGroupID:  mediaGroupID,
			Photos:        []AlbumPhoto{},
			FirstReceived: time.Now(),
		}
		config.SetBuffer(buffer)
	}

	// Add photo to buffer (respect max limit)
	if len(buffer.Photos) < config.MaxPhotos {
		buffer.Photos = append(buffer.Photos, photo)
	}

	// Reset or start timer - dispatch through worker channel when done
	if buffer.Timer != nil {
		buffer.Timer.Stop()
	}

	// Capture buffer reference for timer closure
	capturedBuffer := buffer
	buffer.Timer = time.AfterFunc(config.Timeout, func() {
		config.OnTimeout(capturedBuffer)
	})
}

// ProcessAlbumBufferTimeout is a shared helper for handling album timeout.
// It validates the buffer is still current, clears it, and returns the photos.
// Returns nil if the buffer is stale or empty.
func ProcessAlbumBufferTimeout(albumBuffer *AlbumBuffer, config AlbumBufferConfig) []AlbumPhoto {
	currentBuffer := config.GetBuffer()

	// Verify this is still the active album buffer (wasn't replaced or cleared)
	if currentBuffer != albumBuffer {
		return nil
	}

	// Clear the album buffer
	photos := albumBuffer.Photos
	config.SetBuffer(nil)

	if len(photos) == 0 {
		return nil
	}

	return photos
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
	userId int64
	sender MessageSender
	store  storage.SessionStore // For per-user settings like installation ID
	mu     sync.Mutex           // For thread-safe accessors and TryRefreshTokens

	// Worker channel for sequential message processing
	inbox   chan SessionMessage
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	handler MessageHandler // Set after construction to avoid circular deps

	// Domain state - grouped into sub-structs for cleaner separation
	auth     AuthContext          // Authentication state
	draft    DraftState           // Ad draft creation state
	listings ListingBrowserState  // Listing browser state
	photoCol PhotoCollectionState // Photo collection state
	search   SearchWatchState     // Search watch state

	// Auth flow state for login
	authFlow *AuthFlow

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
	return s.auth.BearerToken != ""
}

// GetAdInputClient returns the adinput client (creates if needed).
func (s *UserSession) GetAdInputClient() tori.AdService {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initAdInputClient()
	return s.draft.AdInputClient
}

// HasActiveDraft returns true if there's an active draft being created.
func (s *UserSession) HasActiveDraft() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draft.DraftID != ""
}

// GetDraftState returns the current draft's state, or AdFlowStateNone if no draft.
func (s *UserSession) GetDraftState() AdFlowState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft.CurrentDraft == nil {
		return AdFlowStateNone
	}
	return s.draft.CurrentDraft.State
}

// GetDraftInfo returns draft ID and etag for API calls.
func (s *UserSession) GetDraftInfo() (draftID, etag string, client tori.AdService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draft.DraftID, s.draft.Etag, s.draft.AdInputClient
}

// UpdateETag updates the etag after API operations.
func (s *UserSession) UpdateETag(newETag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draft.Etag = newETag
}

// SetAdAttributes stores the category attributes.
func (s *UserSession) SetAdAttributes(attrs *tori.AttributesResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draft.AdAttributes = attrs
}

// PhotoCount returns the number of photos in the current listing.
func (s *UserSession) PhotoCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.photoCol.Photos)
}

// AddPhoto adds a photo to the current listing.
func (s *UserSession) AddPhoto(photo tgbotapi.PhotoSize) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.photoCol.Photos = append(s.photoCol.Photos, photo)
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
	// Reset photo collection state
	s.photoCol.PendingPhotos = nil
	s.photoCol.Photos = nil
	// Stop album timer if running
	if s.photoCol.AlbumBuffer != nil && s.photoCol.AlbumBuffer.Timer != nil {
		s.photoCol.AlbumBuffer.Timer.Stop()
	}
	s.photoCol.AlbumBuffer = nil
	if s.authFlow != nil {
		s.authFlow.Reset()
	}
	// Stop draft expiration timer if running
	s.stopDraftExpirationTimer()
	// Reset draft state
	s.draft.AdInputClient = nil
	s.draft.DraftID = ""
	s.draft.Etag = ""
	s.draft.AdAttributes = nil
	s.draft.CurrentDraft = nil
	s.draft.IsCreatingDraft = false
	s.awaitingPostalCodeInput = false
	// Note: bulk session is NOT reset here - use EndBulkSession() explicitly

	// Reset listing browser state
	s.listings.MenuMsgID = 0
	s.listings.BrowsePage = 0
	s.listings.CachedListings = nil
	s.listings.ActiveListingID = 0
	s.listings.ShowOldListings = false
	s.listings.DeletedListingID = ""
}

// stopDraftExpirationTimer stops the draft expiration timer if running.
// Called from session worker - no locking needed.
func (s *UserSession) stopDraftExpirationTimer() {
	if s.draft.CurrentDraft != nil && s.draft.CurrentDraft.ExpirationTimer != nil {
		s.draft.CurrentDraft.ExpirationTimer.Stop()
		s.draft.CurrentDraft.ExpirationTimer = nil
	}
}

// deleteCurrentDraft deletes the current draft ad from Tori API.
// Called from session worker - no locking needed.
func (s *UserSession) deleteCurrentDraft(ctx context.Context) {
	if s.draft.DraftID != "" && s.draft.AdInputClient != nil {
		if err := s.draft.AdInputClient.DeleteAd(ctx, s.draft.DraftID); err != nil {
			log.Warn().Err(err).Str("draftID", s.draft.DraftID).Msg("failed to delete draft ad on cancel")
		} else {
			log.Info().Str("draftID", s.draft.DraftID).Msg("deleted draft ad on cancel")
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
	return s._reply(formatReplyText(MsgUnexpectedErr, err), false)
}

// sendTypingAction sends a "typing" chat action to show the user that the bot is processing.
// The typing indicator automatically expires after ~5 seconds in Telegram.
func (s *UserSession) sendTypingAction() {
	action := tgbotapi.NewChatAction(s.userId, tgbotapi.ChatTyping)
	// Use Request instead of Send because sendChatAction returns a boolean, not a Message
	_, err := s.sender.Request(action)
	if err != nil {
		log.Debug().Err(err).Int64("userId", s.userId).Msg("failed to send typing action")
	}
}

// startTypingLoop sends a typing action every 4 seconds until the context is cancelled.
// This keeps the typing indicator visible during long-running operations like image analysis.
// Run this in a goroutine and cancel the context when done.
func (s *UserSession) startTypingLoop(ctx context.Context) {
	// Send immediately
	s.sendTypingAction()

	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendTypingAction()
		}
	}
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

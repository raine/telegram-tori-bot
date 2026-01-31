package bot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/llm"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/rs/zerolog/log"
)

const (
	// albumBufferTimeout is how long to wait for more photos before processing an album
	albumBufferTimeout = 1500 * time.Millisecond
	// maxAlbumPhotos is the maximum number of photos to process in an album
	maxAlbumPhotos = 10
	// draftExpirationTimeout is how long a draft can be inactive before it expires
	draftExpirationTimeout = 10 * time.Minute
)

// ListingHandler handles ad creation flow for the bot.
type ListingHandler struct {
	tg               BotAPI
	visionAnalyzer   llm.Analyzer
	editIntentParser llm.EditIntentParser
	sessionStore     storage.SessionStore
	searchClient     *tori.SearchClient
	categoryService  *tori.CategoryService
	categoryTree     *tori.CategoryTree
	draftService     *DraftService
}

// NewListingHandler creates a new listing handler.
func NewListingHandler(tg BotAPI, visionAnalyzer llm.Analyzer, editIntentParser llm.EditIntentParser, sessionStore storage.SessionStore) *ListingHandler {
	categoryService := tori.NewCategoryService()
	h := &ListingHandler{
		tg:               tg,
		visionAnalyzer:   visionAnalyzer,
		editIntentParser: editIntentParser,
		sessionStore:     sessionStore,
		searchClient:     tori.NewSearchClient(),
		categoryService:  categoryService,
		categoryTree:     categoryService.Tree,
	}
	// Create DraftService with callback to populate category cache
	h.draftService = NewDraftService(visionAnalyzer, tg.GetFileDirectURL).
		WithOnModelCallback(func(model *tori.AdModel) {
			if h.categoryService != nil && !h.categoryService.IsInitialized() {
				log.Info().Msg("initializing category cache from API model")
				h.categoryService.UpdateFromModel(model)
			}
		})
	return h
}

// HandleInput handles text inputs during the listing flow (replies, attributes, prices).
// Returns true if the message was handled.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleInput(ctx context.Context, session *UserSession, message *tgbotapi.Message) bool {
	// Handle replies to title/description messages (editing)
	if message.ReplyToMessage != nil {
		if h.HandleTitleDescriptionReply(session, message) {
			return true
		}
	}

	// Check current state to handle attribute/price input
	state := session.GetDraftState()

	// Touch draft activity for any input during listing flow
	if state != AdFlowStateNone {
		h.touchDraftActivity(session)
	}

	// Handle /peru during input states
	if message.Text == "/peru" && (state == AdFlowStateAwaitingAttribute || state == AdFlowStateAwaitingPrice || state == AdFlowStateAwaitingPostalCode) {
		session.deleteCurrentDraft(ctx)
		session.reset()
		session.replyAndRemoveCustomKeyboard(MsgOk)
		return true
	}

	// Let /osasto pass through to command handler during input states
	if message.Text == "/osasto" && (state == AdFlowStateAwaitingAttribute || state == AdFlowStateAwaitingPrice || state == AdFlowStateAwaitingPostalCode) {
		return false
	}

	// Handle attribute input
	if state == AdFlowStateAwaitingAttribute {
		h.HandleAttributeInput(ctx, session, message.Text)
		return true
	}

	// Handle price input
	if state == AdFlowStateAwaitingPrice {
		h.HandlePriceInput(ctx, session, message.Text)
		return true
	}

	// Handle postal code input
	if state == AdFlowStateAwaitingPostalCode {
		h.HandlePostalCodeInput(session, message.Text)
		return true
	}

	return false
}

// HandleSendListingCommand handles the /laheta command.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleSendListingCommand(ctx context.Context, session *UserSession) {
	h.HandleSendListing(ctx, session)
}

// HandlePhoto processes a photo message and starts or adds to the listing flow.
// Detects album photos (MediaGroupID) and buffers them for multi-image analysis.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePhoto(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	// Get the largest photo size
	largestPhoto := message.Photo[len(message.Photo)-1]
	mediaGroupID := message.MediaGroupID

	// Check if this is part of an album (has MediaGroupID)
	if mediaGroupID != "" {
		h.bufferAlbumPhoto(ctx, session, largestPhoto, mediaGroupID)
		return
	}

	// Single photo - process immediately
	h.processSinglePhoto(ctx, session, largestPhoto)
}

// bufferAlbumPhoto adds a photo to the album buffer and schedules processing.
// Called from session worker - no locking needed for session state.
func (h *ListingHandler) bufferAlbumPhoto(ctx context.Context, session *UserSession, photo tgbotapi.PhotoSize, mediaGroupID string) {
	// If there's an existing draft, add photos directly without buffering
	if session.draft.DraftID != "" {
		h.processSinglePhoto(ctx, session, photo)
		return
	}

	// If already creating a draft, reject this photo
	if session.draft.IsCreatingDraft {
		session.reply("Odota hetki, luodaan ilmoitusta...")
		return
	}

	BufferAlbumPhoto(ctx, AlbumPhoto{
		FileID: photo.FileID,
		Width:  photo.Width,
		Height: photo.Height,
	}, mediaGroupID, h.albumBufferConfig(session))
}

// albumBufferConfig returns the configuration for album buffering in listing mode.
func (h *ListingHandler) albumBufferConfig(session *UserSession) AlbumBufferConfig {
	return AlbumBufferConfig{
		GetBuffer: func() *AlbumBuffer { return session.photoCol.AlbumBuffer },
		SetBuffer: func(buffer *AlbumBuffer) { session.photoCol.AlbumBuffer = buffer },
		OnFlush: func(ctx context.Context, photos []AlbumPhoto) {
			// Process the previous album immediately (this may set isCreatingDraft=true,
			// causing the current photo to be rejected, which is safer than dropping data)
			h.processAlbumPhotos(ctx, session, photos)
		},
		OnTimeout: func(buffer *AlbumBuffer) {
			// Dispatch album processing through the worker channel
			// Use context.Background() since the original request context may be cancelled by now
			session.Send(SessionMessage{
				Type:        "album_timeout",
				Ctx:         context.Background(),
				AlbumBuffer: buffer,
			})
		},
		Timeout:   albumBufferTimeout,
		MaxPhotos: maxAlbumPhotos,
	}
}

// ProcessAlbumTimeout handles the album timeout message from the worker channel.
// Called from session worker - no locking needed.
func (h *ListingHandler) ProcessAlbumTimeout(ctx context.Context, session *UserSession, albumBuffer *AlbumBuffer) {
	photos := ProcessAlbumBufferTimeout(albumBuffer, h.albumBufferConfig(session))
	if photos == nil {
		return
	}
	h.processAlbumPhotos(ctx, session, photos)
}

// processAlbumPhotos converts AlbumPhoto slice to PhotoSize and processes the batch.
func (h *ListingHandler) processAlbumPhotos(ctx context.Context, session *UserSession, photos []AlbumPhoto) {
	// Convert AlbumPhoto to tgbotapi.PhotoSize
	photoSizes := make([]tgbotapi.PhotoSize, len(photos))
	for i, p := range photos {
		photoSizes[i] = tgbotapi.PhotoSize{
			FileID: p.FileID,
			Width:  p.Width,
			Height: p.Height,
		}
	}

	// Process all photos together
	h.processPhotoBatch(ctx, session, photoSizes)
}

// HandleDraftExpired handles the draft expiration timeout message.
// It deletes the draft from Tori API, resets session state, and notifies the user.
// The timer parameter is used to validate that this expiration message corresponds
// to the current timer (not a stale message from a previous timer that was reset).
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleDraftExpired(ctx context.Context, session *UserSession, timer *time.Timer) {
	// Verify there's still an active draft (user may have cancelled or published)
	if session.draft.DraftID == "" || session.draft.CurrentDraft == nil {
		return
	}

	// Ignore stale timer events - if the timer was reset (user activity),
	// the ExpirationTimer will point to a different timer instance
	if session.draft.CurrentDraft.ExpirationTimer != timer {
		log.Debug().
			Int64("userId", session.userId).
			Msg("ignoring stale draft expiration message")
		return
	}

	log.Info().
		Int64("userId", session.userId).
		Str("draftID", session.draft.DraftID).
		Msg("draft expired due to inactivity")
	LogState(session.userId, "Draft expired due to inactivity (10 min timeout)")

	// Delete draft from Tori API
	session.deleteCurrentDraft(ctx)
	LogAPI(session.userId, "Deleted expired draft")

	// Reset session state
	session.reset()

	// Notify user
	session.replyAndRemoveCustomKeyboard(MsgDraftExpired)
	LogBot(session.userId, "Notified user of draft expiration")
}

// startDraftExpirationTimer starts or resets the draft expiration timer.
// When the timer fires, it dispatches a draft_expired message through the session worker.
// Called from session worker - no locking needed.
func (h *ListingHandler) startDraftExpirationTimer(session *UserSession) {
	if session.draft.CurrentDraft == nil {
		return
	}

	// Stop existing timer if any
	if session.draft.CurrentDraft.ExpirationTimer != nil {
		session.draft.CurrentDraft.ExpirationTimer.Stop()
	}

	// Start new timer - capture the timer instance to pass in the message
	// This allows HandleDraftExpired to validate the timer is still current
	var timer *time.Timer
	timer = time.AfterFunc(draftExpirationTimeout, func() {
		// Dispatch expiration through the worker channel
		// Use context.Background() since the original request context may be cancelled by now
		session.Send(SessionMessage{
			Type:         "draft_expired",
			Ctx:          context.Background(),
			ExpiredTimer: timer,
		})
	})
	session.draft.CurrentDraft.ExpirationTimer = timer
}

// touchDraftActivity resets the draft expiration timer due to user activity.
// Called from session worker - no locking needed.
func (h *ListingHandler) touchDraftActivity(session *UserSession) {
	if session.draft.CurrentDraft == nil || session.draft.DraftID == "" {
		return
	}
	h.startDraftExpirationTimer(session)
}

// processSinglePhoto processes a single photo (non-album).
// Called from session worker - no locking needed.
func (h *ListingHandler) processSinglePhoto(ctx context.Context, session *UserSession, photo tgbotapi.PhotoSize) {
	if session.draft.DraftID != "" {
		h.addPhotoToExistingDraft(ctx, session, photo)
	} else {
		h.processPhotoBatch(ctx, session, []tgbotapi.PhotoSize{photo})
	}
}

// processPhotoBatch processes one or more photos to create a new draft.
// Called from session worker - no locking needed.
func (h *ListingHandler) processPhotoBatch(ctx context.Context, session *UserSession, photos []tgbotapi.PhotoSize) {
	if len(photos) == 0 {
		return
	}

	// Check if already creating a draft
	if session.draft.IsCreatingDraft {
		session.reply("Odota hetki, luodaan ilmoitusta...")
		return
	}

	// Check if draft already exists (shouldn't happen but handle gracefully)
	if session.draft.DraftID != "" {
		return
	}

	// Start new listing log
	StartListingLog(session.userId)
	LogUser(session.userId, "Sent %d photo(s)", len(photos))

	// Mark that we're creating a draft
	session.draft.IsCreatingDraft = true
	if len(photos) > 1 {
		session.reply(fmt.Sprintf("Analysoidaan %d kuvaa...", len(photos)))
	} else {
		session.reply("Analysoidaan kuvaa...")
	}

	// Start typing indicator loop after the reply message (replies clear typing status).
	// This keeps the indicator visible during the long-running image analysis.
	typingCtx, cancelTyping := context.WithCancel(ctx)
	defer cancelTyping()
	go session.startTypingLoop(typingCtx)

	client := session.GetAdInputClient()

	if client == nil {
		session.draft.IsCreatingDraft = false
		session.reply(MsgConnectionInitFailed)
		return
	}

	// Convert photos to PhotoInput for DraftService
	photoInputs := make([]PhotoInput, len(photos))
	for i, p := range photos {
		photoInputs[i] = PhotoInput{
			FileID: p.FileID,
			Width:  p.Width,
			Height: p.Height,
		}
	}

	// Use DraftService to create the draft
	result, err := h.draftService.CreateDraftFromPhotos(ctx, client, photoInputs)
	if err != nil {
		session.draft.IsCreatingDraft = false
		LogError(session.userId, "Draft creation failed: %v", err)
		session.replyWithError(err)
		return
	}

	// Log vision analysis result
	LogLLM(session.userId, "Vision analysis result:")
	LogLLM(session.userId, "  Title: %s", result.Title)
	LogLLM(session.userId, "  Description: %s", result.Description)
	LogLLM(session.userId, "  Category predictions: %d", len(result.CategoryPredictions))
	for i, cat := range result.CategoryPredictions {
		LogLLM(session.userId, "    [%d] %s (ID: %d)", i+1, tori.GetCategoryPath(cat), cat.ID)
	}
	LogAPI(session.userId, "Draft created: ID=%s", result.DraftID)

	// Update session state
	session.draft.IsCreatingDraft = false
	session.photoCol.Photos = append(session.photoCol.Photos, photos...)
	session.draft.DraftID = result.DraftID
	session.draft.Etag = result.ETag

	// Initialize the draft with vision results
	session.draft.CurrentDraft = &AdInputDraft{
		State:               AdFlowStateAwaitingCategory,
		Title:               result.Title,
		Description:         result.Description,
		TradeType:           "1", // Default to sell
		CollectedAttrs:      make(map[string]string),
		Images:              result.Images,
		CategoryPredictions: result.CategoryPredictions,
	}

	// Start draft expiration timer
	h.startDraftExpirationTimer(session)

	// Send title message (user can reply to edit)
	titleMsg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(result.Title)))
	titleMsg.ParseMode = tgbotapi.ModeMarkdown
	sentTitle := session.replyWithMessage(titleMsg)
	session.draft.CurrentDraft.TitleMessageID = sentTitle.MessageID

	// Send description message (user can reply to edit)
	descMsg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(result.Description)))
	descMsg.ParseMode = tgbotapi.ModeMarkdown
	sentDesc := session.replyWithMessage(descMsg)
	session.draft.CurrentDraft.DescriptionMessageID = sentDesc.MessageID

	// Auto-select category using LLM if possible
	if len(result.CategoryPredictions) > 0 {
		// Try LLM-based category selection (includes two-stage fallback)
		selectionResult := h.tryAutoSelectCategory(ctx, result.Title, result.Description, result.CategoryPredictions)

		// Update session's category predictions if fallback found new categories
		if selectionResult.UsedFallback && len(selectionResult.UpdatedPredictions) > 0 {
			session.draft.CurrentDraft.CategoryPredictions = selectionResult.UpdatedPredictions
			LogLLM(session.userId, "Category fallback used, updated predictions")
		}

		if selectionResult.SelectedID > 0 {
			LogLLM(session.userId, "Auto-selected category ID: %d", selectionResult.SelectedID)
			h.ProcessCategorySelection(ctx, session, selectionResult.SelectedID)
			return
		}

		// Fall back to manual selection (use updated predictions if available)
		LogState(session.userId, "Awaiting manual category selection")
		LogBot(session.userId, "Showing category selection keyboard")
		categoriesToShow := selectionResult.UpdatedPredictions
		msg := tgbotapi.NewMessage(session.userId, MsgSelectCategory)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = makeCategoryPredictionKeyboard(categoriesToShow)
		session.replyWithMessage(msg)
	} else {
		// No categories predicted, use default
		session.draft.CurrentDraft.CategoryID = 76 // "Muu" category
		LogInternal(session.userId, "No category predictions, using default (Muu)")
		session.reply(MsgNoCategoryPredictions)
		h.promptForPrice(ctx, session)
	}
}

// addPhotoToExistingDraft adds a photo to an existing draft.
// Called from session worker - no locking needed.
func (h *ListingHandler) addPhotoToExistingDraft(ctx context.Context, session *UserSession, photo tgbotapi.PhotoSize) {
	// Touch draft activity on photo addition
	h.touchDraftActivity(session)

	session.reply(MsgAddingPhoto)
	// Send typing indicator after the reply (replies clear typing status)
	session.sendTypingAction()
	client := session.draft.AdInputClient
	draftID := session.draft.DraftID

	// Check for nil client (session may have been reset)
	if client == nil || draftID == "" {
		log.Warn().Int64("userId", session.userId).Msg("cannot add photo: no active draft")
		return
	}

	// Use DraftService to upload the additional photo
	photoInput := PhotoInput{
		FileID: photo.FileID,
		Width:  photo.Width,
		Height: photo.Height,
	}
	uploaded, newEtag, err := h.draftService.UploadAdditionalPhoto(ctx, client, draftID, session.draft.Etag, photoInput, session.draft.CurrentDraft.Images)
	if err != nil {
		session.replyWithError(err)
		return
	}

	// Update session state
	if session.draft.CurrentDraft == nil {
		log.Info().Int64("userId", session.userId).Msg("draft cancelled during photo upload")
		return
	}
	session.photoCol.Photos = append(session.photoCol.Photos, photo)
	session.draft.CurrentDraft.Images = append(session.draft.CurrentDraft.Images, *uploaded)
	session.draft.Etag = newEtag
	session.reply(MsgPhotoAdded, len(session.photoCol.Photos))
}

// HandleCategorySelection processes category selection from callback query.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleCategorySelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	categoryIDStr := strings.TrimPrefix(query.Data, "cat:")
	categoryID, err := strconv.Atoi(categoryIDStr)
	if err != nil {
		log.Error().Err(err).Str("data", query.Data).Msg("invalid category callback data")
		return
	}

	LogCallback(session.userId, "Category button pressed: cat:%s", categoryIDStr)

	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingCategory {
		session.reply(MsgNoActiveListing)
		return
	}

	// Touch draft activity on category selection
	h.touchDraftActivity(session)

	// Edit the original message to remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	h.ProcessCategorySelection(ctx, session, categoryID)
}

// ProcessCategorySelection handles the common category selection logic.
// It sets category, fetches attributes, and prompts for next step.
// Called from session worker - no locking needed.
func (h *ListingHandler) ProcessCategorySelection(ctx context.Context, session *UserSession, categoryID int) {
	if session.draft.CurrentDraft == nil {
		return
	}

	// Find category for logging and display
	var categoryLabel string
	var categoryPath string
	for _, cat := range session.draft.CurrentDraft.CategoryPredictions {
		if cat.ID == categoryID {
			categoryLabel = cat.Label
			categoryPath = tori.GetCategoryPath(cat)
			break
		}
	}

	session.draft.CurrentDraft.CategoryID = categoryID
	log.Info().Int("categoryId", categoryID).Str("label", categoryLabel).Msg("category selected")
	LogState(session.userId, "Category selected: %s (ID: %d)", categoryPath, categoryID)

	session.reply(fmt.Sprintf("Osasto: *%s*", categoryPath))
	LogBot(session.userId, "Osasto: %s", categoryPath)

	// Get client and draft info for network calls
	client := session.draft.AdInputClient
	draftID := session.draft.DraftID
	etag := session.draft.Etag

	// Set category on draft (network I/O)
	newEtag, err := h.setCategoryOnDraft(ctx, client, draftID, etag, categoryID)
	if err != nil {
		session.replyWithError(err)
		return
	}

	// Fetch attributes for this category (network I/O)
	attrs, err := client.GetAttributes(ctx, draftID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get attributes, skipping to price")
		if session.draft.CurrentDraft == nil {
			// Draft was cancelled during attribute fetch
			log.Info().Int64("userId", session.userId).Msg("draft cancelled during attribute fetch (error path)")
			return
		}
		session.draft.Etag = newEtag
		session.draft.CurrentDraft.State = AdFlowStateAwaitingPrice
		session.reply(MsgEnterPrice)
		return
	}

	if session.draft.CurrentDraft == nil {
		// Draft was cancelled during attribute fetch
		log.Info().Int64("userId", session.userId).Msg("draft cancelled during attribute fetch")
		return
	}

	session.draft.Etag = newEtag
	session.draft.AdAttributes = attrs

	// Get required SELECT attributes
	requiredAttrs := tori.GetRequiredSelectAttributes(attrs)

	// Try auto-selection for required attributes using LLM
	if len(requiredAttrs) > 0 {
		requiredAttrs = h.tryAutoSelectAttributes(ctx, session, requiredAttrs)
	}

	// Try to restore preserved attribute values (e.g., condition) if compatible
	if preserved := session.draft.CurrentDraft.PreservedValues; preserved != nil && len(requiredAttrs) > 0 {
		requiredAttrs = h.tryRestorePreservedAttributes(session, requiredAttrs, preserved)
	}

	if len(requiredAttrs) > 0 {
		session.draft.CurrentDraft.RequiredAttrs = requiredAttrs
		session.draft.CurrentDraft.CurrentAttrIndex = 0
		session.draft.CurrentDraft.State = AdFlowStateAwaitingAttribute
		h.promptForAttribute(session, requiredAttrs[0])
	} else {
		h.proceedAfterAttributes(ctx, session)
	}
}

// HandleAttributeInput handles user selection of an attribute value.
// Called from session worker - no locking needed.
// Returns false if the input was handled as an edit command instead.
func (h *ListingHandler) HandleAttributeInput(ctx context.Context, session *UserSession, text string) bool {
	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingAttribute {
		return true
	}

	LogUser(session.userId, "Attribute input: %s", text)

	attrs := session.draft.CurrentDraft.RequiredAttrs
	idx := session.draft.CurrentDraft.CurrentAttrIndex

	if idx >= len(attrs) {
		// Shouldn't happen, but handle gracefully
		h.proceedAfterAttributes(ctx, session)
		return true
	}

	currentAttr := attrs[idx]

	// Handle skip button - don't add anything to CollectedAttrs for this attribute
	if text == SkipButtonLabel {
		log.Info().Str("attr", currentAttr.Name).Msg("attribute skipped")
		LogUser(session.userId, "Skipped attribute: %s", currentAttr.Label)

		// Move to next attribute or price input
		session.draft.CurrentDraft.CurrentAttrIndex++
		if session.draft.CurrentDraft.CurrentAttrIndex < len(attrs) {
			nextAttr := attrs[session.draft.CurrentDraft.CurrentAttrIndex]
			h.promptForAttribute(session, nextAttr)
		} else {
			h.proceedAfterAttributes(ctx, session)
		}
		return true
	}

	// Find the selected option by label
	opt := tori.FindOptionByLabel(&currentAttr, text)
	if opt == nil {
		// Input doesn't match any attribute option - try parsing as edit command
		// This allows commands like "poista merkki" during attribute selection
		if h.HandleEditCommand(ctx, session, text) {
			return true
		}

		// Not an edit command either - invalid selection, prompt again
		LogBot(session.userId, "Invalid attribute input, re-prompting for: %s", currentAttr.Label)
		session.reply(MsgSelectAttributeRetry, SkipButtonLabel, strings.ToLower(currentAttr.Label))
		h.promptForAttribute(session, currentAttr)
		return true
	}

	// Store the selected value
	session.draft.CurrentDraft.CollectedAttrs[currentAttr.Name] = strconv.Itoa(opt.ID)
	log.Info().Str("attr", currentAttr.Name).Str("label", text).Int("optionId", opt.ID).Msg("attribute selected")
	LogUser(session.userId, "Selected %s: %s", currentAttr.Label, text)

	// Move to next attribute or price input
	session.draft.CurrentDraft.CurrentAttrIndex++
	if session.draft.CurrentDraft.CurrentAttrIndex < len(attrs) {
		nextAttr := attrs[session.draft.CurrentDraft.CurrentAttrIndex]
		h.promptForAttribute(session, nextAttr)
	} else {
		h.proceedAfterAttributes(ctx, session)
	}
	return true
}

// HandlePriceInput handles price input when awaiting price.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePriceInput(ctx context.Context, session *UserSession, text string) {
	LogUser(session.userId, "Price input: %s", text)

	// Check for giveaway selection
	if strings.Contains(text, "Annetaan") || strings.Contains(text, "🎁") {
		LogState(session.userId, "Giveaway selected")
		h.handleGiveawaySelection(ctx, session)
		return
	}

	// Parse price from text
	price, err := parsePriceMessage(text)
	if err != nil {
		session.reply(MsgPriceNotUnderstood)
		return
	}

	session.draft.CurrentDraft.Price = price
	session.draft.CurrentDraft.TradeType = TradeTypeSell
	session.draft.CurrentDraft.State = AdFlowStateAwaitingShipping
	LogState(session.userId, "Price set: %d€", price)

	// Remove reply keyboard and confirm price
	session.replyAndRemoveCustomKeyboard(MsgPriceConfirmed, price)
	LogBot(session.userId, "Asking about shipping")

	// Ask about shipping
	msg := tgbotapi.NewMessage(session.userId, MsgShippingQuestion)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(BtnYes, "shipping:yes"),
			tgbotapi.NewInlineKeyboardButtonData(BtnNo, "shipping:no"),
		),
	)
	session.replyWithMessage(msg)
}

// HandlePostalCodeInput handles postal code input when awaiting postal code.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePostalCodeInput(session *UserSession, text string) {
	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingPostalCode {
		return
	}

	LogUser(session.userId, "Postal code input: %s", text)
	postalCode := strings.TrimSpace(text)
	if !isValidPostalCode(postalCode) {
		session.reply(MsgPostalCodeInvalid)
		LogBot(session.userId, "Invalid postal code")
		return
	}

	// Save postal code
	if h.sessionStore != nil {
		if err := h.sessionStore.SetPostalCode(session.userId, postalCode); err != nil {
			log.Error().Err(err).Msg("failed to save postal code")
			LogError(session.userId, "Failed to save postal code: %v", err)
			session.replyWithError(err)
			return
		}
	}

	LogState(session.userId, "Postal code set: %s", postalCode)
	session.reply(MsgPostalCodeUpdated, postalCode)
	h.showAdSummary(session)
}

// handleGiveawaySelection handles the user selecting "Annetaan" (give away) mode.
// Called from session worker - no locking needed.
func (h *ListingHandler) handleGiveawaySelection(ctx context.Context, session *UserSession) {
	session.draft.CurrentDraft.Price = 0
	session.draft.CurrentDraft.TradeType = TradeTypeGive

	// Get current description for LLM rewrite
	originalDescription := session.draft.CurrentDraft.Description
	descMsgID := session.draft.CurrentDraft.DescriptionMessageID
	userId := session.userId

	// Rewrite description to use "Annetaan" phrasing (network I/O)
	var newDescription string
	if gemini := llm.GetGeminiAnalyzer(h.visionAnalyzer); gemini != nil {
		rewritten, err := gemini.RewriteDescriptionForGiveaway(ctx, originalDescription)
		if err != nil {
			log.Warn().Err(err).Msg("failed to rewrite description for giveaway, using original")
			newDescription = originalDescription
		} else {
			newDescription = rewritten
		}
	} else {
		newDescription = originalDescription
	}

	// Check if session state is still valid after network I/O
	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingPrice {
		return
	}

	// Update description if it was rewritten
	if newDescription != originalDescription {
		session.draft.CurrentDraft.Description = newDescription

		// Update the description message in chat
		if descMsgID != 0 {
			editMsg := tgbotapi.NewEditMessageText(
				userId,
				descMsgID,
				fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(newDescription)),
			)
			editMsg.ParseMode = tgbotapi.ModeMarkdown
			h.tg.Request(editMsg)
		}
	}

	// Giveaways are always no-shipping (meetup only)
	session.draft.CurrentDraft.ShippingPossible = false

	// Apply template now that shipping/giveaway state is known
	h.applyUserTemplate(session)

	// Remove reply keyboard and confirm giveaway
	session.replyAndRemoveCustomKeyboard(MsgPriceGiveaway)

	h.continueToPostalCodeOrSummary(session)
}

// HandleShippingSelection handles the shipping yes/no callback.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleShippingSelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	isYes := strings.HasSuffix(query.Data, ":yes")
	LogCallback(session.userId, "Shipping selection: %s", query.Data)

	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingShipping {
		return
	}

	// Touch draft activity on shipping selection
	h.touchDraftActivity(session)

	session.draft.CurrentDraft.ShippingPossible = isYes
	LogState(session.userId, "Shipping enabled: %v", isYes)

	// Remove the inline keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	// If shipping is enabled, fetch delivery page and check for saved address
	if isYes {
		if err := h.setupToriDiiliShipping(ctx, session); err != nil {
			log.Error().Err(err).Msg("failed to setup Tori Diili shipping")
			// Continue without shipping
			session.draft.CurrentDraft.ShippingPossible = false
		} else {
			// Apply template with confirmed shipping before prompting package size
			h.applyUserTemplate(session)
			// Successfully got address, prompt for package size
			return
		}
	}

	// Apply template after shipping choice is finalized
	h.applyUserTemplate(session)
	h.continueToPostalCodeOrSummary(session)
}

// setupToriDiiliShipping fetches the user's saved shipping address from Tori and prompts for package size.
// Returns an error if shipping cannot be set up (no saved address, API error, etc.)
func (h *ListingHandler) setupToriDiiliShipping(ctx context.Context, session *UserSession) error {
	if session.draft.AdInputClient == nil || session.draft.DraftID == "" {
		return fmt.Errorf("no draft client or ID")
	}

	// Fetch delivery page to get saved address
	deliveryPage, err := session.draft.AdInputClient.GetDeliveryPage(ctx, session.draft.DraftID)
	if err != nil {
		session.reply(MsgShippingSetupError)
		return fmt.Errorf("get delivery page: %w", err)
	}

	// Check if user has complete saved shipping address
	addr := deliveryPage.Sections.Shipping.Address
	if addr.Name == "" || addr.Address == "" || addr.PostalCode == "" || addr.City == "" {
		session.reply(MsgShippingNoProfile)
		return fmt.Errorf("incomplete saved shipping address")
	}

	// Get phone number (prefer mobilePhone, fall back to phoneNumber)
	phone := addr.MobilePhone
	if phone == "" {
		phone = addr.PhoneNumber
	}
	if phone == "" {
		session.reply(MsgShippingNoProfile)
		return fmt.Errorf("no phone number in shipping address")
	}

	// Save the address to the draft
	session.draft.CurrentDraft.SavedShippingAddress = &addr

	log.Info().
		Str("name", addr.Name).
		Str("address", addr.Address).
		Str("city", addr.City).
		Str("postalCode", addr.PostalCode).
		Msg("loaded Tori Diili shipping address")

	// Prompt for package size
	h.promptForPackageSize(session)
	return nil
}

// promptForPackageSize displays the package size selection buttons.
func (h *ListingHandler) promptForPackageSize(session *UserSession) {
	session.draft.CurrentDraft.State = AdFlowStateAwaitingPackageSize

	msg := tgbotapi.NewMessage(session.userId, MsgPackageSizePrompt)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("S · max 4kg · 2,99€", "pkgsize:SMALL"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("M · max 25kg · 4,99€", "pkgsize:MEDIUM"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("L · max 25kg · 12,99€", "pkgsize:LARGE"),
		),
	)
	session.replyWithMessage(msg)
}

// HandlePackageSizeSelection handles the package size callback for Tori Diili shipping.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePackageSizeSelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingPackageSize {
		return
	}

	h.touchDraftActivity(session)

	size := strings.TrimPrefix(query.Data, "pkgsize:")
	session.draft.CurrentDraft.PackageSize = size

	log.Info().Str("size", size).Msg("package size selected")
	LogCallback(session.userId, "Package size selected: %s", size)

	// Remove the inline keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	h.continueToPostalCodeOrSummary(session)
}

// continueToPostalCodeOrSummary checks if postal code is needed, otherwise shows summary.
func (h *ListingHandler) continueToPostalCodeOrSummary(session *UserSession) {
	// Check if postal code is set
	if h.sessionStore != nil {
		postalCode, err := h.sessionStore.GetPostalCode(session.userId)
		if err != nil {
			log.Error().Err(err).Msg("failed to get postal code")
		}
		if postalCode == "" {
			// Prompt for postal code
			session.draft.CurrentDraft.State = AdFlowStateAwaitingPostalCode
			session.reply(MsgPostalCodePrompt)
			return
		}
	}

	h.showAdSummary(session)
}

// HandlePublishCallback handles the publish/cancel button callbacks from the ad summary.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePublishCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	action := strings.TrimPrefix(query.Data, "publish:")

	// Guard against stale buttons from old drafts
	if session.draft.CurrentDraft == nil || query.Message == nil || query.Message.MessageID != session.draft.CurrentDraft.SummaryMessageID {
		// Remove stale buttons
		if query.Message != nil {
			edit := tgbotapi.NewEditMessageReplyMarkup(
				query.Message.Chat.ID,
				query.Message.MessageID,
				tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
			)
			h.tg.Request(edit)
		}
		return
	}

	// Remove the inline keyboard from the summary message
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	switch action {
	case "confirm":
		h.HandleSendListing(ctx, session)
	case "cancel":
		session.deleteCurrentDraft(ctx)
		session.reset()
		session.replyAndRemoveCustomKeyboard(MsgOk)
	}
}

// HandleSendListing sends the listing using the adinput API.
// Publishing happens in a background goroutine so the user gets immediate feedback.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleSendListing(ctx context.Context, session *UserSession) {
	if session.draft.CurrentDraft == nil || len(session.photoCol.Photos) == 0 {
		session.reply(MsgNoListingToSend)
		return
	}

	if session.draft.CurrentDraft.State != AdFlowStateReadyToPublish {
		switch session.draft.CurrentDraft.State {
		case AdFlowStateAwaitingCategory:
			session.reply(MsgSelectCategoryFirst)
		case AdFlowStateAwaitingAttribute:
			session.reply(MsgFillAttributesFirst)
		case AdFlowStateAwaitingPrice:
			session.reply(MsgEnterPriceFirst)
		case AdFlowStateAwaitingShipping:
			session.reply(MsgSelectShippingFirst)
		case AdFlowStateAwaitingPostalCode:
			session.reply(MsgEnterPostalCodeFirst)
		default:
			session.reply(MsgListingNotReady)
		}
		return
	}

	// Copy data needed for background goroutine (can't access session from goroutine)
	draftID := session.draft.DraftID
	etag := session.draft.Etag
	draftCopy := *session.draft.CurrentDraft
	images := make([]UploadedImage, len(session.draft.CurrentDraft.Images))
	copy(images, session.draft.CurrentDraft.Images)
	client := session.draft.AdInputClient
	userId := session.userId

	// Get postal code from storage (need this before starting background task)
	var postalCode string
	if h.sessionStore != nil {
		var err error
		postalCode, err = h.sessionStore.GetPostalCode(userId)
		if err != nil {
			log.Error().Err(err).Msg("failed to get postal code")
		}
	}
	if postalCode == "" {
		// This shouldn't happen as we prompt for postal code before ReadyToPublish
		session.draft.CurrentDraft.State = AdFlowStateAwaitingPostalCode
		session.reply(MsgPostalCodePrompt)
		return
	}

	// Log final listing summary before publish
	LogState(userId, "Publishing listing:")
	LogState(userId, "  Title: %s", draftCopy.Title)
	LogState(userId, "  Price: %d€", draftCopy.Price)
	LogState(userId, "  Category: %d", draftCopy.CategoryID)
	LogState(userId, "  Shipping: %v", draftCopy.ShippingPossible)
	LogState(userId, "  Photos: %d", len(images))
	LogAPI(userId, "Starting background publish for draft %s", draftID)

	// Immediately respond and reset session
	session.replyAndRemoveCustomKeyboard(MsgPublishingSoon)
	session.reset()

	// Start background publishing with delays
	go h.publishInBackground(session, client, draftID, etag, &draftCopy, images, postalCode, userId)
}

// publishInBackground performs the publish API calls with delays.
// This runs in a separate goroutine and sends results back through the session worker channel.
func (h *ListingHandler) publishInBackground(
	session *UserSession,
	client tori.AdService,
	draftID string,
	etag string,
	draft *AdInputDraft,
	images []UploadedImage,
	postalCode string,
	userID int64,
) {
	// Use a fresh context since the original request context may be cancelled
	ctx := context.Background()

	result := &PublishResult{
		Title:   draft.Title,
		Price:   draft.Price,
		DraftID: draftID,
	}

	// Set category on draft
	LogAPI(userID, "Setting category on draft")
	newEtag, err := h.setCategoryOnDraft(ctx, client, draftID, etag, draft.CategoryID)
	if err != nil {
		LogError(userID, "Failed to set category: %v", err)
		result.Error = err
		session.Send(SessionMessage{
			Type:          "publish_complete",
			Ctx:           ctx,
			PublishResult: result,
		})
		return
	}
	etag = newEtag

	// Patch all fields to /items (required for review system)
	LogAPI(userID, "Patching item fields")
	fields := buildItemFields(
		draft.Title,
		draft.GetFullDescription(),
		draft.Price,
		draft.CollectedAttrs,
	)
	_, err = client.PatchItemFields(ctx, draftID, etag, fields)
	if err != nil {
		LogError(userID, "Failed to patch item fields: %v", err)
		result.Error = fmt.Errorf("failed to patch item fields: %w", err)
		session.Send(SessionMessage{
			Type:          "publish_complete",
			Ctx:           ctx,
			PublishResult: result,
		})
		return
	}

	// Get fresh ETag from adinput service
	LogAPI(userID, "Getting fresh ETag")
	adWithModel, err := client.GetAdWithModel(ctx, draftID)
	if err != nil {
		LogError(userID, "Failed to get fresh etag: %v", err)
		result.Error = fmt.Errorf("failed to get fresh etag: %w", err)
		session.Send(SessionMessage{
			Type:          "publish_complete",
			Ctx:           ctx,
			PublishResult: result,
		})
		return
	}
	etag = adWithModel.Ad.ETag

	// Update and publish
	LogAPI(userID, "Publishing ad")
	err = h.updateAndPublishAd(ctx, client, draftID, etag, draft, images, postalCode)
	if err != nil {
		LogError(userID, "Publish failed: %v", err)
		result.Error = err
	} else {
		LogAPI(userID, "Publish successful!")
	}

	// Send result back through session worker channel
	session.Send(SessionMessage{
		Type:          "publish_complete",
		Ctx:           ctx,
		PublishResult: result,
	})
}

// HandlePublishComplete handles the completion of a background publish operation.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePublishComplete(session *UserSession, result *PublishResult) {
	if result == nil {
		return
	}

	if result.Error != nil {
		log.Error().
			Err(result.Error).
			Str("title", result.Title).
			Str("draftID", result.DraftID).
			Msg("background publish failed")
		session.replyWithError(result.Error)
		return
	}

	log.Info().
		Str("title", result.Title).
		Int("price", result.Price).
		Msg("listing published in background")
}

// HandleTitleDescriptionReply handles replies to title/description messages for editing.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleTitleDescriptionReply(session *UserSession, message *tgbotapi.Message) bool {
	draft := session.draft.CurrentDraft
	if draft == nil {
		return false
	}

	replyToID := message.ReplyToMessage.MessageID
	if draft.TitleMessageID == replyToID {
		// Touch draft activity on title edit
		h.touchDraftActivity(session)
		draft.Title = message.Text
		// Edit the original message to show updated title
		editMsg := tgbotapi.NewEditMessageText(session.userId, replyToID, fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(draft.Title)))
		editMsg.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(editMsg)
		session.reply(fmt.Sprintf("✅ Otsikko päivitetty: %s", escapeMarkdown(message.Text)))
		return true
	}
	if draft.DescriptionMessageID == replyToID {
		// Touch draft activity on description edit
		h.touchDraftActivity(session)
		draft.Description = message.Text
		// Edit the original message to show updated description (include template if present)
		editMsg := tgbotapi.NewEditMessageText(session.userId, replyToID, fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(draft.GetFullDescription())))
		editMsg.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(editMsg)
		session.reply("✅ Kuvaus päivitetty")
		return true
	}

	return false
}

// --- Helper methods ---

// CategorySelectionResult contains the result of category auto-selection.
type CategorySelectionResult struct {
	SelectedID         int                       // Selected category ID, 0 if none selected
	UpdatedPredictions []tori.CategoryPrediction // Updated predictions (from fallback search)
	UsedFallback       bool                      // True if fallback search was used
}

// tryAutoSelectCategory attempts to auto-select category using LLM.
//
// # Why fallback exists
//
// Tori's category prediction API (based on uploaded images) can return completely
// wrong categories. For example, a "Salli Twin satulatuoli" (saddle chair) was
// predicted as "Musical instruments" instead of office furniture. When predictions
// are this wrong, users get a poor experience choosing from irrelevant options.
//
// # Hierarchical tree-climbing fallback
//
// When the LLM rejects all Tori predictions (returns category_id 0), we use a
// hierarchical tree-climbing approach that achieved 92% accuracy in testing:
//  1. Build a category tree from the embedded 601 categories
//  2. Start at root level (~11 top-level categories)
//  3. LLM selects from current level categories
//  4. Navigate to selected category's children
//  5. Repeat until reaching a leaf node
//
// This approach is more accurate than keyword-based search because it leverages
// the LLM's understanding at each level with focused choices.
func (h *ListingHandler) tryAutoSelectCategory(ctx context.Context, title, description string, predictions []tori.CategoryPrediction) CategorySelectionResult {
	result := CategorySelectionResult{
		UpdatedPredictions: predictions,
	}

	// Check if the analyzer supports category selection
	gemini := llm.GetGeminiAnalyzer(h.visionAnalyzer)
	if gemini == nil {
		return result
	}

	// First attempt: select from Tori's predictions
	categoryID, err := gemini.SelectCategory(ctx, title, description, predictions)
	if err != nil {
		log.Warn().Err(err).Msg("LLM category selection failed, falling back to manual")
		return result
	}

	// If LLM selected a valid category, return it
	if categoryID > 0 {
		result.SelectedID = categoryID
		return result
	}

	// LLM rejected all predictions (returned 0) - trigger hierarchical fallback
	log.Info().
		Str("title", title).
		Int("predictionCount", len(predictions)).
		Msg("LLM rejected all category predictions, attempting hierarchical tree fallback")

	// Use hierarchical tree-climbing to find the correct category
	selectedCategory := h.selectCategoryHierarchical(ctx, gemini, title, description)
	if selectedCategory != nil {
		result.SelectedID = selectedCategory.ID
		result.UpdatedPredictions = []tori.CategoryPrediction{*selectedCategory}
		result.UsedFallback = true
		log.Info().
			Int("categoryId", selectedCategory.ID).
			Str("label", selectedCategory.Label).
			Msg("hierarchical category selection succeeded")
	}

	return result
}

// selectCategoryHierarchical traverses the category tree level-by-level using LLM selection.
// Returns the final leaf category or nil if selection fails at any level.
func (h *ListingHandler) selectCategoryHierarchical(ctx context.Context, gemini *llm.GeminiAnalyzer, title, description string) *tori.CategoryPrediction {
	if h.categoryTree == nil {
		log.Warn().Msg("category tree not initialized")
		return nil
	}

	var pathContext string
	var currentID int

	// Start at root level
	currentNodes := h.categoryTree.GetRoots()
	if len(currentNodes) == 0 {
		log.Warn().Msg("no root categories in tree")
		return nil
	}

	// Maximum depth to prevent infinite loops
	const maxDepth = 5

	for depth := 0; depth < maxDepth; depth++ {
		// Convert nodes to CategoryPrediction for LLM
		categories := tori.NodesToSimpleCategories(currentNodes)

		// Ask LLM to select from current level
		selectedID, err := gemini.SelectCategoryHierarchical(ctx, title, description, categories, pathContext)
		if err != nil {
			log.Warn().Err(err).Int("depth", depth).Msg("hierarchical selection failed at level")
			return nil
		}

		// LLM returned 0 - no suitable category at this level
		if selectedID == 0 {
			log.Info().Int("depth", depth).Str("pathContext", pathContext).Msg("LLM rejected all categories at level")
			return nil
		}

		currentID = selectedID
		selectedNode := h.categoryTree.GetNode(selectedID)
		if selectedNode == nil {
			log.Warn().Int("selectedID", selectedID).Msg("selected category not found in tree")
			return nil
		}

		// Update path context for next level
		if pathContext == "" {
			pathContext = selectedNode.Label
		} else {
			pathContext = pathContext + " > " + selectedNode.Label
		}

		log.Info().
			Int("depth", depth).
			Int("categoryId", selectedID).
			Str("label", selectedNode.Label).
			Str("pathContext", pathContext).
			Msg("selected category at level")

		// Check if this is a leaf node - we're done
		if h.categoryTree.IsLeaf(selectedID) {
			pred := h.categoryTree.NodeToCategoryPrediction(selectedNode)
			return &pred
		}

		// Navigate to children for next iteration
		currentNodes = h.categoryTree.GetChildren(selectedID)
	}

	// Reached max depth - return the last selected category
	if currentID > 0 {
		node := h.categoryTree.GetNode(currentID)
		if node != nil {
			pred := h.categoryTree.NodeToCategoryPrediction(node)
			return &pred
		}
	}

	return nil
}

// Attributes that should never be auto-selected (require user input)
var manualOnlyAttributes = map[string]bool{
	"condition": true, // Condition is subjective, user must choose
}

// tryAutoSelectAttributes attempts to auto-select attributes using LLM.
// Returns the list of attributes that still need manual selection.
// Caller must hold session.mu.Lock().
func (h *ListingHandler) tryAutoSelectAttributes(ctx context.Context, session *UserSession, attrs []tori.Attribute) []tori.Attribute {
	gemini := llm.GetGeminiAnalyzer(h.visionAnalyzer)
	if gemini == nil {
		return attrs
	}

	// Filter out attributes that must be selected manually
	var autoSelectableAttrs []tori.Attribute
	var manualAttrs []tori.Attribute
	for _, attr := range attrs {
		if manualOnlyAttributes[attr.Name] {
			manualAttrs = append(manualAttrs, attr)
		} else {
			autoSelectableAttrs = append(autoSelectableAttrs, attr)
		}
	}

	if len(autoSelectableAttrs) == 0 {
		return attrs
	}

	title := session.draft.CurrentDraft.Title
	description := session.draft.CurrentDraft.Description

	selectedMap, err := gemini.SelectAttributes(ctx, title, description, autoSelectableAttrs)
	if err != nil {
		log.Warn().Err(err).Msg("LLM attribute selection failed, falling back to manual")
		LogError(session.userId, "LLM attribute selection failed: %v", err)
		return attrs
	}

	if len(selectedMap) == 0 {
		LogLLM(session.userId, "LLM attribute selection: no auto-selections made")
		return attrs
	}

	var remainingAttrs []tori.Attribute
	var autoSelectedInfo []string

	for _, attr := range autoSelectableAttrs {
		selectedID, found := selectedMap[attr.Name]

		// Validate: selected ID must exist in this attribute's options
		validOption := false
		var selectedLabel string
		if found {
			for _, opt := range attr.Options {
				if opt.ID == selectedID {
					validOption = true
					selectedLabel = opt.Label
					break
				}
			}
		}

		if validOption {
			// Auto-select this attribute
			session.draft.CurrentDraft.CollectedAttrs[attr.Name] = strconv.Itoa(selectedID)
			autoSelectedInfo = append(autoSelectedInfo, fmt.Sprintf("%s: *%s*", attr.Label, selectedLabel))
			log.Info().Str("attr", attr.Name).Str("label", selectedLabel).Int("optionId", selectedID).Msg("attribute auto-selected")
			LogLLM(session.userId, "Auto-selected %s: %s", attr.Label, selectedLabel)
		} else {
			// Keep for manual input
			remainingAttrs = append(remainingAttrs, attr)
		}
	}

	// Add manual-only attributes to remaining
	remainingAttrs = append(remainingAttrs, manualAttrs...)

	// Inform user what was auto-selected
	if len(autoSelectedInfo) > 0 {
		session.reply(fmt.Sprintf("%s", strings.Join(autoSelectedInfo, "\n")))
		LogBot(session.userId, "Showing auto-selected attributes")
	}

	return remainingAttrs
}

// tryRestorePreservedAttributes attempts to restore preserved attribute values (like condition)
// if the new category has compatible options. Returns the list of attributes that still need manual selection.
func (h *ListingHandler) tryRestorePreservedAttributes(session *UserSession, attrs []tori.Attribute, preserved *PreservedValues) []tori.Attribute {
	var remainingAttrs []tori.Attribute

	for _, attr := range attrs {
		preservedValue, hasPreserved := preserved.CollectedAttrs[attr.Name]
		if !hasPreserved {
			remainingAttrs = append(remainingAttrs, attr)
			continue
		}

		// Check if the preserved value ID exists in the new category's options
		preservedID, err := strconv.Atoi(preservedValue)
		if err != nil {
			remainingAttrs = append(remainingAttrs, attr)
			continue
		}

		var foundOption *tori.AttributeOption
		for _, opt := range attr.Options {
			if opt.ID == preservedID {
				foundOption = &opt
				break
			}
		}

		if foundOption != nil {
			// Restore the preserved value
			session.draft.CurrentDraft.CollectedAttrs[attr.Name] = preservedValue
			log.Info().Str("attr", attr.Name).Str("label", foundOption.Label).Int("optionId", preservedID).Msg("attribute restored from previous selection")
		} else {
			// Value not compatible, need manual selection
			remainingAttrs = append(remainingAttrs, attr)
		}
	}

	return remainingAttrs
}

// proceedAfterAttributes handles the flow after all attributes are collected.
// If preserved values exist for price/shipping, it skips those prompts.
func (h *ListingHandler) proceedAfterAttributes(ctx context.Context, session *UserSession) {
	preserved := session.draft.CurrentDraft.PreservedValues

	// Only restore if we have a set price (>0) or it is a giveaway.
	// Default state (Price=0, TradeType="1") should trigger a prompt.
	shouldRestore := preserved != nil && (preserved.Price > 0 || preserved.TradeType == TradeTypeGive)

	if !shouldRestore {
		h.promptForPrice(ctx, session)
		return
	}

	// Restore price and trade type
	session.draft.CurrentDraft.Price = preserved.Price
	session.draft.CurrentDraft.TradeType = preserved.TradeType

	// If shipping was also set, restore it and go to summary
	if preserved.ShippingSet {
		session.draft.CurrentDraft.ShippingPossible = preserved.ShippingPossible

		// Clear preserved values as we're done with them
		session.draft.CurrentDraft.PreservedValues = nil

		// Apply template now that shipping is known
		h.applyUserTemplate(session)

		// Check postal code before showing summary (matching HandleShippingSelection logic)
		if h.sessionStore != nil {
			postalCode, _ := h.sessionStore.GetPostalCode(session.userId)
			if postalCode == "" {
				session.draft.CurrentDraft.State = AdFlowStateAwaitingPostalCode
				session.reply(MsgPostalCodePrompt)
				return
			}
		}

		// Go directly to summary
		h.showAdSummary(session)
		return
	}

	// Clear preserved values as price is restored, but shipping still needs to be set
	session.draft.CurrentDraft.PreservedValues = nil

	// Need to ask for shipping
	session.draft.CurrentDraft.State = AdFlowStateAwaitingShipping
	msg := tgbotapi.NewMessage(session.userId, MsgShippingQuestion)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(BtnYes, "shipping:yes"),
			tgbotapi.NewInlineKeyboardButtonData(BtnNo, "shipping:no"),
		),
	)
	session.replyWithMessage(msg)
}

// applyUserTemplate expands and stores the user's template, updating the description message.
// No-op if there is no active draft or template storage.
func (h *ListingHandler) applyUserTemplate(session *UserSession) {
	if h.sessionStore == nil || session.draft.CurrentDraft == nil {
		return
	}

	tmpl, err := h.sessionStore.GetTemplate(session.userId)
	if err != nil {
		log.Error().Err(err).Msg("failed to load template")
		return
	}

	var expanded string
	if tmpl != nil {
		expanded = expandTemplate(tmpl.Content, TemplateData{
			Shipping: session.draft.CurrentDraft.ShippingPossible,
			Giveaway: session.draft.CurrentDraft.TradeType == TradeTypeGive,
			Price:    session.draft.CurrentDraft.Price,
		})
	}

	// Always update template state (may be empty if no content for this shipping option)
	session.draft.CurrentDraft.TemplateContent = strings.TrimSpace(expanded)

	// Update the description message in chat with combined content
	if session.draft.CurrentDraft.DescriptionMessageID != 0 {
		editMsg := tgbotapi.NewEditMessageText(
			session.userId,
			session.draft.CurrentDraft.DescriptionMessageID,
			fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(session.draft.CurrentDraft.GetFullDescription())),
		)
		editMsg.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(editMsg)
	}
}

// promptForAttribute shows a keyboard to select an attribute value.
func (h *ListingHandler) promptForAttribute(session *UserSession, attr tori.Attribute) {
	msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf(MsgSelectAttribute, strings.ToLower(attr.Label)))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = makeAttributeKeyboard(attr)
	session.replyWithMessage(msg)
}

// promptForPrice fetches price recommendations and prompts the user.
// Called from session worker - no locking needed.
func (h *ListingHandler) promptForPrice(ctx context.Context, session *UserSession) {
	session.draft.CurrentDraft.State = AdFlowStateAwaitingPrice
	LogState(session.userId, "Awaiting price input")
	title := session.draft.CurrentDraft.Title
	description := session.draft.CurrentDraft.Description

	// Generate optimized search query using LLM
	searchQuery := title // fallback to title
	if gemini := llm.GetGeminiAnalyzer(h.visionAnalyzer); gemini != nil {
		generatedQuery, err := gemini.GeneratePriceSearchQuery(ctx, title, description)
		if err != nil {
			log.Warn().Err(err).Str("title", title).Msg("failed to generate price search query, using title")
		} else if generatedQuery != "" {
			searchQuery = generatedQuery
			LogLLM(session.userId, "Generated price search query: %s", searchQuery)
		}
	}

	// Build category taxonomy for filtering
	var categoryTaxonomy tori.CategoryTaxonomy
	if cat := tori.FindCategoryByID(session.draft.CurrentDraft.CategoryPredictions, session.draft.CurrentDraft.CategoryID); cat != nil {
		categoryTaxonomy = tori.GetCategoryTaxonomy(*cat)
	}

	log.Debug().
		Str("query", searchQuery).
		Str("originalTitle", title).
		Str("categoryParam", categoryTaxonomy.ParamName).
		Str("categoryValue", categoryTaxonomy.Value).
		Int64("userId", session.userId).
		Msg("searching for similar prices")

	// Search for similar items (network I/O)
	results, err := h.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query:            searchQuery,
		CategoryTaxonomy: categoryTaxonomy,
		Rows:             20,
	})

	if err != nil {
		log.Warn().Err(err).Str("query", searchQuery).Msg("price search failed")
	}

	resultCount := 0
	if results != nil {
		resultCount = len(results.Docs)
	}
	log.Debug().
		Int("count", resultCount).
		Int64("userId", session.userId).
		Msg("price search returned results")

	// Collect prices from results
	var prices []int
	if results != nil {
		for _, doc := range results.Docs {
			if doc.Price != nil && doc.Price.Amount > 0 {
				prices = append(prices, doc.Price.Amount)
			}
		}
	}

	log.Debug().
		Ints("prices", prices).
		Int64("userId", session.userId).
		Msg("found prices from search")

	// Calculate recommendation
	var recommendationMsg string
	var recommendedPrice int

	if len(prices) < 3 {
		log.Debug().
			Int("priceCount", len(prices)).
			Int64("userId", session.userId).
			Msg("insufficient prices for estimation (need at least 3)")
	} else {
		sort.Ints(prices)
		minPrice := prices[0]
		maxPrice := prices[len(prices)-1]

		// Calculate median
		medianPrice := prices[len(prices)/2]
		if len(prices)%2 == 0 {
			medianPrice = (prices[len(prices)/2-1] + prices[len(prices)/2]) / 2
		}

		recommendedPrice = medianPrice
		recommendationMsg = fmt.Sprintf("\n\n💡 *Hinta-arvio* (%d ilmoitusta):\nKeskihinta: *%d€* (vaihteluväli %d–%d€)",
			len(prices), medianPrice, minPrice, maxPrice)
		LogInternal(session.userId, "Price search: %d results, median=%d€ (range %d-%d€)", len(prices), medianPrice, minPrice, maxPrice)

		log.Info().
			Str("title", title).
			Int("median", medianPrice).
			Int("min", minPrice).
			Int("max", maxPrice).
			Int("count", len(prices)).
			Msg("price recommendation")
	}

	// Check if state is still valid (user may have cancelled during search)
	if session.draft.CurrentDraft == nil || session.draft.CurrentDraft.State != AdFlowStateAwaitingPrice {
		return
	}

	msgText := fmt.Sprintf(MsgEnterPriceWithEstimate, recommendationMsg)
	msg := tgbotapi.NewMessage(session.userId, msgText)
	msg.ParseMode = tgbotapi.ModeMarkdown

	// Build keyboard with price suggestion (if available) and giveaway button
	var rows [][]tgbotapi.KeyboardButton
	if recommendedPrice > 0 {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("%d€", recommendedPrice)),
			tgbotapi.NewKeyboardButton("🎁 "+BtnBulkGiveaway),
		))
	} else {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🎁 "+BtnBulkGiveaway),
		))
	}

	keyboard := tgbotapi.NewReplyKeyboard(rows...)
	keyboard.OneTimeKeyboard = true
	keyboard.ResizeKeyboard = true
	msg.ReplyMarkup = keyboard

	session.replyWithMessage(msg)
}

// showAdSummary displays the final ad summary before publishing.
// If a summary message already exists, it edits that message instead of sending a new one.
func (h *ListingHandler) showAdSummary(session *UserSession) {
	session.draft.CurrentDraft.State = AdFlowStateReadyToPublish

	shippingText := BtnNo
	if session.draft.CurrentDraft.ShippingPossible {
		if session.draft.CurrentDraft.SavedShippingAddress != nil && session.draft.CurrentDraft.PackageSize != "" {
			// Show ToriDiili with package size
			sizeLabel := map[string]string{
				PackageSizeSmall:  "S",
				PackageSizeMedium: "M",
				PackageSizeLarge:  "L",
			}[session.draft.CurrentDraft.PackageSize]
			shippingText = fmt.Sprintf("ToriDiili (%s)", sizeLabel)
		} else {
			shippingText = BtnYes
		}
	}

	// Show "Annetaan" for giveaways, price for sales
	var priceText string
	if session.draft.CurrentDraft.TradeType == TradeTypeGive {
		priceText = "🎁 " + BtnBulkGiveaway
	} else {
		priceText = fmt.Sprintf("%d€", session.draft.CurrentDraft.Price)
	}

	summaryText := fmt.Sprintf(`*Ilmoitus valmis:*
📦 *Otsikko:* %s
📝 *Kuvaus:* %s
💰 *Hinta:* %s
🚚 *Postitus:* %s
📷 *Kuvia:* %d`,
		escapeMarkdown(session.draft.CurrentDraft.Title),
		escapeMarkdown(session.draft.CurrentDraft.GetFullDescription()),
		priceText,
		shippingText,
		len(session.photoCol.Photos),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(BtnPublish, "publish:confirm"),
			tgbotapi.NewInlineKeyboardButtonData(BtnCancel, "publish:cancel"),
		),
	)

	// If a summary message already exists, edit it instead of sending a new one
	if session.draft.CurrentDraft.SummaryMessageID != 0 {
		editMsg := tgbotapi.NewEditMessageTextAndMarkup(
			session.userId,
			session.draft.CurrentDraft.SummaryMessageID,
			summaryText,
			keyboard,
		)
		editMsg.ParseMode = tgbotapi.ModeMarkdown

		_, err := h.tg.Request(editMsg)
		if err != nil {
			// If "message is not modified", it's safe to ignore (content hasn't changed)
			if strings.Contains(err.Error(), "message is not modified") {
				return
			}

			// For other errors (e.g., "message to edit not found"), fall back to sending a new message
			log.Warn().Err(err).Int("msgID", session.draft.CurrentDraft.SummaryMessageID).Msg("failed to edit summary, sending new one")

			newMsg := tgbotapi.NewMessage(session.userId, summaryText)
			newMsg.ParseMode = tgbotapi.ModeMarkdown
			newMsg.ReplyMarkup = keyboard
			sentMsg, _ := h.tg.Send(newMsg)
			session.draft.CurrentDraft.SummaryMessageID = sentMsg.MessageID
		}
	} else {
		newMsg := tgbotapi.NewMessage(session.userId, summaryText)
		newMsg.ParseMode = tgbotapi.ModeMarkdown
		newMsg.ReplyMarkup = keyboard
		sentMsg, _ := h.tg.Send(newMsg)
		session.draft.CurrentDraft.SummaryMessageID = sentMsg.MessageID
	}
}

// setCategoryOnDraft sets the category on the draft and returns new ETag.
func (h *ListingHandler) setCategoryOnDraft(ctx context.Context, client tori.AdService, draftID, etag string, categoryID int) (string, error) {
	patchResp, err := client.PatchItem(ctx, draftID, etag, map[string]any{
		"category": categoryID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to set category: %w", err)
	}

	return patchResp.ETag, nil
}

// updateAndPublishAd updates the ad with all fields and publishes it.
func (h *ListingHandler) updateAndPublishAd(
	ctx context.Context,
	client tori.AdService,
	draftID string,
	etag string,
	draft *AdInputDraft,
	images []UploadedImage,
	postalCode string,
) error {
	payload := buildFinalPayload(draft, images, postalCode)

	// Update the ad and capture new ETag
	updateResp, err := client.UpdateAd(ctx, draftID, etag, payload)
	if err != nil {
		return fmt.Errorf("failed to update ad: %w", err)
	}
	etag = updateResp.ETag

	// Build delivery options
	// Meetup and shipping are mutually exclusive
	shippingEnabled := draft.ShippingPossible && draft.SavedShippingAddress != nil
	deliveryOpts := tori.DeliveryOptions{
		Client:             "ANDROID",
		Meetup:             !shippingEnabled,
		SellerPaysShipping: false,
		Shipping:           shippingEnabled,
		BuyNow:             shippingEnabled, // BuyNow required for ToriDiili
	}

	// Add shipping info if Tori Diili shipping is enabled
	if draft.ShippingPossible && draft.SavedShippingAddress != nil {
		// Determine products based on package size
		products := []string{ProductMatkahuoltoShop}
		if draft.PackageSize == PackageSizeSmall {
			products = append(products, ProductKotipaketti)
		} else {
			products = append(products, ProductPostipaketti)
		}

		// Get phone number (prefer mobilePhone, fall back to phoneNumber)
		phone := draft.SavedShippingAddress.MobilePhone
		if phone == "" {
			phone = draft.SavedShippingAddress.PhoneNumber
		}

		deliveryOpts.ShippingInfo = &tori.ShippingInfo{
			Name:        draft.SavedShippingAddress.Name,
			Address:     draft.SavedShippingAddress.Address,
			City:        draft.SavedShippingAddress.City,
			PostalCode:  draft.SavedShippingAddress.PostalCode, // Use sender's address postal code, not ad location
			PhoneNumber: phone,
			Products:    products,
			Size:        draft.PackageSize,
			SaveAddress: true,
			// Zero defaults for optional fields
			DeliveryPointID: 0,
			FlatNo:          0,
			FloorNo:         0,
			FloorType:       "",
			HouseType:       "",
			StreetName:      "",
			StreetNo:        "",
		}

		log.Info().
			Str("size", draft.PackageSize).
			Strs("products", products).
			Str("name", draft.SavedShippingAddress.Name).
			Msg("publishing with Tori Diili shipping")
	}

	err = client.SetDeliveryOptions(ctx, draftID, deliveryOpts)
	if err != nil {
		return fmt.Errorf("failed to set delivery options: %w", err)
	}

	// Get product context before publishing
	if err := client.GetProductContext(ctx, draftID, etag); err != nil {
		log.Warn().Err(err).Msg("failed to get product context")
	}

	// Publish
	orderResp, err := client.PublishAd(ctx, draftID)
	if err != nil {
		return fmt.Errorf("failed to publish ad: %w", err)
	}

	// Get order confirmation
	if err := client.GetOrderConfirmation(ctx, orderResp.OrderID, draftID); err != nil {
		log.Warn().Err(err).Msg("failed to get order confirmation")
	}

	// Track ad confirmation
	if err := client.TrackAdConfirmation(ctx, draftID, orderResp.OrderID); err != nil {
		log.Warn().Err(err).Msg("failed to track ad confirmation")
	}

	return nil
}

// HandleEditCommand handles natural language edit commands.
// Returns true if the message was processed as an edit command.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleEditCommand(ctx context.Context, session *UserSession, message string) bool {
	if h.editIntentParser == nil {
		log.Debug().Msg("edit intent parser not configured")
		return false
	}

	if session.draft.CurrentDraft == nil {
		log.Debug().Msg("no current draft for edit command")
		return false
	}

	LogUser(session.userId, "Text input (possible edit): %s", message)

	// Build draft info with available attributes for the LLM
	draftInfo := &llm.CurrentDraftInfo{
		Title:       session.draft.CurrentDraft.Title,
		Description: session.draft.CurrentDraft.Description,
		Price:       session.draft.CurrentDraft.Price,
	}

	// Add available attributes from the category
	if session.draft.AdAttributes != nil {
		for _, attr := range session.draft.AdAttributes.Attributes {
			draftInfo.Attributes = append(draftInfo.Attributes, llm.AttributeInfo{
				Label: attr.Label,
				Name:  attr.Name,
			})
		}
	}

	log.Debug().
		Str("message", message).
		Str("title", draftInfo.Title).
		Int("price", draftInfo.Price).
		Int("attrCount", len(draftInfo.Attributes)).
		Msg("parsing edit intent")

	// Parse edit intent (network I/O)
	intent, err := h.editIntentParser.ParseEditIntent(ctx, message, draftInfo)
	if err != nil {
		log.Warn().Err(err).Str("message", message).Msg("failed to parse edit intent")
		LogError(session.userId, "Failed to parse edit intent: %v", err)
		// Show error to user and return true to prevent falling through to "send image" prompt
		session.reply(MsgEditTempError)
		return true
	}

	// Check if any changes were requested
	if intent.NewPrice == nil && intent.NewTitle == nil && intent.NewDescription == nil && len(intent.ResetAttributes) == 0 {
		log.Debug().Str("message", message).Msg("no edit intent detected in message")
		LogLLM(session.userId, "No edit intent detected in message")
		return false
	}

	// Check draft still exists after network I/O
	if session.draft.CurrentDraft == nil {
		return false
	}

	var changes []string

	LogLLM(session.userId, "Edit intent parsed: price=%v, title=%v, desc=%v",
		intent.NewPrice != nil, intent.NewTitle != nil, intent.NewDescription != nil)

	// Apply price change
	if intent.NewPrice != nil {
		oldPrice := session.draft.CurrentDraft.Price
		session.draft.CurrentDraft.Price = *intent.NewPrice
		changes = append(changes, fmt.Sprintf("Hinta: %d€ → %d€", oldPrice, *intent.NewPrice))
		log.Info().Int("oldPrice", oldPrice).Int("newPrice", *intent.NewPrice).Msg("price updated via edit command")
		LogState(session.userId, "Price changed: %d€ → %d€", oldPrice, *intent.NewPrice)
	}

	// Apply title change
	if intent.NewTitle != nil {
		oldTitle := session.draft.CurrentDraft.Title
		session.draft.CurrentDraft.Title = *intent.NewTitle
		changes = append(changes, fmt.Sprintf("Otsikko: %s", *intent.NewTitle))

		// Update title message in chat
		if session.draft.CurrentDraft.TitleMessageID != 0 {
			editMsg := tgbotapi.NewEditMessageText(
				session.userId,
				session.draft.CurrentDraft.TitleMessageID,
				fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(*intent.NewTitle)),
			)
			editMsg.ParseMode = tgbotapi.ModeMarkdown
			h.tg.Request(editMsg)
		}
		log.Info().Str("oldTitle", oldTitle).Str("newTitle", *intent.NewTitle).Msg("title updated via edit command")
		LogState(session.userId, "Title changed: %s → %s", oldTitle, *intent.NewTitle)
	}

	// Apply description change
	if intent.NewDescription != nil {
		session.draft.CurrentDraft.Description = *intent.NewDescription
		changes = append(changes, MsgDescriptionChange)

		// Update description message in chat (include template if present)
		if session.draft.CurrentDraft.DescriptionMessageID != 0 {
			editMsg := tgbotapi.NewEditMessageText(
				session.userId,
				session.draft.CurrentDraft.DescriptionMessageID,
				fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(session.draft.CurrentDraft.GetFullDescription())),
			)
			editMsg.ParseMode = tgbotapi.ModeMarkdown
			h.tg.Request(editMsg)
		}
		log.Info().Str("newDescription", *intent.NewDescription).Msg("description updated via edit command")
	}

	// Handle attribute resets - trigger re-selection UI
	if len(intent.ResetAttributes) > 0 {
		if h.handleAttributeReset(ctx, session, intent.ResetAttributes) {
			// Attribute reset started, UI will be shown
			return true
		}
		// If no valid attributes found to reset, continue with other changes
	}

	// Send confirmation message
	if len(changes) == 1 {
		session.reply(MsgChangesConfirm, changes[0])
	} else if len(changes) > 1 {
		session.reply(MsgMultipleChanges, strings.Join(changes, "\n- "))
	}

	// Show updated summary if listing is ready to publish
	if len(changes) > 0 && session.draft.CurrentDraft.State == AdFlowStateReadyToPublish {
		h.showAdSummary(session)
	}

	return true
}

// handleAttributeReset processes attribute reset requests from natural language commands.
// It finds the requested attributes, preserves current state, and triggers re-selection UI.
// Returns true if attribute re-selection was started.
func (h *ListingHandler) handleAttributeReset(ctx context.Context, session *UserSession, attrNames []string) bool {
	if session.draft.AdAttributes == nil {
		log.Debug().Msg("no attributes available for reset")
		return false
	}

	// Find matching attributes from the available attributes
	var attrsToReset []tori.Attribute
	for _, targetName := range attrNames {
		targetLower := strings.ToLower(targetName)
		for _, attr := range session.draft.AdAttributes.Attributes {
			if strings.ToLower(attr.Name) == targetLower {
				attrsToReset = append(attrsToReset, attr)
				log.Info().Str("attr", attr.Name).Str("label", attr.Label).Msg("attribute marked for reset")
				break
			}
		}
	}

	if len(attrsToReset) == 0 {
		log.Debug().Strs("requested", attrNames).Msg("no matching attributes found for reset")
		return false
	}

	// Create PreservedValues with current state, excluding the attributes to reset
	preserved := &PreservedValues{
		Price:            session.draft.CurrentDraft.Price,
		TradeType:        session.draft.CurrentDraft.TradeType,
		ShippingPossible: session.draft.CurrentDraft.ShippingPossible,
		ShippingSet:      session.draft.CurrentDraft.State >= AdFlowStateAwaitingShipping,
		CollectedAttrs:   make(map[string]string),
	}

	// Copy all collected attributes except the ones being reset
	resetSet := make(map[string]bool)
	for _, attr := range attrsToReset {
		resetSet[attr.Name] = true
	}
	for k, v := range session.draft.CurrentDraft.CollectedAttrs {
		if !resetSet[k] {
			preserved.CollectedAttrs[k] = v
		}
	}

	// Store preserved values
	session.draft.CurrentDraft.PreservedValues = preserved

	// Clear the attributes being reset from CollectedAttrs
	for _, attr := range attrsToReset {
		delete(session.draft.CurrentDraft.CollectedAttrs, attr.Name)
	}

	// Set up attribute selection state
	session.draft.CurrentDraft.RequiredAttrs = attrsToReset
	session.draft.CurrentDraft.CurrentAttrIndex = 0
	session.draft.CurrentDraft.State = AdFlowStateAwaitingAttribute

	// Inform user and show the first attribute keyboard
	firstAttr := attrsToReset[0]
	session.reply(MsgReselectAttribute, strings.ToLower(firstAttr.Label))
	h.promptForAttribute(session, firstAttr)

	return true
}

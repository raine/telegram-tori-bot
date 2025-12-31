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
}

// NewListingHandler creates a new listing handler.
func NewListingHandler(tg BotAPI, visionAnalyzer llm.Analyzer, editIntentParser llm.EditIntentParser, sessionStore storage.SessionStore) *ListingHandler {
	categoryService := tori.NewCategoryService()
	return &ListingHandler{
		tg:               tg,
		visionAnalyzer:   visionAnalyzer,
		editIntentParser: editIntentParser,
		sessionStore:     sessionStore,
		searchClient:     tori.NewSearchClient(),
		categoryService:  categoryService,
		categoryTree:     categoryService.Tree,
	}
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
		session.replyAndRemoveCustomKeyboard(okText)
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
	if session.draftID != "" {
		h.processSinglePhoto(ctx, session, photo)
		return
	}

	// If already creating a draft, reject this photo
	if session.isCreatingDraft {
		session.reply("Odota hetki, luodaan ilmoitusta...")
		return
	}

	// Initialize or update album buffer
	if session.albumBuffer == nil || session.albumBuffer.MediaGroupID != mediaGroupID {
		// If there's an existing buffer with photos from a different album, flush it first
		if session.albumBuffer != nil && len(session.albumBuffer.Photos) > 0 {
			if session.albumBuffer.Timer != nil {
				session.albumBuffer.Timer.Stop()
			}
			// Process the previous album immediately (this may set isCreatingDraft=true,
			// causing the current photo to be rejected, which is safer than dropping data)
			h.ProcessAlbumTimeout(ctx, session, session.albumBuffer)
		}
		session.albumBuffer = &AlbumBuffer{
			MediaGroupID:  mediaGroupID,
			Photos:        []AlbumPhoto{},
			FirstReceived: time.Now(),
		}
	}

	// Add photo to buffer (respect max limit)
	if len(session.albumBuffer.Photos) < maxAlbumPhotos {
		session.albumBuffer.Photos = append(session.albumBuffer.Photos, AlbumPhoto{
			FileID: photo.FileID,
			Width:  photo.Width,
			Height: photo.Height,
		})
	}

	// Reset or start timer - dispatch through worker channel when done
	if session.albumBuffer.Timer != nil {
		session.albumBuffer.Timer.Stop()
	}

	// Capture buffer reference for timer closure
	albumBuffer := session.albumBuffer
	session.albumBuffer.Timer = time.AfterFunc(albumBufferTimeout, func() {
		// Dispatch album processing through the worker channel
		// Use context.Background() since the original request context may be cancelled by now
		session.Send(SessionMessage{
			Type:        "album_timeout",
			Ctx:         context.Background(),
			AlbumBuffer: albumBuffer,
		})
	})
}

// ProcessAlbumTimeout handles the album timeout message from the worker channel.
// Called from session worker - no locking needed.
func (h *ListingHandler) ProcessAlbumTimeout(ctx context.Context, session *UserSession, albumBuffer *AlbumBuffer) {
	// Verify this is still the active album buffer (wasn't replaced or cleared)
	if session.albumBuffer != albumBuffer {
		return
	}

	// Clear the album buffer
	photos := albumBuffer.Photos
	session.albumBuffer = nil

	if len(photos) == 0 {
		return
	}

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
	if session.draftID == "" || session.currentDraft == nil {
		return
	}

	// Ignore stale timer events - if the timer was reset (user activity),
	// the ExpirationTimer will point to a different timer instance
	if session.currentDraft.ExpirationTimer != timer {
		log.Debug().
			Int64("userId", session.userId).
			Msg("ignoring stale draft expiration message")
		return
	}

	log.Info().
		Int64("userId", session.userId).
		Str("draftID", session.draftID).
		Msg("draft expired due to inactivity")

	// Delete draft from Tori API
	session.deleteCurrentDraft(ctx)

	// Reset session state
	session.reset()

	// Notify user
	session.replyAndRemoveCustomKeyboard(draftExpiredText)
}

// startDraftExpirationTimer starts or resets the draft expiration timer.
// When the timer fires, it dispatches a draft_expired message through the session worker.
// Called from session worker - no locking needed.
func (h *ListingHandler) startDraftExpirationTimer(session *UserSession) {
	if session.currentDraft == nil {
		return
	}

	// Stop existing timer if any
	if session.currentDraft.ExpirationTimer != nil {
		session.currentDraft.ExpirationTimer.Stop()
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
	session.currentDraft.ExpirationTimer = timer
}

// touchDraftActivity resets the draft expiration timer due to user activity.
// Called from session worker - no locking needed.
func (h *ListingHandler) touchDraftActivity(session *UserSession) {
	if session.currentDraft == nil || session.draftID == "" {
		return
	}
	h.startDraftExpirationTimer(session)
}

// processSinglePhoto processes a single photo (non-album).
// Called from session worker - no locking needed.
func (h *ListingHandler) processSinglePhoto(ctx context.Context, session *UserSession, photo tgbotapi.PhotoSize) {
	if session.draftID != "" {
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
	if session.isCreatingDraft {
		session.reply("Odota hetki, luodaan ilmoitusta...")
		return
	}

	// Check if draft already exists (shouldn't happen but handle gracefully)
	if session.draftID != "" {
		return
	}

	// Mark that we're creating a draft
	session.isCreatingDraft = true
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

	session.initAdInputClient()
	client := session.adInputClient

	if client == nil {
		session.isCreatingDraft = false
		session.reply("Virhe: ei voitu alustaa yhteyttä")
		return
	}

	// Download all photos (network I/O)
	var photoDataList [][]byte
	var validPhotos []tgbotapi.PhotoSize
	for _, photo := range photos {
		data, err := downloadFileID(h.tg.GetFileDirectURL, photo.FileID)
		if err != nil {
			log.Error().Err(err).Str("fileID", photo.FileID).Msg("failed to download photo")
			continue
		}
		photoDataList = append(photoDataList, data)
		validPhotos = append(validPhotos, photo)
	}

	if len(photoDataList) == 0 {
		session.isCreatingDraft = false
		session.reply("Virhe: kuvien lataus epäonnistui")
		return
	}

	// Analyze with Gemini vision (network I/O)
	if h.visionAnalyzer == nil {
		session.isCreatingDraft = false
		session.reply("Kuva-analyysi ei ole käytettävissä")
		return
	}

	result, err := h.visionAnalyzer.AnalyzeImages(ctx, photoDataList)
	if err != nil {
		log.Error().Err(err).Msg("failed to analyze image(s)")
		session.isCreatingDraft = false
		session.replyWithError(err)
		return
	}

	log.Info().
		Str("title", result.Item.Title).
		Int("imageCount", len(photoDataList)).
		Float64("cost", result.Usage.CostUSD).
		Msg("image(s) analyzed")

	// Create draft ad (network I/O)
	draftID, etag, err := h.startNewAdFlow(ctx, client)
	if err != nil {
		session.isCreatingDraft = false
		session.replyWithError(err)
		return
	}

	// Upload all photos (network I/O)
	var allImages []UploadedImage
	for i, photoData := range photoDataList {
		uploaded, err := h.uploadPhotoToAd(ctx, client, draftID, photoData, validPhotos[i].Width, validPhotos[i].Height)
		if err != nil {
			log.Error().Err(err).Int("index", i).Msg("failed to upload photo")
			continue
		}
		allImages = append(allImages, *uploaded)
	}

	if len(allImages) == 0 {
		session.isCreatingDraft = false
		session.reply("Virhe: kuvien lähetys epäonnistui")
		return
	}

	// Set images on draft (network I/O)
	newEtag, err := h.setImageOnDraft(ctx, client, draftID, etag, allImages)
	if err != nil {
		session.isCreatingDraft = false
		session.replyWithError(err)
		return
	}
	etag = newEtag

	// Get category predictions (network I/O)
	categories, err := h.getCategoryPredictions(ctx, client, draftID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get category predictions")
		categories = []tori.CategoryPrediction{}
	}

	// Update session state
	session.isCreatingDraft = false
	session.photos = append(session.photos, validPhotos...)
	session.draftID = draftID
	session.etag = etag

	// Initialize the draft with vision results
	session.currentDraft = &AdInputDraft{
		State:               AdFlowStateAwaitingCategory,
		Title:               result.Item.Title,
		Description:         result.Item.Description,
		TradeType:           "1", // Default to sell
		CollectedAttrs:      make(map[string]string),
		Images:              allImages,
		CategoryPredictions: categories,
	}

	// Start draft expiration timer
	h.startDraftExpirationTimer(session)

	// Send title message (user can reply to edit)
	titleMsg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(result.Item.Title)))
	titleMsg.ParseMode = tgbotapi.ModeMarkdown
	sentTitle := session.replyWithMessage(titleMsg)
	session.currentDraft.TitleMessageID = sentTitle.MessageID

	// Send description message (user can reply to edit)
	descMsg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(result.Item.Description)))
	descMsg.ParseMode = tgbotapi.ModeMarkdown
	sentDesc := session.replyWithMessage(descMsg)
	session.currentDraft.DescriptionMessageID = sentDesc.MessageID

	// Auto-select category using LLM if possible
	if len(categories) > 0 {
		// Try LLM-based category selection (includes two-stage fallback)
		selectionResult := h.tryAutoSelectCategory(ctx, result.Item.Title, result.Item.Description, categories)

		// Update session's category predictions if fallback found new categories
		if selectionResult.UsedFallback && len(selectionResult.UpdatedPredictions) > 0 {
			session.currentDraft.CategoryPredictions = selectionResult.UpdatedPredictions
		}

		if selectionResult.SelectedID > 0 {
			h.ProcessCategorySelection(ctx, session, selectionResult.SelectedID)
			return
		}

		// Fall back to manual selection (use updated predictions if available)
		categoriesToShow := selectionResult.UpdatedPredictions
		msg := tgbotapi.NewMessage(session.userId, "Valitse osasto")
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = makeCategoryPredictionKeyboard(categoriesToShow)
		session.replyWithMessage(msg)
	} else {
		// No categories predicted, use default
		session.currentDraft.CategoryID = 76 // "Muu" category
		session.reply("Ei osastoehdotuksia, käytetään oletusta.")
		h.promptForPrice(ctx, session)
	}
}

// addPhotoToExistingDraft adds a photo to an existing draft.
// Called from session worker - no locking needed.
func (h *ListingHandler) addPhotoToExistingDraft(ctx context.Context, session *UserSession, photo tgbotapi.PhotoSize) {
	// Touch draft activity on photo addition
	h.touchDraftActivity(session)

	session.reply("Lisätään kuva...")
	// Send typing indicator after the reply (replies clear typing status)
	session.sendTypingAction()
	client := session.adInputClient
	draftID := session.draftID

	// Check for nil client (session may have been reset)
	if client == nil || draftID == "" {
		log.Warn().Int64("userId", session.userId).Msg("cannot add photo: no active draft")
		return
	}

	// Download the photo (network I/O)
	photoData, err := downloadFileID(h.tg.GetFileDirectURL, photo.FileID)
	if err != nil {
		log.Error().Err(err).Msg("failed to download photo")
		session.replyWithError(err)
		return
	}

	// Upload photo to draft (network I/O)
	uploaded, err := h.uploadPhotoToAd(ctx, client, draftID, photoData, photo.Width, photo.Height)
	if err != nil {
		session.replyWithError(err)
		return
	}

	// Update session state
	if session.currentDraft == nil {
		log.Info().Int64("userId", session.userId).Msg("draft cancelled during photo upload")
		return
	}
	session.photos = append(session.photos, photo)
	session.currentDraft.Images = append(session.currentDraft.Images, *uploaded)
	images := make([]UploadedImage, len(session.currentDraft.Images))
	copy(images, session.currentDraft.Images)
	currentEtag := session.etag

	// Update images on draft (network I/O)
	newEtag, err := h.setImageOnDraft(ctx, client, draftID, currentEtag, images)
	if err != nil {
		session.replyWithError(err)
		return
	}

	if session.currentDraft == nil {
		log.Info().Int64("userId", session.userId).Msg("draft cancelled during image update")
		return
	}
	session.etag = newEtag
	session.reply(fmt.Sprintf("Kuva lisätty! Kuvia yhteensä: %d", len(session.photos)))
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

	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingCategory {
		session.reply("Ei aktiivista ilmoitusta")
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
	if session.currentDraft == nil {
		return
	}

	// Find category for logging and display
	var categoryLabel string
	var categoryPath string
	for _, cat := range session.currentDraft.CategoryPredictions {
		if cat.ID == categoryID {
			categoryLabel = cat.Label
			categoryPath = tori.GetCategoryPath(cat)
			break
		}
	}

	session.currentDraft.CategoryID = categoryID
	log.Info().Int("categoryId", categoryID).Str("label", categoryLabel).Msg("category selected")

	session.reply(fmt.Sprintf("Osasto: *%s*", categoryPath))

	// Get client and draft info for network calls
	client := session.adInputClient
	draftID := session.draftID
	etag := session.etag

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
		if session.currentDraft == nil {
			// Draft was cancelled during attribute fetch
			log.Info().Int64("userId", session.userId).Msg("draft cancelled during attribute fetch (error path)")
			return
		}
		session.etag = newEtag
		session.currentDraft.State = AdFlowStateAwaitingPrice
		session.reply("Syötä hinta")
		return
	}

	if session.currentDraft == nil {
		// Draft was cancelled during attribute fetch
		log.Info().Int64("userId", session.userId).Msg("draft cancelled during attribute fetch")
		return
	}

	session.etag = newEtag
	session.adAttributes = attrs

	// Get required SELECT attributes
	requiredAttrs := tori.GetRequiredSelectAttributes(attrs)

	// Try auto-selection for required attributes using LLM
	if len(requiredAttrs) > 0 {
		requiredAttrs = h.tryAutoSelectAttributes(ctx, session, requiredAttrs)
	}

	// Try to restore preserved attribute values (e.g., condition) if compatible
	if preserved := session.currentDraft.PreservedValues; preserved != nil && len(requiredAttrs) > 0 {
		requiredAttrs = h.tryRestorePreservedAttributes(session, requiredAttrs, preserved)
	}

	if len(requiredAttrs) > 0 {
		session.currentDraft.RequiredAttrs = requiredAttrs
		session.currentDraft.CurrentAttrIndex = 0
		session.currentDraft.State = AdFlowStateAwaitingAttribute
		h.promptForAttribute(session, requiredAttrs[0])
	} else {
		h.proceedAfterAttributes(ctx, session)
	}
}

// HandleAttributeInput handles user selection of an attribute value.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleAttributeInput(ctx context.Context, session *UserSession, text string) {
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingAttribute {
		return
	}

	attrs := session.currentDraft.RequiredAttrs
	idx := session.currentDraft.CurrentAttrIndex

	if idx >= len(attrs) {
		// Shouldn't happen, but handle gracefully
		h.proceedAfterAttributes(ctx, session)
		return
	}

	currentAttr := attrs[idx]

	// Handle skip button - don't add anything to CollectedAttrs for this attribute
	if text == SkipButtonLabel {
		log.Info().Str("attr", currentAttr.Name).Msg("attribute skipped")

		// Move to next attribute or price input
		session.currentDraft.CurrentAttrIndex++
		if session.currentDraft.CurrentAttrIndex < len(attrs) {
			nextAttr := attrs[session.currentDraft.CurrentAttrIndex]
			h.promptForAttribute(session, nextAttr)
		} else {
			h.proceedAfterAttributes(ctx, session)
		}
		return
	}

	// Find the selected option by label
	opt := tori.FindOptionByLabel(&currentAttr, text)
	if opt == nil {
		// Invalid selection, prompt again
		session.reply(fmt.Sprintf("Valitse jokin vaihtoehdoista tai paina '%s': %s", SkipButtonLabel, strings.ToLower(currentAttr.Label)))
		h.promptForAttribute(session, currentAttr)
		return
	}

	// Store the selected value
	session.currentDraft.CollectedAttrs[currentAttr.Name] = strconv.Itoa(opt.ID)
	log.Info().Str("attr", currentAttr.Name).Str("label", text).Int("optionId", opt.ID).Msg("attribute selected")

	// Move to next attribute or price input
	session.currentDraft.CurrentAttrIndex++
	if session.currentDraft.CurrentAttrIndex < len(attrs) {
		nextAttr := attrs[session.currentDraft.CurrentAttrIndex]
		h.promptForAttribute(session, nextAttr)
	} else {
		h.proceedAfterAttributes(ctx, session)
	}
}

// HandlePriceInput handles price input when awaiting price.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePriceInput(ctx context.Context, session *UserSession, text string) {
	// Check for giveaway selection
	if strings.Contains(text, "Annetaan") || strings.Contains(text, "🎁") {
		h.handleGiveawaySelection(ctx, session)
		return
	}

	// Parse price from text
	price, err := parsePriceMessage(text)
	if err != nil {
		session.reply("En ymmärtänyt hintaa. Syötä hinta numerona (esim. 50€ tai 50)")
		return
	}

	session.currentDraft.Price = price
	session.currentDraft.TradeType = TradeTypeSell
	session.currentDraft.State = AdFlowStateAwaitingShipping

	// Remove reply keyboard and confirm price
	session.replyAndRemoveCustomKeyboard(fmt.Sprintf("Hinta: *%d€*", price))

	// Ask about shipping
	msg := tgbotapi.NewMessage(session.userId, "Onko postitus mahdollinen?")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Kyllä", "shipping:yes"),
			tgbotapi.NewInlineKeyboardButtonData("Ei", "shipping:no"),
		),
	)
	session.replyWithMessage(msg)
}

// HandlePostalCodeInput handles postal code input when awaiting postal code.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandlePostalCodeInput(session *UserSession, text string) {
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingPostalCode {
		return
	}

	postalCode := strings.TrimSpace(text)
	if !isValidPostalCode(postalCode) {
		session.reply(postalCodeInvalidText)
		return
	}

	// Save postal code
	if h.sessionStore != nil {
		if err := h.sessionStore.SetPostalCode(session.userId, postalCode); err != nil {
			log.Error().Err(err).Msg("failed to save postal code")
			session.replyWithError(err)
			return
		}
	}

	session.reply(fmt.Sprintf(postalCodeUpdatedText, postalCode))
	h.showAdSummary(session)
}

// handleGiveawaySelection handles the user selecting "Annetaan" (give away) mode.
// Called from session worker - no locking needed.
func (h *ListingHandler) handleGiveawaySelection(ctx context.Context, session *UserSession) {
	session.currentDraft.Price = 0
	session.currentDraft.TradeType = TradeTypeGive

	// Get current description for LLM rewrite
	originalDescription := session.currentDraft.Description
	descMsgID := session.currentDraft.DescriptionMessageID
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
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingPrice {
		return
	}

	// Update description if it was rewritten
	if newDescription != originalDescription {
		session.currentDraft.Description = newDescription

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

	session.currentDraft.State = AdFlowStateAwaitingShipping

	// Remove reply keyboard and confirm giveaway
	session.replyAndRemoveCustomKeyboard("Hinta: *Annetaan*")

	// Ask about shipping (meetup possible even for giveaways)
	msg := tgbotapi.NewMessage(session.userId, "Onko postitus mahdollinen?")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Kyllä", "shipping:yes"),
			tgbotapi.NewInlineKeyboardButtonData("Ei", "shipping:no"),
		),
	)
	session.replyWithMessage(msg)
}

// HandleShippingSelection handles the shipping yes/no callback.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleShippingSelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	isYes := strings.HasSuffix(query.Data, ":yes")

	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingShipping {
		return
	}

	// Touch draft activity on shipping selection
	h.touchDraftActivity(session)

	session.currentDraft.ShippingPossible = isYes

	// Remove the inline keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	// Apply template if user has one
	if h.sessionStore != nil {
		tmpl, err := h.sessionStore.GetTemplate(session.userId)
		if err == nil && tmpl != nil {
			expanded := expandTemplate(tmpl.Content, isYes)
			// Always update template state (may be empty if no content for this shipping option)
			session.currentDraft.TemplateContent = strings.TrimSpace(expanded)

			// Update the description message in chat with combined content
			if session.currentDraft.DescriptionMessageID != 0 {
				editMsg := tgbotapi.NewEditMessageText(
					session.userId,
					session.currentDraft.DescriptionMessageID,
					fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(session.currentDraft.GetFullDescription())),
				)
				editMsg.ParseMode = tgbotapi.ModeMarkdown
				h.tg.Request(editMsg)
			}
		}
	}

	// Check if postal code is set
	if h.sessionStore != nil {
		postalCode, err := h.sessionStore.GetPostalCode(session.userId)
		if err != nil {
			log.Error().Err(err).Msg("failed to get postal code")
		}
		if postalCode == "" {
			// Prompt for postal code
			session.currentDraft.State = AdFlowStateAwaitingPostalCode
			session.reply(postalCodePromptText)
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
	if session.currentDraft == nil || query.Message == nil || query.Message.MessageID != session.currentDraft.SummaryMessageID {
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
		session.replyAndRemoveCustomKeyboard(okText)
	}
}

// HandleSendListing sends the listing using the adinput API.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleSendListing(ctx context.Context, session *UserSession) {
	if session.currentDraft == nil || len(session.photos) == 0 {
		session.reply("Ei ilmoitusta lähetettäväksi. Lähetä ensin kuva.")
		return
	}

	if session.currentDraft.State != AdFlowStateReadyToPublish {
		switch session.currentDraft.State {
		case AdFlowStateAwaitingCategory:
			session.reply("Valitse ensin osasto.")
		case AdFlowStateAwaitingAttribute:
			session.reply("Täytä ensin lisätiedot.")
		case AdFlowStateAwaitingPrice:
			session.reply("Syötä ensin hinta.")
		case AdFlowStateAwaitingShipping:
			session.reply("Valitse ensin postitusvaihtoehto.")
		case AdFlowStateAwaitingPostalCode:
			session.reply("Syötä ensin postinumero.")
		default:
			session.reply("Ilmoitus ei ole valmis lähetettäväksi.")
		}
		return
	}

	// Copy data needed for network ops
	draftID := session.draftID
	etag := session.etag
	draftCopy := *session.currentDraft
	images := make([]UploadedImage, len(session.currentDraft.Images))
	copy(images, session.currentDraft.Images)
	client := session.adInputClient
	userId := session.userId

	session.reply("Lähetetään ilmoitusta...")

	// Get postal code from storage
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
		session.currentDraft.State = AdFlowStateAwaitingPostalCode
		session.reply(postalCodePromptText)
		return
	}

	// Set category on draft (network I/O)
	newEtag, err := h.setCategoryOnDraft(ctx, client, draftID, etag, draftCopy.CategoryID)
	if err != nil {
		session.replyWithError(err)
		return
	}
	etag = newEtag

	// Update and publish (network I/O)
	if err := h.updateAndPublishAd(ctx, client, draftID, etag, &draftCopy, images, postalCode); err != nil {
		session.replyWithError(err)
		return
	}

	// Check if draft was cancelled during publish - if so, the listing was still published
	// successfully on Tori's side, so just log it and clean up
	if session.currentDraft == nil {
		log.Info().
			Int64("userId", session.userId).
			Str("title", draftCopy.Title).
			Msg("draft cancelled during publish but listing was published successfully")
		session.replyAndRemoveCustomKeyboard(listingSentText)
		return
	}
	session.replyAndRemoveCustomKeyboard(listingSentText)
	log.Info().Str("title", draftCopy.Title).Int("price", draftCopy.Price).Msg("listing published")
	session.reset()
}

// HandleTitleDescriptionReply handles replies to title/description messages for editing.
// Called from session worker - no locking needed.
func (h *ListingHandler) HandleTitleDescriptionReply(session *UserSession, message *tgbotapi.Message) bool {
	draft := session.currentDraft
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

	title := session.currentDraft.Title
	description := session.currentDraft.Description

	selectedMap, err := gemini.SelectAttributes(ctx, title, description, autoSelectableAttrs)
	if err != nil {
		log.Warn().Err(err).Msg("LLM attribute selection failed, falling back to manual")
		return attrs
	}

	if len(selectedMap) == 0 {
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
			session.currentDraft.CollectedAttrs[attr.Name] = strconv.Itoa(selectedID)
			autoSelectedInfo = append(autoSelectedInfo, fmt.Sprintf("%s: *%s*", attr.Label, selectedLabel))
			log.Info().Str("attr", attr.Name).Str("label", selectedLabel).Int("optionId", selectedID).Msg("attribute auto-selected")
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
			session.currentDraft.CollectedAttrs[attr.Name] = preservedValue
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
	preserved := session.currentDraft.PreservedValues

	// Only restore if we have a set price (>0) or it is a giveaway.
	// Default state (Price=0, TradeType="1") should trigger a prompt.
	shouldRestore := preserved != nil && (preserved.Price > 0 || preserved.TradeType == TradeTypeGive)

	if !shouldRestore {
		h.promptForPrice(ctx, session)
		return
	}

	// Restore price and trade type
	session.currentDraft.Price = preserved.Price
	session.currentDraft.TradeType = preserved.TradeType

	// If shipping was also set, restore it and go to summary
	if preserved.ShippingSet {
		session.currentDraft.ShippingPossible = preserved.ShippingPossible

		// Clear preserved values as we're done with them
		session.currentDraft.PreservedValues = nil

		// Check postal code before showing summary (matching HandleShippingSelection logic)
		if h.sessionStore != nil {
			postalCode, _ := h.sessionStore.GetPostalCode(session.userId)
			if postalCode == "" {
				session.currentDraft.State = AdFlowStateAwaitingPostalCode
				session.reply(postalCodePromptText)
				return
			}
		}

		// Go directly to summary
		h.showAdSummary(session)
		return
	}

	// Clear preserved values as price is restored, but shipping still needs to be set
	session.currentDraft.PreservedValues = nil

	// Need to ask for shipping
	session.currentDraft.State = AdFlowStateAwaitingShipping
	msg := tgbotapi.NewMessage(session.userId, "Onko postitus mahdollinen?")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Kyllä", "shipping:yes"),
			tgbotapi.NewInlineKeyboardButtonData("Ei", "shipping:no"),
		),
	)
	session.replyWithMessage(msg)
}

// promptForAttribute shows a keyboard to select an attribute value.
func (h *ListingHandler) promptForAttribute(session *UserSession, attr tori.Attribute) {
	msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("Valitse %s", strings.ToLower(attr.Label)))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = makeAttributeKeyboard(attr)
	session.replyWithMessage(msg)
}

// promptForPrice fetches price recommendations and prompts the user.
// Called from session worker - no locking needed.
func (h *ListingHandler) promptForPrice(ctx context.Context, session *UserSession) {
	session.currentDraft.State = AdFlowStateAwaitingPrice
	title := session.currentDraft.Title
	description := session.currentDraft.Description

	// Generate optimized search query using LLM
	searchQuery := title // fallback to title
	if gemini := llm.GetGeminiAnalyzer(h.visionAnalyzer); gemini != nil {
		generatedQuery, err := gemini.GeneratePriceSearchQuery(ctx, title, description)
		if err != nil {
			log.Warn().Err(err).Str("title", title).Msg("failed to generate price search query, using title")
		} else if generatedQuery != "" {
			searchQuery = generatedQuery
		}
	}

	// Build category taxonomy for filtering
	var categoryTaxonomy tori.CategoryTaxonomy
	if cat := tori.FindCategoryByID(session.currentDraft.CategoryPredictions, session.currentDraft.CategoryID); cat != nil {
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

		log.Info().
			Str("title", title).
			Int("median", medianPrice).
			Int("min", minPrice).
			Int("max", maxPrice).
			Int("count", len(prices)).
			Msg("price recommendation")
	}

	// Check if state is still valid (user may have cancelled during search)
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingPrice {
		return
	}

	msgText := fmt.Sprintf("Syötä hinta%s", recommendationMsg)
	msg := tgbotapi.NewMessage(session.userId, msgText)
	msg.ParseMode = tgbotapi.ModeMarkdown

	// Build keyboard with price suggestion (if available) and giveaway button
	var rows [][]tgbotapi.KeyboardButton
	if recommendedPrice > 0 {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("%d€", recommendedPrice)),
			tgbotapi.NewKeyboardButton("🎁 Annetaan"),
		))
	} else {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🎁 Annetaan"),
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
	session.currentDraft.State = AdFlowStateReadyToPublish

	shippingText := "Ei"
	if session.currentDraft.ShippingPossible {
		shippingText = "Kyllä"
	}

	// Show "Annetaan" for giveaways, price for sales
	var priceText string
	if session.currentDraft.TradeType == TradeTypeGive {
		priceText = "🎁 Annetaan"
	} else {
		priceText = fmt.Sprintf("%d€", session.currentDraft.Price)
	}

	summaryText := fmt.Sprintf(`*Ilmoitus valmis:*
📦 *Otsikko:* %s
📝 *Kuvaus:* %s
💰 *Hinta:* %s
🚚 *Postitus:* %s
📷 *Kuvia:* %d`,
		escapeMarkdown(session.currentDraft.Title),
		escapeMarkdown(session.currentDraft.GetFullDescription()),
		priceText,
		shippingText,
		len(session.photos),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Julkaise", "publish:confirm"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Peru", "publish:cancel"),
		),
	)

	// If a summary message already exists, edit it instead of sending a new one
	if session.currentDraft.SummaryMessageID != 0 {
		editMsg := tgbotapi.NewEditMessageTextAndMarkup(
			session.userId,
			session.currentDraft.SummaryMessageID,
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
			log.Warn().Err(err).Int("msgID", session.currentDraft.SummaryMessageID).Msg("failed to edit summary, sending new one")

			newMsg := tgbotapi.NewMessage(session.userId, summaryText)
			newMsg.ParseMode = tgbotapi.ModeMarkdown
			newMsg.ReplyMarkup = keyboard
			sentMsg, _ := h.tg.Send(newMsg)
			session.currentDraft.SummaryMessageID = sentMsg.MessageID
		}
	} else {
		newMsg := tgbotapi.NewMessage(session.userId, summaryText)
		newMsg.ParseMode = tgbotapi.ModeMarkdown
		newMsg.ReplyMarkup = keyboard
		sentMsg, _ := h.tg.Send(newMsg)
		session.currentDraft.SummaryMessageID = sentMsg.MessageID
	}
}

// startNewAdFlow creates a draft and returns the ID and ETag.
func (h *ListingHandler) startNewAdFlow(ctx context.Context, client tori.AdService) (draftID string, etag string, err error) {
	log.Info().Msg("creating draft ad")
	draft, err := client.CreateDraftAd(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to create draft: %w", err)
	}

	log.Info().Str("draftId", draft.ID).Msg("draft ad created")
	return draft.ID, draft.ETag, nil
}

// uploadPhotoToAd uploads a photo to the draft ad.
func (h *ListingHandler) uploadPhotoToAd(ctx context.Context, client tori.AdService, draftID string, photoData []byte, width, height int) (*UploadedImage, error) {
	if draftID == "" {
		return nil, fmt.Errorf("no draft ad to upload to")
	}

	resp, err := client.UploadImage(ctx, draftID, photoData)
	if err != nil {
		return nil, fmt.Errorf("failed to upload image: %w", err)
	}

	return &UploadedImage{
		ImagePath: resp.ImagePath,
		Location:  resp.Location,
		Width:     width,
		Height:    height,
	}, nil
}

// setImageOnDraft sets the uploaded image(s) on the draft and returns new ETag.
func (h *ListingHandler) setImageOnDraft(ctx context.Context, client tori.AdService, draftID, etag string, images []UploadedImage) (string, error) {
	if len(images) == 0 {
		return etag, nil
	}

	imageData := make([]map[string]any, len(images))
	for i, img := range images {
		imageData[i] = map[string]any{
			"uri":    img.ImagePath,
			"width":  img.Width,
			"height": img.Height,
			"type":   "image/jpg",
		}
	}

	patchResp, err := client.PatchItem(ctx, draftID, etag, map[string]any{
		"image": imageData,
	})
	if err != nil {
		return "", fmt.Errorf("failed to set image on item: %w", err)
	}

	return patchResp.ETag, nil
}

// getCategoryPredictions gets AI-suggested categories from the uploaded image.
func (h *ListingHandler) getCategoryPredictions(ctx context.Context, client tori.AdService, draftID string) ([]tori.CategoryPrediction, error) {
	if draftID == "" {
		return nil, fmt.Errorf("no draft ad")
	}

	return client.GetCategoryPredictions(ctx, draftID)
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

	// Update the ad
	_, err := client.UpdateAd(ctx, draftID, etag, payload)
	if err != nil {
		return fmt.Errorf("failed to update ad: %w", err)
	}

	// Set delivery options - always use meetup mode since ToriDiili shipping
	// requires full address/phone/package info that we don't collect
	err = client.SetDeliveryOptions(ctx, draftID, tori.DeliveryOptions{
		BuyNow:             false,
		Client:             "ANDROID",
		Meetup:             true,
		SellerPaysShipping: false,
		Shipping:           false,
	})
	if err != nil {
		return fmt.Errorf("failed to set delivery options: %w", err)
	}

	// Publish
	_, err = client.PublishAd(ctx, draftID)
	if err != nil {
		return fmt.Errorf("failed to publish ad: %w", err)
	}

	return nil
}

// HandleEditCommand handles natural language edit commands.
// Returns true if the message was processed as an edit command.
func (h *ListingHandler) HandleEditCommand(ctx context.Context, session *UserSession, message string) bool {
	if h.editIntentParser == nil {
		log.Debug().Msg("edit intent parser not configured")
		return false
	}

	// Get current draft info (needs lock)
	session.mu.Lock()
	if session.currentDraft == nil {
		session.mu.Unlock()
		log.Debug().Msg("no current draft for edit command")
		return false
	}

	draftInfo := &llm.CurrentDraftInfo{
		Title:       session.currentDraft.Title,
		Description: session.currentDraft.Description,
		Price:       session.currentDraft.Price,
	}
	session.mu.Unlock()

	log.Debug().
		Str("message", message).
		Str("title", draftInfo.Title).
		Int("price", draftInfo.Price).
		Msg("parsing edit intent")

	// Parse edit intent (NO LOCK - network I/O)
	intent, err := h.editIntentParser.ParseEditIntent(ctx, message, draftInfo)
	if err != nil {
		log.Warn().Err(err).Str("message", message).Msg("failed to parse edit intent")
		return false
	}

	// Check if any changes were requested
	if intent.NewPrice == nil && intent.NewTitle == nil && intent.NewDescription == nil {
		log.Debug().Str("message", message).Msg("no edit intent detected in message")
		return false
	}

	// Apply changes (needs lock)
	session.mu.Lock()
	defer session.mu.Unlock()

	// Double-check draft still exists
	if session.currentDraft == nil {
		return false
	}

	var changes []string

	// Apply price change
	if intent.NewPrice != nil {
		oldPrice := session.currentDraft.Price
		session.currentDraft.Price = *intent.NewPrice
		changes = append(changes, fmt.Sprintf("Hinta: %d€ → %d€", oldPrice, *intent.NewPrice))
		log.Info().Int("oldPrice", oldPrice).Int("newPrice", *intent.NewPrice).Msg("price updated via edit command")
	}

	// Apply title change
	if intent.NewTitle != nil {
		oldTitle := session.currentDraft.Title
		session.currentDraft.Title = *intent.NewTitle
		changes = append(changes, fmt.Sprintf("Otsikko: %s", *intent.NewTitle))

		// Update title message in chat
		if session.currentDraft.TitleMessageID != 0 {
			editMsg := tgbotapi.NewEditMessageText(
				session.userId,
				session.currentDraft.TitleMessageID,
				fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(*intent.NewTitle)),
			)
			editMsg.ParseMode = tgbotapi.ModeMarkdown
			h.tg.Request(editMsg)
		}
		log.Info().Str("oldTitle", oldTitle).Str("newTitle", *intent.NewTitle).Msg("title updated via edit command")
	}

	// Apply description change
	if intent.NewDescription != nil {
		session.currentDraft.Description = *intent.NewDescription
		changes = append(changes, "Kuvaus päivitetty")

		// Update description message in chat (include template if present)
		if session.currentDraft.DescriptionMessageID != 0 {
			editMsg := tgbotapi.NewEditMessageText(
				session.userId,
				session.currentDraft.DescriptionMessageID,
				fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(session.currentDraft.GetFullDescription())),
			)
			editMsg.ParseMode = tgbotapi.ModeMarkdown
			h.tg.Request(editMsg)
		}
		log.Info().Str("newDescription", *intent.NewDescription).Msg("description updated via edit command")
	}

	// Send confirmation message
	if len(changes) == 1 {
		session.reply("✓ " + changes[0])
	} else if len(changes) > 1 {
		confirmMsg := "✓ Muutokset tehty:\n- " + strings.Join(changes, "\n- ")
		session.reply(confirmMsg)
	}

	// Show updated summary if listing is ready to publish
	if len(changes) > 0 && session.currentDraft.State == AdFlowStateReadyToPublish {
		h.showAdSummary(session)
	}

	return true
}

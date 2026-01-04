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
	// maxBulkDrafts is the maximum number of drafts allowed in a bulk session
	maxBulkDrafts = 10
	// bulkStatusUpdateDebounce is how long to wait before updating the status message
	bulkStatusUpdateDebounce = 300 * time.Millisecond
	// bulkAlbumBufferTimeout is how long to wait for more photos in an album
	bulkAlbumBufferTimeout = 1500 * time.Millisecond
)

// BulkHandler handles bulk listing operations.
type BulkHandler struct {
	tg             BotAPI
	visionAnalyzer llm.Analyzer
	sessionStore   storage.SessionStore
	searchClient   *tori.SearchClient
}

// NewBulkHandler creates a new bulk handler.
func NewBulkHandler(tg BotAPI, visionAnalyzer llm.Analyzer, sessionStore storage.SessionStore) *BulkHandler {
	return &BulkHandler{
		tg:             tg,
		visionAnalyzer: visionAnalyzer,
		sessionStore:   sessionStore,
		searchClient:   tori.NewSearchClient(),
	}
}

// HandleEräCommand enters bulk listing mode.
func (h *BulkHandler) HandleEräCommand(ctx context.Context, session *UserSession) {
	if session.bulkSession != nil && session.bulkSession.Active {
		session.reply(MsgBulkAlreadyActive)
		return
	}

	// Check if there's an active single listing
	if session.draftID != "" {
		session.reply(MsgBulkHasActiveListing)
		return
	}

	session.StartBulkSession()
	session.reply(MsgBulkStarted)
}

// HandlePhoto processes a photo in bulk mode.
func (h *BulkHandler) HandlePhoto(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	bulk := session.bulkSession
	if bulk == nil || !bulk.Active {
		return
	}

	// Check draft limit
	if len(bulk.Drafts) >= maxBulkDrafts {
		session.reply(fmt.Sprintf(MsgBulkMaxDraftsReached, maxBulkDrafts))
		return
	}

	largestPhoto := message.Photo[len(message.Photo)-1]
	mediaGroupID := message.MediaGroupID

	// Check if this is part of an album
	if mediaGroupID != "" {
		h.bufferAlbumPhoto(ctx, session, largestPhoto, mediaGroupID)
		return
	}

	// Single photo - create a new draft
	h.createDraftFromPhoto(ctx, session, []AlbumPhoto{{
		FileID: largestPhoto.FileID,
		Width:  largestPhoto.Width,
		Height: largestPhoto.Height,
	}})
}

// bufferAlbumPhoto buffers photos from an album before processing.
func (h *BulkHandler) bufferAlbumPhoto(ctx context.Context, session *UserSession, photo tgbotapi.PhotoSize, mediaGroupID string) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	// Initialize or update album buffer
	if bulk.AlbumBuffer == nil || bulk.AlbumBuffer.MediaGroupID != mediaGroupID {
		// If there's an existing buffer, flush it first
		if bulk.AlbumBuffer != nil && len(bulk.AlbumBuffer.Photos) > 0 {
			if bulk.AlbumBuffer.Timer != nil {
				bulk.AlbumBuffer.Timer.Stop()
			}
			// Create draft from the buffered photos
			h.createDraftFromPhoto(ctx, session, bulk.AlbumBuffer.Photos)
		}
		bulk.AlbumBuffer = &AlbumBuffer{
			MediaGroupID:  mediaGroupID,
			Photos:        []AlbumPhoto{},
			FirstReceived: time.Now(),
		}
	}

	// Add photo to buffer (respect max limit)
	if len(bulk.AlbumBuffer.Photos) < maxAlbumPhotos {
		bulk.AlbumBuffer.Photos = append(bulk.AlbumBuffer.Photos, AlbumPhoto{
			FileID: photo.FileID,
			Width:  photo.Width,
			Height: photo.Height,
		})
	}

	// Reset or start timer
	if bulk.AlbumBuffer.Timer != nil {
		bulk.AlbumBuffer.Timer.Stop()
	}

	albumBuffer := bulk.AlbumBuffer
	bulk.AlbumBuffer.Timer = time.AfterFunc(bulkAlbumBufferTimeout, func() {
		session.Send(SessionMessage{
			Type:        "bulk_album_timeout",
			Ctx:         context.Background(),
			AlbumBuffer: albumBuffer,
		})
	})
}

// ProcessBulkAlbumTimeout handles album timeout in bulk mode.
func (h *BulkHandler) ProcessBulkAlbumTimeout(ctx context.Context, session *UserSession, albumBuffer *AlbumBuffer) {
	bulk := session.bulkSession
	if bulk == nil || bulk.AlbumBuffer != albumBuffer {
		return
	}

	photos := albumBuffer.Photos
	bulk.AlbumBuffer = nil

	if len(photos) == 0 {
		return
	}

	h.createDraftFromPhoto(ctx, session, photos)
}

// createDraftFromPhoto creates a new draft from photos and starts analysis.
func (h *BulkHandler) createDraftFromPhoto(ctx context.Context, session *UserSession, photos []AlbumPhoto) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	// Check draft limit again
	if len(bulk.Drafts) >= maxBulkDrafts {
		session.reply(fmt.Sprintf(MsgBulkMaxDraftsReached, maxBulkDrafts))
		return
	}

	// Create new draft with stable ID
	draft := bulk.NewBulkDraft()
	draft.Photos = photos
	draft.AnalysisStatus = BulkAnalysisAnalyzing
	bulk.Drafts = append(bulk.Drafts, draft)

	// Schedule status update
	h.scheduleStatusUpdate(session)

	// Capture the client before spawning goroutine to avoid race
	session.initAdInputClient()
	client := session.adInputClient

	// Start analysis in background with copied data
	analysisCtx, cancel := context.WithCancel(context.Background())
	draft.CancelAnalysis = cancel

	// Make a copy of photos to avoid race conditions
	photosCopy := make([]AlbumPhoto, len(photos))
	copy(photosCopy, photos)

	go h.analyzeAndCreateDraft(analysisCtx, session, draft.ID, photosCopy, client)
}

// analyzeAndCreateDraft performs vision analysis and creates Tori draft.
// This runs in a background goroutine. It receives copies of data to avoid races.
// Results are sent back through the worker channel.
func (h *BulkHandler) analyzeAndCreateDraft(ctx context.Context, session *UserSession, draftID string, photos []AlbumPhoto, client tori.AdService) {
	// Download photos
	var photoDataList [][]byte
	var validPhotos []AlbumPhoto
	for _, photo := range photos {
		data, err := DownloadTelegramFile(ctx, h.tg.GetFileDirectURL, photo.FileID)
		if err != nil {
			log.Error().Err(err).Str("fileID", photo.FileID).Msg("failed to download photo in bulk mode")
			continue
		}
		photoDataList = append(photoDataList, data)
		validPhotos = append(validPhotos, photo)
	}

	if len(photoDataList) == 0 {
		h.setDraftError(session, draftID, MsgBulkErrImageDownload)
		return
	}

	// Check if cancelled
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Vision analysis
	if h.visionAnalyzer == nil {
		h.setDraftError(session, draftID, MsgBulkErrImageAnalysis)
		return
	}

	result, err := h.visionAnalyzer.AnalyzeImages(ctx, photoDataList)
	if err != nil {
		log.Error().Err(err).Str("draftID", draftID).Msg("bulk vision analysis failed")
		h.setDraftError(session, draftID, MsgBulkErrAnalysisFailed)
		return
	}

	// Check if cancelled
	select {
	case <-ctx.Done():
		return
	default:
	}

	log.Info().
		Str("title", result.Item.Title).
		Int("imageCount", len(photoDataList)).
		Str("draftID", draftID).
		Float64("cost", result.Usage.CostUSD).
		Msg("bulk image(s) analyzed")

	// Create Tori draft
	if client == nil {
		h.setDraftError(session, draftID, MsgBulkErrToriConnection)
		return
	}

	toriDraft, err := client.CreateDraftAd(ctx)
	if err != nil {
		log.Error().Err(err).Str("draftID", draftID).Msg("failed to create Tori draft in bulk mode")
		h.setDraftError(session, draftID, MsgBulkErrDraftCreation)
		return
	}

	// Check if cancelled
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Upload images
	var uploadedImages []UploadedImage
	for i, photoData := range photoDataList {
		resp, err := client.UploadImage(ctx, toriDraft.ID, photoData)
		if err != nil {
			log.Error().Err(err).Str("draftID", draftID).Int("photoIndex", i).Msg("failed to upload image in bulk mode")
			continue
		}
		uploadedImages = append(uploadedImages, UploadedImage{
			ImagePath: resp.ImagePath,
			Location:  resp.Location,
			Width:     validPhotos[i].Width,
			Height:    validPhotos[i].Height,
		})
	}

	if len(uploadedImages) == 0 {
		h.setDraftError(session, draftID, MsgBulkErrImageUpload)
		return
	}

	// Set images on draft
	etag := toriDraft.ETag
	imageData := make([]map[string]any, len(uploadedImages))
	for i, img := range uploadedImages {
		imageData[i] = map[string]any{
			"uri":    img.ImagePath,
			"width":  img.Width,
			"height": img.Height,
			"type":   "image/jpg",
		}
	}

	patchResp, err := client.PatchItem(ctx, toriDraft.ID, etag, map[string]any{
		"image": imageData,
	})
	if err != nil {
		log.Error().Err(err).Str("draftID", draftID).Msg("failed to set images on draft in bulk mode")
		h.setDraftError(session, draftID, MsgBulkErrImageSet)
		return
	}
	etag = patchResp.ETag

	// Get category predictions
	categories, err := client.GetCategoryPredictions(ctx, toriDraft.ID)
	if err != nil {
		log.Warn().Err(err).Str("draftID", draftID).Msg("failed to get category predictions in bulk mode")
		categories = []tori.CategoryPrediction{}
	}

	// Check if cancelled
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Send results back through worker channel
	session.Send(SessionMessage{
		Type: "bulk_analysis_complete",
		Ctx:  context.Background(),
		BulkAnalysisResult: &BulkAnalysisResult{
			BulkDraftID:         draftID,
			Title:               result.Item.Title,
			Description:         result.Item.Description,
			Images:              uploadedImages,
			ToriDraftID:         toriDraft.ID,
			ETag:                etag,
			CategoryPredictions: categories,
		},
	})
}

// BulkAnalysisResult holds the result of bulk analysis to send through worker channel.
type BulkAnalysisResult struct {
	BulkDraftID         string // The stable ID of the bulk draft
	Title               string
	Description         string
	Images              []UploadedImage
	ToriDraftID         string // The Tori API draft ID
	ETag                string
	CategoryPredictions []tori.CategoryPrediction
}

// HandleBulkAnalysisComplete processes analysis completion in the worker goroutine.
func (h *BulkHandler) HandleBulkAnalysisComplete(ctx context.Context, session *UserSession, result *BulkAnalysisResult) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	draft := bulk.GetDraftByID(result.BulkDraftID)
	if draft == nil {
		// Draft was deleted during analysis
		log.Info().Str("draftID", result.BulkDraftID).Msg("bulk draft deleted during analysis, discarding result")
		return
	}

	// Update draft with analysis results
	draft.Title = result.Title
	draft.Description = result.Description
	draft.Images = result.Images
	draft.DraftID = result.ToriDraftID
	draft.ETag = result.ETag
	draft.CategoryPredictions = result.CategoryPredictions
	draft.AnalysisStatus = BulkAnalysisDone

	// Auto-select category if possible
	if len(result.CategoryPredictions) > 0 {
		if gemini := llm.GetGeminiAnalyzer(h.visionAnalyzer); gemini != nil {
			categoryID, err := gemini.SelectCategory(ctx, draft.Title, draft.Description, result.CategoryPredictions)
			if err == nil && categoryID > 0 {
				draft.CategoryID = categoryID
				// Find label
				for _, cat := range result.CategoryPredictions {
					if cat.ID == categoryID {
						draft.CategoryLabel = cat.Label
						break
					}
				}
				log.Info().Str("draftID", draft.ID).Int("categoryID", categoryID).Str("label", draft.CategoryLabel).Msg("bulk auto-selected category")
			}
		}

		// Fall back to first prediction if auto-select failed
		if draft.CategoryID == 0 && len(result.CategoryPredictions) > 0 {
			draft.CategoryID = result.CategoryPredictions[0].ID
			draft.CategoryLabel = result.CategoryPredictions[0].Label
		}
	}

	// Auto-estimate price from similar listings
	h.estimatePriceForDraft(ctx, draft)

	// Schedule status update
	h.scheduleStatusUpdate(session)
}

// estimatePriceForDraft searches for similar items and sets recommended price.
func (h *BulkHandler) estimatePriceForDraft(ctx context.Context, draft *BulkDraft) {
	if draft.Title == "" {
		return
	}

	// Build category taxonomy for filtering
	var categoryTaxonomy tori.CategoryTaxonomy
	if cat := tori.FindCategoryByID(draft.CategoryPredictions, draft.CategoryID); cat != nil {
		categoryTaxonomy = tori.GetCategoryTaxonomy(*cat)
	}

	log.Debug().
		Str("query", draft.Title).
		Str("categoryParam", categoryTaxonomy.ParamName).
		Str("categoryValue", categoryTaxonomy.Value).
		Str("draftID", draft.ID).
		Msg("searching for similar prices")

	results, err := h.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query:            draft.Title,
		CategoryTaxonomy: categoryTaxonomy,
		Rows:             20,
	})

	if err != nil {
		log.Warn().Err(err).Str("draftID", draft.ID).Msg("price search failed")
		return
	}

	resultCount := 0
	if results != nil {
		resultCount = len(results.Docs)
	}
	log.Debug().
		Int("count", resultCount).
		Str("draftID", draft.ID).
		Msg("price search returned results")

	// Collect prices
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
		Str("draftID", draft.ID).
		Msg("found prices from search")

	if len(prices) < 3 {
		log.Debug().
			Int("priceCount", len(prices)).
			Str("draftID", draft.ID).
			Msg("insufficient prices for estimation (need at least 3)")
		return
	}

	// Calculate median
	sort.Ints(prices)
	minPrice := prices[0]
	maxPrice := prices[len(prices)-1]
	medianPrice := prices[len(prices)/2]
	if len(prices)%2 == 0 {
		medianPrice = (prices[len(prices)/2-1] + prices[len(prices)/2]) / 2
	}

	draft.Price = medianPrice
	draft.TradeType = TradeTypeSell
	draft.PriceEstimate = &PriceEstimate{
		Count:  len(prices),
		Min:    minPrice,
		Max:    maxPrice,
		Median: medianPrice,
	}

	log.Info().
		Str("draftID", draft.ID).
		Str("title", draft.Title).
		Int("price", medianPrice).
		Int("sampleCount", len(prices)).
		Int("min", minPrice).
		Int("max", maxPrice).
		Msg("auto-estimated price for bulk draft")
}

// getCategoryPathByID finds a category by ID and returns its full path.
// Falls back to the stored label if the category is not found in predictions.
func getCategoryPathByID(predictions []tori.CategoryPrediction, categoryID int, fallbackLabel string) string {
	for _, cat := range predictions {
		if cat.ID == categoryID {
			return tori.GetCategoryPath(cat)
		}
	}
	return fallbackLabel
}

// setDraftError marks a draft as having an error.
func (h *BulkHandler) setDraftError(session *UserSession, draftID string, errorMsg string) {
	session.Send(SessionMessage{
		Type: "bulk_draft_error",
		Ctx:  context.Background(),
		BulkDraftError: &BulkDraftError{
			DraftID: draftID,
			Error:   errorMsg,
		},
	})
}

// BulkDraftError holds error info for a draft.
type BulkDraftError struct {
	DraftID string
	Error   string
}

// HandleBulkDraftError processes draft error in the worker goroutine.
func (h *BulkHandler) HandleBulkDraftError(session *UserSession, err *BulkDraftError) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	draft := bulk.GetDraftByID(err.DraftID)
	if draft == nil {
		// Draft was deleted during analysis
		return
	}

	draft.AnalysisStatus = BulkAnalysisError
	draft.ErrorMessage = err.Error
	h.scheduleStatusUpdate(session)
}

// scheduleStatusUpdate debounces status message updates.
func (h *BulkHandler) scheduleStatusUpdate(session *UserSession) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	if bulk.UpdateTimer != nil {
		bulk.UpdateTimer.Stop()
	}

	bulk.UpdateTimer = time.AfterFunc(bulkStatusUpdateDebounce, func() {
		session.Send(SessionMessage{
			Type: "bulk_status_update",
			Ctx:  context.Background(),
		})
	})
}

// HandleStatusUpdate updates the status message.
func (h *BulkHandler) HandleStatusUpdate(session *UserSession) {
	bulk := session.bulkSession
	if bulk == nil || !bulk.Active {
		return
	}

	text := h.formatStatusMessage(bulk)

	if bulk.StatusMessageID == 0 {
		// Send new status message
		msg := tgbotapi.NewMessage(session.userId, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		sent := session.replyWithMessage(msg)
		bulk.StatusMessageID = sent.MessageID
	} else {
		// Edit existing message
		edit := tgbotapi.NewEditMessageText(session.userId, bulk.StatusMessageID, text)
		edit.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(edit)
	}
}

// formatStatusMessage formats the bulk session status.
func (h *BulkHandler) formatStatusMessage(bulk *BulkSession) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(MsgBulkStatusHeader, len(bulk.Drafts)))

	if len(bulk.Drafts) == 0 {
		sb.WriteString(MsgBulkSendPhotosToStart)
	} else {
		for _, draft := range bulk.Drafts {
			emoji := draft.StatusEmoji()
			indexEmoji := emojiNumbers[draft.Index]

			switch draft.AnalysisStatus {
			case BulkAnalysisPending, BulkAnalysisAnalyzing:
				sb.WriteString(fmt.Sprintf("%s %s "+MsgBulkAnalyzing, indexEmoji, emoji, len(draft.Photos)))
			case BulkAnalysisDone:
				title := draft.Title
				if len(title) > 50 {
					title = title[:47] + "..."
				}
				sb.WriteString(fmt.Sprintf("%s %s %s\n", indexEmoji, emoji, escapeMarkdown(title)))
			case BulkAnalysisError:
				sb.WriteString(fmt.Sprintf("%s %s "+MsgBulkError, indexEmoji, emoji, draft.ErrorMessage))
			}
		}
	}

	sb.WriteString("\n" + MsgBulkSendPhotosOrCommand)
	return sb.String()
}

// HandleValmisCommand finishes photo collection and shows edit view.
func (h *BulkHandler) HandleValmisCommand(ctx context.Context, session *UserSession) {
	bulk := session.bulkSession
	if bulk == nil || !bulk.Active {
		session.reply(MsgBulkNotInBulkMode)
		return
	}

	if len(bulk.Drafts) == 0 {
		session.reply(MsgBulkSendPhotosFirst)
		return
	}

	// Check if analysis is still in progress
	if !bulk.IsAnalysisComplete() {
		session.reply(MsgBulkWaitAnalysis)
		return
	}

	// Show edit view for each draft
	h.showEditView(session)
}

// showEditView sends individual draft messages with edit keyboards.
func (h *BulkHandler) showEditView(session *UserSession) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	session.reply(MsgBulkEditListings)

	for _, draft := range bulk.Drafts {
		h.sendDraftMessage(session, draft)
	}
}

// sendDraftMessage sends or updates a draft's edit message.
func (h *BulkHandler) sendDraftMessage(session *UserSession, draft *BulkDraft) {
	text := h.formatDraftMessage(draft)
	keyboard := h.makeDraftKeyboard(draft)

	if draft.MessageID == 0 {
		msg := tgbotapi.NewMessage(session.userId, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = keyboard
		sent := session.replyWithMessage(msg)
		draft.MessageID = sent.MessageID
	} else {
		edit := tgbotapi.NewEditMessageText(session.userId, draft.MessageID, text)
		edit.ParseMode = tgbotapi.ModeMarkdown
		edit.ReplyMarkup = &keyboard
		h.tg.Request(edit)
	}
}

// formatDraftMessage formats a draft for the edit view.
func (h *BulkHandler) formatDraftMessage(draft *BulkDraft) string {
	var sb strings.Builder
	indexEmoji := emojiNumbers[draft.Index]

	sb.WriteString(fmt.Sprintf("%s *%s*\n", indexEmoji, escapeMarkdown(draft.Title)))
	sb.WriteString(fmt.Sprintf("📝 %s\n", escapeMarkdown(draft.Description)))

	// Photo count with correct pluralization
	photoCount := len(draft.Photos)
	if photoCount == 1 {
		sb.WriteString(MsgBulkOnePhoto)
	} else {
		sb.WriteString(fmt.Sprintf(MsgBulkMultiPhotos, photoCount))
	}

	// Price with estimation details
	if draft.TradeType == TradeTypeGive {
		sb.WriteString(MsgBulkPriceGiven)
	} else if draft.Price > 0 {
		if draft.PriceEstimate != nil {
			sb.WriteString(fmt.Sprintf(MsgBulkPriceWithEstimate,
				draft.Price, draft.PriceEstimate.Count, draft.PriceEstimate.Min, draft.PriceEstimate.Max))
		} else {
			sb.WriteString(fmt.Sprintf(MsgBulkPriceFmt, draft.Price))
		}
	} else {
		sb.WriteString(MsgBulkPriceNotSet)
	}

	// Category
	if draft.CategoryLabel != "" {
		categoryPath := getCategoryPathByID(draft.CategoryPredictions, draft.CategoryID, draft.CategoryLabel)
		sb.WriteString(fmt.Sprintf(MsgBulkCategoryFmt, categoryPath))
	} else {
		sb.WriteString(MsgBulkCategoryNone)
	}

	// Shipping
	if draft.ShippingPossible {
		sb.WriteString(MsgBulkShippingYes)
	} else {
		sb.WriteString(MsgBulkShippingNo)
	}

	// Ready status
	if draft.IsReadyToPublish() {
		sb.WriteString(MsgBulkReadyToSend)
	} else {
		sb.WriteString(MsgBulkFillMissing)
	}

	return sb.String()
}

// makeDraftKeyboard creates the inline keyboard for a draft.
func (h *BulkHandler) makeDraftKeyboard(draft *BulkDraft) tgbotapi.InlineKeyboardMarkup {
	id := draft.ID

	row1 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkTitle, "bulk:edit:title:"+id),
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkDescription, "bulk:edit:desc:"+id),
	}
	row2 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkPrice, "bulk:edit:price:"+id),
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkCategory, "bulk:edit:cat:"+id),
	}
	row3 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkShipping, "bulk:edit:shipping:"+id),
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkDelete, "bulk:delete:"+id),
	}

	return tgbotapi.NewInlineKeyboardMarkup(row1, row2, row3)
}

// HandleBulkCallback handles inline button presses for bulk mode.
func (h *BulkHandler) HandleBulkCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	if bulk == nil || !bulk.Active {
		return
	}

	data := query.Data

	// Parse callback data: bulk:action:field:id or bulk:action:id
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		return
	}

	action := parts[1]

	switch action {
	case "edit":
		if len(parts) < 4 {
			return
		}
		field := parts[2]
		draftID := parts[3]
		h.handleEditCallback(ctx, session, draftID, field, query)

	case "delete":
		draftID := parts[2]
		h.handleDeleteCallback(session, draftID, query)

	case "confirm":
		if len(parts) < 4 {
			return
		}
		subAction := parts[2]
		draftID := parts[3]
		if subAction == "delete" {
			h.confirmDelete(session, draftID, query)
		}

	case "cancel":
		// Cancel editing
		bulk.EditingDraftID = ""
		bulk.EditingField = ""
		session.reply(MsgBulkEditCancelled)

	case "cat":
		// Category selection: bulk:cat:draftID:categoryId
		if len(parts) < 4 {
			return
		}
		draftID := parts[2]
		catID, err := strconv.Atoi(parts[3])
		if err != nil {
			return
		}
		h.handleCategorySelection(ctx, session, draftID, catID, query)

	case "shipping":
		// Shipping selection: bulk:shipping:draftID:yes/no
		if len(parts) < 4 {
			return
		}
		draftID := parts[2]
		isYes := parts[3] == "yes"
		h.handleShippingSelection(session, draftID, isYes, query)

	case "price":
		// Price selection: bulk:price:draftID:give or bulk:price:draftID:123
		if len(parts) < 4 {
			return
		}
		draftID := parts[2]
		if parts[3] == "give" {
			h.handleGiveawaySelection(session, draftID, query)
		} else {
			// Numeric price from recommendation button
			price, err := strconv.Atoi(parts[3])
			if err == nil && price > 0 {
				h.handlePriceButtonSelection(session, draftID, price, query)
			}
		}
	}
}

// handleEditCallback starts editing a field.
func (h *BulkHandler) handleEditCallback(ctx context.Context, session *UserSession, draftID string, field string, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	draft := bulk.GetDraftByID(draftID)
	if draft == nil {
		return
	}

	bulk.EditingDraftID = draftID
	bulk.EditingField = field

	cancelButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(BtnCancel, "bulk:cancel:0"),
		),
	)

	switch field {
	case "title":
		msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf(MsgBulkEnterNewTitle, draft.Index+1))
		msg.ReplyMarkup = cancelButton
		session.replyWithMessage(msg)
	case "desc":
		msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf(MsgBulkEnterNewDesc, draft.Index+1))
		msg.ReplyMarkup = cancelButton
		session.replyWithMessage(msg)
	case "price":
		h.promptForBulkPrice(ctx, session, draft, draftID)
	case "cat":
		// Show category selection
		if len(draft.CategoryPredictions) == 0 {
			session.reply(MsgNoCategoryOptions)
			bulk.EditingDraftID = ""
			bulk.EditingField = ""
			return
		}
		h.showCategorySelection(session, draft)
	case "shipping":
		// Show shipping options
		msg := tgbotapi.NewMessage(session.userId, MsgShippingQuestion)
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(BtnYes, fmt.Sprintf("bulk:shipping:%s:yes", draftID)),
				tgbotapi.NewInlineKeyboardButtonData(BtnNo, fmt.Sprintf("bulk:shipping:%s:no", draftID)),
			),
		)
		msg.ReplyMarkup = keyboard
		session.replyWithMessage(msg)
	}
}

// promptForBulkPrice searches for similar items and shows price prompt with recommendation.
func (h *BulkHandler) promptForBulkPrice(ctx context.Context, session *UserSession, draft *BulkDraft, draftID string) {
	title := draft.Title

	// Build category taxonomy for filtering
	var categoryTaxonomy tori.CategoryTaxonomy
	if cat := tori.FindCategoryByID(draft.CategoryPredictions, draft.CategoryID); cat != nil {
		categoryTaxonomy = tori.GetCategoryTaxonomy(*cat)
	}

	log.Debug().
		Str("query", title).
		Str("categoryParam", categoryTaxonomy.ParamName).
		Str("categoryValue", categoryTaxonomy.Value).
		Str("draftID", draftID).
		Msg("searching for price recommendation")

	// Search for similar items
	results, err := h.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query:            title,
		CategoryTaxonomy: categoryTaxonomy,
		Rows:             20,
	})

	if err != nil {
		log.Warn().Err(err).Str("draftID", draftID).Msg("bulk price search failed")
	}

	resultCount := 0
	if results != nil {
		resultCount = len(results.Docs)
	}
	log.Debug().
		Int("count", resultCount).
		Str("draftID", draftID).
		Msg("price search returned results")

	// Collect prices from results
	var prices []int
	if err == nil && results != nil {
		for _, doc := range results.Docs {
			if doc.Price != nil && doc.Price.Amount > 0 {
				prices = append(prices, doc.Price.Amount)
			}
		}
	}

	log.Debug().
		Ints("prices", prices).
		Str("draftID", draftID).
		Msg("found prices from search")

	// Calculate recommendation
	var recommendationMsg string
	var recommendedPrice int

	if len(prices) >= 3 {
		sort.Ints(prices)
		minPrice := prices[0]
		maxPrice := prices[len(prices)-1]

		// Calculate median
		medianPrice := prices[len(prices)/2]
		if len(prices)%2 == 0 {
			medianPrice = (prices[len(prices)/2-1] + prices[len(prices)/2]) / 2
		}

		recommendedPrice = medianPrice
		recommendationMsg = fmt.Sprintf(MsgBulkPriceEstimate,
			len(prices), medianPrice, minPrice, maxPrice)

		log.Info().
			Str("draftID", draftID).
			Str("title", title).
			Int("median", medianPrice).
			Int("min", minPrice).
			Int("max", maxPrice).
			Int("count", len(prices)).
			Msg("bulk price recommendation")
	} else {
		log.Debug().
			Int("priceCount", len(prices)).
			Str("draftID", draftID).
			Msg("insufficient prices for recommendation (need at least 3)")
	}

	// Check if session is still in edit mode (user may have cancelled during search)
	bulk := session.bulkSession
	if bulk == nil || bulk.EditingDraftID != draftID {
		return
	}

	msgText := fmt.Sprintf(MsgBulkEnterPrice, draft.Index+1, recommendationMsg)
	msg := tgbotapi.NewMessage(session.userId, msgText)
	msg.ParseMode = tgbotapi.ModeMarkdown

	// Build keyboard with price suggestion (if available) and giveaway/cancel buttons
	var rows [][]tgbotapi.InlineKeyboardButton
	if recommendedPrice > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d€", recommendedPrice), fmt.Sprintf("bulk:price:%s:%d", draftID, recommendedPrice)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(BtnBulkGiveaway, fmt.Sprintf("bulk:price:%s:give", draftID)),
		tgbotapi.NewInlineKeyboardButtonData(BtnCancel, "bulk:cancel:0"),
	))

	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	session.replyWithMessage(msg)
}

// showCategorySelection shows category selection keyboard for a draft.
func (h *BulkHandler) showCategorySelection(session *UserSession, draft *BulkDraft) {
	var rows [][]tgbotapi.InlineKeyboardButton

	for i, cat := range draft.CategoryPredictions {
		displayText := tori.GetCategoryPathLastN(cat, 2)
		prefix := emojiNumbers[i]
		if i >= len(emojiNumbers) {
			prefix = fmt.Sprintf("[%d]", i+1)
		}

		callbackData := fmt.Sprintf("bulk:cat:%s:%d", draft.ID, cat.ID)
		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s %s", prefix, displayText),
			callbackData,
		)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	msg := tgbotapi.NewMessage(session.userId, MsgBulkSelectCategory)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	session.replyWithMessage(msg)
}

// handleCategorySelection processes category selection.
func (h *BulkHandler) handleCategorySelection(ctx context.Context, session *UserSession, draftID string, categoryID int, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	draft := bulk.GetDraftByID(draftID)
	if draft == nil {
		return
	}

	// Remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	draft.CategoryID = categoryID
	// Find label and path in single pass
	var categoryPath string
	for _, cat := range draft.CategoryPredictions {
		if cat.ID == categoryID {
			draft.CategoryLabel = cat.Label
			categoryPath = tori.GetCategoryPath(cat)
			break
		}
	}
	if categoryPath == "" {
		categoryPath = draft.CategoryLabel
	}

	// Set category on Tori draft
	if draft.DraftID != "" {
		client := session.adInputClient
		if client != nil {
			newEtag, err := h.setCategoryOnDraft(ctx, client, draft.DraftID, draft.ETag, categoryID)
			if err != nil {
				log.Error().Err(err).Str("draftID", draftID).Msg("failed to set category on bulk draft")
			} else {
				draft.ETag = newEtag
			}
		}
	}

	bulk.EditingDraftID = ""
	bulk.EditingField = ""

	session.reply(MsgBulkCategorySet, categoryPath)
	h.sendDraftMessage(session, draft)
}

// handleShippingSelection processes shipping selection.
func (h *BulkHandler) handleShippingSelection(session *UserSession, draftID string, isYes bool, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	draft := bulk.GetDraftByID(draftID)
	if draft == nil {
		return
	}

	// Remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	draft.ShippingPossible = isYes
	bulk.EditingDraftID = ""
	bulk.EditingField = ""

	shippingText := BtnNo
	if isYes {
		shippingText = BtnYes
	}
	session.reply(MsgBulkShippingSet, shippingText)
	h.sendDraftMessage(session, draft)
}

// handleGiveawaySelection sets a draft as giveaway.
func (h *BulkHandler) handleGiveawaySelection(session *UserSession, draftID string, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	draft := bulk.GetDraftByID(draftID)
	if draft == nil {
		return
	}

	// Remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	draft.TradeType = TradeTypeGive
	draft.Price = 0
	bulk.EditingDraftID = ""
	bulk.EditingField = ""

	session.reply(MsgBulkPriceGiveaway)
	h.sendDraftMessage(session, draft)
}

// handlePriceButtonSelection handles price selection from recommendation button.
func (h *BulkHandler) handlePriceButtonSelection(session *UserSession, draftID string, price int, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	draft := bulk.GetDraftByID(draftID)
	if draft == nil {
		return
	}

	// Remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	draft.TradeType = TradeTypeSell
	draft.Price = price
	bulk.EditingDraftID = ""
	bulk.EditingField = ""

	session.reply(MsgBulkPriceSet, price)
	h.sendDraftMessage(session, draft)
}

// handleDeleteCallback shows delete confirmation.
func (h *BulkHandler) handleDeleteCallback(session *UserSession, draftID string, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	draft := bulk.GetDraftByID(draftID)
	if draft == nil {
		return
	}

	msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf(MsgBulkConfirmDelete, draft.Index+1))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(BtnBulkConfirmDel, fmt.Sprintf("bulk:confirm:delete:%s", draftID)),
			tgbotapi.NewInlineKeyboardButtonData(BtnNo, "bulk:cancel:0"),
		),
	)
	msg.ReplyMarkup = keyboard
	session.replyWithMessage(msg)
}

// confirmDelete removes a draft.
func (h *BulkHandler) confirmDelete(session *UserSession, draftID string, query *tgbotapi.CallbackQuery) {
	bulk := session.bulkSession
	if bulk == nil {
		return
	}

	// Remove confirmation keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	bulk.RemoveDraft(draftID)
	session.reply(MsgBulkListingDeleted)

	// Update remaining draft messages
	for _, draft := range bulk.Drafts {
		h.sendDraftMessage(session, draft)
	}
}

// HandleBulkTextInput handles text input during bulk field editing.
func (h *BulkHandler) HandleBulkTextInput(session *UserSession, text string) bool {
	bulk := session.bulkSession
	if bulk == nil || bulk.EditingDraftID == "" {
		return false
	}

	draft := bulk.GetDraftByID(bulk.EditingDraftID)
	if draft == nil {
		bulk.EditingDraftID = ""
		bulk.EditingField = ""
		return false
	}

	switch bulk.EditingField {
	case "title":
		draft.Title = text
		session.reply(MsgBulkTitleUpdated, escapeMarkdown(text))
		h.sendDraftMessage(session, draft)

	case "desc":
		draft.Description = text
		session.reply(MsgBulkDescUpdated)
		h.sendDraftMessage(session, draft)

	case "price":
		price, err := parsePriceMessage(text)
		if err != nil {
			session.reply(MsgPriceNotUnderstood)
			return true
		}
		draft.Price = price
		draft.TradeType = TradeTypeSell
		session.reply(MsgBulkPriceSet, price)
		h.sendDraftMessage(session, draft)

	default:
		return false
	}

	bulk.EditingDraftID = ""
	bulk.EditingField = ""
	return true
}

// HandleLahetaCommand publishes drafts.
func (h *BulkHandler) HandleLahetaCommand(ctx context.Context, session *UserSession, args string) {
	bulk := session.bulkSession
	if bulk == nil || !bulk.Active {
		return
	}

	// Parse optional draft number (1-based display index)
	args = strings.TrimSpace(args)
	if args != "" {
		displayIdx, err := strconv.Atoi(args)
		if err != nil || displayIdx < 1 || displayIdx > len(bulk.Drafts) {
			session.reply(MsgBulkInvalidNumber, len(bulk.Drafts))
			return
		}
		// Get draft by display index (0-based)
		draft := bulk.GetDraft(displayIdx - 1)
		if draft == nil {
			session.reply(MsgBulkListingNotFound)
			return
		}
		// Publish single draft
		h.publishDraft(ctx, session, draft)
		return
	}

	// Publish all ready drafts
	h.publishAllDrafts(ctx, session)
}

// publishDraft publishes a single draft.
func (h *BulkHandler) publishDraft(ctx context.Context, session *UserSession, draft *BulkDraft) {
	bulk := session.bulkSession
	if draft == nil {
		session.reply(MsgBulkListingNotFound)
		return
	}

	if !draft.IsReadyToPublish() {
		session.reply(MsgBulkListingNotReady, draft.Index+1)
		return
	}

	session.reply(MsgBulkSendingSingle, draft.Index+1)

	err := h.doPublishDraft(ctx, session, draft)
	if err != nil {
		session.replyWithError(err)
		return
	}

	session.reply(MsgBulkPublishedSingle, draft.Index+1)

	// Remove published draft
	bulk.RemoveDraft(draft.ID)

	// If no more drafts, end bulk session
	if len(bulk.Drafts) == 0 {
		session.EndBulkSession()
		session.reply(MsgBulkAllSentEnded)
	} else {
		// Update remaining draft messages
		for _, d := range bulk.Drafts {
			h.sendDraftMessage(session, d)
		}
	}
}

// publishAllDrafts publishes all ready drafts.
func (h *BulkHandler) publishAllDrafts(ctx context.Context, session *UserSession) {
	bulk := session.bulkSession
	readyDrafts := bulk.GetCompleteDrafts()

	if len(readyDrafts) == 0 {
		session.reply(MsgBulkNoReadyListings)
		return
	}

	session.reply(MsgBulkSendingMultiple, len(readyDrafts))

	var published int
	var failed int

	for _, draft := range readyDrafts {
		err := h.doPublishDraft(ctx, session, draft)
		if err != nil {
			log.Error().Err(err).Str("draftID", draft.ID).Msg("failed to publish bulk draft")
			failed++
			continue
		}
		published++
		bulk.RemoveDraft(draft.ID)
	}

	if failed > 0 {
		session.reply(MsgBulkPublishedWithErrors, published, failed)
	} else {
		session.reply(MsgBulkPublishedMultiple, published)
	}

	// If no more drafts, end bulk session
	if len(bulk.Drafts) == 0 {
		session.EndBulkSession()
		session.reply(MsgBulkEnded)
	}
}

// doPublishDraft performs the actual publishing of a draft.
func (h *BulkHandler) doPublishDraft(ctx context.Context, session *UserSession, draft *BulkDraft) error {
	client := session.adInputClient
	if client == nil {
		return fmt.Errorf("no Tori client")
	}

	// Get postal code
	var postalCode string
	if h.sessionStore != nil {
		var err error
		postalCode, err = h.sessionStore.GetPostalCode(session.userId)
		if err != nil {
			log.Error().Err(err).Msg("failed to get postal code")
		}
	}
	if postalCode == "" {
		return fmt.Errorf(MsgPostalCodeMissing)
	}

	// Build payload
	adDraft := &AdInputDraft{
		CategoryID:       draft.CategoryID,
		Title:            draft.Title,
		Description:      draft.Description,
		TradeType:        draft.TradeType,
		Price:            draft.Price,
		ShippingPossible: draft.ShippingPossible,
		CollectedAttrs:   draft.CollectedAttrs,
	}

	payload := buildFinalPayload(adDraft, draft.Images, postalCode)

	// Update the ad
	_, err := client.UpdateAd(ctx, draft.DraftID, draft.ETag, payload)
	if err != nil {
		return fmt.Errorf("failed to update ad: %w", err)
	}

	// Set delivery options
	err = client.SetDeliveryOptions(ctx, draft.DraftID, tori.DeliveryOptions{
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
	_, err = client.PublishAd(ctx, draft.DraftID)
	if err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	log.Info().
		Str("title", draft.Title).
		Int("price", draft.Price).
		Int("draftIndex", draft.Index).
		Msg("bulk listing published")

	return nil
}

// HandlePeruCommand cancels bulk session.
func (h *BulkHandler) HandlePeruCommand(ctx context.Context, session *UserSession) {
	bulk := session.bulkSession
	if bulk == nil || !bulk.Active {
		return
	}

	// Cancel all ongoing analyses
	for _, draft := range bulk.Drafts {
		if draft.CancelAnalysis != nil {
			draft.CancelAnalysis()
		}
	}

	// Delete all Tori drafts that were created
	client := session.adInputClient
	if client != nil {
		for _, draft := range bulk.Drafts {
			if draft.DraftID != "" {
				if err := client.DeleteAd(ctx, draft.DraftID); err != nil {
					log.Warn().Err(err).Str("draftID", draft.DraftID).Msg("failed to delete bulk draft on cancel")
				} else {
					log.Info().Str("draftID", draft.DraftID).Msg("deleted bulk draft on cancel")
				}
			}
		}
	}

	session.EndBulkSession()
	session.replyAndRemoveCustomKeyboard(MsgBulkCancelled)
}

// setCategoryOnDraft sets the category on the Tori draft.
func (h *BulkHandler) setCategoryOnDraft(ctx context.Context, client tori.AdService, draftID, etag string, categoryID int) (string, error) {
	patchResp, err := client.PatchItem(ctx, draftID, etag, map[string]any{
		"category": categoryID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to set category: %w", err)
	}
	return patchResp.ETag, nil
}

package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/llm"
	"github.com/raine/telegram-tori-bot/storage"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

// ListingHandler handles ad creation flow for the bot.
type ListingHandler struct {
	tg             BotAPI
	visionAnalyzer llm.Analyzer
	sessionStore   storage.SessionStore
	searchClient   *tori.SearchClient
}

// NewListingHandler creates a new listing handler.
func NewListingHandler(tg BotAPI, visionAnalyzer llm.Analyzer, sessionStore storage.SessionStore) *ListingHandler {
	return &ListingHandler{
		tg:             tg,
		visionAnalyzer: visionAnalyzer,
		sessionStore:   sessionStore,
		searchClient:   tori.NewSearchClient(),
	}
}

// HandleInput handles text inputs during the listing flow (replies, attributes, prices).
// Returns true if the message was handled.
func (h *ListingHandler) HandleInput(ctx context.Context, session *UserSession, message *tgbotapi.Message) bool {
	// Handle replies to title/description messages (editing)
	if message.ReplyToMessage != nil {
		session.mu.Lock()
		handled := h.HandleTitleDescriptionReply(session, message)
		session.mu.Unlock()
		if handled {
			return true
		}
	}

	// Check current state to handle attribute/price input
	state := session.GetDraftState()

	// Handle /peru during input states
	if message.Text == "/peru" && (state == AdFlowStateAwaitingAttribute || state == AdFlowStateAwaitingPrice || state == AdFlowStateAwaitingPostalCode) {
		session.mu.Lock()
		session.reset()
		session.replyAndRemoveCustomKeyboard(okText)
		session.mu.Unlock()
		return true
	}

	// Let /osasto pass through to command handler during input states
	if message.Text == "/osasto" && (state == AdFlowStateAwaitingAttribute || state == AdFlowStateAwaitingPrice || state == AdFlowStateAwaitingPostalCode) {
		return false
	}

	// Handle attribute input
	if state == AdFlowStateAwaitingAttribute {
		session.mu.Lock()
		h.HandleAttributeInput(ctx, session, message.Text)
		session.mu.Unlock()
		return true
	}

	// Handle price input
	if state == AdFlowStateAwaitingPrice {
		session.mu.Lock()
		h.HandlePriceInput(session, message.Text)
		session.mu.Unlock()
		return true
	}

	// Handle postal code input
	if state == AdFlowStateAwaitingPostalCode {
		session.mu.Lock()
		h.HandlePostalCodeInput(session, message.Text)
		session.mu.Unlock()
		return true
	}

	return false
}

// HandleSendListingCommand handles the /laheta command.
// This function manages its own locking and returns without holding the lock.
func (h *ListingHandler) HandleSendListingCommand(ctx context.Context, session *UserSession) {
	session.mu.Lock()
	h.HandleSendListing(ctx, session)
	// HandleSendListing returns with lock held in all paths
	session.mu.Unlock()
}

// HandlePhoto processes a photo message and starts or adds to the listing flow.
func (h *ListingHandler) HandlePhoto(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	// Get the largest photo size
	largestPhoto := message.Photo[len(message.Photo)-1]

	// Check state and reserve draft creation if needed
	session.mu.Lock()

	// If another goroutine is already creating a draft (album race), wait for it
	if session.isCreatingDraft {
		session.reply("Odota hetki, luodaan ilmoitusta...")
		session.mu.Unlock()
		return
	}

	existingDraft := session.draftID != ""
	client := session.adInputClient

	if !existingDraft {
		// Reserve the right to create the draft (prevents album race)
		session.isCreatingDraft = true
		session.reply("Analysoidaan kuvaa...")
		session.initAdInputClient()
		client = session.adInputClient
	} else {
		session.reply("Lisätään kuva...")
	}
	draftID := session.draftID
	etag := session.etag
	session.mu.Unlock()

	if client == nil {
		session.mu.Lock()
		session.isCreatingDraft = false
		session.reply("Virhe: ei voitu alustaa yhteyttä")
		session.mu.Unlock()
		return
	}

	// Download the photo (NO LOCK - network I/O)
	photoData, err := downloadFileID(h.tg.GetFileDirectURL, largestPhoto.FileID)
	if err != nil {
		log.Error().Err(err).Msg("failed to download photo")
		session.mu.Lock()
		if !existingDraft {
			session.isCreatingDraft = false
		}
		session.replyWithError(err)
		session.mu.Unlock()
		return
	}

	// If this is the first photo, analyze with Gemini and create draft
	var result *llm.AnalysisResult
	var categories []tori.CategoryPrediction

	if !existingDraft {
		// Analyze with Gemini vision (NO LOCK - network I/O)
		if h.visionAnalyzer == nil {
			session.mu.Lock()
			session.isCreatingDraft = false
			session.reply("Kuva-analyysi ei ole käytettävissä")
			session.mu.Unlock()
			return
		}

		result, err = h.visionAnalyzer.AnalyzeImage(ctx, photoData, "image/jpeg")
		if err != nil {
			log.Error().Err(err).Msg("failed to analyze image")
			session.mu.Lock()
			session.isCreatingDraft = false
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}

		log.Info().
			Str("title", result.Item.Title).
			Float64("cost", result.Usage.CostUSD).
			Msg("image analyzed")

		// Create draft ad (NO LOCK - network I/O)
		draftID, etag, err = h.startNewAdFlow(ctx, client)
		if err != nil {
			session.mu.Lock()
			session.isCreatingDraft = false
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}

		// Set image on draft (NO LOCK - network I/O)
		allImages := []UploadedImage{{
			ImagePath: "", // Will be set after upload
			Width:     largestPhoto.Width,
			Height:    largestPhoto.Height,
		}}

		// Upload photo to draft (NO LOCK - network I/O)
		uploaded, err := h.uploadPhotoToAd(ctx, client, draftID, photoData, largestPhoto.Width, largestPhoto.Height)
		if err != nil {
			session.mu.Lock()
			session.isCreatingDraft = false
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}
		allImages[0] = *uploaded

		// Set image on draft (NO LOCK - network I/O)
		newEtag, err := h.setImageOnDraft(ctx, client, draftID, etag, allImages)
		if err != nil {
			session.mu.Lock()
			session.isCreatingDraft = false
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}
		etag = newEtag

		// Get category predictions (NO LOCK - network I/O)
		categories, err = h.getCategoryPredictions(ctx, client, draftID)
		if err != nil {
			log.Warn().Err(err).Msg("failed to get category predictions")
			categories = []tori.CategoryPrediction{}
		}

		// Now update session state (LOCK)
		session.mu.Lock()
		session.isCreatingDraft = false
		session.photos = append(session.photos, largestPhoto)
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
			// Try LLM-based category selection
			autoSelectedID := h.tryAutoSelectCategory(ctx, result.Item.Title, result.Item.Description, categories)
			if autoSelectedID > 0 {
				// Release lock and process category selection (which acquires its own lock)
				session.mu.Unlock()
				h.ProcessCategorySelection(ctx, session, autoSelectedID)
				return
			}

			// Fall back to manual selection
			msg := tgbotapi.NewMessage(session.userId, "Valitse osasto")
			msg.ParseMode = tgbotapi.ModeMarkdown
			msg.ReplyMarkup = makeCategoryPredictionKeyboard(categories)
			session.replyWithMessage(msg)
		} else {
			// No categories predicted, use default
			session.currentDraft.CategoryID = 76 // "Muu" category
			session.reply("Ei osastoehdotuksia, käytetään oletusta.")
			// promptForPrice releases and re-acquires lock internally
			h.promptForPrice(ctx, session)
			// Lock is held after promptForPrice returns
		}
		session.mu.Unlock()
	} else {
		// Adding to existing draft - upload photo (NO LOCK - network I/O)
		uploaded, err := h.uploadPhotoToAd(ctx, client, draftID, photoData, largestPhoto.Width, largestPhoto.Height)
		if err != nil {
			session.mu.Lock()
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}

		// Update session and set images on draft
		session.mu.Lock()
		if session.currentDraft == nil {
			// Draft was cancelled during upload
			log.Info().Int64("userId", session.userId).Msg("draft cancelled during photo upload")
			session.mu.Unlock()
			return
		}
		session.photos = append(session.photos, largestPhoto)
		session.currentDraft.Images = append(session.currentDraft.Images, *uploaded)
		images := make([]UploadedImage, len(session.currentDraft.Images))
		copy(images, session.currentDraft.Images)
		currentEtag := session.etag
		session.mu.Unlock()

		// Update images on draft (NO LOCK - network I/O)
		newEtag, err := h.setImageOnDraft(ctx, client, draftID, currentEtag, images)
		if err != nil {
			session.mu.Lock()
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}

		session.mu.Lock()
		if session.currentDraft == nil {
			// Draft was cancelled during image update
			log.Info().Int64("userId", session.userId).Msg("draft cancelled during image update")
			session.mu.Unlock()
			return
		}
		session.etag = newEtag
		session.reply(fmt.Sprintf("Kuva lisätty! Kuvia yhteensä: %d", len(session.photos)))
		session.mu.Unlock()
	}
}

// HandleCategorySelection processes category selection from callback query.
func (h *ListingHandler) HandleCategorySelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	categoryIDStr := strings.TrimPrefix(query.Data, "cat:")
	categoryID, err := strconv.Atoi(categoryIDStr)
	if err != nil {
		log.Error().Err(err).Str("data", query.Data).Msg("invalid category callback data")
		return
	}

	session.mu.Lock()
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingCategory {
		session.reply("Ei aktiivista ilmoitusta")
		session.mu.Unlock()
		return
	}

	// Edit the original message to remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}
	session.mu.Unlock()

	h.ProcessCategorySelection(ctx, session, categoryID)
}

// ProcessCategorySelection handles the common category selection logic.
// It sets category, fetches attributes, and prompts for next step.
func (h *ListingHandler) ProcessCategorySelection(ctx context.Context, session *UserSession, categoryID int) {
	session.mu.Lock()

	if session.currentDraft == nil {
		session.mu.Unlock()
		return
	}

	// Find category label for logging
	var categoryLabel string
	for _, cat := range session.currentDraft.CategoryPredictions {
		if cat.ID == categoryID {
			categoryLabel = cat.Label
			break
		}
	}

	session.currentDraft.CategoryID = categoryID
	log.Info().Int("categoryId", categoryID).Str("label", categoryLabel).Msg("category selected")

	session.reply(fmt.Sprintf("Osasto: *%s*", categoryLabel))

	// Get client and draft info for network calls
	client := session.adInputClient
	draftID := session.draftID
	etag := session.etag
	session.mu.Unlock()

	// Set category on draft (NO LOCK - network I/O)
	newEtag, err := h.setCategoryOnDraft(ctx, client, draftID, etag, categoryID)
	if err != nil {
		session.mu.Lock()
		session.replyWithError(err)
		session.mu.Unlock()
		return
	}

	// Fetch attributes for this category (NO LOCK - network I/O)
	attrs, err := client.GetAttributes(ctx, draftID)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get attributes, skipping to price")
		session.mu.Lock()
		if session.currentDraft == nil {
			// Draft was cancelled during attribute fetch
			log.Info().Int64("userId", session.userId).Msg("draft cancelled during attribute fetch (error path)")
			session.mu.Unlock()
			return
		}
		session.etag = newEtag
		session.currentDraft.State = AdFlowStateAwaitingPrice
		session.reply("Syötä hinta (esim. 50€)")
		session.mu.Unlock()
		return
	}

	// Re-acquire lock to update session
	session.mu.Lock()
	defer session.mu.Unlock()

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

	if len(requiredAttrs) > 0 {
		session.currentDraft.RequiredAttrs = requiredAttrs
		session.currentDraft.CurrentAttrIndex = 0
		session.currentDraft.State = AdFlowStateAwaitingAttribute
		h.promptForAttribute(session, requiredAttrs[0])
	} else {
		h.promptForPrice(ctx, session)
	}
}

// HandleAttributeInput handles user selection of an attribute value.
// Caller must hold session.mu.Lock().
func (h *ListingHandler) HandleAttributeInput(ctx context.Context, session *UserSession, text string) {
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingAttribute {
		return
	}

	attrs := session.currentDraft.RequiredAttrs
	idx := session.currentDraft.CurrentAttrIndex

	if idx >= len(attrs) {
		// Shouldn't happen, but handle gracefully
		h.promptForPrice(ctx, session)
		return
	}

	currentAttr := attrs[idx]

	// Find the selected option by label
	opt := tori.FindOptionByLabel(&currentAttr, text)
	if opt == nil {
		// Invalid selection, prompt again
		session.reply(fmt.Sprintf("Valitse jokin vaihtoehdoista: %s", strings.ToLower(currentAttr.Label)))
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
		h.promptForPrice(ctx, session)
	}
}

// HandlePriceInput handles price input when awaiting price.
// Caller must hold session.mu.Lock().
func (h *ListingHandler) HandlePriceInput(session *UserSession, text string) {
	// Parse price from text
	price, err := parsePriceMessage(text)
	if err != nil {
		session.reply("En ymmärtänyt hintaa. Syötä hinta numerona (esim. 50€ tai 50)")
		return
	}

	session.currentDraft.Price = price
	session.currentDraft.State = AdFlowStateAwaitingShipping

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
// Caller must hold session.mu.Lock().
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

// HandleShippingSelection handles the shipping yes/no callback.
func (h *ListingHandler) HandleShippingSelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	isYes := strings.HasSuffix(query.Data, ":yes")

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingShipping {
		return
	}

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
			if strings.TrimSpace(expanded) != "" {
				session.currentDraft.Description += "\n\n" + expanded

				// Update the description message in chat
				if session.currentDraft.DescriptionMessageID != 0 {
					editMsg := tgbotapi.NewEditMessageText(
						session.userId,
						session.currentDraft.DescriptionMessageID,
						fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(session.currentDraft.Description)),
					)
					editMsg.ParseMode = tgbotapi.ModeMarkdown
					h.tg.Request(editMsg)
				}
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

// HandleSendListing sends the listing using the adinput API.
// Caller must hold session.mu.Lock(). Function returns with lock held.
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

	// Release lock for network I/O
	session.mu.Unlock()

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
		session.mu.Lock()
		session.currentDraft.State = AdFlowStateAwaitingPostalCode
		session.reply(postalCodePromptText)
		return
	}

	// Set category on draft
	newEtag, err := h.setCategoryOnDraft(ctx, client, draftID, etag, draftCopy.CategoryID)
	if err != nil {
		session.mu.Lock()
		// Note: Even if draft was cancelled, we still report the error
		session.replyWithError(err)
		return
	}
	etag = newEtag

	// Update and publish
	if err := h.updateAndPublishAd(ctx, client, draftID, etag, &draftCopy, images, postalCode); err != nil {
		session.mu.Lock()
		// Note: Even if draft was cancelled, we still report the error
		session.replyWithError(err)
		return
	}

	// Re-acquire lock for final state update
	session.mu.Lock()
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
// Caller must hold session.mu.Lock().
func (h *ListingHandler) HandleTitleDescriptionReply(session *UserSession, message *tgbotapi.Message) bool {
	draft := session.currentDraft
	if draft == nil {
		return false
	}

	replyToID := message.ReplyToMessage.MessageID
	if draft.TitleMessageID == replyToID {
		draft.Title = message.Text
		// Edit the original message to show updated title
		editMsg := tgbotapi.NewEditMessageText(session.userId, replyToID, fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(draft.Title)))
		editMsg.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(editMsg)
		session.reply(fmt.Sprintf("✅ Otsikko päivitetty: %s", escapeMarkdown(message.Text)))
		return true
	}
	if draft.DescriptionMessageID == replyToID {
		draft.Description = message.Text
		// Edit the original message to show updated description
		editMsg := tgbotapi.NewEditMessageText(session.userId, replyToID, fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(draft.Description)))
		editMsg.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(editMsg)
		session.reply("✅ Kuvaus päivitetty")
		return true
	}

	return false
}

// --- Helper methods ---

// tryAutoSelectCategory attempts to auto-select category using LLM.
// Returns the selected category ID or 0 if auto-selection failed.
func (h *ListingHandler) tryAutoSelectCategory(ctx context.Context, title, description string, predictions []tori.CategoryPrediction) int {
	// Check if the analyzer supports category selection
	gemini, ok := h.visionAnalyzer.(*llm.GeminiAnalyzer)
	if !ok {
		return 0
	}

	categoryID, err := gemini.SelectCategory(ctx, title, description, predictions)
	if err != nil {
		log.Warn().Err(err).Msg("LLM category selection failed, falling back to manual")
		return 0
	}

	return categoryID
}

// Attributes that should never be auto-selected (require user input)
var manualOnlyAttributes = map[string]bool{
	"condition": true, // Condition is subjective, user must choose
}

// tryAutoSelectAttributes attempts to auto-select attributes using LLM.
// Returns the list of attributes that still need manual selection.
// Caller must hold session.mu.Lock().
func (h *ListingHandler) tryAutoSelectAttributes(ctx context.Context, session *UserSession, attrs []tori.Attribute) []tori.Attribute {
	gemini, ok := h.visionAnalyzer.(*llm.GeminiAnalyzer)
	if !ok {
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

// promptForAttribute shows a keyboard to select an attribute value.
func (h *ListingHandler) promptForAttribute(session *UserSession, attr tori.Attribute) {
	msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("Valitse %s", strings.ToLower(attr.Label)))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = makeAttributeKeyboard(attr)
	session.replyWithMessage(msg)
}

// promptForPrice fetches price recommendations and prompts the user.
// Caller must hold session.mu.Lock().
func (h *ListingHandler) promptForPrice(ctx context.Context, session *UserSession) {
	session.currentDraft.State = AdFlowStateAwaitingPrice
	title := session.currentDraft.Title

	// Release lock for network search
	session.mu.Unlock()

	// Search for similar items
	results, err := h.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query: title,
		Rows:  20,
	})

	// Collect prices from results
	var prices []int
	if err == nil && results != nil {
		for _, doc := range results.Docs {
			if doc.Price != nil && doc.Price.Amount > 0 {
				prices = append(prices, doc.Price.Amount)
			}
		}
	} else if err != nil {
		log.Warn().Err(err).Msg("price search failed")
	}

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

	// Re-acquire lock
	session.mu.Lock()

	// Check if state is still valid (user may have cancelled during search)
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingPrice {
		return
	}

	msgText := fmt.Sprintf("Syötä hinta (esim. 50€)%s", recommendationMsg)
	msg := tgbotapi.NewMessage(session.userId, msgText)
	msg.ParseMode = tgbotapi.ModeMarkdown

	// Add suggestion button if we have a recommendation
	if recommendedPrice > 0 {
		keyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton(fmt.Sprintf("%d€", recommendedPrice)),
			),
		)
		keyboard.OneTimeKeyboard = true
		keyboard.ResizeKeyboard = true
		msg.ReplyMarkup = keyboard
	}

	session.replyWithMessage(msg)
}

// showAdSummary displays the final ad summary before publishing.
func (h *ListingHandler) showAdSummary(session *UserSession) {
	session.currentDraft.State = AdFlowStateReadyToPublish

	shippingText := "Ei"
	if session.currentDraft.ShippingPossible {
		shippingText = "Kyllä"
	}

	msg := fmt.Sprintf(`*Ilmoitus valmis:*
📦 *Otsikko:* %s
📝 *Kuvaus:* %s
💰 *Hinta:* %d€
🚚 *Postitus:* %s
📷 *Kuvia:* %d

Lähetä /laheta julkaistaksesi tai /peru peruuttaaksesi.`,
		escapeMarkdown(session.currentDraft.Title),
		escapeMarkdown(session.currentDraft.Description),
		session.currentDraft.Price,
		shippingText,
		len(session.photos),
	)

	session.reply(msg)
}

// startNewAdFlow creates a draft and returns the ID and ETag.
func (h *ListingHandler) startNewAdFlow(ctx context.Context, client *tori.AdinputClient) (draftID string, etag string, err error) {
	log.Info().Msg("creating draft ad")
	draft, err := client.CreateDraftAd(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to create draft: %w", err)
	}

	log.Info().Str("draftId", draft.ID).Msg("draft ad created")
	return draft.ID, draft.ETag, nil
}

// uploadPhotoToAd uploads a photo to the draft ad.
func (h *ListingHandler) uploadPhotoToAd(ctx context.Context, client *tori.AdinputClient, draftID string, photoData []byte, width, height int) (*UploadedImage, error) {
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
func (h *ListingHandler) setImageOnDraft(ctx context.Context, client *tori.AdinputClient, draftID, etag string, images []UploadedImage) (string, error) {
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
func (h *ListingHandler) getCategoryPredictions(ctx context.Context, client *tori.AdinputClient, draftID string) ([]tori.CategoryPrediction, error) {
	if draftID == "" {
		return nil, fmt.Errorf("no draft ad")
	}

	return client.GetCategoryPredictions(ctx, draftID)
}

// setCategoryOnDraft sets the category on the draft and returns new ETag.
func (h *ListingHandler) setCategoryOnDraft(ctx context.Context, client *tori.AdinputClient, draftID, etag string, categoryID int) (string, error) {
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
	client *tori.AdinputClient,
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

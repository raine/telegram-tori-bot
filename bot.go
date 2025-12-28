package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/storage"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/raine/telegram-tori-bot/tori/auth"
	"github.com/raine/telegram-tori-bot/vision"
	"github.com/rs/zerolog/log"
)

type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	GetFileDirectURL(fileID string) (string, error)
}

type Bot struct {
	tg             BotAPI
	state          BotState
	toriApiBaseUrl string
	sessionStore   storage.SessionStore
	visionAnalyzer vision.Analyzer
}

func NewBot(tg BotAPI, toriApiBaseUrl string, sessionStore storage.SessionStore) *Bot {
	bot := &Bot{
		tg:             tg,
		toriApiBaseUrl: toriApiBaseUrl,
		sessionStore:   sessionStore,
	}

	bot.state = bot.NewBotState()
	return bot
}

// SetVisionAnalyzer sets the vision analyzer for image analysis
func (b *Bot) SetVisionAnalyzer(analyzer vision.Analyzer) {
	b.visionAnalyzer = analyzer
}

// handlePhoto processes a photo message and starts or adds to the listing flow
func (b *Bot) handlePhoto(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	// Get the largest photo size (no lock needed for read)
	largestPhoto := message.Photo[len(message.Photo)-1]

	// Check if we have an existing draft (brief lock)
	session.mu.Lock()
	existingDraft := session.draftID != ""
	client := session.adInputClient
	draftID := session.draftID
	etag := session.etag

	if !existingDraft {
		session.reply("Analysoidaan kuvaa...")
		session.initAdInputClient()
		client = session.adInputClient
	} else {
		session.reply("Lisätään kuva...")
	}
	session.mu.Unlock()

	if client == nil {
		session.mu.Lock()
		session.reply("Virhe: ei voitu alustaa yhteyttä")
		session.mu.Unlock()
		return
	}

	// Download the photo (NO LOCK - network I/O)
	photoData, err := downloadFileID(b.tg.GetFileDirectURL, largestPhoto.FileID)
	if err != nil {
		log.Error().Err(err).Msg("failed to download photo")
		session.mu.Lock()
		session.replyWithError(err)
		session.mu.Unlock()
		return
	}

	// If this is the first photo, analyze with Gemini and create draft
	var result *vision.AnalysisResult
	var categories []tori.CategoryPrediction

	if !existingDraft {
		// Analyze with Gemini vision (NO LOCK - network I/O)
		if b.visionAnalyzer == nil {
			session.mu.Lock()
			session.reply("Kuva-analyysi ei ole käytettävissä")
			session.mu.Unlock()
			return
		}

		result, err = b.visionAnalyzer.AnalyzeImage(ctx, photoData, "image/jpeg")
		if err != nil {
			log.Error().Err(err).Msg("failed to analyze image")
			session.mu.Lock()
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}

		log.Info().
			Str("title", result.Item.Title).
			Float64("cost", result.Usage.CostUSD).
			Msg("image analyzed")

		// Create draft ad (NO LOCK - network I/O)
		draftID, etag, err = b.startNewAdFlow(ctx, client)
		if err != nil {
			session.mu.Lock()
			session.replyWithError(err)
			session.mu.Unlock()
			return
		}
	}

	// Upload photo to draft (NO LOCK - network I/O)
	uploaded, err := b.uploadPhotoToAd(ctx, client, draftID, photoData, largestPhoto.Width, largestPhoto.Height)
	if err != nil {
		session.mu.Lock()
		session.replyWithError(err)
		session.mu.Unlock()
		return
	}

	// Now update session state (LOCK)
	session.mu.Lock()
	defer session.mu.Unlock()

	// Add photo to session
	session.photos = append(session.photos, largestPhoto)

	if !existingDraft {
		// Update session with draft info
		session.draftID = draftID
		session.etag = etag

		// Set image on draft
		allImages := []UploadedImage{*uploaded}
		newEtag, err := b.setImageOnDraft(ctx, client, draftID, etag, allImages)
		if err != nil {
			session.replyWithError(err)
			return
		}
		session.etag = newEtag

		// Get category predictions
		categories, err = b.getCategoryPredictions(ctx, client, draftID)
		if err != nil {
			log.Warn().Err(err).Msg("failed to get category predictions")
			categories = []tori.CategoryPrediction{}
		}

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

		// Send category selection
		if len(categories) > 0 {
			msg := tgbotapi.NewMessage(session.userId, "Valitse osasto")
			msg.ParseMode = tgbotapi.ModeMarkdown
			msg.ReplyMarkup = makeCategoryPredictionKeyboard(categories)
			session.replyWithMessage(msg)
		} else {
			// No categories predicted, use default
			session.currentDraft.CategoryID = 76 // "Muu" category
			session.currentDraft.State = AdFlowStateAwaitingPrice
			session.reply("Ei osastoehdotuksia, käytetään oletusta.\n\nSyötä hinta (esim. 50€)")
		}
	} else {
		// Adding to existing draft
		session.currentDraft.Images = append(session.currentDraft.Images, *uploaded)

		// Update images on draft
		newEtag, err := b.setImageOnDraft(ctx, client, draftID, session.etag, session.currentDraft.Images)
		if err != nil {
			session.replyWithError(err)
			return
		}
		session.etag = newEtag

		session.reply(fmt.Sprintf("Kuva lisätty! Kuvia yhteensä: %d", len(session.photos)))
	}
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	// Handle callback queries (inline keyboard button presses)
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	userId := update.Message.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	log.Info().Str("text", update.Message.Text).Str("caption", update.Message.Caption).Msg("got message")

	// Handle auth flow (needs lock)
	session.mu.Lock()
	if session.authFlow != nil && session.authFlow.IsTimedOut() {
		session.authFlow.Reset()
		session.reply(loginTimeoutText)
	}

	if session.authFlow != nil && session.authFlow.IsActive() {
		b.handleAuthFlowMessage(ctx, session, update.Message.Text)
		session.mu.Unlock()
		return
	}
	session.mu.Unlock()

	// Handle photo messages (handlePhoto manages its own locking)
	if len(update.Message.Photo) > 0 {
		session.mu.Lock()
		if session.client == nil {
			session.reply(loginRequiredText)
			session.mu.Unlock()
			return
		}
		session.mu.Unlock()
		b.handlePhoto(ctx, session, update.Message)
		return
	}

	// Handle replies to title/description messages (editing)
	if update.Message.ReplyToMessage != nil {
		session.mu.Lock()
		draft := session.currentDraft
		if draft != nil {
			replyToID := update.Message.ReplyToMessage.MessageID
			if draft.TitleMessageID == replyToID {
				draft.Title = update.Message.Text
				// Edit the original message to show updated title
				editMsg := tgbotapi.NewEditMessageText(session.userId, replyToID, fmt.Sprintf("📦 *Otsikko:* %s", escapeMarkdown(draft.Title)))
				editMsg.ParseMode = tgbotapi.ModeMarkdown
				b.tg.Request(editMsg)
				session.reply(fmt.Sprintf("✅ Otsikko päivitetty: %s", escapeMarkdown(update.Message.Text)))
				session.mu.Unlock()
				return
			}
			if draft.DescriptionMessageID == replyToID {
				draft.Description = update.Message.Text
				// Edit the original message to show updated description
				editMsg := tgbotapi.NewEditMessageText(session.userId, replyToID, fmt.Sprintf("📝 *Kuvaus:* %s", escapeMarkdown(draft.Description)))
				editMsg.ParseMode = tgbotapi.ModeMarkdown
				b.tg.Request(editMsg)
				session.reply("✅ Kuvaus päivitetty")
				session.mu.Unlock()
				return
			}
		}
		session.mu.Unlock()
	}

	// Handle attribute input if awaiting attribute
	session.mu.Lock()
	if session.currentDraft != nil && session.currentDraft.State == AdFlowStateAwaitingAttribute {
		b.handleAttributeInput(session, update.Message.Text)
		session.mu.Unlock()
		return
	}
	session.mu.Unlock()

	// Handle price input if awaiting price
	session.mu.Lock()
	if session.currentDraft != nil && session.currentDraft.State == AdFlowStateAwaitingPrice {
		b.handlePriceInput(session, update.Message.Text)
		session.mu.Unlock()
		return
	}
	session.mu.Unlock()

	// Handle commands
	session.mu.Lock()
	defer session.mu.Unlock()

	command, _ := parseCommand(update.Message.Text)
	switch command {
	case "/start":
		if session.client == nil {
			session.reply(loginRequiredText)
		} else {
			session.reply(startText)
		}
	case "/login":
		b.handleLoginCommand(session)
	case "/peru":
		session.reset()
		session.replyAndRemoveCustomKeyboard(okText)
	case "/laheta":
		b.handleSendListing(ctx, session)
	case "/poistakuvat":
		session.photos = nil
		session.pendingPhotos = nil
		session.currentDraft = nil
		session.reply(photosRemoved)
	default:
		if session.client == nil {
			session.reply(loginRequiredText)
			return
		}
		// For now, just tell user to send a photo
		session.reply("Lähetä kuva aloittaaksesi ilmoituksen teon")
	}
}

// handleCallbackQuery handles inline keyboard button presses
func (b *Bot) handleCallbackQuery(ctx context.Context, query *tgbotapi.CallbackQuery) {
	userId := query.From.ID
	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	// Answer the callback to remove the loading state
	callback := tgbotapi.NewCallback(query.ID, "")
	b.tg.Request(callback)

	// Handle category selection
	if strings.HasPrefix(query.Data, "cat:") {
		b.handleCategorySelection(ctx, session, query)
	}
}

// handleCategorySelection processes category selection and fetches attributes
func (b *Bot) handleCategorySelection(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
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

	// Edit the original message to remove keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		b.tg.Request(edit)
	}

	session.reply(fmt.Sprintf("Osasto: *%s*", categoryLabel))

	// Get client and draft info for network calls
	client := session.adInputClient
	draftID := session.draftID
	etag := session.etag
	session.mu.Unlock()

	// Set category on draft (NO LOCK - network I/O)
	newEtag, err := b.setCategoryOnDraft(ctx, client, draftID, etag, categoryID)
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
		session.etag = newEtag
		session.currentDraft.State = AdFlowStateAwaitingPrice
		session.reply("Syötä hinta (esim. 50€)")
		session.mu.Unlock()
		return
	}

	// Re-acquire lock to update session
	session.mu.Lock()
	defer session.mu.Unlock()

	session.etag = newEtag
	session.adAttributes = attrs

	// Get required SELECT attributes
	requiredAttrs := tori.GetRequiredSelectAttributes(attrs)

	if len(requiredAttrs) > 0 {
		session.currentDraft.RequiredAttrs = requiredAttrs
		session.currentDraft.CurrentAttrIndex = 0
		session.currentDraft.State = AdFlowStateAwaitingAttribute
		b.promptForAttribute(session, requiredAttrs[0])
	} else {
		session.currentDraft.State = AdFlowStateAwaitingPrice
		session.reply("Syötä hinta (esim. 50€)")
	}
}

// promptForAttribute shows a keyboard to select an attribute value
func (b *Bot) promptForAttribute(session *UserSession, attr tori.Attribute) {
	msg := tgbotapi.NewMessage(session.userId, fmt.Sprintf("Valitse %s", strings.ToLower(attr.Label)))
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = makeAttributeKeyboard(attr)
	session.replyWithMessage(msg)
}

// handleAttributeInput handles user selection of an attribute value
func (b *Bot) handleAttributeInput(session *UserSession, text string) {
	if session.currentDraft == nil || session.currentDraft.State != AdFlowStateAwaitingAttribute {
		return
	}

	attrs := session.currentDraft.RequiredAttrs
	idx := session.currentDraft.CurrentAttrIndex

	if idx >= len(attrs) {
		// Shouldn't happen, but handle gracefully
		session.currentDraft.State = AdFlowStateAwaitingPrice
		session.reply("Syötä hinta (esim. 50€)")
		return
	}

	currentAttr := attrs[idx]

	// Find the selected option by label
	opt := tori.FindOptionByLabel(&currentAttr, text)
	if opt == nil {
		// Invalid selection, prompt again
		session.reply(fmt.Sprintf("Valitse jokin vaihtoehdoista: %s", strings.ToLower(currentAttr.Label)))
		b.promptForAttribute(session, currentAttr)
		return
	}

	// Store the selected value
	session.currentDraft.CollectedAttrs[currentAttr.Name] = strconv.Itoa(opt.ID)
	log.Info().Str("attr", currentAttr.Name).Str("label", text).Int("optionId", opt.ID).Msg("attribute selected")

	// Move to next attribute or price input
	session.currentDraft.CurrentAttrIndex++
	if session.currentDraft.CurrentAttrIndex < len(attrs) {
		nextAttr := attrs[session.currentDraft.CurrentAttrIndex]
		b.promptForAttribute(session, nextAttr)
	} else {
		session.currentDraft.State = AdFlowStateAwaitingPrice
		session.replyAndRemoveCustomKeyboard("Syötä hinta (esim. 50€)")
	}
}

// handlePriceInput handles price input when awaiting price
func (b *Bot) handlePriceInput(session *UserSession, text string) {
	// Parse price from text
	price, err := parsePriceMessage(text)
	if err != nil {
		session.reply("En ymmärtänyt hintaa. Syötä hinta numerona (esim. 50€ tai 50)")
		return
	}

	session.currentDraft.Price = price
	session.currentDraft.State = AdFlowStateReadyToPublish

	// Show summary
	msg := fmt.Sprintf(`*Ilmoitus valmis:*
📦 *Otsikko:* %s
📝 *Kuvaus:* %s
💰 *Hinta:* %d€
📷 *Kuvia:* %d

Lähetä /laheta julkaistaksesi tai /peru peruuttaaksesi.`,
		escapeMarkdown(session.currentDraft.Title),
		escapeMarkdown(session.currentDraft.Description),
		session.currentDraft.Price,
		len(session.photos),
	)

	session.reply(msg)
}

var priceRegex = regexp.MustCompile(`(\d+)`)

func parsePriceMessage(text string) (int, error) {
	m := priceRegex.FindStringSubmatch(text)
	if m == nil {
		return 0, fmt.Errorf("no price found")
	}
	return strconv.Atoi(m[1])
}

// handleSendListing sends the listing using the new adinput API
func (b *Bot) handleSendListing(ctx context.Context, session *UserSession) {
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

	session.reply("Lähetetään ilmoitusta...")

	// Release lock for network I/O
	session.mu.Unlock()

	// Set category on draft
	newEtag, err := b.setCategoryOnDraft(ctx, client, draftID, etag, draftCopy.CategoryID)
	if err != nil {
		session.mu.Lock()
		session.replyWithError(err)
		return
	}
	etag = newEtag

	// TODO: get postal code from user profile or session
	postalCode := "00420"

	// Update and publish
	if err := b.updateAndPublishAd(ctx, client, draftID, etag, &draftCopy, images, postalCode); err != nil {
		session.mu.Lock()
		session.replyWithError(err)
		return
	}

	// Re-acquire lock for final state update
	session.mu.Lock()
	session.replyAndRemoveCustomKeyboard(listingSentText)
	log.Info().Str("title", draftCopy.Title).Int("price", draftCopy.Price).Msg("listing published")
	session.reset()
}

// handleLoginCommand starts the login flow
func (b *Bot) handleLoginCommand(session *UserSession) {
	// Check if already logged in
	if session.client != nil {
		session.reply(loginAlreadyLoggedInText)
		return
	}

	// Initialize auth flow
	authenticator, err := auth.NewAuthenticator()
	if err != nil {
		session.reply(loginFailedText, err)
		return
	}

	if err := authenticator.InitSession(); err != nil {
		session.reply(loginFailedText, err)
		return
	}

	session.authFlow.Authenticator = authenticator
	session.authFlow.State = AuthStateAwaitingEmail
	session.authFlow.Touch()

	session.reply(loginPromptEmailText)
}

// handleAuthFlowMessage handles messages during the login flow
func (b *Bot) handleAuthFlowMessage(ctx context.Context, session *UserSession, text string) {
	// Handle /peru to cancel login
	if text == "/peru" {
		session.authFlow.Reset()
		session.reply(loginCancelledText)
		return
	}

	// Reject other commands during auth flow
	if strings.HasPrefix(text, "/") {
		session.reply(loginInProgressText)
		return
	}

	session.authFlow.Touch()

	switch session.authFlow.State {
	case AuthStateAwaitingEmail:
		b.handleAuthEmail(ctx, session, text)
	case AuthStateAwaitingEmailCode:
		b.handleAuthEmailCode(ctx, session, text)
	case AuthStateAwaitingSMSCode:
		b.handleAuthSMSCode(ctx, session, text)
	}
}

func (b *Bot) handleAuthEmail(ctx context.Context, session *UserSession, email string) {
	session.authFlow.Email = email

	if err := session.authFlow.Authenticator.StartLogin(email); err != nil {
		log.Error().Err(err).Msg("login start failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	session.authFlow.State = AuthStateAwaitingEmailCode
	session.reply(loginEmailCodeSentText)
}

func (b *Bot) handleAuthEmailCode(ctx context.Context, session *UserSession, code string) {
	mfaRequired, err := session.authFlow.Authenticator.SubmitEmailCode(code)
	if err != nil {
		log.Error().Err(err).Msg("email code submission failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	if mfaRequired {
		session.authFlow.MFARequired = true
		if err := session.authFlow.Authenticator.RequestSMS(); err != nil {
			log.Error().Err(err).Msg("SMS request failed")
			session.reply(loginFailedText, err)
			session.authFlow.Reset()
			return
		}
		session.authFlow.State = AuthStateAwaitingSMSCode
		session.reply(loginSMSCodeSentText)
		return
	}

	// No MFA required, finalize
	b.finalizeAuth(ctx, session)
}

func (b *Bot) handleAuthSMSCode(ctx context.Context, session *UserSession, code string) {
	if err := session.authFlow.Authenticator.SubmitSMSCode(code); err != nil {
		log.Error().Err(err).Msg("SMS code submission failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	b.finalizeAuth(ctx, session)
}

func (b *Bot) finalizeAuth(ctx context.Context, session *UserSession) {
	tokens, err := session.authFlow.Authenticator.Finalize()
	if err != nil {
		log.Error().Err(err).Msg("auth finalization failed")
		session.reply(loginFailedText, err)
		session.authFlow.Reset()
		return
	}

	// Save session to store
	if b.sessionStore != nil {
		storedSession := &storage.StoredSession{
			TelegramID: session.userId,
			ToriUserID: tokens.UserID,
			Tokens:     *tokens,
		}
		if err := b.sessionStore.Save(storedSession); err != nil {
			log.Error().Err(err).Msg("failed to save session")
			session.reply(loginFailedText, err)
			session.authFlow.Reset()
			return
		}
	}

	// Update session with new client
	session.toriAccountId = tokens.UserID
	session.refreshToken = tokens.RefreshToken
	session.deviceID = tokens.DeviceID
	session.client = tori.NewClient(tori.ClientOpts{
		Auth:    "Bearer " + tokens.BearerToken,
		BaseURL: b.toriApiBaseUrl,
	})

	session.authFlow.Reset()
	session.reply(loginSuccessText)
	log.Info().Int64("userId", session.userId).Str("toriUserId", tokens.UserID).Msg("user logged in successfully")
}

// tryRefreshTokens attempts to refresh the session's tokens using the stored refresh token.
func (b *Bot) tryRefreshTokens(session *UserSession) error {
	if session.refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}
	if session.deviceID == "" {
		return fmt.Errorf("no device ID available")
	}

	log.Info().Int64("userId", session.userId).Msg("attempting token refresh")

	newTokens, err := auth.RefreshTokens(session.refreshToken, session.deviceID)
	if err != nil {
		return fmt.Errorf("refresh failed: %w", err)
	}

	// Update session with new tokens
	session.refreshToken = newTokens.RefreshToken
	session.client = tori.NewClient(tori.ClientOpts{
		Auth:    "Bearer " + newTokens.BearerToken,
		BaseURL: b.toriApiBaseUrl,
	})

	// Persist new tokens to storage
	if b.sessionStore != nil {
		storedSession := &storage.StoredSession{
			TelegramID: session.userId,
			ToriUserID: newTokens.UserID,
			Tokens:     *newTokens,
		}
		if err := b.sessionStore.Save(storedSession); err != nil {
			log.Warn().Err(err).Msg("failed to persist refreshed tokens")
		}
	}

	log.Info().Int64("userId", session.userId).Msg("token refresh successful")
	return nil
}

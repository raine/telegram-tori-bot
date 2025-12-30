package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/llm"
	"github.com/raine/telegram-tori-bot/storage"
	"github.com/rs/zerolog/log"
)

// BotAPI defines the interface for Telegram bot API operations.
type BotAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	GetFileDirectURL(fileID string) (string, error)
}

// Bot is the main Telegram bot handler.
type Bot struct {
	tg             BotAPI
	state          BotState
	sessionStore   storage.SessionStore
	visionAnalyzer llm.Analyzer

	// Handlers
	authHandler    *AuthHandler
	listingHandler *ListingHandler
	bulkHandler    *BulkHandler
}

// NewBot creates a new Bot instance.
func NewBot(tg BotAPI, sessionStore storage.SessionStore) *Bot {
	bot := &Bot{
		tg:           tg,
		sessionStore: sessionStore,
	}

	bot.state = bot.NewBotState()
	bot.authHandler = NewAuthHandler(sessionStore)

	return bot
}

// SetLLMClients sets the LLM clients for vision and text analysis.
// visionAnalyzer: handles image analysis (can be cached)
// editParser: handles natural language editing (direct LLM access, not cached)
func (b *Bot) SetLLMClients(visionAnalyzer llm.Analyzer, editParser llm.EditIntentParser) {
	b.visionAnalyzer = visionAnalyzer
	b.listingHandler = NewListingHandler(b.tg, visionAnalyzer, editParser, b.sessionStore)
	b.bulkHandler = NewBulkHandler(b.tg, visionAnalyzer, b.sessionStore)
}

// handleUpdate is the main message router.
// It dispatches messages to the appropriate session worker for sequential processing.
func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	b.dispatchUpdate(ctx, update, false)
}

// handleUpdateSync is like handleUpdate but waits for message processing to complete.
// Used in tests where we need synchronous behavior.
func (b *Bot) handleUpdateSync(ctx context.Context, update tgbotapi.Update) {
	b.dispatchUpdate(ctx, update, true)
}

// dispatchUpdate routes updates to the appropriate session worker.
// If sync is true, it waits for message processing to complete.
func (b *Bot) dispatchUpdate(ctx context.Context, update tgbotapi.Update, sync bool) {
	var userId int64

	// Determine user ID from the update
	if update.CallbackQuery != nil {
		userId = update.CallbackQuery.From.ID
	} else if update.Message != nil {
		userId = update.Message.From.ID
	} else {
		return
	}

	session, err := b.state.getUserSession(userId)
	if err != nil {
		log.Error().Err(err).Send()
		return
	}

	// Helper to send sync or async based on flag
	send := func(msg SessionMessage) {
		if sync {
			session.SendSync(msg)
		} else {
			session.Send(msg)
		}
	}

	// Dispatch to session worker based on update type
	if update.CallbackQuery != nil {
		send(SessionMessage{
			Type:          "callback",
			Ctx:           ctx,
			CallbackQuery: update.CallbackQuery,
		})
		return
	}

	if update.Message != nil {
		log.Info().Str("text", update.Message.Text).Str("caption", update.Message.Caption).Msg("got message")

		if len(update.Message.Photo) > 0 {
			send(SessionMessage{
				Type:    "photo",
				Ctx:     ctx,
				Message: update.Message,
			})
		} else {
			send(SessionMessage{
				Type:    "text",
				Ctx:     ctx,
				Message: update.Message,
			})
		}
	}
}

// HandleSessionMessage implements MessageHandler interface.
// This is called by the session worker goroutine for sequential processing.
// No mutex locking is needed here since only one goroutine accesses session state.
func (b *Bot) HandleSessionMessage(ctx context.Context, session *UserSession, msg SessionMessage) {
	switch msg.Type {
	case "callback":
		b.handleCallbackQuery(ctx, session, msg.CallbackQuery)
	case "photo":
		b.handlePhotoMessage(ctx, session, msg.Message)
	case "text":
		b.handleTextMessage(ctx, session, msg.Message)
	case "album_timeout":
		b.listingHandler.ProcessAlbumTimeout(msg.Ctx, session, msg.AlbumBuffer)
	// Bulk mode message types
	case "bulk_album_timeout":
		b.bulkHandler.ProcessBulkAlbumTimeout(msg.Ctx, session, msg.AlbumBuffer)
	case "bulk_analysis_complete":
		b.bulkHandler.HandleBulkAnalysisComplete(msg.Ctx, session, msg.BulkAnalysisResult)
	case "bulk_draft_error":
		b.bulkHandler.HandleBulkDraftError(session, msg.BulkDraftError)
	case "bulk_status_update":
		b.bulkHandler.HandleStatusUpdate(session)
	}
}

// handlePhotoMessage processes photo messages.
// Called from session worker - no locking needed.
func (b *Bot) handlePhotoMessage(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	if !session.isLoggedIn() {
		session.reply(loginRequiredText)
		return
	}

	// Check if in bulk mode first
	if session.IsInBulkMode() {
		b.bulkHandler.HandlePhoto(ctx, session, message)
		return
	}

	b.listingHandler.HandlePhoto(ctx, session, message)
}

// handleTextMessage processes text messages.
// Called from session worker - no locking needed.
func (b *Bot) handleTextMessage(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	// Handle auth flow
	if b.authHandler.HandleMessage(ctx, session, message.Text) {
		return
	}

	// Handle postal code command input
	if b.handlePostalCodeInput(session, message.Text) {
		return
	}

	// Handle bulk mode text input (for editing fields)
	if session.IsInBulkMode() && !strings.HasPrefix(message.Text, "/") {
		if b.bulkHandler.HandleBulkTextInput(session, message.Text) {
			return
		}
	}

	// Handle listing flow inputs (replies, attributes, prices)
	if b.listingHandler.HandleInput(ctx, session, message) {
		return
	}

	// Try to handle as natural language edit command if there's an active draft
	// and the message looks like an edit command (not a regular command)
	if message.Text != "" && !strings.HasPrefix(message.Text, "/") {
		hasActiveDraft := session.HasActiveDraft()
		draftState := session.GetDraftState()
		log.Debug().
			Bool("hasActiveDraft", hasActiveDraft).
			Str("draftState", draftState.String()).
			Str("text", message.Text).
			Msg("checking for natural language edit")
		if hasActiveDraft {
			if b.listingHandler.HandleEditCommand(ctx, session, message.Text) {
				return
			}
		}
	}

	// Handle commands
	b.handleCommand(ctx, session, message)
}

// handleCommand processes bot commands.
// Called from session worker - no locking needed.
func (b *Bot) handleCommand(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	command, args := parseCommand(message.Text)
	argsStr := strings.Join(args, " ")
	switch command {
	case "/start":
		if !session.isLoggedIn() {
			session.reply(loginRequiredText)
		} else {
			session.reply(startText)
		}
	case "/login":
		b.authHandler.HandleLoginCommand(session)
	case "/peru":
		// Handle bulk mode cancellation
		if session.IsInBulkMode() {
			b.bulkHandler.HandlePeruCommand(ctx, session)
			return
		}
		session.deleteCurrentDraft(ctx)
		session.reset()
		session.replyAndRemoveCustomKeyboard(okText)
	case "/laheta":
		// Handle bulk mode publishing
		if session.IsInBulkMode() {
			b.bulkHandler.HandleLahetaCommand(ctx, session, argsStr)
			return
		}
		b.listingHandler.HandleSendListingCommand(ctx, session)
	case "/era":
		if !session.isLoggedIn() {
			session.reply(loginRequiredText)
			return
		}
		b.bulkHandler.HandleEräCommand(ctx, session)
	case "/valmis":
		if !session.IsInBulkMode() {
			session.reply("Et ole erätilassa. Aloita /era komennolla.")
			return
		}
		b.bulkHandler.HandleValmisCommand(ctx, session)
	case "/poistakuvat":
		session.photos = nil
		session.pendingPhotos = nil
		session.currentDraft = nil
		session.reply(photosRemoved)
	case "/osasto":
		b.handleOsastoCommand(session)
	case "/malli":
		b.handleTemplateCommand(session, message.Text)
	case "/poistamalli":
		b.handleDeleteTemplate(session)
	case "/postinumero":
		b.handlePostalCodeCommand(session)
	default:
		if !session.isLoggedIn() {
			session.reply(loginRequiredText)
			return
		}
		if session.IsInBulkMode() {
			session.reply("Lähetä kuvia tai käytä /valmis kun olet valmis.")
			return
		}
		session.reply("Lähetä kuva aloittaaksesi ilmoituksen teon")
	}
}

// handleCallbackQuery handles inline keyboard button presses.
// Called from session worker - no locking needed.
func (b *Bot) handleCallbackQuery(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	// Answer the callback to remove the loading state
	callback := tgbotapi.NewCallback(query.ID, "")
	b.tg.Request(callback)

	// Route to appropriate handler
	if strings.HasPrefix(query.Data, "bulk:") {
		b.bulkHandler.HandleBulkCallback(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "cat:") {
		b.listingHandler.HandleCategorySelection(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "shipping:") {
		b.listingHandler.HandleShippingSelection(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "reselect:") {
		b.handleReselectCallback(ctx, session, query)
	}
}

// --- Template commands ---

// handleTemplateCommand handles /malli command - view or set template.
func (b *Bot) handleTemplateCommand(session *UserSession, text string) {
	if b.sessionStore == nil {
		session.reply("Mallit eivät ole käytettävissä")
		return
	}

	args := strings.TrimSpace(strings.TrimPrefix(text, "/malli"))

	if args == "" {
		// View current template
		tmpl, err := b.sessionStore.GetTemplate(session.userId)
		if err != nil {
			session.replyWithError(err)
			return
		}
		if tmpl == nil {
			session.reply("Ei tallennettua mallia.\n\nAseta malli: `/malli <teksti>`\n\nEsim: `/malli Nouto Kannelmäestä{{#if shipping}} tai postitus{{/end}}. Mobilepay/käteinen.`")
			return
		}
		session.reply(fmt.Sprintf("*Nykyinen malli:*\n`%s`\n\nPoista malli: /poistamalli", escapeMarkdown(tmpl.Content)))
		return
	}

	// Set new template
	if err := b.sessionStore.SetTemplate(session.userId, args); err != nil {
		session.replyWithError(err)
		return
	}
	session.reply("✅ Malli tallennettu.")
}

// handleDeleteTemplate handles /poistamalli command.
func (b *Bot) handleDeleteTemplate(session *UserSession) {
	if b.sessionStore == nil {
		session.reply("Mallit eivät ole käytettävissä")
		return
	}

	if err := b.sessionStore.DeleteTemplate(session.userId); err != nil {
		session.replyWithError(err)
		return
	}
	session.reply("🗑 Malli poistettu.")
}

// handlePostalCodeCommand handles /postinumero command - view or change postal code.
func (b *Bot) handlePostalCodeCommand(session *UserSession) {
	if b.sessionStore == nil {
		session.reply("Postinumerot eivät ole käytettävissä")
		return
	}

	currentCode, err := b.sessionStore.GetPostalCode(session.userId)
	if err != nil {
		session.replyWithError(err)
		return
	}

	session.awaitingPostalCodeInput = true
	if currentCode != "" {
		session.reply(postalCodeCurrentText, currentCode)
	} else {
		session.reply(postalCodeNotSetText)
	}
}

// handlePostalCodeInput handles text input when awaiting postal code from /postinumero command.
// Returns true if the message was handled.
// Called from session worker - no locking needed.
func (b *Bot) handlePostalCodeInput(session *UserSession, text string) bool {
	if !session.awaitingPostalCodeInput {
		return false
	}

	// Handle /peru command to cancel
	if text == "/peru" {
		session.awaitingPostalCodeInput = false
		session.reply(postalCodeCommandCancelText)
		return true
	}

	postalCode := strings.TrimSpace(text)
	if !isValidPostalCode(postalCode) {
		session.reply(postalCodeInvalidText)
		return true
	}

	// Save postal code
	if b.sessionStore != nil {
		if err := b.sessionStore.SetPostalCode(session.userId, postalCode); err != nil {
			session.replyWithError(err)
			return true
		}
	}

	session.awaitingPostalCodeInput = false
	session.reply(postalCodeUpdatedText, postalCode)
	return true
}

// handleOsastoCommand handles /osasto command - re-select category or attributes
func (b *Bot) handleOsastoCommand(session *UserSession) {
	if session.currentDraft == nil {
		session.reply("Ei aktiivista ilmoitusta. Lähetä ensin kuva.")
		return
	}

	if len(session.currentDraft.CategoryPredictions) == 0 {
		session.reply("Ei osastoehdotuksia saatavilla.")
		return
	}

	// If still awaiting category, just show category keyboard
	if session.currentDraft.State == AdFlowStateAwaitingCategory {
		msg := tgbotapi.NewMessage(session.userId, "Valitse osasto")
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = makeCategoryPredictionKeyboard(session.currentDraft.CategoryPredictions)
		session.replyWithMessage(msg)
		return
	}

	// If past category selection, show options menu
	msg := tgbotapi.NewMessage(session.userId, "Mitä haluat muuttaa?")
	buttons := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Vaihda osasto", "reselect:category")},
	}
	// Only show attribute option if there were required attributes
	if session.adAttributes != nil && len(session.adAttributes.Attributes) > 0 {
		buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("Valitse lisätiedot uudelleen", "reselect:attrs"),
		})
	}
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	session.replyWithMessage(msg)
}

// handleReselectCallback handles reselect:category and reselect:attrs callbacks
// Called from session worker - no locking needed.
func (b *Bot) handleReselectCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	if session.currentDraft == nil {
		session.reply("Ei aktiivista ilmoitusta.")
		return
	}

	// Remove the inline keyboard
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		b.tg.Request(edit)
	}

	if query.Data == "reselect:category" {
		// Reset to category selection state and clear collected attributes
		session.currentDraft.State = AdFlowStateAwaitingCategory
		session.currentDraft.CategoryID = 0
		session.currentDraft.CollectedAttrs = make(map[string]string)
		session.currentDraft.RequiredAttrs = nil
		session.currentDraft.CurrentAttrIndex = 0

		// Show category selection keyboard
		msg := tgbotapi.NewMessage(session.userId, "Valitse osasto")
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = makeCategoryPredictionKeyboard(session.currentDraft.CategoryPredictions)
		session.replyWithMessage(msg)
	} else if query.Data == "reselect:attrs" {
		// Clear collected attributes but keep category
		session.currentDraft.CollectedAttrs = make(map[string]string)
		session.currentDraft.CurrentAttrIndex = 0
		categoryID := session.currentDraft.CategoryID

		// Re-process category selection to fetch and prompt for attributes
		b.listingHandler.ProcessCategorySelection(ctx, session, categoryID)
	}
}

// --- Template expansion ---

// templateRegex matches {{#if shipping}}...{{/end}} blocks (case-insensitive, flexible whitespace)
var templateRegex = regexp.MustCompile(`(?si)\{\{\s*#if\s+shipping\s*\}\}(.*?)\{\{\s*/end\s*\}\}`)

// expandTemplate expands template conditionals based on shipping flag.
func expandTemplate(content string, shipping bool) string {
	return templateRegex.ReplaceAllStringFunc(content, func(match string) string {
		if shipping {
			submatch := templateRegex.FindStringSubmatch(match)
			if len(submatch) > 1 {
				return submatch[1]
			}
		}
		return ""
	})
}

// --- Price parsing ---

// priceRegex matches valid price formats:
// - Plain numbers: "50", "100", "1000"
// - With thousands separator (space): "1 000", "10 000"
// - With euro symbol: "50€", "100 €", "€50", "€ 100"
// - With "e" or "eur" suffix: "50e", "100 eur", "50 EUR"
// - Optionally with decimals: "50.50", "99,99€"
// The entire input (after trimming) must match the pattern.
var priceRegex = regexp.MustCompile(`(?i)^€?\s*(\d+(?:\s\d+)*(?:[.,]\d+)?)\s*(?:€|e|eur)?$`)

func parsePriceMessage(text string) (int, error) {
	text = strings.TrimSpace(text)
	m := priceRegex.FindStringSubmatch(text)
	if m == nil {
		return 0, fmt.Errorf("no price found")
	}
	// Remove thousands separators (spaces) and replace comma with dot
	priceStr := strings.ReplaceAll(m[1], " ", "")
	priceStr = strings.Replace(priceStr, ",", ".", 1)
	var price float64
	_, err := fmt.Sscanf(priceStr, "%f", &price)
	if err != nil {
		return 0, err
	}
	// Round to nearest integer
	return int(price + 0.5), nil
}

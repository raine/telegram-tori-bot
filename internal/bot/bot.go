package bot

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/llm"
	"github.com/raine/telegram-tori-bot/internal/storage"
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
	adminID        int64

	// Handlers
	authHandler    *AuthHandler
	listingHandler *ListingHandler
	bulkHandler    *BulkHandler
	listingManager *ListingManager
	watchHandler   *WatchHandler
}

// NewBot creates a new Bot instance.
func NewBot(tg BotAPI, sessionStore storage.SessionStore, adminID int64) *Bot {
	bot := &Bot{
		tg:           tg,
		sessionStore: sessionStore,
		adminID:      adminID,
	}

	bot.state = bot.NewBotState()
	bot.authHandler = NewAuthHandler(sessionStore)
	bot.listingManager = NewListingManager(tg)
	bot.watchHandler = NewWatchHandler(tg, sessionStore)

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

// HandleUpdate is the main message router.
// It dispatches messages to the appropriate session worker for sequential processing.
func (b *Bot) HandleUpdate(ctx context.Context, update tgbotapi.Update) {
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

	// Check if user is allowed (admin always allowed)
	// MUST be before getUserSession to prevent memory exhaustion from random user IDs
	if userId != b.adminID {
		allowed, err := b.sessionStore.IsUserAllowed(userId)
		if err != nil {
			log.Error().Err(err).Int64("user_id", userId).Msg("whitelist check failed")
			return // Fail closed
		}
		if !allowed {
			return // Silent drop
		}
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
	case "draft_expired":
		b.listingHandler.HandleDraftExpired(msg.Ctx, session, msg.ExpiredTimer)
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
		session.reply(MsgLoginRequired)
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
			session.reply(MsgLoginRequired)
		} else {
			session.reply(MsgStartPrompt)
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
		session.replyAndRemoveCustomKeyboard(MsgOk)
	case "/laheta":
		// Handle bulk mode publishing
		if session.IsInBulkMode() {
			b.bulkHandler.HandleLahetaCommand(ctx, session, argsStr)
			return
		}
		b.listingHandler.HandleSendListingCommand(ctx, session)
	case "/era":
		if !session.isLoggedIn() {
			session.reply(MsgLoginRequired)
			return
		}
		b.bulkHandler.HandleEräCommand(ctx, session)
	case "/valmis":
		if !session.IsInBulkMode() {
			session.reply(MsgBulkNotInBulkMode)
			return
		}
		b.bulkHandler.HandleValmisCommand(ctx, session)
	case "/poistakuvat":
		session.photoCol.Photos = nil
		session.photoCol.PendingPhotos = nil
		session.draft.CurrentDraft = nil
		session.reply(MsgPhotosRemoved)
	case "/osasto":
		b.handleOsastoCommand(session)
	case "/malli":
		b.handleTemplateCommand(session, message.Text)
	case "/poistamalli":
		b.handleDeleteTemplate(session)
	case "/postinumero":
		b.handlePostalCodeCommand(session)
	case "/ilmoitukset":
		if !session.isLoggedIn() {
			session.reply(MsgLoginRequired)
			return
		}
		b.listingManager.HandleIlmoituksetCommand(ctx, session)
	case "/haku":
		b.watchHandler.HandleHakuCommand(ctx, session, argsStr)
	case "/seuraa":
		b.watchHandler.HandleSeuraaCommand(ctx, session, argsStr)
	case "/seurattavat":
		b.watchHandler.HandleSeurattavatCommand(ctx, session)
	case "/admin":
		b.handleAdminCommand(session, argsStr)
	case "/versio":
		session.reply(MsgVersionInfo, Version, BuildTime)
	default:
		if !session.isLoggedIn() {
			session.reply(MsgLoginRequired)
			return
		}
		if session.IsInBulkMode() {
			session.reply(MsgBulkSendPhotosOrFinish)
			return
		}
		session.reply(MsgStartPrompt)
	}
}

// handleCallbackQuery handles inline keyboard button presses.
// Called from session worker - no locking needed.
func (b *Bot) handleCallbackQuery(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	// Answer the callback to remove the loading state
	callback := tgbotapi.NewCallback(query.ID, "")
	b.tg.Request(callback)

	// Route to appropriate handler
	if strings.HasPrefix(query.Data, "listings:") || strings.HasPrefix(query.Data, "ad:") {
		b.listingManager.HandleListingCallback(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "bulk:") {
		b.bulkHandler.HandleBulkCallback(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "cat:") {
		b.listingHandler.HandleCategorySelection(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "shipping:") {
		b.listingHandler.HandleShippingSelection(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "pkgsize:") {
		b.listingHandler.HandlePackageSizeSelection(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "reselect:") {
		b.handleReselectCallback(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "publish:") {
		b.listingHandler.HandlePublishCallback(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "watch:") {
		b.watchHandler.HandleWatchCallback(ctx, session, query)
	}
}

// --- Template commands ---

// handleTemplateCommand handles /malli command - view or set template.
func (b *Bot) handleTemplateCommand(session *UserSession, text string) {
	if b.sessionStore == nil {
		session.reply(MsgTemplateNotAvailable)
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
			session.reply(MsgTemplateNotSet)
			return
		}
		session.reply(fmt.Sprintf(MsgTemplateCurrentFmt, escapeMarkdown(tmpl.Content)))
		return
	}

	// Set new template
	if err := b.sessionStore.SetTemplate(session.userId, args); err != nil {
		session.replyWithError(err)
		return
	}
	session.reply(MsgTemplateSaved)
}

// handleDeleteTemplate handles /poistamalli command.
func (b *Bot) handleDeleteTemplate(session *UserSession) {
	if b.sessionStore == nil {
		session.reply(MsgTemplateNotAvailable)
		return
	}

	if err := b.sessionStore.DeleteTemplate(session.userId); err != nil {
		session.replyWithError(err)
		return
	}
	session.reply(MsgTemplateDeleted)
}

// handlePostalCodeCommand handles /postinumero command - view or change postal code.
func (b *Bot) handlePostalCodeCommand(session *UserSession) {
	if b.sessionStore == nil {
		session.reply(MsgPostalCodeNotAvailable)
		return
	}

	currentCode, err := b.sessionStore.GetPostalCode(session.userId)
	if err != nil {
		session.replyWithError(err)
		return
	}

	session.awaitingPostalCodeInput = true
	if currentCode != "" {
		session.reply(MsgPostalCodeCurrent, currentCode)
	} else {
		session.reply(MsgPostalCodeNotSet)
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
		session.reply(MsgPostalCodeCommandCancel)
		return true
	}

	postalCode := strings.TrimSpace(text)
	if !isValidPostalCode(postalCode) {
		session.reply(MsgPostalCodeInvalid)
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
	session.reply(MsgPostalCodeUpdated, postalCode)
	return true
}

// handleAdminCommand handles /admin command with subcommands.
// Only the admin user can use this command (defense in depth check).
func (b *Bot) handleAdminCommand(session *UserSession, args string) {
	// Defense in depth: verify caller is admin even though whitelist check passed
	if session.userId != b.adminID {
		return // Silent drop for non-admin users
	}

	parts := strings.Fields(args)
	if len(parts) == 0 {
		session.reply(MsgAdminUsage)
		return
	}

	switch parts[0] {
	case "users":
		if len(parts) < 2 {
			session.reply(MsgAdminUsage)
			return
		}
		b.handleAdminUsersCommand(session, parts[1], parts[2:])
	default:
		session.reply(MsgAdminUsage)
	}
}

// handleAdminUsersCommand handles /admin users subcommands.
func (b *Bot) handleAdminUsersCommand(session *UserSession, action string, args []string) {
	switch action {
	case "add":
		if len(args) < 1 {
			session.reply(MsgAdminUserAddUsage)
			return
		}
		userID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			session.reply(MsgAdminUserInvalidID)
			return
		}
		if err := b.sessionStore.AddAllowedUser(userID, session.userId); err != nil {
			session.replyWithError(err)
			return
		}
		session.reply(MsgAdminUserAdded, userID)

	case "remove":
		if len(args) < 1 {
			session.reply(MsgAdminUserRemoveUsage)
			return
		}
		userID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			session.reply(MsgAdminUserInvalidID)
			return
		}
		if err := b.sessionStore.RemoveAllowedUser(userID); err != nil {
			session.replyWithError(err)
			return
		}
		session.reply(MsgAdminUserRemoved, userID)

	case "list":
		users, err := b.sessionStore.GetAllowedUsers()
		if err != nil {
			session.replyWithError(err)
			return
		}
		if len(users) == 0 {
			session.reply(MsgAdminNoUsers)
			return
		}
		var sb strings.Builder
		sb.WriteString(MsgAdminAllowedUsers)
		for _, u := range users {
			sb.WriteString(fmt.Sprintf("• `%d` (lisätty %s)\n", u.TelegramID, u.AddedAt.Format("2006-01-02")))
		}
		session.reply(sb.String())

	default:
		session.reply(MsgAdminUsage)
	}
}

// handleOsastoCommand handles /osasto command - re-select category or attributes
func (b *Bot) handleOsastoCommand(session *UserSession) {
	if session.draft.CurrentDraft == nil {
		session.reply(MsgNoActiveListingPhoto)
		return
	}

	if len(session.draft.CurrentDraft.CategoryPredictions) == 0 {
		session.reply(MsgNoCategoryOptions)
		return
	}

	// If still awaiting category, just show category keyboard
	if session.draft.CurrentDraft.State == AdFlowStateAwaitingCategory {
		msg := tgbotapi.NewMessage(session.userId, MsgSelectCategory)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = makeCategoryPredictionKeyboard(session.draft.CurrentDraft.CategoryPredictions)
		session.replyWithMessage(msg)
		return
	}

	// If past category selection, show options menu
	msg := tgbotapi.NewMessage(session.userId, MsgWhatToChange)
	buttons := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(BtnChangeCategory, "reselect:category")},
	}
	// Only show attribute option if there were required attributes
	if session.draft.AdAttributes != nil && len(session.draft.AdAttributes.Attributes) > 0 {
		buttons = append(buttons, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(BtnReselectAttributes, "reselect:attrs"),
		})
	}
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	session.replyWithMessage(msg)
}

// handleReselectCallback handles reselect:category and reselect:attrs callbacks
// Called from session worker - no locking needed.
func (b *Bot) handleReselectCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	if session.draft.CurrentDraft == nil {
		session.reply(MsgNoActiveListing)
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
		// Preserve values before resetting for category change
		preserved := &PreservedValues{
			Price:            session.draft.CurrentDraft.Price,
			TradeType:        session.draft.CurrentDraft.TradeType,
			ShippingPossible: session.draft.CurrentDraft.ShippingPossible,
			ShippingSet:      session.draft.CurrentDraft.State >= AdFlowStateReadyToPublish || session.draft.CurrentDraft.State == AdFlowStateAwaitingPostalCode,
			CollectedAttrs:   make(map[string]string),
		}

		// Copy collected attributes (including condition)
		for k, v := range session.draft.CurrentDraft.CollectedAttrs {
			preserved.CollectedAttrs[k] = v
		}

		// Reset to category selection state and clear collected attributes
		session.draft.CurrentDraft.State = AdFlowStateAwaitingCategory
		session.draft.CurrentDraft.CategoryID = 0
		session.draft.CurrentDraft.CollectedAttrs = make(map[string]string)
		session.draft.CurrentDraft.RequiredAttrs = nil
		session.draft.CurrentDraft.CurrentAttrIndex = 0
		session.draft.CurrentDraft.PreservedValues = preserved

		// Show category selection keyboard
		msg := tgbotapi.NewMessage(session.userId, MsgSelectCategory)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = makeCategoryPredictionKeyboard(session.draft.CurrentDraft.CategoryPredictions)
		session.replyWithMessage(msg)
	} else if query.Data == "reselect:attrs" {
		// Clear collected attributes but keep category
		session.draft.CurrentDraft.CollectedAttrs = make(map[string]string)
		session.draft.CurrentDraft.CurrentAttrIndex = 0
		categoryID := session.draft.CurrentDraft.CategoryID

		// Re-process category selection to fetch and prompt for attributes
		b.listingHandler.ProcessCategorySelection(ctx, session, categoryID)
	}
}

// --- Template expansion ---

// TemplateData holds variables available in user templates.
type TemplateData struct {
	Shipping bool // Whether shipping is enabled
	Giveaway bool // Whether it's a giveaway (free item)
	Price    int  // Price in euros (0 for giveaways)
}

// expandTemplate expands template conditionals using Go text/template syntax.
// Available variables: {{.shipping}}, {{.giveaway}}, {{.price}}
func expandTemplate(content string, data TemplateData) string {
	tmpl, err := template.New("").Parse(content)
	if err != nil {
		log.Error().Err(err).Str("content", content).Msg("failed to parse template")
		return content
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]interface{}{
		"shipping": data.Shipping,
		"giveaway": data.Giveaway,
		"price":    data.Price,
	})
	if err != nil {
		log.Error().Err(err).Str("content", content).Msg("failed to execute template")
		return content
	}
	return buf.String()
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

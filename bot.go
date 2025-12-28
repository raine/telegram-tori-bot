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

// SetVisionAnalyzer sets the vision analyzer for image analysis.
func (b *Bot) SetVisionAnalyzer(analyzer llm.Analyzer) {
	b.visionAnalyzer = analyzer
	b.listingHandler = NewListingHandler(b.tg, analyzer, b.sessionStore)
}

// handleUpdate is the main message router.
// Note: Handlers manage their own locking internally to avoid deadlocks.
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

	// Handle auth flow (handler manages its own locking)
	if b.authHandler.HandleMessage(ctx, session, update.Message.Text) {
		return
	}

	// Handle photo messages (handler manages its own locking)
	if len(update.Message.Photo) > 0 {
		if !session.IsLoggedIn() {
			session.mu.Lock()
			session.reply(loginRequiredText)
			session.mu.Unlock()
			return
		}
		b.listingHandler.HandlePhoto(ctx, session, update.Message)
		return
	}

	// Handle listing flow inputs (replies, attributes, prices)
	// Handler manages its own locking and checks state internally
	if b.listingHandler.HandleInput(ctx, session, update.Message) {
		return
	}

	// Handle commands (requires locking)
	b.handleCommand(ctx, session, update.Message)
}

// handleCommand processes bot commands.
func (b *Bot) handleCommand(ctx context.Context, session *UserSession, message *tgbotapi.Message) {
	session.mu.Lock()
	defer session.mu.Unlock()

	command, _ := parseCommand(message.Text)
	switch command {
	case "/start":
		if !session.isLoggedIn() {
			session.reply(loginRequiredText)
		} else {
			session.reply(startText)
		}
	case "/login":
		// Release lock before calling handler that manages its own locking
		session.mu.Unlock()
		b.authHandler.HandleLoginCommand(session)
		session.mu.Lock()
	case "/peru":
		session.reset()
		session.replyAndRemoveCustomKeyboard(okText)
	case "/laheta":
		// Release lock before calling handler that manages its own locking
		session.mu.Unlock()
		b.listingHandler.HandleSendListingCommand(ctx, session)
		session.mu.Lock()
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
	default:
		if !session.isLoggedIn() {
			session.reply(loginRequiredText)
			return
		}
		session.reply("Lähetä kuva aloittaaksesi ilmoituksen teon")
	}
}

// handleCallbackQuery handles inline keyboard button presses.
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

	// Route to appropriate handler
	if strings.HasPrefix(query.Data, "cat:") {
		b.listingHandler.HandleCategorySelection(ctx, session, query)
	} else if strings.HasPrefix(query.Data, "shipping:") {
		b.listingHandler.HandleShippingSelection(ctx, session, query)
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

// handleOsastoCommand handles /osasto command - re-select category
func (b *Bot) handleOsastoCommand(session *UserSession) {
	if session.currentDraft == nil {
		session.reply("Ei aktiivista ilmoitusta. Lähetä ensin kuva.")
		return
	}

	if len(session.currentDraft.CategoryPredictions) == 0 {
		session.reply("Ei osastoehdotuksia saatavilla.")
		return
	}

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

var priceRegex = regexp.MustCompile(`(\d+)`)

func parsePriceMessage(text string) (int, error) {
	m := priceRegex.FindStringSubmatch(text)
	if m == nil {
		return 0, fmt.Errorf("no price found")
	}
	var price int
	_, err := fmt.Sscanf(m[1], "%d", &price)
	return price, err
}

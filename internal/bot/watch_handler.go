package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/rs/zerolog/log"
)

const (
	searchResultsLimit = 5 // Number of search results to display
)

// WatchHandler handles search watch commands and callbacks.
type WatchHandler struct {
	tg           BotAPI
	store        storage.SessionStore
	searchClient *tori.SearchClient
}

// NewWatchHandler creates a new WatchHandler.
func NewWatchHandler(tg BotAPI, store storage.SessionStore) *WatchHandler {
	return &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: tori.NewSearchClient(),
	}
}

// HandleHakuCommand handles the /haku command - search and show results.
func (h *WatchHandler) HandleHakuCommand(ctx context.Context, session *UserSession, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		session.reply(MsgSearchQueryMissing)
		return
	}

	// Store query in session for callback
	session.pendingSearchQuery = query

	// Perform search
	results, err := h.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query: query,
		Rows:  searchResultsLimit,
	})
	if err != nil {
		log.Error().Err(err).Str("query", query).Msg("search failed")
		session.reply(MsgSearchError, err.Error())
		return
	}

	// Build response message
	var sb strings.Builder
	if len(results.Docs) == 0 {
		sb.WriteString(fmt.Sprintf(MsgSearchNoResults, escapeMarkdown(query)))
	} else {
		sb.WriteString(fmt.Sprintf(MsgSearchResults, escapeMarkdown(query), results.Metadata.ResultSize.MatchCount))
		for i, doc := range results.Docs {
			title := doc.Heading
			if len([]rune(title)) > 40 {
				title = string([]rune(title)[:37]) + "..."
			}

			price := "—"
			if doc.Price != nil {
				price = doc.Price.Value
			}

			sb.WriteString(fmt.Sprintf("%d. %s — %s", i+1, escapeMarkdown(title), escapeMarkdown(price)))
			if doc.Location != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", escapeMarkdown(doc.Location)))
			}
			sb.WriteString("\n")
		}
	}

	// Create "watch" button
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(BtnCreateWatch, "watch:create"),
		),
	)

	msg := tgbotapi.NewMessage(session.userId, sb.String())
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = keyboard

	session.replyWithMessage(msg)
}

// HandleSeuraaCommand handles the /seuraa command - create watch directly.
func (h *WatchHandler) HandleSeuraaCommand(ctx context.Context, session *UserSession, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		session.reply(MsgWatchQueryMissing)
		return
	}

	h.createWatch(session, query)
}

// HandleSeurattavatCommand handles the /seurattavat command - list watches.
func (h *WatchHandler) HandleSeurattavatCommand(ctx context.Context, session *UserSession) {
	sqliteStore, ok := h.store.(*storage.SQLiteStore)
	if !ok {
		log.Error().Msg("store is not SQLiteStore")
		session.reply(MsgUnexpectedErr, "storage error")
		return
	}

	watches, err := sqliteStore.GetWatchesByUser(session.userId)
	if err != nil {
		log.Error().Err(err).Int64("userId", session.userId).Msg("failed to get watches")
		session.replyWithError(err)
		return
	}

	if len(watches) == 0 {
		session.reply(MsgNoWatches)
		return
	}

	// Build message with list of watches
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(MsgWatchesHeader, len(watches)))

	for i, watch := range watches {
		sb.WriteString(fmt.Sprintf(MsgWatchItem, i+1, escapeMarkdown(watch.Query)))
	}

	// Build delete buttons (2 per row)
	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton

	for i, watch := range watches {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s %d", BtnDeleteWatch, i+1),
			fmt.Sprintf("watch:delete:%s", watch.ID),
		)
		currentRow = append(currentRow, btn)

		// 4 buttons per row
		if len(currentRow) == 4 || i == len(watches)-1 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}

	// Add close button
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(BtnClose, "watch:close"),
	))

	msg := tgbotapi.NewMessage(session.userId, sb.String())
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)

	session.replyWithMessage(msg)
}

// HandleWatchCallback handles watch-related callbacks.
func (h *WatchHandler) HandleWatchCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	data := query.Data

	switch {
	case data == "watch:create":
		h.handleCreateWatchCallback(session, query)
	case strings.HasPrefix(data, "watch:delete:"):
		watchID := strings.TrimPrefix(data, "watch:delete:")
		h.handleDeleteWatchCallback(session, query, watchID)
	case data == "watch:close":
		h.handleCloseCallback(session, query)
	}
}

// handleCreateWatchCallback handles the "create watch" button callback.
func (h *WatchHandler) handleCreateWatchCallback(session *UserSession, query *tgbotapi.CallbackQuery) {
	// Get query from session
	searchQuery := session.pendingSearchQuery
	if searchQuery == "" {
		// Edit the message to show error
		if query.Message != nil {
			edit := tgbotapi.NewEditMessageText(
				query.Message.Chat.ID,
				query.Message.MessageID,
				"Haku vanhentui. Tee uusi haku komennolla /haku.",
			)
			h.tg.Request(edit)
		}
		return
	}

	// Clear the pending query
	session.pendingSearchQuery = ""

	// Remove the button from the message
	if query.Message != nil {
		edit := tgbotapi.NewEditMessageReplyMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
		)
		h.tg.Request(edit)
	}

	h.createWatch(session, searchQuery)
}

// handleDeleteWatchCallback handles delete button callbacks.
func (h *WatchHandler) handleDeleteWatchCallback(session *UserSession, query *tgbotapi.CallbackQuery, watchID string) {
	sqliteStore, ok := h.store.(*storage.SQLiteStore)
	if !ok {
		log.Error().Msg("store is not SQLiteStore")
		return
	}

	err := sqliteStore.DeleteWatch(watchID, session.userId)
	if err != nil {
		log.Error().Err(err).Str("watchID", watchID).Msg("failed to delete watch")
		session.reply(MsgWatchNotFound)
		return
	}

	log.Info().Str("watchID", watchID).Int64("userId", session.userId).Msg("watch deleted")

	// Update the message with refreshed list
	watches, err := sqliteStore.GetWatchesByUser(session.userId)
	if err != nil {
		log.Error().Err(err).Msg("failed to refresh watches")
		session.reply(MsgWatchDeleted)
		return
	}

	if len(watches) == 0 {
		// Delete the message and show "no watches"
		if query.Message != nil {
			h.tg.Request(tgbotapi.NewDeleteMessage(query.Message.Chat.ID, query.Message.MessageID))
		}
		session.reply(MsgWatchDeleted + "\n\n" + MsgNoWatches)
		return
	}

	// Rebuild message
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(MsgWatchesHeader, len(watches)))
	for i, watch := range watches {
		sb.WriteString(fmt.Sprintf(MsgWatchItem, i+1, escapeMarkdown(watch.Query)))
	}

	// Rebuild buttons
	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton
	for i, watch := range watches {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s %d", BtnDeleteWatch, i+1),
			fmt.Sprintf("watch:delete:%s", watch.ID),
		)
		currentRow = append(currentRow, btn)
		if len(currentRow) == 4 || i == len(watches)-1 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(BtnClose, "watch:close"),
	))

	if query.Message != nil {
		edit := tgbotapi.NewEditMessageTextAndMarkup(
			query.Message.Chat.ID,
			query.Message.MessageID,
			sb.String(),
			tgbotapi.NewInlineKeyboardMarkup(rows...),
		)
		edit.ParseMode = tgbotapi.ModeMarkdown
		h.tg.Request(edit)
	}
}

// handleCloseCallback handles the close button callback.
func (h *WatchHandler) handleCloseCallback(session *UserSession, query *tgbotapi.CallbackQuery) {
	if query.Message != nil {
		h.tg.Request(tgbotapi.NewDeleteMessage(query.Message.Chat.ID, query.Message.MessageID))
	}
}

// createWatch creates a new watch for the user.
func (h *WatchHandler) createWatch(session *UserSession, query string) {
	sqliteStore, ok := h.store.(*storage.SQLiteStore)
	if !ok {
		log.Error().Msg("store is not SQLiteStore")
		session.reply(MsgUnexpectedErr, "storage error")
		return
	}

	// Check if watch already exists
	exists, err := sqliteStore.WatchExistsForQuery(session.userId, query)
	if err != nil {
		log.Error().Err(err).Msg("failed to check existing watch")
		session.replyWithError(err)
		return
	}
	if exists {
		session.reply(MsgWatchAlreadyExists, query)
		return
	}

	// Check watch limit
	count, err := sqliteStore.CountWatchesByUser(session.userId)
	if err != nil {
		log.Error().Err(err).Msg("failed to count watches")
		session.replyWithError(err)
		return
	}
	if count >= MaxWatchesPerUser {
		session.reply(MsgWatchLimitReached, MaxWatchesPerUser)
		return
	}

	// Create watch
	watch, err := sqliteStore.CreateWatch(session.userId, query)
	if err != nil {
		log.Error().Err(err).Msg("failed to create watch")
		session.replyWithError(err)
		return
	}

	log.Info().
		Str("watchID", watch.ID).
		Int64("userId", session.userId).
		Str("query", query).
		Msg("watch created")

	// Seed seen listings with current results to avoid notifying about existing listings
	go h.seedSeenListings(context.Background(), sqliteStore, watch.ID, query)

	session.reply(MsgWatchCreated, query)
}

// seedSeenListings fetches current search results and marks them as seen.
// This prevents notifying about listings that already exist when the watch is created.
func (h *WatchHandler) seedSeenListings(ctx context.Context, store *storage.SQLiteStore, watchID, query string) {
	results, err := h.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query: query,
		Rows:  50, // Seed with first 50 results
	})
	if err != nil {
		log.Warn().Err(err).Str("watchID", watchID).Msg("failed to seed seen listings")
		return
	}

	var listingIDs []string
	for _, doc := range results.Docs {
		listingIDs = append(listingIDs, doc.ID)
	}

	if len(listingIDs) > 0 {
		if err := store.MarkListingsSeenBatch(watchID, listingIDs); err != nil {
			log.Warn().Err(err).Str("watchID", watchID).Msg("failed to mark listings as seen")
			return
		}
		log.Info().Str("watchID", watchID).Int("count", len(listingIDs)).Msg("seeded seen listings")
	}
}

// parseWatchNumber parses a watch number from callback data like "1", "2", etc.
func parseWatchNumber(data string) (int, error) {
	return strconv.Atoi(data)
}

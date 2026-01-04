package watcher

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/rs/zerolog/log"
)

const (
	// PollInterval is the time between polling cycles.
	PollInterval = 10 * time.Minute

	// DelayBetweenQueries is the delay between processing each unique query.
	DelayBetweenQueries = 2 * time.Second

	// MaxResultsPerSearch is the maximum number of results to fetch per search.
	MaxResultsPerSearch = 20

	// PruneInterval is how often to prune old seen listings.
	PruneInterval = 24 * time.Hour

	// SeenListingsMaxAge is how long to keep seen listings before pruning.
	SeenListingsMaxAge = 30 * 24 * time.Hour // 30 days
)

// BotSender abstracts the Telegram bot API for sending messages.
type BotSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// Service is the background watcher service that polls for new listings.
type Service struct {
	store        *storage.SQLiteStore
	searchClient *tori.SearchClient
	bot          BotSender
}

// NewService creates a new watcher service.
func NewService(store *storage.SQLiteStore, bot BotSender) *Service {
	return &Service{
		store:        store,
		searchClient: tori.NewSearchClient(),
		bot:          bot,
	}
}

// Run starts the polling loop. It blocks until the context is cancelled.
func (s *Service) Run(ctx context.Context) {
	log.Info().Dur("interval", PollInterval).Msg("starting watcher service")

	// Run initial poll after a short delay to let the bot fully start
	time.Sleep(5 * time.Second)
	s.poll(ctx)

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	pruneTicker := time.NewTicker(PruneInterval)
	defer pruneTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("watcher service stopped")
			return
		case <-ticker.C:
			s.poll(ctx)
		case <-pruneTicker.C:
			s.pruneOldSeenListings()
		}
	}
}

// poll executes one polling cycle for all watches.
func (s *Service) poll(ctx context.Context) {
	log.Debug().Msg("starting poll cycle")

	watches, err := s.store.GetAllWatches()
	if err != nil {
		log.Error().Err(err).Msg("failed to fetch watches")
		return
	}

	if len(watches) == 0 {
		log.Debug().Msg("no watches to poll")
		return
	}

	// Group watches by query to minimize API calls
	// Map: query -> []Watch
	grouped := make(map[string][]storage.Watch)
	for _, w := range watches {
		grouped[w.Query] = append(grouped[w.Query], w)
	}

	log.Debug().Int("watches", len(watches)).Int("unique_queries", len(grouped)).Msg("processing watches")

	processedQueries := 0
	for query, watchGroup := range grouped {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return
		}

		// Rate limit between queries
		if processedQueries > 0 {
			time.Sleep(DelayBetweenQueries)
		}
		processedQueries++

		s.processQuery(ctx, query, watchGroup)
	}

	log.Debug().Msg("poll cycle complete")
}

// processQuery executes a search and notifies all watches for that query.
func (s *Service) processQuery(ctx context.Context, query string, watches []storage.Watch) {
	log.Debug().Str("query", query).Int("watchers", len(watches)).Msg("searching")

	results, err := s.searchClient.Search(ctx, tori.SearchKeyBapCommon, tori.SearchParams{
		Query: query,
		Rows:  MaxResultsPerSearch,
	})
	if err != nil {
		log.Error().Err(err).Str("query", query).Msg("search failed during poll")
		return
	}

	log.Debug().Str("query", query).Int("results", len(results.Docs)).Msg("search completed")

	// For each watch, check for new listings
	for _, watch := range watches {
		s.processWatchResults(ctx, watch, query, results.Docs)
	}
}

// processWatchResults checks search results against a watch's seen listings and notifies.
func (s *Service) processWatchResults(ctx context.Context, watch storage.Watch, query string, docs []tori.SearchDoc) {
	// Get all seen listing IDs for this watch
	seenIDs, err := s.store.GetSeenListingIDs(watch.ID)
	if err != nil {
		log.Error().Err(err).Str("watchID", watch.ID).Msg("failed to get seen listings")
		return
	}

	// Find new listings
	var newDocs []tori.SearchDoc
	for _, doc := range docs {
		if !seenIDs[doc.ID] {
			newDocs = append(newDocs, doc)
		}
	}

	if len(newDocs) == 0 {
		return
	}

	log.Info().Str("watchID", watch.ID).Int("new", len(newDocs)).Str("query", query).Msg("found new listings")

	// Mark all as seen before sending notifications
	var newIDs []string
	for _, doc := range newDocs {
		newIDs = append(newIDs, doc.ID)
	}
	if err := s.store.MarkListingsSeenBatch(watch.ID, newIDs); err != nil {
		log.Error().Err(err).Str("watchID", watch.ID).Msg("failed to mark listings as seen")
		// Continue anyway - we'll re-notify next time, which is better than silent failure
	}

	// Send notifications (one per listing for MVP)
	for _, doc := range newDocs {
		s.sendNotification(watch.UserID, query, doc)
	}
}

// sendNotification sends a notification message for a new listing.
func (s *Service) sendNotification(userID int64, query string, doc tori.SearchDoc) {
	var sb strings.Builder

	// Header with search query
	sb.WriteString(fmt.Sprintf("🔔 *Uusi ilmoitus:* \"%s\"\n\n", escapeMarkdown(query)))

	// Title
	sb.WriteString(fmt.Sprintf("*%s*\n", escapeMarkdown(doc.Heading)))

	// Price
	if doc.Price != nil && doc.Price.Value != "" {
		sb.WriteString(fmt.Sprintf("💰 %s\n", escapeMarkdown(doc.Price.Value)))
	}

	// Location
	if doc.Location != "" {
		sb.WriteString(fmt.Sprintf("📍 %s\n", escapeMarkdown(doc.Location)))
	}

	// Link button
	var keyboard tgbotapi.InlineKeyboardMarkup
	if doc.CanonicalURL != "" {
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("Avaa Torissa", doc.CanonicalURL),
			),
		)
	}

	msg := tgbotapi.NewMessage(userID, sb.String())
	msg.ParseMode = tgbotapi.ModeMarkdown
	if doc.CanonicalURL != "" {
		msg.ReplyMarkup = keyboard
	}

	_, err := s.bot.Send(msg)
	if err != nil {
		log.Error().
			Err(err).
			Int64("userID", userID).
			Str("listingID", doc.ID).
			Msg("failed to send notification")
	} else {
		log.Debug().
			Int64("userID", userID).
			Str("listingID", doc.ID).
			Msg("notification sent")
	}
}

// pruneOldSeenListings removes old seen listings to prevent database bloat.
func (s *Service) pruneOldSeenListings() {
	count, err := s.store.PruneOldSeenListings(SeenListingsMaxAge)
	if err != nil {
		log.Error().Err(err).Msg("failed to prune old seen listings")
		return
	}
	if count > 0 {
		log.Info().Int64("pruned", count).Msg("pruned old seen listings")
	}
}

// escapeMarkdown escapes special characters for Telegram Markdown V1.
func escapeMarkdown(text string) string {
	text = strings.ReplaceAll(text, "*", "\\*")
	text = strings.ReplaceAll(text, "_", "\\_")
	text = strings.ReplaceAll(text, "`", "\\`")
	text = strings.ReplaceAll(text, "[", "\\[")
	return text
}

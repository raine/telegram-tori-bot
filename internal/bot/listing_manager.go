package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/rs/zerolog/log"
)

const (
	listingsPerPage = 5
)

// ListingManager handles viewing and managing existing listings
type ListingManager struct {
	tg BotAPI
}

// NewListingManager creates a new ListingManager
func NewListingManager(tg BotAPI) *ListingManager {
	return &ListingManager{tg: tg}
}

// HandleIlmoituksetCommand handles the /ilmoitukset command
func (m *ListingManager) HandleIlmoituksetCommand(ctx context.Context, session *UserSession) {
	session.listingBrowsePage = 1
	session.activeListingID = 0
	session.showOldListings = false
	m.refreshListingView(ctx, session, true) // true = send new message
}

// HandleListingCallback routes callbacks starting with "listings:" or "ad:"
func (m *ListingManager) HandleListingCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	data := query.Data

	// Navigation callbacks
	if strings.HasPrefix(data, "listings:page:") {
		pageStr := strings.TrimPrefix(data, "listings:page:")
		page, _ := strconv.Atoi(pageStr)
		session.listingBrowsePage = page
		session.activeListingID = 0
		m.refreshListingView(ctx, session, false)
		return
	}

	if data == "listings:close" {
		m.deleteMenuMessage(session)
		return
	}

	if data == "listings:toggle_old" {
		session.showOldListings = !session.showOldListings
		session.listingBrowsePage = 1 // Reset to page 1 when toggling
		m.refreshListingView(ctx, session, false)
		return
	}

	// View specific ad
	if strings.HasPrefix(data, "listings:view:") {
		adIDStr := strings.TrimPrefix(data, "listings:view:")
		adID, _ := strconv.ParseInt(adIDStr, 10, 64)
		m.showAdDetail(ctx, session, adID)
		return
	}

	// Ad actions
	if strings.HasPrefix(data, "ad:") {
		m.handleAdAction(ctx, session, data)
		return
	}
}

// refreshListingView fetches API data and renders the list
func (m *ListingManager) refreshListingView(ctx context.Context, session *UserSession, forceNewMessage bool) {
	client := session.GetAdInputClient()
	if client == nil {
		session.reply("Kirjaudu sisään ensin.")
		return
	}

	// Always fetch ALL and filter client-side
	// API facets are individual states: ALL, ACTIVE, DRAFT, PENDING, EXPIRED, DISPOSED
	result, err := client.GetAdSummaries(ctx, 100, 0, "ALL") // Fetch all, paginate client-side
	if err != nil {
		session.replyWithError(err)
		return
	}

	// Filter based on showOldListings setting and deleted items
	var filtered []tori.AdSummary
	for _, ad := range result.Summaries {
		// Skip recently deleted ad (API may return stale data)
		if session.deletedListingID != "" && strconv.FormatInt(ad.ID, 10) == session.deletedListingID {
			continue
		}

		if session.showOldListings {
			// Show all
			filtered = append(filtered, ad)
		} else {
			// Only show ACTIVE and PENDING
			if ad.State.Type == "ACTIVE" || ad.State.Type == "PENDING" {
				filtered = append(filtered, ad)
			}
		}
	}

	if len(filtered) == 0 {
		session.reply("Sinulla ei ole ilmoituksia.")
		return
	}

	// Paginate client-side
	total := len(filtered)
	limit := listingsPerPage
	offset := (session.listingBrowsePage - 1) * limit
	end := offset + limit
	if end > total {
		end = total
	}

	session.cachedListings = filtered[offset:end]
	totalPages := (total + limit - 1) / limit

	// Build message text
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*Omat ilmoitukset* — Sivu %d/%d (%d ilmoitusta)\n", session.listingBrowsePage, totalPages, total))

	// Build inline keyboard
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, ad := range session.cachedListings {
		// Status icon
		statusIcon := ""
		switch ad.State.Type {
		case "PENDING":
			statusIcon = "⏳ "
		case "DISPOSED":
			statusIcon = "✅ "
		case "EXPIRED":
			statusIcon = "⏰ "
		}

		// Truncate title if needed (use runes for proper UTF-8 handling)
		title := ad.Data.Title
		maxTitleLen := 50
		titleRunes := []rune(title)
		if len(titleRunes) > maxTitleLen {
			title = string(titleRunes[:maxTitleLen-3]) + "..."
		}

		// Button label with status, title and price
		priceLabel := formatSubtitle(ad.Data.Subtitle)
		label := fmt.Sprintf("%s%s | %s", statusIcon, title, priceLabel)

		// Telegram limits callback data to 64 bytes
		btnData := fmt.Sprintf("listings:view:%d", ad.ID)

		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(label, btnData),
		})
	}

	// Toggle row
	toggleLabel := "Näytä vanhat"
	if session.showOldListings {
		toggleLabel = "Piilota vanhat"
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(toggleLabel, "listings:toggle_old"),
	})

	// Navigation row
	var navRow []tgbotapi.InlineKeyboardButton
	if session.listingBrowsePage > 1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Edellinen", fmt.Sprintf("listings:page:%d", session.listingBrowsePage-1)))
	}
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Sulje", "listings:close"))
	if end < total {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Seuraava", fmt.Sprintf("listings:page:%d", session.listingBrowsePage+1)))
	}
	rows = append(rows, navRow)

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	m.editOrSend(session, sb.String(), markup, forceNewMessage)
}

// showAdDetail renders a single ad detail view
func (m *ListingManager) showAdDetail(ctx context.Context, session *UserSession, adID int64) {
	// Find ad in cache
	var ad *tori.AdSummary
	for i := range session.cachedListings {
		if session.cachedListings[i].ID == adID {
			ad = &session.cachedListings[i]
			break
		}
	}

	if ad == nil {
		// Cache miss - refresh list
		m.refreshListingView(ctx, session, false)
		return
	}

	session.activeListingID = adID

	// Build text
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%s*\n\n", escapeMarkdown(ad.Data.Title)))
	sb.WriteString(fmt.Sprintf("💰 %s\n", escapeMarkdown(formatSubtitle(ad.Data.Subtitle))))

	// Stats (handle empty values)
	clicks := ad.ExternalData.Clicks.Value
	if clicks == "" {
		clicks = "0"
	}
	favorites := ad.ExternalData.Favorites.Value
	if favorites == "" {
		favorites = "0"
	}
	sb.WriteString(fmt.Sprintf("👁 %s | ❤️ %s\n", clicks, favorites))

	// Status
	if ad.State.Type == "PENDING" {
		sb.WriteString("⏳ Tarkistettavana\n")
	} else if ad.State.Type == "ACTIVE" {
		if ad.DaysUntilExpires > 0 {
			sb.WriteString(fmt.Sprintf("⏰ Vanhenee %d päivässä\n", ad.DaysUntilExpires))
		}
	} else {
		sb.WriteString(fmt.Sprintf("📋 Tila: %s\n", ad.State.Label))
	}

	// Build action buttons based on available actions
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, action := range ad.Actions {
		var btnLabel, btnData string

		switch action.Name {
		case "DISPOSE":
			// Can't mark as sold while in review
			if ad.State.Type == "PENDING" {
				continue
			}
			btnLabel = "Merkitse myydyksi"
			btnData = fmt.Sprintf("ad:action:DISPOSE:%d", ad.ID)
		case "UNDISPOSE":
			btnLabel = "Aktivoi uudelleen"
			btnData = fmt.Sprintf("ad:action:UNDISPOSE:%d", ad.ID)
		case "DELETE":
			btnLabel = "Poista"
			btnData = fmt.Sprintf("ad:confirm_delete:%d", ad.ID)
			// Skip EDIT, STATISTICS, OBJECT_PAGE, PAUSE for v1
		}

		if btnLabel != "" {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData(btnLabel, btnData),
			})
		}
	}

	// Back button
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("Takaisin", fmt.Sprintf("listings:page:%d", session.listingBrowsePage)),
	})

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	m.editOrSend(session, sb.String(), markup, false)
}

// handleAdAction processes ad action callbacks (ad:action:*, ad:confirm_delete:*)
func (m *ListingManager) handleAdAction(ctx context.Context, session *UserSession, data string) {
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		return
	}

	actionType := parts[1] // "action", "confirm_delete"

	switch actionType {
	case "confirm_delete":
		adIDStr := parts[2]
		m.showDeleteConfirmation(ctx, session, adIDStr)

	case "action":
		if len(parts) < 4 {
			return
		}
		actionName := parts[2]
		adIDStr := parts[3]
		m.executeAction(ctx, session, actionName, adIDStr)
	}
}

// showDeleteConfirmation displays delete confirmation prompt
func (m *ListingManager) showDeleteConfirmation(ctx context.Context, session *UserSession, adIDStr string) {
	// Find ad title for confirmation message
	adID, _ := strconv.ParseInt(adIDStr, 10, 64)
	var adTitle string
	for _, ad := range session.cachedListings {
		if ad.ID == adID {
			adTitle = ad.Data.Title
			break
		}
	}

	text := fmt.Sprintf("⚠️ *Haluatko varmasti poistaa ilmoituksen?*\n\n\"%s\"\n\nTätä ei voi perua.", escapeMarkdown(adTitle))

	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("Poista pysyvästi", fmt.Sprintf("ad:action:DELETE:%s", adIDStr))},
		{tgbotapi.NewInlineKeyboardButtonData("Peruuta", fmt.Sprintf("listings:view:%s", adIDStr))},
	}

	m.editOrSend(session, text, tgbotapi.NewInlineKeyboardMarkup(rows...), false)
}

// executeAction executes an ad action via the API
func (m *ListingManager) executeAction(ctx context.Context, session *UserSession, actionName, adIDStr string) {
	client := session.GetAdInputClient()
	if client == nil {
		session.reply("Kirjaudu sisään ensin.")
		return
	}

	var err error
	switch actionName {
	case "DISPOSE":
		err = client.DisposeAd(ctx, adIDStr)
	case "UNDISPOSE":
		err = client.UndisposeAd(ctx, adIDStr)
	case "DELETE":
		err = client.DeleteAd(ctx, adIDStr)
	default:
		session.reply("Tuntematon toiminto: " + actionName)
		return
	}

	if err != nil {
		log.Error().Err(err).Str("action", actionName).Str("adID", adIDStr).Msg("action failed")
		session.reply(fmt.Sprintf("❌ Toiminto epäonnistui: %s", err.Error()))
		return
	}

	log.Info().Str("action", actionName).Str("adID", adIDStr).Msg("action executed successfully")

	// After successful action, refresh the view
	if actionName == "DELETE" {
		// Go back to list after deletion
		session.activeListingID = 0
		session.deletedListingID = adIDStr // Track deleted ID to filter from stale API data
		m.refreshListingView(ctx, session, false)
		session.deletedListingID = "" // Clear after refresh
	} else {
		// Refresh list to get updated state, then show detail again
		adID, _ := strconv.ParseInt(adIDStr, 10, 64)

		// Fetch fresh data
		client := session.GetAdInputClient()
		if client == nil {
			return
		}

		limit := listingsPerPage
		offset := (session.listingBrowsePage - 1) * limit
		result, err := client.GetAdSummaries(ctx, limit, offset, "ALL")
		if err != nil {
			session.replyWithError(err)
			return
		}
		session.cachedListings = result.Summaries

		// Show detail view again with fresh data
		m.showAdDetail(ctx, session, adID)
	}
}

// editOrSend edits the existing menu message or sends a new one
func (m *ListingManager) editOrSend(session *UserSession, text string, markup tgbotapi.InlineKeyboardMarkup, forceNew bool) {
	if !forceNew && session.listingMenuMsgID != 0 {
		edit := tgbotapi.NewEditMessageTextAndMarkup(
			session.userId,
			session.listingMenuMsgID,
			text,
			markup,
		)
		edit.ParseMode = tgbotapi.ModeMarkdown

		_, err := m.tg.Request(edit)
		if err == nil {
			return // Success
		}

		// Ignore "message is not modified" error
		if strings.Contains(err.Error(), "message is not modified") {
			return
		}

		// For other errors (message too old, deleted), fall through to send new
		log.Warn().Err(err).Int("msgID", session.listingMenuMsgID).Msg("failed to edit listing menu")
	}

	// Send new message
	msg := tgbotapi.NewMessage(session.userId, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = markup

	sent, err := m.tg.Send(msg)
	if err != nil {
		log.Error().Err(err).Msg("failed to send listing menu")
		return
	}

	// Delete old message if exists (to keep chat clean)
	if session.listingMenuMsgID != 0 {
		m.tg.Request(tgbotapi.NewDeleteMessage(session.userId, session.listingMenuMsgID))
	}

	session.listingMenuMsgID = sent.MessageID
}

// deleteMenuMessage deletes the listing menu message
func (m *ListingManager) deleteMenuMessage(session *UserSession) {
	if session.listingMenuMsgID != 0 {
		m.tg.Request(tgbotapi.NewDeleteMessage(session.userId, session.listingMenuMsgID))
		session.listingMenuMsgID = 0
	}
	session.activeListingID = 0
	session.cachedListings = nil
}

// formatSubtitle converts API subtitle to display format
// "Tori myydään 200 €" -> "200 €"
// "Tori annetaan" -> "Annetaan"
func formatSubtitle(subtitle string) string {
	if strings.HasPrefix(subtitle, "Tori myydään ") {
		return strings.TrimPrefix(subtitle, "Tori myydään ")
	}
	if subtitle == "Tori annetaan" {
		return "Annetaan"
	}
	return subtitle
}

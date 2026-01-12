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
	session.listings.BrowsePage = 1
	session.listings.ActiveListingID = 0
	session.listings.ShowOldListings = false
	m.refreshListingView(ctx, session, true) // true = send new message
}

// HandleListingCallback routes callbacks starting with "listings:" or "ad:"
func (m *ListingManager) HandleListingCallback(ctx context.Context, session *UserSession, query *tgbotapi.CallbackQuery) {
	data := query.Data

	// Navigation callbacks
	if strings.HasPrefix(data, "listings:page:") {
		pageStr := strings.TrimPrefix(data, "listings:page:")
		page, _ := strconv.Atoi(pageStr)
		session.listings.BrowsePage = page
		session.listings.ActiveListingID = 0
		m.refreshListingView(ctx, session, false)
		return
	}

	if data == "listings:close" {
		m.deleteMenuMessage(session)
		return
	}

	if data == "listings:toggle_old" {
		session.listings.ShowOldListings = !session.listings.ShowOldListings
		session.listings.BrowsePage = 1 // Reset to page 1 when toggling
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
		session.reply(MsgLoginFirstRequired)
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
		if session.listings.DeletedListingID != "" && strconv.FormatInt(ad.ID, 10) == session.listings.DeletedListingID {
			continue
		}

		if session.listings.ShowOldListings {
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
		session.reply(MsgNoListings)
		return
	}

	// Paginate client-side
	total := len(filtered)
	limit := listingsPerPage
	offset := (session.listings.BrowsePage - 1) * limit
	end := offset + limit
	if end > total {
		end = total
	}

	session.listings.CachedListings = filtered[offset:end]
	totalPages := (total + limit - 1) / limit

	// Build message text
	var sb strings.Builder
	countLabel := MsgListingsCountPlural
	if total == 1 {
		countLabel = MsgListingsCountSingle
	}
	sb.WriteString(fmt.Sprintf(MsgListingsHeader, session.listings.BrowsePage, totalPages, total, countLabel))

	// Build inline keyboard
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, ad := range session.listings.CachedListings {
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
	toggleLabel := BtnShowOld
	if session.listings.ShowOldListings {
		toggleLabel = BtnHideOld
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(toggleLabel, "listings:toggle_old"),
	})

	// Navigation row
	var navRow []tgbotapi.InlineKeyboardButton
	if session.listings.BrowsePage > 1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(BtnPrev, fmt.Sprintf("listings:page:%d", session.listings.BrowsePage-1)))
	}
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(BtnClose, "listings:close"))
	if end < total {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(BtnNext, fmt.Sprintf("listings:page:%d", session.listings.BrowsePage+1)))
	}
	rows = append(rows, navRow)

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	m.editOrSend(session, sb.String(), markup, forceNewMessage)
}

// showAdDetail renders a single ad detail view
func (m *ListingManager) showAdDetail(ctx context.Context, session *UserSession, adID int64) {
	// Find ad in cache
	var ad *tori.AdSummary
	for i := range session.listings.CachedListings {
		if session.listings.CachedListings[i].ID == adID {
			ad = &session.listings.CachedListings[i]
			break
		}
	}

	if ad == nil {
		// Cache miss - refresh list
		m.refreshListingView(ctx, session, false)
		return
	}

	session.listings.ActiveListingID = adID

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
		sb.WriteString(MsgListingPending)
	} else if ad.State.Type == "ACTIVE" {
		if ad.DaysUntilExpires > 0 {
			sb.WriteString(fmt.Sprintf(MsgListingExpiresDays, ad.DaysUntilExpires))
		}
	} else {
		sb.WriteString(fmt.Sprintf(MsgListingStateFmt, ad.State.Label))
	}

	// Build action buttons based on available actions
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, action := range ad.Actions {
		var btnLabel, btnData string

		switch action.Name {
		case "DISPOSE":
			// Can't mark as sold while in review or expired
			if ad.State.Type == "PENDING" || ad.State.Type == "EXPIRED" {
				continue
			}
			btnLabel = BtnMarkAsSold
			btnData = fmt.Sprintf("ad:action:DISPOSE:%d", ad.ID)
		case "UNDISPOSE":
			btnLabel = BtnReactivate
			btnData = fmt.Sprintf("ad:action:UNDISPOSE:%d", ad.ID)
		case "DELETE":
			// Add republish button for expired listings (before delete)
			if ad.State.Type == "EXPIRED" {
				rows = append(rows, []tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData(BtnRepublish, fmt.Sprintf("ad:republish:%d", ad.ID)),
				})
			}
			btnLabel = BtnDelete
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
		tgbotapi.NewInlineKeyboardButtonData(BtnBack, fmt.Sprintf("listings:page:%d", session.listings.BrowsePage)),
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

	case "republish":
		adIDStr := parts[2]
		m.startRepublish(ctx, session, adIDStr)
	}
}

// showDeleteConfirmation displays delete confirmation prompt
func (m *ListingManager) showDeleteConfirmation(ctx context.Context, session *UserSession, adIDStr string) {
	// Find ad title for confirmation message
	adID, _ := strconv.ParseInt(adIDStr, 10, 64)
	var adTitle string
	for _, ad := range session.listings.CachedListings {
		if ad.ID == adID {
			adTitle = ad.Data.Title
			break
		}
	}

	text := fmt.Sprintf(MsgConfirmDelete, escapeMarkdown(adTitle))

	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(BtnDeleteConfirm, fmt.Sprintf("ad:action:DELETE:%s", adIDStr))},
		{tgbotapi.NewInlineKeyboardButtonData(BtnCancel, fmt.Sprintf("listings:view:%s", adIDStr))},
	}

	m.editOrSend(session, text, tgbotapi.NewInlineKeyboardMarkup(rows...), false)
}

// executeAction executes an ad action via the API
func (m *ListingManager) executeAction(ctx context.Context, session *UserSession, actionName, adIDStr string) {
	client := session.GetAdInputClient()
	if client == nil {
		session.reply(MsgLoginFirstRequired)
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
		session.reply(MsgUnknownAction + actionName)
		return
	}

	if err != nil {
		log.Error().Err(err).Str("action", actionName).Str("adID", adIDStr).Msg("action failed")
		session.reply(MsgActionFailed, err.Error())
		return
	}

	log.Info().Str("action", actionName).Str("adID", adIDStr).Msg("action executed successfully")

	// Show success feedback
	switch actionName {
	case "DISPOSE":
		session.reply(MsgMarkedAsSold)
	case "UNDISPOSE":
		session.reply(MsgReactivated)
	}

	// After successful action, refresh the view
	if actionName == "DELETE" {
		// Go back to list after deletion
		session.listings.ActiveListingID = 0
		session.listings.DeletedListingID = adIDStr // Track deleted ID to filter from stale API data
		m.refreshListingView(ctx, session, false)
		session.listings.DeletedListingID = "" // Clear after refresh
	} else if actionName == "UNDISPOSE" {
		// Go back to list after reactivation
		session.listings.ActiveListingID = 0
		m.refreshListingView(ctx, session, false)
	} else {
		// Refresh list to get updated state, then show detail again
		adID, _ := strconv.ParseInt(adIDStr, 10, 64)

		// Fetch fresh data
		client := session.GetAdInputClient()
		if client == nil {
			return
		}

		limit := listingsPerPage
		offset := (session.listings.BrowsePage - 1) * limit
		result, err := client.GetAdSummaries(ctx, limit, offset, "ALL")
		if err != nil {
			session.replyWithError(err)
			return
		}
		session.listings.CachedListings = result.Summaries

		// Show detail view again with fresh data
		m.showAdDetail(ctx, session, adID)
	}
}

// editOrSend edits the existing menu message or sends a new one
func (m *ListingManager) editOrSend(session *UserSession, text string, markup tgbotapi.InlineKeyboardMarkup, forceNew bool) {
	if !forceNew && session.listings.MenuMsgID != 0 {
		edit := tgbotapi.NewEditMessageTextAndMarkup(
			session.userId,
			session.listings.MenuMsgID,
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
		log.Warn().Err(err).Int("msgID", session.listings.MenuMsgID).Msg("failed to edit listing menu")
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
	if session.listings.MenuMsgID != 0 {
		m.tg.Request(tgbotapi.NewDeleteMessage(session.userId, session.listings.MenuMsgID))
	}

	session.listings.MenuMsgID = sent.MessageID
}

// deleteMenuMessage deletes the listing menu message
func (m *ListingManager) deleteMenuMessage(session *UserSession) {
	if session.listings.MenuMsgID != 0 {
		m.tg.Request(tgbotapi.NewDeleteMessage(session.userId, session.listings.MenuMsgID))
		session.listings.MenuMsgID = 0
	}
	session.listings.ActiveListingID = 0
	session.listings.CachedListings = nil
}

// formatSubtitle converts API subtitle to display format
// "Tori myydään 200 €" -> "200 €"
// "Tori annetaan" -> "Annetaan"
func formatSubtitle(subtitle string) string {
	if strings.HasPrefix(subtitle, "Tori myydään ") {
		return strings.TrimPrefix(subtitle, "Tori myydään ")
	}
	if subtitle == "Tori annetaan" {
		return BtnBulkGiveaway // "Annetaan"
	}
	return subtitle
}

// startRepublish creates a new ad with the same data as an expired ad
func (m *ListingManager) startRepublish(ctx context.Context, session *UserSession, adIDStr string) {
	client := session.GetAdInputClient()
	if client == nil {
		session.reply(MsgLoginFirstRequired)
		return
	}

	// Show progress message
	m.editOrSend(session, MsgRepublishProgress, tgbotapi.InlineKeyboardMarkup{}, false)

	// Fetch full ad data
	oldAd, err := client.GetAdWithModel(ctx, adIDStr)
	if err != nil {
		log.Error().Err(err).Str("adID", adIDStr).Msg("failed to fetch ad for republish")
		session.reply(MsgRepublishFetchError, err.Error())
		return
	}

	values := oldAd.Ad.Values

	// Create new draft
	draft, _, err := client.CreateDraftAd(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to create draft for republish")
		session.reply(MsgRepublishCreateError, err.Error())
		return
	}

	etag := draft.ETag

	// Download and upload images from multi_image
	var uploadedImages []republishImage
	if multiImages, ok := values["multi_image"].([]any); ok {
		for i, img := range multiImages {
			imgMap, ok := img.(map[string]any)
			if !ok {
				continue
			}
			imgURL, _ := imgMap["url"].(string)
			if imgURL == "" {
				continue
			}

			// Download image
			imageData, err := DownloadImage(ctx, imgURL)
			if err != nil {
				log.Warn().Err(err).Str("url", imgURL).Int("index", i).Msg("failed to download image, skipping")
				continue
			}

			// Upload to new draft
			uploadResp, err := client.UploadImage(ctx, draft.ID, imageData)
			if err != nil {
				log.Warn().Err(err).Int("index", i).Msg("failed to upload image, skipping")
				continue
			}

			// Preserve dimensions from original if available
			width, _ := imgMap["width"].(float64)
			height, _ := imgMap["height"].(float64)
			imgType, _ := imgMap["type"].(string)
			if imgType == "" {
				imgType = "image/jpeg"
			}

			uploadedImages = append(uploadedImages, republishImage{
				path:     uploadResp.ImagePath,
				location: uploadResp.Location,
				width:    int(width),
				height:   int(height),
				imgType:  imgType,
			})
		}
	}

	if len(uploadedImages) == 0 {
		log.Warn().Str("adID", adIDStr).Msg("no images could be transferred for republish")
	}

	// Extract values from old ad
	category, _ := values["category"].(string)
	title, _ := values["title"].(string)
	description, _ := values["description"].(string)
	tradeType, _ := values["trade_type"].(string)
	condition, _ := values["condition"].(string)

	// Build location array
	var locationArr []map[string]string
	if locData, ok := values["location"].([]any); ok {
		for _, loc := range locData {
			if locMap, ok := loc.(map[string]any); ok {
				entry := make(map[string]string)
				if country, ok := locMap["country"].(string); ok {
					entry["country"] = country
				}
				if postalCode, ok := locMap["postal-code"].(string); ok {
					entry["postal-code"] = postalCode
				}
				if len(entry) > 0 {
					locationArr = append(locationArr, entry)
				}
			}
		}
	}

	// Build image arrays for update payload
	var imageArr []map[string]string
	var multiImageArr []map[string]any
	for _, img := range uploadedImages {
		imageArr = append(imageArr, map[string]string{
			"uri":    img.path,
			"width":  strconv.Itoa(img.width),
			"height": strconv.Itoa(img.height),
			"type":   img.imgType,
		})
		multiImageArr = append(multiImageArr, map[string]any{
			"path":        img.path,
			"url":         img.location,
			"width":       img.width,
			"height":      img.height,
			"type":        img.imgType,
			"description": "",
		})
	}

	// Build update payload
	payload := tori.AdUpdatePayload{
		Category:    category,
		Title:       title,
		Description: description,
		TradeType:   tradeType,
		Condition:   condition,
		Location:    locationArr,
		Image:       imageArr,
		MultiImage:  multiImageArr,
		Extra:       make(map[string]any),
	}

	// Copy dynamic category-specific attributes from old ad
	// These are excluded from Extra since they're handled separately
	ignoredKeys := map[string]bool{
		"category": true, "title": true, "description": true,
		"trade_type": true, "condition": true, "location": true,
		"multi_image": true, "image": true, "price": true,
		// Internal/system fields
		"id": true, "ad_id": true, "list_id": true, "status": true,
		"phone": true, "account": true, "type": true, "images": true,
	}
	for k, v := range values {
		if !ignoredKeys[k] {
			payload.Extra[k] = v
		}
	}

	// Copy price if present (overwrite any copied value with proper format)
	if priceData, ok := values["price"].([]any); ok && len(priceData) > 0 {
		var priceArr []map[string]any
		for _, p := range priceData {
			if priceMap, ok := p.(map[string]any); ok {
				entry := make(map[string]any)
				if amount, ok := priceMap["price_amount"].(string); ok {
					entry["price_amount"] = amount
				} else if amount, ok := priceMap["price_amount"].(float64); ok {
					entry["price_amount"] = strconv.Itoa(int(amount))
				}
				if len(entry) > 0 {
					priceArr = append(priceArr, entry)
				}
			}
		}
		if len(priceArr) > 0 {
			payload.Extra["price"] = priceArr
		}
	}

	// Patch all fields to /items (required for review system)
	fields := buildItemFieldsFromValues(values)
	_, err = client.PatchItemFields(ctx, draft.ID, etag, fields)
	if err != nil {
		log.Error().Err(err).Str("draftID", draft.ID).Msg("failed to patch item fields for republish")
		session.reply(MsgRepublishUpdateError, err.Error())
		return
	}

	// Get fresh ETag from adinput service (iOS does this before update)
	adWithModel, err := client.GetAdWithModel(ctx, draft.ID)
	if err != nil {
		log.Error().Err(err).Str("draftID", draft.ID).Msg("failed to get fresh etag for republish")
		session.reply(MsgRepublishUpdateError, err.Error())
		return
	}

	// Update the draft (with fresh etag)
	updateResp, err := client.UpdateAd(ctx, draft.ID, adWithModel.Ad.ETag, payload)
	if err != nil {
		log.Error().Err(err).Str("draftID", draft.ID).Msg("failed to update draft for republish")
		session.reply(MsgRepublishUpdateError, err.Error())
		return
	}
	_ = updateResp // etag not needed after this

	// Set delivery options (default to meetup only)
	err = client.SetDeliveryOptions(ctx, draft.ID, tori.DeliveryOptions{
		BuyNow:             false,
		Client:             "ANDROID",
		Meetup:             true,
		SellerPaysShipping: false,
		Shipping:           false,
	})
	if err != nil {
		log.Error().Err(err).Str("draftID", draft.ID).Msg("failed to set delivery options for republish")
		session.reply(MsgRepublishDeliveryErr, err.Error())
		return
	}

	// Publish the ad
	_, err = client.PublishAd(ctx, draft.ID)
	if err != nil {
		log.Error().Err(err).Str("draftID", draft.ID).Msg("failed to publish republished ad")
		session.reply(MsgRepublishPublishErr, err.Error())
		return
	}

	log.Info().Str("oldAdID", adIDStr).Str("newAdID", draft.ID).Msg("ad republished successfully")

	// Show success message and refresh the list
	session.reply(MsgRepublishSuccess)
	session.listings.ActiveListingID = 0
	m.refreshListingView(ctx, session, true)
}

// republishImage holds data for an uploaded image during republish
type republishImage struct {
	path     string
	location string
	width    int
	height   int
	imgType  string
}

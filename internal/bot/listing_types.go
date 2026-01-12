package bot

import (
	"fmt"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/rs/zerolog/log"
)

// AdFlowState tracks where we are in the ad creation flow
type AdFlowState int

const (
	AdFlowStateNone AdFlowState = iota
	AdFlowStateAwaitingCategory
	AdFlowStateAwaitingAttribute
	AdFlowStateAwaitingPrice
	AdFlowStateAwaitingShipping
	AdFlowStateAwaitingPackageSize
	AdFlowStateAwaitingPostalCode
	AdFlowStateReadyToPublish
)

// String returns a human-readable name for the AdFlowState.
func (s AdFlowState) String() string {
	switch s {
	case AdFlowStateNone:
		return "None"
	case AdFlowStateAwaitingCategory:
		return "AwaitingCategory"
	case AdFlowStateAwaitingAttribute:
		return "AwaitingAttribute"
	case AdFlowStateAwaitingPrice:
		return "AwaitingPrice"
	case AdFlowStateAwaitingShipping:
		return "AwaitingShipping"
	case AdFlowStateAwaitingPackageSize:
		return "AwaitingPackageSize"
	case AdFlowStateAwaitingPostalCode:
		return "AwaitingPostalCode"
	case AdFlowStateReadyToPublish:
		return "ReadyToPublish"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// TradeType constants for listing type
const (
	TradeTypeSell = "1" // Selling an item
	TradeTypeGive = "2" // Giving away for free

	// SkipButtonLabel is the label for the skip button in attribute selection
	SkipButtonLabel = "Ohita"
)

// Package size constants for Tori Diili shipping
const (
	PackageSizeSmall  = "SMALL"  // Max 4kg, 40x32x15cm
	PackageSizeMedium = "MEDIUM" // Max 25kg, 40x32x26cm
	PackageSizeLarge  = "LARGE"  // Max 25kg, 100x60x60cm
)

// Shipping product codes for Tori Diili carriers
const (
	ProductMatkahuoltoShop = "MATKAHUOLTO_SHOP" // Matkahuolto for all sizes
	ProductKotipaketti     = "KOTIPAKETTI"      // Posti small package
	ProductPostipaketti    = "POSTIPAKETTI"     // Posti medium/large package
)

// AdInputDraft tracks the state of a new-API ad creation
type AdInputDraft struct {
	State            AdFlowState
	CategoryID       int
	Title            string
	Description      string
	TemplateContent  string // Template content stored separately to preserve during LLM edits
	TradeType        string // "1" = sell, "2" = give away
	Price            int
	ShippingPossible bool

	// Tori Diili shipping details (fetched from Tori API)
	SavedShippingAddress *tori.SavedAddress
	PackageSize          string // SMALL, MEDIUM, or LARGE

	// Message IDs for editing via reply
	TitleMessageID       int
	DescriptionMessageID int
	SummaryMessageID     int

	// Image data
	Images []UploadedImage

	// Category predictions for selection
	CategoryPredictions []tori.CategoryPrediction

	// Dynamic attributes collected from user
	CollectedAttrs map[string]string

	// Attribute collection state
	RequiredAttrs    []tori.Attribute // Attributes that need user input
	CurrentAttrIndex int              // Which attribute we're currently asking about

	// Expiration timer for automatic draft cleanup
	ExpirationTimer *time.Timer

	// Preserved values when changing category - used to skip prompts for already-set values
	PreservedValues *PreservedValues
}

// GetFullDescription returns the description combined with template content if present.
func (d *AdInputDraft) GetFullDescription() string {
	if d.TemplateContent == "" {
		return d.Description
	}
	return d.Description + "\n\n" + d.TemplateContent
}

// PreservedValues holds values to preserve when changing category
type PreservedValues struct {
	Price            int
	TradeType        string
	ShippingPossible bool
	ShippingSet      bool              // Tracks if shipping was explicitly set (to distinguish false from unset)
	CollectedAttrs   map[string]string // Collected attributes to preserve (e.g., condition)
}

// UploadedImage holds info about an uploaded image
type UploadedImage struct {
	ImagePath string
	Location  string
	Width     int
	Height    int
}

// initAdInputClient initializes the AdinputClient for the session if not already done.
// It retrieves or generates a unique installation ID per user for the finn-app-installation-id header.
// NOTE: This method must be called with s.mu held (use GetAdInputClient() for thread-safe access).
func (s *UserSession) initAdInputClient() {
	if s.draft.AdInputClient == nil && s.auth.BearerToken != "" {
		installationID := getOrCreateInstallationID(s.store, s.userId)
		s.draft.AdInputClient = tori.NewAdinputClient(s.auth.BearerToken, installationID)
	}
}

// getOrCreateInstallationID retrieves the installation ID for a user from storage,
// or generates and stores a new UUID if none exists.
func getOrCreateInstallationID(store storage.SessionStore, telegramID int64) string {
	if store == nil {
		// Fallback to generating a new UUID each time if no store
		return uuid.New().String()
	}

	installationID, err := store.GetInstallationID(telegramID)
	if err != nil {
		log.Error().Err(err).Int64("telegramID", telegramID).Msg("failed to get installation ID")
		return uuid.New().String()
	}

	if installationID == "" {
		// Generate and store a new UUID
		installationID = uuid.New().String()
		if err := store.SetInstallationID(telegramID, installationID); err != nil {
			log.Error().Err(err).Int64("telegramID", telegramID).Msg("failed to save installation ID")
		}
	}

	return installationID
}

// Common errors
var (
	ErrNotLoggedIn    = fmt.Errorf("user not logged in")
	ErrNoRefreshToken = fmt.Errorf("no refresh token available")
	ErrNoDeviceID     = fmt.Errorf("no device ID available")
	ErrNoDraft        = fmt.Errorf("no active draft")
)

var emojiNumbers = []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"}

// makeCategoryPredictionKeyboard creates an inline keyboard for category selection
func makeCategoryPredictionKeyboard(categories []tori.CategoryPrediction) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for i, cat := range categories {
		// Show last 2 levels of category path for cleaner display
		displayText := tori.GetCategoryPathLastN(cat, 2)
		callbackData := fmt.Sprintf("cat:%d", cat.ID)

		// Use emoji number if available, otherwise fall back to bracketed number
		prefix := fmt.Sprintf("[%d]", i+1)
		if i < len(emojiNumbers) {
			prefix = emojiNumbers[i]
		}

		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s %s", prefix, displayText),
			callbackData,
		)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{button})
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// makeAttributeKeyboard creates a reply keyboard for attribute selection.
// Adds an "Ohita" (Skip) button to allow users to skip optional attributes.
func makeAttributeKeyboard(attr tori.Attribute) tgbotapi.ReplyKeyboardMarkup {
	buttonsPerRow := 3
	var rows [][]tgbotapi.KeyboardButton

	for i := 0; i < len(attr.Options); i += buttonsPerRow {
		end := i + buttonsPerRow
		if end > len(attr.Options) {
			end = len(attr.Options)
		}

		var row []tgbotapi.KeyboardButton
		for _, opt := range attr.Options[i:end] {
			row = append(row, tgbotapi.NewKeyboardButton(opt.Label))
		}
		rows = append(rows, row)
	}

	// Add skip button to allow skipping optional attributes
	rows = append(rows, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton(SkipButtonLabel),
	))

	return tgbotapi.NewOneTimeReplyKeyboard(rows...)
}

// buildFinalPayload builds the final ad update payload
func buildFinalPayload(
	draft *AdInputDraft,
	images []UploadedImage,
	postalCode string,
) tori.AdUpdatePayload {
	// Build image arrays
	imageArr := make([]map[string]string, len(images))
	multiImageArr := make([]map[string]any, len(images))

	for i, img := range images {
		imageArr[i] = map[string]string{
			"uri":    img.ImagePath,
			"width":  strconv.Itoa(img.Width),
			"height": strconv.Itoa(img.Height),
			"type":   "image/jpg",
		}
		multiImageArr[i] = map[string]any{
			"path":        img.ImagePath,
			"url":         img.Location,
			"width":       img.Width,
			"height":      img.Height,
			"type":        "image/jpg",
			"description": "",
		}
	}

	payload := tori.AdUpdatePayload{
		Category:    strconv.Itoa(draft.CategoryID),
		Title:       draft.Title,
		Description: draft.GetFullDescription(),
		TradeType:   draft.TradeType,
		Condition:   draft.CollectedAttrs["condition"], // Set condition directly (reserved key, not via Extra)
		Location: []map[string]string{
			{"country": "FI", "postal-code": postalCode},
		},
		Image:      imageArr,
		MultiImage: multiImageArr,
		Extra:      make(map[string]any),
	}

	// Add collected attributes (except condition which is set directly above)
	for k, v := range draft.CollectedAttrs {
		if k != "condition" {
			payload.Extra[k] = v
		}
	}

	// Add price if selling (not for giveaways)
	if draft.TradeType == TradeTypeSell && draft.Price > 0 {
		payload.Extra["price"] = []map[string]any{
			{"price_amount": strconv.Itoa(draft.Price)},
		}
	}

	return payload
}

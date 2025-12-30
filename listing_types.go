package main

import (
	"fmt"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
)

// AdFlowState tracks where we are in the ad creation flow
type AdFlowState int

const (
	AdFlowStateNone AdFlowState = iota
	AdFlowStateAwaitingCategory
	AdFlowStateAwaitingAttribute
	AdFlowStateAwaitingPrice
	AdFlowStateAwaitingShipping
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
)

// AdInputDraft tracks the state of a new-API ad creation
type AdInputDraft struct {
	State            AdFlowState
	CategoryID       int
	Title            string
	Description      string
	TradeType        string // "1" = sell, "2" = give away
	Price            int
	ShippingPossible bool

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

// initAdInputClient initializes the AdinputClient for the session if not already done
func (s *UserSession) initAdInputClient() {
	if s.adInputClient == nil && s.bearerToken != "" {
		s.adInputClient = tori.NewAdinputClient(s.bearerToken)
	}
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

// makeAttributeKeyboard creates a reply keyboard for attribute selection
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
		Description: draft.Description,
		TradeType:   draft.TradeType,
		Location: []map[string]string{
			{"country": "FI", "postal-code": postalCode},
		},
		Image:      imageArr,
		MultiImage: multiImageArr,
		Extra:      make(map[string]any),
	}

	// Add collected attributes
	for k, v := range draft.CollectedAttrs {
		payload.Extra[k] = v
	}

	// Add price if selling (not for giveaways)
	if draft.TradeType == TradeTypeSell && draft.Price > 0 {
		payload.Extra["price"] = []map[string]any{
			{"price_amount": strconv.Itoa(draft.Price)},
		}
	}

	return payload
}

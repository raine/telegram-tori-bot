package main

import (
	"context"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
)

// AdInputDraft tracks the state of a new-API ad creation
type AdInputDraft struct {
	DraftID     string
	ETag        string
	CategoryID  int
	Title       string
	Description string
	TradeType   string // "1" = sell, "2" = give away
	Price       int
	PostalCode  string

	// Image data
	Images []UploadedImage

	// Dynamic attributes collected from user
	CollectedAttrs map[string]string

	// Current attribute being asked (index into required attrs)
	CurrentAttrIndex int
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
	if s.adInputClient == nil && s.client != nil {
		// Get bearer token from existing client auth header
		// The client stores auth as "Bearer <token>", extract just the token
		auth := s.client.GetAuth()
		if len(auth) > 7 && auth[:7] == "Bearer " {
			bearerToken := auth[7:]
			s.adInputClient = tori.NewAdinputClient(bearerToken)
		}
	}
}

// ErrNotLoggedIn is returned when the user tries to create an ad without being logged in
var ErrNotLoggedIn = fmt.Errorf("user not logged in")

// startNewAdFlow begins the new API ad creation flow
func (b *Bot) startNewAdFlow(ctx context.Context, session *UserSession) error {
	if session.client == nil {
		return ErrNotLoggedIn
	}
	session.initAdInputClient()
	if session.adInputClient == nil {
		return fmt.Errorf("could not initialize ad input client: failed to extract bearer token")
	}

	// Create a draft ad
	log.Info().Int64("userId", session.userId).Msg("creating draft ad")
	draft, err := session.adInputClient.CreateDraftAd(ctx)
	if err != nil {
		return fmt.Errorf("failed to create draft: %w", err)
	}

	session.draftID = draft.ID
	session.etag = draft.ETag

	log.Info().
		Int64("userId", session.userId).
		Str("draftId", draft.ID).
		Msg("draft ad created")

	return nil
}

// uploadPhotoToAd uploads a photo to the draft ad
func (b *Bot) uploadPhotoToAd(ctx context.Context, session *UserSession, photoData []byte, width, height int) (*UploadedImage, error) {
	if session.draftID == "" {
		return nil, fmt.Errorf("no draft ad to upload to")
	}

	resp, err := session.adInputClient.UploadImage(ctx, session.draftID, photoData)
	if err != nil {
		return nil, fmt.Errorf("failed to upload image: %w", err)
	}

	return &UploadedImage{
		ImagePath: resp.ImagePath,
		Location:  resp.Location,
		Width:     width,
		Height:    height,
	}, nil
}

// setImageOnDraft sets the uploaded image(s) on the draft
func (b *Bot) setImageOnDraft(ctx context.Context, session *UserSession, images []UploadedImage) error {
	if len(images) == 0 {
		return nil
	}

	imageData := make([]map[string]any, len(images))
	for i, img := range images {
		imageData[i] = map[string]any{
			"uri":    img.ImagePath,
			"width":  img.Width,
			"height": img.Height,
			"type":   "image/jpg",
		}
	}

	patchResp, err := session.adInputClient.PatchItem(ctx, session.draftID, session.etag, map[string]any{
		"image": imageData,
	})
	if err != nil {
		return fmt.Errorf("failed to set image on item: %w", err)
	}

	session.etag = patchResp.ETag
	return nil
}

// getCategoryPredictions gets AI-suggested categories from the uploaded image
func (b *Bot) getCategoryPredictions(ctx context.Context, session *UserSession) ([]tori.CategoryPrediction, error) {
	if session.draftID == "" {
		return nil, fmt.Errorf("no draft ad")
	}

	return session.adInputClient.GetCategoryPredictions(ctx, session.draftID)
}

// setCategoryOnDraft sets the category on the draft
func (b *Bot) setCategoryOnDraft(ctx context.Context, session *UserSession, categoryID int) error {
	patchResp, err := session.adInputClient.PatchItem(ctx, session.draftID, session.etag, map[string]any{
		"category": categoryID,
	})
	if err != nil {
		return fmt.Errorf("failed to set category: %w", err)
	}

	session.etag = patchResp.ETag
	return nil
}

// getAttributesForDraft fetches category-specific attributes
func (b *Bot) getAttributesForDraft(ctx context.Context, session *UserSession) (*tori.AttributesResponse, error) {
	if session.draftID == "" {
		return nil, fmt.Errorf("no draft ad")
	}

	attrs, err := session.adInputClient.GetAttributes(ctx, session.draftID)
	if err != nil {
		return nil, fmt.Errorf("failed to get attributes: %w", err)
	}

	session.adAttributes = attrs
	return attrs, nil
}

// makeCategoryPredictionKeyboard creates an inline keyboard for category selection
func makeCategoryPredictionKeyboard(categories []tori.CategoryPrediction) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for i, cat := range categories {
		path := tori.GetCategoryPath(cat)
		callbackData := fmt.Sprintf("cat:%d", cat.ID)

		// Truncate long paths for button text
		displayText := path
		if len(displayText) > 50 {
			displayText = "..." + displayText[len(displayText)-47:]
		}

		button := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("[%d] %s", i+1, displayText),
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

	// Add price if selling
	if draft.TradeType == "1" && draft.Price > 0 {
		payload.Extra["price"] = []map[string]any{
			{"price_amount": strconv.Itoa(draft.Price)},
		}
	}

	return payload
}

// updateAndPublishAd updates the ad with all fields and publishes it
func (b *Bot) updateAndPublishAd(
	ctx context.Context,
	session *UserSession,
	draft *AdInputDraft,
	images []UploadedImage,
	postalCode string,
) error {
	payload := buildFinalPayload(draft, images, postalCode)

	// Update the ad
	updateResp, err := session.adInputClient.UpdateAd(ctx, session.draftID, session.etag, payload)
	if err != nil {
		return fmt.Errorf("failed to update ad: %w", err)
	}
	session.etag = updateResp.ETag

	// Set delivery options (meetup only for now)
	err = session.adInputClient.SetDeliveryOptions(ctx, session.draftID, tori.DeliveryOptions{
		BuyNow:             false,
		Client:             "IOS",
		Meetup:             true,
		SellerPaysShipping: false,
		Shipping:           false,
	})
	if err != nil {
		return fmt.Errorf("failed to set delivery options: %w", err)
	}

	// Publish
	_, err = session.adInputClient.PublishAd(ctx, session.draftID)
	if err != nil {
		return fmt.Errorf("failed to publish ad: %w", err)
	}

	return nil
}

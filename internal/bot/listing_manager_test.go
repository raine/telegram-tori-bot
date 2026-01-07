package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/stretchr/testify/mock"
)

func TestDownloadImage_Success(t *testing.T) {
	// Create a test server that returns image data
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer ts.Close()

	data, err := DownloadImage(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(data) != len(imageData) {
		t.Errorf("expected %d bytes, got %d", len(imageData), len(data))
	}
}

func TestDownloadImage_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := DownloadImage(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestDownloadImage_ContextCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should never be reached
		t.Error("request should have been canceled")
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := DownloadImage(ctx, ts.URL)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestStartRepublish_Success(t *testing.T) {
	userId := int64(1)
	tg := new(botApiMock)

	// Create mock ad service
	mockService := &tori.MockAdService{
		GetAdWithModelFunc: func(ctx context.Context, adID string) (*tori.AdWithModel, error) {
			return &tori.AdWithModel{
				Ad: struct {
					ID          string         `json:"id"`
					AdType      string         `json:"ad-type"`
					ETag        string         `json:"etag"`
					UpdateURL   string         `json:"update-url"`
					UploadURL   string         `json:"upload-url"`
					CheckoutURL string         `json:"checkout-url"`
					Values      map[string]any `json:"values"`
				}{
					ID:     adID,
					AdType: "SELL",
					ETag:   "old-etag",
					Values: map[string]any{
						"title":       "Test Item",
						"description": "Test Description",
						"category":    "5012",
						"trade_type":  "1",
						"condition":   "good",
						"location": []any{
							map[string]any{"country": "FI", "postal-code": "00100"},
						},
						"price": []any{
							map[string]any{"price_amount": "50"},
						},
						// No images for this test
					},
				},
			}, nil
		},
	}

	// Create session with mock service
	session := &UserSession{
		userId: userId,
		draft:  DraftState{AdInputClient: mockService},
		sender: tg,
	}

	manager := NewListingManager(tg)

	// Expect progress message, success message, and refresh list
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageTextConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Maybe()
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "⏳ Luodaan uusi ilmoitus samoilla tiedoilla..."
	})).Return(tgbotapi.Message{MessageID: 1}, nil).Maybe()
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "✅ Ilmoitus julkaistu uudelleen!"
	})).Return(tgbotapi.Message{}, nil).Once()
	// Refresh list will fail because no cached listings, but that's ok
	tg.On("Send", mock.AnythingOfType("tgbotapi.MessageConfig")).
		Return(tgbotapi.Message{}, nil).Maybe()

	manager.startRepublish(context.Background(), session, "12345")

	// Verify API calls were made
	if !mockService.WasCalled("GetAdWithModel") {
		t.Error("expected GetAdWithModel to be called")
	}
	if !mockService.WasCalled("CreateDraftAd") {
		t.Error("expected CreateDraftAd to be called")
	}
	if !mockService.WasCalled("UpdateAd") {
		t.Error("expected UpdateAd to be called")
	}
	if !mockService.WasCalled("SetDeliveryOptions") {
		t.Error("expected SetDeliveryOptions to be called")
	}
	if !mockService.WasCalled("PublishAd") {
		t.Error("expected PublishAd to be called")
	}

	tg.AssertExpectations(t)
}

func TestStartRepublish_CopiesExtraAttributes(t *testing.T) {
	userId := int64(1)
	tg := new(botApiMock)

	var capturedPayload tori.AdUpdatePayload

	// Create mock ad service that captures the update payload
	mockService := &tori.MockAdService{
		GetAdWithModelFunc: func(ctx context.Context, adID string) (*tori.AdWithModel, error) {
			return &tori.AdWithModel{
				Ad: struct {
					ID          string         `json:"id"`
					AdType      string         `json:"ad-type"`
					ETag        string         `json:"etag"`
					UpdateURL   string         `json:"update-url"`
					UploadURL   string         `json:"upload-url"`
					CheckoutURL string         `json:"checkout-url"`
					Values      map[string]any `json:"values"`
				}{
					ID:     adID,
					AdType: "SELL",
					ETag:   "old-etag",
					Values: map[string]any{
						"title":       "Test Item",
						"description": "Test Description",
						"category":    "5012",
						"trade_type":  "1",
						// Category-specific attributes that should be copied
						"size":      "M",
						"brand":     "TestBrand",
						"color":     "blue",
						"some_attr": "some_value",
						"location":  []any{map[string]any{"country": "FI", "postal-code": "00100"}},
						"price":     []any{map[string]any{"price_amount": "100"}},
					},
				},
			}, nil
		},
		UpdateAdFunc: func(ctx context.Context, adID, etag string, payload tori.AdUpdatePayload) (*tori.UpdateAdResponse, error) {
			capturedPayload = payload
			return &tori.UpdateAdResponse{ID: adID, ETag: "new-etag"}, nil
		},
	}

	session := &UserSession{
		userId: userId,
		draft:  DraftState{AdInputClient: mockService},
		sender: tg,
	}

	manager := NewListingManager(tg)

	// Set up minimal expectations
	tg.On("Request", mock.Anything).Return(&tgbotapi.APIResponse{Ok: true}, nil).Maybe()
	tg.On("Send", mock.Anything).Return(tgbotapi.Message{}, nil).Maybe()

	manager.startRepublish(context.Background(), session, "12345")

	// Verify extra attributes were copied
	if capturedPayload.Extra == nil {
		t.Fatal("expected Extra to be set")
	}
	if capturedPayload.Extra["size"] != "M" {
		t.Errorf("expected size='M', got %v", capturedPayload.Extra["size"])
	}
	if capturedPayload.Extra["brand"] != "TestBrand" {
		t.Errorf("expected brand='TestBrand', got %v", capturedPayload.Extra["brand"])
	}
	if capturedPayload.Extra["color"] != "blue" {
		t.Errorf("expected color='blue', got %v", capturedPayload.Extra["color"])
	}
	if capturedPayload.Extra["some_attr"] != "some_value" {
		t.Errorf("expected some_attr='some_value', got %v", capturedPayload.Extra["some_attr"])
	}

	// Verify standard fields are NOT in Extra (they should be in main payload)
	if _, exists := capturedPayload.Extra["title"]; exists {
		t.Error("title should not be in Extra")
	}
	if _, exists := capturedPayload.Extra["description"]; exists {
		t.Error("description should not be in Extra")
	}
	if _, exists := capturedPayload.Extra["category"]; exists {
		t.Error("category should not be in Extra")
	}
}

func TestStartRepublish_WithImages(t *testing.T) {
	userId := int64(1)
	tg := new(botApiMock)

	// Create image server
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	}))
	defer imageServer.Close()

	uploadCount := 0
	mockService := &tori.MockAdService{
		GetAdWithModelFunc: func(ctx context.Context, adID string) (*tori.AdWithModel, error) {
			return &tori.AdWithModel{
				Ad: struct {
					ID          string         `json:"id"`
					AdType      string         `json:"ad-type"`
					ETag        string         `json:"etag"`
					UpdateURL   string         `json:"update-url"`
					UploadURL   string         `json:"upload-url"`
					CheckoutURL string         `json:"checkout-url"`
					Values      map[string]any `json:"values"`
				}{
					ID:     adID,
					AdType: "SELL",
					ETag:   "old-etag",
					Values: map[string]any{
						"title":       "Item with images",
						"description": "Has two images",
						"category":    "5012",
						"trade_type":  "1",
						"location":    []any{map[string]any{"country": "FI", "postal-code": "00100"}},
						"multi_image": []any{
							map[string]any{
								"url":    imageServer.URL + "/image1.jpg",
								"width":  float64(800),
								"height": float64(600),
								"type":   "image/jpeg",
							},
							map[string]any{
								"url":    imageServer.URL + "/image2.jpg",
								"width":  float64(1024),
								"height": float64(768),
								"type":   "image/jpeg",
							},
						},
					},
				},
			}, nil
		},
		UploadImageFunc: func(ctx context.Context, adID string, data []byte) (*tori.UploadImageResponse, error) {
			uploadCount++
			return &tori.UploadImageResponse{
				ImagePath: "/uploaded/image" + string(rune('0'+uploadCount)) + ".jpg",
				Location:  "https://storage.example.com/image" + string(rune('0'+uploadCount)) + ".jpg",
			}, nil
		},
	}

	session := &UserSession{
		userId: userId,
		draft:  DraftState{AdInputClient: mockService},
		sender: tg,
	}

	manager := NewListingManager(tg)

	tg.On("Request", mock.Anything).Return(&tgbotapi.APIResponse{Ok: true}, nil).Maybe()
	tg.On("Send", mock.Anything).Return(tgbotapi.Message{}, nil).Maybe()

	manager.startRepublish(context.Background(), session, "12345")

	// Verify both images were uploaded
	if uploadCount != 2 {
		t.Errorf("expected 2 image uploads, got %d", uploadCount)
	}
}

func TestStartRepublish_ImageDownloadFailure_ContinuesWithRemainingImages(t *testing.T) {
	userId := int64(1)
	tg := new(botApiMock)

	// Create image server that fails for first image
	requestCount := 0
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer imageServer.Close()

	uploadCount := 0
	mockService := &tori.MockAdService{
		GetAdWithModelFunc: func(ctx context.Context, adID string) (*tori.AdWithModel, error) {
			return &tori.AdWithModel{
				Ad: struct {
					ID          string         `json:"id"`
					AdType      string         `json:"ad-type"`
					ETag        string         `json:"etag"`
					UpdateURL   string         `json:"update-url"`
					UploadURL   string         `json:"upload-url"`
					CheckoutURL string         `json:"checkout-url"`
					Values      map[string]any `json:"values"`
				}{
					ID:     adID,
					AdType: "SELL",
					ETag:   "old-etag",
					Values: map[string]any{
						"title":       "Item",
						"description": "Description",
						"category":    "5012",
						"trade_type":  "1",
						"location":    []any{map[string]any{"country": "FI", "postal-code": "00100"}},
						"multi_image": []any{
							map[string]any{"url": imageServer.URL + "/fail.jpg", "width": float64(800), "height": float64(600)},
							map[string]any{"url": imageServer.URL + "/success.jpg", "width": float64(800), "height": float64(600)},
						},
					},
				},
			}, nil
		},
		UploadImageFunc: func(ctx context.Context, adID string, data []byte) (*tori.UploadImageResponse, error) {
			uploadCount++
			return &tori.UploadImageResponse{ImagePath: "/path", Location: "https://loc"}, nil
		},
	}

	session := &UserSession{
		userId: userId,
		draft:  DraftState{AdInputClient: mockService},
		sender: tg,
	}

	manager := NewListingManager(tg)

	tg.On("Request", mock.Anything).Return(&tgbotapi.APIResponse{Ok: true}, nil).Maybe()
	tg.On("Send", mock.Anything).Return(tgbotapi.Message{}, nil).Maybe()

	manager.startRepublish(context.Background(), session, "12345")

	// Should still upload the second image even though first failed
	if uploadCount != 1 {
		t.Errorf("expected 1 successful upload (second image), got %d", uploadCount)
	}

	// Should still publish successfully
	if !mockService.WasCalled("PublishAd") {
		t.Error("expected PublishAd to be called despite image failure")
	}
}

func TestShowAdDetail_ShowsRepublishButtonForExpiredAd(t *testing.T) {
	userId := int64(1)
	tg := new(botApiMock)

	session := &UserSession{
		userId: userId,
		listings: ListingBrowserState{
			BrowsePage: 1,
			MenuMsgID:  100, // Set this so editOrSend uses Request (edit) instead of Send
			CachedListings: []tori.AdSummary{
				{
					ID: 12345,
					State: tori.AdState{
						Type:  "EXPIRED",
						Label: "Vanhentunut",
					},
					Data: struct {
						Title    string `json:"title"`
						Subtitle string `json:"subtitle"`
						Image    string `json:"image"`
					}{
						Title:    "Test Item",
						Subtitle: "Tori myydään 50 €",
					},
					Actions: []tori.AdAction{
						{Name: "DELETE"},
					},
				},
			},
		},
		sender: tg,
	}

	manager := NewListingManager(tg)

	// Expect message with republish button
	tg.On("Request", mock.MatchedBy(func(cfg tgbotapi.EditMessageTextConfig) bool {
		if cfg.ReplyMarkup == nil {
			return false
		}
		// Check that one of the buttons is the republish button
		for _, row := range cfg.ReplyMarkup.InlineKeyboard {
			for _, btn := range row {
				if btn.Text == "Julkaise uudelleen" && *btn.CallbackData == "ad:republish:12345" {
					return true
				}
			}
		}
		return false
	})).Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()

	manager.showAdDetail(context.Background(), session, 12345)

	tg.AssertExpectations(t)
}

func TestShowAdDetail_NoRepublishButtonForActiveAd(t *testing.T) {
	userId := int64(1)
	tg := new(botApiMock)

	session := &UserSession{
		userId: userId,
		listings: ListingBrowserState{
			BrowsePage: 1,
			MenuMsgID:  100, // Set this so editOrSend uses Request (edit) instead of Send
			CachedListings: []tori.AdSummary{
				{
					ID: 12345,
					State: tori.AdState{
						Type:  "ACTIVE",
						Label: "Aktiivinen",
					},
					Data: struct {
						Title    string `json:"title"`
						Subtitle string `json:"subtitle"`
						Image    string `json:"image"`
					}{
						Title:    "Test Item",
						Subtitle: "Tori myydään 50 €",
					},
					DaysUntilExpires: 30,
					Actions: []tori.AdAction{
						{Name: "DISPOSE"},
						{Name: "DELETE"},
					},
				},
			},
		},
		sender: tg,
	}

	manager := NewListingManager(tg)

	// Expect message WITHOUT republish button
	tg.On("Request", mock.MatchedBy(func(cfg tgbotapi.EditMessageTextConfig) bool {
		if cfg.ReplyMarkup == nil {
			return false
		}
		// Check that NO button is the republish button
		for _, row := range cfg.ReplyMarkup.InlineKeyboard {
			for _, btn := range row {
				if btn.Text == "Julkaise uudelleen" {
					return false // Found republish button - should not exist
				}
			}
		}
		return true
	})).Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()

	manager.showAdDetail(context.Background(), session, 12345)

	tg.AssertExpectations(t)
}

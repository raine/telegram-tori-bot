package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/llm"
	"github.com/raine/telegram-tori-bot/storage"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/raine/telegram-tori-bot/tori/auth"
	"github.com/stretchr/testify/mock"
)

// mockSessionStore implements storage.SessionStore for testing
type mockSessionStore struct {
	sessions map[int64]*storage.StoredSession
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{
		sessions: make(map[int64]*storage.StoredSession),
	}
}

func (m *mockSessionStore) Get(telegramID int64) (*storage.StoredSession, error) {
	if s, ok := m.sessions[telegramID]; ok {
		return s, nil
	}
	return nil, nil
}

func (m *mockSessionStore) Save(session *storage.StoredSession) error {
	m.sessions[session.TelegramID] = session
	return nil
}

func (m *mockSessionStore) Delete(telegramID int64) error {
	delete(m.sessions, telegramID)
	return nil
}

func (m *mockSessionStore) GetAll() ([]storage.StoredSession, error) {
	var sessions []storage.StoredSession
	for _, s := range m.sessions {
		sessions = append(sessions, *s)
	}
	return sessions, nil
}

func (m *mockSessionStore) Close() error {
	return nil
}

func (m *mockSessionStore) SetTemplate(telegramID int64, content string) error {
	return nil
}

func (m *mockSessionStore) GetTemplate(telegramID int64) (*storage.Template, error) {
	return nil, nil
}

func (m *mockSessionStore) DeleteTemplate(telegramID int64) error {
	return nil
}

func (m *mockSessionStore) SetPostalCode(telegramID int64, postalCode string) error {
	return nil
}

func (m *mockSessionStore) GetPostalCode(telegramID int64) (string, error) {
	return "", nil
}

func (m *mockSessionStore) GetVisionCache(imageHash string) (*storage.VisionCacheEntry, error) {
	return nil, nil
}

func (m *mockSessionStore) SetVisionCache(imageHash string, entry *storage.VisionCacheEntry) error {
	return nil
}

func (m *mockSessionStore) IsUserAllowed(telegramID int64) (bool, error) {
	return true, nil // All users allowed in tests by default
}

func (m *mockSessionStore) AddAllowedUser(telegramID, addedBy int64) error {
	return nil
}

func (m *mockSessionStore) RemoveAllowedUser(telegramID int64) error {
	return nil
}

func (m *mockSessionStore) GetAllowedUsers() ([]storage.AllowedUser, error) {
	return nil, nil
}

const testAdminID int64 = 1 // Default admin ID for tests

func setup(t *testing.T) (int64, *botApiMock, *Bot, *UserSession) {
	userId := int64(1)
	tg := new(botApiMock)

	// Create a mock store with a pre-authenticated user
	store := newMockSessionStore()
	store.sessions[userId] = &storage.StoredSession{
		TelegramID: userId,
		ToriUserID: "123123",
		Tokens: auth.TokenSet{
			BearerToken: "test-token",
		},
	}

	bot := NewBot(tg, store, testAdminID)
	session, err := bot.state.getUserSession(userId)
	if err != nil {
		t.Fatal(err)
	}

	return userId, tg, bot, session
}

func formatJson(b []byte) string {
	var out bytes.Buffer
	err := json.Indent(&out, b, "", "  ")
	if err != nil {
		panic(err)
	}
	return out.String()
}

type botApiMock struct {
	mock.Mock
}

func (m *botApiMock) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	args := m.Called(c)
	return args.Get(0).(tgbotapi.Message), args.Error(1)
}

func (m *botApiMock) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	args := m.Called(c)
	return args.Get(0).(*tgbotapi.APIResponse), args.Error(1)
}

func (m *botApiMock) GetFileDirectURL(fileID string) (string, error) {
	args := m.Called(fileID)
	return args.Get(0).(string), args.Error(1)
}

// mockVisionAnalyzer implements llm.Analyzer for testing
type mockVisionAnalyzer struct {
	mock.Mock
}

func (m *mockVisionAnalyzer) AnalyzeImage(ctx context.Context, data []byte, mimeType string) (*llm.AnalysisResult, error) {
	args := m.Called(ctx, data, mimeType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.AnalysisResult), args.Error(1)
}

func makeUpdateWithMessageText(userId int64, text string) tgbotapi.Update {
	return tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{
				ID: userId,
			},
			Text: text,
		},
	}
}

func makeMessage(userId int64, text string) tgbotapi.MessageConfig {
	msg := tgbotapi.NewMessage(userId, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	return msg
}

func TestMain(m *testing.M) {
	os.Setenv("GO_ENV", "test")
	os.Exit(m.Run())
}

func TestHandleUpdate_UnauthenticatedUser(t *testing.T) {
	userId := int64(99999) // Not in session store
	tg := new(botApiMock)
	store := newMockSessionStore() // Empty store - no authenticated users
	bot := NewBot(tg, store, testAdminID)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			MessageID: 10,
			From:      &tgbotapi.User{ID: userId},
			Text:      "/start",
		},
	}

	// Unauthenticated user should get login required message
	tg.On("Send", makeMessage(userId, loginRequiredText)).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)
}

func TestHandleUpdate_AuthenticatedUserStart(t *testing.T) {
	userId, tg, bot, _ := setup(t)

	update := makeUpdateWithMessageText(userId, "/start")

	// Authenticated user should get start message
	tg.On("Send", makeMessage(userId, startText)).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)
}

func TestHandleUpdate_RemovePhotosCommand(t *testing.T) {
	userId, tg, bot, session := setup(t)

	update := makeUpdateWithMessageText(userId, "/poistakuvat")

	session.photos = []tgbotapi.PhotoSize{
		{FileID: "1", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
		{FileID: "2", FileUniqueID: "2", Width: 371, Height: 495, FileSize: 28548},
	}

	tg.On("Send", makeMessage(userId, "Kuvat poistettu.")).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)
}

// makeAdInputTestServer creates a test server that mocks the adinput API endpoints
func makeAdInputTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasPrefix(r.URL.Path, "/ads/") && r.Method == "DELETE":
			// Mock DELETE /ads/{adId} - delete draft
			w.WriteHeader(http.StatusNoContent)

		case strings.HasPrefix(r.URL.Path, "/item/") && r.Method == "PATCH":
			// Mock PATCH /item/{draftId} - set category, etc.
			w.Header().Set("ETag", "new-etag-123")
			w.Write([]byte(`{"id":"draft-123"}`))

		case strings.HasPrefix(r.URL.Path, "/attributes/"):
			// Mock GET /attributes/{draftId} - return test attributes
			w.Write([]byte(`{
				"attributes": [
					{
						"name": "condition",
						"type": "SELECT",
						"label": "Kunto",
						"isPredictable": true,
						"options": [
							{"id": 1, "label": "Uusi"},
							{"id": 2, "label": "Erinomainen"},
							{"id": 3, "label": "Hyvä"},
							{"id": 4, "label": "Tyydyttävä"}
						]
					},
					{
						"name": "computeracc_type",
						"type": "SELECT",
						"label": "Tyyppi",
						"isPredictable": false,
						"options": [
							{"id": 10, "label": "Hiiri"},
							{"id": 11, "label": "Näppäimistö"},
							{"id": 12, "label": "Kuulokkeet"}
						]
					}
				],
				"category": {"id": 5012, "label": "Tietokoneen oheislaitteet"}
			}`))

		default:
			w.Write([]byte("{}"))
		}
	}))
}

// setupAdInputSession creates a test session with adinput API ready
func setupAdInputSession(t *testing.T, ts *httptest.Server) (*httptest.Server, int64, *botApiMock, *Bot, *UserSession) {
	userId := int64(1)
	tg := new(botApiMock)

	store := newMockSessionStore()
	store.sessions[userId] = &storage.StoredSession{
		TelegramID: userId,
		ToriUserID: "123123",
		Tokens: auth.TokenSet{
			BearerToken: "test-token",
		},
	}

	bot := NewBot(tg, store, testAdminID)
	session, err := bot.state.getUserSession(userId)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize adinput client with test server URL
	session.adInputClient = tori.NewAdinputClientWithBaseURL("test-token", ts.URL)
	session.draftID = "draft-123"
	session.etag = "test-etag"

	return ts, userId, tg, bot, session
}

func TestHandleCategorySelection_FetchesAttributes(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, bot, session := setupAdInputSession(t, ts)

	// Create listing handler for the bot
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	// Set up session with draft awaiting category (with parent to test full path)
	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingCategory,
		CollectedAttrs: make(map[string]string),
		CategoryPredictions: []tori.CategoryPrediction{
			{ID: 5012, Label: "Tietokoneen oheislaitteet", Parent: &tori.CategoryPrediction{ID: 5000, Label: "Tietokoneet"}},
		},
	}

	// Create callback query
	query := &tgbotapi.CallbackQuery{
		ID:   "callback-1",
		From: &tgbotapi.User{ID: userId},
		Data: "cat:5012",
		Message: &tgbotapi.Message{
			MessageID: 100,
			Chat:      &tgbotapi.Chat{ID: userId},
		},
	}

	// Expect: edit keyboard, send category confirmation (with full path), send attribute prompt
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageReplyMarkupConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	tg.On("Send", makeMessage(userId, "Osasto: *Tietokoneet > Tietokoneen oheislaitteet*")).
		Return(tgbotapi.Message{}, nil).Once()
	// Attribute prompt with reply keyboard
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "Valitse kunto" && msg.ReplyMarkup != nil
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.listingHandler.HandleCategorySelection(context.Background(), session, query)
	tg.AssertExpectations(t)

	// Verify state was updated
	if session.currentDraft.State != AdFlowStateAwaitingAttribute {
		t.Errorf("expected state AwaitingAttribute, got %v", session.currentDraft.State)
	}
	if len(session.currentDraft.RequiredAttrs) != 2 {
		t.Errorf("expected 2 required attrs, got %d", len(session.currentDraft.RequiredAttrs))
	}
}

func TestHandleAttributeInput_SelectsOption(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, _, session := setupAdInputSession(t, ts)

	// Create a listing handler with the tg mock
	listingHandler := NewListingHandler(tg, nil, nil, nil)

	// Set up session with draft awaiting attribute
	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		CollectedAttrs: make(map[string]string),
		RequiredAttrs: []tori.Attribute{
			{
				Name:  "condition",
				Type:  "SELECT",
				Label: "Kunto",
				Options: []tori.AttributeOption{
					{ID: 1, Label: "Uusi"},
					{ID: 2, Label: "Erinomainen"},
					{ID: 3, Label: "Hyvä"},
				},
			},
			{
				Name:  "computeracc_type",
				Type:  "SELECT",
				Label: "Tyyppi",
				Options: []tori.AttributeOption{
					{ID: 10, Label: "Hiiri"},
					{ID: 11, Label: "Näppäimistö"},
				},
			},
		},
		CurrentAttrIndex: 0,
	}

	// Expect prompt for next attribute (tyyppi)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "Valitse tyyppi" && msg.ReplyMarkup != nil
	})).Return(tgbotapi.Message{}, nil).Once()

	listingHandler.HandleAttributeInput(context.Background(), session, "Erinomainen")

	tg.AssertExpectations(t)

	// Verify attribute was stored
	if session.currentDraft.CollectedAttrs["condition"] != "2" {
		t.Errorf("expected condition=2, got %s", session.currentDraft.CollectedAttrs["condition"])
	}
	if session.currentDraft.CurrentAttrIndex != 1 {
		t.Errorf("expected CurrentAttrIndex=1, got %d", session.currentDraft.CurrentAttrIndex)
	}
}

func TestHandleAttributeInput_LastAttribute_MovesToPrice(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, _, session := setupAdInputSession(t, ts)

	listingHandler := NewListingHandler(tg, nil, nil, nil)

	// Set up session with only one attribute left (the last one)
	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		Title:          "Test Item",
		CollectedAttrs: map[string]string{"condition": "2"},
		RequiredAttrs: []tori.Attribute{
			{
				Name:  "condition",
				Type:  "SELECT",
				Label: "Kunto",
				Options: []tori.AttributeOption{
					{ID: 2, Label: "Erinomainen"},
				},
			},
			{
				Name:  "computeracc_type",
				Type:  "SELECT",
				Label: "Tyyppi",
				Options: []tori.AttributeOption{
					{ID: 10, Label: "Hiiri"},
					{ID: 11, Label: "Näppäimistö"},
				},
			},
		},
		CurrentAttrIndex: 1, // On the last attribute
	}

	// Expect price prompt (may include recommendation text)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.HasPrefix(msg.Text, "Syötä hinta")
	})).Return(tgbotapi.Message{}, nil).Once()

	listingHandler.HandleAttributeInput(context.Background(), session, "Hiiri")

	tg.AssertExpectations(t)

	// Verify state moved to price
	if session.currentDraft.State != AdFlowStateAwaitingPrice {
		t.Errorf("expected state AwaitingPrice, got %v", session.currentDraft.State)
	}
	if session.currentDraft.CollectedAttrs["computeracc_type"] != "10" {
		t.Errorf("expected computeracc_type=10, got %s", session.currentDraft.CollectedAttrs["computeracc_type"])
	}
}

func TestHandlePriceInput_ValidPrice(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, _, session := setupAdInputSession(t, ts)

	listingHandler := NewListingHandler(tg, nil, nil, nil)

	session.currentDraft = &AdInputDraft{
		State:       AdFlowStateAwaitingPrice,
		Title:       "Logitech hiiri",
		Description: "Langaton pelihiiri",
	}
	session.photos = []tgbotapi.PhotoSize{{FileID: "1"}}

	// Expect price confirmation message with keyboard removal
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		_, hasRemoveKeyboard := msg.ReplyMarkup.(tgbotapi.ReplyKeyboardRemove)
		return strings.Contains(msg.Text, "Hinta: *50€*") && hasRemoveKeyboard
	})).Return(tgbotapi.Message{}, nil).Once()

	// Expect shipping question (flow now asks about shipping after price)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Onko postitus mahdollinen?")
	})).Return(tgbotapi.Message{}, nil).Once()

	listingHandler.HandlePriceInput(context.Background(), session, "50€")

	tg.AssertExpectations(t)

	if session.currentDraft.Price != 50 {
		t.Errorf("expected price=50, got %d", session.currentDraft.Price)
	}
	if session.currentDraft.TradeType != TradeTypeSell {
		t.Errorf("expected trade_type=TradeTypeSell, got %s", session.currentDraft.TradeType)
	}
	if session.currentDraft.State != AdFlowStateAwaitingShipping {
		t.Errorf("expected state AwaitingShipping, got %v", session.currentDraft.State)
	}
}

func TestHandlePriceInput_InvalidPrice(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, _, session := setupAdInputSession(t, ts)

	listingHandler := NewListingHandler(tg, nil, nil, nil)

	session.currentDraft = &AdInputDraft{
		State: AdFlowStateAwaitingPrice,
	}

	// Expect error message
	tg.On("Send", makeMessage(userId, "En ymmärtänyt hintaa. Syötä hinta numerona (esim. 50€ tai 50)")).
		Return(tgbotapi.Message{}, nil).Once()

	listingHandler.HandlePriceInput(context.Background(), session, "ilmainen")

	tg.AssertExpectations(t)

	// State should remain awaiting price
	if session.currentDraft.State != AdFlowStateAwaitingPrice {
		t.Errorf("expected state to remain AwaitingPrice, got %v", session.currentDraft.State)
	}
}

func TestHandlePriceInput_Giveaway(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, _, session := setupAdInputSession(t, ts)

	listingHandler := NewListingHandler(tg, nil, nil, nil)

	session.currentDraft = &AdInputDraft{
		State:       AdFlowStateAwaitingPrice,
		Title:       "Logitech hiiri",
		Description: "Myydään langaton pelihiiri",
	}
	session.photos = []tgbotapi.PhotoSize{{FileID: "1"}}

	// Expect giveaway confirmation message with keyboard removal
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		_, hasRemoveKeyboard := msg.ReplyMarkup.(tgbotapi.ReplyKeyboardRemove)
		return strings.Contains(msg.Text, "Hinta: *Annetaan*") && hasRemoveKeyboard
	})).Return(tgbotapi.Message{}, nil).Once()

	// Expect shipping question after giveaway selection
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Onko postitus mahdollinen?")
	})).Return(tgbotapi.Message{}, nil).Once()

	listingHandler.HandlePriceInput(context.Background(), session, "🎁 Annetaan")

	tg.AssertExpectations(t)

	if session.currentDraft.Price != 0 {
		t.Errorf("expected price=0, got %d", session.currentDraft.Price)
	}
	if session.currentDraft.TradeType != TradeTypeGive {
		t.Errorf("expected trade_type=TradeTypeGive, got %s", session.currentDraft.TradeType)
	}
	if session.currentDraft.State != AdFlowStateAwaitingShipping {
		t.Errorf("expected state AwaitingShipping, got %v", session.currentDraft.State)
	}
}

func TestParsePriceMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		// Valid formats
		{name: "plain number", input: "50", want: 50, wantErr: false},
		{name: "plain number with spaces", input: "  50  ", want: 50, wantErr: false},
		{name: "number with euro suffix", input: "50€", want: 50, wantErr: false},
		{name: "number with euro suffix space", input: "50 €", want: 50, wantErr: false},
		{name: "euro prefix", input: "€50", want: 50, wantErr: false},
		{name: "euro prefix space", input: "€ 50", want: 50, wantErr: false},
		{name: "number with e suffix", input: "50e", want: 50, wantErr: false},
		{name: "number with eur suffix", input: "50 eur", want: 50, wantErr: false},
		{name: "number with EUR suffix", input: "50 EUR", want: 50, wantErr: false},
		{name: "decimal with dot", input: "50.50", want: 51, wantErr: false},
		{name: "decimal with comma", input: "99,99€", want: 100, wantErr: false},
		{name: "large number", input: "1000", want: 1000, wantErr: false},
		{name: "decimal rounds down", input: "50.49", want: 50, wantErr: false},
		{name: "thousands separator", input: "1 000", want: 1000, wantErr: false},
		{name: "thousands separator with euro", input: "1 200 €", want: 1200, wantErr: false},
		{name: "large with thousands separator", input: "10 000", want: 10000, wantErr: false},

		// Invalid formats - these should be rejected
		{name: "product model number", input: "Beyerdynamic DT 770", want: 0, wantErr: true},
		{name: "model with numbers", input: "DT 770", want: 0, wantErr: true},
		{name: "text with number", input: "Model 123", want: 0, wantErr: true},
		{name: "number in middle of text", input: "test 50 test", want: 0, wantErr: true},
		{name: "no number at all", input: "ilmainen", want: 0, wantErr: true},
		{name: "empty string", input: "", want: 0, wantErr: true},
		{name: "only spaces", input: "   ", want: 0, wantErr: true},
		{name: "number with extra text after", input: "50 euros please", want: 0, wantErr: true},
		{name: "number with text before", input: "price 50", want: 0, wantErr: true},
		{name: "alphanumeric", input: "50abc", want: 0, wantErr: true},
		{name: "phone number format", input: "123-456", want: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePriceMessage(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePriceMessage(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parsePriceMessage(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleUpdate_ReplyToTitleEdits(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, bot, session := setupAdInputSession(t, ts)

	// Set up the listing handler
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	// Set up session with draft that has message IDs
	session.currentDraft = &AdInputDraft{
		State:                AdFlowStateAwaitingCategory,
		Title:                "Original title",
		Description:          "Original description",
		TitleMessageID:       100,
		DescriptionMessageID: 101,
	}

	// Create update with reply to title message
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: userId},
			Text: "New title",
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 100,
			},
		},
	}

	// Expect edit of original message
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageTextConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	// Expect confirmation message
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Otsikko päivitetty") &&
			strings.Contains(msg.Text, "New title")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// Verify title was updated
	if session.currentDraft.Title != "New title" {
		t.Errorf("expected title='New title', got '%s'", session.currentDraft.Title)
	}
}

func TestHandleUpdate_ReplyToDescriptionEdits(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, bot, session := setupAdInputSession(t, ts)

	// Set up the listing handler
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	// Set up session with draft that has message IDs
	session.currentDraft = &AdInputDraft{
		State:                AdFlowStateAwaitingCategory,
		Title:                "Original title",
		Description:          "Original description",
		TitleMessageID:       100,
		DescriptionMessageID: 101,
	}

	// Create update with reply to description message
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: userId},
			Text: "New description",
			ReplyToMessage: &tgbotapi.Message{
				MessageID: 101,
			},
		},
	}

	// Expect edit of original message
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageTextConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	// Expect confirmation message
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Kuvaus päivitetty")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// Verify description was updated
	if session.currentDraft.Description != "New description" {
		t.Errorf("expected description='New description', got '%s'", session.currentDraft.Description)
	}
}

func TestHandleUpdate_PeruDuringAttributeInput(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, bot, session := setupAdInputSession(t, ts)

	// Set up the listing handler
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	// Set up session with draft awaiting attribute
	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		CollectedAttrs: make(map[string]string),
		RequiredAttrs: []tori.Attribute{
			{
				Name:  "condition",
				Type:  "SELECT",
				Label: "Kunto",
				Options: []tori.AttributeOption{
					{ID: 1, Label: "Uusi"},
				},
			},
		},
		CurrentAttrIndex: 0,
	}

	update := makeUpdateWithMessageText(userId, "/peru")

	// Expect "Ok!" message with keyboard removal
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		_, hasRemoveKeyboard := msg.ReplyMarkup.(tgbotapi.ReplyKeyboardRemove)
		return msg.Text == okText && hasRemoveKeyboard
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// Verify session was reset
	if session.currentDraft != nil {
		t.Errorf("expected currentDraft to be nil after /peru")
	}
}

func TestHandleUpdate_PeruDuringPriceInput(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, bot, session := setupAdInputSession(t, ts)

	// Set up the listing handler
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	// Set up session with draft awaiting price
	session.currentDraft = &AdInputDraft{
		State:       AdFlowStateAwaitingPrice,
		Title:       "Test item",
		Description: "Test description",
	}

	update := makeUpdateWithMessageText(userId, "/peru")

	// Expect "Ok!" message with keyboard removal
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		_, hasRemoveKeyboard := msg.ReplyMarkup.(tgbotapi.ReplyKeyboardRemove)
		return msg.Text == okText && hasRemoveKeyboard
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// Verify session was reset
	if session.currentDraft != nil {
		t.Errorf("expected currentDraft to be nil after /peru")
	}
}

func TestHandleOsastoCommand_NoDraft(t *testing.T) {
	_, userId, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))

	// No current draft
	session.currentDraft = nil

	tg.On("Send", makeMessage(userId, "Ei aktiivista ilmoitusta. Lähetä ensin kuva.")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.handleOsastoCommand(session)

	tg.AssertExpectations(t)
}

func TestHandleOsastoCommand_NoPredictions(t *testing.T) {
	_, userId, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))

	// Draft exists but no category predictions
	session.currentDraft = &AdInputDraft{
		State:               AdFlowStateAwaitingPrice,
		CategoryPredictions: []tori.CategoryPrediction{},
	}

	tg.On("Send", makeMessage(userId, "Ei osastoehdotuksia saatavilla.")).
		Return(tgbotapi.Message{}, nil).Once()

	bot.handleOsastoCommand(session)

	tg.AssertExpectations(t)
}

func TestHandleOsastoCommand_ShowsMenuWhenPastCategorySelection(t *testing.T) {
	_, _, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))

	// Draft exists with predictions and we're past category selection
	session.currentDraft = &AdInputDraft{
		State:      AdFlowStateAwaitingPrice,
		CategoryID: 5012,
		CategoryPredictions: []tori.CategoryPrediction{
			{ID: 5012, Label: "Tietokoneen oheislaitteet"},
			{ID: 5013, Label: "Näytöt"},
		},
		CollectedAttrs:   map[string]string{"condition": "2"},
		RequiredAttrs:    []tori.Attribute{{Name: "condition"}},
		CurrentAttrIndex: 1,
	}

	// Expect options menu with inline keyboard (since past category selection)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "Mitä haluat muuttaa?" && msg.ReplyMarkup != nil
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleOsastoCommand(session)

	tg.AssertExpectations(t)

	// State should NOT be reset yet (menu was shown, user hasn't selected)
	if session.currentDraft.State != AdFlowStateAwaitingPrice {
		t.Errorf("expected state to remain AwaitingPrice, got %v", session.currentDraft.State)
	}
}

func TestHandleOsastoCommand_ShowsCategoryKeyboardWhenAwaitingCategory(t *testing.T) {
	_, _, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))

	// Draft exists with predictions and we're still awaiting category
	session.currentDraft = &AdInputDraft{
		State: AdFlowStateAwaitingCategory,
		CategoryPredictions: []tori.CategoryPrediction{
			{ID: 5012, Label: "Tietokoneen oheislaitteet"},
			{ID: 5013, Label: "Näytöt"},
		},
	}

	// Expect category selection message with inline keyboard
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "Valitse osasto" && msg.ReplyMarkup != nil
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleOsastoCommand(session)

	tg.AssertExpectations(t)
}

func TestHandleUpdate_OsastoDuringPriceInput(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, userId, tg, bot, session := setupAdInputSession(t, ts)

	// Set up session with draft awaiting price
	session.currentDraft = &AdInputDraft{
		State:      AdFlowStateAwaitingPrice,
		CategoryID: 5012,
		CategoryPredictions: []tori.CategoryPrediction{
			{ID: 5012, Label: "Tietokoneen oheislaitteet"},
		},
	}

	update := makeUpdateWithMessageText(userId, "/osasto")

	// Expect options menu (since past category selection)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "Mitä haluat muuttaa?" && msg.ReplyMarkup != nil
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// State should remain (menu was shown, user hasn't selected yet)
	if session.currentDraft.State != AdFlowStateAwaitingPrice {
		t.Errorf("expected state to remain AwaitingPrice, got %v", session.currentDraft.State)
	}
}

func TestHandleReselectCallback_PreservesValues(t *testing.T) {
	_, _, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))

	// Set up session with draft that has price, shipping, and condition set
	session.currentDraft = &AdInputDraft{
		State:            AdFlowStateReadyToPublish,
		CategoryID:       5012,
		Price:            100,
		TradeType:        TradeTypeSell,
		ShippingPossible: true,
		CollectedAttrs:   map[string]string{"condition": "2"},
		CategoryPredictions: []tori.CategoryPrediction{
			{ID: 5012, Label: "Tietokoneen oheislaitteet"},
			{ID: 5013, Label: "Näytöt"},
		},
	}

	query := &tgbotapi.CallbackQuery{
		ID:   "callback-1",
		Data: "reselect:category",
		Message: &tgbotapi.Message{
			MessageID: 100,
			Chat:      &tgbotapi.Chat{ID: session.userId},
		},
	}

	// Expect: edit keyboard to remove it, send category selection message
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageReplyMarkupConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return msg.Text == "Valitse osasto" && msg.ReplyMarkup != nil
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleReselectCallback(context.Background(), session, query)
	tg.AssertExpectations(t)

	// Verify state was reset
	if session.currentDraft.State != AdFlowStateAwaitingCategory {
		t.Errorf("expected state AwaitingCategory, got %v", session.currentDraft.State)
	}
	if session.currentDraft.CategoryID != 0 {
		t.Errorf("expected CategoryID to be 0, got %d", session.currentDraft.CategoryID)
	}

	// Verify preserved values were saved
	if session.currentDraft.PreservedValues == nil {
		t.Fatal("expected PreservedValues to be set")
	}
	if session.currentDraft.PreservedValues.Price != 100 {
		t.Errorf("expected preserved Price 100, got %d", session.currentDraft.PreservedValues.Price)
	}
	if session.currentDraft.PreservedValues.TradeType != TradeTypeSell {
		t.Errorf("expected preserved TradeType %s, got %s", TradeTypeSell, session.currentDraft.PreservedValues.TradeType)
	}
	if !session.currentDraft.PreservedValues.ShippingSet {
		t.Error("expected ShippingSet to be true")
	}
	if !session.currentDraft.PreservedValues.ShippingPossible {
		t.Error("expected ShippingPossible to be true")
	}
	if session.currentDraft.PreservedValues.CollectedAttrs["condition"] != "2" {
		t.Errorf("expected preserved condition '2', got %s", session.currentDraft.PreservedValues.CollectedAttrs["condition"])
	}
}

func TestTryRestorePreservedAttributes_RestoresCompatibleCondition(t *testing.T) {
	_, _, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		CollectedAttrs: make(map[string]string),
		PreservedValues: &PreservedValues{
			CollectedAttrs: map[string]string{"condition": "2"}, // "Erinomainen" has ID 2
		},
	}

	// Test attributes where condition has option ID 2
	attrs := []tori.Attribute{
		{
			Name:  "condition",
			Label: "Kunto",
			Options: []tori.AttributeOption{
				{ID: 1, Label: "Uusi"},
				{ID: 2, Label: "Erinomainen"},
				{ID: 3, Label: "Hyvä"},
			},
		},
	}

	remaining := bot.listingHandler.tryRestorePreservedAttributes(session, attrs, session.currentDraft.PreservedValues)

	// Condition should be restored, no remaining attributes
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining attributes, got %d", len(remaining))
	}
	if session.currentDraft.CollectedAttrs["condition"] != "2" {
		t.Errorf("expected condition '2' to be restored, got %s", session.currentDraft.CollectedAttrs["condition"])
	}
}

func TestTryRestorePreservedAttributes_SkipsIncompatibleCondition(t *testing.T) {
	_, _, tg, bot, session := setupAdInputSession(t, makeAdInputTestServer(t))
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		CollectedAttrs: make(map[string]string),
		PreservedValues: &PreservedValues{
			CollectedAttrs: map[string]string{"condition": "99"}, // ID 99 doesn't exist in new category
		},
	}

	// Test attributes where condition does NOT have option ID 99
	attrs := []tori.Attribute{
		{
			Name:  "condition",
			Label: "Kunto",
			Options: []tori.AttributeOption{
				{ID: 1, Label: "Uusi"},
				{ID: 2, Label: "Erinomainen"},
			},
		},
	}

	// No message expected since nothing was restored
	remaining := bot.listingHandler.tryRestorePreservedAttributes(session, attrs, session.currentDraft.PreservedValues)
	tg.AssertExpectations(t)

	// Condition should NOT be restored, attribute remains
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining attribute, got %d", len(remaining))
	}
	if _, ok := session.currentDraft.CollectedAttrs["condition"]; ok {
		t.Error("expected condition to NOT be restored")
	}
}

func TestProceedAfterAttributes_SkipsPriceAndShipping(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, bot, session := setupAdInputSession(t, ts)

	// Create a mock session store that returns a postal code
	mockStore := newMockSessionStore()
	mockStore.sessions[session.userId] = &storage.StoredSession{TelegramID: session.userId}
	// Override GetPostalCode to return a valid postal code
	bot.listingHandler = &ListingHandler{
		tg:           tg,
		sessionStore: &mockSessionStoreWithPostalCode{mockSessionStore: mockStore, postalCode: "00100"},
	}

	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		Title:          "Test Item",
		Description:    "Test description",
		CollectedAttrs: map[string]string{"condition": "2"},
		PreservedValues: &PreservedValues{
			Price:            50,
			TradeType:        TradeTypeSell,
			ShippingPossible: true,
			ShippingSet:      true,
		},
	}

	// Expect: summary shown directly (no price/shipping messages)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Test Item")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.listingHandler.proceedAfterAttributes(context.Background(), session)
	tg.AssertExpectations(t)

	// Should be at ready to publish state
	if session.currentDraft.State != AdFlowStateReadyToPublish {
		t.Errorf("expected state ReadyToPublish, got %v", session.currentDraft.State)
	}
	// PreservedValues should be cleared
	if session.currentDraft.PreservedValues != nil {
		t.Error("expected PreservedValues to be nil after restoration")
	}
}

func TestProceedAfterAttributes_PromptsForPriceWhenNotSet(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, bot, session := setupAdInputSession(t, ts)
	bot.listingHandler = NewListingHandler(tg, nil, nil, nil)

	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		Title:          "Test Item",
		CollectedAttrs: map[string]string{},
		PreservedValues: &PreservedValues{
			Price:     0,             // Price not set
			TradeType: TradeTypeSell, // Default trade type
		},
	}

	// Expect price prompt (contains "Syötä hinta")
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Syötä hinta")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.listingHandler.proceedAfterAttributes(context.Background(), session)
	tg.AssertExpectations(t)

	// Should be at awaiting price state
	if session.currentDraft.State != AdFlowStateAwaitingPrice {
		t.Errorf("expected state AwaitingPrice, got %v", session.currentDraft.State)
	}
}

func TestProceedAfterAttributes_RestoresGiveaway(t *testing.T) {
	ts := makeAdInputTestServer(t)
	defer ts.Close()
	_, _, tg, bot, session := setupAdInputSession(t, ts)

	mockStore := newMockSessionStore()
	bot.listingHandler = &ListingHandler{
		tg:           tg,
		sessionStore: &mockSessionStoreWithPostalCode{mockSessionStore: mockStore, postalCode: "00100"},
	}

	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingAttribute,
		Title:          "Free Item",
		Description:    "Giving away",
		CollectedAttrs: map[string]string{},
		PreservedValues: &PreservedValues{
			Price:            0,
			TradeType:        TradeTypeGive, // Giveaway
			ShippingPossible: false,
			ShippingSet:      true,
		},
	}

	// Expect: summary shown directly (no giveaway/shipping messages)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Free Item")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.listingHandler.proceedAfterAttributes(context.Background(), session)
	tg.AssertExpectations(t)

	if session.currentDraft.TradeType != TradeTypeGive {
		t.Errorf("expected TradeType %s, got %s", TradeTypeGive, session.currentDraft.TradeType)
	}
}

// mockSessionStoreWithPostalCode wraps mockSessionStore to return a specific postal code
type mockSessionStoreWithPostalCode struct {
	*mockSessionStore
	postalCode string
}

func (m *mockSessionStoreWithPostalCode) GetPostalCode(telegramID int64) (string, error) {
	return m.postalCode, nil
}

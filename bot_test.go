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

	bot := NewBot(tg, store)
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
	bot := NewBot(tg, store)

	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			MessageID: 10,
			From:      &tgbotapi.User{ID: userId},
			Text:      "/start",
		},
	}

	// Unauthenticated user should get login required message
	tg.On("Send", makeMessage(userId, loginRequiredText)).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(context.Background(), update)
	tg.AssertExpectations(t)
}

func TestHandleUpdate_AuthenticatedUserStart(t *testing.T) {
	userId, tg, bot, _ := setup(t)

	update := makeUpdateWithMessageText(userId, "/start")

	// Authenticated user should get start message
	tg.On("Send", makeMessage(userId, startText)).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(context.Background(), update)
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

	bot.handleUpdate(context.Background(), update)
	tg.AssertExpectations(t)
}

// makeAdInputTestServer creates a test server that mocks the adinput API endpoints
func makeAdInputTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
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

	bot := NewBot(tg, store)
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

	// Set up session with draft awaiting category
	session.currentDraft = &AdInputDraft{
		State:          AdFlowStateAwaitingCategory,
		CollectedAttrs: make(map[string]string),
		CategoryPredictions: []tori.CategoryPrediction{
			{ID: 5012, Label: "Tietokoneen oheislaitteet"},
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

	// Expect: edit keyboard, send category confirmation, send attribute prompt
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageReplyMarkupConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	tg.On("Send", makeMessage(userId, "Osasto: *Tietokoneen oheislaitteet*")).
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

	session.mu.Lock()
	listingHandler.HandleAttributeInput(context.Background(), session, "Erinomainen")
	session.mu.Unlock()

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
		return strings.HasPrefix(msg.Text, "Syötä hinta (esim. 50€)")
	})).Return(tgbotapi.Message{}, nil).Once()

	session.mu.Lock()
	listingHandler.HandleAttributeInput(context.Background(), session, "Hiiri")
	session.mu.Unlock()

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

	// Expect shipping question (flow now asks about shipping after price)
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Onko postitus mahdollinen?")
	})).Return(tgbotapi.Message{}, nil).Once()

	session.mu.Lock()
	listingHandler.HandlePriceInput(context.Background(), session, "50€")
	session.mu.Unlock()

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

	session.mu.Lock()
	listingHandler.HandlePriceInput(context.Background(), session, "ilmainen")
	session.mu.Unlock()

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

	// Expect shipping question after giveaway selection
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Onko postitus mahdollinen?")
	})).Return(tgbotapi.Message{}, nil).Once()

	session.mu.Lock()
	listingHandler.HandlePriceInput(context.Background(), session, "🎁 Annetaan")
	session.mu.Unlock()

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

	bot.handleUpdate(context.Background(), update)
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

	bot.handleUpdate(context.Background(), update)
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

	bot.handleUpdate(context.Background(), update)
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

	bot.handleUpdate(context.Background(), update)
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

	session.mu.Lock()
	bot.handleOsastoCommand(session)
	session.mu.Unlock()

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

	session.mu.Lock()
	bot.handleOsastoCommand(session)
	session.mu.Unlock()

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

	session.mu.Lock()
	bot.handleOsastoCommand(session)
	session.mu.Unlock()

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

	session.mu.Lock()
	bot.handleOsastoCommand(session)
	session.mu.Unlock()

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

	bot.handleUpdate(context.Background(), update)
	tg.AssertExpectations(t)

	// State should remain (menu was shown, user hasn't selected yet)
	if session.currentDraft.State != AdFlowStateAwaitingPrice {
		t.Errorf("expected state to remain AwaitingPrice, got %v", session.currentDraft.State)
	}
}

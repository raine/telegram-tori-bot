package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/storage"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/stretchr/testify/mock"
)

// makeWatchSearchTestServer creates a test server that mocks the Tori search API for watch tests
func makeWatchSearchTestServer(t *testing.T, response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
}

// setupWatchTest creates test infrastructure for watch handler tests
func setupWatchTest(t *testing.T) (*storage.SQLiteStore, *botApiMock, *Bot, *UserSession, func()) {
	// Create in-memory SQLite database
	store, err := storage.NewSQLiteStore(":memory:", []byte("test-key-32-bytes-long-ok-test!"))
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	userId := int64(1)
	tg := new(botApiMock)

	bot := NewBot(tg, store, testAdminID)
	session, err := bot.state.getUserSession(userId)
	if err != nil {
		store.Close()
		t.Fatalf("failed to get session: %v", err)
	}

	cleanup := func() {
		store.Close()
	}

	return store, tg, bot, session, cleanup
}

func TestHandleHakuCommand_ShowsSearchResults(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	// Create mock search server
	searchResponse := `{
		"docs": [
			{"id": "123", "heading": "iPhone 14 Pro", "price": {"value": "450€"}, "location": "Helsinki"},
			{"id": "124", "heading": "iPhone 14 Plus", "price": {"value": "400€"}, "location": "Espoo"}
		],
		"metadata": {"result_size": {"match_count": 2}}
	}`
	ts := makeWatchSearchTestServer(t, searchResponse)
	defer ts.Close()

	// Override the search client with test server
	bot.watchHandler = &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: newTestSearchClient(ts.URL),
	}

	// Expect message with results and "create watch" button
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		hasResults := strings.Contains(msg.Text, "iPhone 14 Pro") &&
			strings.Contains(msg.Text, "450€") &&
			strings.Contains(msg.Text, "Helsinki")
		hasButton := msg.ReplyMarkup != nil
		return hasResults && hasButton
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleHakuCommand(context.Background(), session, "iphone 14")

	tg.AssertExpectations(t)

	// Verify query was stored in session
	if session.pendingSearchQuery != "iphone 14" {
		t.Errorf("expected pendingSearchQuery='iphone 14', got '%s'", session.pendingSearchQuery)
	}
}

func TestHandleHakuCommand_NoQuery(t *testing.T) {
	_, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	// Expect usage message
	tg.On("Send", makeMessage(session.userId, MsgSearchQueryMissing)).
		Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleHakuCommand(context.Background(), session, "")

	tg.AssertExpectations(t)
}

func TestHandleHakuCommand_NoResults(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	// Create mock search server with no results
	searchResponse := `{
		"docs": [],
		"metadata": {"result_size": {"match_count": 0}}
	}`
	ts := makeWatchSearchTestServer(t, searchResponse)
	defer ts.Close()

	bot.watchHandler = &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: newTestSearchClient(ts.URL),
	}

	// Expect message with no results but still offer watch button
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		hasNoResults := strings.Contains(msg.Text, "Ei tuloksia")
		hasButton := msg.ReplyMarkup != nil
		return hasNoResults && hasButton
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleHakuCommand(context.Background(), session, "rare item xyz")

	tg.AssertExpectations(t)
}

func TestHandleSeuraaCommand_CreatesWatch(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	// Create mock search server for seeding
	searchResponse := `{"docs": [], "metadata": {}}`
	ts := makeWatchSearchTestServer(t, searchResponse)
	defer ts.Close()

	bot.watchHandler = &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: newTestSearchClient(ts.URL),
	}

	// Expect watch creation confirmation
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Seuranta luotu") &&
			strings.Contains(msg.Text, "iphone 14")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleSeuraaCommand(context.Background(), session, "iphone 14")

	tg.AssertExpectations(t)

	// Verify watch was created in database
	watches, err := store.GetWatchesByUser(session.userId)
	if err != nil {
		t.Fatalf("failed to get watches: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected 1 watch, got %d", len(watches))
	}
	if watches[0].Query != "iphone 14" {
		t.Errorf("expected query='iphone 14', got '%s'", watches[0].Query)
	}
}

func TestHandleSeuraaCommand_NoQuery(t *testing.T) {
	_, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	// Expect usage message
	tg.On("Send", makeMessage(session.userId, MsgWatchQueryMissing)).
		Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleSeuraaCommand(context.Background(), session, "")

	tg.AssertExpectations(t)
}

func TestHandleSeuraaCommand_DuplicateWatch(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	searchResponse := `{"docs": [], "metadata": {}}`
	ts := makeWatchSearchTestServer(t, searchResponse)
	defer ts.Close()

	bot.watchHandler = &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: newTestSearchClient(ts.URL),
	}

	// Create first watch
	_, err := store.CreateWatch(session.userId, "iphone 14")
	if err != nil {
		t.Fatalf("failed to create watch: %v", err)
	}

	// Expect duplicate error
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "on jo olemassa")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleSeuraaCommand(context.Background(), session, "iphone 14")

	tg.AssertExpectations(t)
}

func TestHandleSeuraaCommand_WatchLimitReached(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	searchResponse := `{"docs": [], "metadata": {}}`
	ts := makeWatchSearchTestServer(t, searchResponse)
	defer ts.Close()

	bot.watchHandler = &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: newTestSearchClient(ts.URL),
	}

	// Create max watches
	for i := 0; i < MaxWatchesPerUser; i++ {
		_, err := store.CreateWatch(session.userId, "watch "+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("failed to create watch: %v", err)
		}
	}

	// Expect limit error
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "maksimimäärän")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleSeuraaCommand(context.Background(), session, "new watch")

	tg.AssertExpectations(t)
}

func TestHandleSeurattavatCommand_ShowsWatches(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	bot.watchHandler = &WatchHandler{
		tg:    tg,
		store: store,
	}

	// Create some watches
	store.CreateWatch(session.userId, "iphone 14")
	store.CreateWatch(session.userId, "macbook pro")

	// Expect list with watches and delete buttons
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		hasHeader := strings.Contains(msg.Text, "Seurannat") && strings.Contains(msg.Text, "2 kpl")
		hasWatches := strings.Contains(msg.Text, "iphone 14") && strings.Contains(msg.Text, "macbook pro")
		hasButtons := msg.ReplyMarkup != nil
		return hasHeader && hasWatches && hasButtons
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleSeurattavatCommand(context.Background(), session)

	tg.AssertExpectations(t)
}

func TestHandleSeurattavatCommand_NoWatches(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	bot.watchHandler = &WatchHandler{
		tg:    tg,
		store: store,
	}

	// Expect "no watches" message
	tg.On("Send", makeMessage(session.userId, MsgNoWatches)).
		Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleSeurattavatCommand(context.Background(), session)

	tg.AssertExpectations(t)
}

func TestHandleWatchCallback_CreateWatch(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	searchResponse := `{"docs": [], "metadata": {}}`
	ts := makeWatchSearchTestServer(t, searchResponse)
	defer ts.Close()

	bot.watchHandler = &WatchHandler{
		tg:           tg,
		store:        store,
		searchClient: newTestSearchClient(ts.URL),
	}

	// Set pending query (as if user just searched)
	session.pendingSearchQuery = "iphone 14"

	query := &tgbotapi.CallbackQuery{
		ID:   "callback-1",
		From: &tgbotapi.User{ID: session.userId},
		Data: "watch:create",
		Message: &tgbotapi.Message{
			MessageID: 100,
			Chat:      &tgbotapi.Chat{ID: session.userId},
		},
	}

	// Expect: remove keyboard, then send confirmation
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageReplyMarkupConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Seuranta luotu")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleWatchCallback(context.Background(), session, query)

	tg.AssertExpectations(t)

	// Verify watch was created
	watches, err := store.GetWatchesByUser(session.userId)
	if err != nil {
		t.Fatalf("failed to get watches: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected 1 watch, got %d", len(watches))
	}

	// Verify pending query was cleared
	if session.pendingSearchQuery != "" {
		t.Errorf("expected pendingSearchQuery to be cleared, got '%s'", session.pendingSearchQuery)
	}
}

func TestHandleWatchCallback_CreateWatchExpired(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	bot.watchHandler = &WatchHandler{
		tg:    tg,
		store: store,
	}

	// No pending query (expired)
	session.pendingSearchQuery = ""

	query := &tgbotapi.CallbackQuery{
		ID:   "callback-1",
		From: &tgbotapi.User{ID: session.userId},
		Data: "watch:create",
		Message: &tgbotapi.Message{
			MessageID: 100,
			Chat:      &tgbotapi.Chat{ID: session.userId},
		},
	}

	// Expect: edit message with error
	tg.On("Request", mock.AnythingOfType("tgbotapi.EditMessageTextConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()

	bot.watchHandler.HandleWatchCallback(context.Background(), session, query)

	tg.AssertExpectations(t)
}

func TestHandleWatchCallback_DeleteWatch(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	bot.watchHandler = &WatchHandler{
		tg:    tg,
		store: store,
	}

	// Create watch to delete
	watch, err := store.CreateWatch(session.userId, "iphone 14")
	if err != nil {
		t.Fatalf("failed to create watch: %v", err)
	}

	query := &tgbotapi.CallbackQuery{
		ID:   "callback-1",
		From: &tgbotapi.User{ID: session.userId},
		Data: "watch:delete:" + watch.ID,
		Message: &tgbotapi.Message{
			MessageID: 100,
			Chat:      &tgbotapi.Chat{ID: session.userId},
		},
	}

	// Expect: delete message, then send "deleted + no watches" message
	tg.On("Request", mock.AnythingOfType("tgbotapi.DeleteMessageConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Seuranta poistettu")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.watchHandler.HandleWatchCallback(context.Background(), session, query)

	tg.AssertExpectations(t)

	// Verify watch was deleted
	watches, err := store.GetWatchesByUser(session.userId)
	if err != nil {
		t.Fatalf("failed to get watches: %v", err)
	}
	if len(watches) != 0 {
		t.Errorf("expected 0 watches, got %d", len(watches))
	}
}

func TestHandleWatchCallback_Close(t *testing.T) {
	store, tg, bot, session, cleanup := setupWatchTest(t)
	defer cleanup()

	bot.watchHandler = &WatchHandler{
		tg:    tg,
		store: store,
	}

	query := &tgbotapi.CallbackQuery{
		ID:   "callback-1",
		From: &tgbotapi.User{ID: session.userId},
		Data: "watch:close",
		Message: &tgbotapi.Message{
			MessageID: 100,
			Chat:      &tgbotapi.Chat{ID: session.userId},
		},
	}

	// Expect: delete message
	tg.On("Request", mock.AnythingOfType("tgbotapi.DeleteMessageConfig")).
		Return(&tgbotapi.APIResponse{Ok: true}, nil).Once()

	bot.watchHandler.HandleWatchCallback(context.Background(), session, query)

	tg.AssertExpectations(t)
}

// newTestSearchClient creates a search client with a custom base URL for testing
func newTestSearchClient(baseURL string) *tori.SearchClient {
	return tori.NewSearchClientWithBaseURL(baseURL)
}

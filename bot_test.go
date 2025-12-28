package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/storage"
	"github.com/raine/telegram-tori-bot/tori/auth"
	"github.com/raine/telegram-tori-bot/vision"
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

func setupWithTestServer(t *testing.T, ts *httptest.Server) (*httptest.Server, int64, *botApiMock, *Bot, *UserSession) {
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

	bot := NewBot(tg, ts.URL, store)
	session, err := bot.state.getUserSession(userId)
	if err != nil {
		t.Fatal(err)
	}

	return ts, userId, tg, bot, session
}

func setup(t *testing.T) (*httptest.Server, int64, *botApiMock, *Bot, *UserSession) {
	ts := makeTestServer(t)
	return setupWithTestServer(t, ts)
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

// mockVisionAnalyzer implements vision.Analyzer for testing
type mockVisionAnalyzer struct {
	mock.Mock
}

func (m *mockVisionAnalyzer) AnalyzeImage(ctx context.Context, data []byte, mimeType string) (*vision.AnalysisResult, error) {
	args := m.Called(ctx, data, mimeType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*vision.AnalysisResult), args.Error(1)
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

func makeTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.2/private/accounts/123123":
			b, err := os.ReadFile("tori/testdata/v1_2_private_accounts_123123.json")
			if err != nil {
				t.Fatal(err)
			}
			w.Write(b)
		default:
			w.Write([]byte("{}"))
		}
	}))
}

func TestMain(m *testing.M) {
	os.Setenv("GO_ENV", "test")
	os.Exit(m.Run())
}

func TestHandleUpdate_UnauthenticatedUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("no requests expected")
	}))
	defer ts.Close()
	userId := int64(99999) // Not in session store
	tg := new(botApiMock)
	store := newMockSessionStore() // Empty store - no authenticated users
	bot := NewBot(tg, ts.URL, store)

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
	ts, userId, tg, bot, _ := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "/start")

	// Authenticated user should get start message
	tg.On("Send", makeMessage(userId, startText)).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(context.Background(), update)
	tg.AssertExpectations(t)
}

func TestHandleUpdate_RemovePhotosCommand(t *testing.T) {
	ts, userId, tg, bot, session := setup(t)
	defer ts.Close()

	update := makeUpdateWithMessageText(userId, "/poistakuvat")

	session.photos = []tgbotapi.PhotoSize{
		{FileID: "1", FileUniqueID: "1", Width: 371, Height: 495, FileSize: 28548},
		{FileID: "2", FileUniqueID: "2", Width: 371, Height: 495, FileSize: 28548},
	}

	tg.On("Send", makeMessage(userId, "Kuvat poistettu.")).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdate(context.Background(), update)
	tg.AssertExpectations(t)
}

package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- BulkSession tests ---

func TestNewBulkSession(t *testing.T) {
	bs := NewBulkSession()

	assert.True(t, bs.Active)
	assert.Empty(t, bs.Drafts)
	assert.Equal(t, "", bs.EditingDraftID)
	assert.Equal(t, 1, bs.nextDraftID)
}

func TestBulkSession_NewBulkDraft(t *testing.T) {
	bs := NewBulkSession()

	draft1 := bs.NewBulkDraft()
	assert.Equal(t, "draft_1", draft1.ID)
	assert.Equal(t, BulkAnalysisPending, draft1.AnalysisStatus)
	assert.Equal(t, TradeTypeSell, draft1.TradeType)
	assert.NotNil(t, draft1.CollectedAttrs)

	draft2 := bs.NewBulkDraft()
	assert.Equal(t, "draft_2", draft2.ID)

	// IDs should be unique
	assert.NotEqual(t, draft1.ID, draft2.ID)
}

func TestBulkSession_GetDraftByID(t *testing.T) {
	bs := NewBulkSession()

	draft1 := bs.NewBulkDraft()
	draft2 := bs.NewBulkDraft()
	bs.Drafts = append(bs.Drafts, draft1, draft2)

	// Find existing draft
	found := bs.GetDraftByID("draft_1")
	assert.NotNil(t, found)
	assert.Equal(t, "draft_1", found.ID)

	found = bs.GetDraftByID("draft_2")
	assert.NotNil(t, found)
	assert.Equal(t, "draft_2", found.ID)

	// Non-existent draft
	notFound := bs.GetDraftByID("draft_999")
	assert.Nil(t, notFound)
}

func TestBulkSession_RemoveDraft(t *testing.T) {
	bs := NewBulkSession()

	draft1 := bs.NewBulkDraft()
	draft1.Index = 0
	draft2 := bs.NewBulkDraft()
	draft2.Index = 1
	draft3 := bs.NewBulkDraft()
	draft3.Index = 2
	bs.Drafts = []*BulkDraft{draft1, draft2, draft3}

	// Remove middle draft
	bs.RemoveDraft("draft_2")

	assert.Len(t, bs.Drafts, 2)
	assert.Equal(t, "draft_1", bs.Drafts[0].ID)
	assert.Equal(t, "draft_3", bs.Drafts[1].ID)

	// Verify re-indexing
	assert.Equal(t, 0, bs.Drafts[0].Index)
	assert.Equal(t, 1, bs.Drafts[1].Index)
}

func TestBulkSession_RemoveDraft_CancelsAnalysis(t *testing.T) {
	bs := NewBulkSession()

	cancelCalled := false
	draft := bs.NewBulkDraft()
	draft.CancelAnalysis = func() { cancelCalled = true }
	bs.Drafts = []*BulkDraft{draft}

	bs.RemoveDraft(draft.ID)

	assert.True(t, cancelCalled)
	assert.Empty(t, bs.Drafts)
}

func TestBulkSession_GetDraft(t *testing.T) {
	bs := NewBulkSession()

	draft1 := bs.NewBulkDraft()
	draft2 := bs.NewBulkDraft()
	bs.Drafts = []*BulkDraft{draft1, draft2}

	// Valid index
	assert.Equal(t, draft1, bs.GetDraft(0))
	assert.Equal(t, draft2, bs.GetDraft(1))

	// Invalid indices
	assert.Nil(t, bs.GetDraft(-1))
	assert.Nil(t, bs.GetDraft(2))
}

func TestBulkSession_IsAnalysisComplete(t *testing.T) {
	bs := NewBulkSession()

	// Empty session - complete
	assert.True(t, bs.IsAnalysisComplete())

	// Add pending draft - not complete
	draft1 := bs.NewBulkDraft()
	draft1.AnalysisStatus = BulkAnalysisPending
	bs.Drafts = []*BulkDraft{draft1}
	assert.False(t, bs.IsAnalysisComplete())

	// Change to analyzing - still not complete
	draft1.AnalysisStatus = BulkAnalysisAnalyzing
	assert.False(t, bs.IsAnalysisComplete())

	// Change to done - complete
	draft1.AnalysisStatus = BulkAnalysisDone
	assert.True(t, bs.IsAnalysisComplete())

	// Add another pending - not complete again
	draft2 := bs.NewBulkDraft()
	draft2.AnalysisStatus = BulkAnalysisPending
	bs.Drafts = append(bs.Drafts, draft2)
	assert.False(t, bs.IsAnalysisComplete())

	// Error status also counts as complete (not pending/analyzing)
	draft2.AnalysisStatus = BulkAnalysisError
	assert.True(t, bs.IsAnalysisComplete())
}

func TestBulkSession_GetCompleteDrafts(t *testing.T) {
	bs := NewBulkSession()

	// Draft that's ready
	draft1 := bs.NewBulkDraft()
	draft1.AnalysisStatus = BulkAnalysisDone
	draft1.Title = "Test Item"
	draft1.CategoryID = 123

	// Draft that's still pending
	draft2 := bs.NewBulkDraft()
	draft2.AnalysisStatus = BulkAnalysisPending

	// Draft that's done but missing title
	draft3 := bs.NewBulkDraft()
	draft3.AnalysisStatus = BulkAnalysisDone
	draft3.CategoryID = 456

	// Draft that's done but missing category
	draft4 := bs.NewBulkDraft()
	draft4.AnalysisStatus = BulkAnalysisDone
	draft4.Title = "Another Item"

	bs.Drafts = []*BulkDraft{draft1, draft2, draft3, draft4}

	complete := bs.GetCompleteDrafts()
	assert.Len(t, complete, 1)
	assert.Equal(t, draft1.ID, complete[0].ID)
}

// --- BulkDraft tests ---

func TestBulkDraft_IsReadyToPublish(t *testing.T) {
	tests := []struct {
		name     string
		draft    BulkDraft
		expected bool
	}{
		{
			name: "ready sell listing",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "Test Item",
				CategoryID:     123,
				TradeType:      TradeTypeSell,
				Price:          50,
			},
			expected: true,
		},
		{
			name: "ready giveaway listing",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "Test Item",
				CategoryID:     123,
				TradeType:      TradeTypeGive,
				Price:          0,
			},
			expected: true,
		},
		{
			name: "sell listing without price",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "Test Item",
				CategoryID:     123,
				TradeType:      TradeTypeSell,
				Price:          0,
			},
			expected: false,
		},
		{
			name: "pending analysis",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisPending,
				Title:          "Test Item",
				CategoryID:     123,
				TradeType:      TradeTypeSell,
				Price:          50,
			},
			expected: false,
		},
		{
			name: "missing title",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "",
				CategoryID:     123,
				TradeType:      TradeTypeSell,
				Price:          50,
			},
			expected: false,
		},
		{
			name: "missing category",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "Test Item",
				CategoryID:     0,
				TradeType:      TradeTypeSell,
				Price:          50,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.draft.IsReadyToPublish())
		})
	}
}

func TestBulkDraft_StatusEmoji(t *testing.T) {
	tests := []struct {
		name     string
		draft    BulkDraft
		expected string
	}{
		{
			name:     "pending",
			draft:    BulkDraft{AnalysisStatus: BulkAnalysisPending},
			expected: "⏳",
		},
		{
			name:     "analyzing",
			draft:    BulkDraft{AnalysisStatus: BulkAnalysisAnalyzing},
			expected: "⏳",
		},
		{
			name: "done and ready",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "Test",
				CategoryID:     123,
				TradeType:      TradeTypeSell,
				Price:          50,
			},
			expected: "✅",
		},
		{
			name: "done but needs editing",
			draft: BulkDraft{
				AnalysisStatus: BulkAnalysisDone,
				Title:          "Test",
				CategoryID:     123,
				TradeType:      TradeTypeSell,
				Price:          0, // Missing price
			},
			expected: "📝",
		},
		{
			name:     "error",
			draft:    BulkDraft{AnalysisStatus: BulkAnalysisError},
			expected: "❌",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.draft.StatusEmoji())
		})
	}
}

// --- Price estimation tests ---

func makeSearchTestServer(t *testing.T, prices []int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Build search result with prices
		docs := make([]tori.SearchDoc, len(prices))
		for i, price := range prices {
			docs[i] = tori.SearchDoc{
				Price: &tori.SearchPrice{Amount: price},
			}
		}

		result := tori.SearchResult{Docs: docs}
		json.NewEncoder(w).Encode(result)
	}))
}

func TestEstimatePriceForDraft_CalculatesMedian(t *testing.T) {
	// Prices: 10, 20, 30, 40, 50 -> median = 30
	ts := makeSearchTestServer(t, []int{10, 20, 30, 40, 50})
	defer ts.Close()

	handler := &BulkHandler{
		searchClient: tori.NewSearchClientWithBaseURL(ts.URL),
	}

	draft := &BulkDraft{
		ID:    "test_draft",
		Title: "Test Item",
	}

	handler.estimatePriceForDraft(context.Background(), draft)

	assert.Equal(t, 30, draft.Price)
	assert.Equal(t, TradeTypeSell, draft.TradeType)

	// Verify PriceEstimate details
	assert.NotNil(t, draft.PriceEstimate)
	assert.Equal(t, 5, draft.PriceEstimate.Count)
	assert.Equal(t, 10, draft.PriceEstimate.Min)
	assert.Equal(t, 50, draft.PriceEstimate.Max)
	assert.Equal(t, 30, draft.PriceEstimate.Median)
}

func TestEstimatePriceForDraft_CalculatesMedianEvenCount(t *testing.T) {
	// Prices: 10, 20, 30, 40 -> median = (20+30)/2 = 25
	ts := makeSearchTestServer(t, []int{10, 20, 30, 40})
	defer ts.Close()

	handler := &BulkHandler{
		searchClient: tori.NewSearchClientWithBaseURL(ts.URL),
	}

	draft := &BulkDraft{
		ID:    "test_draft",
		Title: "Test Item",
	}

	handler.estimatePriceForDraft(context.Background(), draft)

	assert.Equal(t, 25, draft.Price)
}

func TestEstimatePriceForDraft_NotEnoughResults(t *testing.T) {
	// Only 2 prices - not enough for estimation (need >= 3)
	ts := makeSearchTestServer(t, []int{10, 20})
	defer ts.Close()

	handler := &BulkHandler{
		searchClient: tori.NewSearchClientWithBaseURL(ts.URL),
	}

	draft := &BulkDraft{
		ID:    "test_draft",
		Title: "Test Item",
		Price: 0, // Should remain 0
	}

	handler.estimatePriceForDraft(context.Background(), draft)

	assert.Equal(t, 0, draft.Price)
	assert.Equal(t, "", draft.TradeType) // Not set
}

func TestEstimatePriceForDraft_EmptyTitle(t *testing.T) {
	ts := makeSearchTestServer(t, []int{10, 20, 30})
	defer ts.Close()

	handler := &BulkHandler{
		searchClient: tori.NewSearchClientWithBaseURL(ts.URL),
	}

	draft := &BulkDraft{
		ID:    "test_draft",
		Title: "", // Empty title - should skip
	}

	handler.estimatePriceForDraft(context.Background(), draft)

	assert.Equal(t, 0, draft.Price)
}

func TestEstimatePriceForDraft_FiltersZeroPrices(t *testing.T) {
	// Include some zero prices which should be filtered out
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		docs := []tori.SearchDoc{
			{Price: &tori.SearchPrice{Amount: 0}}, // Should be filtered
			{Price: &tori.SearchPrice{Amount: 10}},
			{Price: &tori.SearchPrice{Amount: 20}},
			{Price: &tori.SearchPrice{Amount: 30}},
			{Price: nil}, // No price - should be filtered
		}

		result := tori.SearchResult{Docs: docs}
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	handler := &BulkHandler{
		searchClient: tori.NewSearchClientWithBaseURL(ts.URL),
	}

	draft := &BulkDraft{
		ID:    "test_draft",
		Title: "Test Item",
	}

	handler.estimatePriceForDraft(context.Background(), draft)

	// Only 3 valid prices: 10, 20, 30 -> median = 20
	assert.Equal(t, 20, draft.Price)
}

// --- Bulk mode command tests ---

func TestHandleEraCommand_StartsSession(t *testing.T) {
	userId, tg, bot, session := setup(t)

	// Initialize bulk handler
	bot.bulkHandler = NewBulkHandler(tg, nil, newMockSessionStore())

	update := makeUpdateWithMessageText(userId, "/era")

	// Expect bulk mode started message
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Erätila aloitettu")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// Verify session is in bulk mode
	assert.True(t, session.IsInBulkMode())
	assert.NotNil(t, session.bulkSession)
}

func TestHandleEraCommand_AlreadyInBulkMode(t *testing.T) {
	userId, tg, bot, session := setup(t)

	bot.bulkHandler = NewBulkHandler(tg, nil, newMockSessionStore())

	// Start bulk session first
	session.StartBulkSession()

	update := makeUpdateWithMessageText(userId, "/era")

	// Expect "already in bulk mode" message
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Olet jo erätilassa")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)
}

func TestHandleValmisCommand_NotInBulkMode(t *testing.T) {
	userId, tg, bot, _ := setup(t)

	bot.bulkHandler = NewBulkHandler(tg, nil, newMockSessionStore())

	update := makeUpdateWithMessageText(userId, "/valmis")

	// Expect "not in bulk mode" message
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		return strings.Contains(msg.Text, "Et ole erätilassa")
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)
}

func TestHandlePeruCommand_InBulkMode(t *testing.T) {
	userId, tg, bot, session := setup(t)

	bot.bulkHandler = NewBulkHandler(tg, nil, newMockSessionStore())

	// Start bulk session
	session.StartBulkSession()

	update := makeUpdateWithMessageText(userId, "/peru")

	// Expect cancellation message with keyboard removal
	tg.On("Send", mock.MatchedBy(func(msg tgbotapi.MessageConfig) bool {
		_, hasRemoveKeyboard := msg.ReplyMarkup.(tgbotapi.ReplyKeyboardRemove)
		return strings.Contains(msg.Text, "Erätila peruutettu") && hasRemoveKeyboard
	})).Return(tgbotapi.Message{}, nil).Once()

	bot.handleUpdateSync(context.Background(), update)
	tg.AssertExpectations(t)

	// Verify session is no longer in bulk mode
	assert.False(t, session.IsInBulkMode())
}

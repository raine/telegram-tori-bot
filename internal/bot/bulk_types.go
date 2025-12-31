package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/raine/telegram-tori-bot/internal/tori"
)

// BulkAnalysisStatus represents the analysis status of a bulk draft.
type BulkAnalysisStatus string

const (
	BulkAnalysisPending   BulkAnalysisStatus = "pending"
	BulkAnalysisAnalyzing BulkAnalysisStatus = "analyzing"
	BulkAnalysisDone      BulkAnalysisStatus = "done"
	BulkAnalysisError     BulkAnalysisStatus = "error"
)

// BulkSession represents a bulk listing session where multiple drafts can be created at once.
type BulkSession struct {
	Active          bool
	Drafts          []*BulkDraft
	StatusMessageID int         // The live-updating status message
	EditingDraftID  string      // Empty if not editing, otherwise the ID being edited
	EditingField    string      // "title", "description", "price", "category", "photos"
	UpdateTimer     *time.Timer // Debounce timer for status updates
	AlbumBuffer     *AlbumBuffer
	nextDraftID     int // Counter for generating unique draft IDs
}

// PriceEstimate holds details about how a price was estimated.
type PriceEstimate struct {
	Count  int // Number of listings used for estimation
	Min    int // Minimum price in sample
	Max    int // Maximum price in sample
	Median int // The calculated median price
}

// BulkDraft represents a single draft in a bulk listing session.
type BulkDraft struct {
	ID             string // Stable unique identifier (not affected by deletions)
	Index          int    // Display index (re-calculated after deletions)
	MessageID      int    // The message ID for this draft's edit view
	Photos         []AlbumPhoto
	AnalysisStatus BulkAnalysisStatus
	CancelAnalysis context.CancelFunc
	ErrorMessage   string // Error message if AnalysisStatus is "error"

	// Listing data (populated after analysis)
	Title            string
	Description      string
	Price            int
	PriceEstimate    *PriceEstimate // Details about how price was estimated
	TradeType        string         // "1" = sell, "2" = give
	CategoryID       int
	CategoryLabel    string
	CollectedAttrs   map[string]string
	ShippingPossible bool
	Images           []UploadedImage // Uploaded images to Tori

	// Category predictions for selection
	CategoryPredictions []tori.CategoryPrediction

	// Attribute collection state
	RequiredAttrs    []tori.Attribute
	CurrentAttrIndex int

	// Tori API state
	DraftID string
	ETag    string
}

// NewBulkSession creates a new bulk session.
func NewBulkSession() *BulkSession {
	return &BulkSession{
		Active:         true,
		Drafts:         make([]*BulkDraft, 0),
		EditingDraftID: "",
		nextDraftID:    1,
	}
}

// NewBulkDraft creates a new draft with a unique ID.
func (bs *BulkSession) NewBulkDraft() *BulkDraft {
	id := fmt.Sprintf("draft_%d", bs.nextDraftID)
	bs.nextDraftID++
	return &BulkDraft{
		ID:             id,
		Index:          len(bs.Drafts), // Will be re-indexed on add
		AnalysisStatus: BulkAnalysisPending,
		TradeType:      TradeTypeSell,
		CollectedAttrs: make(map[string]string),
	}
}

// GetDraft returns a draft by index, or nil if not found.
func (bs *BulkSession) GetDraft(index int) *BulkDraft {
	if index < 0 || index >= len(bs.Drafts) {
		return nil
	}
	return bs.Drafts[index]
}

// GetDraftByID returns a draft by its stable ID, or nil if not found.
func (bs *BulkSession) GetDraftByID(id string) *BulkDraft {
	for _, d := range bs.Drafts {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// RemoveDraft removes a draft by ID and re-indexes remaining drafts.
func (bs *BulkSession) RemoveDraft(id string) {
	newDrafts := make([]*BulkDraft, 0, len(bs.Drafts)-1)
	for _, d := range bs.Drafts {
		if d.ID == id {
			// Cancel any ongoing analysis
			if d.CancelAnalysis != nil {
				d.CancelAnalysis()
			}
			continue
		}
		newDrafts = append(newDrafts, d)
	}

	// Re-index drafts for display
	for i, d := range newDrafts {
		d.Index = i
	}
	bs.Drafts = newDrafts
}

// DraftCount returns the number of drafts in the session.
func (bs *BulkSession) DraftCount() int {
	return len(bs.Drafts)
}

// IsAnalysisComplete returns true if all drafts have completed analysis.
func (bs *BulkSession) IsAnalysisComplete() bool {
	for _, d := range bs.Drafts {
		if d.AnalysisStatus == BulkAnalysisPending || d.AnalysisStatus == BulkAnalysisAnalyzing {
			return false
		}
	}
	return true
}

// GetCompleteDrafts returns all drafts that are ready to publish (done + have required data).
func (bs *BulkSession) GetCompleteDrafts() []*BulkDraft {
	var complete []*BulkDraft
	for _, d := range bs.Drafts {
		if d.AnalysisStatus == BulkAnalysisDone && d.Title != "" && d.CategoryID > 0 {
			complete = append(complete, d)
		}
	}
	return complete
}

// IsReadyToPublish checks if a draft has all required data for publishing.
func (d *BulkDraft) IsReadyToPublish() bool {
	if d.AnalysisStatus != BulkAnalysisDone {
		return false
	}
	if d.Title == "" || d.CategoryID == 0 {
		return false
	}
	if d.TradeType == TradeTypeSell && d.Price == 0 {
		return false
	}
	return true
}

// StatusEmoji returns an emoji representing the draft's current status.
func (d *BulkDraft) StatusEmoji() string {
	switch d.AnalysisStatus {
	case BulkAnalysisPending:
		return "⏳"
	case BulkAnalysisAnalyzing:
		return "⏳"
	case BulkAnalysisDone:
		if d.IsReadyToPublish() {
			return "✅"
		}
		return "📝" // Needs editing
	case BulkAnalysisError:
		return "❌"
	default:
		return "❓"
	}
}

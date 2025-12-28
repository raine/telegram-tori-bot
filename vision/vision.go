package vision

import "context"

// ItemDescription contains structured information about an item for selling.
type ItemDescription struct {
	Title       string // Short title suitable for marketplace listing
	Description string // Longer description with details
	Brand       string // Brand name if identifiable
	Model       string // Model name/number if identifiable
	Condition   string // Estimated condition (new, like new, good, fair, poor)
}

// Usage contains token usage and cost information.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	CostUSD      float64
}

// AnalysisResult contains the item description and usage information.
type AnalysisResult struct {
	Item  *ItemDescription
	Usage Usage
}

// Analyzer can analyze images and generate item descriptions.
type Analyzer interface {
	// AnalyzeImage takes image data and returns a description suitable for selling.
	AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error)
}

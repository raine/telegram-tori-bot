package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

const geminiModel = "gemini-3-flash-preview"

// Gemini 3.0 Flash pricing (per million tokens)
const (
	geminiInputPricePerMillion  = 0.50 // $0.50 per 1M input tokens (text/image/video)
	geminiOutputPricePerMillion = 3.00 // $3.00 per 1M output tokens (including thinking)
)

const geminiPrompt = `Analyze this image and identify the item for selling on a secondhand marketplace.

Respond in JSON format with these fields:
- title: A short, descriptive title suitable for a marketplace listing (in Finnish if possible, otherwise English). Include brand and model if visible.
- description: A longer description with relevant details about the item (2-3 sentences, in Finnish if possible)
- brand: The brand name if identifiable (empty string if unknown)
- model: The model name or number if identifiable (empty string if unknown)

Example response:
{"title": "Logitech G Pro X Superlight langaton pelihiiri", "description": "Kevyt langaton pelihiiri ammattipelaamiseen. Logitech Hero 25K -sensori, jopa 70 tunnin akunkesto.", "brand": "Logitech", "model": "G Pro X Superlight"}

Respond ONLY with the JSON object, no markdown or other text.`

// GeminiAnalyzer uses Google's Gemini API for image analysis.
type GeminiAnalyzer struct {
	client *genai.Client
}

// NewGeminiAnalyzer creates a new Gemini-based analyzer.
// It uses the GEMINI_API_KEY environment variable for authentication.
func NewGeminiAnalyzer(ctx context.Context) (*GeminiAnalyzer, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	return &GeminiAnalyzer{client: client}, nil
}

// AnalyzeImage implements the Analyzer interface using Gemini.
func (g *GeminiAnalyzer) AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
	parts := []*genai.Part{
		genai.NewPartFromText(geminiPrompt),
		{InlineData: &genai.Blob{Data: imageData, MIMEType: mimeType}},
	}

	contents := []*genai.Content{
		genai.NewContentFromParts(parts, genai.RoleUser),
	}

	result, err := g.client.Models.GenerateContent(ctx, geminiModel, contents, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no response from Gemini")
	}

	text := result.Text()
	log.Info().Str("response", text).Msg("gemini vision response")

	item, err := parseItemDescription(text)
	if err != nil {
		return nil, err
	}

	// Calculate usage and cost
	usage := Usage{}
	if result.UsageMetadata != nil {
		usage.InputTokens = int64(result.UsageMetadata.PromptTokenCount)
		usage.OutputTokens = int64(result.UsageMetadata.CandidatesTokenCount)
		usage.TotalTokens = int64(result.UsageMetadata.TotalTokenCount)
		usage.CostUSD = calculateGeminiCost(usage.InputTokens, usage.OutputTokens)
	}

	return &AnalysisResult{Item: item, Usage: usage}, nil
}

func calculateGeminiCost(inputTokens, outputTokens int64) float64 {
	inputCost := float64(inputTokens) / 1_000_000 * geminiInputPricePerMillion
	outputCost := float64(outputTokens) / 1_000_000 * geminiOutputPricePerMillion
	return inputCost + outputCost
}

func parseItemDescription(text string) (*ItemDescription, error) {
	// Clean up the response - remove markdown code blocks if present
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var desc ItemDescription
	if err := json.Unmarshal([]byte(text), &desc); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w (response: %s)", err, text)
	}

	return &desc, nil
}

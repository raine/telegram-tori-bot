package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
	"google.golang.org/genai"
)

const (
	geminiModel     = "gemini-3-flash-preview"
	geminiLiteModel = "gemini-2.5-flash-lite"
)

// Gemini pricing (per million tokens)
const (
	geminiInputPricePerMillion      = 0.50 // $0.50 per 1M input tokens (text/image/video)
	geminiOutputPricePerMillion     = 3.00 // $3.00 per 1M output tokens (including thinking)
	geminiLiteInputPricePerMillion  = 0.075
	geminiLiteOutputPricePerMillion = 0.30
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

const categorySelectionPrompt = `Select the most appropriate category for this item from the provided list.

Item Title: %s
Item Description: %s

Available Categories:
%s

Respond with a JSON object containing the best matching "category_id".
Example: {"category_id": 123}

Respond ONLY with the JSON object.`

const attributeSelectionPrompt = `Analyze the item title and description to select the most appropriate option for each attribute.

Item Title: %s
Item Description: %s

For each attribute below, select the best matching option ID.
If the correct option cannot be confidently determined from the text, return null for that attribute. Do not guess.

Attributes:
%s

Respond ONLY with a JSON object mapping attribute names to option IDs.
Example: {"condition": 123, "color": 456, "size": null}`

// GeminiAnalyzer uses Google's Gemini API for image analysis and category selection.
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
		usage.CostUSD = calculateGeminiCost(usage.InputTokens, usage.OutputTokens, geminiInputPricePerMillion, geminiOutputPricePerMillion)
	}

	log.Info().
		Str("model", geminiModel).
		Int64("inputTokens", usage.InputTokens).
		Int64("outputTokens", usage.OutputTokens).
		Float64("costUSD", usage.CostUSD).
		Msg("vision llm call")

	return &AnalysisResult{Item: item, Usage: usage}, nil
}

func calculateGeminiCost(inputTokens, outputTokens int64, inputPrice, outputPrice float64) float64 {
	inputCost := float64(inputTokens) / 1_000_000 * inputPrice
	outputCost := float64(outputTokens) / 1_000_000 * outputPrice
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

// buildCategoryPath builds a breadcrumb path from a CategoryPrediction
func buildCategoryPath(cat tori.CategoryPrediction) string {
	var parts []string
	if cat.Parent != nil {
		if cat.Parent.Parent != nil {
			parts = append(parts, cat.Parent.Parent.Label)
		}
		parts = append(parts, cat.Parent.Label)
	}
	parts = append(parts, cat.Label)
	return strings.Join(parts, " > ")
}

// SelectCategory selects the best category from the list using Gemini Lite
func (g *GeminiAnalyzer) SelectCategory(ctx context.Context, title, description string, predictions []tori.CategoryPrediction) (int, error) {
	if len(predictions) == 0 {
		return 0, fmt.Errorf("no predictions provided")
	}

	var catLines []string
	for _, p := range predictions {
		label := buildCategoryPath(p)
		catLines = append(catLines, fmt.Sprintf("- ID: %d, Label: %s", p.ID, label))
	}

	prompt := fmt.Sprintf(categorySelectionPrompt, title, description, strings.Join(catLines, "\n"))

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("gemini lite call failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return 0, fmt.Errorf("empty response from gemini lite")
	}

	text := result.Text()

	// Extract JSON object from response (handles markdown blocks and chatty responses)
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return 0, fmt.Errorf("no JSON object found in response: %s", text)
	}
	text = text[start : end+1]

	var resp struct {
		CategoryID int `json:"category_id"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return 0, fmt.Errorf("failed to parse category json: %w (response: %s)", err, text)
	}

	// Log usage and cost
	if result.UsageMetadata != nil {
		cost := calculateGeminiCost(
			int64(result.UsageMetadata.PromptTokenCount),
			int64(result.UsageMetadata.CandidatesTokenCount),
			geminiLiteInputPricePerMillion,
			geminiLiteOutputPricePerMillion,
		)
		log.Info().
			Str("model", geminiLiteModel).
			Int("inputTokens", int(result.UsageMetadata.PromptTokenCount)).
			Int("outputTokens", int(result.UsageMetadata.CandidatesTokenCount)).
			Float64("costUSD", cost).
			Int("selectedCategoryID", resp.CategoryID).
			Msg("category selection llm call")
	}

	return resp.CategoryID, nil
}

// SelectAttributes selects the best options for the given attributes using Gemini Lite.
// Returns a map of attribute name -> selected option ID.
// Attributes where the LLM returns null are omitted from the result.
func (g *GeminiAnalyzer) SelectAttributes(ctx context.Context, title, description string, attrs []tori.Attribute) (map[string]int, error) {
	if len(attrs) == 0 {
		return nil, nil
	}

	var attrBuilder strings.Builder
	for _, attr := range attrs {
		attrBuilder.WriteString(fmt.Sprintf("\nAttribute: %s (name: %s)\nOptions:\n", attr.Label, attr.Name))
		for _, opt := range attr.Options {
			attrBuilder.WriteString(fmt.Sprintf("- %d: %s\n", opt.ID, opt.Label))
		}
	}

	prompt := fmt.Sprintf(attributeSelectionPrompt, title, description, attrBuilder.String())

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini attribute selection failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from gemini")
	}

	text := result.Text()

	// Extract JSON object from response
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response: %s", text)
	}
	text = text[start : end+1]

	// Parse as map with nullable ints
	var selections map[string]*int
	if err := json.Unmarshal([]byte(text), &selections); err != nil {
		return nil, fmt.Errorf("failed to parse attribute json: %w (response: %s)", err, text)
	}

	// Filter out nulls and convert to map[string]int
	finalMap := make(map[string]int)
	for k, v := range selections {
		if v != nil {
			finalMap[k] = *v
		}
	}

	// Log usage and cost
	if result.UsageMetadata != nil {
		cost := calculateGeminiCost(
			int64(result.UsageMetadata.PromptTokenCount),
			int64(result.UsageMetadata.CandidatesTokenCount),
			geminiLiteInputPricePerMillion,
			geminiLiteOutputPricePerMillion,
		)
		log.Info().
			Str("model", geminiLiteModel).
			Int("inputTokens", int(result.UsageMetadata.PromptTokenCount)).
			Int("outputTokens", int(result.UsageMetadata.CandidatesTokenCount)).
			Float64("costUSD", cost).
			Int("autoSelectedCount", len(finalMap)).
			Msg("attribute selection llm call")
	}

	return finalMap, nil
}

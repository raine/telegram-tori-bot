package vision

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/openai/openai-go"
)

const openaiModel = "gpt-5.2"

// GPT-5.2 pricing (per million tokens)
const (
	openaiInputPricePerMillion  = 1.75  // $1.75 per 1M input tokens
	openaiOutputPricePerMillion = 14.00 // $14.00 per 1M output tokens
)

const openaiPrompt = `Analyze this image and identify the item for selling on a secondhand marketplace.

Respond in JSON format with these fields:
- title: A short, descriptive title suitable for a marketplace listing (in Finnish if possible, otherwise English). Include brand and model if visible.
- description: A longer description with relevant details about the item (2-3 sentences, in Finnish if possible)
- brand: The brand name if identifiable (empty string if unknown)
- model: The model name or number if identifiable (empty string if unknown)

Example response:
{"title": "Logitech G Pro X Superlight langaton pelihiiri", "description": "Kevyt langaton pelihiiri ammattipelaamiseen. Logitech Hero 25K -sensori, jopa 70 tunnin akunkesto.", "brand": "Logitech", "model": "G Pro X Superlight"}

Respond ONLY with the JSON object, no markdown or other text.`

// OpenAIAnalyzer uses OpenAI's GPT-4o Vision API for image analysis.
type OpenAIAnalyzer struct {
	client openai.Client
}

// NewOpenAIAnalyzer creates a new OpenAI-based analyzer.
// It uses the OPENAI_API_KEY environment variable for authentication.
func NewOpenAIAnalyzer() *OpenAIAnalyzer {
	return &OpenAIAnalyzer{client: openai.NewClient()}
}

// AnalyzeImage implements the Analyzer interface using OpenAI.
func (o *OpenAIAnalyzer) AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
	// Encode image as base64 data URL
	b64Data := base64.StdEncoding.EncodeToString(imageData)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, b64Data)

	resp, err := o.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openaiModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.TextContentPart(openaiPrompt),
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: dataURL,
				}),
			}),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	text := resp.Choices[0].Message.Content
	item, err := parseItemDescription(text)
	if err != nil {
		return nil, err
	}

	// Calculate usage and cost
	usage := Usage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		TotalTokens:  resp.Usage.TotalTokens,
		CostUSD:      calculateOpenAICost(resp.Usage.PromptTokens, resp.Usage.CompletionTokens),
	}

	return &AnalysisResult{Item: item, Usage: usage}, nil
}

func calculateOpenAICost(inputTokens, outputTokens int64) float64 {
	inputCost := float64(inputTokens) / 1_000_000 * openaiInputPricePerMillion
	outputCost := float64(outputTokens) / 1_000_000 * openaiOutputPricePerMillion
	return inputCost + outputCost
}

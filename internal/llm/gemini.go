package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/raine/telegram-tori-bot/internal/tori"
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

const geminiMultiImagePrompt = `Analyze these images showing the same item from different angles and identify it for selling on a secondhand marketplace.

The images show the same item - use all images together to get a complete understanding of the item's condition, brand, model, and features.

Respond in JSON format with these fields:
- title: A short, descriptive title suitable for a marketplace listing (in Finnish if possible, otherwise English). Include brand and model if visible.
- description: A longer description with relevant details about the item (2-3 sentences, in Finnish if possible). Mention notable features or condition details visible across the images.
- brand: The brand name if identifiable (empty string if unknown)
- model: The model name or number if identifiable (empty string if unknown)

Example response:
{"title": "Logitech G Pro X Superlight langaton pelihiiri", "description": "Kevyt langaton pelihiiri ammattipelaamiseen. Logitech Hero 25K -sensori, jopa 70 tunnin akunkesto. Hyvässä kunnossa, ei näkyviä käytön jälkiä.", "brand": "Logitech", "model": "G Pro X Superlight"}

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
If the correct option cannot be confidently determined from the text, use -1 for that attribute. Do not guess.

Attributes:
%s`

const giveawayDescriptionRewritePrompt = `Transform this marketplace listing description from a selling context to a giveaway context.

Original description:
%s

Rules:
- Replace "Myydään" with "Annetaan" if present at the start
- Replace other selling phrases with giveaway equivalents
- Keep all other details about the item unchanged
- Do not add any new information
- Keep the same language (Finnish)
- Return ONLY the transformed description text, no JSON or other formatting`

const priceSearchQueryPrompt = `Generate an optimized search query to find similar items for price comparison on a marketplace.

Title: %s
Description: %s

Extract the core product identifier that would match similar items:
- For electronics: model number/name (e.g., "RTX 3070 Ti", "iPhone 13 Pro", "PlayStation 5")
- For furniture: type and key characteristics (e.g., "nahkasohva", "työtuoli")
- For clothing: brand and type (e.g., "Nike Air Max", "Fjällräven takki")
- For games/media: title and platform if relevant (e.g., "Zelda Switch", "Harry Potter kirja")

Do NOT include:
- Brand-specific variant names (e.g., "Zotac Trinity", "ASUS ROG Strix")
- Generic category words in Finnish (e.g., "näytönohjain", "puhelin", "tuoli")
- Condition descriptors (e.g., "uusi", "käytetty", "hyvä kunto")
- Marketing terms (e.g., "Gaming", "Pro", "Ultimate")

Respond with ONLY the search query (1-5 words), no quotes or explanation.`

const categoryKeywordExtractionPrompt = `Analysoi tämä tuote ja tunnista mikä esine se on. Anna 1-3 suomenkielistä hakusanaa, jotka kuvaavat tuotteen tyyppiä tai kategoriaa.

Otsikko: %s
Kuvaus: %s

TÄRKEÄÄ: Anna sekä spesifi termi ETTÄ yleisempi yläkategoria. Esimerkiksi:
- "Salli Twin satulatuoli" -> ["satulatuoli", "työtuoli", "tuolit"]
- "iPhone 13 Pro" -> ["älypuhelin", "puhelin"]
- "IKEA Kallax hylly" -> ["hylly", "kaapit ja hyllyt", "huonekalu"]
- "Sony WH-1000XM5" -> ["kuulokkeet", "äänentoistolaitteet"]

Älä käytä tuotemerkkejä (Apple, IKEA, Salli jne.) hakusanoina.

Vastaa JSON-objektilla, jossa on "keywords" lista.
Esimerkki: {"keywords": ["satulatuoli", "työtuoli", "tuolit"]}

Vastaa VAIN JSON-objektilla, ei muuta tekstiä.`

const templateGenerationPrompt = `Generate a Go text/template string based on the user's description for a marketplace listing footer.

Available variables in the template context:
- .shipping (boolean): true if shipping is possible
- .giveaway (boolean): true if the item is being given away for free
- .price (integer): price in euros (0 if giveaway)

Supported Go template syntax:
- {{if .shipping}}...{{end}}
- {{if not .shipping}}...{{end}}
- {{if .giveaway}}...{{end}}
- {{.price}} (prints the price)

User description: %q

Rules:
1. Return ONLY the template string. No markdown code blocks, no JSON, no explanations.
2. Ensure the template logic matches the user's description.
3. If the user asks for specific text, include it exactly.
4. If the user doesn't mention specific conditions, output the text unconditionally.
5. Do not include any surrounding quotes or backticks.

Example input: "Add text 'No shipping' if shipping is not selected"
Example output: {{if not .shipping}}No shipping{{end}}`

const hierarchicalCategoryPrompt = `Select the most appropriate category for this item from the list below.

Item Title: %s
Item Description: %s
%s
Available Categories:
%s

Respond with a JSON object containing the "category_id" of the best match.
If NONE of the categories are appropriate, respond with {"category_id": 0}.
Example: {"category_id": 123}

Respond ONLY with the JSON object.`

const editIntentPrompt = `Olet avustaja, joka auttaa muokkaamaan myynti-ilmoituksen tietoja. Käyttäjä haluaa muokata ilmoitustaan luonnollisella kielellä.

Nykyinen ilmoitus:
- Otsikko: %s
- Kuvaus: %s
- Hinta: %d€
%s
Käyttäjän viesti: "%s"

Analysoi käyttäjän viesti ja päättele mitä muutoksia hän haluaa tehdä. Palauta JSON-objekti seuraavilla kentillä (käytä null tai tyhjä lista jos ei muuteta):

- new_price: uusi hinta kokonaislukuna (ilman €-merkkiä), null jos ei muuteta
- new_title: uusi otsikko kokonaisuudessaan, null jos ei muuteta
- new_description: uusi kuvaus kokonaisuudessaan (jos käyttäjä haluaa lisätä, poistaa tai muuttaa kuvausta, palauta koko muokattu kuvaus), null jos ei muuteta
- reset_attributes: lista attribuuttien nimistä jotka käyttäjä haluaa vaihtaa tai poistaa, tyhjä lista [] jos ei muuteta

Esimerkkejä (oletetaan kuvaus on "Toimiva hiiri, pieni naarmu"):
- "Vaihda hinnaksi 40e" -> {"new_price": 40, "new_title": null, "new_description": null, "reset_attributes": []}
- "Lisää että koirataloudesta" -> {"new_price": null, "new_title": null, "new_description": "Toimiva hiiri, pieni naarmu. Koirataloudesta.", "reset_attributes": []}
- "Poista maininta naarmusta" -> {"new_price": null, "new_title": null, "new_description": "Toimiva hiiri.", "reset_attributes": []}
- "Muuta otsikoksi Nintendo Switch" -> {"new_price": null, "new_title": "Nintendo Switch", "new_description": null, "reset_attributes": []}
- "Vaihda merkki" -> {"new_price": null, "new_title": null, "new_description": null, "reset_attributes": ["brand"]}
- "Poista merkki" -> {"new_price": null, "new_title": null, "new_description": null, "reset_attributes": ["brand"]}
- "Merkki on väärä" -> {"new_price": null, "new_title": null, "new_description": null, "reset_attributes": ["brand"]}

Vastaa VAIN JSON-objektilla, ei muuta tekstiä.`

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
// It delegates to AnalyzeImages with a single-element slice.
func (g *GeminiAnalyzer) AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
	return g.AnalyzeImages(ctx, [][]byte{imageData})
}

// AnalyzeImages analyzes one or more images together.
// For single images, uses the single-image prompt. For multiple images,
// uses the multi-image prompt for better context from different angles.
func (g *GeminiAnalyzer) AnalyzeImages(ctx context.Context, images [][]byte) (*AnalysisResult, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("no images provided")
	}

	// Limit to 10 images (Telegram's album limit)
	if len(images) > 10 {
		images = images[:10]
	}

	// Choose prompt based on image count
	prompt := geminiPrompt
	if len(images) > 1 {
		prompt = geminiMultiImagePrompt
	}

	// Build parts: prompt first, then all images
	parts := []*genai.Part{
		genai.NewPartFromText(prompt),
	}
	for _, imgData := range images {
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{Data: imgData, MIMEType: "image/jpeg"},
		})
	}

	return g.executeVisionRequest(ctx, parts, len(images))
}

// executeVisionRequest executes the Gemini API call and parses the response.
func (g *GeminiAnalyzer) executeVisionRequest(ctx context.Context, parts []*genai.Part, imageCount int) (*AnalysisResult, error) {
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

	item, err := parseItemDescription(result.Text())
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
		Int("imageCount", imageCount).
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

// extractJSONObject extracts a JSON object from text that may contain markdown
// code blocks or other formatting. Returns the extracted JSON string or an error.
func extractJSONObject(text string) (string, error) {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return "", fmt.Errorf("no JSON object found in response: %s", text)
	}
	return text[start : end+1], nil
}

func parseItemDescription(text string) (*ItemDescription, error) {
	jsonStr, err := extractJSONObject(text)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	var desc ItemDescription
	if err := json.Unmarshal([]byte(jsonStr), &desc); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w (response: %s)", err, jsonStr)
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

	jsonStr, err := extractJSONObject(result.Text())
	if err != nil {
		return 0, err
	}

	var resp struct {
		CategoryID int `json:"category_id"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return 0, fmt.Errorf("failed to parse category json: %w (response: %s)", err, jsonStr)
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

// SelectCategoryHierarchical selects a category from a list at a given tree level.
// It's designed for hierarchical tree-climbing where we traverse level-by-level.
// pathContext provides the breadcrumb of previously selected categories (e.g., "Ajoneuvot > Autot")
// for context in deeper levels.
func (g *GeminiAnalyzer) SelectCategoryHierarchical(ctx context.Context, title, description string, categories []tori.CategoryPrediction, pathContext string) (int, error) {
	if len(categories) == 0 {
		return 0, fmt.Errorf("no categories provided")
	}

	var catLines []string
	for _, p := range categories {
		catLines = append(catLines, fmt.Sprintf("- ID: %d, Label: %s", p.ID, p.Label))
	}

	// Add path context if navigating deeper levels
	pathContextSection := ""
	if pathContext != "" {
		pathContextSection = fmt.Sprintf("Current path: %s\n", pathContext)
	}

	prompt := fmt.Sprintf(hierarchicalCategoryPrompt, title, description, pathContextSection, strings.Join(catLines, "\n"))

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("gemini lite call failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return 0, fmt.Errorf("empty response from gemini lite")
	}

	jsonStr, err := extractJSONObject(result.Text())
	if err != nil {
		return 0, err
	}

	var resp struct {
		CategoryID int `json:"category_id"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return 0, fmt.Errorf("failed to parse category json: %w (response: %s)", err, jsonStr)
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
			Str("pathContext", pathContext).
			Msg("hierarchical category selection llm call")
	}

	return resp.CategoryID, nil
}

// buildAttributeSelectionSchema creates a dynamic JSON schema for attribute selection.
// The schema ensures the LLM returns an object with attribute names as keys and integer option IDs as values.
func buildAttributeSelectionSchema(attrs []tori.Attribute) *genai.Schema {
	properties := make(map[string]*genai.Schema)
	required := make([]string, 0, len(attrs))
	propertyOrdering := make([]string, 0, len(attrs))

	for _, attr := range attrs {
		properties[attr.Name] = &genai.Schema{
			Type:        genai.TypeInteger,
			Description: fmt.Sprintf("Option ID for %s. Use -1 if uncertain.", attr.Label),
		}
		required = append(required, attr.Name)
		propertyOrdering = append(propertyOrdering, attr.Name)
	}

	return &genai.Schema{
		Type:             genai.TypeObject,
		Properties:       properties,
		Required:         required,
		PropertyOrdering: propertyOrdering,
	}
}

// SelectAttributes selects the best options for the given attributes using Gemini Lite.
// Returns a map of attribute name -> selected option ID.
// Attributes where the LLM returns -1 (uncertain) are omitted from the result.
// Uses Gemini's structured output (ResponseSchema) to ensure valid JSON with correct keys.
func (g *GeminiAnalyzer) SelectAttributes(ctx context.Context, title, description string, attrs []tori.Attribute) (map[string]int, error) {
	if len(attrs) == 0 {
		return nil, nil
	}

	var attrBuilder strings.Builder
	for _, attr := range attrs {
		attrBuilder.WriteString(fmt.Sprintf("\nAttribute: %s (name: %s)\nOptions:\n", attr.Label, attr.Name))
		var optLabels []string
		for _, opt := range attr.Options {
			attrBuilder.WriteString(fmt.Sprintf("- %d: %s\n", opt.ID, opt.Label))
			optLabels = append(optLabels, fmt.Sprintf("%d:%s", opt.ID, opt.Label))
		}
		log.Debug().Str("attribute", attr.Label).Str("name", attr.Name).Strs("options", optLabels).Msg("attribute options for llm selection")
	}

	prompt := fmt.Sprintf(attributeSelectionPrompt, title, description, attrBuilder.String())
	log.Debug().Str("prompt", prompt).Msg("attribute selection llm input")

	// Build dynamic schema based on the attributes
	schema := buildAttributeSelectionSchema(attrs)

	// Configure generation with structured output
	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   schema,
	}

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, config)
	if err != nil {
		return nil, fmt.Errorf("gemini attribute selection failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from gemini")
	}

	text := result.Text()
	log.Debug().Str("response", text).Msg("attribute selection llm output")

	// Parse the JSON response (schema ensures it's valid JSON with correct structure)
	var selections map[string]int
	if err := json.Unmarshal([]byte(text), &selections); err != nil {
		return nil, fmt.Errorf("failed to parse attribute json: %w (response: %s)", err, text)
	}

	// Build a lookup map for valid option IDs per attribute
	validOptions := make(map[string]map[int]bool)
	for _, attr := range attrs {
		validOptions[attr.Name] = make(map[int]bool)
		for _, opt := range attr.Options {
			validOptions[attr.Name][opt.ID] = true
		}
	}

	// Filter out -1 values (uncertain) and validate option IDs
	finalMap := make(map[string]int)
	for k, v := range selections {
		if v == -1 {
			log.Debug().Str("attribute", k).Msg("attribute returned -1 by llm, will prompt user")
			continue
		}

		// Validate the ID actually exists for this attribute
		if opts, ok := validOptions[k]; ok && opts[v] {
			finalMap[k] = v
			log.Debug().Str("attribute", k).Int("selectedOptionId", v).Msg("attribute auto-selected by llm")
		} else {
			log.Warn().Str("attribute", k).Int("invalidId", v).Msg("llm returned invalid option ID, ignoring")
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

// RewriteDescriptionForGiveaway rewrites a description from selling to giveaway context.
// Uses Gemini Lite to transform "Myydään" to "Annetaan" and similar phrases.
func (g *GeminiAnalyzer) RewriteDescriptionForGiveaway(ctx context.Context, description string) (string, error) {
	if description == "" {
		return description, nil
	}

	prompt := fmt.Sprintf(giveawayDescriptionRewritePrompt, description)

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return "", fmt.Errorf("gemini giveaway rewrite failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	text := strings.TrimSpace(result.Text())

	// Strip markdown code blocks if present (LLM may occasionally add them)
	text = strings.TrimPrefix(text, "```text")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

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
			Msg("giveaway description rewrite llm call")
	}

	return text, nil
}

// EditIntent represents the parsed intent from a natural language edit command.
type EditIntent struct {
	NewPrice        *int     `json:"new_price"`
	NewTitle        *string  `json:"new_title"`
	NewDescription  *string  `json:"new_description"`
	ResetAttributes []string `json:"reset_attributes"` // Attribute names to reset/reselect (e.g., ["brand"])
}

// CurrentDraftInfo contains the current draft state for the LLM to make edit decisions.
type CurrentDraftInfo struct {
	Title       string
	Description string
	Price       int
	Attributes  []AttributeInfo // Available attributes that can be reset
}

// AttributeInfo provides context about an editable attribute.
type AttributeInfo struct {
	Label string // User-visible label (e.g., "Merkki")
	Name  string // Internal name (e.g., "brand")
}

// ParseEditIntent parses a natural language edit command and returns the intended changes.
func (g *GeminiAnalyzer) ParseEditIntent(ctx context.Context, message string, draft *CurrentDraftInfo) (*EditIntent, error) {
	// Build attributes context for the prompt
	var attrsContext string
	if len(draft.Attributes) > 0 {
		var attrLines []string
		for _, attr := range draft.Attributes {
			attrLines = append(attrLines, fmt.Sprintf("  - %s (nimi: %s)", attr.Label, attr.Name))
		}
		attrsContext = fmt.Sprintf("- Muokattavat attribuutit:\n%s\n", strings.Join(attrLines, "\n"))
	}

	prompt := fmt.Sprintf(editIntentPrompt, draft.Title, draft.Description, draft.Price, attrsContext, message)

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini edit intent failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from gemini")
	}

	jsonStr, err := extractJSONObject(result.Text())
	if err != nil {
		return nil, err
	}

	var intent EditIntent
	if err := json.Unmarshal([]byte(jsonStr), &intent); err != nil {
		return nil, fmt.Errorf("failed to parse edit intent json: %w (response: %s)", err, jsonStr)
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
			Str("message", message).
			Msg("edit intent llm call")
	}

	return &intent, nil
}

// ExtractCategoryKeywords extracts Finnish category keywords from title and description.
// Used for fallback category search when Tori's predictions are wrong.
func (g *GeminiAnalyzer) ExtractCategoryKeywords(ctx context.Context, title, description string) ([]string, error) {
	prompt := fmt.Sprintf(categoryKeywordExtractionPrompt, title, description)

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("keyword extraction failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from gemini")
	}

	jsonStr, err := extractJSONObject(result.Text())
	if err != nil {
		return nil, err
	}

	var resp struct {
		Keywords []string `json:"keywords"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse keywords json: %w (response: %s)", err, jsonStr)
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
			Strs("keywords", resp.Keywords).
			Msg("category keyword extraction llm call")
	}

	return resp.Keywords, nil
}

// GeneratePriceSearchQuery generates an optimized search query for finding similar items.
// It extracts the core product identifier from the title and description.
func (g *GeminiAnalyzer) GeneratePriceSearchQuery(ctx context.Context, title, description string) (string, error) {
	// Truncate description to save tokens - core identifier is unlikely at the end
	if len(description) > 500 {
		description = description[:500]
	}

	prompt := fmt.Sprintf(priceSearchQueryPrompt, title, description)

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return "", fmt.Errorf("gemini price search query failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	query := strings.TrimSpace(result.Text())

	// Strip markdown code blocks if present
	query = strings.TrimPrefix(query, "```text")
	query = strings.TrimPrefix(query, "```")
	query = strings.TrimSuffix(query, "```")
	query = strings.TrimSpace(query)

	// Strip surrounding quotes
	query = strings.Trim(query, `"'`)

	// If output is suspiciously long (likely a refusal/explanation), return empty to trigger fallback
	if len(query) > 50 {
		return "", nil
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
			Str("title", title).
			Str("query", query).
			Msg("price search query llm call")
	}

	return query, nil
}

// GenerateTemplate generates a Go text/template string based on a user description.
func (g *GeminiAnalyzer) GenerateTemplate(ctx context.Context, description string) (string, error) {
	prompt := fmt.Sprintf(templateGenerationPrompt, description)

	result, err := g.client.Models.GenerateContent(ctx, geminiLiteModel, []*genai.Content{
		genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser),
	}, nil)
	if err != nil {
		return "", fmt.Errorf("gemini template generation failed: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	text := strings.TrimSpace(result.Text())

	// Strip markdown code blocks if present
	text = strings.TrimPrefix(text, "```text")
	text = strings.TrimPrefix(text, "```go")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	// Log usage
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
			Msg("template generation llm call")
	}

	return text, nil
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/raine/telegram-tori-bot/vision"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <image-path> [gemini|openai|both]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nEnvironment variables:\n")
		fmt.Fprintf(os.Stderr, "  GOOGLE_API_KEY - Required for Gemini\n")
		fmt.Fprintf(os.Stderr, "  OPENAI_API_KEY - Required for OpenAI\n")
		os.Exit(1)
	}

	imagePath := os.Args[1]
	provider := "both"
	if len(os.Args) >= 3 {
		provider = os.Args[2]
	}

	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read image: %v\n", err)
		os.Exit(1)
	}

	mimeType := getMimeType(imagePath)
	ctx := context.Background()

	switch provider {
	case "gemini":
		runGemini(ctx, imageData, mimeType)
	case "openai":
		runOpenAI(ctx, imageData, mimeType)
	case "both":
		runGemini(ctx, imageData, mimeType)
		fmt.Println("\n" + strings.Repeat("-", 50) + "\n")
		runOpenAI(ctx, imageData, mimeType)
	default:
		fmt.Fprintf(os.Stderr, "Unknown provider: %s (use gemini, openai, or both)\n", provider)
		os.Exit(1)
	}
}

func runGemini(ctx context.Context, imageData []byte, mimeType string) {
	fmt.Println("=== GEMINI ===")

	analyzer, err := vision.NewGeminiAnalyzer(ctx)
	if err != nil {
		fmt.Printf("Error creating Gemini analyzer: %v\n", err)
		return
	}

	result, err := analyzer.AnalyzeImage(ctx, imageData, mimeType)
	if err != nil {
		fmt.Printf("Error analyzing image: %v\n", err)
		return
	}

	printResult(result)
}

func runOpenAI(ctx context.Context, imageData []byte, mimeType string) {
	fmt.Println("=== OPENAI ===")

	analyzer := vision.NewOpenAIAnalyzer()

	result, err := analyzer.AnalyzeImage(ctx, imageData, mimeType)
	if err != nil {
		fmt.Printf("Error analyzing image: %v\n", err)
		return
	}

	printResult(result)
}

func printResult(result *vision.AnalysisResult) {
	fmt.Printf("Title:       %s\n", result.Item.Title)
	fmt.Printf("Brand:       %s\n", result.Item.Brand)
	fmt.Printf("Model:       %s\n", result.Item.Model)
	fmt.Printf("Condition:   %s\n", result.Item.Condition)
	fmt.Printf("Description: %s\n", result.Item.Description)
	fmt.Println()
	fmt.Printf("Tokens:      %d in / %d out / %d total\n",
		result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.TotalTokens)
	fmt.Printf("Cost:        $%.6f\n", result.Usage.CostUSD)
}

func getMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

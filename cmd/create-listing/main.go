package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/raine/telegram-tori-bot/internal/tori"
)

const tokenFile = "auth_tokens.json"

func main() {
	fmt.Println("=== Tori Listing Creator ===")
	fmt.Println()

	ctx := context.Background()

	// Load tokens
	tokens, err := loadTokens()
	if err != nil {
		fmt.Printf("Failed to load tokens: %v\n", err)
		fmt.Println("Run login-test first to generate auth_tokens.json")
		os.Exit(1)
	}

	bearerToken := tokens["bearer_token"]
	if bearerToken == "" {
		fmt.Println("No bearer_token found in auth_tokens.json")
		os.Exit(1)
	}

	fmt.Printf("Loaded auth from %s\n", tokenFile)
	if userID := tokens["user_id"]; userID != "" {
		fmt.Printf("User ID: %s\n", userID)
	}

	// Get image path
	imagePath := prompt("Enter image path")
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		fmt.Printf("Failed to read image: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Image loaded: %d bytes\n\n", len(imageData))

	// Create client with a random installation ID for CLI usage
	// In the bot, this ID is persisted per user; for CLI it's generated fresh
	installationID := uuid.New().String()
	client := tori.NewAdinputClient(bearerToken, installationID)

	// Step 1: Create draft
	fmt.Println("Creating draft ad...")
	draft, _, err := client.CreateDraftAd(ctx)
	if err != nil {
		fmt.Printf("Failed to create draft: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Draft created: ID %s\n\n", draft.ID)

	etag := draft.ETag

	// Step 2: Upload image
	fmt.Println("Uploading image...")
	uploadResp, err := client.UploadImage(ctx, draft.ID, imageData)
	if err != nil {
		fmt.Printf("Failed to upload image: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Image uploaded: %s\n\n", uploadResp.ImagePath)

	// Step 3: Set image on item
	fmt.Println("Setting image on item...")
	patchResp, err := client.PatchItem(ctx, draft.ID, etag, map[string]any{
		"image": []map[string]any{
			{
				"uri":    uploadResp.ImagePath,
				"width":  4032,
				"height": 3024,
				"type":   "image/jpg",
			},
		},
	})
	if err != nil {
		fmt.Printf("Failed to set image: %v\n", err)
		os.Exit(1)
	}
	etag = patchResp.ETag
	fmt.Printf("✓ Image set on item\n\n")

	// Step 4: Get category predictions
	fmt.Println("Getting category predictions from image...")
	categories, err := client.GetCategoryPredictions(ctx, draft.ID)
	if err != nil {
		fmt.Printf("Failed to get predictions: %v\n", err)
		os.Exit(1)
	}

	// Show category options
	fmt.Println("\nSuggested categories:")
	for i, cat := range categories {
		path := getCategoryPath(cat)
		fmt.Printf("  [%d] %s\n", i+1, path)
	}
	fmt.Printf("  [0] Enter category ID manually\n")

	categoryChoice := promptInt("Select category", 0, len(categories))
	var selectedCategory int
	if categoryChoice == 0 {
		selectedCategory = promptInt("Enter category ID", 1, 9999)
	} else {
		selectedCategory = categories[categoryChoice-1].ID
	}

	// Step 5: Set category
	fmt.Printf("\nSetting category %d...\n", selectedCategory)
	patchResp, err = client.PatchItem(ctx, draft.ID, etag, map[string]any{
		"category": selectedCategory,
	})
	if err != nil {
		fmt.Printf("Failed to set category: %v\n", err)
		os.Exit(1)
	}
	etag = patchResp.ETag
	fmt.Printf("✓ Category set\n\n")

	// Step 6: Get attributes for category
	fmt.Println("Fetching attributes for category...")
	attrs, err := client.GetAttributes(ctx, draft.ID)
	if err != nil {
		fmt.Printf("Failed to get attributes: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Category: %s (ID: %d)\n\n", attrs.Category.Label, attrs.Category.ID)

	// Collect attribute values
	extraFields := make(map[string]any)
	for _, attr := range attrs.Attributes {
		if attr.Type == "SELECT" && len(attr.Options) > 0 {
			fmt.Printf("%s (%s):\n", attr.Name, attr.Label)
			for i, opt := range attr.Options {
				fmt.Printf("  [%d] %s\n", i+1, opt.Label)
			}
			choice := promptInt("Select", 1, len(attr.Options))
			extraFields[attr.Name] = strconv.Itoa(attr.Options[choice-1].ID)
			fmt.Println()
		}
	}

	// Step 7: Get listing details
	title := prompt("Enter title")
	description := prompt("Enter description")
	priceStr := prompt("Enter price (EUR, 0 for free)")
	price, _ := strconv.Atoi(priceStr)
	postalCode := prompt("Enter postal code")

	fmt.Println("\nTrade type:")
	fmt.Println("  [1] Myydään (Sell)")
	fmt.Println("  [2] Annetaan (Give away)")
	tradeType := promptInt("Select", 1, 2)
	tradeTypeStr := strconv.Itoa(tradeType)

	fmt.Println("\nDelivery options:")
	meetup := promptYesNo("Allow meetup?", true)
	shipping := promptYesNo("Allow shipping?", false)

	// Step 8: Update ad with all fields
	fmt.Println("\nUpdating listing...")
	payload := tori.AdUpdatePayload{
		Category:    strconv.Itoa(selectedCategory),
		Title:       title,
		Description: description,
		TradeType:   tradeTypeStr,
		Location: []map[string]string{
			{"country": "FI", "postal-code": postalCode},
		},
		Image: []map[string]string{
			{
				"uri":    uploadResp.ImagePath,
				"width":  "4032",
				"height": "3024",
				"type":   "image/jpg",
			},
		},
		MultiImage: []map[string]any{
			{
				"path":        uploadResp.ImagePath,
				"url":         uploadResp.Location,
				"width":       4032,
				"height":      3024,
				"type":        "image/jpg",
				"description": "",
			},
		},
		Extra: extraFields,
	}

	// Price is an array like location
	if tradeType == 1 && price > 0 {
		payload.Extra["price"] = []map[string]any{
			{"price_amount": strconv.Itoa(price)},
		}
	}

	updateResp, err := client.UpdateAd(ctx, draft.ID, etag, payload)
	if err != nil {
		fmt.Printf("Failed to update ad: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Listing updated\n\n")

	// Step 9: Set delivery options
	fmt.Println("Setting delivery options...")
	err = client.SetDeliveryOptions(ctx, draft.ID, tori.DeliveryOptions{
		BuyNow:             false,
		Client:             "IOS",
		Meetup:             meetup,
		SellerPaysShipping: false,
		Shipping:           shipping,
	})
	if err != nil {
		fmt.Printf("Failed to set delivery options: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Delivery options set\n\n")

	// Step 10: Publish (SKIPPED for testing)
	fmt.Println("=== SKIPPING PUBLISH (test mode) ===")
	fmt.Printf("Draft ID: %s\n", draft.ID)
	fmt.Printf("ETag: %s\n", updateResp.ETag)
	fmt.Println("\nTo publish manually, POST to:")
	fmt.Printf("  %s/adinput/order/choices/%s\n", tori.AdinputBaseURL, draft.ID)
	fmt.Println("  Body: choices=urn:product:package-specification:10")
}

func loadTokens() (map[string]string, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}

	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}

	return tokens, nil
}

func prompt(label string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s: ", label)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func promptInt(label string, min, max int) int {
	for {
		input := prompt(fmt.Sprintf("%s [%d-%d]", label, min, max))
		val, err := strconv.Atoi(input)
		if err == nil && val >= min && val <= max {
			return val
		}
		fmt.Printf("Please enter a number between %d and %d\n", min, max)
	}
}

func promptYesNo(label string, defaultVal bool) bool {
	defaultStr := "Y/n"
	if !defaultVal {
		defaultStr = "y/N"
	}
	input := strings.ToLower(prompt(fmt.Sprintf("%s [%s]", label, defaultStr)))
	if input == "" {
		return defaultVal
	}
	return input == "y" || input == "yes"
}

func getCategoryPath(cat tori.CategoryPrediction) string {
	if cat.Parent == nil {
		return cat.Label
	}
	return getCategoryPath(*cat.Parent) + " > " + cat.Label
}

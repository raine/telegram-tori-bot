package tori

import (
	"encoding/json"
	"testing"
)

func TestGetAdWithModel_ParsesValuesCorrectly(t *testing.T) {
	// Test that the AdWithModel type can properly unmarshal JSON
	jsonData := `{
		"ad": {
			"id": "67890",
			"ad-type": "SELL",
			"etag": "xyz789",
			"update-url": "/update",
			"upload-url": "/upload",
			"checkout-url": "/checkout",
			"values": {
				"title": "My Item",
				"description": "Item description",
				"category": "5050",
				"price": {"value": "50", "currency": "EUR"},
				"condition": "good"
			}
		}
	}`

	var result AdWithModel
	err := json.Unmarshal([]byte(jsonData), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if result.Ad.ID != "67890" {
		t.Errorf("expected ad ID 67890, got %s", result.Ad.ID)
	}
	if result.Ad.ETag != "xyz789" {
		t.Errorf("expected etag xyz789, got %s", result.Ad.ETag)
	}
	if result.Ad.AdType != "SELL" {
		t.Errorf("expected ad-type SELL, got %s", result.Ad.AdType)
	}

	// Check values map
	if result.Ad.Values == nil {
		t.Fatal("expected values map to be non-nil")
	}
	if result.Ad.Values["title"] != "My Item" {
		t.Errorf("expected title 'My Item', got %v", result.Ad.Values["title"])
	}
	if result.Ad.Values["description"] != "Item description" {
		t.Errorf("expected description 'Item description', got %v", result.Ad.Values["description"])
	}
	if result.Ad.Values["category"] != "5050" {
		t.Errorf("expected category '5050', got %v", result.Ad.Values["category"])
	}

	// Check nested price value
	price, ok := result.Ad.Values["price"].(map[string]any)
	if !ok {
		t.Fatalf("expected price to be a map, got %T", result.Ad.Values["price"])
	}
	if price["value"] != "50" {
		t.Errorf("expected price value '50', got %v", price["value"])
	}
}

func TestAdWithModel_EmptyValues(t *testing.T) {
	// Test that empty values map is handled correctly
	jsonData := `{
		"ad": {
			"id": "11111",
			"ad-type": "SELL",
			"etag": "empty",
			"update-url": "/update",
			"upload-url": "/upload",
			"checkout-url": "/checkout",
			"values": {}
		}
	}`

	var result AdWithModel
	err := json.Unmarshal([]byte(jsonData), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if result.Ad.Values == nil {
		t.Error("expected values map to be non-nil (empty map)")
	}
	if len(result.Ad.Values) != 0 {
		t.Errorf("expected empty values map, got %d entries", len(result.Ad.Values))
	}
}

func TestAdWithModel_WithImagesAndLocation(t *testing.T) {
	// Test parsing complex nested structures like images and location arrays
	jsonData := `{
		"ad": {
			"id": "12345",
			"ad-type": "SELL",
			"etag": "abc123",
			"update-url": "/update",
			"upload-url": "/upload",
			"checkout-url": "/checkout",
			"values": {
				"title": "Test Item",
				"description": "A test description",
				"category": "3020",
				"location": [
					{"key": "region", "label": "Uusimaa"},
					{"key": "zipcode", "label": "00100"}
				],
				"image": [
					{"url": "https://example.com/image1.jpg"},
					{"url": "https://example.com/image2.jpg"}
				],
				"multi_image": [
					{"path": "/path/to/image1.jpg", "order": 0},
					{"path": "/path/to/image2.jpg", "order": 1}
				]
			}
		}
	}`

	var result AdWithModel
	err := json.Unmarshal([]byte(jsonData), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Check location array
	location, ok := result.Ad.Values["location"].([]any)
	if !ok {
		t.Fatalf("expected location to be an array, got %T", result.Ad.Values["location"])
	}
	if len(location) != 2 {
		t.Errorf("expected 2 location entries, got %d", len(location))
	}

	// Check image array
	images, ok := result.Ad.Values["image"].([]any)
	if !ok {
		t.Fatalf("expected image to be an array, got %T", result.Ad.Values["image"])
	}
	if len(images) != 2 {
		t.Errorf("expected 2 images, got %d", len(images))
	}

	// Check multi_image array
	multiImages, ok := result.Ad.Values["multi_image"].([]any)
	if !ok {
		t.Fatalf("expected multi_image to be an array, got %T", result.Ad.Values["multi_image"])
	}
	if len(multiImages) != 2 {
		t.Errorf("expected 2 multi_images, got %d", len(multiImages))
	}
}

func TestAdWithModel_AllFieldsPresent(t *testing.T) {
	// Test that all expected ad struct fields are parsed
	jsonData := `{
		"ad": {
			"id": "99999",
			"ad-type": "SELL",
			"etag": "test-etag-123",
			"update-url": "/adinput/ad/recommerce/99999/update",
			"upload-url": "/adinput/ad/recommerce/99999/upload",
			"checkout-url": "/adinput/order/choices/99999",
			"values": {"title": "Test"}
		}
	}`

	var result AdWithModel
	err := json.Unmarshal([]byte(jsonData), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if result.Ad.ID != "99999" {
		t.Errorf("expected ID '99999', got %s", result.Ad.ID)
	}
	if result.Ad.AdType != "SELL" {
		t.Errorf("expected AdType 'SELL', got %s", result.Ad.AdType)
	}
	if result.Ad.ETag != "test-etag-123" {
		t.Errorf("expected ETag 'test-etag-123', got %s", result.Ad.ETag)
	}
	if result.Ad.UpdateURL != "/adinput/ad/recommerce/99999/update" {
		t.Errorf("expected UpdateURL '/adinput/ad/recommerce/99999/update', got %s", result.Ad.UpdateURL)
	}
	if result.Ad.UploadURL != "/adinput/ad/recommerce/99999/upload" {
		t.Errorf("expected UploadURL '/adinput/ad/recommerce/99999/upload', got %s", result.Ad.UploadURL)
	}
	if result.Ad.CheckoutURL != "/adinput/order/choices/99999" {
		t.Errorf("expected CheckoutURL '/adinput/order/choices/99999', got %s", result.Ad.CheckoutURL)
	}
}

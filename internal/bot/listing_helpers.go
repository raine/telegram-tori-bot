package bot

import (
	"strconv"

	"github.com/raine/telegram-tori-bot/internal/tori"
)

// buildItemFields creates ItemFields from draft data for patching to /items.
// This is used by all publish flows (interactive, bulk, republish).
func buildItemFields(
	title string,
	description string,
	price int,
	collectedAttrs map[string]string,
) tori.ItemFields {
	// Convert condition to int
	conditionInt := 0
	if cond, ok := collectedAttrs["condition"]; ok {
		conditionInt, _ = strconv.Atoi(cond)
	}

	// Build attributes map (excluding condition which is handled separately)
	attrs := make(map[string]any)
	for k, v := range collectedAttrs {
		if k == "condition" {
			continue
		}
		// Convert to int if possible (SELECT attributes use int IDs)
		// Keep as string for TEXT or other attribute types
		if intVal, err := strconv.Atoi(v); err == nil {
			attrs[k] = intVal
		} else {
			attrs[k] = v
		}
	}

	return tori.ItemFields{
		Title:       title,
		Description: description,
		Condition:   conditionInt,
		Price:       price,
		Attributes:  attrs,
	}
}

// buildItemFieldsFromValues extracts ItemFields from ad values map (for republish).
func buildItemFieldsFromValues(values map[string]any) tori.ItemFields {
	fields := tori.ItemFields{
		Attributes: make(map[string]any),
	}

	if title, ok := values["title"].(string); ok {
		fields.Title = title
	}
	if desc, ok := values["description"].(string); ok {
		fields.Description = desc
	}
	if cond, ok := values["condition"].(string); ok {
		fields.Condition, _ = strconv.Atoi(cond)
	}
	// Extract price from array format
	if priceArr, ok := values["price"].([]any); ok && len(priceArr) > 0 {
		if priceMap, ok := priceArr[0].(map[string]any); ok {
			if amount, ok := priceMap["price_amount"].(string); ok {
				fields.Price, _ = strconv.Atoi(amount)
			}
		}
	}

	// Copy category-specific attributes (skip known fields)
	skipFields := map[string]bool{
		"title": true, "description": true, "condition": true, "price": true,
		"trade_type": true, "location": true, "image": true, "multi_image": true,
		"category": true,
	}
	for k, v := range values {
		if skipFields[k] {
			continue
		}
		// Handle different value types from API
		switch val := v.(type) {
		case string:
			// Convert string IDs to int for /items endpoint
			if intVal, err := strconv.Atoi(val); err == nil {
				fields.Attributes[k] = intVal
			} else {
				fields.Attributes[k] = val
			}
		case float64:
			// JSON numbers come as float64
			fields.Attributes[k] = int(val)
		case int:
			fields.Attributes[k] = val
			// Skip arrays/objects - they're handled separately or not needed
		}
	}

	return fields
}

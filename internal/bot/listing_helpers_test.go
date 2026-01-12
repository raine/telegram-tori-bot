package bot

import (
	"testing"

	"github.com/raine/telegram-tori-bot/internal/tori"
	"github.com/stretchr/testify/assert"
)

func TestBuildItemFields(t *testing.T) {
	tests := []struct {
		name           string
		title          string
		description    string
		price          int
		collectedAttrs map[string]string
		want           tori.ItemFields
	}{
		{
			name:        "basic selling item",
			title:       "Test Item",
			description: "A great item for sale",
			price:       100,
			collectedAttrs: map[string]string{
				"condition":        "2",
				"computeracc_type": "8",
			},
			want: tori.ItemFields{
				Title:       "Test Item",
				Description: "A great item for sale",
				Price:       100,
				Condition:   2,
				Attributes:  map[string]any{"computeracc_type": 8},
			},
		},
		{
			name:        "giveaway (price=0)",
			title:       "Free Item",
			description: "Giving away",
			price:       0,
			collectedAttrs: map[string]string{
				"condition": "3",
			},
			want: tori.ItemFields{
				Title:       "Free Item",
				Description: "Giving away",
				Price:       0,
				Condition:   3,
				Attributes:  map[string]any{},
			},
		},
		{
			name:           "no attributes",
			title:          "Simple Item",
			description:    "Just a thing",
			price:          50,
			collectedAttrs: nil,
			want: tori.ItemFields{
				Title:       "Simple Item",
				Description: "Just a thing",
				Price:       50,
				Condition:   0,
				Attributes:  map[string]any{},
			},
		},
		{
			name:        "non-numeric attribute value",
			title:       "Text Attr Item",
			description: "Has text attribute",
			price:       25,
			collectedAttrs: map[string]string{
				"condition":   "1",
				"custom_text": "some text value",
			},
			want: tori.ItemFields{
				Title:       "Text Attr Item",
				Description: "Has text attribute",
				Price:       25,
				Condition:   1,
				Attributes:  map[string]any{"custom_text": "some text value"},
			},
		},
		{
			name:        "multiple attributes",
			title:       "Multi Attr",
			description: "Multiple attributes",
			price:       200,
			collectedAttrs: map[string]string{
				"condition": "2",
				"brand":     "123",
				"size":      "456",
				"color":     "789",
			},
			want: tori.ItemFields{
				Title:       "Multi Attr",
				Description: "Multiple attributes",
				Price:       200,
				Condition:   2,
				Attributes: map[string]any{
					"brand": 123,
					"size":  456,
					"color": 789,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildItemFields(tt.title, tt.description, tt.price, tt.collectedAttrs)
			assert.Equal(t, tt.want.Title, got.Title)
			assert.Equal(t, tt.want.Description, got.Description)
			assert.Equal(t, tt.want.Price, got.Price)
			assert.Equal(t, tt.want.Condition, got.Condition)
			assert.Equal(t, tt.want.Attributes, got.Attributes)
		})
	}
}

func TestBuildItemFieldsFromValues(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   tori.ItemFields
	}{
		{
			name: "basic ad values",
			values: map[string]any{
				"title":       "Republished Item",
				"description": "Item description",
				"condition":   "2",
				"price": []any{
					map[string]any{"price_amount": "150"},
				},
				"brand": "42",
			},
			want: tori.ItemFields{
				Title:       "Republished Item",
				Description: "Item description",
				Price:       150,
				Condition:   2,
				Attributes:  map[string]any{"brand": 42},
			},
		},
		{
			name: "price as float64 (JSON number)",
			values: map[string]any{
				"title":       "Float Price",
				"description": "Has float price",
				"price": []any{
					map[string]any{"price_amount": "99"},
				},
				"some_attr": float64(123),
			},
			want: tori.ItemFields{
				Title:       "Float Price",
				Description: "Has float price",
				Price:       99,
				Condition:   0,
				Attributes:  map[string]any{"some_attr": 123},
			},
		},
		{
			name: "skip known fields",
			values: map[string]any{
				"title":       "Skip Test",
				"description": "Should skip known",
				"category":    "78",
				"trade_type":  "1",
				"location":    []any{map[string]any{"postal_code": "00100"}},
				"image":       []any{},
				"multi_image": []any{},
				"custom_attr": "999",
			},
			want: tori.ItemFields{
				Title:       "Skip Test",
				Description: "Should skip known",
				Price:       0,
				Condition:   0,
				Attributes:  map[string]any{"custom_attr": 999},
			},
		},
		{
			name: "empty price array",
			values: map[string]any{
				"title":       "No Price",
				"description": "Empty price",
				"price":       []any{},
			},
			want: tori.ItemFields{
				Title:       "No Price",
				Description: "Empty price",
				Price:       0,
				Condition:   0,
				Attributes:  map[string]any{},
			},
		},
		{
			name: "giveaway with zero price",
			values: map[string]any{
				"title":       "Free Stuff",
				"description": "Giveaway item",
				"condition":   "3",
				"price": []any{
					map[string]any{"price_amount": "0"},
				},
			},
			want: tori.ItemFields{
				Title:       "Free Stuff",
				Description: "Giveaway item",
				Price:       0,
				Condition:   3,
				Attributes:  map[string]any{},
			},
		},
		{
			name:   "empty values",
			values: map[string]any{},
			want: tori.ItemFields{
				Title:       "",
				Description: "",
				Price:       0,
				Condition:   0,
				Attributes:  map[string]any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildItemFieldsFromValues(tt.values)
			assert.Equal(t, tt.want.Title, got.Title)
			assert.Equal(t, tt.want.Description, got.Description)
			assert.Equal(t, tt.want.Price, got.Price)
			assert.Equal(t, tt.want.Condition, got.Condition)
			assert.Equal(t, tt.want.Attributes, got.Attributes)
		})
	}
}

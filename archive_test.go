package main

import (
	"encoding/json"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/stretchr/testify/assert"
)

func TestListingArchiveMarshalJSON(t *testing.T) {
	archive := NewListingArchive(
		tori.Listing{
			Subject: "Test",
			Body:    "Myydään testi xD",
			Price:   10,
			Type:    tori.ListingTypeSell,
			AdDetails: tori.AdDetails{
				"general_condition": "new",
			},
			Category: "2046",
			Location: &tori.ListingLocation{Region: "18", Zipcode: "00420", Area: "313"},
			Images: &[]tori.ListingMedia{
				{Id: "/public/media/ad/100120413803"},
			},
			PhoneHidden: true,
			AccountId:   "396024",
		},
		[]tgbotapi.PhotoSize{
			{
				FileID:       "AgACAgQAAxkBAAIIhWKkXDtoRD7O987e06cfjLzrgUYzAAIRuzEb6BkYUYQoJuMmIykMAQADAgADeQADJAQ",
				FileUniqueID: "AQADEbsxG-gZGFF-",
				Width:        1280,
				Height:       960,
				FileSize:     291525,
			},
		},
	)

	bytes, _ := json.MarshalIndent(archive, "", "  ")
	want := `
{
  "listing": {
    "subject": "Test",
    "body": "Myydään testi xD",
    "price": {
      "currency": "€",
      "value": 10
    },
    "type": "s",
    "ad_details": {
      "general_condition": {
        "single": {
          "code": "new"
        }
      }
    },
    "category": "2046",
    "phone_hidden": true,
    "account_id": ""
  },
  "photos": [
    {
      "file_id": "AgACAgQAAxkBAAIIhWKkXDtoRD7O987e06cfjLzrgUYzAAIRuzEb6BkYUYQoJuMmIykMAQADAgADeQADJAQ",
      "file_unique_id": "AQADEbsxG-gZGFF-",
      "width": 1280,
      "height": 960,
      "file_size": 291525
    }
  ]
}`

	assert.Equal(t, strings.TrimSpace(want), string(bytes))
}

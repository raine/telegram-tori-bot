package tori

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListingMarshalJSON(t *testing.T) {
	tests := map[string]struct {
		listing Listing
		want    string
	}{
		"basic": {
			listing: Listing{
				Subject: "test",
				Body:    "foo",
				Price:   100,
				Type:    ListingTypeSell,
				AdDetails: map[string]any{
					"clothing_kind": "16",
					"clothing_size": "1",
					"test":          []string{"1", "2", "3"},
				},
			},
			want: `
{
  "subject": "test",
  "body": "foo",
  "price": {
    "currency": "€",
    "value": 100
  },
  "type": "s",
  "ad_details": {
    "clothing_kind": {
      "single": {
        "code": "16"
      }
    },
    "clothing_size": {
      "single": {
        "code": "1"
      }
    },
    "test": {
      "multiple": [
        {
          "code": "1"
        },
        {
          "code": "2"
        },
        {
          "code": "3"
        }
      ]
    }
  },
  "category": "",
  "phone_hidden": false,
  "account_id": ""
}`,
		},
		"empty multi value in AdDetails is not marshaled": {
			listing: Listing{
				Type: ListingTypeSell,
				AdDetails: map[string]any{
					"delivery_options": []string{},
				},
			},
			want: `
{
  "subject": "",
  "body": "",
  "price": {
    "currency": "€",
    "value": 0
  },
  "type": "s",
  "ad_details": {},
  "category": "",
  "phone_hidden": false,
  "account_id": ""
}`,
		},
		"listing location": {
			listing: Listing{
				Type: ListingTypeSell,
				Location: &ListingLocation{
					Region:  "18",
					Zipcode: "00420",
					Area:    "313",
				},
			},
			want: `
{
  "subject": "",
  "body": "",
  "price": {
    "currency": "€",
    "value": 0
  },
  "type": "s",
  "ad_details": {},
  "category": "",
  "location": {
    "region": "18",
    "zipcode": "00420",
    "area": "313"
  },
  "phone_hidden": false,
  "account_id": ""
}`,
		},
		"images": {
			listing: Listing{
				Type: ListingTypeSell,
				Images: &[]ListingMedia{
					{Id: "1"},
					{Id: "2"},
					{Id: "3"},
				},
			},
			want: `
{
  "subject": "",
  "body": "",
  "price": {
    "currency": "€",
    "value": 0
  },
  "type": "s",
  "ad_details": {},
  "category": "",
  "images": [
    {
      "media_id": "1"
    },
    {
      "media_id": "2"
    },
    {
      "media_id": "3"
    }
  ],
  "phone_hidden": false,
  "account_id": ""
}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			bytes, err := json.MarshalIndent(tc.listing, "", "  ")
			if err != nil {
				t.Error(err)
			}
			assert.Equal(t, strings.TrimSpace(tc.want), string(bytes))
		})
	}
}

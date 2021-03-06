package main

import (
	"os"
	"testing"

	"github.com/raine/telegram-tori-bot/tori"
	"github.com/stretchr/testify/assert"
)

func TestSetListingFieldFromMessage(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		listing tori.Listing
		field   string
		message string
		want    tori.Listing
	}{
		"body": {
			listing: tori.Listing{},
			field:   "body",
			message: "Hehheh",
			want: tori.Listing{
				Body: "Hehheh",
			},
		},
		"price": {
			listing: tori.Listing{},
			field:   "price",
			message: "50€\n",
			want: tori.Listing{
				Price: 50,
			},
		},
		"existing price, general_condition": {
			listing: tori.Listing{
				Price: 50,
			},
			field:   "general_condition",
			message: "Hyvä",
			want: tori.Listing{
				Price: 50,
				AdDetails: tori.AdDetails{
					"general_condition": "good",
				},
			},
		},
		"delivery_options/yes": {
			listing: tori.Listing{},
			field:   "delivery_options",
			message: "Kyllä",
			want: tori.Listing{
				AdDetails: tori.AdDetails{
					"delivery_options": []string{"delivery_send"},
				},
			},
		},
		"delivery_options/no": {
			listing: tori.Listing{},
			field:   "delivery_options",
			message: "En",
			want: tori.Listing{
				AdDetails: tori.AdDetails{
					"delivery_options": []string{},
				},
			},
		},
		"clothing_sex_0": {
			listing: tori.Listing{},
			field:   "clothing_sex_0",
			message: "Naisten",
			want: tori.Listing{
				AdDetails: tori.AdDetails{
					"clothing_sex": "1",
				},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := setListingFieldFromMessage(newadFilters.Newad.ParamMap, tc.listing, tc.field, tc.message)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindValueForLabel(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		field   string
		message string
		want    string
	}{
		"general_condition": {
			message: "Uusi",
			want:    "new",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			param := newadFilters.Newad.ParamMap[name]
			got, err := findParamValueForLabel(param, tc.message)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewListingFromMessage(t *testing.T) {
	tests := map[string]struct {
		message string
		want    tori.Listing
	}{
		"defaults to sell listing type": {
			message: "Horipad Logitech Switch peliohjain",
			want: tori.Listing{
				Subject: "Horipad Logitech Switch peliohjain",
				Type:    tori.ListingTypeSell,
			},
		},
		"you can prefix with 'myydään' but it's redundant": {
			message: "Myydään Horipad Logitech Switch peliohjain",
			want: tori.Listing{
				Subject: "Horipad Logitech Switch peliohjain",
				Type:    tori.ListingTypeSell,
			},
		},
		"annetaan listing type": {
			message: "Annetaan Horipad Logitech Switch peliohjain",
			want: tori.Listing{
				Subject: "Horipad Logitech Switch peliohjain",
				Type:    tori.ListingTypeGive,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := newListingFromMessage(tc.message)
			assert.Equal(t, tc.want, got)
		})
	}
}

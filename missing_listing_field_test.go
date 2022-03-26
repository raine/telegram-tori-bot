package main

import (
	"os"
	"testing"

	"github.com/raine/telegram-tori-bot/tori"
	"github.com/stretchr/testify/assert"
)

func TestGetMissingUnsetListingFieldWithSettingsParamBasic(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}
	paramMap := newadFilters.Newad.ParamMap

	json := []byte(`
{
  "keys": ["category", "type"],
  "settings": [
    {
      "settings_result": ["type_skg","general_condition", "zipcode", "price"],
      "values": ["3029", "s"]
    },
    {
      "settings_result": ["type_skg","general_condition", "zipcode", "price"],
      "values": ["3030", "s"]
    }
  ]
}`)

	settingsParam, err := tori.ParseOneSettingsParam(json)
	if err != nil {
		panic(err)
	}
	tests := map[string]struct {
		listing tori.Listing
		want    string
	}{
		"price": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
			},
			want: "general_condition",
		},
		"general condition": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
				Price:    10,
			},
			want: "general_condition",
		},
		"no missing fields": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
				},
				Price: 10,
			},
			want: "",
		},
		"category does not match": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3031",
			},
			want: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := getMissingListingFieldWithSettingsParam(paramMap, settingsParam, tc.listing)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetMissingUnsetListingFieldWithSettingsParamPeripheral(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}
	paramMap := newadFilters.Newad.ParamMap

	tests := map[string]struct {
		listing tori.Listing
		json    []byte
		want    string
	}{
		"asterisk matches any value": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
				AdDetails: tori.AdDetails{
					"peripheral": "printer",
				},
			},
			json: []byte(`
{
  "keys": ["type", "?peripheral", "peripheral"],
  "settings": [
    {
      "settings_result": ["general_condition", "peripheral"],
      "values": ["s", "true", "*"]
    }
  ]
}`),
			want: "general_condition",
		},
		"returns inchlist_3": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
				AdDetails: tori.AdDetails{
					"peripheral":        "monitor",
					"general_condition": "fair",
				},
			},
			json: []byte(`
{
  "keys": ["type", "?peripheral", "peripheral"],
  "settings": [
    {
      "settings_result": ["general_condition", "peripheral", "inchlist_3"],
      "values": ["s", "true", "monitor"]
    }
  ]
}`),
			want: "inchlist_3",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			settingsParam, err := tori.ParseOneSettingsParam(tc.json)
			if err != nil {
				panic(err)
			}
			got := getMissingListingFieldWithSettingsParam(paramMap, settingsParam, tc.listing)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetMissingUnsetListingFieldWithSettingsParamClothing(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}
	paramMap := newadFilters.Newad.ParamMap

	tests := map[string]struct {
		listing tori.Listing
		json    []byte
		want    string
	}{
		"clothing_sex_0 matches clothing_sex in ad details": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
				AdDetails: tori.AdDetails{
					"clothing_sex": "2",
				},
			},
			json: []byte(`
{
  "keys": ["type", "?clothing_sex", "clothing_sex"],
  "settings": [
    {
      "settings_result": ["clothing_sex_0", "clothing_kind_2"],
      "values": ["s", "true", "2"]
    }
  ]
}
`),
			want: "clothing_kind_2",
		},
		"clothing sex and kind": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3030",
				AdDetails: tori.AdDetails{
					"clothing_sex":  "2",
					"clothing_kind": "9",
				},
			},
			json: []byte(`
{
  "keys": ["type", "?clothing_sex", "?clothing_kind", "clothing_sex", "clothing_kind"],
  "settings": [
    {
      "settings_result": ["clothing_sex_0", "clothing_kind_2", "clothing_size_8"],
      "values": ["s", "true", "true", "2", "9"]
    }
  ]
}
`),
			want: "clothing_size_8",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			settingsParam, err := tori.ParseOneSettingsParam(tc.json)
			if err != nil {
				panic(err)
			}
			got := getMissingListingFieldWithSettingsParam(paramMap, settingsParam, tc.listing)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetMissingListingField(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}
	paramMap := newadFilters.Newad.ParamMap

	json := []byte(`
[
  {
    "keys": ["type", "?clothing_sex", "?clothing_kind", "clothing_sex", "clothing_kind"],
    "settings": [
      {
        "settings_result": ["type_skg", "general_condition", "zipcode", "clothing_sex_0", "clothing_kind_1", "clothing_size_2", "delivery_options"],
        "values": ["s", "true", "true", "1", "1"]
      }
    ]
  },
  {
    "keys": ["type", "?clothing_sex", "clothing_sex"],
    "settings": [
      {
        "settings_result": ["type_skg", "general_condition", "zipcode", "price", "clothing_sex_0", "clothing_kind_1", "delivery_options"],
        "values": ["s", "true", "1"]
      }
    ]
  },
  {
    "keys": ["category", "type"],
    "settings": [
      {
        "settings_result": ["type_skuhg", "general_condition", "zipcode", "price", "clothing_sex_0", "delivery_options"],
        "values": ["3050", "s"]
      }
    ]
  }
]
`)

	settingsParam, err := tori.ParseMultipleSettingsParams(json)
	if err != nil {
		panic(err)
	}
	tests := map[string]struct {
		listing tori.Listing
		want    string
	}{
		"body": {
			listing: tori.Listing{
				Type:     tori.ListingTypeSell,
				Category: "3050",
			},
			want: "body",
		},
		"general_condition": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
			},
			want: "general_condition",
		},
		"price": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
				},
			},
			want: "price",
		},
		"clothing_sex": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
				Price:    1,
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
				},
			},
			want: "clothing_sex_0",
		},
		"clothing_kind matches after clothing_sex is set": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
				Price:    1,
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
					"clothing_sex":      "1",
				},
			},
			want: "clothing_kind_1",
		},
		"clothing_size matches after clothing_kind is set": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
				Price:    1,
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
					"clothing_sex":      "1",
					"clothing_kind":     "1",
				},
			},
			want: "clothing_size_2",
		},
		"delivery_options": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
				Price:    1,
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
					"clothing_sex":      "1",
					"clothing_kind":     "1",
					"clothing_size":     "44",
				},
			},
			want: "delivery_options",
		},
		"no fields after delivery_options": {
			listing: tori.Listing{
				Body:     "asdf",
				Type:     tori.ListingTypeSell,
				Category: "3050",
				Price:    1,
				AdDetails: tori.AdDetails{
					"general_condition": "fair",
					"clothing_sex":      "1",
					"clothing_kind":     "1",
					"clothing_size":     "44",
					"delivery_options":  []string{"delivery_send"},
				},
			},
			want: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := getMissingListingField(paramMap, settingsParam, tc.listing)
			assert.Equal(t, tc.want, got)
		})
	}
}

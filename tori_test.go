package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raine/go-telegram-bot/tori"
	"github.com/stretchr/testify/assert"
)

func makeListingResponse(t *testing.T, id string, category tori.Category) []byte {
	listingResponse := tori.GetListingResponse{
		Ad: tori.Ad{
			ListIdCode: id,
			Category:   category,
		},
	}

	bytes, err := json.Marshal(listingResponse)
	if err != nil {
		t.Fatal(err)
	}

	return bytes
}

func makeListingsSearchResponse(t *testing.T) []byte {
	listingsSearchResponse := tori.SearchListingsResponse{
		ListAds: []tori.ListAdItem{
			{
				ListAd:      tori.ListAd{ListIdCode: "1"},
				SptMetadata: tori.SptMetadata{Category: "Electronics > Phones and accessories > Phones"},
			},
			{
				ListAd:      tori.ListAd{ListIdCode: "2"},
				SptMetadata: tori.SptMetadata{Category: "Electronics > Tv audio video cameras > Television"},
			},
			{
				ListAd:      tori.ListAd{ListIdCode: "3"},
				SptMetadata: tori.SptMetadata{Category: "Electronics > Phones and accessories > Tablets"},
			},
			{
				ListAd:      tori.ListAd{ListIdCode: "4"},
				SptMetadata: tori.SptMetadata{Category: "Electronics > Phones and accessories > Tablets"},
			},
		},
	}

	bytes, err := json.Marshal(listingsSearchResponse)
	if err != nil {
		t.Fatal(err)
	}

	return bytes
}

func TestGetDistinctCategoriesFromSearchQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/listings/search":
			w.Write(makeListingsSearchResponse(t))
		case "/v2/listings/1":
			w.Write(makeListingResponse(t, "1", tori.Category{Code: "5012", Label: "Puhelimet"}))
		case "/v2/listings/2":
			w.Write(makeListingResponse(t, "2", tori.Category{Code: "5022", Label: "Televisiot"}))
		case "/v2/listings/4":
			w.Write(makeListingResponse(t, "4", tori.Category{Code: "5031", Label: "Tabletit"}))
		default:
			t.Fatal("invalid path " + r.URL.Path)
		}
	}))

	defer ts.Close()

	client := tori.NewClient(tori.ClientOpts{
		BaseURL: ts.URL,
		Auth:    "foo",
	})

	categories, err := getDistinctCategoriesFromSearchQuery(client, "test")
	if err != nil {
		t.Fatal(err)
	}
	assert.ElementsMatch(t, []tori.Category{
		{Code: "5012", Label: "Puhelimet"},
		{Code: "5022", Label: "Televisiot"},
		{Code: "5031", Label: "Tabletit"},
	}, categories)
}

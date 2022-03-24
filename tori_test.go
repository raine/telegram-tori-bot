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

func makeListingsSearchResponse(t *testing.T, listAds []tori.ListAdItem) []byte {
	listingsSearchResponse := tori.SearchListingsResponse{
		ListAds: listAds,
	}

	bytes, err := json.Marshal(listingsSearchResponse)
	if err != nil {
		t.Fatal(err)
	}

	return bytes
}

func makeListAdItem(listIdCode string, sptMetadataCategory string) tori.ListAdItem {
	return tori.ListAdItem{
		ListAd:      tori.ListAd{ListIdCode: listIdCode},
		SptMetadata: tori.SptMetadata{Category: sptMetadataCategory},
	}
}

// Spt category strings are not really like in the test responses. Using
// category labels for simplicity.
func TestGetCategoriesForSubject(t *testing.T) {
	t.Run("generic test", func(t *testing.T) {
		calledUrls := make([]string, 0)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			url := r.URL.RequestURI()
			calledUrls = append(calledUrls, url)
			w.Header().Set("Content-Type", "application/json")
			switch url {
			case "/v2/listings/search?q=nintendo+switch+horipad+peliohjain":
				// Full subject does not return any results
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{}))
			case "/v2/listings/search?q=switch+horipad+peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("1", "Pelikonsolit ja pelaaminen"),
				}))
			case "/v2/listings/search?q=horipad+peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("2", "Muu viihde-elektroniikka"),
					makeListAdItem("3", "Oheislaitteet"),
				}))
			case "/v2/listings/search?q=peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("4", "Tabletit"),
				}))
			case "/v2/listings/1":
				w.Write(makeListingResponse(t, "1", tori.Category{Code: "5027", Label: "Pelikonsolit ja pelaaminen"}))
			case "/v2/listings/2":
				w.Write(makeListingResponse(t, "2", tori.Category{Code: "5029", Label: "Muu viihde-elektroniikka"}))
			case "/v2/listings/3":
				w.Write(makeListingResponse(t, "3", tori.Category{Code: "5036", Label: "Oheislaitteet"}))
			case "/v2/listings/4":
				w.Write(makeListingResponse(t, "4", tori.Category{Code: "5031", Label: "Tabletit"}))
			default:
				t.Fatal("invalid url " + url)
			}
		}))

		defer ts.Close()

		client := tori.NewClient(tori.ClientOpts{
			BaseURL: ts.URL,
			Auth:    "foo",
		})

		categories, err := getCategoriesForSubject(client, "nintendo switch horipad peliohjain")
		if err != nil {
			t.Fatal(err)
		}

		assert.Equal(t, []tori.Category{
			{Code: "5027", Label: "Pelikonsolit ja pelaaminen"},
			{Code: "5029", Label: "Muu viihde-elektroniikka"},
			{Code: "5036", Label: "Oheislaitteet"},
			{Code: "5031", Label: "Tabletit"},
		}, categories)

		assert.ElementsMatch(t, []string{
			"/v2/listings/search?q=nintendo+switch+horipad+peliohjain",
			"/v2/listings/search?q=switch+horipad+peliohjain",
			"/v2/listings/search?q=horipad+peliohjain",
			"/v2/listings/search?q=peliohjain",
			"/v2/listings/1",
			"/v2/listings/2",
			"/v2/listings/3",
			"/v2/listings/4",
		}, calledUrls)
	})

	t.Run("no additional queries are made if the initial query provides enough results", func(t *testing.T) {
		calledUrls := make([]string, 0)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			url := r.URL.RequestURI()
			calledUrls = append(calledUrls, url)
			w.Header().Set("Content-Type", "application/json")
			switch url {
			case "/v2/listings/search?q=nintendo+switch+horipad+peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("1", "Pelikonsolit ja pelaaminen"),
					makeListAdItem("2", "Muu viihde-elektroniikka"),
					makeListAdItem("3", "Oheislaitteet"),
					makeListAdItem("4", "Televisiot"),
					makeListAdItem("5", "Tabletit"),
				}))
			case "/v2/listings/1":
				w.Write(makeListingResponse(t, "1", tori.Category{Code: "5027", Label: "Pelikonsolit ja pelaaminen"}))
			case "/v2/listings/2":
				w.Write(makeListingResponse(t, "2", tori.Category{Code: "5029", Label: "Muu viihde-elektroniikka"}))
			case "/v2/listings/3":
				w.Write(makeListingResponse(t, "3", tori.Category{Code: "5036", Label: "Oheislaitteet"}))
			case "/v2/listings/4":
				w.Write(makeListingResponse(t, "4", tori.Category{Code: "5022", Label: "Televisiot"}))
			case "/v2/listings/5":
				w.Write(makeListingResponse(t, "5", tori.Category{Code: "5031", Label: "Tabletit"}))
			default:
				t.Fatal("invalid url " + url)
			}
		}))

		defer ts.Close()

		client := tori.NewClient(tori.ClientOpts{
			BaseURL: ts.URL,
			Auth:    "foo",
		})

		categories, err := getCategoriesForSubject(client, "nintendo switch horipad peliohjain")
		if err != nil {
			t.Fatal(err)
		}

		assert.Equal(t, []tori.Category{
			{Code: "5027", Label: "Pelikonsolit ja pelaaminen"},
			{Code: "5029", Label: "Muu viihde-elektroniikka"},
			{Code: "5036", Label: "Oheislaitteet"},
			{Code: "5022", Label: "Televisiot"},
			{Code: "5031", Label: "Tabletit"},
		}, categories)

		assert.ElementsMatch(t, []string{
			"/v2/listings/search?q=nintendo+switch+horipad+peliohjain",
			"/v2/listings/1",
			"/v2/listings/2",
			"/v2/listings/3",
			"/v2/listings/4",
			"/v2/listings/5",
		}, calledUrls)
	})

	t.Run("no duplicate categories", func(t *testing.T) {
		calledUrls := make([]string, 0)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			url := r.URL.RequestURI()
			calledUrls = append(calledUrls, url)
			w.Header().Set("Content-Type", "application/json")
			switch url {
			case "/v2/listings/search?q=nintendo+switch+horipad+peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("1", "Pelikonsolit ja pelaaminen"),
					makeListAdItem("2", "Muu viihde-elektroniikka"),
					makeListAdItem("3", "Oheislaitteet"),
				}))
			case "/v2/listings/search?q=switch+horipad+peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("1", "Pelikonsolit ja pelaaminen"),
					makeListAdItem("2", "Muu viihde-elektroniikka"),
					makeListAdItem("3", "Oheislaitteet"),
				}))
			case "/v2/listings/search?q=horipad+peliohjain":
				w.Write(makeListingsSearchResponse(t, []tori.ListAdItem{
					makeListAdItem("4", "Televisiot"),
					makeListAdItem("5", "Tabletit"),
				}))
			case "/v2/listings/1":
				w.Write(makeListingResponse(t, "1", tori.Category{Code: "5027", Label: "Pelikonsolit ja pelaaminen"}))
			case "/v2/listings/2":
				w.Write(makeListingResponse(t, "2", tori.Category{Code: "5029", Label: "Muu viihde-elektroniikka"}))
			case "/v2/listings/3":
				w.Write(makeListingResponse(t, "3", tori.Category{Code: "5036", Label: "Oheislaitteet"}))
			case "/v2/listings/4":
				w.Write(makeListingResponse(t, "4", tori.Category{Code: "5022", Label: "Televisiot"}))
			case "/v2/listings/5":
				w.Write(makeListingResponse(t, "5", tori.Category{Code: "5031", Label: "Tabletit"}))
			default:
				t.Fatal("invalid url " + url)
			}
		}))

		defer ts.Close()

		client := tori.NewClient(tori.ClientOpts{
			BaseURL: ts.URL,
			Auth:    "foo",
		})

		categories, err := getCategoriesForSubject(client, "nintendo switch horipad peliohjain")
		if err != nil {
			t.Fatal(err)
		}

		assert.Equal(t, []tori.Category{
			{Code: "5027", Label: "Pelikonsolit ja pelaaminen"},
			{Code: "5029", Label: "Muu viihde-elektroniikka"},
			{Code: "5036", Label: "Oheislaitteet"},
			{Code: "5022", Label: "Televisiot"},
			{Code: "5031", Label: "Tabletit"},
		}, categories)

		assert.ElementsMatch(t, []string{
			"/v2/listings/search?q=nintendo+switch+horipad+peliohjain",
			"/v2/listings/search?q=switch+horipad+peliohjain",
			"/v2/listings/search?q=horipad+peliohjain",
			"/v2/listings/1",
			"/v2/listings/2",
			"/v2/listings/3",
			"/v2/listings/4",
			"/v2/listings/5",
		}, calledUrls)
	})
}

package tori

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSearchListings(t *testing.T) {
	b, err := os.ReadFile("testdata/v2_listings_search.json")
	if err != nil {
		t.Fatal(err)
	}
	var req *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req = r
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer ts.Close()

	client := NewClient(ClientOpts{
		BaseURL: ts.URL,
		Auth:    "foo",
	})
	listings, err := client.SearchListings(context.Background(), "test")
	assert.Equal(t, "/v2/listings/search", req.URL.Path)
	assert.Nil(t, err)
	assert.Len(t, listings, 40)
	assert.Equal(t, ListAd{
		ListIdCode: "95194022",
	}, listings[0].ListAd)
}

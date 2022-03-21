package tori

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetAccount(t *testing.T) {
	var req *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req = r
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"account":{"account_id":"/private/accounts/123123 ","address":"","birthday":"01.01.1970","can_publish":true,"email":"raine@example.com","gender":"m","locations":[{"code":"18","key":"region","label":"Uusimaa","locations":[{"code":"313","key":"area","label":"Helsinki","locations":[{"code":"00320","key":"zipcode","label":"EtelÃ¤-Haaga"}]}]}],"name":"Raine Virta","newsletter":"f","phone":"+358405551212","phone_hidden":true,"professional":false,"receive_email":false,"receive_watchmail":false,"uuid":"409ae75f-3b17-4cfe-84cd-f6200986cd29","zipcode":"00320"}}`))
	}))
	defer ts.Close()

	client := NewClient(ClientOpts{
		BaseURL: ts.URL,
		Auth:    "foo",
	})
	acc, err := client.GetAccount("123123")
	assert.Nil(t, err)
	assert.Equal(t, acc, Account{
		AccountId: "/private/accounts/123123 ",
		Locations: []Location{
			{
				Code:  "18",
				Key:   "region",
				Label: "Uusimaa",
				Locations: []Location{
					{
						Code:  "313",
						Key:   "area",
						Label: "Helsinki",
						Locations: []Location{
							{
								Code:  "00320",
								Key:   "zipcode",
								Label: "EtelÃ¤-Haaga",
							},
						},
					},
				},
			},
		},
	})
	assert.Equal(t, "/v1.2/private/accounts/123123", req.URL.Path)
	assert.Equal(t, "foo", req.Header.Get("Authorization"))
}

func TestUploadMedia(t *testing.T) {
	var req *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req = r
		body, _ := io.ReadAll(req.Body)
		assert.Equal(t, 4, len(body))
		w.Header().Set("Content-Type", "plain/text") // Yes, really
		io.WriteString(w, `{"image":{"url":"https://images.tori.fi/api/v1/imagestori/images/100094050777.jpg?rule=images","id":"100094050777"}}`)
	}))
	defer ts.Close()

	data := []byte("asdf")
	client := NewClient(ClientOpts{
		BaseURL: ts.URL,
		Auth:    "foo",
	})
	media, err := client.UploadMedia(data)
	assert.Nil(t, err)
	assert.Equal(t, Media{
		Url: "https://images.tori.fi/api/v1/imagestori/images/100094050777.jpg?rule=images",
		Id:  "100094050777",
	}, media)

	assert.Equal(t, "/v2.2/media", req.URL.Path)
	assert.Equal(t, "foo", req.Header.Get("Authorization"))
	assert.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
}

func TestGetListing(t *testing.T) {
	b, err := ioutil.ReadFile("testdata/v2_listings_95194022.json")
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
	listing, err := client.GetListing("95194022")
	assert.Equal(t, "/v2/listings/95194022", req.URL.Path)
	assert.Nil(t, err)
	assert.Equal(t, Ad{
		ListIdCode: "95194022",
		Category: Category{
			Code:  "5012",
			Label: "Puhelimet",
		},
	}, listing)
}

func TestGetCategories(t *testing.T) {
	b, err := ioutil.ReadFile("testdata/v1_2_public_categories_insert.json")
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
	categories, err := client.GetCategories()
	assert.Equal(t, "/v1.2/public/categories/insert", req.URL.Path)
	assert.Nil(t, err)
	assert.Len(t, categories.Categories, 7)
	assert.Equal(t, "2000", categories.Categories[0].Code)
}

func TestGetFiltersSectionNewad(t *testing.T) {
	b, err := ioutil.ReadFile("testdata/v1_2_public_filters_section_newad.json")
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
	_, err = client.GetFiltersSectionNewad()
	assert.Equal(t, "/v1.2/public/filters", req.URL.Path)
	assert.Equal(t, "section=newad", req.URL.RawQuery)
	assert.Nil(t, err)
}

func TestPostListing(t *testing.T) {
	var handlerCalled bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		want := `{
  "subject": "iPhone 12",
  "body": "Myydään käytetty iPhone 12",
  "price": {
    "currency": "€",
    "value": 50
  },
  "type": "s",
  "ad_details": {
    "cell_phone": {
      "single": {
        "code": "apple"
      }
    },
    "general_condition": {
      "single": {
        "code": "new"
      }
    }
  },
  "category": "5012",
  "location": {
    "region": "18",
    "zipcode": "00420",
    "area": "313"
  },
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
  "phone_hidden": true,
  "account_id": ""
}`
		assert.Equal(t, want, formatJson(body))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))

	defer ts.Close()
	client := NewClient(ClientOpts{
		BaseURL: ts.URL,
		Auth:    "foo",
	})

	listing := Listing{
		Subject:  "iPhone 12",
		Body:     "Myydään käytetty iPhone 12",
		Category: "5012",
		Type:     ListingTypeSell,
		Price:    50,
		AdDetails: AdDetails{
			"general_condition": "new",
			"cell_phone":        "apple",
			"delivery_options":  []string{},
		},
		Location: &ListingLocation{
			Region:  "18",
			Zipcode: "00420",
			Area:    "313",
		},
		Images: &[]ListingMedia{
			{Id: "1"},
			{Id: "2"},
			{Id: "3"},
		},
		PhoneHidden: true,
	}

	err := client.PostListing(listing)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, true, handlerCalled)
}

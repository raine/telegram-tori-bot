package tori

import (
	"context"
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
	acc, err := client.GetAccount(context.Background(), "123123")
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

package tori

import (
	"encoding/json"
	"fmt"

	"github.com/go-resty/resty/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	ApiBaseUrl = "https://api.tori.fi/api"
)

type AccountResponse struct {
	Account `json:"account"`
}

type Location struct {
	Code      string     `json:"code"`
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Locations []Location `json:"locations"`
}

type Account struct {
	AccountId string     `json:"account_id"`
	Locations []Location `json:"locations"`
}

type ClientOpts struct {
	BaseURL string
	Auth    string
}

type Client struct {
	httpClient *resty.Client
	baseURL    string
	auth       string
}

func NewClient(opts ClientOpts) *Client {
	c := Client{baseURL: ApiBaseUrl}
	if opts.BaseURL != "" {
		c.baseURL = opts.BaseURL
	}
	if opts.Auth != "" {
		c.auth = opts.Auth
	}
	c.httpClient = resty.New().
		SetDebug(false).
		SetBaseURL(c.baseURL).
		SetHeaders(
			map[string]string{
				"Accept":     "*/*",
				"User-Agent": "Tori/190 CFNetwork/1329 Darwin/21.3.0",
			},
		)

	return &c
}

func (c *Client) req(result any) *resty.Request {
	request := c.httpClient.
		SetBaseURL(c.baseURL).
		NewRequest().
		SetHeader("Authorization", c.auth)

	if result != nil {
		request.SetResult(result)
	}

	return request
}

func (c *Client) GetAccount(accountId string) (Account, error) {
	result := &AccountResponse{}

	_, err := handleError(c.req(result).
		SetPathParams(map[string]string{
			"accountId": accountId,
		}).
		Get("/v1.2/private/accounts/{accountId}"))

	return result.Account, err
}

type UploadMediaResponse struct {
	Media `json:"image"`
}

type Media struct {
	Id  string `json:"id"`
	Url string `json:"url"`
}

func (c *Client) UploadMedia(data []byte) (Media, error) {
	media := Media{}

	res, err := handleError(
		c.req(nil).SetHeaders(
			map[string]string{
				"Content-Type":   "application/x-www-form-urlencoded",
				"User-Agent":     "Tori/12.1.16 (com.tori.tori; build:190; iOS 15.3.1) Alamofire/5.4.4",
				"Content-Length": fmt.Sprintf("%v", len(data)),
			},
		).
			SetBody(data).
			Post("/v2.2/media"))
	if err != nil {
		return media, err
	}

	// Tori API returns the wrong content-type (text/plain) so we can't use
	// resty's SetResult to unmarshal JSON automatically.
	var uploadImageResponse UploadMediaResponse
	err = json.Unmarshal(res.Body(), &uploadImageResponse)
	if err != nil {
		return media, err
	}
	return uploadImageResponse.Media, err
}

type Ad struct {
	ListIdCode string   `json:"list_id_code"`
	Category   Category `json:"category"`
}

type GetListingResponse struct {
	Ad Ad `json:"ad"`
}

func (c *Client) GetListing(id string) (Ad, error) {
	result := &GetListingResponse{}
	_, err := handleError(
		c.req(result).
			SetPathParam("id", id).
			Get("/v2/listings/{id}"))

	return result.Ad, err
}

func (c *Client) GetCategories() (Categories, error) {
	result := &Categories{}
	_, err := handleError(
		c.req(result).Get("/v1.2/public/categories/insert"))

	return *result, err
}

func (c *Client) GetFiltersSectionNewad() (NewadFilters, error) {
	result := &NewadFilters{}
	_, err := handleError(
		c.req(result).
			SetQueryParam("section", "newad").
			Get("/v1.2/public/filters"))

	return *result, err
}

func (c *Client) PostListing(listing Listing) error {
	_, err := handleError(
		c.req(nil).
			SetBody(listing).
			Post("/v2/listings"))

	return err
}

// handleError is a generic error handler for failing response (>399 status
// code). Without this, failing responses would have nil error.
func handleError(res *resty.Response, err error) (*resty.Response, error) {
	if err != nil {
		return res, err
	}
	if res.IsError() {
		log.Error().
			Str("url", res.Request.URL).
			Str("method", res.Request.Method).
			Int("status_code", res.StatusCode()).
			Interface("request", res.Request.Body).
			Bytes("response", res.Body()).
			Send()
		return res, errors.Errorf("request failed: %s %s", res.Request.Method, res.Request.URL)
	}

	return res, nil
}

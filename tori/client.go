package tori

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-resty/resty/v2"
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

func (c *Client) req(ctx context.Context, result any) *resty.Request {
	request := c.httpClient.
		NewRequest().
		SetContext(ctx).
		SetHeader("Authorization", c.auth)

	if result != nil {
		request.SetResult(result)
	}

	return request
}

func (c *Client) GetAccount(ctx context.Context, accountId string) (Account, error) {
	result := &AccountResponse{}

	_, err := handleError(c.req(ctx, result).
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

func (c *Client) UploadMedia(ctx context.Context, data []byte) (Media, error) {
	media := Media{}

	res, err := handleError(
		c.req(ctx, nil).SetHeaders(
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

func (c *Client) GetListing(ctx context.Context, id string) (Ad, error) {
	result := &GetListingResponse{}
	_, err := handleError(
		c.req(ctx, result).
			SetPathParam("id", id).
			Get("/v2/listings/{id}"))

	return result.Ad, err
}

func (c *Client) GetCategories(ctx context.Context) (Categories, error) {
	result := &Categories{}
	_, err := handleError(
		c.req(ctx, result).Get("/v1.2/public/categories/insert"))

	return *result, err
}

func (c *Client) GetFiltersSectionNewad(ctx context.Context) (NewadFilters, error) {
	result := &NewadFilters{}
	_, err := handleError(
		c.req(ctx, result).
			SetQueryParam("section", "newad").
			Get("/v1.2/public/filters"))

	return *result, err
}

func (c *Client) PostListing(ctx context.Context, listing Listing) error {
	_, err := handleError(
		c.req(ctx, nil).
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
		return res, fmt.Errorf("request failed: %s %s (status: %d)", res.Request.Method, res.Request.URL, res.StatusCode())
	}

	return res, nil
}

package tori

import (
	"context"
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

// GetAuth returns the authorization header value
func (c *Client) GetAuth() string {
	return c.auth
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

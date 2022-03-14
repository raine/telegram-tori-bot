package tori

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-resty/resty/v2"
)

const (
	ApiBaseUrl = "https://api.tori.fi/api"
)

type AccountResponse struct {
	Account `json:"account"`
}

type Location interface{}

type Account struct {
	AccountId string `json:"account_id"`
	Locations []Location
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

func (c *Client) req(result interface{}) *resty.Request {
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

	_, err := c.req(result).
		SetPathParams(map[string]string{
			"accountId": accountId,
		}).
		Get("/v1.2/private/accounts/{accountId}")

	return result.Account, err
}

type UploadMediaResponse struct {
	Image `json:"image"`
}

type Image struct {
	Id  string `json:"id"`
	Url string `json:"url"`
}

func (c *Client) UploadMedia(file *os.File) (Image, error) {
	image := Image{}
	fileInfo, err := file.Stat()
	if err != nil {
		return image, err
	}

	res, err := c.req(nil).SetHeaders(
		map[string]string{
			"Content-Type":   "application/x-www-form-urlencoded",
			"User-Agent":     "Tori/12.1.16 (com.tori.tori; build:190; iOS 15.3.1) Alamofire/5.4.4",
			"Content-Length": fmt.Sprintf("%v", fileInfo.Size()),
		},
	).
		SetBody(file).
		Post("/v2.2/media")
	if err != nil {
		return image, err
	}

	// Tori API returns the wrong content-type (text/plain) so we can't use
	// resty's SetResult to unmarshal JSON automatically.
	var uploadImageResponse UploadMediaResponse
	err = json.Unmarshal(res.Body(), &uploadImageResponse)
	if err != nil {
		return image, err
	}
	return uploadImageResponse.Image, err
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
	_, err := c.req(result).
		SetPathParam("id", id).
		Get("/v2/listings/{id}")

	return result.Ad, err
}

func (c *Client) GetCategories() (Categories, error) {
	result := &Categories{}
	_, err := c.req(result).Get("/v1.2/public/categories/insert")
	return *result, err
}

func (c *Client) GetFiltersSectionNewad() (NewadFilters, error) {
	result := &NewadFilters{}
	_, err := c.req(result).
		SetQueryParam("section", "newad").
		Get("/v1.2/public/filters")
	return *result, err
}

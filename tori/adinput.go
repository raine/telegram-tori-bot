package tori

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

const (
	AdinputBaseURL = "https://apps-adinput.svc.tori.fi"
	GatewayBaseURL = "https://apps-gw-poc.svc.tori.fi"

	// Service names for gateway routing
	ServiceAdinput      = "APPS-ADINPUT"
	ServiceItemCreation = "RC-ITEM-CREATION-FLOW-API"
	ServiceTjtAPI       = "TJT-API"
	ServiceAdAction     = "AD-ACTION"

	// Android app version strings - update these when the API requires newer versions
	// Format: ToriApp_And/{version} (Linux; U; Android {os}; {locale}; {device} Build/{build}) ToriNativeApp(UA spoofed for tracking) ToriApp_And
	androidUserAgent      = "ToriApp_And/26.4.0 (Linux; U; Android 14; en_us; Pixel 6 Build/UP1A.231005.007) ToriNativeApp(UA spoofed for tracking) ToriApp_And"
	androidAppVersionName = "26.4.0"
	androidAppBuildNumber = "26357"
	androidOSVersion      = "14"
	androidDevice         = "Pixel 6"
	adinputVersion        = "viewings"
)

// AdinputClient handles the new tori.fi ad creation APIs
type AdinputClient struct {
	httpClient     *http.Client
	bearerToken    string
	installationID string
	baseURL        string
}

// NewAdinputClient creates a new client for the adinput APIs
func NewAdinputClient(bearerToken string) *AdinputClient {
	return &AdinputClient{
		httpClient:     &http.Client{},
		bearerToken:    bearerToken,
		installationID: "cliTool001",
		baseURL:        GatewayBaseURL,
	}
}

// NewAdinputClientWithBaseURL creates a client with a custom base URL (for testing)
func NewAdinputClientWithBaseURL(bearerToken, baseURL string) *AdinputClient {
	return &AdinputClient{
		httpClient:     &http.Client{},
		bearerToken:    bearerToken,
		installationID: "cliTool001",
		baseURL:        baseURL,
	}
}

// setCommonHeaders sets headers common to all requests
func (c *AdinputClient) setCommonHeaders(req *http.Request, service string, body []byte) {
	path := req.URL.Path
	method := req.Method

	req.Header.Set("User-Agent", androidUserAgent)
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)

	// Gateway headers
	req.Header.Set("finn-device-info", "Android, mobile")
	req.Header.Set("finn-gw-service", service)
	req.Header.Set("finn-gw-key", CalculateGatewayKey(method, path, service, body))
	req.Header.Set("finn-app-installation-id", c.installationID)

	// NMP headers (Android format)
	req.Header.Set("x-nmp-os-name", "Android")
	req.Header.Set("x-nmp-os-version", androidOSVersion)
	req.Header.Set("x-nmp-app-version-name", androidAppVersionName)
	req.Header.Set("x-nmp-app-build-number", androidAppBuildNumber)
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-device", androidDevice)
	req.Header.Set("x-finn-apps-adinput-version-name", adinputVersion)
}

// requestOptions contains optional settings for doJSON
type requestOptions struct {
	etag          string // If-Match header value
	expectedCodes []int  // Expected success status codes (defaults to 200)
}

// doJSON performs a JSON API request and decodes the response.
// The reqBody can be nil, a map, or any struct that can be JSON marshaled.
// The respDest should be a pointer to a struct to decode the response into, or nil if no response body is expected.
func (c *AdinputClient) doJSON(ctx context.Context, method, fullURL, service string, reqBody any, respDest any, opts *requestOptions) error {
	var bodyReader io.Reader
	var bodyBytes []byte

	if reqBody != nil {
		var err error
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return err
	}

	c.setCommonHeaders(req, service, bodyBytes)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Length", "0")
	}
	if opts != nil && opts.etag != "" {
		req.Header.Set("If-Match", opts.etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check status code
	expectedCodes := []int{http.StatusOK}
	if opts != nil && len(opts.expectedCodes) > 0 {
		expectedCodes = opts.expectedCodes
	}
	statusOK := false
	for _, code := range expectedCodes {
		if resp.StatusCode == code {
			statusOK = true
			break
		}
	}
	if !statusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	if respDest != nil {
		if err := json.NewDecoder(resp.Body).Decode(respDest); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
}

// DraftAd represents the ad object returned when creating a draft
type DraftAd struct {
	ID          string `json:"id"`
	AdType      string `json:"ad-type"`
	ETag        string `json:"etag"`
	UpdateURL   string `json:"update-url"`
	UploadURL   string `json:"upload-url"`
	CheckoutURL string `json:"checkout-url"`
}

// CreateDraftResponse is the response from creating a draft ad
type CreateDraftResponse struct {
	Ad DraftAd `json:"ad"`
	// Model is ignored - too large and we fetch attributes separately
}

// CreateDraftAd creates a new draft ad
func (c *AdinputClient) CreateDraftAd(ctx context.Context) (*DraftAd, error) {
	var result CreateDraftResponse
	err := c.doJSON(ctx, "POST", AdinputBaseURL+"/adinput/ad/withModel/recommerce", ServiceAdinput, nil, &result, &requestOptions{
		expectedCodes: []int{http.StatusCreated},
	})
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}
	return &result.Ad, nil
}

// UploadImageResponse contains the image path from upload
type UploadImageResponse struct {
	ImagePath string // from adinput-image-path header
	Location  string // from Location header
}

// UploadImage uploads an image to the draft ad
func (c *AdinputClient) UploadImage(ctx context.Context, adID string, imageData []byte) (*UploadImageResponse, error) {
	path := fmt.Sprintf("/adinput/ad/recommerce/%s/upload", adID)
	reqURL := AdinputBaseURL + path

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "image")
	if err != nil {
		return nil, err
	}
	part.Write(imageData)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, &buf)
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, ServiceAdinput, nil)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("upload-draft-interop-version", "6")
	req.Header.Set("upload-complete", "?1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to upload image: %d - %s", resp.StatusCode, string(body))
	}

	return &UploadImageResponse{
		ImagePath: resp.Header.Get("adinput-image-path"),
		Location:  resp.Header.Get("Location"),
	}, nil
}

// CategoryPrediction represents a predicted category
type CategoryPrediction struct {
	ID     int                 `json:"id"`
	Label  string              `json:"label"`
	Parent *CategoryPrediction `json:"parent,omitempty"`
}

// CategoryPredictionsResponse is the response from category predictions
type CategoryPredictionsResponse struct {
	ID         string `json:"id"`
	Prediction struct {
		Categories []CategoryPrediction `json:"categories"`
	} `json:"prediction"`
}

// GetCategoryPredictions gets AI-suggested categories based on the uploaded image
func (c *AdinputClient) GetCategoryPredictions(ctx context.Context, adID string) ([]CategoryPrediction, error) {
	var result CategoryPredictionsResponse
	err := c.doJSON(ctx, "POST", c.baseURL+"/categories/predictions/"+adID, ServiceItemCreation, nil, &result, nil)
	if err != nil {
		return nil, fmt.Errorf("get category predictions: %w", err)
	}
	return result.Prediction.Categories, nil
}

// PatchItemResponse is the response from patching an item
type PatchItemResponse struct {
	ID   int    `json:"id"`
	ETag string `json:"etag"`
}

// PatchItem updates item data (used to set image, category, etc.)
func (c *AdinputClient) PatchItem(ctx context.Context, adID, etag string, data map[string]any) (*PatchItemResponse, error) {
	var result PatchItemResponse
	err := c.doJSON(ctx, "PATCH", c.baseURL+"/items/"+adID, ServiceItemCreation, map[string]any{"data": data}, &result, &requestOptions{etag: etag})
	if err != nil {
		return nil, fmt.Errorf("patch item: %w", err)
	}
	return &result, nil
}

// AttributeOption represents an option for a select attribute
type AttributeOption struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// Attribute represents a category-specific attribute
type Attribute struct {
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Label         string            `json:"label"`
	IsPredictable bool              `json:"isPredictable"`
	Options       []AttributeOption `json:"options"`
}

// AttributesResponse is the response from getting attributes
type AttributesResponse struct {
	Attributes []Attribute `json:"attributes"`
	Category   struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
	} `json:"category"`
}

// GetAttributes gets category-specific attributes for the current category
func (c *AdinputClient) GetAttributes(ctx context.Context, adID string) (*AttributesResponse, error) {
	var result AttributesResponse
	err := c.doJSON(ctx, "GET", c.baseURL+"/attributes/"+adID, ServiceItemCreation, nil, &result, nil)
	if err != nil {
		return nil, fmt.Errorf("get attributes: %w", err)
	}
	return &result, nil
}

// AdUpdatePayload is the payload for updating an ad
type AdUpdatePayload struct {
	Category    string              `json:"category"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	TradeType   string              `json:"trade_type"`
	Location    []map[string]string `json:"location"`
	Image       []map[string]string `json:"image"`
	MultiImage  []map[string]any    `json:"multi_image"`
	Condition   string              `json:"condition,omitempty"`
	// Dynamic attributes are added via the Extra field
	Extra map[string]any `json:"-"`
}

// reservedPayloadKeys are the keys that cannot be overwritten by Extra fields
var reservedPayloadKeys = map[string]bool{
	"category":    true,
	"title":       true,
	"description": true,
	"trade_type":  true,
	"location":    true,
	"image":       true,
	"multi_image": true,
	"condition":   true,
}

// MarshalJSON custom marshals AdUpdatePayload to include extra fields
func (p AdUpdatePayload) MarshalJSON() ([]byte, error) {
	// Create a map with all the known fields
	m := map[string]any{
		"category":    p.Category,
		"title":       p.Title,
		"description": p.Description,
		"trade_type":  p.TradeType,
		"location":    p.Location,
		"image":       p.Image,
		"multi_image": p.MultiImage,
	}
	if p.Condition != "" {
		m["condition"] = p.Condition
	}
	// Add extra dynamic fields (skip reserved keys to prevent overwrites)
	for k, v := range p.Extra {
		if !reservedPayloadKeys[k] {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// UpdateAdResponse is the response from updating an ad
type UpdateAdResponse struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}

// UpdateAd updates the ad with all fields
func (c *AdinputClient) UpdateAd(ctx context.Context, adID, etag string, payload AdUpdatePayload) (*UpdateAdResponse, error) {
	var result UpdateAdResponse
	err := c.doJSON(ctx, "PUT", AdinputBaseURL+"/adinput/ad/recommerce/"+adID+"/update", ServiceAdinput, payload, &result, &requestOptions{etag: etag})
	if err != nil {
		return nil, fmt.Errorf("update ad: %w", err)
	}
	return &result, nil
}

// DeliveryOptions represents delivery settings for the ad
type DeliveryOptions struct {
	BuyNow             bool   `json:"buyNow"`
	Client             string `json:"client"`
	Meetup             bool   `json:"meetup"`
	SellerPaysShipping bool   `json:"sellerPaysShipping"`
	Shipping           bool   `json:"shipping"`
}

// SetDeliveryOptions sets delivery options for the ad
func (c *AdinputClient) SetDeliveryOptions(ctx context.Context, adID string, opts DeliveryOptions) error {
	err := c.doJSON(ctx, "POST", c.baseURL+"/ads/"+adID+"/delivery", ServiceTjtAPI, opts, nil, nil)
	if err != nil {
		return fmt.Errorf("set delivery options: %w", err)
	}
	return nil
}

// OrderResponse is the response from publishing an ad
type OrderResponse struct {
	OrderID     int  `json:"order-id"`
	IsCompleted bool `json:"is-completed"`
}

// PublishAd publishes the ad with the free package
func (c *AdinputClient) PublishAd(ctx context.Context, adID string) (*OrderResponse, error) {
	path := fmt.Sprintf("/adinput/order/choices/%s", adID)
	reqURL := AdinputBaseURL + path

	// URL-encoded form data
	formData := url.Values{}
	formData.Set("choices", "urn:product:package-specification:10") // Free package

	body := formData.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, ServiceAdinput, []byte(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to publish ad: %d - %s", resp.StatusCode, string(respBody))
	}

	var result OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteAd deletes a draft ad
func (c *AdinputClient) DeleteAd(ctx context.Context, adID string) error {
	path := "/ads/" + adID
	fullURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, "DELETE", fullURL, nil)
	if err != nil {
		return err
	}

	c.setCommonHeaders(req, ServiceAdAction, nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete ad: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

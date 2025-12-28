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
)

// AdinputClient handles the new tori.fi ad creation APIs
type AdinputClient struct {
	httpClient     *http.Client
	bearerToken    string
	installationID string
}

// NewAdinputClient creates a new client for the adinput APIs
func NewAdinputClient(bearerToken string) *AdinputClient {
	return &AdinputClient{
		httpClient:     &http.Client{},
		bearerToken:    bearerToken,
		installationID: "cliTool001",
	}
}

// setCommonHeaders sets headers common to all requests
func (c *AdinputClient) setCommonHeaders(req *http.Request, service string, body []byte) {
	path := req.URL.Path
	method := req.Method

	req.Header.Set("User-Agent", "ToriApp_iOS/26.4.0-26357 (iPhone; CPU iPhone OS 26.1 like Mac OS X)")
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)

	// Gateway headers
	req.Header.Set("finn-device-info", "iOS, mobile")
	req.Header.Set("finn-gw-service", service)
	req.Header.Set("finn-gw-key", CalculateGatewayKey(method, path, service, body))
	req.Header.Set("finn-app-installation-id", c.installationID)

	// NMP headers
	req.Header.Set("x-nmp-os-name", "iOS")
	req.Header.Set("x-nmp-os-version", "18.0")
	req.Header.Set("x-nmp-app-version-name", "26.4.0")
	req.Header.Set("x-nmp-app-build-number", "26357")
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-device", "iPhone")
	req.Header.Set("x-finn-apps-adinput-version-name", "viewings")
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
	path := "/adinput/ad/withModel/recommerce"
	reqURL := AdinputBaseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, "APPS-ADINPUT", nil)
	req.Header.Set("Content-Length", "0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create draft: %d - %s", resp.StatusCode, string(body))
	}

	var result CreateDraftResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
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

	c.setCommonHeaders(req, "APPS-ADINPUT", nil)
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
	path := fmt.Sprintf("/categories/predictions/%s", adID)
	reqURL := GatewayBaseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, "RC-ITEM-CREATION-FLOW-API", nil)
	req.Header.Set("Content-Length", "0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get category predictions: %d - %s", resp.StatusCode, string(body))
	}

	var result CategoryPredictionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
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
	path := fmt.Sprintf("/items/%s", adID)
	reqURL := GatewayBaseURL + path

	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, "RC-ITEM-CREATION-FLOW-API", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to patch item: %d - %s", resp.StatusCode, string(respBody))
	}

	var result PatchItemResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
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
	path := fmt.Sprintf("/attributes/%s", adID)
	reqURL := GatewayBaseURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, "RC-ITEM-CREATION-FLOW-API", nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get attributes: %d - %s", resp.StatusCode, string(body))
	}

	var result AttributesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
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
	// Add extra dynamic fields
	for k, v := range p.Extra {
		m[k] = v
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
	path := fmt.Sprintf("/adinput/ad/recommerce/%s/update", adID)
	reqURL := AdinputBaseURL + path

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	c.setCommonHeaders(req, "APPS-ADINPUT", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to update ad: %d - %s", resp.StatusCode, string(respBody))
	}

	var result UpdateAdResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
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
	path := fmt.Sprintf("/ads/%s/delivery", adID)
	reqURL := GatewayBaseURL + path

	body, err := json.Marshal(opts)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	c.setCommonHeaders(req, "TJT-API", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set delivery options: %d - %s", resp.StatusCode, string(respBody))
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

	c.setCommonHeaders(req, "APPS-ADINPUT", []byte(body))
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

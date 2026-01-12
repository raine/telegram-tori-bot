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
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	AdinputBaseURL = "https://apps-adinput.svc.tori.fi"
	GatewayBaseURL = "https://apps-gw-poc.svc.tori.fi"

	// Service names for gateway routing
	ServiceAdinput         = "APPS-ADINPUT"
	ServiceItemCreation    = "RC-ITEM-CREATION-FLOW-API"
	ServiceTjtAPI          = "TJT-API"
	ServiceAdAction        = "AD-ACTION"
	ServiceAdSummaries     = "AD-SUMMARIES"
	ServiceBillingTracking = "BILLING-TRACKING-SERVICE"
	ServiceOrderPayment    = "ORDER-PAYMENT-SERVER"

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
	abTestDeviceID string // UUID for A/B testing, persistent per client instance
}

// NewAdinputClient creates a new client for the adinput APIs.
// The installationID should be a unique UUID per user, persisted across sessions.
func NewAdinputClient(bearerToken, installationID string) *AdinputClient {
	return &AdinputClient{
		httpClient:     &http.Client{},
		bearerToken:    bearerToken,
		installationID: installationID,
		baseURL:        GatewayBaseURL,
		abTestDeviceID: uuid.New().String(),
	}
}

// NewAdinputClientWithBaseURL creates a client with a custom base URL (for testing).
// The installationID should be a unique UUID per user, persisted across sessions.
func NewAdinputClientWithBaseURL(bearerToken, installationID, baseURL string) *AdinputClient {
	return &AdinputClient{
		httpClient:     &http.Client{},
		bearerToken:    bearerToken,
		installationID: installationID,
		baseURL:        baseURL,
		abTestDeviceID: uuid.New().String(),
	}
}

// setCommonHeaders sets headers common to all requests
func (c *AdinputClient) setCommonHeaders(req *http.Request, service string, body []byte) {
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path = path + "?" + req.URL.RawQuery
	}
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

	// CMP consent headers
	req.Header.Set("cmp-analytics", "1")
	req.Header.Set("cmp-personalisation", "1")
	req.Header.Set("cmp-marketing", "1")
	req.Header.Set("cmp-advertising", "1")

	// A/B testing header - uses a persistent UUID per client instance (matches mobile app behavior)
	req.Header.Set("Ab-Test-Device-Id", c.abTestDeviceID)
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

// AdModel represents the model/schema returned with draft ad responses.
// It contains the full category tree and form field definitions.
type AdModel struct {
	Sections []ModelSection `json:"sections"`
}

// ModelSection represents a section in the ad model
type ModelSection struct {
	Content []ModelWidget `json:"content"`
}

// ModelWidget represents a widget in the model (e.g., category selector)
type ModelWidget struct {
	ID    string      `json:"id"` // e.g., "category"
	Nodes []ModelNode `json:"value-nodes"`
}

// ModelNode represents a node in the category tree from the model response.
// Used for parsing the hierarchical category structure.
type ModelNode struct {
	ID          string      `json:"id"` // Category ID as string (e.g., "78")
	Label       string      `json:"label"`
	Persistable bool        `json:"persistable"` // true = selectable leaf category
	Children    []ModelNode `json:"children,omitempty"`
}

// CreateDraftResponse is the response from creating a draft ad
type CreateDraftResponse struct {
	Ad    DraftAd  `json:"ad"`
	Model *AdModel `json:"model,omitempty"`
}

// CreateDraftAd creates a new draft ad and returns the model containing the category tree.
// The model can be used to populate the category cache with real Tori category IDs.
func (c *AdinputClient) CreateDraftAd(ctx context.Context) (*DraftAd, *AdModel, error) {
	var result CreateDraftResponse
	err := c.doJSON(ctx, "POST", AdinputBaseURL+"/adinput/ad/withModel/recommerce", ServiceAdinput, nil, &result, &requestOptions{
		expectedCodes: []int{http.StatusCreated},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create draft: %w", err)
	}
	return &result.Ad, result.Model, nil
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

	// Log all returned category predictions
	for i, cat := range result.Prediction.Categories {
		logEvent := log.Debug().
			Int("index", i).
			Int("id", cat.ID).
			Str("label", cat.Label)
		if cat.Parent != nil {
			logEvent = logEvent.Int("parentId", cat.Parent.ID).Str("parentLabel", cat.Parent.Label)
		}
		logEvent.Msg("category prediction from tori api")
	}

	return result.Prediction.Categories, nil
}

// ItemFields contains fields to patch to /items endpoint.
// Note: postalcode is intentionally omitted - iOS also doesn't patch it to /items
// and listings still pass review.
type ItemFields struct {
	Title       string
	Description string
	Condition   int            // 0 means not set
	Price       int            // 0 means not set (giveaway)
	Attributes  map[string]any // category-specific attributes (e.g., computeracc_type)
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

// PatchItemFields patches all item fields to the /items endpoint.
// This is required for the review system to see complete item data.
// Note: postalcode is NOT required - iOS also leaves it unset in /items.
func (c *AdinputClient) PatchItemFields(ctx context.Context, adID, etag string, fields ItemFields) (*PatchItemResponse, error) {
	data := make(map[string]any)

	if fields.Title != "" {
		data["title"] = fields.Title
	}
	if fields.Description != "" {
		data["description"] = fields.Description
	}
	if fields.Condition != 0 {
		data["condition"] = fields.Condition
	}
	if fields.Price > 0 {
		// Note: /items uses object format, not array
		data["price"] = map[string]any{"price_amount": fields.Price}
	}
	// Add category-specific attributes (already converted to proper types)
	for k, v := range fields.Attributes {
		data[k] = v
	}

	return c.PatchItem(ctx, adID, etag, data)
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

// DeliveryPageResponse is the response from GET /ui/addelivery
type DeliveryPageResponse struct {
	Context  DeliveryContext  `json:"context"`
	Sections DeliverySections `json:"sections"`
}

// DeliveryContext contains delivery configuration flags
type DeliveryContext struct {
	AdID             int      `json:"adId"`
	Shipping         bool     `json:"shipping"`
	DefaultShipping  bool     `json:"defaultShipping"`
	Meetup           bool     `json:"meetup"`
	DefaultMeetup    bool     `json:"defaultMeetup"`
	ShippingProducts []string `json:"shippingProducts"`
}

// DeliverySections contains the delivery page sections
type DeliverySections struct {
	Shipping ShippingSection `json:"shipping"`
}

// ShippingSection contains shipping configuration and saved address
type ShippingSection struct {
	Address SavedAddress `json:"address"`
}

// SavedAddress represents the user's saved shipping address from Tori
type SavedAddress struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	PostalCode  string `json:"postalCode"`
	City        string `json:"city"`
	PhoneNumber string `json:"phoneNumber"`
	MobilePhone string `json:"mobilePhone"`
	Email       string `json:"email"`
}

// ShippingInfo represents the detailed shipping information for Tori Diili
type ShippingInfo struct {
	Address         string   `json:"address"`
	City            string   `json:"city"`
	DeliveryPointID int      `json:"deliveryPointId"`
	FlatNo          int      `json:"flatNo"`
	FloorNo         int      `json:"floorNo"`
	FloorType       string   `json:"floorType"`
	HouseType       string   `json:"houseType"`
	Name            string   `json:"name"`
	PhoneNumber     string   `json:"phoneNumber"`
	PostalCode      string   `json:"postalCode"`
	Products        []string `json:"products"`
	SaveAddress     bool     `json:"saveAddress"`
	Size            string   `json:"size"`
	StreetName      string   `json:"streetName"`
	StreetNo        string   `json:"streetNo"`
}

// DeliveryOptions represents delivery settings for the ad
type DeliveryOptions struct {
	BuyNow             bool          `json:"buyNow"`
	Client             string        `json:"client"`
	Meetup             bool          `json:"meetup"`
	SellerPaysShipping bool          `json:"sellerPaysShipping"`
	Shipping           bool          `json:"shipping"`
	ShippingInfo       *ShippingInfo `json:"shippingInfo,omitempty"`
}

// SetDeliveryOptions sets delivery options for the ad
func (c *AdinputClient) SetDeliveryOptions(ctx context.Context, adID string, opts DeliveryOptions) error {
	err := c.doJSON(ctx, "POST", c.baseURL+"/ads/"+adID+"/delivery", ServiceTjtAPI, opts, nil, nil)
	if err != nil {
		return fmt.Errorf("set delivery options: %w", err)
	}
	return nil
}

// GetDeliveryPage fetches the delivery configuration page including the user's saved address
func (c *AdinputClient) GetDeliveryPage(ctx context.Context, adID string) (*DeliveryPageResponse, error) {
	path := fmt.Sprintf("/ui/addelivery?adId=%s&editMode=false", adID)
	var result DeliveryPageResponse
	err := c.doJSON(ctx, "GET", c.baseURL+path, ServiceTjtAPI, nil, &result, nil)
	if err != nil {
		return nil, fmt.Errorf("get delivery page: %w", err)
	}
	return &result, nil
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

// TrackAdConfirmation calls the ad confirmation tracking endpoint.
func (c *AdinputClient) TrackAdConfirmation(ctx context.Context, adID string, orderID int) error {
	params := url.Values{}
	params.Set("adId", adID)
	params.Set("orderId", strconv.Itoa(orderID))

	reqURL := c.baseURL + "/tracking/adconfirmation?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	c.setCommonHeaders(req, ServiceBillingTracking, nil)
	req.Header.Set("Content-Length", "0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Accept any 2xx status as success
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tracking request failed: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetOrderConfirmation fetches order confirmation after publishing.
// iOS calls this before tracking.
func (c *AdinputClient) GetOrderConfirmation(ctx context.Context, orderID int, adID string) error {
	path := fmt.Sprintf("/orders/%d/confirmation/%s", orderID, adID)
	return c.doJSON(ctx, "GET", c.baseURL+path, ServiceOrderPayment, nil, nil, nil)
}

// AdState represents the state of an ad
type AdState struct {
	Label   string `json:"label"`
	Type    string `json:"type"`
	Display string `json:"display"`
}

// AdAction represents an available action for an ad
type AdAction struct {
	Label  string `json:"label"`  // e.g. "Merkitse myydyksi"
	Method string `json:"method"` // "PUT", "DELETE", "GET"
	Name   string `json:"name"`   // "DISPOSE", "DELETE", "EDIT", etc.
	Path   string `json:"path"`   // e.g. "/items/123/dispose"
	URL    string `json:"url"`    // e.g. "/ads/dispose/123"
}

// AdSummary represents a single ad in the response from the AD-SUMMARIES service
type AdSummary struct {
	ID               int64      `json:"id"`
	Created          string     `json:"created"`
	Updated          string     `json:"updated"`
	Expires          string     `json:"expires"`
	DaysUntilExpires int        `json:"daysUntilExpires"`
	State            AdState    `json:"state"`
	Mode             string     `json:"mode"`
	Review           string     `json:"review"`
	Actions          []AdAction `json:"actions"`
	Data             struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Image    string `json:"image"`
	} `json:"data"`
	ExternalData struct {
		Clicks    AdStat `json:"clicks"`
		Favorites AdStat `json:"favorites"`
	} `json:"externalData"`
}

// AdStat represents a statistic like clicks or favorites
type AdStat struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// AdFacet represents a filter option in the response
type AdFacet struct {
	Label string `json:"label"`
	Name  string `json:"name"`
	Total int    `json:"total"`
}

// AdSummariesResult is the response from the /search endpoint with AD-SUMMARIES service
type AdSummariesResult struct {
	Summaries []AdSummary `json:"summaries"`
	Total     int         `json:"total"`
	Facets    []AdFacet   `json:"facets"`
}

// GetAdSummaries fetches the user's ads from the AD-SUMMARIES service.
// Facet can be: ALL, ACTIVE, DRAFT, PENDING, EXPIRED, DISPOSED
func (c *AdinputClient) GetAdSummaries(ctx context.Context, limit, offset int, facet string) (*AdSummariesResult, error) {
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	if facet != "" {
		params.Set("facet", facet)
	}

	path := "/search"
	fullURL := c.baseURL + path + "?" + params.Encode()

	var result AdSummariesResult
	err := c.doJSON(ctx, "GET", fullURL, ServiceAdSummaries, nil, &result, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch ad summaries: %w", err)
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

// DisposeAd marks an ad as sold (PUT /ads/dispose/{id})
func (c *AdinputClient) DisposeAd(ctx context.Context, adID string) error {
	path := fmt.Sprintf("/ads/dispose/%s", adID)
	fullURL := c.baseURL + path

	return c.doJSON(ctx, "PUT", fullURL, ServiceAdAction, map[string]string{}, nil, &requestOptions{
		expectedCodes: []int{http.StatusOK, http.StatusNoContent},
	})
}

// UndisposeAd reactivates a sold ad (DELETE /ads/dispose/{id})
func (c *AdinputClient) UndisposeAd(ctx context.Context, adID string) error {
	path := fmt.Sprintf("/ads/dispose/%s", adID)
	fullURL := c.baseURL + path

	return c.doJSON(ctx, "DELETE", fullURL, ServiceAdAction, nil, nil, &requestOptions{
		expectedCodes: []int{http.StatusOK, http.StatusNoContent},
	})
}

// AdWithModel represents the full ad data returned by the /ad/withModel/{adId} endpoint.
// This includes the ad state with etag and all field values needed for re-publishing.
type AdWithModel struct {
	Ad struct {
		ID          string         `json:"id"`
		AdType      string         `json:"ad-type"`
		ETag        string         `json:"etag"`
		UpdateURL   string         `json:"update-url"`
		UploadURL   string         `json:"upload-url"`
		CheckoutURL string         `json:"checkout-url"`
		Values      map[string]any `json:"values"`
	} `json:"ad"`
	// Model contains the schema/form definition - omitted as it's large and not needed for re-publishing
}

// GetAdWithModel fetches the full ad data including values and etag.
// This is used for re-publishing expired ads.
func (c *AdinputClient) GetAdWithModel(ctx context.Context, adID string) (*AdWithModel, error) {
	path := fmt.Sprintf("/adinput/ad/withModel/%s", adID)
	fullURL := AdinputBaseURL + path

	var result AdWithModel
	err := c.doJSON(ctx, "GET", fullURL, ServiceAdinput, nil, &result, nil)
	if err != nil {
		return nil, fmt.Errorf("get ad with model: %w", err)
	}
	return &result, nil
}

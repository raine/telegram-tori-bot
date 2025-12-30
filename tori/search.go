package tori

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	ServiceSearchQuest = "SEARCH-QUEST"

	// Search keys for different verticals
	SearchKeyBapCommon = "SEARCH_ID_BAP_COMMON" // General marketplace (tori.fi)
	SearchKeyBapAll    = "SEARCH_ID_BAP_ALL"    // All marketplace items
)

// SearchClient handles tori.fi search API
type SearchClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewSearchClient creates a new search client
func NewSearchClient() *SearchClient {
	return &SearchClient{
		httpClient: &http.Client{},
		baseURL:    GatewayBaseURL,
	}
}

// NewSearchClientWithBaseURL creates a search client with a custom base URL (for testing)
func NewSearchClientWithBaseURL(baseURL string) *SearchClient {
	return &SearchClient{
		httpClient: &http.Client{},
		baseURL:    baseURL,
	}
}

// SearchParams contains search query parameters
type SearchParams struct {
	Query            string           // Free text search query
	Location         string           // Location filter (e.g., "Helsinki")
	CategoryTaxonomy CategoryTaxonomy // Category taxonomy filter with param name and value
	Page             int              // Page number (starts at 0)
	Rows             int              // Results per page
}

// SearchResult is the response from the search API
type SearchResult struct {
	Docs     []SearchDoc    `json:"docs"`
	Metadata SearchMetadata `json:"metadata"`
}

// SearchMetadata contains pagination info
type SearchMetadata struct {
	ResultSize   ResultSize `json:"result_size"`
	Paging       Paging     `json:"paging"`
	SearchParams struct {
		Q string `json:"q"`
	} `json:"search_params"`
}

// ResultSize contains count information
type ResultSize struct {
	MatchCount int `json:"match_count"`
}

// Paging contains pagination info
type Paging struct {
	Last   int    `json:"last"`
	Param  string `json:"param"` // Can be string or int in API
	Offset int    `json:"offset"`
}

// SearchDoc represents a single search result
type SearchDoc struct {
	ID           string       `json:"id"`
	Heading      string       `json:"heading"`
	Location     string       `json:"location"`
	Timestamp    int64        `json:"timestamp"`
	Image        *SearchImage `json:"image,omitempty"`
	Price        *SearchPrice `json:"price,omitempty"`
	TradeType    string       `json:"trade_type"`
	CanonicalURL string       `json:"canonical_url"`
	Type         string       `json:"type"`
	Flags        []string     `json:"flags,omitempty"`
}

// SearchImage contains image info
type SearchImage struct {
	URL    string `json:"url"`
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// SearchPrice contains price info
type SearchPrice struct {
	Amount int    `json:"amount"`
	Value  string `json:"value"`
}

// Search performs a search query
func (c *SearchClient) Search(ctx context.Context, searchKey string, params SearchParams) (*SearchResult, error) {
	// Build query parameters
	queryParams := url.Values{}
	queryParams.Set("client", "android")

	if params.Query != "" {
		queryParams.Set("q", params.Query)
	}
	if params.Location != "" {
		queryParams.Set("location", params.Location)
	}
	if params.Rows > 0 {
		queryParams.Set("rows", fmt.Sprintf("%d", params.Rows))
	} else {
		queryParams.Set("rows", "20") // Default
	}
	if params.Page > 0 {
		queryParams.Set("page", fmt.Sprintf("%d", params.Page))
	}
	if params.CategoryTaxonomy.Value != "" {
		queryParams.Set(params.CategoryTaxonomy.ParamName, params.CategoryTaxonomy.Value)
	}

	// Build URL
	reqURL := fmt.Sprintf("%s/search/%s?%s", c.baseURL, searchKey, queryParams.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed: %d - %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// setHeaders sets the required headers for search requests
func (c *SearchClient) setHeaders(req *http.Request) {
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path = path + "?" + req.URL.RawQuery
	}

	req.Header.Set("User-Agent", androidUserAgent)
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Gateway headers
	req.Header.Set("finn-device-info", "Android, mobile")
	req.Header.Set("finn-gw-service", ServiceSearchQuest)
	req.Header.Set("finn-gw-key", CalculateGatewayKey("GET", path, ServiceSearchQuest, nil))

	// NMP headers
	req.Header.Set("x-nmp-os-name", "Android")
	req.Header.Set("x-nmp-os-version", androidOSVersion)
	req.Header.Set("x-nmp-app-version-name", androidAppVersionName)
	req.Header.Set("x-nmp-app-build-number", androidAppBuildNumber)
	req.Header.Set("x-nmp-app-brand", "Tori")
	req.Header.Set("x-nmp-device", androidDevice)
}

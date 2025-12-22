package tori

import "context"

type SptMetadata struct {
	Category  string `json:"category"`
	Contentid string `json:"contentid"`
}

type ListAd struct {
	ListIdCode string `json:"list_id_code"`
}

type ListAdItem struct {
	ListAd      ListAd      `json:"ad"`
	SptMetadata SptMetadata `json:"spt_metadata"`
}

type SearchListingsResponse struct {
	ListAds []ListAdItem `json:"list_ads"`
}

func (c *Client) SearchListings(ctx context.Context, query string) ([]ListAdItem, error) {
	result := &SearchListingsResponse{}
	_, err := c.req(ctx, result).
		SetQueryParam("q", query).
		Get("/v2/listings/search")

	return result.ListAds, err
}

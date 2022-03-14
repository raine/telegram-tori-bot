package tori

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

func (c *Client) SearchListings(query string) ([]ListAdItem, error) {
	result := &SearchListingsResponse{}
	_, err := c.req(result).
		SetQueryParam("q", query).
		Get("/v2/listings/search")

	return result.ListAds, err
}

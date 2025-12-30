package tori

import "context"

// AdService abstracts the Tori.fi ad creation API operations.
// This interface allows for easy mocking in tests.
type AdService interface {
	// CreateDraftAd creates a new draft ad.
	CreateDraftAd(ctx context.Context) (*DraftAd, error)

	// UploadImage uploads an image to the draft ad.
	UploadImage(ctx context.Context, adID string, imageData []byte) (*UploadImageResponse, error)

	// GetCategoryPredictions gets AI-suggested categories based on the uploaded image.
	GetCategoryPredictions(ctx context.Context, adID string) ([]CategoryPrediction, error)

	// PatchItem updates item data (used to set image, category, etc.).
	PatchItem(ctx context.Context, adID, etag string, data map[string]any) (*PatchItemResponse, error)

	// GetAttributes gets category-specific attributes for the current category.
	GetAttributes(ctx context.Context, adID string) (*AttributesResponse, error)

	// UpdateAd updates the ad with all fields.
	UpdateAd(ctx context.Context, adID, etag string, payload AdUpdatePayload) (*UpdateAdResponse, error)

	// SetDeliveryOptions sets delivery options for the ad.
	SetDeliveryOptions(ctx context.Context, adID string, opts DeliveryOptions) error

	// PublishAd publishes the ad with the free package.
	PublishAd(ctx context.Context, adID string) (*OrderResponse, error)

	// DeleteAd deletes a draft ad.
	DeleteAd(ctx context.Context, adID string) error
}

// Ensure AdinputClient implements AdService
var _ AdService = (*AdinputClient)(nil)

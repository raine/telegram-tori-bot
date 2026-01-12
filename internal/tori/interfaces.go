package tori

import "context"

// AdService abstracts the Tori.fi ad creation API operations.
// This interface allows for easy mocking in tests.
type AdService interface {
	// CreateDraftAd creates a new draft ad and returns the model containing the category tree.
	CreateDraftAd(ctx context.Context) (*DraftAd, *AdModel, error)

	// UploadImage uploads an image to the draft ad.
	UploadImage(ctx context.Context, adID string, imageData []byte) (*UploadImageResponse, error)

	// GetCategoryPredictions gets AI-suggested categories based on the uploaded image.
	GetCategoryPredictions(ctx context.Context, adID string) ([]CategoryPrediction, error)

	// PatchItem updates item data (used to set image, category, etc.).
	PatchItem(ctx context.Context, adID, etag string, data map[string]any) (*PatchItemResponse, error)

	// PatchItemFields patches all item fields to the /items endpoint.
	// This is required for the review system to see complete item data.
	PatchItemFields(ctx context.Context, adID, etag string, fields ItemFields) (*PatchItemResponse, error)

	// GetAttributes gets category-specific attributes for the current category.
	GetAttributes(ctx context.Context, adID string) (*AttributesResponse, error)

	// UpdateAd updates the ad with all fields.
	UpdateAd(ctx context.Context, adID, etag string, payload AdUpdatePayload) (*UpdateAdResponse, error)

	// SetDeliveryOptions sets delivery options for the ad.
	SetDeliveryOptions(ctx context.Context, adID string, opts DeliveryOptions) error

	// GetDeliveryPage fetches the delivery configuration page including the user's saved address.
	GetDeliveryPage(ctx context.Context, adID string) (*DeliveryPageResponse, error)

	// PublishAd publishes the ad with the free package.
	PublishAd(ctx context.Context, adID string) (*OrderResponse, error)

	// TrackAdConfirmation calls the ad confirmation tracking endpoint.
	TrackAdConfirmation(ctx context.Context, adID string, orderID int) error

	// DeleteAd deletes a draft ad.
	DeleteAd(ctx context.Context, adID string) error

	// GetAdSummaries fetches the user's ads.
	GetAdSummaries(ctx context.Context, limit, offset int, facet string) (*AdSummariesResult, error)

	// DisposeAd marks an ad as sold.
	DisposeAd(ctx context.Context, adID string) error

	// UndisposeAd reactivates a sold ad.
	UndisposeAd(ctx context.Context, adID string) error

	// GetAdWithModel fetches full ad data including values and etag.
	GetAdWithModel(ctx context.Context, adID string) (*AdWithModel, error)
}

// Ensure AdinputClient implements AdService
var _ AdService = (*AdinputClient)(nil)

package tori

import (
	"context"
	"sync"
)

// MockAdService is a test double for AdService.
// Each method can be overridden with a custom function.
// If not overridden, methods return sensible defaults.
// Thread-safe for use in concurrent tests.
type MockAdService struct {
	CreateDraftAdFunc          func(ctx context.Context) (*DraftAd, *AdModel, error)
	UploadImageFunc            func(ctx context.Context, adID string, imageData []byte) (*UploadImageResponse, error)
	GetCategoryPredictionsFunc func(ctx context.Context, adID string) ([]CategoryPrediction, error)
	PatchItemFunc              func(ctx context.Context, adID, etag string, data map[string]any) (*PatchItemResponse, error)
	PatchItemFieldsFunc        func(ctx context.Context, adID, etag string, fields ItemFields) (*PatchItemResponse, error)
	GetAttributesFunc          func(ctx context.Context, adID string) (*AttributesResponse, error)
	UpdateAdFunc               func(ctx context.Context, adID, etag string, payload AdUpdatePayload) (*UpdateAdResponse, error)
	SetDeliveryOptionsFunc     func(ctx context.Context, adID string, opts DeliveryOptions) error
	GetDeliveryPageFunc        func(ctx context.Context, adID string) (*DeliveryPageResponse, error)
	PublishAdFunc              func(ctx context.Context, adID string) (*OrderResponse, error)
	TrackAdConfirmationFunc    func(ctx context.Context, adID string, orderID int) error
	GetOrderConfirmationFunc   func(ctx context.Context, orderID int, adID string) error
	GetProductContextFunc      func(ctx context.Context, adID string, adRevision string) error
	DeleteAdFunc               func(ctx context.Context, adID string) error
	GetAdSummariesFunc         func(ctx context.Context, limit, offset int, facet string) (*AdSummariesResult, error)
	DisposeAdFunc              func(ctx context.Context, adID string) error
	UndisposeAdFunc            func(ctx context.Context, adID string) error
	GetAdWithModelFunc         func(ctx context.Context, adID string) (*AdWithModel, error)

	mu sync.Mutex

	// Calls tracks all method invocations for assertions
	Calls []MockCall
}

// MockCall records a method call for test assertions.
type MockCall struct {
	Method string
	Args   []any
}

// Ensure MockAdService implements AdService
var _ AdService = (*MockAdService)(nil)

func (m *MockAdService) CreateDraftAd(ctx context.Context) (*DraftAd, *AdModel, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "CreateDraftAd"})
	fn := m.CreateDraftAdFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}
	return &DraftAd{
		ID:   "mock-draft-id",
		ETag: "mock-etag",
	}, nil, nil
}

func (m *MockAdService) UploadImage(ctx context.Context, adID string, imageData []byte) (*UploadImageResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "UploadImage", Args: []any{adID, len(imageData)}})
	fn := m.UploadImageFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, imageData)
	}
	return &UploadImageResponse{
		ImagePath: "mock/image/path.jpg",
		Location:  "https://mock.location/path.jpg",
	}, nil
}

func (m *MockAdService) GetCategoryPredictions(ctx context.Context, adID string) ([]CategoryPrediction, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetCategoryPredictions", Args: []any{adID}})
	fn := m.GetCategoryPredictionsFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return []CategoryPrediction{
		{ID: 100, Label: "Mock Category"},
	}, nil
}

func (m *MockAdService) PatchItem(ctx context.Context, adID, etag string, data map[string]any) (*PatchItemResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "PatchItem", Args: []any{adID, etag, data}})
	fn := m.PatchItemFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, etag, data)
	}
	return &PatchItemResponse{
		ID:   1,
		ETag: "mock-etag-v2",
	}, nil
}

func (m *MockAdService) PatchItemFields(ctx context.Context, adID, etag string, fields ItemFields) (*PatchItemResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "PatchItemFields", Args: []any{adID, etag, fields}})
	fn := m.PatchItemFieldsFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, etag, fields)
	}
	return &PatchItemResponse{
		ID:   1,
		ETag: "mock-etag-v2",
	}, nil
}

func (m *MockAdService) GetAttributes(ctx context.Context, adID string) (*AttributesResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetAttributes", Args: []any{adID}})
	fn := m.GetAttributesFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return &AttributesResponse{
		Attributes: []Attribute{},
		Category: struct {
			ID    int    `json:"id"`
			Label string `json:"label"`
		}{
			ID:    100,
			Label: "Mock Category",
		},
	}, nil
}

func (m *MockAdService) UpdateAd(ctx context.Context, adID, etag string, payload AdUpdatePayload) (*UpdateAdResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "UpdateAd", Args: []any{adID, etag, payload}})
	fn := m.UpdateAdFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, etag, payload)
	}
	return &UpdateAdResponse{
		ID:   adID,
		ETag: "mock-etag-v3",
	}, nil
}

func (m *MockAdService) SetDeliveryOptions(ctx context.Context, adID string, opts DeliveryOptions) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "SetDeliveryOptions", Args: []any{adID, opts}})
	fn := m.SetDeliveryOptionsFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, opts)
	}
	return nil
}

func (m *MockAdService) GetDeliveryPage(ctx context.Context, adID string) (*DeliveryPageResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetDeliveryPage", Args: []any{adID}})
	fn := m.GetDeliveryPageFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return &DeliveryPageResponse{
		Context: DeliveryContext{
			AdID:            12345,
			DefaultShipping: true,
			DefaultMeetup:   true,
		},
		Sections: DeliverySections{
			Shipping: ShippingSection{
				Address: SavedAddress{
					Name:        "Test User",
					Address:     "Testikatu 1",
					PostalCode:  "00100",
					City:        "Helsinki",
					PhoneNumber: "0401234567",
				},
			},
		},
	}, nil
}

func (m *MockAdService) PublishAd(ctx context.Context, adID string) (*OrderResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "PublishAd", Args: []any{adID}})
	fn := m.PublishAdFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return &OrderResponse{
		OrderID:     12345,
		IsCompleted: true,
	}, nil
}

func (m *MockAdService) TrackAdConfirmation(ctx context.Context, adID string, orderID int) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "TrackAdConfirmation", Args: []any{adID, orderID}})
	fn := m.TrackAdConfirmationFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, orderID)
	}
	return nil
}

func (m *MockAdService) GetOrderConfirmation(ctx context.Context, orderID int, adID string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetOrderConfirmation", Args: []any{orderID, adID}})
	fn := m.GetOrderConfirmationFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, orderID, adID)
	}
	return nil
}

func (m *MockAdService) GetProductContext(ctx context.Context, adID string, adRevision string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetProductContext", Args: []any{adID, adRevision}})
	fn := m.GetProductContextFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID, adRevision)
	}
	return nil
}

func (m *MockAdService) DeleteAd(ctx context.Context, adID string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "DeleteAd", Args: []any{adID}})
	fn := m.DeleteAdFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return nil
}

func (m *MockAdService) GetAdSummaries(ctx context.Context, limit, offset int, facet string) (*AdSummariesResult, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetAdSummaries", Args: []any{limit, offset, facet}})
	fn := m.GetAdSummariesFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, limit, offset, facet)
	}
	return &AdSummariesResult{
		Summaries: []AdSummary{},
		Total:     0,
		Facets:    []AdFacet{},
	}, nil
}

func (m *MockAdService) DisposeAd(ctx context.Context, adID string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "DisposeAd", Args: []any{adID}})
	fn := m.DisposeAdFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return nil
}

func (m *MockAdService) UndisposeAd(ctx context.Context, adID string) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "UndisposeAd", Args: []any{adID}})
	fn := m.UndisposeAdFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return nil
}

func (m *MockAdService) GetAdWithModel(ctx context.Context, adID string) (*AdWithModel, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockCall{Method: "GetAdWithModel", Args: []any{adID}})
	fn := m.GetAdWithModelFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, adID)
	}
	return &AdWithModel{
		Ad: struct {
			ID          string         `json:"id"`
			AdType      string         `json:"ad-type"`
			ETag        string         `json:"etag"`
			UpdateURL   string         `json:"update-url"`
			UploadURL   string         `json:"upload-url"`
			CheckoutURL string         `json:"checkout-url"`
			Values      map[string]any `json:"values"`
		}{
			ID:     adID,
			AdType: "SELL",
			ETag:   "mock-etag",
			Values: map[string]any{
				"title":       "Mock Title",
				"description": "Mock Description",
			},
		},
	}, nil
}

// Reset clears all recorded calls.
func (m *MockAdService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
}

// CallCount returns the number of times a method was called.
func (m *MockAdService) CallCount(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, call := range m.Calls {
		if call.Method == method {
			count++
		}
	}
	return count
}

// WasCalled returns true if the method was called at least once.
func (m *MockAdService) WasCalled(method string) bool {
	return m.CallCount(method) > 0
}

// LastCallArgs returns the arguments from the last call to the specified method.
func (m *MockAdService) LastCallArgs(method string) []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.Calls) - 1; i >= 0; i-- {
		if m.Calls[i].Method == method {
			return m.Calls[i].Args
		}
	}
	return nil
}

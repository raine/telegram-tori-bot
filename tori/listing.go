package tori

import (
	"encoding/json"
	"reflect"

	"github.com/pkg/errors"
)

type ListingType uint8

const (
	ListingTypeUnknown ListingType = iota
	ListingTypeSell
	ListingTypeBuy
	ListingTypeGive
)

var listingTypeMap = map[string]ListingType{
	"s": ListingTypeSell,
	"k": ListingTypeBuy,
	"g": ListingTypeGive,
}

func ParseListingType(s string) ListingType {
	listingType, ok := listingTypeMap[s]
	if ok {
		return listingType
	} else {
		return ListingTypeUnknown
	}
}

func (t ListingType) MarshalJSON() ([]byte, error) {
	var str string
	for k, v := range listingTypeMap {
		if t == v {
			str = k
		}
	}

	if str == "" {
		return nil, errors.Errorf("don't know how to marshal %+v", t)
	} else {
		return json.Marshal(str)
	}
}

type (
	Price       int
	AdDetails   map[string]any
	SingleValue string
	MultiValue  []string
)

type ListingLocation struct {
	Region  string `json:"region"`
	Zipcode string `json:"zipcode"`
	Area    string `json:"area"`
}

type ListingMedia struct {
	Id string `json:"media_id"`
}

type Listing struct {
	Subject     string           `json:"subject"`
	Body        string           `json:"body"`
	Price       Price            `json:"price"`
	Type        ListingType      `json:"type"`
	AdDetails   AdDetails        `json:"ad_details"`
	Category    string           `json:"category"`
	Location    *ListingLocation `json:"location,omitempty"`
	Images      *[]ListingMedia  `json:"images,omitempty"`
	PhoneHidden bool             `json:"phone_hidden"`
	AccountId   string           `json:"account_id"`
}

func (a AdDetails) MarshalJSON() ([]byte, error) {
	obj := make(map[string]any)

	for k, v := range a {
		switch v := v.(type) {
		case string:
			obj[k] = SingleValue(v)
		case []string:
			if len(v) != 0 {
				obj[k] = MultiValue(v)
			}
		default:
			return nil, errors.Errorf("invalid value type %s on key '%s'", reflect.TypeOf(v), k)
		}
	}

	return json.Marshal(obj)
}

func (s SingleValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"single": map[string]any{
			"code": string(s),
		},
	})
}

func (m MultiValue) MarshalJSON() ([]byte, error) {
	type MultiValueArray []map[string]string
	var multi MultiValueArray

	for _, c := range m {
		multi = append(multi, map[string]string{
			"code": c,
		})
	}

	return json.Marshal(map[string]MultiValueArray{
		"multiple": multi,
	})
}

func (p Price) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"value":    int(p),
		"currency": "€",
	})
}

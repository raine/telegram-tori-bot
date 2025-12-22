package tori

import (
	"fmt"
	"strings"
)

// doesListingMatchValue checks if a listing matches a specific key-value pair
func doesListingMatchValue(listing Listing, key string, value string) bool {
	switch key {
	case "category":
		if listing.Category != value {
			return false
		}
	case "type":
		if listing.Type != ListingType(ParseListingType(value)) {
			return false
		}
	default:
		// Apparently ? prefix in key is for checking if it exists, but it doesn't
		// seem very useful, because the next key is always the key itself
		if strings.HasPrefix(key, "?") {
			return true
		}

		if val, ok := listing.AdDetails[key]; ok {
			// * matches any value
			if value == "*" || val == value {
				return true
			} else {
				return false
			}
		}

		return false
	}

	return true
}

// doesListingMatchAllValues checks if a listing matches all key-value pairs
func doesListingMatchAllValues(listing Listing, keys []string, values []string) bool {
	for i, key := range keys {
		if m := doesListingMatchValue(listing, key, values[i]); !m {
			return false
		}
	}

	return true
}

// getMissingFieldFromSettingsResult finds the first missing field from a settings result
func getMissingFieldFromSettingsResult(paramMap ParamMap, listing Listing, settingsResult []string) string {
	for _, sr := range settingsResult {
		// Type and zipcode are always set so skip them
		if strings.HasPrefix(sr, "type") || sr == "zipcode" {
			continue
		}

		// Price is in top level of the Listing
		if sr == "price" {
			if listing.Price == 0 {
				return sr
			} else {
				continue
			}
		}

		param, ok := paramMap[sr]
		if !ok {
			panic(fmt.Sprintf("%s not found in param_map (should not happen)", sr))
		}
		var paramKey string
		switch {
		case param.SingleSelection != nil:
			paramKey = param.SingleSelection.ParamKey
		case param.MultiSelection != nil:
			paramKey = param.MultiSelection.ParamKey
		case param.Text != nil:
			paramKey = param.Text.ParamKey
		}

		// Otherwise, check if key is defined in listing.AdDetails
		if _, ok := listing.AdDetails[paramKey]; !ok {
			return sr
		}
	}

	return ""
}

// getMissingListingFieldWithSettingsParam finds missing field using a specific settings param
func getMissingListingFieldWithSettingsParam(
	paramMap ParamMap,
	settingsParam SettingsParam,
	listing Listing,
) string {
	for _, setting := range settingsParam.Settings {
		if doesListingMatchAllValues(listing, settingsParam.Keys, setting.Values) {
			return getMissingFieldFromSettingsResult(paramMap, listing, setting.SettingsResult)
		}
	}
	return ""
}

// GetMissingListingField determines which field needs to be filled next for a listing.
// Returns an empty string if all required fields are present.
func GetMissingListingField(paramMap ParamMap, settingsParams []SettingsParam, listing Listing) string {
	// body is not specified in tori's settings_param list so we check it separately
	if listing.Body == "" {
		return "body"
	}

	for _, settingsParam := range settingsParams {
		if next := getMissingListingFieldWithSettingsParam(paramMap, settingsParam, listing); next != "" {
			return next
		}
	}
	return ""
}

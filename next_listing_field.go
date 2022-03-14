package main

import (
	"fmt"
	"strings"

	"github.com/raine/go-telegram-bot/tori"
)

func doesListingMatchValue(listing tori.Listing, key string, value string) bool {
	switch key {
	case "category":
		if listing.Category != value {
			return false
		}
	case "type":
		if listing.Type != tori.ListingType(tori.ParseListingType(value)) {
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

func doesListingMatchAllValues(listing tori.Listing, keys []string, values []string) bool {
	for i, key := range keys {
		if m := doesListingMatchValue(listing, key, values[i]); !m {
			return false
		}
	}

	return true
}

func getMissingFieldFromSettingsResult(paramMap tori.ParamMap, listing tori.Listing, settingsResult []string) string {
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

func getMissingListingFieldWithSettingsParam(
	paramMap tori.ParamMap,
	settingsParam tori.SettingsParam,
	listing tori.Listing,
) string {
	for _, setting := range settingsParam.Settings {
		if doesListingMatchAllValues(listing, settingsParam.Keys, setting.Values) {
			return getMissingFieldFromSettingsResult(paramMap, listing, setting.SettingsResult)
		}
	}
	return ""
}

func getMissingListingField(paramMap tori.ParamMap, settingsParams []tori.SettingsParam, listing tori.Listing) string {
	for _, settingsParam := range settingsParams {
		if next := getMissingListingFieldWithSettingsParam(paramMap, settingsParam, listing); next != "" {
			return next
		}
	}
	return ""
}

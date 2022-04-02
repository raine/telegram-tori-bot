package main

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/raine/telegram-tori-bot/tori"
)

type NoLabelFoundError struct {
	Label string
	Field string
}

func (e *NoLabelFoundError) Error() string {
	return fmt.Sprintf("could not find value for label %s with field %s", e.Label, e.Field)
}

// findParamValueForLabel tries to find a value for a given human friendly
// label. For example if you have general_condition param, and the label
// "Uusi", the value would be "new".
func findParamValueForLabel(param tori.Param, label string) (string, error) {
	switch {
	case param.SingleSelection != nil:
		for _, v := range param.SingleSelection.ValuesList {
			if strings.EqualFold(v.Label, label) {
				return v.Value, nil
			}
		}

		return "", &NoLabelFoundError{Label: label, Field: param.SingleSelection.ParamKey}
	default:
		return "", errors.Errorf("findValueForLabel can only be used with single selection params")
	}
}

func initEmptyAdDetails(listing *tori.Listing) {
	if listing.AdDetails == nil {
		listing.AdDetails = tori.AdDetails{}
	}
}

func setListingFieldFromMessage(paramMap tori.ParamMap, listing tori.Listing, field string, message string) (tori.Listing, error) {
	switch field {
	case "body":
		listing.Body = strings.TrimSpace(message)
	case "price":
		price, err := parsePriceMessage(message)
		if err != nil {
			return listing, err
		}
		listing.Price = price
	default:
		param := paramMap[field]
		switch {
		case param.SingleSelection != nil:
			value, err := findParamValueForLabel(param, message)
			if err != nil {
				return listing, err
			}
			initEmptyAdDetails(&listing)
			listing.AdDetails[param.SingleSelection.ParamKey] = value
		case param.Text != nil:
			listing.AdDetails[param.Text.ParamKey] = message
		case param.MultiSelection != nil:
			paramKey := param.MultiSelection.ParamKey
			// delivery_options param is multi selection with single value. For a
			// bot, it makes more sense as a single selection with yes/no answers,
			// but in tori UI it is a checkbox multi selection.
			if field == "delivery_options" {
				initEmptyAdDetails(&listing)
				if message == "Kyllä" {
					listing.AdDetails[paramKey] = []string{param.MultiSelection.ValuesList[0].Value}
				} else {
					listing.AdDetails[paramKey] = []string{}
				}
				return listing, nil
			}

			return listing, errors.Errorf("multi selection param %s not implemented", field)
		default:
			return listing, errors.Errorf("could not find param for field %s", field)
		}
	}
	return listing, nil
}

func newListingFromMessage(message string) tori.Listing {
	parts := strings.Split(strings.TrimSpace(message), "\n\n")
	subject := parts[0]
	body := strings.Join(parts[1:], "\n\n")

	listing := tori.Listing{
		Subject: subject,
		Body:    body,
		// For now, assume only sell listings
		Type: tori.ListingTypeSell,
	}
	return listing
}

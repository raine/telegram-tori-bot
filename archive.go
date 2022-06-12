package main

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/raine/telegram-tori-bot/tori"
)

type ListingArchive struct {
	Listing tori.Listing         `json:"listing"`
	Photos  []tgbotapi.PhotoSize `json:"photos"`
}

func NewListingArchive(listing tori.Listing, photos []tgbotapi.PhotoSize) ListingArchive {
	// Images will be uploaded again from `photos` that are stored in Telegram
	listing.Images = nil
	// Location is queried again whenl the listing is sent so not needed in the archive
	listing.Location = nil
	// AccountId is queried again whenl the listing is sent so not needed in the archive
	listing.AccountId = ""

	archive := ListingArchive{
		Listing: listing,
		Photos:  photos,
	}

	return archive
}

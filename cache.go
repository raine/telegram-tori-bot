package main

import "github.com/raine/telegram-tori-bot/tori"

// TODO: TTL
var cachedNewadFilters *tori.NewadFilters

func clearCachedNewadFilters() {
	cachedNewadFilters = nil
}

func setCachedNewadFilters(newadFilters tori.NewadFilters) {
	cachedNewadFilters = &newadFilters
}

func getCachedNewadFilters() (tori.NewadFilters, bool) {
	if cachedNewadFilters == nil {
		return tori.NewadFilters{}, false
	} else {
		return *cachedNewadFilters, true
	}
}

package main

import "github.com/raine/go-telegram-bot/tori"

// TODO: TTL
var cachedNewadFilters *tori.FiltersNewad

func clearCachedNewadFilters() {
	cachedNewadFilters = nil
}

func setCachedNewadFilters(newadFilters tori.FiltersNewad) {
	cachedNewadFilters = &newadFilters
}

func getCachedNewadFilters() (tori.FiltersNewad, bool) {
	if cachedNewadFilters == nil {
		return tori.FiltersNewad{}, false
	} else {
		return *cachedNewadFilters, true
	}
}

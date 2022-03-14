package main

import (
	tf "github.com/raine/go-telegram-bot/tori_filters"
)

// TODO: TTL
var cachedNewadFilters *tf.FiltersNewad

func clearCachedNewadFilters() {
	cachedNewadFilters = nil
}

func setCachedNewadFilters(newadFilters tf.FiltersNewad) {
	cachedNewadFilters = &newadFilters
}

func getCachedNewadFilters() (tf.FiltersNewad, bool) {
	if cachedNewadFilters == nil {
		return tf.FiltersNewad{}, false
	} else {
		return *cachedNewadFilters, true
	}
}

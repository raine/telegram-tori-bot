package main

import (
	"os"
	"testing"

	"github.com/raine/go-telegram-bot/tori"
	"github.com/stretchr/testify/assert"
)

func TestCachedNewadFilters(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("tori/testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	newadFilters, err := tori.ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}

	clearCachedNewadFilters()
	_, ok := getCachedNewadFilters()
	assert.False(t, ok)
	setCachedNewadFilters(newadFilters)
	result, ok := getCachedNewadFilters()
	assert.True(t, ok)
	assert.NotEqual(t, tori.FiltersNewad{}, result)
}

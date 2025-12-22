package main

import (
	"os"
	"testing"
	"time"

	"github.com/raine/telegram-tori-bot/tori"
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
	assert.NotEqual(t, tori.NewadFilters{}, result)
}

func TestFilterCacheTTL(t *testing.T) {
	cache := NewFilterCache(10 * time.Millisecond)

	filters := tori.NewadFilters{}
	cache.Set(filters)

	// Should be available immediately
	_, ok := cache.Get()
	assert.True(t, ok, "cache should have data immediately after set")

	// Wait for TTL to expire (10x margin to avoid flakiness)
	time.Sleep(100 * time.Millisecond)

	// Should be expired now
	_, ok = cache.Get()
	assert.False(t, ok, "cache should be expired after TTL")
}

func TestFilterCacheClear(t *testing.T) {
	cache := NewFilterCache(time.Hour)

	filters := tori.NewadFilters{}
	cache.Set(filters)

	_, ok := cache.Get()
	assert.True(t, ok)

	cache.Clear()

	_, ok = cache.Get()
	assert.False(t, ok, "cache should be empty after clear")
}

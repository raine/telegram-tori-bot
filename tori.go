package main

import (
	"sort"
	"sync"

	"github.com/pkg/errors"
	"github.com/raine/go-telegram-bot/tori"
	"github.com/rs/zerolog/log"
)

func getCategoryFromSearchQuery(client *tori.Client, query string) (tori.Category, error) {
	category := tori.Category{}
	ads, err := client.SearchListings(query)
	if err != nil {
		return category, err
	}
	if len(ads) == 0 {
		return category, errors.Errorf("could not find category for search query")
	}

	listing, err := client.GetListing(ads[0].ListAd.ListIdCode)
	if err != nil {
		return category, err
	}
	return listing.Category, err
}

func getDistinctCategoriesFromSearchQuery(client *tori.Client, query string) ([]tori.Category, error) {
	ads, err := client.SearchListings(query)
	if err != nil {
		return nil, err
	}

	// Get unique categories with help of a map
	sptMetadataCategoryToListIdCode := make(map[string]string)
	for _, listAdItem := range ads {
		sptMetadataCategoryToListIdCode[listAdItem.SptMetadata.Category] = listAdItem.ListAd.ListIdCode
	}

	// Take 5 first list ids from the map
	const categoryCount = 5
	var listIds []string
	var i int
	for _, listIdCode := range sptMetadataCategoryToListIdCode {
		if i > categoryCount-1 {
			break
		}
		listIds = append(listIds, listIdCode)
		i++
	}

	var wg sync.WaitGroup
	categoryChan := make(chan tori.Category)
	for _, id := range listIds {
		wg.Add(1)
		go func(id string) {
			listing, err := client.GetListing(id)
			if err != nil {
				log.Error().Str("listIdCode", id).Err(err).Msg("error when fetching listing")
			} else {
				categoryChan <- listing.Category
			}
			defer wg.Done()
		}(id)
	}

	go func() {
		wg.Wait()
		close(categoryChan)
	}()

	categories := make([]tori.Category, 0, len(sptMetadataCategoryToListIdCode))
	for c := range categoryChan {
		categories = append(categories, c)
	}

	// Sort categories to avoid indeterminism in tests
	// TODO: Sort categories based on how many there are of each in listing
	// search result
	sort.SliceStable(categories, func(i, j int) bool {
		return categories[i].Code < categories[j].Code
	})

	return categories, nil
}

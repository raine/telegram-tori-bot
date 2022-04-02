package main

import (
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/raine/telegram-tori-bot/tori"
	"github.com/rs/zerolog/log"
	orderedmap "github.com/wk8/go-ordered-map"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"
)

const maxCategoryCount = 5

func getListingsCategoryMap(listings []tori.ListAdItem) *orderedmap.OrderedMap {
	sptMetadataCategoryToListIdCode := orderedmap.New()

	// Get unique categories from listings with help of a ordered map
	for _, listAdItem := range listings {
		sptMetadataCategoryToListIdCode.Set(listAdItem.SptMetadata.Category, listAdItem.ListAd.ListIdCode)
	}

	return sptMetadataCategoryToListIdCode
}

func getCategoriesForSubject(client *tori.Client, subject string) ([]tori.Category, error) {
	log.Info().Str("subject", subject).Msg("getting categories for subject")

	// Remove parenthesis blocks from subject. This could be something
	// like "(2 kpl)" or "(M-koko)"
	re := regexp.MustCompile(`\(.+\)`)
	subject = strings.TrimSpace(re.ReplaceAllString(subject, ""))

	allSubjectParts := strings.Split(subject, " ")
	accCategoryMap := orderedmap.New()

	for i := 0; i < len(allSubjectParts); i++ {
		parts := allSubjectParts[i:]
		query := strings.Join(parts, " ")
		log.Info().Str("query", query).Msg("searching listings with query")
		ads, err := client.SearchListings(query)
		if err != nil {
			return nil, err
		}

		categoryMap := getListingsCategoryMap(ads)
		log.Info().Msgf("got %d distinct categories", categoryMap.Len())

		for pair := categoryMap.Oldest(); pair != nil; pair = pair.Next() {
			if accCategoryMap.Len() < maxCategoryCount {
				accCategoryMap.Set(pair.Key, pair.Value)
			}
		}

		if accCategoryMap.Len() >= maxCategoryCount {
			break
		}
	}

	listingIds := make([]string, 0)
	for pair := accCategoryMap.Oldest(); pair != nil; pair = pair.Next() {
		listingIds = append(listingIds, pair.Value.(string))
	}

	g := new(errgroup.Group)
	listings := make([]tori.Ad, len(listingIds))
	for i := range listingIds {
		i := i
		g.Go(func() error {
			id := listingIds[i]
			listing, err := client.GetListing(id)
			if err != nil {
				log.Error().Str("listIdCode", id).Err(err).Msg("error when fetching listing")
				return err
			} else {
				listings[i] = listing
				return nil
			}
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	slices.SortStableFunc(listings, func(a tori.Ad, b tori.Ad) bool {
		aPos := slices.Index(listingIds, a.ListIdCode)
		bPos := slices.Index(listingIds, b.ListIdCode)
		return aPos < bPos
	})

	categories := make([]tori.Category, 0)
	for _, listing := range listings {
		categories = append(categories, listing.Category)
	}

	return categories, nil
}

func getLabelForField(
	paramMap tori.ParamMap,
	field string,
) (string, error) {
	param := paramMap[field]
	switch {
	case param.SingleSelection != nil:
		return (*param.SingleSelection).Label, nil
	case param.MultiSelection != nil:
		return param.MultiSelection.ValuesList[0].Label, nil
	case param.Text != nil:
		return (*param.Text).Label, nil
	default:
		return "", errors.Errorf("could not find param for field '%s'", field)
	}
}

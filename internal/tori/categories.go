// Package tori provides Tori.fi API clients and utilities.
//
// # Category Fallback System
//
// Tori's category prediction API (vision-based) sometimes returns completely wrong
// categories. For example, a saddle chair was predicted as "Musical instruments".
//
// This file provides an embedded category list and search functionality used as a
// fallback when Tori's predictions are rejected by the LLM. The flow is:
//  1. LLM rejects all Tori predictions (returns category_id 0)
//  2. LLM extracts category keywords from title/description
//  3. CategoryService.SearchCategories finds matching categories from embedded list
//  4. LLM selects from the fallback candidates
//
// The embedded category list covers common Tori marketplace categories. Category IDs
// should match Tori's actual IDs for the categories to work when submitting listings.
package tori

import (
	"sort"
	"strings"
	"sync"
)

// Category represents a Tori category with its full path
type Category struct {
	ID       int
	Label    string // The leaf label (e.g., "Työtuolit")
	FullPath string // Full path (e.g., "Koti ja sisustus > Huonekalut > Työtuolit")
	Parent   *CategoryParent
}

// CategoryParent represents the parent hierarchy for a category
type CategoryParent struct {
	ID     int
	Label  string
	Parent *CategoryParent
}

// CategoryService handles category loading and searching
type CategoryService struct {
	categories []Category
	Tree       *CategoryTree
	mu         sync.RWMutex
}

// NewCategoryService creates a new service with embedded category data
func NewCategoryService() *CategoryService {
	s := &CategoryService{
		categories: embeddedCategories,
	}
	s.Tree = BuildCategoryTree(s.categories)
	return s
}

// GetCategories returns all categories in the service
func (s *CategoryService) GetCategories() []Category {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.categories
}

// SearchCategories searches for categories matching any of the keywords.
// Returns up to limit matches as CategoryPrediction for compatibility with existing code.
func (s *CategoryService) SearchCategories(keywords []string, limit int) []CategoryPrediction {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(keywords) == 0 {
		return nil
	}

	// Normalize keywords
	normalizedKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		normalizedKeywords[i] = strings.ToLower(strings.TrimSpace(kw))
	}

	type scoredCategory struct {
		cat   Category
		score int
	}

	var matches []scoredCategory
	seen := make(map[int]bool)

	for _, cat := range s.categories {
		if seen[cat.ID] {
			continue
		}

		labelLower := strings.ToLower(cat.Label)
		pathLower := strings.ToLower(cat.FullPath)

		score := 0
		for _, kw := range normalizedKeywords {
			if kw == "" {
				continue
			}

			// Exact match on label (highest score)
			if labelLower == kw {
				score += 100
			} else if strings.Contains(labelLower, kw) {
				// Label contains keyword (e.g., kw "tuoli" matches "työtuolit")
				score += 50
			} else if strings.Contains(kw, labelLower) {
				// Keyword contains label (e.g., kw "satulatuoli" contains "tuoli")
				// Slightly lower score for more specific keywords
				score += 40
			} else if strings.Contains(pathLower, kw) {
				// Path contains keyword
				score += 25
			}
		}

		if score > 0 {
			matches = append(matches, scoredCategory{cat: cat, score: score})
			seen[cat.ID] = true
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	// Convert to CategoryPrediction
	var results []CategoryPrediction
	for i, m := range matches {
		if i >= limit {
			break
		}

		pred := CategoryPrediction{
			ID:    m.cat.ID,
			Label: m.cat.Label,
		}

		// Build parent chain
		if m.cat.Parent != nil {
			pred.Parent = buildCategoryParentPrediction(m.cat.Parent)
		}

		results = append(results, pred)
	}

	return results
}

func buildCategoryParentPrediction(p *CategoryParent) *CategoryPrediction {
	if p == nil {
		return nil
	}
	return &CategoryPrediction{
		ID:     p.ID,
		Label:  p.Label,
		Parent: buildCategoryParentPrediction(p.Parent),
	}
}

// embeddedCategories contains Tori categories for fallback search.
// This list covers common marketplace categories.
// Category IDs are based on Tori's actual category system.
var embeddedCategories = []Category{
	// Koti ja sisustus (Home and Interior) - parent 5000
	{ID: 5001, Label: "Sohvat ja nojatuolit", FullPath: "Koti ja sisustus > Huonekalut > Sohvat ja nojatuolit", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5002, Label: "Pöydät", FullPath: "Koti ja sisustus > Huonekalut > Pöydät", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5003, Label: "Tuolit", FullPath: "Koti ja sisustus > Huonekalut > Tuolit", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5004, Label: "Työtuolit", FullPath: "Koti ja sisustus > Huonekalut > Työtuolit", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5005, Label: "Toimistokalusteet", FullPath: "Koti ja sisustus > Huonekalut > Toimistokalusteet", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5006, Label: "Sängyt ja patjat", FullPath: "Koti ja sisustus > Huonekalut > Sängyt ja patjat", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5007, Label: "Kaapit ja hyllyt", FullPath: "Koti ja sisustus > Huonekalut > Kaapit ja hyllyt", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5008, Label: "TV-tasot ja mediakaapit", FullPath: "Koti ja sisustus > Huonekalut > TV-tasot ja mediakaapit", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5009, Label: "Muut huonekalut", FullPath: "Koti ja sisustus > Huonekalut > Muut huonekalut", Parent: &CategoryParent{ID: 5000, Label: "Huonekalut", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}}},
	{ID: 5010, Label: "Sisustustavarat", FullPath: "Koti ja sisustus > Sisustustavarat", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}},
	{ID: 5011, Label: "Valaisimet", FullPath: "Koti ja sisustus > Valaisimet", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}},
	{ID: 5012, Label: "Matot", FullPath: "Koti ja sisustus > Matot", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}},
	{ID: 5013, Label: "Keittiötarvikkeet", FullPath: "Koti ja sisustus > Keittiötarvikkeet", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}},
	{ID: 5014, Label: "Tekstiilit", FullPath: "Koti ja sisustus > Tekstiilit", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}},
	{ID: 5015, Label: "Kylpyhuone", FullPath: "Koti ja sisustus > Kylpyhuone", Parent: &CategoryParent{ID: 78, Label: "Koti ja sisustus"}},

	// Elektroniikka (Electronics) - parent 93
	{ID: 9301, Label: "Puhelimet", FullPath: "Elektroniikka > Puhelimet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9302, Label: "Tabletit", FullPath: "Elektroniikka > Tabletit", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9303, Label: "Tietokoneet", FullPath: "Elektroniikka > Tietokoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9304, Label: "Kannettavat", FullPath: "Elektroniikka > Kannettavat", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9305, Label: "Näytöt", FullPath: "Elektroniikka > Näytöt", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9306, Label: "Näytönohjaimet", FullPath: "Elektroniikka > Näytönohjaimet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9307, Label: "Televisiot", FullPath: "Elektroniikka > Televisiot", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9308, Label: "Kamerat", FullPath: "Elektroniikka > Kamerat", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9309, Label: "Pelikonsolit", FullPath: "Elektroniikka > Pelikonsolit", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9310, Label: "Äänentoistolaitteet", FullPath: "Elektroniikka > Äänentoistolaitteet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9311, Label: "Kuulokkeet", FullPath: "Elektroniikka > Kuulokkeet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9312, Label: "Älykellot ja rannekkeet", FullPath: "Elektroniikka > Älykellot ja rannekkeet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9313, Label: "Oheislaitteet", FullPath: "Elektroniikka > Oheislaitteet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9314, Label: "Tulostimet ja skannerit", FullPath: "Elektroniikka > Tulostimet ja skannerit", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},

	// Kodinkoneet (Appliances) - parent 93
	{ID: 9320, Label: "Kodinkoneet", FullPath: "Elektroniikka > Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}},
	{ID: 9321, Label: "Jääkaapit ja pakastimet", FullPath: "Elektroniikka > Kodinkoneet > Jääkaapit ja pakastimet", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},
	{ID: 9322, Label: "Pesukoneet ja kuivausrummut", FullPath: "Elektroniikka > Kodinkoneet > Pesukoneet ja kuivausrummut", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},
	{ID: 9323, Label: "Astianpesukoneet", FullPath: "Elektroniikka > Kodinkoneet > Astianpesukoneet", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},
	{ID: 9324, Label: "Liedet ja uunit", FullPath: "Elektroniikka > Kodinkoneet > Liedet ja uunit", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},
	{ID: 9325, Label: "Mikroaaltouunit", FullPath: "Elektroniikka > Kodinkoneet > Mikroaaltouunit", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},
	{ID: 9326, Label: "Pölynimurit", FullPath: "Elektroniikka > Kodinkoneet > Pölynimurit", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},
	{ID: 9327, Label: "Kahvinkeittimet ja -koneet", FullPath: "Elektroniikka > Kodinkoneet > Kahvinkeittimet ja -koneet", Parent: &CategoryParent{ID: 9320, Label: "Kodinkoneet", Parent: &CategoryParent{ID: 93, Label: "Elektroniikka"}}},

	// Urheilu ja ulkoilu (Sports) - parent 69
	{ID: 6901, Label: "Pyörät", FullPath: "Urheilu ja ulkoilu > Pyörät", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6902, Label: "Sähköpyörät", FullPath: "Urheilu ja ulkoilu > Sähköpyörät", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6903, Label: "Lasketteluvälineet", FullPath: "Urheilu ja ulkoilu > Lasketteluvälineet", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6904, Label: "Hiihtovälineet", FullPath: "Urheilu ja ulkoilu > Hiihtovälineet", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6905, Label: "Golf", FullPath: "Urheilu ja ulkoilu > Golf", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6906, Label: "Kalastus", FullPath: "Urheilu ja ulkoilu > Kalastus", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6907, Label: "Retkeily ja vaellus", FullPath: "Urheilu ja ulkoilu > Retkeily ja vaellus", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6908, Label: "Kuntosalilaitteet", FullPath: "Urheilu ja ulkoilu > Kuntosalilaitteet", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6909, Label: "Palloilu", FullPath: "Urheilu ja ulkoilu > Palloilu", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6910, Label: "Rullaluistelu ja skeittaus", FullPath: "Urheilu ja ulkoilu > Rullaluistelu ja skeittaus", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},
	{ID: 6911, Label: "Vesiurheilu", FullPath: "Urheilu ja ulkoilu > Vesiurheilu", Parent: &CategoryParent{ID: 69, Label: "Urheilu ja ulkoilu"}},

	// Vaatteet ja asusteet (Clothing) - parent 71
	{ID: 7101, Label: "Naisten vaatteet", FullPath: "Vaatteet ja asusteet > Naisten vaatteet", Parent: &CategoryParent{ID: 71, Label: "Vaatteet ja asusteet"}},
	{ID: 7102, Label: "Miesten vaatteet", FullPath: "Vaatteet ja asusteet > Miesten vaatteet", Parent: &CategoryParent{ID: 71, Label: "Vaatteet ja asusteet"}},
	{ID: 7103, Label: "Kengät", FullPath: "Vaatteet ja asusteet > Kengät", Parent: &CategoryParent{ID: 71, Label: "Vaatteet ja asusteet"}},
	{ID: 7104, Label: "Laukut ja lompakot", FullPath: "Vaatteet ja asusteet > Laukut ja lompakot", Parent: &CategoryParent{ID: 71, Label: "Vaatteet ja asusteet"}},
	{ID: 7105, Label: "Kellot ja korut", FullPath: "Vaatteet ja asusteet > Kellot ja korut", Parent: &CategoryParent{ID: 71, Label: "Vaatteet ja asusteet"}},
	{ID: 7106, Label: "Asusteet", FullPath: "Vaatteet ja asusteet > Asusteet", Parent: &CategoryParent{ID: 71, Label: "Vaatteet ja asusteet"}},

	// Lapset ja vanhemmuus (Children) - parent 68
	{ID: 6801, Label: "Lastenvaatteet", FullPath: "Lapset ja vanhemmuus > Lastenvaatteet", Parent: &CategoryParent{ID: 68, Label: "Lapset ja vanhemmuus"}},
	{ID: 6802, Label: "Lastenkengät", FullPath: "Lapset ja vanhemmuus > Lastenkengät", Parent: &CategoryParent{ID: 68, Label: "Lapset ja vanhemmuus"}},
	{ID: 6803, Label: "Lastenvaunut ja rattaat", FullPath: "Lapset ja vanhemmuus > Lastenvaunut ja rattaat", Parent: &CategoryParent{ID: 68, Label: "Lapset ja vanhemmuus"}},
	{ID: 6804, Label: "Turvaistuimet", FullPath: "Lapset ja vanhemmuus > Turvaistuimet", Parent: &CategoryParent{ID: 68, Label: "Lapset ja vanhemmuus"}},
	{ID: 6805, Label: "Lastenhuoneen kalusteet", FullPath: "Lapset ja vanhemmuus > Lastenhuoneen kalusteet", Parent: &CategoryParent{ID: 68, Label: "Lapset ja vanhemmuus"}},
	{ID: 6806, Label: "Lelut", FullPath: "Lapset ja vanhemmuus > Lelut", Parent: &CategoryParent{ID: 68, Label: "Lapset ja vanhemmuus"}},

	// Viihde ja harrastukset (Entertainment) - parent 86
	{ID: 8601, Label: "Kirjat", FullPath: "Viihde ja harrastukset > Kirjat", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8602, Label: "Elokuvat", FullPath: "Viihde ja harrastukset > Elokuvat", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8603, Label: "Musiikki", FullPath: "Viihde ja harrastukset > Musiikki", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8604, Label: "Pelit", FullPath: "Viihde ja harrastukset > Pelit", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8605, Label: "Videopelit", FullPath: "Viihde ja harrastukset > Videopelit", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8606, Label: "Soittimet", FullPath: "Viihde ja harrastukset > Soittimet", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8607, Label: "Keräily", FullPath: "Viihde ja harrastukset > Keräily", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8608, Label: "Käsityöt ja askartelu", FullPath: "Viihde ja harrastukset > Käsityöt ja askartelu", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},
	{ID: 8609, Label: "Valokuvaus", FullPath: "Viihde ja harrastukset > Valokuvaus", Parent: &CategoryParent{ID: 86, Label: "Viihde ja harrastukset"}},

	// Puutarha ja remontointi (Garden) - parent 67
	{ID: 6701, Label: "Puutarhakalusteet", FullPath: "Puutarha ja remontointi > Puutarhakalusteet", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},
	{ID: 6702, Label: "Puutarhatyökalut", FullPath: "Puutarha ja remontointi > Puutarhatyökalut", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},
	{ID: 6703, Label: "Grillit", FullPath: "Puutarha ja remontointi > Grillit", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},
	{ID: 6704, Label: "Rakennustarvikkeet", FullPath: "Puutarha ja remontointi > Rakennustarvikkeet", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},
	{ID: 6705, Label: "Työkalut", FullPath: "Puutarha ja remontointi > Työkalut", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},
	{ID: 6706, Label: "Lämmitys ja ilmanvaihto", FullPath: "Puutarha ja remontointi > Lämmitys ja ilmanvaihto", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},
	{ID: 6707, Label: "Porealtaat ja saunat", FullPath: "Puutarha ja remontointi > Porealtaat ja saunat", Parent: &CategoryParent{ID: 67, Label: "Puutarha ja remontointi"}},

	// Eläimet (Animals) - parent 77
	{ID: 7701, Label: "Koirat", FullPath: "Eläimet > Koirat", Parent: &CategoryParent{ID: 77, Label: "Eläimet"}},
	{ID: 7702, Label: "Kissat", FullPath: "Eläimet > Kissat", Parent: &CategoryParent{ID: 77, Label: "Eläimet"}},
	{ID: 7703, Label: "Pieneläimet", FullPath: "Eläimet > Pieneläimet", Parent: &CategoryParent{ID: 77, Label: "Eläimet"}},
	{ID: 7704, Label: "Linnut", FullPath: "Eläimet > Linnut", Parent: &CategoryParent{ID: 77, Label: "Eläimet"}},
	{ID: 7705, Label: "Akvaarioeläimet", FullPath: "Eläimet > Akvaarioeläimet", Parent: &CategoryParent{ID: 77, Label: "Eläimet"}},
	{ID: 7706, Label: "Eläintarvikkeet", FullPath: "Eläimet > Eläintarvikkeet", Parent: &CategoryParent{ID: 77, Label: "Eläimet"}},

	// Autot ja kuljetusvälineet (Vehicles) - parent 90
	{ID: 9001, Label: "Autot", FullPath: "Autot ja kuljetusvälineet > Autot", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9002, Label: "Moottoripyörät", FullPath: "Autot ja kuljetusvälineet > Moottoripyörät", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9003, Label: "Mopot ja skootterit", FullPath: "Autot ja kuljetusvälineet > Mopot ja skootterit", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9004, Label: "Veneet", FullPath: "Autot ja kuljetusvälineet > Veneet", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9005, Label: "Perävaunut", FullPath: "Autot ja kuljetusvälineet > Perävaunut", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9006, Label: "Mönkijät", FullPath: "Autot ja kuljetusvälineet > Mönkijät", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9007, Label: "Moottorikelkat", FullPath: "Autot ja kuljetusvälineet > Moottorikelkat", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9008, Label: "Renkaat ja vanteet", FullPath: "Autot ja kuljetusvälineet > Renkaat ja vanteet", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9009, Label: "Varaosat", FullPath: "Autot ja kuljetusvälineet > Varaosat", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},
	{ID: 9010, Label: "Autotarvikkeet", FullPath: "Autot ja kuljetusvälineet > Autotarvikkeet", Parent: &CategoryParent{ID: 90, Label: "Autot ja kuljetusvälineet"}},

	// Antiikki ja taide (Antiques) - parent 76
	{ID: 7601, Label: "Antiikki", FullPath: "Antiikki ja taide > Antiikki", Parent: &CategoryParent{ID: 76, Label: "Antiikki ja taide"}},
	{ID: 7602, Label: "Taulut ja taide", FullPath: "Antiikki ja taide > Taulut ja taide", Parent: &CategoryParent{ID: 76, Label: "Antiikki ja taide"}},
	{ID: 7603, Label: "Design", FullPath: "Antiikki ja taide > Design", Parent: &CategoryParent{ID: 76, Label: "Antiikki ja taide"}},

	// Liike-elämä (Business) - parent 91
	{ID: 9101, Label: "Toimistolaitteet", FullPath: "Liike-elämä > Toimistolaitteet", Parent: &CategoryParent{ID: 91, Label: "Liike-elämä"}},
	{ID: 9102, Label: "Myymäläkalusteet", FullPath: "Liike-elämä > Myymäläkalusteet", Parent: &CategoryParent{ID: 91, Label: "Liike-elämä"}},
	{ID: 9103, Label: "Ravintola- ja kahvilakalusto", FullPath: "Liike-elämä > Ravintola- ja kahvilakalusto", Parent: &CategoryParent{ID: 91, Label: "Liike-elämä"}},

	// Muu (Other) - fallback
	{ID: 76, Label: "Muu", FullPath: "Muu", Parent: nil},
}

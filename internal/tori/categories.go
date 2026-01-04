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
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
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

// IsInitialized returns true if the category cache has been populated
func (s *CategoryService) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.categories) > 0
}

// UpdateFromModel parses the category tree from an AdModel response and updates the cache.
// This extracts real category IDs from the Tori API response.
func (s *CategoryService) UpdateFromModel(model *AdModel) {
	if model == nil {
		return
	}

	// Find the category widget in the model
	var categoryNodes []ModelNode
	found := false
	for _, section := range model.Sections {
		for _, widget := range section.Content.Widgets {
			if widget.ID == "category" {
				categoryNodes = widget.Nodes
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found || len(categoryNodes) == 0 {
		log.Warn().Msg("no category widget found in model response")
		return
	}

	// Flatten the tree into []Category (collecting all nodes, marking persistable ones)
	var newCategories []Category

	var traverse func(nodes []ModelNode, parent *CategoryParent, pathPrefix string)
	traverse = func(nodes []ModelNode, parent *CategoryParent, pathPrefix string) {
		for _, node := range nodes {
			id, err := strconv.Atoi(node.ID)
			if err != nil || id == 0 {
				continue
			}

			fullPath := node.Label
			if pathPrefix != "" {
				fullPath = pathPrefix + " > " + node.Label
			}

			// Build parent structure for children to reference
			currentParent := &CategoryParent{
				ID:     id,
				Label:  node.Label,
				Parent: parent,
			}

			// Add all nodes to the category list (not just persistable ones)
			// This allows hierarchical navigation through parent categories
			newCategories = append(newCategories, Category{
				ID:       id,
				Label:    node.Label,
				FullPath: fullPath,
				Parent:   parent,
			})

			// Recurse into children
			if len(node.Children) > 0 {
				traverse(node.Children, currentParent, fullPath)
			}
		}
	}

	traverse(categoryNodes, nil, "")

	if len(newCategories) == 0 {
		log.Warn().Msg("no categories extracted from model")
		return
	}

	// Update the service safely
	s.mu.Lock()
	defer s.mu.Unlock()

	s.categories = newCategories
	s.Tree = BuildCategoryTree(s.categories)

	log.Info().Int("count", len(newCategories)).Msg("category cache updated from API model")
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

// embeddedCategories is initialized empty and populated dynamically from the Tori API.
// The UpdateFromModel method extracts real category IDs from the /ad/withModel response.
// This replaces the previous hardcoded list which contained fake category IDs.
var embeddedCategories = []Category{}

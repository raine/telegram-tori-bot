package tori

import "fmt"

// GetRequiredSelectAttributes returns the SELECT-type attributes that need user input.
// These are displayed as button options in the Telegram bot.
func GetRequiredSelectAttributes(attrs *AttributesResponse) []Attribute {
	var required []Attribute
	for _, attr := range attrs.Attributes {
		if attr.Type == "SELECT" && len(attr.Options) > 0 {
			required = append(required, attr)
		}
	}
	return required
}

// FindAttributeByName finds an attribute by its name field
func FindAttributeByName(attrs *AttributesResponse, name string) *Attribute {
	for _, attr := range attrs.Attributes {
		if attr.Name == name {
			return &attr
		}
	}
	return nil
}

// FindOptionByLabel finds an option within an attribute by its label
func FindOptionByLabel(attr *Attribute, label string) *AttributeOption {
	for _, opt := range attr.Options {
		if opt.Label == label {
			return &opt
		}
	}
	return nil
}

// FindOptionByID finds an option within an attribute by its ID
func FindOptionByID(attr *Attribute, id int) *AttributeOption {
	for _, opt := range attr.Options {
		if opt.ID == id {
			return &opt
		}
	}
	return nil
}

// GetCategoryPath returns the full category path as a string (e.g., "Parent > Child > Leaf")
func GetCategoryPath(cat CategoryPrediction) string {
	if cat.Parent == nil {
		return cat.Label
	}
	return GetCategoryPath(*cat.Parent) + " > " + cat.Label
}

// GetCategoryPathLastN returns the last N levels of the category path
func GetCategoryPathLastN(cat CategoryPrediction, n int) string {
	parts := getCategoryParts(cat)
	if len(parts) <= n {
		return joinParts(parts)
	}
	return joinParts(parts[len(parts)-n:])
}

func getCategoryParts(cat CategoryPrediction) []string {
	if cat.Parent == nil {
		return []string{cat.Label}
	}
	return append(getCategoryParts(*cat.Parent), cat.Label)
}

func joinParts(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += " > " + parts[i]
	}
	return result
}

// CategoryTaxonomy contains both the parameter name and value for the search API.
type CategoryTaxonomy struct {
	ParamName string // "category", "sub_category", or "product_category"
	Value     string // e.g., "0.5000", "1.5000.5010", "2.5000.5010.8368"
}

// GetCategoryTaxonomy returns the category in API taxonomy format.
// Format: "0.<id>" for top-level, "1.<parent>.<id>" for sub, "2.<gp>.<p>.<id>" for product.
// Returns the appropriate parameter name and value for the category depth.
func GetCategoryTaxonomy(cat CategoryPrediction) CategoryTaxonomy {
	ids := getCategoryIDs(cat)
	depth := len(ids) - 1 // 0 for top-level, 1 for sub, 2 for product
	if depth > 2 {
		depth = 2 // Cap at product category level
	}

	// Determine parameter name based on depth
	var paramName string
	switch depth {
	case 0:
		paramName = "category"
	case 1:
		paramName = "sub_category"
	default:
		paramName = "product_category"
	}

	// Build value string
	value := fmt.Sprintf("%d", depth)
	for _, id := range ids {
		value += fmt.Sprintf(".%d", id)
	}

	return CategoryTaxonomy{ParamName: paramName, Value: value}
}

// FindCategoryByID finds a category by ID in the predictions list
func FindCategoryByID(predictions []CategoryPrediction, categoryID int) *CategoryPrediction {
	for i := range predictions {
		if predictions[i].ID == categoryID {
			return &predictions[i]
		}
	}
	return nil
}

func getCategoryIDs(cat CategoryPrediction) []int {
	if cat.Parent == nil {
		return []int{cat.ID}
	}
	return append(getCategoryIDs(*cat.Parent), cat.ID)
}

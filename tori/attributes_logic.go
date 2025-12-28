package tori

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

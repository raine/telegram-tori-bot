package tori

import (
	"encoding/json"
	"strings"
)

type Categories struct {
	Categories []Category `json:"categories"`
}

type Category struct {
	Categories        []Category `json:"categories"`
	Code              string     `json:"code"`
	Icon              string     `json:"icon"`
	Label             string     `json:"label"`
	MaxImages         int        `json:"max_images"`
	RegionPickerLevel string     `json:"region_picker_level"`
}

func padRight(str string, item string, count int) string {
	return str + strings.Repeat(item, count)
}

func (c *Categories) GetCategoryLabel(code string) string {
	var recur func(depth int, categories []Category) string
	recur = func(depth int, categories []Category) string {
		truncatedCode := padRight(code[:(depth+2)], "0", 2-depth)
		for _, c := range categories {
			if c.Code == code {
				return c.Label
			} else if c.Code == truncatedCode && len(c.Categories) > 0 {
				return recur(depth+1, c.Categories)
			}
		}
		return ""
	}

	return recur(0, c.Categories)
}

func parseCategories(jsonData []byte) (Categories, error) {
	var categories Categories
	err := json.Unmarshal(jsonData, &categories)
	return categories, err
}

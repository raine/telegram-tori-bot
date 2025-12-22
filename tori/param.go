package tori

import "fmt"

// GetLabelForField returns the human-readable label for a given field name
// from the param map.
func GetLabelForField(paramMap ParamMap, field string) (string, error) {
	param := paramMap[field]
	switch {
	case param.SingleSelection != nil:
		return (*param.SingleSelection).Label, nil
	case param.MultiSelection != nil:
		return param.MultiSelection.ValuesList[0].Label, nil
	case param.Text != nil:
		return (*param.Text).Label, nil
	default:
		return "", fmt.Errorf("could not find param for field '%s'", field)
	}
}

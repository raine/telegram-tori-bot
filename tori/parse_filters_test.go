package tori

import (
	"os"
	"testing"
)

func TestParseNewadFilters(t *testing.T) {
	filtersSectionNewadJson, err := os.ReadFile("testdata/v1_2_public_filters_section_newad.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseNewadFilters(filtersSectionNewadJson)
	if err != nil {
		t.Fatal(err)
	}
}

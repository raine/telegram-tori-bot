package tori

import (
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCategoryLabel(t *testing.T) {
	b, err := ioutil.ReadFile("testdata/v1_2_public_categories_insert.json")
	if err != nil {
		t.Fatal(err)
	}
	categories, err := parseCategories(b)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "ELEKTRONIIKKA", categories.GetCategoryLabel("5000"))
	assert.Equal(t, "Viihde-elektroniikka", categories.GetCategoryLabel("5020"))
	assert.Equal(t, "Televisiot", categories.GetCategoryLabel("5022"))
	assert.Equal(t, "", categories.GetCategoryLabel("132041980"))
}

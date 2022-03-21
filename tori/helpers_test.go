package tori

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAccountLocationListToListingLocation(t *testing.T) {
	location := []Location{
		{
			Code:  "18",
			Key:   "region",
			Label: "Uusimaa",
			Locations: []Location{
				{
					Code:  "313",
					Key:   "area",
					Label: "Helsinki",
					Locations: []Location{
						{
							Code:  "00320",
							Key:   "zipcode",
							Label: "EtelÃ¤-Haaga",
						},
					},
				},
			},
		},
	}

	got := AccountLocationListToListingLocation(location)
	want := ListingLocation{Region: "18", Area: "313", Zipcode: "00320"}

	assert.Equal(t, want, got)
}

func formatJson(b []byte) string {
	var out bytes.Buffer
	err := json.Indent(&out, b, "", "  ")
	if err != nil {
		panic(err)
	}
	return out.String()
}

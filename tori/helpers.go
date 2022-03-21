package tori

import (
	"fmt"
	"regexp"
)

func AccountLocationListToListingLocation(location []Location) ListingLocation {
	region := location[0]
	area := region.Locations[0]
	zipcode := area.Locations[0]

	return ListingLocation{
		Region:  region.Code,
		Area:    area.Code,
		Zipcode: zipcode.Code,
	}
}

func ParseAccountIdNumberFromPath(str string) string {
	re := regexp.MustCompile(`/private/accounts/(\d+)`)
	m := re.FindStringSubmatch(str)
	fmt.Printf("m = %+v\n", m)
	if m == nil {
		return ""
	} else {
		return m[1]
	}
}

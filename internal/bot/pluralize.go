package bot

import "fmt"

func pluralize(singular string, plural string, count int) string {
	var s string
	if count == 1 {
		s = singular
	} else {
		s = plural
	}
	return fmt.Sprintf("%d %s", count, s)
}

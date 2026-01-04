package bot

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
)

func formatReplyText(text string, a ...any) string {
	return fmt.Sprintf(strings.TrimSpace(dedent.Dedent(text)), a...)
}

func parseCommand(s string) (string, []string) {
	parts := strings.Split(s, " ")
	return parts[0], parts[1:]
}

// isValidPostalCode validates Finnish postal codes (5 digits).
func isValidPostalCode(code string) bool {
	if len(code) != 5 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

package util

import (
	"strconv"
	"strings"
)

// ParsePortToInt parses various port formats (int, float64, string, ranges like "10808-10900", or comma-separated like "10808,10900") into int.
func ParsePortToInt(val interface{}) int {
	switch v := val.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case string:
		s := strings.TrimSpace(v)
		if idx := strings.IndexAny(s, "-,"); idx != -1 {
			s = strings.TrimSpace(s[:idx])
		}
		if pi, err := strconv.Atoi(s); err == nil && pi > 0 {
			return pi
		}
	}
	return 0
}

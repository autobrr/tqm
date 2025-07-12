package evaluate

import (
	"slices"
	"strings"
)

func StringSliceContains(slice []string, contains string, caseInsensitive bool) bool {
	return slices.ContainsFunc(slice, func(s string) bool {
		if caseInsensitive {
			return strings.EqualFold(s, contains)
		}

		return s == contains
	})
}

package stringutils

import (
	"strconv"
	"strings"
)

func Atof64(val string, defaultVal float64) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil {
		return defaultVal
	}
	return n
}

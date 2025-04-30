package regex

import (
	"github.com/dlclark/regexp2"
)

func Compile(pattern string) (*Pattern, error) {
	re, err := regexp2.Compile(pattern, regexp2.None)
	if err != nil {
		return nil, err
	}

	return &Pattern{
		Expression: re,
	}, nil
}

func ValidatePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := Compile(pattern); err != nil {
			return err
		}
	}
	return nil
}

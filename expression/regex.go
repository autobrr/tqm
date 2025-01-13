package expression

import (
	"fmt"
	"strings"

	"github.com/autobrr/tqm/config"
	"github.com/dlclark/regexp2"
)

var (
	// Matches: RegexMatch("pattern"), RegexMatchAny("pattern1, pattern2"), RegexMatchAll("pattern1, pattern2")
	regexFuncPattern = regexp2.MustCompile(`RegexMatch(?:Any|All)?\("([^"\\]*(?:\\.[^"\\]*)*)"\)`, regexp2.None)
)

// getAllPatternsFromFilter extracts all regex patterns from filter expressions
func getAllPatternsFromFilter(filter *config.FilterConfiguration) ([]string, error) {
	var patterns []string

	// helper to extract patterns from any regex function
	extractPatterns := func(update string) error {
		match, err := regexFuncPattern.FindStringMatch(update)
		if err != nil {
			return fmt.Errorf("invalid regex function: %w", err)
		}

		if match == nil {
			return nil
		}

		// group 1 contains the pattern(s)
		patternStr := match.GroupByNumber(1).String()

		// handle comma-separated patterns for RegexMatchAny/All
		for _, p := range strings.Split(patternStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				patterns = append(patterns, p)
			}
		}

		return nil
	}

	// collect from label updates
	for _, label := range filter.Label {
		for _, update := range label.Update {
			if err := extractPatterns(update); err != nil {
				return nil, fmt.Errorf("in label %q: %w", label.Name, err)
			}
		}
	}

	// collect from tag updates
	for _, tag := range filter.Tag {
		for _, update := range tag.Update {
			if err := extractPatterns(update); err != nil {
				return nil, fmt.Errorf("in tag %q: %w", tag.Name, err)
			}
		}
	}

	return patterns, nil
}

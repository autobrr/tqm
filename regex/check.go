package regex

func Check(text string, pattern *Pattern) (bool, error) {
	match, err := pattern.Expression.MatchString(text)
	if err != nil {
		return false, err
	}
	return match, nil
}

// CheckAny returns true if any pattern matches
func CheckAny(text string, patterns []*Pattern) (bool, error) {
	for _, pattern := range patterns {
		match, err := Check(text, pattern)
		if err != nil {
			return false, err
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}

// CheckAll returns true if all patterns match
func CheckAll(text string, patterns []*Pattern) (bool, error) {
	for _, pattern := range patterns {
		match, err := Check(text, pattern)
		if err != nil {
			return false, err
		}
		if !match {
			return false, nil
		}
	}
	return true, nil
}

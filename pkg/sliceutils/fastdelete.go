package sliceutils

func FastDelete[S ~[]E, E any](s S, i int) S {
	_ = s[i] // bounds check

	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

package sliceutils

func IndexOfString(s []string, e string) int {
	for i, v := range s {
		if v == e {
			return i
		}
	}

	return -1
}

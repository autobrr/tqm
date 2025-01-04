package httputils

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

func Join(base string, paths ...string) string {
	// credits: https://stackoverflow.com/a/57220413
	p := path.Join(paths...)
	return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), strings.TrimLeft(p, "/"))
}

func WithQuery(base string, q url.Values) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("url parse: %w", err)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

package httputils

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/ratelimit"

	"github.com/autobrr/tqm/pkg/runtime"
)

func NewRetryableHttpClient(timeout time.Duration, rl ratelimit.Limiter) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 1
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 10 * time.Second
	retryClient.RequestLogHook = func(l retryablehttp.Logger, request *http.Request, i int) {
		// set user-agent
		if request != nil {
			request.Header.Set("User-Agent", "tqm/"+runtime.Version)
		}

		// rate limit
		if rl != nil {
			rl.Take()
		}
	}
	retryClient.HTTPClient.Timeout = timeout
	retryClient.Logger = nil
	return retryClient.StandardClient()
}

func URLWithQuery(base string, q url.Values) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("url parse: %w", err)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func MakeAPIRequest(ctx context.Context, client *http.Client, method string, requestURL string, body io.Reader, headers map[string]string, toType any) error {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	buf := bufio.NewReader(res.Body)

	if err = json.NewDecoder(buf).Decode(toType); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

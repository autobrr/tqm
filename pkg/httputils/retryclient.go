package httputils

import (
	"net/http"
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

package httputils

import (
	"net/http"
	"time"

	"github.com/autobrr/tqm/runtime"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
	"go.uber.org/ratelimit"
)

func NewRetryableHttpClient(timeout time.Duration, rl ratelimit.Limiter, log *logrus.Entry) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 10
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

		// log
		if log != nil && request != nil && request.URL != nil {
			switch i {
			case 0:
				// first
				log.Tracef("Sending request to %s", request.URL.String())
			default:
				// retry
				log.Debugf("Retrying failed request to %s (attempt: %d)", request.URL.String(), i)
			}
		}
	}
	retryClient.HTTPClient.Timeout = timeout
	retryClient.Logger = nil
	return retryClient.StandardClient()
}

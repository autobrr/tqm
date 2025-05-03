package client

import (
	"net"
	"net/url"
	"strings"

	"github.com/bobesa/go-domain-util/domainutil"
	"github.com/sirupsen/logrus"
)

func parseTrackerDomain(trackerHost string) string {
	// return empty host
	if trackerHost == "" {
		return trackerHost
	}

	// parse url components
	u, err := url.Parse(trackerHost)
	if err != nil {
		logrus.WithError(err).Warnf("Failed parsing tracker host: %q", trackerHost)
		return trackerHost
	}

	// parse host
	host := u.Host
	if strings.Contains(host, ":") {
		// remove port
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}

	// remove subdomain
	if domain := domainutil.Domain(host); domain != "" {
		return domain
	}

	return host
}

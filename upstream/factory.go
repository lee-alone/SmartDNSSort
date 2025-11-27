package upstream

import (
	"fmt"
	"net/url"
	"strings"

	"smartdnssort/upstream/bootstrap"
	"smartdnssort/upstream/transport"
)

func NewUpstream(serverUrl string, boot *bootstrap.Resolver) (Upstream, error) {
	// Check if it has scheme
	if !strings.Contains(serverUrl, "://") {
		// Default to UDP if no scheme, assuming it's just IP:Port
		return transport.NewUDP(serverUrl), nil
	}

	u, err := url.Parse(serverUrl)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "udp":
		return transport.NewUDP(u.Host), nil
	case "tcp":
		return transport.NewTCP(u.Host), nil
	case "tls", "dot":
		return transport.NewDoT(serverUrl), nil
	case "https", "doh":
		return transport.NewDoH(serverUrl, boot)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", u.Scheme)
	}
}

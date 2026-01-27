package upstream

import (
	"fmt"
	"net/url"
	"strings"

	"smartdnssort/config"
	"smartdnssort/upstream/bootstrap"
	"smartdnssort/upstream/transport"
)

func NewUpstream(serverUrl string, boot *bootstrap.Resolver, upstreamCfg *config.UpstreamConfig) (Upstream, error) {
	// Check if it has scheme
	if !strings.Contains(serverUrl, "://") {
		// Default to UDP if no scheme, assuming it's just IP:Port
		return transport.NewUDP(serverUrl, upstreamCfg.MaxConnections), nil
	}

	u, err := url.Parse(serverUrl)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "udp":
		return transport.NewUDP(u.Host, upstreamCfg.MaxConnections), nil
	case "tcp":
		return transport.NewTCP(u.Host, upstreamCfg.MaxConnections), nil
	case "tls", "dot":
		return transport.NewDoT(serverUrl), nil // DoT/DoH doesn't use generic connection pool
	case "https", "doh":
		return transport.NewDoH(serverUrl, boot) // DoT/DoH doesn't use generic connection pool
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", u.Scheme)
	}
}

package proxytraffic

import (
	"net"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

// Key identifies a managed route consistently across HTTP, SOCKS and TLS clients.
func Key(raw string) string {
	_, parsed, err := proxyurl.Parse(raw)
	if err != nil || parsed == nil {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "1080"
		}
	}
	parsed.Host = net.JoinHostPort(host, port)
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.Opaque = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
}

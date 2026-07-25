package videocollector

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrUnsafeMediaURL = errors.New("media URL must resolve to a public HTTP or HTTPS address")

type IPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

func ValidatePublicMediaURL(ctx context.Context, value string, resolver IPResolver) (*url.URL, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if len(value) == 0 || len(value) > 2048 {
		return nil, ErrUnsafeMediaURL
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" {
		return nil, ErrUnsafeMediaURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrUnsafeMediaURL
	}
	if parsed.User != nil {
		return nil, ErrUnsafeMediaURL
	}

	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, ErrUnsafeMediaURL
	}
	if literalIP := net.ParseIP(host); literalIP != nil {
		if !isPublicIP(literalIP) {
			return nil, ErrUnsafeMediaURL
		}
		return parsed, nil
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrUnsafeMediaURL
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return nil, ErrUnsafeMediaURL
		}
	}
	return parsed, nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast()
}

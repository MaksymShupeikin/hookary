package shared

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

const maxPublicURLLength = 2048

var blockedAddressRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// ValidatePublicURL accepts only a web URL whose current DNS answers are all
// public. The ingest worker repeats this check and sends yt-dlp through its own
// validating proxy, because a check made only when the API receives the URL
// would still be vulnerable to redirects and DNS rebinding.
func ValidatePublicURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := parsePublicURL(raw)
	if err != nil {
		return nil, err
	}
	if _, err := ResolvePublicHost(ctx, parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func parsePublicURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxPublicURLLength {
		return nil, errors.New("URL is empty or too long")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("URL is not valid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("only http and https URLs are allowed")
	}
	if parsed.User != nil {
		return nil, errors.New("URLs containing credentials are not allowed")
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".localhost") {
		return nil, errors.New("local addresses are not allowed")
	}
	port := parsed.Port()
	if port != "" && port != "80" && port != "443" {
		return nil, errors.New("only web ports 80 and 443 are allowed")
	}
	return parsed, nil
}

// ResolvePublicHost returns the exact public addresses a caller may dial.
// Returning addresses, rather than merely a boolean, lets the ingest proxy
// connect to a checked IP instead of resolving the hostname a second time.
func ResolvePublicHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return resolvePublicHost(ctx, host, net.DefaultResolver.LookupNetIP)
}

func resolvePublicHost(
	ctx context.Context,
	host string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if !isPublicAddress(literal) {
			return nil, errors.New("private or special-use addresses are not allowed")
		}
		return []netip.Addr{literal}, nil
	}

	addresses, err := lookup(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve URL host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("URL host has no addresses")
	}
	public := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !isPublicAddress(address) {
			return nil, errors.New("URL host resolves to a private or special-use address")
		}
		public = append(public, address)
	}
	return public, nil
}

func isPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() ||
		address.IsPrivate() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	for _, blocked := range blockedAddressRanges {
		if blocked.Contains(address) {
			return false
		}
	}
	return true
}

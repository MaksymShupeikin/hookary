package shared

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestParsePublicURLRejectsUnsafeForms(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"http://localhost/video",
		"https://example.com:8443/video",
		"https://user:secret@example.com/video",
		"not a URL",
	} {
		if _, err := parsePublicURL(raw); err == nil {
			t.Errorf("parsePublicURL(%q) succeeded", raw)
		}
	}
}

func TestResolvePublicHostRejectsAnyPrivateDNSAnswer(t *testing.T) {
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("142.250.74.206"),
			netip.MustParseAddr("169.254.169.254"),
		}, nil
	}
	if _, err := resolvePublicHost(context.Background(), "example.com", lookup); err == nil {
		t.Fatal("mixed public/private DNS response was accepted")
	}
}

func TestResolvePublicHostReturnsCheckedPublicAddresses(t *testing.T) {
	want := netip.MustParseAddr("142.250.74.206")
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{want}, nil
	}
	got, err := resolvePublicHost(context.Background(), "example.com", lookup)
	if err != nil {
		t.Fatalf("resolvePublicHost: %v", err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("addresses = %v, want [%v]", got, want)
	}
}

func TestResolvePublicHostPropagatesLookupFailure(t *testing.T) {
	want := errors.New("resolver unavailable")
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, want
	}
	if _, err := resolvePublicHost(context.Background(), "example.com", lookup); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped %v", err, want)
	}
}

func TestPublicAddressClassification(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254",
		"192.0.2.1", "198.18.0.1", "::1", "fc00::1", "2001:db8::1",
	} {
		if isPublicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("%s classified as public", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("%s classified as private", raw)
		}
	}
}

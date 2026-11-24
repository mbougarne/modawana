// Package publichttp provides the restricted transport for external integrations.
package publichttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"
)

var errUnsafeDestination = errors.New("public HTTPS destination rejected")
var domainLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

var specialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}
var globalIPv6 = netip.MustParsePrefix("2000::/3")

func IsPublicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Is6() && !globalIPv6.Contains(ip) {
		return false
	}
	return !slices.ContainsFunc(specialNetworks, func(network netip.Prefix) bool { return network.Contains(ip) })
}

func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 16 << 10,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil || port != "443" || !ValidHost(host) {
					return nil, errUnsafeDestination
				}
				resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host+".")
				if err != nil || len(resolved) == 0 || len(resolved) > 16 ||
					slices.ContainsFunc(resolved, func(ip netip.Addr) bool { return !IsPublicAddress(ip) }) {
					return nil, errUnsafeDestination
				}
				dialer := &net.Dialer{}
				for _, ip := range resolved {
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
				}
				return nil, errors.New("public HTTPS connection failed")
			},
		},
	}
}

func ValidHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !domainLabel.MatchString(label) {
			return false
		}
	}
	return true
}

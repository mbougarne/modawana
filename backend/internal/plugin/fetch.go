package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

const fetchInputLimit = 64 << 10
const fetchBodyLimit = 64 << 10

type fetchRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type fetchResponse struct {
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

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

func publicDialAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Is6() && !globalIPv6.Contains(ip) {
		return false
	}
	return !slices.ContainsFunc(specialNetworks, func(network netip.Prefix) bool { return network.Contains(ip) })
}

func newFetchClient() *http.Client {
	return &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 16 << 10,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil || port != "443" || !validHTTPSHost(host) {
					return nil, ErrStorageInvalid
				}
				resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host+".")
				if err != nil || len(resolved) == 0 || len(resolved) > 16 ||
					slices.ContainsFunc(resolved, func(ip netip.Addr) bool { return !publicDialAddress(ip) }) {
					return nil, ErrStorageInvalid
				}
				dialer := &net.Dialer{}
				for _, ip := range resolved {
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
				}
				return nil, errors.New("plugin HTTPS connection failed")
			},
		},
	}
}

func pluginHTTPRequest(ctx context.Context, hosts []string, raw []byte) (*http.Request, error) {
	var input fetchRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
		return nil, ErrStorageInvalid
	}
	u, err := url.Parse(input.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" ||
		u.Host != u.Hostname() || !slices.Contains(hosts, u.Host) || len(input.URL) > 2048 {
		return nil, ErrStorageInvalid
	}
	switch input.Method {
	case "GET", "HEAD":
		if len(input.Body) != 0 {
			return nil, ErrStorageInvalid
		}
	case "POST", "PUT", "PATCH", "DELETE":
	default:
		return nil, ErrStorageInvalid
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, u.String(), bytes.NewReader(input.Body))
	if err != nil {
		return nil, ErrStorageInvalid
	}
	for key, value := range input.Headers {
		name := http.CanonicalHeaderKey(key)
		switch name {
		case "Accept", "Content-Type", "Authorization", "X-Api-Key", "Idempotency-Key":
		default:
			return nil, ErrStorageInvalid
		}
		if !boundedText(value, 4096) || req.Header.Values(name) != nil {
			return nil, ErrStorageInvalid
		}
		req.Header.Set(name, value)
	}
	return req, nil
}

func (r *Runtime) fetch(ctx context.Context, inv *invocation, raw []byte) ([]byte, error) {
	if !inv.externalAccess {
		return nil, ErrStorageInvalid
	}
	req, err := pluginHTTPRequest(ctx, inv.manifest.HTTPSHosts, raw)
	if err != nil {
		return nil, err
	}
	res, err := r.fetchClient.Do(req)
	if err != nil {
		return nil, errors.New("plugin HTTPS request failed")
	}
	defer res.Body.Close()
	if strings.TrimSpace(res.Header.Get("Content-Encoding")) != "" {
		return nil, ErrStorageInvalid
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, fetchBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > fetchBodyLimit {
		return nil, ErrQuota
	}
	return json.Marshal(fetchResponse{Status: res.StatusCode, Body: body})
}

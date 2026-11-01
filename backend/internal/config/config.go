// Package config validates operator configuration. Never serialize Config to clients or logs.
package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	LearningIdleDays      int
	LearningMaxIdentities int
	AuthKeyFile           string
	Environment           string
	BaseURL               string
	DataRoot              string
	FrontendDist          string
	ListenAddr            string
	RPID                  string
	WebAuthnOrigins       []string
	TrustedProxies        []netip.Prefix
	// ForceSPA is the operator rollback switch for ADR 0002: when set, every
	// public page serves the SPA shell even if a server-rendered template exists.
	ForceSPA bool
}

type Public struct {
	APIVersion    string   `json:"api_version"`
	DefaultLocale string   `json:"default_locale"`
	Locales       []string `json:"locales"`
}

func (c Config) Public() Public { return Public{"v1", "en", []string{"en", "ar", "fr"}} }

// Load accepts an environment lookup so tests never depend on operator secrets.
func Load(get func(string) string, cwd string) (Config, error) {
	c := Config{AuthKeyFile: get("BLOG_AUTH_KEY_FILE"), Environment: get("BLOG_ENV"), BaseURL: get("BLOG_BASE_URL"), DataRoot: get("BLOG_DATA_ROOT"), FrontendDist: get("BLOG_FRONTEND_DIST"), ListenAddr: get("BLOG_LISTEN_ADDR"), RPID: get("BLOG_RP_ID")}
	c.LearningIdleDays = 180
	c.LearningMaxIdentities = 10000
	for _, setting := range []struct {
		name   string
		target *int
		max    int
	}{{"BLOG_LEARNING_IDLE_DAYS", &c.LearningIdleDays, 3650}, {"BLOG_LEARNING_MAX_IDENTITIES", &c.LearningMaxIdentities, 1000000}} {
		if raw := get(setting.name); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > setting.max {
				return c, invalid(setting.name)
			}
			*setting.target = n
		}
	}
	switch get("BLOG_FORCE_SPA") {
	case "", "0":
	case "1":
		c.ForceSPA = true
	default:
		return c, invalid("BLOG_FORCE_SPA")
	}
	if c.Environment == "" {
		c.Environment = "production"
	}
	if c.Environment != "production" && c.Environment != "development" {
		return c, invalid("BLOG_ENV")
	}
	u, err := origin(c.BaseURL, c.Environment)
	if err != nil {
		return c, invalid("BLOG_BASE_URL")
	}
	if c.Environment == "production" && strings.HasSuffix(u.Hostname(), ".invalid") {
		return c, invalid("BLOG_BASE_URL")
	}
	c.BaseURL = u.String()
	if c.RPID == "" || c.RPID != u.Hostname() {
		return c, invalid("BLOG_RP_ID")
	}
	origins := get("BLOG_WEBAUTHN_ORIGINS")
	if origins == "" {
		return c, invalid("BLOG_WEBAUTHN_ORIGINS")
	}
	seen := map[string]bool{}
	for _, v := range strings.Split(origins, ",") {
		o, e := origin(strings.TrimSpace(v), c.Environment)
		if e != nil || o.String() != c.BaseURL {
			return c, invalid("BLOG_WEBAUTHN_ORIGINS")
		}
		if !seen[o.String()] {
			c.WebAuthnOrigins = append(c.WebAuthnOrigins, o.String())
			seen[o.String()] = true
		}
	}
	if c.ListenAddr == "" {
		c.ListenAddr = "127.0.0.1:8080"
	}
	host, port, e := net.SplitHostPort(c.ListenAddr)
	n, pe := strconv.Atoi(port)
	_, ie := netip.ParseAddr(host)
	if e != nil || pe != nil || ie != nil || n < 1 || n > 65535 {
		return c, invalid("BLOG_LISTEN_ADDR")
	}
	if raw := get("BLOG_TRUSTED_PROXIES"); raw != "" {
		for _, v := range strings.Split(raw, ",") {
			p, e := netip.ParsePrefix(strings.TrimSpace(v))
			if e != nil {
				return c, invalid("BLOG_TRUSTED_PROXIES")
			}
			c.TrustedProxies = append(c.TrustedProxies, p.Masked())
		}
	}
	if c.FrontendDist == "" {
		c.FrontendDist = filepath.Join(cwd, "../frontend/dist")
	}
	c.FrontendDist, e = filepath.Abs(c.FrontendDist)
	if e != nil {
		return c, invalid("BLOG_FRONTEND_DIST")
	}
	if c.DataRoot == "" || !filepath.IsAbs(c.DataRoot) {
		return c, invalid("BLOG_DATA_ROOT")
	}
	c.DataRoot, e = canonical(c.DataRoot)
	if e != nil {
		return c, invalid("BLOG_DATA_ROOT")
	}
	// Exclude the checkout, the current release, and its frontend directory, even via symlinks.
	roots := []string{cwd, filepath.Dir(c.FrontendDist), c.FrontendDist}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			roots = append(roots, dir)
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for _, root := range roots {
		r, e := canonical(root)
		if e != nil {
			return c, invalid("BLOG_DATA_ROOT")
		}
		if within(r, c.DataRoot) || within(c.DataRoot, r) {
			return c, invalid("BLOG_DATA_ROOT")
		}
	}
	return c, nil
}
func invalid(name string) error { return fmt.Errorf("invalid %s", name) }

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func origin(raw, env string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery || strings.ContainsAny(raw, "\\ \t\r\n") {
		return nil, fmt.Errorf("invalid origin")
	}
	if u.Scheme != "https" {
		ip, e := netip.ParseAddr(u.Hostname())
		if env != "development" || u.Scheme != "http" || (u.Hostname() != "localhost" && (e != nil || !ip.IsLoopback())) {
			return nil, fmt.Errorf("insecure origin")
		}
	}
	if strings.Contains(raw, "#") || strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid host")
	}
	if _, e := netip.ParseAddr(u.Hostname()); e != nil {
		if len(u.Hostname()) > 253 {
			return nil, fmt.Errorf("invalid host")
		}
		for _, label := range strings.Split(u.Hostname(), ".") {
			if !dnsLabel.MatchString(label) {
				return nil, fmt.Errorf("invalid host")
			}
		}
	}
	if u.Hostname() != strings.ToLower(u.Hostname()) || strings.HasSuffix(u.Hostname(), ".") {
		return nil, fmt.Errorf("noncanonical host")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid port")
		}
	}
	return u, nil
}
func within(parent, child string) bool {
	rel, e := filepath.Rel(parent, child)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Resolve the existing ancestor, preserving missing suffixes. Do not create data during validation.
func canonical(path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	var suffix []string
	for {
		resolved, e := filepath.EvalSymlinks(p)
		if e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", e
		}
		suffix = append(suffix, filepath.Base(p))
		p = parent
	}
}

// ClientIP ignores all forwarding headers unless the direct peer is explicitly trusted.
// Walk right to left; a client-supplied left prefix cannot override the nearest untrusted hop.
func (c Config) ClientIP(remote, forwardedFor string) netip.Addr {
	host, _, e := net.SplitHostPort(remote)
	if e != nil {
		return netip.Addr{}
	}
	peer, e := netip.ParseAddr(host)
	if e != nil {
		return netip.Addr{}
	}
	peer = peer.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, p := range c.TrustedProxies {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) || forwardedFor == "" || len(forwardedFor) > 2048 {
		return peer
	}
	parts := strings.Split(forwardedFor, ",")
	if len(parts) > 16 {
		return peer
	}
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		ip, e := netip.ParseAddr(strings.TrimSpace(part))
		if e != nil {
			return peer
		}
		chain = append(chain, ip.Unmap())
	}
	current := peer
	for i := len(chain) - 1; i >= 0 && trusted(current); i-- {
		current = chain[i]
	}
	return current
}

// ThrottleIP aggregates IPv6 privacy addresses without changing ClientIP's
// accurate proxy attribution. IPv4 addresses retain their individual bucket.
func (c Config) ThrottleIP(remote, forwardedFor string) string {
	ip := c.ClientIP(remote, forwardedFor)
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

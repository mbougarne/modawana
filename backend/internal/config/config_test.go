package config

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (map[string]string, string) {
	t.Helper()
	root := t.TempDir()
	cwd := filepath.Join(root, "repo", "backend")
	if err := os.MkdirAll(filepath.Join(root, "repo", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"BLOG_ENV": "production", "BLOG_BASE_URL": "https://blog.example", "BLOG_RP_ID": "blog.example", "BLOG_WEBAUTHN_ORIGINS": "https://blog.example", "BLOG_DATA_ROOT": filepath.Join(root, "data")}, cwd
}
func TestConfiguration(t *testing.T) {
	env, cwd := fixture(t)
	cfg, err := Load(func(k string) string { return env[k] }, cwd)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cfg.Public())
	for _, private := range []string{"blog.example", cfg.DataRoot, "RP", "proxy", "password", "secret"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("public configuration contains private value")
		}
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatal(cfg.ListenAddr)
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"BLOG_ENV", "test"}, {"BLOG_BASE_URL", "http://blog.example"}, {"BLOG_BASE_URL", "https://user:secret@blog.example"}, {"BLOG_BASE_URL", "https://blog.example/path"}, {"BLOG_BASE_URL", "https://blog.example?"}, {"BLOG_BASE_URL", "https://blog.example:99999"}, {"BLOG_RP_ID", "example"}, {"BLOG_WEBAUTHN_ORIGINS", "https://evil.example"}, {"BLOG_WEBAUTHN_ORIGINS", ""}, {"BLOG_TRUSTED_PROXIES", "localhost"}, {"BLOG_LISTEN_ADDR", ":8080"}, {"BLOG_LISTEN_ADDR", "127.0.0.1:0"}, {"BLOG_DATA_ROOT", "relative"}} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			env, cwd := fixture(t)
			env[tc.name] = tc.value
			_, err := Load(func(k string) string { return env[k] }, cwd)
			if err == nil {
				t.Fatal("accepted invalid configuration")
			}
			if strings.Contains(err.Error(), tc.value) && len(tc.value) > 15 {
				t.Fatal("error leaked value")
			}
		})
	}
}
func TestDataCannotLiveInsideCheckoutOrSymlink(t *testing.T) {
	env, cwd := fixture(t)
	repo := filepath.Dir(cwd)
	for _, data := range []string{filepath.Join(repo, "data"), filepath.Dir(repo), filepath.Join(cwd, "data")} {
		env["BLOG_DATA_ROOT"] = data
		if _, err := Load(func(k string) string { return env[k] }, cwd); err == nil {
			t.Fatal("accepted code/data overlap")
		}
	}
	link := filepath.Join(filepath.Dir(repo), "alias")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	env["BLOG_DATA_ROOT"] = filepath.Join(link, "nested", "data")
	if _, err := Load(func(k string) string { return env[k] }, cwd); err == nil {
		t.Fatal("accepted symlink overlap")
	}
}
func TestDevelopmentOrigin(t *testing.T) {
	env, cwd := fixture(t)
	env["BLOG_ENV"] = "development"
	env["BLOG_BASE_URL"] = "http://127.0.0.1:3000"
	env["BLOG_RP_ID"] = "127.0.0.1"
	env["BLOG_WEBAUTHN_ORIGINS"] = env["BLOG_BASE_URL"]
	if _, err := Load(func(k string) string { return env[k] }, cwd); err != nil {
		t.Fatal(err)
	}
}
func TestProxySpoofing(t *testing.T) {
	c := Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	for _, tc := range []struct{ peer, header, want string }{
		{"192.0.2.2:9", "203.0.113.1", "192.0.2.2"}, {"10.0.0.2:9", "203.0.113.1, 192.0.2.2", "192.0.2.2"}, {"10.0.0.2:9", "192.0.2.2, 10.0.0.3", "192.0.2.2"}, {"10.0.0.2:9", "malformed, 192.0.2.2", "10.0.0.2"}, {"10.0.0.2:9", strings.Repeat("1.1.1.1,", 20), "10.0.0.2"}} {
		if got := c.ClientIP(tc.peer, tc.header).String(); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}

func TestLearningRetentionConfiguration(t *testing.T) {
	env, cwd := fixture(t)
	cfg, e := Load(func(k string) string { return env[k] }, cwd)
	if e != nil || cfg.LearningIdleDays != 180 || cfg.LearningMaxIdentities != 10000 {
		t.Fatal(cfg.LearningIdleDays, cfg.LearningMaxIdentities, e)
	}
	env["BLOG_LEARNING_IDLE_DAYS"] = "90"
	env["BLOG_LEARNING_MAX_IDENTITIES"] = "500"
	cfg, e = Load(func(k string) string { return env[k] }, cwd)
	if e != nil || cfg.LearningIdleDays != 90 || cfg.LearningMaxIdentities != 500 {
		t.Fatal(e)
	}
	for _, bad := range []string{"0", "-1", "3651", "1.5", "days"} {
		env["BLOG_LEARNING_IDLE_DAYS"] = bad
		if _, e = Load(func(k string) string { return env[k] }, cwd); e == nil {
			t.Fatal(bad)
		}
	}
}

func TestThrottleIPv6NetworkAndTrustedProxy(t *testing.T) {
	c := Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	for _, tc := range []struct{ peer, forwarded, want string }{
		{"[2001:db8:1:2::1]:443", "", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:ffff::a]:443", "", "2001:db8:1:2::/64"},
		{"[2001:db8:1:3::1]:443", "", "2001:db8:1:3::/64"},
		{"192.0.2.1:443", "203.0.113.1", "192.0.2.1"},
		{"10.0.0.1:443", "2001:db8:1:2::5", "2001:db8:1:2::/64"},
	} {
		if got := c.ThrottleIP(tc.peer, tc.forwarded); got != tc.want {
			t.Fatalf("%s: %s", tc.peer, got)
		}
	}
}

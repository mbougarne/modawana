package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/analytics"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/forms"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestAnalyticsCountsCanonicalTrafficAndAcceptedLeadOnce(t *testing.T) {
	ctx := t.Context()
	contents, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: contents.DB, Files: contents.Files}
	a, err := analytics.Open(ctx, contents.DB)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	login := auth.New(contents.DB, cfg)
	formsService := &forms.Service{DB: contents.DB}
	host := extension.NewHost(nil)
	d := &Domain{Analytics: a, Content: contents, Auth: login, Forms: formsService, Extensions: host}
	for _, ext := range d.Builtins() {
		if err := host.Install(ext); err != nil {
			t.Fatal(err)
		}
	}
	h := New(Options{Assets: asset, Config: cfg, SiteConfig: settings, Auth: login, Domain: d, Analytics: a, Extensions: host})
	request := func(method, path, origin, choice, signal string, body url.Values, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, strings.NewReader(body.Encode()))
		if body != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", origin)
		}
		if choice != "" {
			r.AddCookie(&http.Cookie{Name: analyticsCookie, Value: choice})
		}
		if signal != "" {
			r.Header.Set(signal, "1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	form, err := formsService.Save(ctx, forms.Form{Key: "contact", Enabled: true, Definition: forms.Definition{Title: "Contact", RetentionDays: 30,
		Fields: []forms.Field{{Key: "email", Label: "Email", Kind: "email", Required: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	draft := content.DraftInput{ExpectedVersion: &zero, Kind: "page", SourceLocale: "en", Slug: "contact", Title: "Contact",
		PageTemplate: "landing", BodyFormat: content.BlockBodyFormat,
		PageBlocks: json.RawMessage(`{"version":1,"blocks":[{"id":"contact","type":"core.form","version":1,"properties":{"form_key":"contact","form_version":1}}]}`)}
	saved, err := contents.Save(ctx, owner, "contact", "en", "", draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = contents.ChangePublication(ctx, owner, "contact", "en", "", "publish",
		content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID}); err != nil {
		t.Fatal(err)
	}
	const page = "/en/pages/contact"
	if body := request("GET", page, "", analytics.Allow, "", nil, 200).Body.String(); strings.Contains(body, analyticsChoice) {
		t.Fatal("disabled analytics exposed consent controls")
	}
	request("GET", analyticsAPI, "", "", "", nil, 401)
	in := a.Settings()
	in.Enabled = true
	in.Exclusions = []string{"/en/about"}
	if _, err = a.Update(ctx, in); err != nil {
		t.Fatal(err)
	}
	if body := request("GET", page, "", "", "", nil, 200).Body.String(); !strings.Contains(body, "Allow analytics") {
		t.Fatal("opt-in footer is absent")
	}
	choice := url.Values{"choice": {"allow"}, "return": {page}}
	request("POST", analyticsChoice, "https://other.test", "", "", choice, 403)
	untrusted := url.Values{"choice": {"allow"}, "return": {"//other.test/en"}}
	request("POST", analyticsChoice, cfg.BaseURL, "", "", untrusted, 422)
	accepted := request("POST", analyticsChoice, cfg.BaseURL, "", "", choice, 303)
	cookies := accepted.Result().Cookies()
	if accepted.Header().Get("Location") != page || len(cookies) != 1 || cookies[0].Value != "allow" ||
		!cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("invalid consent cookie or redirect")
	}
	request("GET", page+"?email=someone%40example.test", "", analytics.Allow, "", nil, 200)
	request("GET", page, "", analytics.Allow, "Sec-GPC", nil, 200)
	request("GET", page, "", analytics.Allow, "DNT", nil, 200)
	request("GET", page, "", analytics.Decline, "", nil, 200)
	request("HEAD", page, "", analytics.Allow, "", nil, 200)
	request("GET", "/en/about", "", analytics.Allow, "", nil, 200)
	request("GET", "/en/admin/analytics", "", analytics.Allow, "", nil, 401)
	values := url.Values{"form": {form.Key}, "version": {"1"}, "locale": {"en"}, "page": {"contact"}, "revision": {saved.RevisionID},
		"block": {"contact"}, "retry": {strings.Repeat("a", 32)}, "f_email": {"private@example.test"}}
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, analytics.Allow, "", values, 200)
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, analytics.Allow, "", values, 200)
	values.Set("retry", strings.Repeat("b", 32))
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, analytics.Decline, "", values, 200)
	values.Set("retry", strings.Repeat("c", 32))
	values.Set("website", "spam")
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, analytics.Allow, "", values, 200)
	if err = a.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	report, err := a.Report(ctx)
	if err != nil || report.Totals != (analytics.Totals{PageViews: 1, Leads: 1}) || len(report.Pages) != 1 || report.Pages[0].Path != page {
		t.Fatal(report, err)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "example.test") || strings.Contains(string(raw), saved.RevisionID) {
		t.Fatal("private submission or request metadata appeared in analytics")
	}
}

type analyticsGatedFixture struct{ themeFixture }

func (analyticsGatedFixture) ReaderContext(r *http.Request) context.Context {
	return access.WithPrincipal(r.Context(), access.Principal{Audience: access.Learner, ID: "reader"})
}

func (d analyticsGatedFixture) ResolvePage(ctx context.Context, path string) (content.PageRoute, error) {
	if access.FromContext(ctx).Audience == access.Anonymous {
		return content.PageRoute{}, content.ErrLocked
	}
	return d.themeFixture.ResolvePage(ctx, path)
}

func TestReaderAuthorizedPageIsNotCountedAsPublicTraffic(t *testing.T) {
	contents, _ := newThemeContent(t)
	a, err := analytics.Open(t.Context(), contents.DB)
	if err != nil {
		t.Fatal(err)
	}
	settings := a.Settings()
	settings.Enabled, settings.Consent = true, analytics.UnlessDeclined
	if _, err = a.Update(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Assets: assets, Analytics: a, Domain: analyticsGatedFixture{}})
	if w := get(t, h, "GET", "/en/articles/plain"); w.Code != 200 || strings.Contains(w.Body.String(), analyticsChoice) {
		t.Fatal("signed-in view or analytics isolation failed", w.Code)
	}
	if err = a.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if out, err := a.Report(t.Context()); err != nil || out.Totals.PageViews != 0 {
		t.Fatal("private read appeared in the aggregate", out, err)
	}
}

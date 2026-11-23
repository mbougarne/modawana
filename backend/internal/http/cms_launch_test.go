package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/forms"
	"github.com/mbougarne/modawana/backend/internal/outbox"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestSpanishLandingPageWithLeadFormAndSEO(t *testing.T) {
	ctx := t.Context()
	contents, owner := newThemeContent(t)
	settings := &siteconfig.Service{DB: contents.DB, Files: contents.Files}
	leadForms := &forms.Service{DB: contents.DB}
	cfg := config.Config{BaseURL: "https://example.test"}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	a := auth.New(contents.DB, cfg)
	h := New(Options{Assets: assets, Config: cfg, SiteConfig: settings, Auth: a,
		Domain: &Domain{Forms: leadForms, SiteConfig: settings, Auth: a, Content: contents}})
	request := func(method, path, origin string, body url.Values, expected int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, strings.NewReader(body.Encode()))
		if body != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s %s returned %d, expected %d: %s", method, path, w.Code, expected, w.Body.String())
		}
		return w
	}
	form, err := leadForms.Save(ctx, forms.Form{Key: "contacto", Enabled: true,
		Definition: forms.Definition{Title: "Contacto", RetentionDays: 30, Consent: "Acepto ser contactado",
			Fields: []forms.Field{{Key: "email", Label: "Correo", Kind: "email", Required: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := settings.Get(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	var site siteconfig.Site
	if err := json.Unmarshal(doc.Snapshot, &site); err != nil {
		t.Fatal(err)
	}
	site.PublicDefaultLocale = "es"
	hiddenDefault, err := json.Marshal(site)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: hiddenDefault}); err == nil {
		t.Fatal("accepted a hidden default locale")
	}
	site.PublicLocales = []string{"en", "ar", "fr", "es"}
	site.Redirects = []siteconfig.Redirect{{From: "/es/anterior", To: "/es/pages/contacto"}}
	visible, err := json.Marshal(site)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &doc.Version, Snapshot: visible}); err != nil {
		t.Fatal(err)
	}
	if root := request("GET", "/", "", nil, 200).Body.String(); !strings.Contains(root, `lang="es" dir="ltr"`) ||
		!strings.Contains(root, `href="/es/writing" lang="en" dir="auto"`) {
		t.Fatal("Spanish default or English fallback metadata missing")
	}
	zero := int64(0)
	draft := content.DraftInput{ExpectedVersion: &zero, Kind: "page", SourceLocale: "es", Slug: "contacto", Title: "Contáctanos",
		PageTemplate: "landing", BodyFormat: content.BlockBodyFormat,
		PageBlocks: json.RawMessage(`{"version":1,"blocks":[{"id":"contact","type":"core.form","version":1,"properties":{"form_key":"contacto","form_version":1}}]}`),
		SEO:        &content.SEO{Title: "Contáctanos hoy", Description: "Escríbenos", Schema: "product", Product: &content.ProductSEO{SKU: "product-01", Brand: "Ejemplo"}}}
	saved, err := contents.Save(ctx, owner, "contact", "es", "", draft)
	if err != nil {
		t.Fatal(err)
	}
	fields := url.Values{"form": {"contacto"}, "version": {"1"}, "locale": {"es"}, "page": {"contact"},
		"revision": {saved.RevisionID}, "block": {"contact"}, "retry": {strings.Repeat("1", 32)}, "consent": {"yes"}, "f_email": {"reader@example.test"}}
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, fields, http.StatusNotFound)
	published, err := contents.ChangePublication(ctx, owner, "contact", "es", "", "publish",
		content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	request("GET", "/en/pages/contacto", "", nil, http.StatusNotFound)
	body := request("GET", "/es/pages/contacto", "", nil, http.StatusOK).Body.String()
	for _, want := range []string{"Contáctanos hoy", "Escríbenos", "Correo", "application/ld+json", "data-lead-form", "Enviar", `"@type":"Product"`, `"sku":"product-01"`} {
		if !strings.Contains(body, want) {
			t.Fatal("missing expected landing metadata or live form", want)
		}
	}
	if location := request("GET", "/es/anterior", "", nil, http.StatusTemporaryRedirect).Header().Get("Location"); location != "/es/pages/contacto" {
		t.Fatal("wrong redirect destination", location)
	}
	request("POST", "/api/v1/public/lead-submit", "https://unrelated.test", fields, http.StatusForbidden)
	for range 2 {
		if response := request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, fields, 200).Body.String(); response != "{\"accepted\":true}\n" {
			t.Fatal("public acknowledgement exposed additional data")
		}
	}
	fields.Set("f_email", "other@example.test")
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, fields, http.StatusConflict)
	for _, endpoint := range []string{"/api/v1/owner/forms", "/api/v1/owner/leads", "/api/v1/owner/leads/export"} {
		request("GET", endpoint, "", nil, http.StatusUnauthorized)
	}
	inbox, err := leadForms.Inbox(ctx, "")
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].Consent != form.Consent || inbox.Items[0].Values["email"] != "reader@example.test" {
		t.Fatal("lead or consent snapshot lost", err)
	}
	worker := outbox.Worker{DB: contents.DB, Handle: forms.PrepareNotification}
	deliveries, err := worker.List(ctx, "")
	if err != nil || len(deliveries.Items) != 1 || deliveries.Items[0].Event != outbox.LeadCaptured {
		t.Fatal("expected one queued notification", err)
	}
	t.Run("notification", func(t *testing.T) {
		workerCtx := t.Context()
		done := make(chan struct{})
		go func() { defer close(done); worker.Run(workerCtx) }()
		t.Cleanup(func() { <-done })
		timeout := time.After(3 * time.Second)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				t.Fatal("notification remained pending")
			case <-ticker.C:
				inbox, err := leadForms.Inbox(workerCtx, "")
				if err != nil {
					t.Fatal(err)
				}
				if len(inbox.Items) == 1 && inbox.Items[0].Notified > 0 {
					return
				}
			}
		}
	})
	form, err = leadForms.Save(ctx, form)
	if err != nil || form.Version != 2 {
		t.Fatal("form update failed", err)
	}
	fields.Set("retry", strings.Repeat("2", 32))
	request("POST", "/api/v1/public/lead-submit", cfg.BaseURL, fields, http.StatusNotFound)
	draft.ExpectedVersion = &published.Version
	draft.PageBlocks = json.RawMessage(strings.ReplaceAll(string(draft.PageBlocks), `"form_version":1`, `"form_version":2`))
	draft.SEO.NoIndex = true
	next, err := contents.Save(ctx, owner, "contact", "es", "", draft)
	if err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/es/pages/contacto", "", nil, 200); w.Header().Get("X-Robots-Tag") != "" {
		t.Fatal("draft noindex changed live metadata")
	}
	if _, err := contents.ChangePublication(ctx, owner, "contact", "es", "", "publish",
		content.Action{ExpectedVersion: &next.Version, RevisionID: next.RevisionID}); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/es/pages/contacto", "", nil, 200); w.Header().Get("X-Robots-Tag") != "noindex" ||
		strings.Contains(w.Body.String(), "application/ld+json") {
		t.Fatal("published noindex was not honored")
	}
	if sitemap := request("GET", "/sitemap.xml", "", nil, 200).Body.String(); strings.Contains(sitemap, "/es/pages/contacto") {
		t.Fatal("noindex page included in sitemap")
	}
}

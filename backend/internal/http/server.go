package httpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/content"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

type Options struct {
	Maintenance func() bool
	SiteConfig  *siteconfig.Service
	Config      config.Config
	Assets      *Assets
	Ready       func(context.Context) error
	Logger      *slog.Logger
	Extensions  *extension.Host
	Domain      interface {
		Handle(http.ResponseWriter, *http.Request) bool
	}
	Auth interface {
		Handle(http.ResponseWriter, *http.Request) bool
	}
}
type Server struct {
	options       Options
	builtinOnce   sync.Once
	extensionHost *extension.Host
	socialOnce    sync.Once
	socialPNG     []byte
	socialErr     error
}

func New(options Options) http.Handler {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Server{options: options}
}

// extensions installs the server's built-in extensions exactly once on the
// shared host, or on a private one when the caller provided none.
func (s *Server) extensions() *extension.Host {
	s.builtinOnce.Do(func() {
		host := s.options.Extensions
		if host == nil {
			host = extension.NewHost(s.options.Logger)
		}
		if err := host.Install(socialCards{}); err != nil {
			s.options.Logger.Error("built-in extension rejected", "extension", "social-cards", "reason", err.Error())
		}
		s.extensionHost = host
	})
	return s.extensionHost
}

type requestKey struct{}

func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestKey{}).(string); return id }

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *response) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (s *Server) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		http.Error(out, "Service unavailable.", 503)
		return
	}
	id := hex.EncodeToString(random[:])
	w := &response{ResponseWriter: out}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	defer func() {
		if recover() != nil {
			if w.status == 0 {
				writeError(w, InternalError)
			}
			s.options.Logger.Error("request panic", "request_id", id)
		}
		status := w.status
		if status == 0 {
			status = 200
		}
		method := r.Method
		switch method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		default:
			method = "OTHER"
		}
		s.options.Logger.Info("request", "request_id", id, "method", method, "status", status, "duration_ms", time.Since(start).Milliseconds())
	}()
	r = r.WithContext(context.WithValue(r.Context(), requestKey{}, id))
	s.route(w, r, id)
}
func (s *Server) route(w http.ResponseWriter, r *http.Request, nonce string) {
	// Avoid redirect/clean-path behavior that can cross the API/media boundary.
	escaped := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "//") || strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		writeError(w, NotFound)
		return
	}
	for _, part := range strings.Split(r.URL.Path, "/") {
		if part == "." || part == ".." {
			writeError(w, NotFound)
			return
		}
	}

	if s.options.Maintenance != nil && s.options.Maintenance() && !maintenanceAllowed(r) {
		w.Header().Set("Retry-After", "60")
		writeError(w, Unavailable)
		return
	}
	if s.options.Auth != nil && s.options.Auth.Handle(w, r) {
		return
	}
	if s.options.Domain != nil && s.options.Domain.Handle(w, r) {
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, MethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/social-card.png":
		s.serveSocialImage(w, r)
		return
	case "/healthz":
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	case "/readyz":
		if s.options.Ready == nil {
			writeError(w, Unavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if s.options.Ready(ctx) != nil {
			writeError(w, Unavailable)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	case "/api/v1/public/runtime":
		writeJSON(w, 200, s.options.Config.Public())
		return
	}
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/media" || strings.HasPrefix(r.URL.Path, "/media/") {
		writeError(w, NotFound)
		return
	}
	if r.URL.Path == "/assets" || strings.HasPrefix(r.URL.Path, "/assets/") {
		s.serveAsset(w, r)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if wantsHTML(r) {
		if r.URL.Path == "/admin/login" || (len(parts) == 3 && pageLocale(parts[0]) && parts[1] == "admin" && parts[2] == "login") {
			locale := "en"
			if len(parts) == 3 {
				locale = parts[0]
			}
			redirectOwnerLogin(w, r, locale, "")
			return
		}
	}
	private := len(parts) >= 2 && parts[1] == "admin"
	if private {
		guard, ok := s.options.Auth.(interface{ Authorized(*http.Request) bool })
		if !ok || !guard.Authorized(r) {
			if pageLocale(parts[0]) && wantsHTML(r) {
				redirectOwnerLogin(w, r, parts[0], r.URL.RequestURI())
				return
			}
			writeError(w, Unauthenticated)
			return
		}
	}
	locale := "en"
	dir := "ltr"
	status := http.StatusOK
	switch r.URL.Path {
	case "/", "/en", "/en/login", "/en/admin", "/en/admin/security":
	case "/ar", "/ar/login", "/ar/admin", "/ar/admin/security":
		locale = "ar"
	case "/fr", "/fr/login", "/fr/admin", "/fr/admin/security":
		locale = "fr"
	default:
		status = 404
	}
	if private && len(parts) == 5 && (parts[2] == "preview" || parts[2] == "editor") && (parts[0] == "en" || parts[0] == "ar" || parts[0] == "fr") {
		locale, status = parts[0], 200
	}
	if private && len(parts) == 3 && (parts[2] == "oauth" || parts[2] == "settings" || parts[2] == "profile" || parts[2] == "labels" || parts[2] == "posts" || parts[2] == "new" || parts[2] == "collections" || parts[2] == "taxonomy" || parts[2] == "media" || parts[2] == "transfer") && pageLocale(parts[0]) {
		locale, status = parts[0], 200
	}
	var page content.PageRoute
	if len(parts) > 0 && (parts[0] == "en" || parts[0] == "ar" || parts[0] == "fr") {
		locale = parts[0]
	}
	if !private && !strings.HasSuffix(r.URL.Path, "/login") {
		if resolver, ok := s.options.Domain.(interface {
			ResolvePage(context.Context, string) (content.PageRoute, error)
		}); ok {
			var e error
			ctx := r.Context()
			if principal, ok := s.options.Domain.(interface {
				ReaderContext(*http.Request) context.Context
			}); ok {
				ctx = principal.ReaderContext(r)
			}
			page, e = resolver.ResolvePage(ctx, r.URL.Path)
			switch {
			case e == nil:
				status = 200
			case errors.Is(e, content.ErrLocked):
				status = 403
			case errors.Is(e, sql.ErrNoRows):
				status = 404
			default:
				writeError(w, Unavailable)
				return
			}
		}
	}
	if locale == "ar" {
		dir = "rtl"
	}
	if s.options.Assets == nil {
		writeError(w, Unavailable)
		return
	}
	if status >= 400 || private || strings.HasSuffix(r.URL.Path, "/login") {
		w.Header().Set("X-Robots-Tag", "noindex")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'nonce-"+nonce+"'; style-src 'self' 'nonce-"+nonce+"'; img-src 'self'; font-src 'self'; connect-src 'self'; media-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	var brandCSS template.CSS
	var boot template.JS
	var favicon string
	var settings siteconfig.Public
	if s.options.SiteConfig != nil {
		out, e := s.options.SiteConfig.Public(r.Context(), locale)
		if e != nil {
			writeError(w, Unavailable)
			return
		}
		settings = out
		brandCSS = template.CSS(out.Palette.CSS())
		encoded, e := json.Marshal(out)
		if e != nil {
			writeError(w, InternalError)
			return
		}
		boot = template.JS(encoded)
		if out.Site.FaviconID != "" {
			favicon = "/media/" + out.Site.FaviconID
		}
	}
	title, description := page.Title, page.Summary
	if title == "" {
		key := "page." + page.Kind
		if status == 404 {
			key = "foundation.notFound"
		}
		title = pageLabel(settings, key, locale)
		if title == "" {
			title = "Personal blog"
		}
		if page.Kind == "home" || page.Kind == "about" {
			if name := settings.Effective["name"].Text; name != "" {
				title = name + " · " + title
			}
			description = settings.Effective["bio"].Text
		}
	}
	canonicalURL := ""
	alternatives := []content.Alternative{}
	if status == 200 && page.URL != "" {
		canonicalURL = s.options.Config.BaseURL + page.URL
		for _, a := range page.Alternatives {
			a.URL = s.options.Config.BaseURL + a.URL
			alternatives = append(alternatives, a)
		}
	}
	social := socialMetadata{}
	if status == 200 && !private && !strings.HasSuffix(r.URL.Path, "/login") {
		social = s.renderedMeta(r.Context(), page, settings, s.options.Config.BaseURL, title)
	}
	var body bytes.Buffer
	err := shell.Execute(&body, struct {
		Locale, Direction, Nonce      string
		Scripts, Styles               []string
		BrandCSS                      template.CSS
		Boot                          template.JS
		Favicon                       string
		Title, Description, Canonical string
		Alternatives                  []content.Alternative
		Social                        socialMetadata
	}{locale, dir, nonce, s.options.Assets.Manifest.Scripts, s.options.Assets.Manifest.Styles, brandCSS, boot, favicon, title, description, canonicalURL, alternatives, social})
	if err != nil {
		writeError(w, InternalError)
		return
	}
	w.WriteHeader(status)
	if r.Method != "HEAD" {
		_, _ = w.Write(body.Bytes())
	}
}
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	if s.options.Assets == nil {
		writeError(w, NotFound)
		return
	}
	a, ok := s.options.Assets.files[r.URL.Path]
	if !ok {
		writeError(w, NotFound)
		return
	}
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// Non-content-addressed files, uploads and Markdown can never reach this map.
	http.ServeContent(w, r, r.URL.Path, a.modified, bytes.NewReader(a.body))
}

var shell = template.Must(template.New("shell").Parse(`<!doctype html><html lang="{{.Locale}}" dir="{{.Direction}}"><head><meta charset="utf-8"><meta name="csp-nonce" content="{{.Nonce}}"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title><meta name="description" content="{{.Description}}"><meta property="og:title" content="{{.Title}}"><meta property="og:description" content="{{.Description}}">{{if .Social.Image}}<meta property="og:type" content="{{.Social.Type}}"><meta property="og:site_name" content="{{.Social.SiteName}}"><meta property="og:image" content="{{.Social.Image}}"><meta property="og:image:alt" content="{{.Social.Alt}}">{{if .Social.Width}}<meta property="og:image:width" content="{{.Social.Width}}"><meta property="og:image:height" content="{{.Social.Height}}">{{end}}<meta name="twitter:card" content="summary_large_image"><meta name="twitter:title" content="{{.Title}}"><meta name="twitter:description" content="{{.Description}}"><meta name="twitter:image" content="{{.Social.Image}}"><meta name="twitter:image:alt" content="{{.Social.Alt}}">{{end}}{{if .Canonical}}<link rel="canonical" href="{{.Canonical}}"><meta property="og:url" content="{{.Canonical}}">{{end}}{{range .Alternatives}}<link rel="alternate" hreflang="{{.Locale}}" href="{{.URL}}">{{end}}<script nonce="{{.Nonce}}">try{var t=localStorage.getItem('theme');if(t==='light'||t==='dark')document.documentElement.dataset.theme=t}catch(e){}</script>{{range .Styles}}<link rel="stylesheet" href="{{.}}">{{end}}{{if .BrandCSS}}<style id="initial-brand" nonce="{{.Nonce}}">{{.BrandCSS}}</style>{{end}}{{if .Favicon}}<link rel="icon" href="{{.Favicon}}">{{end}}{{if .Boot}}<script id="public-configuration" type="application/json" nonce="{{.Nonce}}">{{.Boot}}</script>{{end}}{{range .Scripts}}<script defer src="{{.}}"></script>{{end}}</head><body><div id="root"></div><noscript lang="en" dir="ltr">JavaScript is required. <span lang="ar" dir="rtl">يتطلب JavaScript.</span> <span lang="fr">JavaScript est nécessaire.</span></noscript></body></html>`))

package httpserver

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

const feedLimit = 30
const sitemapLimit = 50_000
const sitemapBytes = 50 << 20
const sitemapURLLength = 2048

type syndicationSource interface {
	SyndicatedPages(context.Context, string, int) ([]content.SyndicatedPage, error)
}

func (d *Domain) SyndicatedPages(ctx context.Context, locale string, limit int) ([]content.SyndicatedPage, error) {
	if d.Content == nil {
		return nil, errors.New("syndication content unavailable")
	}
	return d.Content.SyndicatedPages(ctx, locale, limit)
}

func feedPath(locale string) string {
	return "/" + locale + "/feed.xml"
}

func (s *Server) syndicationSource() syndicationSource {
	source, _ := s.options.Domain.(syndicationSource)
	if s.options.Config.BaseURL == "" {
		return nil
	}
	return source
}

func (s *Server) syndicationRoutes(w http.ResponseWriter, r *http.Request) bool {
	source := s.syndicationSource()
	if source == nil {
		return false
	}
	origin := s.options.Config.BaseURL
	switch p := r.URL.Path; p {
	case "/robots.txt":
		serveSyndication(w, r, "text/plain; charset=utf-8", []byte("User-agent: *\nDisallow:\n\nSitemap: "+origin+"/sitemap.xml\n"))
	case "/sitemap.xml":
		s.serveSitemap(w, r, source, origin)
	case "/feed.xml":
		http.Redirect(w, r, feedPath(s.options.Config.Public().DefaultLocale), http.StatusPermanentRedirect)
	default:
		locale, rest, ok := strings.Cut(strings.TrimPrefix(p, "/"), "/")
		if !ok || !pageLocale(locale) || rest != "feed.xml" {
			return false
		}
		s.serveRSS(w, r, source, origin, locale)
	}
	return true
}

type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	Self        rssSelf   `xml:"atom:link"`
	Items       []rssItem `xml:"item"`
}

type rssSelf struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	Description string  `xml:"description,omitempty"`
	Published   string  `xml:"pubDate"`
	GUID        rssGUID `xml:"guid"`
}

type rssGUID struct {
	ID        string `xml:",chardata"`
	Permalink string `xml:"isPermaLink,attr"`
}

func (s *Server) serveRSS(w http.ResponseWriter, r *http.Request, source syndicationSource, origin, locale string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	pages, err := source.SyndicatedPages(ctx, locale, feedLimit)
	if err != nil {
		writeError(w, Unavailable)
		return
	}

	var settings siteconfig.Public
	if s.options.SiteConfig != nil {
		settings, err = s.options.SiteConfig.Public(ctx, locale)
		if err != nil {
			writeError(w, Unavailable)
			return
		}
	}
	title := strings.TrimSpace(settings.Effective["name"].Text)
	if title == "" {
		title = strings.TrimSpace(settings.Site.Wordmark)
	}
	if title == "" {
		title = "Modawana"
	}
	description := settings.Effective["bio"].Text
	if description == "" {
		description = title
	}
	channel := rssChannel{
		Title: title, Description: html.EscapeString(description), Language: locale,
		Link: origin + "/" + locale, Self: rssSelf{Href: origin + feedPath(locale), Rel: "self", Type: "application/rss+xml"},
	}
	for _, page := range pages {
		published, err := time.Parse(db.TimestampLayout, page.Published)
		if err != nil {
			writeError(w, Unavailable)
			return
		}
		channel.Items = append(channel.Items, rssItem{
			Title: page.Title, Link: origin + page.URL, Description: html.EscapeString(page.Summary),
			Published: published.Format(time.RFC1123Z), GUID: rssGUID{ID: "urn:modawana:version:" + page.ID, Permalink: "false"},
		})
	}
	serveSyndicationXML(w, r, "application/rss+xml", rssDocument{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Channel: channel})
}

type sitemapDocument struct {
	XMLName xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Location string `xml:"loc"`
	Modified string `xml:"lastmod,omitempty"`
}

func (s *Server) serveSitemap(w http.ResponseWriter, r *http.Request, source syndicationSource, origin string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	pages, err := source.SyndicatedPages(ctx, "", sitemapLimit+1)
	if err != nil || len(pages) > sitemapLimit {
		writeError(w, Unavailable)
		return
	}
	doc := sitemapDocument{}
	seen := map[string]bool{}
	add := func(path, modified string) {
		location := origin + path
		if !seen[path] && len(location) < sitemapURLLength {
			seen[path] = true
			doc.URLs = append(doc.URLs, sitemapURL{Location: location, Modified: modified})
		}
	}
	for _, locale := range s.options.Config.Public().Locales {
		for _, suffix := range []string{"", "/about", "/writing", "/learn", "/docs"} {
			add("/"+locale+suffix, "")
		}
	}
	for _, page := range pages {
		if page.CollectionURL != "" {
			add(page.CollectionURL, "")
		}
		add(page.URL, page.Published)
	}
	if len(doc.URLs) > sitemapLimit {
		writeError(w, Unavailable)
		return
	}
	serveSyndicationXML(w, r, "application/xml", doc)
}

// Buffer before sending headers so an over-limit or failed encoding cannot
// send a successful status with a truncated XML document.
type syndicationBuffer struct {
	bytes.Buffer
}

func (b *syndicationBuffer) Write(p []byte) (int, error) {
	if len(p) > sitemapBytes-b.Len() {
		return 0, errors.New("syndication response too large")
	}
	return b.Buffer.Write(p)
}

func serveSyndicationXML(w http.ResponseWriter, r *http.Request, contentType string, doc any) {
	var out syndicationBuffer
	out.WriteString(xml.Header)
	if err := xml.NewEncoder(&out).Encode(doc); err != nil {
		writeError(w, Unavailable)
		return
	}
	serveSyndication(w, r, contentType+"; charset=utf-8", out.Bytes())
}

func serveSyndication(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

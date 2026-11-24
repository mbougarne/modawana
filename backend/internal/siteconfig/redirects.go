package siteconfig

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/locale"
)

// Redirects use decoded, locale-prefixed public paths. The HTTP resolver
// escapes the resolved canonical destination when it builds Location.
type Redirect struct {
	From string `json:"from"`
	To   string `json:"to"`
}

var redirectSlug = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]{0,159}$`)

func redirectParts(path string) []string {
	if len(path) > 512 || !strings.HasPrefix(path, "/") {
		return nil
	}
	parts := strings.Split(path[1:], "/")
	if !locale.Valid(parts[0]) || len(parts) > 4 {
		return nil
	}
	for _, part := range parts[1:] {
		if !redirectSlug.MatchString(part) {
			return nil
		}
	}
	if len(parts) >= 2 {
		switch parts[1] {
		case "admin", "staff", "login", "setup", "api", "media", "assets", "search":
			return nil
		}
	}
	return parts
}

func validRedirects(rules []Redirect) bool {
	if len(rules) > 100 {
		return false
	}
	sources := map[string]bool{}
	for _, rule := range rules {
		from := redirectParts(rule.From)
		to := redirectParts(rule.To)
		if len(from) < 2 || len(to) == 0 || from[1] == "store" || sources[rule.From] {
			return false
		}
		if len(from) == 2 {
			switch from[1] {
			case "about", "writing", "learn", "docs", "articles", "pages", "doc":
				return false
			}
		}
		sources[rule.From] = true
	}
	for _, rule := range rules {
		// Require a real destination rather than a chain of redirects.
		if sources[rule.To] {
			return false
		}
	}
	return true
}

func validateRedirectSources(ctx context.Context, q query, rules []Redirect) error {
	for _, rule := range rules {
		parts := redirectParts(rule.From)
		var collision bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM content_route_aliases WHERE path=?)
			OR EXISTS(SELECT 1 FROM content_versions WHERE locale=? AND slug=? AND published_revision_id IS NOT NULL)
			OR EXISTS(SELECT 1 FROM collection_locales WHERE locale=? AND slug=? AND visible=1)`,
			rule.From, parts[0], parts[len(parts)-1], parts[0], parts[len(parts)-1]).Scan(&collision)
		if err != nil {
			return err
		}
		if collision {
			return invalidField("redirects", "redirects")
		}
	}
	return nil
}

func (s *Service) RedirectTarget(ctx context.Context, from string) (string, error) {
	doc, err := s.Get(ctx, "site")
	if err != nil {
		return "", err
	}
	var site Site
	if err := json.Unmarshal(doc.Snapshot, &site); err != nil {
		return "", err
	}
	for _, rule := range site.Redirects {
		if rule.From == from {
			return rule.To, nil
		}
	}
	return "", nil
}

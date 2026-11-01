package auth

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type TokenInput struct {
	Label         string   `json:"label"`
	Scopes        []string `json:"scopes"`
	Collections   []string `json:"collections"`
	AllowArticles bool     `json:"allow_articles"`
	Days          int      `json:"days"`
}
type PublisherToken struct {
	ID            string   `json:"id"`
	OAuthHash     string   `json:"-"`
	Owner         string   `json:"-"`
	Label         string   `json:"label"`
	Expires       string   `json:"expires_at"`
	LastUsed      *string  `json:"last_used_at"`
	Scopes        []string `json:"scopes"`
	Collections   []string `json:"collections"`
	AllowArticles bool     `json:"allow_articles"`
	Secret        string   `json:"secret,omitempty"`
}

func (s *Service) CreatePublisherToken(ctx context.Context, session Session, in TokenInput) (PublisherToken, error) {
	var out PublisherToken
	if strings.TrimSpace(in.Label) == "" || utf8.RuneCountInString(in.Label) > 120 {
		return out, tokenField("security.token_label", "token_label")
	}
	if len(in.Scopes) < 1 || len(in.Scopes) > 4 {
		return out, tokenField("security.token_scopes", "token_scope")
	}
	if len(in.Collections) > 50 || len(in.Collections) == 0 && !in.AllowArticles {
		return out, tokenField("security.token_collections", "token_collection")
	}
	if in.Days < 1 || in.Days > 365 {
		return out, tokenField("security.token_days", "token_days")
	}
	seen := map[string]bool{}
	for _, scope := range in.Scopes {
		if seen[scope] || (scope != "draft" && scope != "media" && scope != "quiz" && scope != "publish") {
			return out, tokenField("security.token_scopes", "token_scope")
		}
		seen[scope] = true
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = s.freshInTx(ctx, tx, session); e != nil {
		return out, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM api_tokens WHERE revoked_at IS NULL AND expires_at>?", s.now()).Scan(&count); e != nil {
		return out, e
	}
	if count >= 50 {
		return out, ErrRate
	}
	out.ID, e = db.NewID()
	if e != nil {
		return out, e
	}
	out.Secret, e = secret()
	if e != nil {
		return out, e
	}
	out.Owner = session.Owner
	out.Label = strings.TrimSpace(in.Label)
	out.Expires = db.UTC(s.Now().Add(time.Duration(in.Days) * 24 * time.Hour))
	out.Scopes = in.Scopes
	out.Collections = append([]string{}, in.Collections...)
	out.AllowArticles = in.AllowArticles
	if _, e = tx.ExecContext(ctx, "INSERT INTO api_tokens(id,owner_id,token_hash,label,expires_at,allow_articles) VALUES(?,?,?,?,?,?)", out.ID, out.Owner, digest(out.Secret), out.Label, out.Expires, in.AllowArticles); e != nil {
		return out, e
	}
	for _, scope := range in.Scopes {
		if _, e = tx.ExecContext(ctx, "INSERT INTO api_token_scopes VALUES(?,?)", out.ID, scope); e != nil {
			return out, e
		}
	}
	seenCollections := map[string]bool{}
	for _, key := range in.Collections {
		if seenCollections[key] {
			return out, tokenField("security.token_collections", "token_collection")
		}
		seenCollections[key] = true
		var id string
		if e = tx.QueryRowContext(ctx, "SELECT id FROM collections WHERE key=?", key).Scan(&id); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return out, tokenField("security.token_collections", "token_collection")
			}
			return out, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO api_token_collections VALUES(?,?)", out.ID, id); e != nil {
			return out, ErrForbidden
		}
	}
	if e = s.audit(ctx, tx, "publisher_token_created", ""); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Service) PublisherRequest(r *http.Request) (PublisherToken, error) {
	var out PublisherToken
	// Bearer credentials have a separate namespace; owner/learner cookies cannot
	// grant this route additional privileges. No URL credential is accepted.
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !validSecret(raw) || r.URL.RawQuery != "" {
		return out, ErrAuth
	}
	e := s.DB.QueryRowContext(r.Context(), "SELECT id,owner_id,label,expires_at,allow_articles FROM api_tokens WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?", digest(raw), s.now()).Scan(&out.ID, &out.Owner, &out.Label, &out.Expires, &out.AllowArticles)
	if errors.Is(e, sql.ErrNoRows) {
		return s.oauthPublisher(r, raw)
	}
	return out, e
}
func (s *Service) PublisherCheck(ctx context.Context, tx *sql.Tx, t PublisherToken, scope, key, collection, kind string, quiz bool) error {
	if e := s.oauthCheck(ctx, tx, t); e != nil {
		return e
	}
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM api_tokens t JOIN api_token_scopes s ON s.token_id=t.id WHERE t.id=? AND t.revoked_at IS NULL AND t.expires_at>? AND s.scope=?`, t.ID, s.now(), scope).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrForbidden
	}
	var item, existingKind, existingCollection string
	e = tx.QueryRowContext(ctx, `SELECT i.id,i.kind,coalesce(c.key,'') FROM content_items i LEFT JOIN collection_items m ON m.item_id=i.id LEFT JOIN collections c ON c.id=m.collection_id WHERE i.key=?`, key).Scan(&item, &existingKind, &existingCollection)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if item != "" {
		kind = existingKind
		collection = existingCollection
	}
	if scope != "media" {
		if kind == "article" {
			if !t.AllowArticles {
				return ErrForbidden
			}
		} else {
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM api_token_collections a JOIN collections c ON c.id=a.collection_id WHERE a.token_id=? AND c.key=?`, t.ID, collection).Scan(&n)
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrForbidden
			}
		}
		if scope == "draft" && item != "" {
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM content_versions v JOIN quiz_bindings b ON b.revision_id=v.draft_revision_id WHERE v.item_id=?`, item).Scan(&n); e != nil {
				return e
			}
			quiz = quiz || n > 0
		}
	}
	if quiz {
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM api_token_scopes WHERE token_id=? AND scope='quiz'", t.ID).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return ErrForbidden
		}
	}
	_, e = tx.ExecContext(ctx, "UPDATE api_tokens SET last_used_at=? WHERE id=?", s.now(), t.ID)
	return e
}
func (s *Service) PublisherReferences(ctx context.Context, tx *sql.Tx, t PublisherToken, revision string) error {
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM revision_media rm WHERE rm.revision_id=? AND NOT EXISTS(SELECT 1 FROM publisher_media pm WHERE pm.token_id=? AND pm.media_id=rm.media_id) AND NOT EXISTS(SELECT 1 FROM revision_media previous JOIN revisions r ON r.id=previous.revision_id JOIN content_versions v ON v.id=r.version_id JOIN content_items i ON i.id=v.item_id LEFT JOIN collection_items m ON m.item_id=i.id WHERE previous.media_id=rm.media_id AND previous.revision_id!=? AND ((i.kind='article' AND ?=1) OR EXISTS(SELECT 1 FROM api_token_collections c WHERE c.token_id=? AND c.collection_id=m.collection_id)))`, revision, t.ID, revision, t.AllowArticles, t.ID).Scan(&n)
	if e != nil {
		return e
	}
	if n != 0 {
		return ErrForbidden
	}
	return nil
}
func (s *Service) publisherSecurity(w http.ResponseWriter, r *http.Request, session Session) bool {
	if r.URL.Path != "/api/v1/security/publisher-tokens" {
		return false
	}
	if r.Method == "POST" {
		var in TokenInput
		if !decode(w, r, &in) {
			return true
		}
		out, e := s.CreatePublisherToken(r.Context(), session, in)
		if e != nil {
			fail(w, e)
		} else {
			jsonOut(w, 201, out)
		}
		return true
	}
	if r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,label,expires_at,last_used_at,allow_articles FROM api_tokens WHERE owner_id=? AND revoked_at IS NULL AND expires_at>? ORDER BY created_at DESC LIMIT 50", session.Owner, s.now())
		if e != nil {
			fail(w, e)
			return true
		}
		out := []PublisherToken{}
		for rows.Next() {
			var t PublisherToken
			if e = rows.Scan(&t.ID, &t.Label, &t.Expires, &t.LastUsed, &t.AllowArticles); e != nil {
				rows.Close()
				fail(w, e)
				return true
			}
			t.Scopes = []string{}
			t.Collections = []string{}
			out = append(out, t)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			fail(w, e)
			return true
		}
		for i := range out {
			for _, part := range []struct {
				query string
				dest  *[]string
			}{{"SELECT scope FROM api_token_scopes WHERE token_id=? ORDER BY scope", &out[i].Scopes}, {"SELECT c.key FROM api_token_collections t JOIN collections c ON c.id=t.collection_id WHERE t.token_id=? ORDER BY c.key", &out[i].Collections}} {
				rr, e := s.DB.QueryContext(r.Context(), part.query, out[i].ID)
				if e != nil {
					fail(w, e)
					return true
				}
				for rr.Next() {
					var v string
					if e = rr.Scan(&v); e != nil {
						break
					}
					*part.dest = append(*part.dest, v)
				}
				if e == nil {
					e = rr.Err()
				}
				rr.Close()
				if e != nil {
					fail(w, e)
					return true
				}
			}
		}
		jsonOut(w, 200, out)
		return true
	}
	if r.Method == "DELETE" || r.Method == "PATCH" {
		var in struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		if !decode(w, r, &in) {
			return true
		}
		if r.Method == "PATCH" && (strings.TrimSpace(in.Label) == "" || utf8.RuneCountInString(in.Label) > 120) {
			fail(w, ErrForbidden)
			return true
		}
		query := "UPDATE api_tokens SET revoked_at=? WHERE id=? AND owner_id=? AND revoked_at IS NULL"
		value := s.now()
		if r.Method == "PATCH" {
			query = "UPDATE api_tokens SET label=? WHERE id=? AND owner_id=? AND revoked_at IS NULL"
			value = strings.TrimSpace(in.Label)
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			fail(w, e)
			return true
		}
		defer tx.Rollback()
		if e = s.freshInTx(r.Context(), tx, session); e != nil {
			fail(w, e)
			return true
		}
		result, e := tx.ExecContext(r.Context(), query, value, in.ID, session.Owner)
		if e != nil {
			fail(w, e)
			return true
		}
		n, e := result.RowsAffected()
		if e != nil {
			fail(w, e)
			return true
		}
		if n != 1 {
			fail(w, ErrConflict)
			return true
		}
		if r.Method == "DELETE" {
			e = s.audit(r.Context(), tx, "publisher_token_revoked", w.Header().Get("X-Request-ID"))
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			fail(w, e)
			return true
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
		return true
	}
	fail(w, ErrForbidden)
	return true
}

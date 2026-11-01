package httpserver

import (
	"database/sql"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/content"
	"net/http"
	"strings"
)

func (d *Domain) publisherRoutes(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/publisher/") {
		return false
	}
	token, e := d.Auth.PublisherRequest(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	if r.URL.Path == "/api/v1/publisher/media" && r.Method == "POST" {
		if r.Header.Get("Idempotency-Key") == "" {
			writeError(w, InvalidFields)
			return true
		}
		ctx := access.WithMutation(r.Context(), access.Mutation{Actor: "publisher:" + token.ID, Check: func(tx *sql.Tx, id string) error {
			if e := d.Auth.PublisherCheck(r.Context(), tx, token, "media", "", "", "", false); e != nil {
				return e
			}
			if id != "" {
				_, e := tx.ExecContext(r.Context(), "INSERT INTO publisher_media VALUES(?,?)", token.ID, id)
				return e
			}
			return nil
		}})
		return d.uploadMedia(w, r.WithContext(ctx), token.Owner)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/publisher/content/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/api/v1/publisher/content/") || len(parts) < 2 || len(parts) > 3 {
		writeError(w, NotFound)
		return true
	}
	key, locale := parts[0], parts[1]
	scope := "draft"
	var in content.DraftInput
	var action content.Action
	if len(parts) == 3 && parts[2] == "publish" && r.Method == "POST" {
		scope = "publish"
		if !DecodeJSON(w, r, &action, 4096) {
			return true
		}
		if action.Plan != nil {
			writeError(w, Forbidden)
			return true
		}
	} else if len(parts) == 2 && r.Method == "PUT" {
		if !DecodeJSON(w, r, &in, content.MaxBody+(512<<10)) {
			return true
		}
	} else if len(parts) != 2 || r.Method != "GET" {
		writeError(w, MethodNotAllowed)
		return true
	}
	ctx := access.WithMutation(r.Context(), access.Mutation{Actor: "publisher:" + token.ID, Check: func(tx *sql.Tx, revision string) error {
		if e := d.Auth.PublisherCheck(r.Context(), tx, token, scope, key, in.CollectionKey, in.Kind, in.Quiz != nil); e != nil {
			return e
		}
		if revision != "" {
			return d.Auth.PublisherReferences(r.Context(), tx, token, revision)
		}
		return nil
	}})
	if r.Method == "GET" {
		out, e := d.Content.Draft(ctx, key, locale)
		if e != nil {
			domainError(w, e)
		} else {
			// Reading a quiz answer key also requires the quiz capability.
			var n int
			e = d.Auth.DB.QueryRowContext(ctx, "SELECT count(*) FROM api_token_scopes WHERE token_id=? AND scope='quiz'", token.ID).Scan(&n)
			if e != nil {
				domainError(w, e)
				return true
			}
			if n == 0 {
				out.Revision.Quiz = nil
			}
			writeJSON(w, 200, out)
		}
		return true
	}
	retry := r.Header.Get("Idempotency-Key")
	if retry == "" {
		writeError(w, InvalidFields)
		return true
	}
	var out content.Result
	if scope == "publish" {
		out, e = d.Content.ChangePublication(ctx, token.Owner, key, locale, retry, "publish", action)
	} else {
		out, e = d.Content.Save(ctx, token.Owner, key, locale, retry, in)
	}
	if e != nil {
		domainError(w, e)
	} else {
		writeJSON(w, 200, out)
	}
	return true
}

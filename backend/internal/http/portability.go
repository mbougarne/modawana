package httpserver

import (
	"errors"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/portability"
	"io"
	"net/http"
	"os"
	"time"
)

var transferGate = make(chan struct{}, 1)

func (d *Domain) portabilityRoutes(w http.ResponseWriter, r *http.Request) bool {
	notion := r.URL.Path == "/api/v1/portability/notion/preview" || r.URL.Path == "/api/v1/portability/notion/import"
	if !notion && r.URL.Path != "/api/v1/portability/export" && r.URL.Path != "/api/v1/portability/import" {
		return false
	}
	if r.Method != "POST" {
		writeError(w, MethodNotAllowed)
		return true
	}
	options := portability.NotionOptions{}
	if notion {
		query := r.URL.Query()
		if len(query) != 2 || len(query["locale"]) != 1 || len(query["kind"]) != 1 {
			writeError(w, InvalidFields)
			return true
		}
		options = portability.NotionOptions{Locale: query.Get("locale"), Kind: query.Get("kind")}
	} else if r.URL.RawQuery != "" {
		writeError(w, InvalidFields)
		return true
	}
	owner, e := d.Auth.OwnerRequest(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	check, e := d.Auth.FreshOwnerCheck(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	select {
	case transferGate <- struct{}{}:
		defer func() { <-transferGate }()
	default:
		writeError(w, RateLimited)
		return true
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Minute))
	svc := &portability.Service{DB: d.Content.DB, Files: d.Content.Files}
	f, e := os.CreateTemp("", "blog-transfer-*.zip")
	if e != nil {
		domainError(w, e)
		return true
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if r.URL.Path == "/api/v1/portability/export" {
		e = svc.Export(r.Context(), f)
		if e != nil {
			domainError(w, e)
			return true
		}
		tx, e := d.Content.DB.BeginTx(r.Context(), nil)
		if e != nil {
			domainError(w, e)
			return true
		}
		e = check(tx)
		tx.Rollback()
		if e != nil {
			domainError(w, e)
			return true
		}
		if _, e = f.Seek(0, 0); e != nil {
			domainError(w, e)
			return true
		}
		st, e := f.Stat()
		if e != nil {
			domainError(w, e)
			return true
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="blog-content.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "blog-content.zip", st.ModTime(), f)
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	if _, e = io.Copy(f, r.Body); e != nil {
		var limit *http.MaxBytesError
		if errors.As(e, &limit) {
			writeError(w, TooLarge)
		} else {
			domainError(w, e)
		}
		return true
	}
	if e = f.Close(); e != nil {
		domainError(w, e)
		return true
	}
	if notion {
		var result any
		if r.URL.Path == "/api/v1/portability/notion/preview" {
			result, e = svc.PreviewNotion(r.Context(), f.Name(), options)
		} else {
			result, e = svc.ImportNotion(r.Context(), owner, f.Name(), options, check)
		}
		if errors.Is(e, bundle.ErrInvalid) {
			writeError(w, InvalidFields)
		} else if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, result)
		}
		return true
	}
	result, e := svc.Import(r.Context(), owner, f.Name(), check)
	if e != nil {
		if errors.Is(e, bundle.ErrInvalid) {
			writeError(w, InvalidFields)
		} else {
			domainError(w, e)
		}
	} else {
		writeJSON(w, 200, result)
	}
	return true
}

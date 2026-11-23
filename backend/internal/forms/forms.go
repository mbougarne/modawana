package forms

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/db"
)

var ErrInvalid = errors.New("invalid lead form")
var ErrConflict = errors.New("lead submission key conflict")
var ErrLimited = errors.New("lead submission limit exceeded")

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Reference struct {
	Key     string `json:"form_key"`
	Version int64  `json:"form_version"`
}

func (r Reference) Valid() bool { return keyPattern.MatchString(r.Key) && r.Version >= 1 }

type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
}

type Definition struct {
	Title         string  `json:"title"`
	Fields        []Field `json:"fields"`
	Consent       string  `json:"consent,omitempty"`
	RetentionDays int     `json:"retention_days"`
}

type Form struct {
	Key     string `json:"key"`
	Version int64  `json:"version"`
	Enabled bool   `json:"enabled"`
	Definition
}

type Service struct{ DB *sql.DB }

type reader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Visible(ctx context.Context, q reader, ref Reference) (Definition, error) {
	var out Definition
	if !ref.Valid() {
		return out, ErrInvalid
	}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT r.definition FROM lead_forms f JOIN lead_form_versions r
		ON r.form_key=f.key AND r.version=f.version WHERE f.key=? AND f.version=? AND f.enabled=1`, ref.Key, ref.Version).Scan(&raw)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(raw, &out)
}

func validText(value string, min, max int, multiline bool) bool {
	n := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && n >= min && n <= max &&
		!strings.ContainsFunc(value, func(c rune) bool {
			return unicode.IsControl(c) && !(multiline && (c == '\n' || c == '\t' || c == '\r'))
		})
}

func (d Definition) valid() bool {
	if !validText(strings.TrimSpace(d.Title), 1, 100, false) ||
		!validText(d.Consent, 0, 500, false) || len(d.Fields) < 1 || len(d.Fields) > 12 ||
		d.RetentionDays < 1 || d.RetentionDays > 365 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		if !keyPattern.MatchString(f.Key) || seen[f.Key] || !validText(strings.TrimSpace(f.Label), 1, 100, false) {
			return false
		}
		switch f.Kind {
		case "text", "email", "textarea":
		default:
			return false
		}
		seen[f.Key] = true
	}
	return true
}

func (s *Service) Save(ctx context.Context, in Form) (Form, error) {
	if !keyPattern.MatchString(in.Key) || in.Version < 0 || !in.Definition.valid() {
		return Form{}, ErrInvalid
	}
	definition, err := json.Marshal(in.Definition)
	if err != nil {
		return Form{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Form{}, err
	}
	defer tx.Rollback()
	var version int64
	err = tx.QueryRowContext(ctx, "SELECT version FROM lead_forms WHERE key=?", in.Key).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Form{}, err
	}
	if in.Version != version {
		return Form{}, db.ErrVersionConflict
	}
	if version == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM lead_forms").Scan(&count); err != nil {
			return Form{}, err
		}
		if count >= 100 {
			return Form{}, ErrInvalid
		}
	}
	in.Version++
	if _, err := tx.ExecContext(ctx, `INSERT INTO lead_forms(key,version,enabled) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET version=excluded.version,enabled=excluded.enabled`, in.Key, in.Version, in.Enabled); err != nil {
		return Form{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO lead_form_versions(form_key,version,definition) VALUES(?,?,?)", in.Key, in.Version, string(definition)); err != nil {
		return Form{}, err
	}
	return in, tx.Commit()
}

func (s *Service) List(ctx context.Context) ([]Form, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT f.key,f.version,f.enabled,r.definition FROM lead_forms f
		JOIN lead_form_versions r ON r.form_key=f.key AND r.version=f.version ORDER BY f.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Form{}
	for rows.Next() {
		var form Form
		var raw []byte
		if err := rows.Scan(&form.Key, &form.Version, &form.Enabled, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &form.Definition); err != nil {
			return nil, err
		}
		out = append(out, form)
	}
	return out, rows.Err()
}

func (d Definition) validateSubmission(values map[string]string, consent bool) bool {
	if len(values) > len(d.Fields) || (d.Consent != "") != consent {
		return false
	}
	allowed := map[string]bool{}
	for _, f := range d.Fields {
		allowed[f.Key] = true
		value := values[f.Key]
		if f.Required && strings.TrimSpace(value) == "" {
			return false
		}
		length := 300
		if f.Kind == "textarea" {
			length = 2000
		}
		if !validText(value, 0, length, f.Kind == "textarea") {
			return false
		}
		if f.Kind == "email" && value != "" {
			email, err := mail.ParseAddress(value)
			if err != nil || email.Address != value {
				return false
			}
		}
	}
	for key := range values {
		if !allowed[key] {
			return false
		}
	}
	return true
}

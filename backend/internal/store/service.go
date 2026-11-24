// Package store owns the optional storefront, catalog, and stock ledger.
package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"golang.org/x/text/currency"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const Slug = "store"

var (
	ErrInvalid  = errors.New("invalid catalog fields")
	ErrConflict = errors.New("catalog resource already in use")
	ErrStock    = errors.New("stock would be outside the supported range")
	ErrVariant  = errors.New("a published product requires an active variant")
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var slugPattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]{0,159}$`)
var skuPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,47}$`)

type Service struct {
	DB                *sql.DB
	PaymentConnection func(currency string) string
	PaymentAmount     func(currency string, minor int64) bool
	Paused            func() bool
}

func (*Service) Slug() string { return Slug }

func (s *Service) Register(r *extension.Registrar) error {
	return r.Job(extension.Job{Name: "expire-orders", Every: time.Minute, Timeout: 15 * time.Second, Run: func(ctx context.Context) error {
		if s.Paused != nil && s.Paused() {
			return nil
		}
		return s.expireOrders(ctx, time.Now())
	}})
}

type Settings struct {
	Version  int64  `json:"version"`
	Enabled  bool   `json:"enabled"`
	Currency string `json:"currency"`
	Digits   int    `json:"digits"`
}

type SettingsInput struct {
	Version  int64  `json:"version"`
	Enabled  bool   `json:"enabled"`
	Currency string `json:"currency"`
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func currencyDigits(code string) (int, bool) {
	u, err := currency.ParseISO(code)
	if err != nil || u == currency.XXX || u.String() != code {
		return 0, false
	}
	digits, _ := currency.Standard.Rounding(u)
	return digits, digits <= 4 && digits >= 0
}

func readSettings(ctx context.Context, q querier) (Settings, error) {
	var out Settings
	err := q.QueryRowContext(ctx, "SELECT version,enabled,currency FROM store_settings WHERE id=1").Scan(&out.Version, &out.Enabled, &out.Currency)
	if err != nil {
		return out, err
	}
	var ok bool
	out.Digits, ok = currencyDigits(out.Currency)
	if !ok {
		return out, ErrInvalid
	}
	return out, nil
}

func (s *Service) Settings(ctx context.Context) (Settings, error) { return readSettings(ctx, s.DB) }

func (s *Service) SaveSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	if _, ok := currencyDigits(in.Currency); !ok || in.Version < 1 {
		return Settings{}, ErrInvalid
	}
	out := Settings{}
	err := s.write(ctx, "", func(tx *sql.Tx) error {
		current, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return db.ErrVersionConflict
		}
		if current.Currency != in.Currency {
			var used bool
			if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM store_variants)").Scan(&used); err != nil {
				return err
			}
			if used {
				return ErrConflict
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE store_settings SET enabled=?,currency=?,version=version+1 WHERE id=1", in.Enabled, in.Currency); err != nil {
			return err
		}
		out, err = readSettings(ctx, tx)
		return err
	})
	return out, err
}

func (s *Service) write(ctx context.Context, resource string, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = access.CheckMutation(ctx, tx, resource); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return constraint(err)
	}
	return tx.Commit()
}

func constraint(err error) error {
	var e *sqlite.Error
	if errors.As(err, &e) {
		switch e.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return ErrConflict
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, sqlite3.SQLITE_CONSTRAINT_CHECK:
			return ErrInvalid
		}
	}
	return err
}

func plain(value string, max int, required, multiline bool) bool {
	return (!required || strings.TrimSpace(value) != "") && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && !(multiline && r == '\n') })
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Package editorial generates bounded, reviewable suggestions with explicitly
// selected public context. The package has no draft or publication writer.
package editorial

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/media"
)

const Slug = "editorial-ai"
const Model = "gpt-5.4-mini"

var (
	ErrInvalid       = errors.New("invalid editorial request")
	ErrDisabled      = errors.New("editorial generation unavailable")
	ErrKey           = errors.New("editorial credential unavailable")
	ErrQuota         = errors.New("editorial daily quota reached")
	ErrBusy          = errors.New("editorial concurrency limit reached")
	ErrProviderLimit = errors.New("editorial provider rate or quota limit reached")
	ErrTimeout       = errors.New("editorial provider timed out")
	ErrProvider      = errors.New("editorial provider request failed")
	ErrOutput        = errors.New("editorial provider output invalid")
	ErrChanged       = errors.New("published editorial context changed")
)

type Settings struct {
	Version      int64 `json:"version"`
	Enabled      bool  `json:"enabled"`
	DailyLimit   int   `json:"daily_limit"`
	Configured   bool  `json:"configured"`
	KeyAvailable bool  `json:"key_available"`
	Used         int   `json:"used"`
}

type Update struct {
	Version          int64   `json:"version"`
	Enabled          bool    `json:"enabled"`
	DailyLimit       int     `json:"daily_limit"`
	Credential       *string `json:"credential,omitempty"`
	RemoveCredential bool    `json:"remove_credential,omitempty"`
}

type SourceRequest struct {
	Kind     string   `json:"kind"`
	Key      string   `json:"key"`
	Locale   string   `json:"locale"`
	Revision string   `json:"revision_id"`
	Target   string   `json:"target_locale"`
	Fields   []string `json:"fields"`
}

// Input is the complete provider boundary. IDs, unpublished data, and
// operational records never cross it.
type Input struct {
	Kind, Locale, Target                     string
	Field, Title, Summary, Excerpt, ImageURL string
}

type Output struct {
	Text        string `json:"text"`
	Description string `json:"description"`
}

type Provider interface {
	Suggest(context.Context, string, Input) (Output, error)
}

type Service struct {
	DB       *sql.DB
	Content  *content.Service
	key      func() ([]byte, error)
	provider Provider
	origin   string
	now      func() time.Time
	slots    chan struct{}
}

func New(database *sql.DB, contents *content.Service, origin string, key func() ([]byte, error), provider Provider) *Service {
	return &Service{DB: database, Content: contents, key: key, provider: provider, origin: origin, now: time.Now, slots: make(chan struct{}, 2)}
}

func (*Service) Slug() string { return Slug }

func (*Service) Register(r *extension.Registrar) error {
	return r.Contribution(extension.Contribution{Kind: extension.ProviderAdapter, Key: "openai", Target: "editorial_ai", Version: 1,
		Label: extension.LocalizedText{"en": "OpenAI", "fr": "OpenAI", "ar": "أوبن إيه آي"}}, nil)
}

func (s *Service) box() (cipher.AEAD, error) {
	if s.key == nil {
		return nil, ErrKey
	}
	key, err := s.key()
	if err != nil || len(key) != 32 {
		return nil, ErrKey
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("modawana/editorial-ai/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, ErrKey
	}
	return cipher.NewGCMWithRandomNonce(block)
}

var credentialFormat = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var credentialPurpose = []byte("openai")

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	var out Settings
	_, keyErr := s.box()
	out.KeyAvailable = keyErr == nil
	day := s.now().UTC().Format(time.DateOnly)
	err := s.DB.QueryRowContext(ctx, `SELECT version,enabled,daily_limit,credential IS NOT NULL,
		coalesce((SELECT requests FROM editorial_usage WHERE day=?),0) FROM editorial_settings WHERE id=1`, day).
		Scan(&out.Version, &out.Enabled, &out.DailyLimit, &out.Configured, &out.Used)
	return out, err
}

func (s *Service) Update(ctx context.Context, in Update, check func(*sql.Tx) error) (Settings, error) {
	if in.Version < 1 || in.DailyLimit < 1 || in.DailyLimit > 100 || in.Credential != nil && in.RemoveCredential || check == nil {
		return Settings{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return Settings{}, err
	}
	var version int64
	var secret []byte
	if err = tx.QueryRowContext(ctx, "SELECT version,credential FROM editorial_settings WHERE id=1").Scan(&version, &secret); err != nil {
		return Settings{}, err
	}
	if version != in.Version {
		return Settings{}, db.ErrVersionConflict
	}
	if in.RemoveCredential {
		secret = nil
	}
	if in.Credential != nil {
		value := strings.TrimSpace(*in.Credential)
		if len(value) < 8 || len(value) > 1024 || !credentialFormat.MatchString(value) {
			return Settings{}, ErrInvalid
		}
		box, err := s.box()
		if err != nil {
			return Settings{}, err
		}
		secret = box.Seal(nil, nil, []byte(value), credentialPurpose)
	}
	if in.Enabled {
		if len(secret) == 0 {
			return Settings{}, ErrKey
		}
		box, err := s.box()
		if err != nil {
			return Settings{}, err
		}
		if _, err = box.Open(nil, nil, secret, credentialPurpose); err != nil {
			return Settings{}, ErrKey
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE editorial_settings SET version=version+1,enabled=?,daily_limit=?,credential=? WHERE id=1`,
		in.Enabled, in.DailyLimit, secret); err != nil {
		return Settings{}, err
	}
	if err = tx.Commit(); err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}

func (s *Service) reserve(ctx context.Context, version int64, source SourceRequest, cover string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var active bool
	var limit int
	var liveVersion int64
	var encrypted []byte
	err = tx.QueryRowContext(ctx, "SELECT version,enabled,daily_limit,credential FROM editorial_settings WHERE id=1").Scan(&liveVersion, &active, &limit, &encrypted)
	if err != nil {
		return "", err
	}
	if !active || len(encrypted) == 0 || liveVersion != version {
		return "", ErrDisabled
	}
	box, err := s.box()
	if err != nil {
		return "", err
	}
	token, err := box.Open(nil, nil, encrypted, credentialPurpose)
	if err != nil || len(token) < 8 || len(token) > 1024 || !credentialFormat.Match(token) {
		return "", ErrKey
	}
	if err = s.Content.CheckEditorialSource(ctx, tx, source.Key, source.Locale, source.Revision, cover); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrChanged
		}
		return "", err
	}
	now := s.now().UTC()
	day := now.Format(time.DateOnly)
	if _, err = tx.ExecContext(ctx, "DELETE FROM editorial_usage WHERE day<?", now.AddDate(0, 0, -7).Format(time.DateOnly)); err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO editorial_usage(day,requests) VALUES(?,1)
		ON CONFLICT(day) DO UPDATE SET requests=requests+1 WHERE requests<?`, day, limit)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrQuota
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return string(token), nil
}

func validateRequest(in SourceRequest) bool {
	if !locale.Valid(in.Locale) || !locale.Valid(in.Target) || !media.ID.MatchString(in.Revision) ||
		len(in.Fields) < 1 || len(in.Fields) > 4 {
		return false
	}
	seen := map[string]bool{}
	for _, field := range in.Fields {
		if seen[field] || !slices.Contains([]string{"title", "summary", "excerpt", "cover"}, field) {
			return false
		}
		seen[field] = true
	}
	switch in.Kind {
	case "translation":
		return in.Locale != in.Target && len(in.Fields) == 1 && !seen["cover"]
	case "alt_text":
		return in.Locale == in.Target && seen["cover"]
	case "summary", "seo", "social":
		return in.Locale == in.Target && !seen["cover"]
	default:
		return false
	}
}

func (s *Service) Suggest(ctx context.Context, in SourceRequest) (Output, error) {
	if !validateRequest(in) {
		return Output{}, ErrInvalid
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Output{}, ErrBusy
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		return Output{}, err
	}
	if !settings.Enabled || !settings.Configured || s.provider == nil {
		return Output{}, ErrDisabled
	}
	if settings.Used >= settings.DailyLimit {
		return Output{}, ErrQuota
	}
	excerpt, cover := slices.Contains(in.Fields, "excerpt"), slices.Contains(in.Fields, "cover")
	source, err := s.Content.EditorialContext(ctx, in.Key, in.Locale, in.Revision, excerpt, cover)
	if errors.Is(err, content.ErrReview) {
		return Output{}, ErrChanged
	}
	if err != nil {
		return Output{}, err
	}
	input := Input{Kind: in.Kind, Locale: in.Locale, Target: in.Target}
	for _, field := range in.Fields {
		switch field {
		case "title":
			input.Title = source.Title
		case "summary":
			input.Summary = source.Summary
		case "excerpt":
			input.Excerpt = source.Excerpt
		case "cover":
			if source.Cover == "" || !strings.HasPrefix(s.origin, "https://") {
				return Output{}, ErrInvalid
			}
			input.ImageURL = s.origin + source.Cover
		}
	}
	if in.Kind == "translation" {
		input.Field = in.Fields[0]
	}
	if input.Title == "" && input.Summary == "" && input.Excerpt == "" && input.ImageURL == "" {
		return Output{}, ErrInvalid
	}
	credential, err := s.reserve(ctx, settings.Version, in, source.CoverID)
	if err != nil {
		return Output{}, err
	}
	out, err := s.provider.Suggest(ctx, credential, input)
	if err != nil {
		return Output{}, err
	}
	return validOutput(input, out)
}

func validOutput(in Input, out Output) (Output, error) {
	out.Text = strings.TrimSpace(out.Text)
	out.Description = strings.TrimSpace(out.Description)
	var limit int
	switch in.Kind {
	case "alt_text":
		limit = 300
	case "summary":
		limit = 500
	case "seo":
		limit = 120
	case "social":
		limit = 500
	case "translation":
		limit = 1800
		if in.Field == "title" {
			limit = 200
		} else if in.Field == "summary" {
			limit = 500
		}
	}
	multiline := in.Kind == "social" || in.Kind == "translation" && in.Field == "excerpt"
	if !safeText(out.Text, limit, multiline) || in.Kind == "seo" && !safeText(out.Description, 320, false) ||
		in.Kind != "seo" && out.Description != "" {
		return Output{}, ErrOutput
	}
	return out, nil
}

func safeText(value string, limit int, multiline bool) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && !(multiline && r == '\n') })
}

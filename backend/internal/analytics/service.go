// Package analytics counts public activity in day and pathname buckets.
package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/locale"
)

const Slug = "analytics"
const (
	PageView       = "page_view"
	LeadSubmitted  = "lead_submitted"
	OptIn          = "opt_in"
	UnlessDeclined = "unless_declined"
	Allow          = "allow"
	Decline        = "decline"
	FlushInterval  = 30 * time.Second
	maxBuckets     = 4096
	maxDayCount    = 1_000_000_000
)

var ErrInvalid = errors.New("invalid analytics settings")

type Settings struct {
	Version       int      `json:"version"`
	Enabled       bool     `json:"enabled"`
	Consent       string   `json:"consent"`
	RetentionDays int      `json:"retention_days"`
	Exclusions    []string `json:"exclusions"`
}

// Privacy contains only counting eligibility. The collector receives neither
// identifiers nor the request attributes used to make this decision.
type Privacy struct {
	Choice     string
	Disallowed bool
}

// Bucket exposes only an aggregate for a UTC day, a canonical public path,
// and a registered event. Adapters consume these bounded batches.
type Bucket struct {
	Day, Path, Event string
	Count            int64
}

type Sink interface {
	WriteBatch(context.Context, string, []Bucket) error
}

type bucketKey struct{ day, path, event string }

type Service struct {
	database *sql.DB
	sink     Sink
	now      func() time.Time
	write    sync.Mutex
	mu       sync.Mutex
	settings Settings
	pending  map[bucketKey]int64
	prunedOn string
}

func Open(ctx context.Context, database *sql.DB) (*Service, error) {
	s := &Service{database: database, sink: sqliteSink{database}, now: time.Now, pending: map[bucketKey]int64{}}
	var raw string
	err := database.QueryRowContext(ctx, `SELECT version,enabled,consent,retention_days,exclusions FROM analytics_settings WHERE id=1`).
		Scan(&s.settings.Version, &s.settings.Enabled, &s.settings.Consent, &s.settings.RetentionDays, &raw)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal([]byte(raw), &s.settings.Exclusions) != nil {
		return nil, ErrInvalid
	}
	settings, err := normalize(s.settings)
	if err != nil {
		return nil, err
	}
	s.settings = settings
	return s, nil
}

func (*Service) Slug() string { return Slug }

func (s *Service) Register(r *extension.Registrar) error {
	if err := r.Contribution(extension.Contribution{Kind: extension.ProviderAdapter, Key: "local", Target: "analytics", Version: 1,
		Label: extension.LocalizedText{"en": "Local aggregates", "fr": "Agrégats locaux", "ar": "إحصاءات مجمّعة محلية"}}, nil); err != nil {
		return err
	}
	return r.Job(extension.Job{Name: "flush", Every: FlushInterval, Timeout: 10 * time.Second, Run: func(ctx context.Context) error {
		if err := s.Flush(ctx); err != nil {
			return errors.New("analytics batch failed")
		}
		return nil
	}})
}

func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.settings
	out.Exclusions = slices.Clone(out.Exclusions)
	return out
}

// Path accepts a locale-rooted public route and returns its canonical escaped
// pathname. Query strings, fragments, authority and encoded separators are rejected.
func Path(raw string) (string, bool) {
	if len(raw) > 512 || !strings.HasPrefix(raw, "/") || strings.ContainsAny(raw, "?#\\") {
		return "", false
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "?#\\") || strings.ContainsFunc(decoded, unicode.IsControl) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(decoded, "/"), "/")
	if len(parts) > 4 || !locale.Valid(parts[0]) {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	if len(parts) > 1 && !slices.Contains([]string{"about", "writing", "search", "learn", "docs", "doc", "articles", "pages", "store"}, parts[1]) {
		return "", false
	}
	out := (&url.URL{Path: decoded}).EscapedPath()
	return out, len(out) <= 512
}

func normalize(in Settings) (Settings, error) {
	if in.Version < 1 || (in.Consent != OptIn && in.Consent != UnlessDeclined) ||
		in.RetentionDays < 1 || in.RetentionDays > 90 || len(in.Exclusions) > 64 {
		return Settings{}, ErrInvalid
	}
	out := in
	out.Exclusions = []string{}
	seen := map[string]bool{}
	for _, rule := range in.Exclusions {
		rule = strings.TrimSpace(rule)
		base, prefix := strings.CutSuffix(rule, "/*")
		path, ok := Path(base)
		if !ok || strings.Contains(base, "*") {
			return Settings{}, ErrInvalid
		}
		if prefix {
			path += "/*"
		}
		if !seen[path] {
			out.Exclusions = append(out.Exclusions, path)
			seen[path] = true
		}
	}
	slices.Sort(out.Exclusions)
	return out, nil
}

func (settings Settings) Excluded(path string) bool {
	for _, rule := range settings.Exclusions {
		base, prefix := strings.CutSuffix(rule, "/*")
		if path == base || (prefix && strings.HasPrefix(path, base+"/")) {
			return true
		}
	}
	return false
}

func (settings Settings) Allows(privacy Privacy) bool {
	return settings.Enabled && !privacy.Disallowed && privacy.Choice != Decline &&
		(settings.Consent == UnlessDeclined || privacy.Choice == Allow)
}

func (s *Service) Update(ctx context.Context, in Settings) (Settings, error) {
	next, err := normalize(in)
	if err != nil {
		return Settings{}, err
	}
	raw, err := json.Marshal(next.Exclusions)
	if err != nil {
		return Settings{}, err
	}
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `UPDATE analytics_settings SET version=version+1,enabled=?,consent=?,retention_days=?,exclusions=?
		WHERE id=1 AND version=? RETURNING version`, next.Enabled, next.Consent, next.RetentionDays, string(raw), in.Version).Scan(&next.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, db.ErrVersionConflict
	}
	if err != nil {
		return Settings{}, err
	}
	now := s.now()
	if _, err = tx.ExecContext(ctx, "DELETE FROM analytics_daily WHERE day<?", cutoff(now, next.RetentionDays)); err != nil {
		return Settings{}, err
	}
	if err = tx.Commit(); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = next
	clear(s.pending)
	s.prunedOn = day(now)
	out := next
	out.Exclusions = slices.Clone(next.Exclusions)
	return out, nil
}

func (s *Service) Record(event, rawPath string, privacy Privacy) {
	if event != PageView && event != LeadSubmitted {
		return
	}
	path, ok := Path(rawPath)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.settings.Allows(privacy) || s.settings.Excluded(path) {
		return
	}
	key := bucketKey{day(s.now()), path, event}
	if _, ok := s.pending[key]; ok || len(s.pending) < maxBuckets {
		s.pending[key] = min(s.pending[key]+1, maxDayCount)
	}
}

func day(now time.Time) string { return now.UTC().Format(time.DateOnly) }
func cutoff(now time.Time, retention int) string {
	return day(now.UTC().AddDate(0, 0, 1-retention))
}

func (s *Service) Flush(ctx context.Context) error {
	s.write.Lock()
	defer s.write.Unlock()
	now := s.now()
	today := day(now)
	s.mu.Lock()
	if len(s.pending) == 0 && s.prunedOn == today {
		s.mu.Unlock()
		return nil
	}
	pruneBefore := cutoff(now, s.settings.RetentionDays)
	pending := s.pending
	s.pending = map[bucketKey]int64{}
	s.mu.Unlock()

	batch := make([]Bucket, 0, len(pending))
	for key, count := range pending {
		if key.day >= pruneBefore {
			batch = append(batch, Bucket{key.day, key.path, key.event, count})
		}
	}
	if err := s.sink.WriteBatch(ctx, pruneBefore, batch); err != nil {
		s.mu.Lock()
		for key, count := range pending {
			if _, ok := s.pending[key]; key.day >= pruneBefore && (ok || len(s.pending) < maxBuckets) {
				s.pending[key] = min(s.pending[key]+count, maxDayCount)
			}
		}
		s.mu.Unlock()
		return err
	}
	s.prunedOn = today
	return nil
}

type sqliteSink struct{ database *sql.DB }

func (s sqliteSink) WriteBatch(ctx context.Context, before string, batch []Bucket) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM analytics_daily WHERE day<?", before); err != nil {
		return err
	}
	upsert, err := tx.PrepareContext(ctx, `INSERT INTO analytics_daily(day,event,path,count) VALUES(?,?,?,?)
		ON CONFLICT(day,event,path) DO UPDATE SET count=min(count+excluded.count,?)`)
	if err != nil {
		return err
	}
	defer upsert.Close()
	for _, item := range batch {
		if _, err = upsert.ExecContext(ctx, item.Day, item.Event, item.Path, item.Count, maxDayCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}

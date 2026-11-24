package analytics

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s, err := Open(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBatchCountingPrivacyAndExpiry(t *testing.T) {
	ctx := t.Context()
	s := fixture(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	const article = "/en/articles/visible"
	s.Record(PageView, article, Privacy{Choice: Allow})
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	settings := s.Settings()
	if settings.Enabled || settings.Consent != OptIn {
		t.Fatal("analytics default changed", settings)
	}
	settings.Enabled, settings.RetentionDays = true, 2
	settings.Exclusions = []string{"/en/about", "/ar/learn/*"}
	updated, err := s.Update(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, settings); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale update accepted", err)
	}
	for _, raw := range []string{article + "?email=private", "/en/admin", "/ar/learn/course", "/en/about"} {
		s.Record(PageView, raw, Privacy{Choice: Allow})
	}
	s.Record(PageView, article, Privacy{Choice: Allow, Disallowed: true})
	s.Record(PageView, article, Privacy{Choice: Decline})
	s.Record(PageView, article, Privacy{})
	s.Record("arbitrary", article, Privacy{Choice: Allow})
	s.Record(PageView, article, Privacy{Choice: Allow})
	s.Record(PageView, article, Privacy{Choice: Allow})
	s.Record(LeadSubmitted, article, Privacy{Choice: Allow})
	before, err := s.Report(ctx)
	if err != nil || before.Totals != (Totals{}) {
		t.Fatal("view wrote before batch flush", before, err)
	}
	if err = s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Report(ctx)
	if err != nil || after.Totals != (Totals{PageViews: 2, Leads: 1}) || len(after.Pages) != 1 || after.Pages[0].Path != article || after.Days[0].Day != "2026-11-24" {
		t.Fatal(after, err)
	}
	now = now.AddDate(0, 0, 1)
	if err = s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	report, err := s.Report(ctx)
	if err != nil || report.Totals != after.Totals {
		t.Fatal("count expired one day early", report, err)
	}
	now = now.AddDate(0, 0, 1)
	if err = s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.database.QueryRow("SELECT count(*) FROM analytics_daily").Scan(&n); err != nil || n != 0 {
		t.Fatal("count was not physically pruned", n, err)
	}
	updated.Consent = UnlessDeclined
	updated, err = s.Update(ctx, updated)
	if err != nil {
		t.Fatal(err)
	}
	s.Record(PageView, article, Privacy{})
	s.Record(PageView, article, Privacy{Choice: Decline})
	if err = s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	report, err = s.Report(ctx)
	if err != nil || report.Totals.PageViews != 1 {
		t.Fatal(report, err)
	}
	s.Record(PageView, article, Privacy{})
	updated.Enabled = false
	if _, err = s.Update(ctx, updated); err != nil {
		t.Fatal(err)
	}
	s.Record(PageView, article, Privacy{Choice: Allow})
	if err = s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	report, err = s.Report(ctx)
	if err != nil || report.Totals.PageViews != 1 {
		t.Fatal("disabled setting retained or recorded new counts", report, err)
	}
}

type failedSink struct{}

func (failedSink) WriteBatch(context.Context, string, []Bucket) error {
	return errors.New("temporary write failure")
}

func TestBatchRetriesWithoutDuplicatingCounts(t *testing.T) {
	s := fixture(t)
	settings := s.Settings()
	settings.Enabled, settings.Consent = true, UnlessDeclined
	if _, err := s.Update(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	s.Record(PageView, "/en", Privacy{})
	sqlite := s.sink
	s.sink = failedSink{}
	if err := s.Flush(t.Context()); err == nil {
		t.Fatal("flush succeeded against failing storage")
	}
	s.Record(PageView, "/en", Privacy{})
	s.sink = sqlite
	if err := s.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	report, err := s.Report(t.Context())
	if err != nil || report.Totals.PageViews != 2 {
		t.Fatal(report, err)
	}
}

func TestPathCanonicalization(t *testing.T) {
	path, ok := Path("/ar/pages/أهلا")
	if parsed, valid := Path(path); !ok || !valid || path == "/ar/pages/أهلا" || parsed != path {
		t.Fatal(path)
	}
	for _, raw := range []string{"/en/admin", "/en//pages", "/en/pages/..", "//example.test/en", "/en/pages/a%2fb", "/en/pages/a%5cb", "/en/pages/%0A", "/en/pages/a?x=1"} {
		if _, ok := Path(raw); ok {
			t.Fatal("accepted ineligible path", raw)
		}
	}
}

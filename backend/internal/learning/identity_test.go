package learning

import (
	"context"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIdentityHashIsolationExpiryResetAndBounds(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	database, e := db.Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	s := NewIdentity(database, 180, 2)
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	first, a, e := s.Create(ctx, "one")
	if e != nil {
		t.Fatal(e)
	}
	second, b, e := s.Create(ctx, "two")
	if e != nil || a.ID == b.ID || first == second {
		t.Fatal(e)
	}
	var stored string
	database.QueryRow("SELECT token_hash FROM learning_identities WHERE id=?", a.ID).Scan(&stored)
	if stored == first || len(stored) != 64 {
		t.Fatal("raw token stored")
	}
	if !CheckCSRF(first, LearningCSRF(first)) || CheckCSRF(first, LearningCSRF(second)) {
		t.Fatal("CSRF crossed identities")
	}
	if _, _, e = s.Create(ctx, "three"); !errors.Is(e, ErrLimited) {
		t.Fatal(e)
	}
	if e = s.Reset(ctx, first); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Current(ctx, first, false); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	if _, e = s.Current(ctx, second, false); e != nil {
		t.Fatal(e)
	}
	now = now.Add(179 * 24 * time.Hour)
	if _, e = s.Current(ctx, second, true); e != nil {
		t.Fatal(e)
	}
	now = now.Add(181 * 24 * time.Hour)
	if _, e = s.Current(ctx, second, false); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
	n, e := s.Cleanup(ctx)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	for i := 0; i < 10; i++ {
		if !s.allow("same-ip") {
			t.Fatal(i)
		}
	}
	if s.allow("same-ip") {
		t.Fatal("unbounded creation")
	}
	now = now.Add(time.Minute)
	if !s.allow("same-ip") {
		t.Fatal("lifetime attempt cap")
	}
	if _, e = s.Current(ctx, strings.Repeat("a", 42), false); !errors.Is(e, ErrIdentity) {
		t.Fatal(e)
	}
}

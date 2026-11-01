package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDurabilityAndConfinement(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var f File
	if e = s.Exclusive(context.Background(), func() error {
		var e error
		f, _, e = s.WriteLocked(strings.Repeat("a", 32), "ar", []byte("# مرحباً"))
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if b, e := s.Read(f); e != nil || string(b) != "# مرحباً" {
		t.Fatal(string(b), e)
	}
	for _, p := range []string{"../outside.md", "/tmp/a.md", "content/../outside.md"} {
		bad := f
		bad.Key = p
		if _, e := s.Read(bad); !errors.Is(e, ErrIntegrity) {
			t.Fatal(p, e)
		}
	}
	os.WriteFile(filepath.Join(dir, f.Key), []byte("wrong"), 0600)
	if _, e = s.Read(f); e != ErrIntegrity {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(dir, f.Key))
	if _, e = s.Read(f); e != ErrIntegrity {
		t.Fatal(e)
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(dir, "content", strings.Repeat("b", 32)))
	if _, _, e = s.WriteLocked(strings.Repeat("b", 32), "en", nil); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestFailuresLeaveOnlyDetectableOrphans(t *testing.T) {
	for _, stage := range []string{"before_rename", "after_rename", "after_directory_sync"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			os.Chmod(dir, 0700)
			s, _ := Open(dir)
			defer s.Close()
			s.fault = func(p string) error {
				if p == stage {
					return errors.New("injected")
				}
				return nil
			}
			e := s.Exclusive(context.Background(), func() error { _, _, e := s.WriteLocked(strings.Repeat("a", 32), "en", []byte("body")); return e })
			if e == nil {
				t.Fatal("fault ignored")
			}
			r, e := s.InspectLocked(nil)
			if e != nil {
				t.Fatal(e)
			}
			want := 1
			if stage == "before_rename" {
				want = 0
			}
			if len(r.Orphans) != want {
				t.Fatal(r)
			}
		})
	}
}
func TestMaintenanceLockAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	a, _ := Open(dir)
	b, _ := Open(dir)
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := a.Exclusive(ctx, func() error { cancel(); return b.Exclusive(ctx, func() error { t.Fatal("lock bypass"); return nil }) }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

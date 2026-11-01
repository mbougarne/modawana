package storage

import (
	"context"
	"io/fs"
	"os"
	"path"
	"regexp"
	"time"
)

var temporaryKey = regexp.MustCompile(`^(?:content/[0-9a-f]{32}/(?:en|ar|fr)|uploads)/\.[0-9a-f]{32}\.tmp$`)

// CleanupOrphansLocked never removes metadata-backed files or recent writes.
// Scanning and removal are bounded; immutable history is retained indefinitely.
func (s *Store) CleanupOrphansLocked(ctx context.Context, refs []File, before time.Time) (int, error) {
	known := map[string]bool{}
	for _, f := range refs {
		known[f.Key] = true
	}
	removed := 0
	visited := 0
	for _, dir := range []string{"content", "uploads"} {
		e := fs.WalkDir(s.root.FS(), dir, func(p string, d fs.DirEntry, e error) error {
			if os.IsNotExist(e) && p == dir {
				return nil
			}
			if e != nil {
				return e
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			visited++
			if visited > 20000 || removed >= 500 {
				return fs.SkipAll
			}
			if d.Type()&os.ModeSymlink != 0 {
				return ErrStorage
			}
			if d.IsDir() || known[p] {
				return nil
			}
			if !keyPattern.MatchString(p) && !uploadKey.MatchString(p) && !temporaryKey.MatchString(p) {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || !info.ModTime().Before(before) {
				return nil
			}
			if !s.safe(p) {
				return ErrStorage
			}
			if e = s.root.Remove(p); e != nil {
				return e
			}
			removed++
			return s.syncDir(path.Dir(p))
		})
		if e != nil {
			return removed, e
		}
	}
	return removed, nil
}

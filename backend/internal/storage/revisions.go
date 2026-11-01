// Package storage keeps immutable bodies outside SQLite and HTTP asset roots.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/db"
)

const MaxMarkdown = 2 << 20

var ErrStorage = errors.New("revision storage unavailable")
var ErrIntegrity = errors.New("revision missing or corrupt")
var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var keyPattern = regexp.MustCompile(`^content/[0-9a-f]{32}/(en|ar|fr)/[0-9a-f]{32}\.md$`)

type File struct {
	Key    string
	SHA256 string
	Size   int64
}
type Store struct {
	root  *os.Root
	fault func(string) error
}

func Open(dataRoot string) (*Store, error) {
	info, e := os.Lstat(dataRoot)
	if e != nil || !info.IsDir() || info.Mode()&0077 != 0 {
		return nil, ErrStorage
	}
	r, e := os.OpenRoot(dataRoot)
	if e != nil {
		return nil, ErrStorage
	}
	return &Store{root: r}, nil
}
func (s *Store) Close() error { return s.root.Close() }

// Exclusive acquires the cross-process maintenance lock. Every writer, backup and
// collector must hold this lock through its file/SQLite operation. No GC is automatic.
func (s *Store) Exclusive(ctx context.Context, fn func() error) error {
	f, e := s.root.OpenFile("maintenance.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return ErrStorage
	}
	defer f.Close()
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
			return ErrStorage
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func (s *Store) point(stage string) error {
	if s.fault != nil {
		return s.fault(stage)
	}
	return nil
}
func (s *Store) syncDir(p string) error {
	f, e := s.root.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (s *Store) safe(p string) bool {
	current := ""
	for _, part := range strings.Split(p, "/") {
		current = path.Join(current, part)
		i, e := s.root.Lstat(current)
		if e != nil || i.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// WriteLocked requires Exclusive for the full subsequent database transaction.
// Only generated IDs can name a file. A fresh ID is allocated even for identical bytes.
func (s *Store) WriteLocked(item, locale string, body []byte) (File, string, error) {
	var out File
	if !idPattern.MatchString(item) || (locale != "en" && locale != "ar" && locale != "fr") || len(body) > MaxMarkdown || !utf8.Valid(body) {
		return out, "", ErrStorage
	}
	revision, e := db.NewID()
	if e != nil {
		return out, "", ErrStorage
	}
	return s.writeLocked("content/"+item+"/"+locale, revision, ".md", body)
}
func (s *Store) writeLocked(dir, revision, extension string, body []byte) (File, string, error) {
	var out File
	var e error
	current := ""
	for _, part := range strings.Split(dir, "/") {
		parent := current
		if parent == "" {
			parent = "."
		}
		current = path.Join(current, part)
		e = s.root.Mkdir(current, 0700)
		if e != nil && !os.IsExist(e) {
			return out, "", ErrStorage
		}
		if !s.safe(current) {
			return out, "", ErrStorage
		}
		if e = s.syncDir(parent); e != nil {
			return out, "", ErrStorage
		}
	}
	temp := dir + "/." + revision + ".tmp"
	key := dir + "/" + revision + extension
	f, e := s.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return out, "", ErrStorage
	}
	defer s.root.Remove(temp)
	if _, e = f.Write(body); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return out, "", ErrStorage
	}
	if e = s.point("before_rename"); e != nil {
		return out, "", e
	}
	if _, e = s.root.Lstat(key); !os.IsNotExist(e) {
		return out, "", ErrStorage
	}
	if e = s.root.Rename(temp, key); e != nil {
		return out, "", ErrStorage
	}
	if e = s.point("after_rename"); e != nil {
		return out, "", e
	}
	if e = s.syncDir(dir); e != nil {
		return out, "", ErrStorage
	}
	if e = s.point("after_directory_sync"); e != nil {
		return out, "", e
	}
	sum := sha256.Sum256(body)
	return File{key, hex.EncodeToString(sum[:]), int64(len(body))}, revision, nil
}
func (s *Store) Read(f File) ([]byte, error) {
	if !keyPattern.MatchString(f.Key) || f.Size < 0 || f.Size > MaxMarkdown || !s.safe(f.Key) {
		return nil, ErrIntegrity
	}
	h, e := s.root.OpenFile(f.Key, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, ErrIntegrity
	}
	defer h.Close()
	info, e := h.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return nil, ErrIntegrity
	}
	b, e := io.ReadAll(io.LimitReader(h, MaxMarkdown+1))
	sum := sha256.Sum256(b)
	if e != nil || int64(len(b)) != f.Size || hex.EncodeToString(sum[:]) != f.SHA256 || !utf8.Valid(b) {
		return nil, ErrIntegrity
	}
	return b, nil
}

type Inspection struct {
	Orphans []string
	Invalid []string
}

// InspectLocked is read-only. References include retained history, not just live pointers.
// Call under Exclusive; backup/GC must never race file write plus metadata commit.
func (s *Store) InspectLocked(refs []File) (Inspection, error) {
	result := Inspection{Orphans: []string{}, Invalid: []string{}}
	known := map[string]bool{}
	for _, f := range refs {
		known[f.Key] = true
		if _, e := s.Read(f); e != nil {
			result.Invalid = append(result.Invalid, f.Key)
		}
	}
	e := fs.WalkDir(s.root.FS(), "content", func(p string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) && p == "content" {
			return nil
		}
		if e != nil {
			return ErrStorage
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrStorage
		}
		if !d.IsDir() && !known[p] {
			result.Orphans = append(result.Orphans, p)
		}
		return nil
	})
	return result, e
}

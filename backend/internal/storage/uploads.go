package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"syscall"

	"github.com/mbougarne/modawana/backend/internal/db"
)

const MaxUpload = 50 << 20

var uploadKey = regexp.MustCompile(`^uploads/[0-9a-f]{32}\.bin$`)

// WriteUploadLocked shares the revision/backup lock and durability protocol.
func (s *Store) WriteUploadLocked(ctx context.Context, body []byte) (File, string, error) {
	if ctx.Err() != nil {
		return File{}, "", ctx.Err()
	}
	if len(body) == 0 || len(body) > MaxUpload {
		return File{}, "", ErrStorage
	}
	id, e := db.NewID()
	if e != nil {
		return File{}, "", e
	}
	f, id, e := s.writeLocked("uploads", id, ".bin", body)
	if e == nil {
		e = ctx.Err()
	}
	return f, id, e
}

// OpenUpload verifies the whole immutable file before any bytes or range are served.
func (s *Store) OpenUpload(f File) (*os.File, error) {
	if !uploadKey.MatchString(f.Key) || f.Size < 1 || f.Size > MaxUpload || !s.safe(f.Key) {
		return nil, ErrIntegrity
	}
	h, e := s.root.OpenFile(f.Key, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, ErrIntegrity
	}
	valid := false
	defer func() {
		if !valid {
			h.Close()
		}
	}()
	info, e := h.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return nil, ErrIntegrity
	}
	hash := sha256.New()
	n, e := io.Copy(hash, io.LimitReader(h, MaxUpload+1))
	if e != nil || n != f.Size || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		return nil, ErrIntegrity
	}
	if _, e = h.Seek(0, 0); e != nil {
		return nil, ErrIntegrity
	}
	valid = true
	return h, nil
}

// RemoveUploadLocked is for explicitly unreferenced files after metadata commit.
// A failed unlink leaves an orphan for coordinated maintenance, never a live missing file.
func (s *Store) RemoveUploadLocked(key string) error {
	if !uploadKey.MatchString(key) || !s.safe(key) {
		return ErrStorage
	}
	if e := s.root.Remove(key); e != nil {
		return e
	}
	return s.syncDir("uploads")
}

package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
)

// InstallLocked copies a newly generated staging file under the maintenance lock.
// Existing files are never overwritten, and bytes must match the staged metadata.
func (s *Store) InstallLocked(f File, source io.Reader) error {
	if (!keyPattern.MatchString(f.Key) && !uploadKey.MatchString(f.Key)) || f.Size < 0 || f.Size > MaxUpload {
		return ErrStorage
	}
	current := ""
	for _, part := range strings.Split(path.Dir(f.Key), "/") {
		parent := current
		if parent == "" {
			parent = "."
		}
		current = path.Join(current, part)
		if e := s.root.Mkdir(current, 0700); e != nil && !os.IsExist(e) {
			return e
		}
		if !s.safe(current) {
			return ErrStorage
		}
		if e := s.syncDir(parent); e != nil {
			return e
		}
	}
	w, e := s.root.OpenFile(f.Key, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(w, h), io.LimitReader(source, f.Size+1))
	if e == nil {
		e = w.Sync()
	}
	w.Close()
	if e != nil || n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		s.root.Remove(f.Key)
		return ErrIntegrity
	}
	return s.syncDir(path.Dir(f.Key))
}

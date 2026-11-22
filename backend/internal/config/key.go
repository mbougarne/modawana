package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var errKey = errors.New("encryption key unavailable")

// ReadAuthKey reads the operator key on demand from outside the data root.
func (c Config) ReadAuthKey() ([]byte, error) {
	p := c.AuthKeyFile
	if !filepath.IsAbs(p) {
		return nil, errKey
	}
	resolved, e := filepath.EvalSymlinks(p)
	if e != nil {
		return nil, errKey
	}
	dataRoot, e := filepath.EvalSymlinks(c.DataRoot)
	if e != nil {
		return nil, errKey
	}
	rel, e := filepath.Rel(dataRoot, resolved)
	if e != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return nil, errKey
	}
	f, e := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, errKey
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return nil, errKey
	}
	key := make([]byte, 32)
	if _, e = f.ReadAt(key, 0); e != nil {
		return nil, errKey
	}
	return key, nil
}

package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// PrivateText opens a bounded operator credential outside the data root.
func PrivateText(path, dataRoot string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errKey
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errKey
	}
	root, err := filepath.EvalSymlinks(dataRoot)
	if err != nil {
		return "", errKey
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return "", errKey
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", errKey
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
		return "", errKey
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1025))
	value := strings.TrimSpace(string(raw))
	if err != nil || len(raw) > 1024 || value == "" || strings.ContainsFunc(value, func(r rune) bool { return r < 33 || r > 126 }) {
		return "", errKey
	}
	return value, nil
}

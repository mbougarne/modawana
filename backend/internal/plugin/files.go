package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
)

// ErrFiles reports an unreadable or out-of-bounds plugin directory.
var ErrFiles = errors.New("plugin files unavailable")

// ErrChecksum reports plugin files that no longer match the approved install.
var ErrChecksum = errors.New("plugin files do not match the approved checksums")

const maxPluginFiles = 256
const maxModuleBytes = 32 << 20
const maxTotalBytes = 64 << 20

// File is one checksummed plugin file, recorded in SQLite at install. Paths
// are slash-separated and relative to the plugin directory.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Directory is one validated plugin directory: its manifest, the raw manifest
// document, the module bytes, and the checksums of every file.
type Directory struct {
	Manifest    Manifest
	RawManifest []byte
	Module      []byte
	Files       []File
}

// ReadDirectory loads and validates plugins/{slug} under the data root. Every
// open goes through os.Root, so symlinks and traversal cannot escape the
// plugin directory; irregular files are refused rather than followed.
func ReadDirectory(pluginsDir, slug string) (Directory, error) {
	var out Directory
	if !slugPattern.MatchString(slug) {
		return out, fmt.Errorf("%w: slug", ErrManifest)
	}
	root, err := os.OpenRoot(pluginsDir)
	if err != nil {
		return out, ErrFiles
	}
	defer root.Close()
	dir, err := root.OpenRoot(slug)
	if err != nil {
		return out, ErrFiles
	}
	defer dir.Close()
	if out.Files, err = checksumAll(dir); err != nil {
		return out, err
	}
	if out.RawManifest, err = readBounded(dir, "manifest.json", manifestLimit); err != nil {
		return out, err
	}
	if out.Manifest, err = ParseManifest(out.RawManifest, slug); err != nil {
		return out, err
	}
	if out.Module, err = readBounded(dir, out.Manifest.Module, maxModuleBytes); err != nil {
		return out, err
	}
	return out, nil
}

func checksumAll(dir *os.Root) ([]File, error) {
	var files []File
	var total int64
	err := fs.WalkDir(dir.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return ErrFiles
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%w: %q is not a regular file", ErrFiles, name)
		}
		if len(files) >= maxPluginFiles {
			return fmt.Errorf("%w: more than %d files", ErrFiles, maxPluginFiles)
		}
		f, e := dir.Open(name)
		if e != nil {
			return ErrFiles
		}
		defer f.Close()
		hash := sha256.New()
		size, e := io.Copy(hash, io.LimitReader(f, maxTotalBytes+1))
		if e != nil {
			return ErrFiles
		}
		total += size
		if total > maxTotalBytes {
			return fmt.Errorf("%w: plugin exceeds %d bytes", ErrFiles, maxTotalBytes)
		}
		files = append(files, File{Path: path.Clean(name), SHA256: hex.EncodeToString(hash.Sum(nil)), Size: size})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func readBounded(dir *os.Root, name string, limit int64) ([]byte, error) {
	f, err := dir.Open(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q missing", ErrFiles, name)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, ErrFiles
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: %q exceeds %d bytes", ErrFiles, name, limit)
	}
	return raw, nil
}

// SameFiles reports whether two checksum lists describe identical contents.
func SameFiles(a, b []File) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

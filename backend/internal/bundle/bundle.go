// Package bundle handles bounded, checksum-verified archives without trusting paths.
package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"
)

const MaxBytes int64 = 2 << 30
const MaxFile int64 = 256 << 20
const MaxFiles = 20000
const MaxManifest = 8 << 20

var ErrInvalid = errors.New("invalid, incompatible or corrupt bundle")
var safePath = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/._-]{0,239}$`)

type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Schema  int     `json:"schema"`
	Kind    string  `json:"kind"`
	Created string  `json:"created_at"`
	Files   []Entry `json:"files"`
}

func ValidPath(p string) bool {
	return safePath.MatchString(p) && path.Clean(p) == p && !filepath.IsAbs(p) && p != "manifest.json"
}
func Decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func Write(out io.Writer, kind string, root string, paths []string) error {
	z := zip.NewWriter(out)
	m := Manifest{Schema: 1, Kind: kind, Created: time.Now().UTC().Format(time.RFC3339), Files: []Entry{}}
	seen := map[string]bool{}
	var total int64
	for _, p := range paths {
		if !ValidPath(p) || seen[p] || len(m.Files) >= MaxFiles {
			return ErrInvalid
		}
		seen[p] = true
		f, e := os.Open(filepath.Join(root, filepath.FromSlash(p)))
		if e != nil {
			return e
		}
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() > MaxFile {
			f.Close()
			return ErrInvalid
		}
		total += st.Size()
		if total > MaxBytes {
			f.Close()
			return ErrInvalid
		}
		h := sha256.New()
		w, e := z.CreateHeader(&zip.FileHeader{Name: p, Method: zip.Deflate})
		if e != nil {
			f.Close()
			return e
		}
		n, e := io.Copy(io.MultiWriter(w, h), io.LimitReader(f, st.Size()+1))
		f.Close()
		if e != nil || n != st.Size() {
			return ErrInvalid
		}
		m.Files = append(m.Files, Entry{p, n, hex.EncodeToString(h.Sum(nil))})
	}
	b, e := json.Marshal(m)
	if e != nil || len(b) > MaxManifest {
		return ErrInvalid
	}
	w, e := z.Create("manifest.json")
	if e != nil {
		return e
	}
	if _, e = w.Write(b); e != nil {
		return e
	}
	return z.Close()
}

// Read extracts only declared regular files into a new private temporary directory.
// The caller removes the directory after use. No archive path reaches a live data root.
func Read(source string, kind string) (string, Manifest, error) {
	var m Manifest
	z, e := zip.OpenReader(source)
	if e != nil {
		return "", m, ErrInvalid
	}
	defer z.Close()
	if len(z.File) > MaxFiles+1 {
		return "", m, ErrInvalid
	}
	seen := map[string]*zip.File{}
	var raw []byte
	for _, f := range z.File {
		if seen[f.Name] != nil || !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(MaxFile) {
			return "", m, ErrInvalid
		}
		seen[f.Name] = f
		if f.Name == "manifest.json" {
			if f.UncompressedSize64 > MaxManifest {
				return "", m, ErrInvalid
			}
			r, e := f.Open()
			if e != nil {
				return "", m, ErrInvalid
			}
			raw, e = io.ReadAll(io.LimitReader(r, MaxManifest+1))
			r.Close()
			if e != nil {
				return "", m, e
			}
		} else if !ValidPath(f.Name) {
			return "", m, ErrInvalid
		}
	}
	if Decode(raw, &m) != nil || m.Schema != 1 || m.Kind != kind || len(m.Files)+1 != len(seen) {
		return "", m, ErrInvalid
	}
	root, e := os.MkdirTemp("", "blog-bundle-")
	if e != nil {
		return "", m, e
	}
	good := false
	defer func() {
		if !good {
			os.RemoveAll(root)
		}
	}()
	var total int64
	used := map[string]bool{}
	for _, entry := range m.Files {
		f := seen[entry.Path]
		if !ValidPath(entry.Path) || used[entry.Path] || f == nil || entry.Size < 0 || entry.Size > MaxFile || uint64(entry.Size) != f.UncompressedSize64 || len(entry.SHA256) != 64 {
			return "", m, ErrInvalid
		}
		used[entry.Path] = true
		total += entry.Size
		if total > MaxBytes {
			return "", m, ErrInvalid
		}
		p := filepath.Join(root, filepath.FromSlash(entry.Path))
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return "", m, e
		}
		w, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", m, e
		}
		r, e := f.Open()
		if e != nil {
			w.Close()
			return "", m, e
		}
		h := sha256.New()
		n, e := io.Copy(io.MultiWriter(w, h), io.LimitReader(r, entry.Size+1))
		r.Close()
		if e == nil {
			e = w.Sync()
		}
		w.Close()
		if e != nil || n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return "", m, ErrInvalid
		}
	}
	good = true
	return root, m, nil
}

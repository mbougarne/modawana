package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"regexp"
	"strings"
	"time"
)

type Manifest struct {
	Version int      `json:"version"`
	Scripts []string `json:"scripts"`
	Styles  []string `json:"styles"`
	Assets  []string `json:"assets"`
}
type asset struct {
	body        []byte
	modified    time.Time
	contentType string
}
type Assets struct {
	Manifest Manifest
	files    map[string]asset
}

var assetName = regexp.MustCompile(`^/assets/[a-zA-Z0-9_-][a-zA-Z0-9_.-]*\.[a-f0-9]{16,64}\.(js|css|woff2|png|jpe?g|webp|svg)$`)

// LoadAssets snapshots only build-manifest assets. Data directories are never mounted.
func LoadAssets(dir string) (*Assets, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.New("frontend build unavailable")
	}
	defer root.Close()
	f, err := root.Open("manifest.json")
	if err != nil {
		return nil, errors.New("manifest unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("invalid manifest")
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&m) != nil || dec.Decode(new(any)) != io.EOF || m.Version != 1 || len(m.Scripts) == 0 || len(m.Assets) == 0 || len(m.Assets) > 512 {
		return nil, errors.New("invalid manifest")
	}
	a := &Assets{Manifest: m, files: map[string]asset{}}
	var total int64
	for _, name := range m.Assets {
		if !assetName.MatchString(name) {
			return nil, errors.New("invalid asset name")
		}
		if _, ok := a.files[name]; ok {
			return nil, errors.New("duplicate asset")
		}
		rel := strings.TrimPrefix(name, "/")
		info, e := root.Lstat(rel)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return nil, errors.New("invalid asset file")
		}
		total += info.Size()
		if total > 128<<20 {
			return nil, errors.New("asset budget exceeded")
		}
		f, e := root.Open(rel)
		if e != nil {
			return nil, errors.New("asset unavailable")
		}
		b, e := io.ReadAll(io.LimitReader(f, (16<<20)+1))
		_ = f.Close()
		if e != nil || int64(len(b)) != info.Size() {
			return nil, errors.New("asset changed during load")
		}
		ext := name[strings.LastIndex(name, "."):]
		contentType := mime.TypeByExtension(ext)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		a.files[name] = asset{b, info.ModTime(), contentType}
	}
	for _, group := range []struct {
		paths []string
		ext   string
	}{{m.Scripts, ".js"}, {m.Styles, ".css"}} {
		for _, name := range group.paths {
			if _, ok := a.files[name]; !ok || !strings.HasSuffix(name, group.ext) {
				return nil, errors.New("invalid entrypoint")
			}
		}
	}
	return a, nil
}

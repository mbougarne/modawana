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
	// Islands are host-owned interactivity bundles for server-rendered pages
	// (ADR 0002), keyed by island name; each entry lists manifest assets.
	Islands map[string][]string `json:"islands,omitempty"`
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
	if len(m.Islands) > 16 {
		return nil, errors.New("invalid manifest")
	}
	for island, paths := range m.Islands {
		if !islandName.MatchString(island) || len(paths) == 0 || len(paths) > 8 {
			return nil, errors.New("invalid island")
		}
		for _, name := range paths {
			if _, ok := a.files[name]; !ok || (!strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css")) {
				return nil, errors.New("invalid island entrypoint")
			}
		}
	}
	return a, nil
}

var islandName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Island returns an island's script and style assets, or ok=false when the
// loaded frontend build does not provide it.
func (a *Assets) Island(name string) (scripts, styles []string, ok bool) {
	paths, ok := a.Manifest.Islands[name]
	if !ok {
		return nil, nil, false
	}
	for _, p := range paths {
		if strings.HasSuffix(p, ".js") {
			scripts = append(scripts, p)
		} else {
			styles = append(styles, p)
		}
	}
	return scripts, styles, true
}

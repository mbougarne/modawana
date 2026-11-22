package theme

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image/png"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"syscall"
	"text/template"
	"text/template/parse"
	"unicode"

	"github.com/mbougarne/modawana/backend/internal/bundle"
)

var ErrPack = errors.New("invalid theme pack")
var ErrPackFiles = errors.New("theme pack file unavailable")
var ErrPackChanged = errors.New("theme pack checksum changed")

const maxManifestBytes = 192 << 10
const maxBannerBytes = 96 << 10

var packChecksum = regexp.MustCompile(`^[a-f0-9]{64}$`)
var packID = regexp.MustCompile(`^pack-[a-z][a-z0-9-]{0,30}$`)

func IsPack(id string) bool { return packID.MatchString(id) }

func PackPath(id string) string { return path.Join("themes", id, "theme.json") }

type manifest struct {
	Version int    `json:"api_version"`
	Name    string `json:"name"`
	Layout  string `json:"layout"`
	CSS     string `json:"css"`
	Banner  []byte `json:"banner_png,omitempty"`
}

type Pack struct {
	Appearance
	Name, Checksum string
	BannerPNG      []byte
	Raw            []byte
}

func ReadPack(dataRoot, id string) (Pack, error) {
	if !IsPack(id) {
		return Pack{}, ErrPack
	}
	root, err := os.OpenRoot(dataRoot)
	if err != nil {
		return Pack{}, ErrPackFiles
	}
	defer root.Close()
	for _, dir := range []string{"themes", path.Join("themes", id)} {
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			return Pack{}, ErrPackFiles
		}
	}
	file, err := root.OpenFile(PackPath(id), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Pack{}, ErrPackFiles
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxManifestBytes {
		return Pack{}, ErrPackFiles
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return Pack{}, ErrPackFiles
	}
	return parsePack(id, raw)
}

func ReadVerifiedPack(dataRoot, id, checksum string) (Pack, error) {
	pack, err := ReadPack(dataRoot, id)
	if err == nil && pack.Checksum != checksum {
		err = ErrPackChanged
	}
	return pack, err
}

func parsePack(id string, raw []byte) (Pack, error) {
	var m manifest
	if bundle.Decode(raw, &m) != nil || m.Version != 1 || strings.TrimSpace(m.Name) != m.Name ||
		m.Name == "" || len(m.Name) > 120 || strings.ContainsFunc(m.Name, unicode.IsControl) {
		return Pack{}, ErrPack
	}
	sum := sha256.Sum256(raw)
	checksum := hex.EncodeToString(sum[:])
	appearance := Appearance{ID: id}
	if len(m.Banner) > 0 {
		config, err := png.DecodeConfig(bytes.NewReader(m.Banner))
		if err != nil || len(m.Banner) > maxBannerBytes || config.Width < 1 || config.Width > 1600 ||
			config.Height < 1 || config.Height > 800 {
			return Pack{}, ErrPack
		}
		appearance.BannerURL = "/theme-assets/" + id + "/" + checksum + "/banner.png"
	}
	layout, err := parseLayout(m.Layout, appearance.BannerURL != "")
	if err != nil {
		return Pack{}, err
	}
	css, err := scopeCSS(id, m.CSS)
	if err != nil {
		return Pack{}, err
	}
	appearance.Layout, appearance.CSS = layout, css
	return Pack{Appearance: appearance, Name: m.Name, Checksum: checksum, BannerPNG: m.Banner, Raw: raw}, nil
}

// parseLayout compiles an allowlisted sequence of host partials. Pack source
// never executes and cannot supply markup, data expressions, or functions.
func parseLayout(source string, banner bool) ([]string, error) {
	if len(source) > 1024 {
		return nil, ErrPack
	}
	parsed, err := template.New("layout").Parse(source)
	if err != nil || len(parsed.Templates()) != 1 {
		return nil, ErrPack
	}
	allowed := map[string]bool{"header": true, "content": true, "footer": true, "banner": banner}
	var slots []string
	for _, node := range parsed.Tree.Root.Nodes {
		switch n := node.(type) {
		case *parse.TextNode:
			if strings.TrimSpace(string(n.Text)) != "" {
				return nil, ErrPack
			}
		case *parse.TemplateNode:
			if !allowed[n.Name] || n.Pipe == nil || len(n.Pipe.Decl) != 0 || len(n.Pipe.Cmds) != 1 ||
				len(n.Pipe.Cmds[0].Args) != 1 || n.Pipe.Cmds[0].Args[0].Type() != parse.NodeDot {
				return nil, ErrPack
			}
			allowed[n.Name] = false
			slots = append(slots, n.Name)
		default:
			return nil, ErrPack
		}
	}
	for _, remaining := range allowed {
		if remaining {
			return nil, ErrPack
		}
	}
	return slots, nil
}

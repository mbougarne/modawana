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
	Version int                       `json:"api_version"`
	Name    string                    `json:"name"`
	Layout  string                    `json:"layout"`
	CSS     string                    `json:"css"`
	Banner  []byte                    `json:"banner_png,omitempty"`
	Layouts map[string]layoutManifest `json:"layouts,omitempty"`
	Assets  map[string][]byte         `json:"assets,omitempty"`
	Tokens  map[string]string         `json:"tokens,omitempty"`
}

type Pack struct {
	Appearance
	Name, Checksum string
	APIVersion     int
	BannerPNG      []byte
	assets         map[string][]byte
	Raw            []byte
}

func ReadPack(dataRoot, id string) (Pack, error) {
	if !IsPack(id) {
		return Pack{}, ErrPack
	}
	return readPack(dataRoot, id, PackPath(id))
}

func readPack(dataRoot, id, location string) (Pack, error) {
	root, err := os.OpenRoot(dataRoot)
	if err != nil {
		return Pack{}, ErrPackFiles
	}
	defer root.Close()
	for dir := path.Dir(location); dir != "."; dir = path.Dir(dir) {
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			return Pack{}, ErrPackFiles
		}
	}
	file, err := root.OpenFile(location, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
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
	return ParsePack(id, raw)
}

func ReadVerifiedPack(dataRoot, id, checksum string) (Pack, error) {
	if !IsPack(id) || !packChecksum.MatchString(checksum) {
		return Pack{}, ErrPack
	}
	location := CatalogPath(id, checksum)
	root, err := os.OpenRoot(dataRoot)
	if err != nil {
		return Pack{}, ErrPackFiles
	}
	_, statErr := root.Lstat(location)
	root.Close()
	if os.IsNotExist(statErr) {
		location = PackPath(id)
	} else if statErr != nil {
		return Pack{}, ErrPackFiles
	}
	pack, err := readPack(dataRoot, id, location)
	if err == nil && pack.Checksum != checksum {
		err = ErrPackChanged
	}
	return pack, err
}

func CatalogPath(id, checksum string) string {
	return path.Join("themes", ".catalog", checksum, id, "theme.json")
}

func ParsePack(id string, raw []byte) (Pack, error) {
	if !IsPack(id) || len(raw) > maxManifestBytes {
		return Pack{}, ErrPack
	}
	return parsePack(id, raw)
}

func parsePack(id string, raw []byte) (Pack, error) {
	var m manifest
	if bundle.Decode(raw, &m) != nil || (m.Version != 1 && m.Version != 2) || strings.TrimSpace(m.Name) != m.Name ||
		m.Name == "" || len(m.Name) > 120 || strings.ContainsFunc(m.Name, unicode.IsControl) {
		return Pack{}, ErrPack
	}
	sum := sha256.Sum256(raw)
	checksum := hex.EncodeToString(sum[:])
	if m.Version == 2 {
		pack, err := parsePackV2(id, checksum, m, raw)
		pack.APIVersion = m.Version
		return pack, err
	}
	if m.Layouts != nil || m.Assets != nil || m.Tokens != nil {
		return Pack{}, ErrPack
	}
	appearance := Appearance{ID: id}
	if len(m.Banner) > 0 {
		if !validBanner(m.Banner) {
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
	return Pack{Appearance: appearance, Name: m.Name, Checksum: checksum, APIVersion: m.Version, BannerPNG: m.Banner, Raw: raw}, nil
}

func validBanner(raw []byte) bool {
	if len(raw) == 0 || len(raw) > maxBannerBytes {
		return false
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	return err == nil && config.Width >= 1 && config.Width <= 1600 && config.Height >= 1 && config.Height <= 800
}

func (p Pack) Asset(name string) []byte {
	if name == "banner.png" && len(p.BannerPNG) != 0 {
		return p.BannerPNG
	}
	return p.assets[name]
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

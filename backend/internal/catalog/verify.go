package catalog

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"syscall"
	"time"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

const maxCatalog = 256 << 10
const maxBundle = 64 << 20
const maxTheme = 192 << 10

var ErrVerification = errors.New("extension catalog signature, entry or bundle rejected")
var identityPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var versionPattern = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,64}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Entry struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Version    string `json:"version"`
	APIVersion int    `json:"api_version"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
}

type index struct {
	Schema    int     `json:"schema"`
	ExpiresAt string  `json:"expires_at"`
	Entries   []Entry `json:"entries"`
}

type signature struct {
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

type keyring struct {
	Keys    map[string]string `json:"keys"`
	Revoked []string          `json:"revoked"`
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func readInput(filename string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrVerification
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, ErrVerification
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, ErrVerification
	}
	return raw, nil
}

func verify(raw, signed, trusted []byte, now time.Time) (index, string, error) {
	var record signature
	var keys keyring
	var items index
	if bundle.Decode(signed, &record) != nil || bundle.Decode(trusted, &keys) != nil ||
		!identityPattern.MatchString(record.KeyID) {
		return items, "", ErrVerification
	}
	for _, revoked := range keys.Revoked {
		if revoked == record.KeyID {
			return items, "", ErrVerification
		}
	}
	key, err := base64.StdEncoding.Strict().DecodeString(keys.Keys[record.KeyID])
	sig, sigErr := base64.StdEncoding.Strict().DecodeString(record.Signature)
	if err != nil || sigErr != nil || len(key) != ed25519.PublicKeySize ||
		!ed25519.Verify(ed25519.PublicKey(key), raw, sig) || bundle.Decode(raw, &items) != nil {
		return items, "", ErrVerification
	}
	expires, err := time.Parse(time.RFC3339, items.ExpiresAt)
	if err != nil || !now.Before(expires) || items.Schema != 1 || len(items.Entries) == 0 || len(items.Entries) > 256 {
		return items, "", ErrVerification
	}
	seen := map[[3]string]bool{}
	for _, entry := range items.Entries {
		slot := [3]string{entry.Kind, entry.ID, entry.Version}
		if seen[slot] || !identityPattern.MatchString(entry.ID) || !versionPattern.MatchString(entry.Version) ||
			!digestPattern.MatchString(entry.SHA256) || entry.Bytes < 1 || entry.Bytes > maxBundle ||
			entry.APIVersion < 1 {
			return items, "", ErrVerification
		}
		switch entry.Kind {
		case "plugin":
		case "theme":
			if !theme.IsPack(entry.ID) || entry.Bytes > maxTheme {
				return items, "", ErrVerification
			}
		default:
			return items, "", ErrVerification
		}
		seen[slot] = true
	}
	return items, record.KeyID, nil
}

func unpackPlugin(raw []byte, destination *os.Root) error {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > 256 {
		return ErrVerification
	}
	var total uint64
	for _, file := range reader.File {
		name := file.Name
		if !file.Mode().IsRegular() || (name != "manifest.json" && !bundle.ValidPath(name)) ||
			file.UncompressedSize64 > maxBundle-total {
			return ErrVerification
		}
		total += file.UncompressedSize64
		if dir := path.Dir(name); dir != "." {
			if err := destination.MkdirAll(dir, 0700); err != nil {
				return ErrVerification
			}
		}
		src, err := file.Open()
		if err != nil {
			return ErrVerification
		}
		err = writeFile(destination, name, src, int64(file.UncompressedSize64))
		src.Close()
		if err != nil {
			return ErrVerification
		}
	}
	return nil
}

func writeFile(root *os.Root, name string, reader io.Reader, size int64) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(reader, size+1))
	if err != nil || n != size {
		return ErrVerification
	}
	return f.Sync()
}

func compatible(entry Entry) bool {
	switch entry.Kind {
	case "plugin":
		switch entry.ID {
		case "maintenance", "social-cards", "publisher-mcp", "social-publishing", "analytics", "editorial-ai", "store", "paypal", "notion-delivery", "lesson-scheduler":
			return false
		}
		return entry.APIVersion <= plugin.HostAPIVersion
	case "theme":
		return entry.APIVersion == 1 || entry.APIVersion == 2
	}
	return false
}

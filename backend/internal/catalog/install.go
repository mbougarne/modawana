package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

type Request struct {
	Index, Signature, Keyring, Bundle string
	Kind, ID, Version                 string
}

type Review struct {
	Entry          Entry                 `json:"entry"`
	KeyID          string                `json:"key_id"`
	CatalogSHA256  string                `json:"catalog_sha256"`
	Plugin         *plugin.CatalogReview `json:"plugin,omitempty"`
	CurrentTheme   *theme.Approval       `json:"current_theme,omitempty"`
	CandidateTheme string                `json:"candidate_theme,omitempty"`
	ReviewSHA256   string                `json:"review_sha256"`
}

type Installer struct {
	Root    string
	Plugins *plugin.Service
	Themes  *theme.Service
}

// Run previews or installs an entry under the caller's offline runtime lease.
// Approval is the only switch to the new version. The published path is
// content-addressed and never replaces a previously approved directory.
func (i Installer) Run(ctx context.Context, req Request, approval string, now time.Time) (Review, error) {
	indexBytes, err := readInput(req.Index, maxCatalog)
	if err != nil {
		return Review{}, err
	}
	signature, err := readInput(req.Signature, 4096)
	if err != nil {
		return Review{}, err
	}
	trusted, err := readInput(req.Keyring, maxCatalog)
	if err != nil {
		return Review{}, err
	}
	catalog, keyID, err := verify(indexBytes, signature, trusted, now)
	if err != nil {
		return Review{}, err
	}
	var entry Entry
	for _, item := range catalog.Entries {
		if item.Kind == req.Kind && item.ID == req.ID && item.Version == req.Version {
			entry = item
			break
		}
	}
	if !compatible(entry) {
		return Review{}, ErrVerification
	}
	payload, err := readInput(req.Bundle, entry.Bytes)
	if err != nil || int64(len(payload)) != entry.Bytes || digest(payload) != entry.SHA256 {
		return Review{}, ErrVerification
	}

	stage, err := os.MkdirTemp(i.Root, ".extension-")
	if err != nil {
		return Review{}, err
	}
	defer os.RemoveAll(stage)
	staged, err := os.OpenRoot(stage)
	if err != nil {
		return Review{}, err
	}
	defer staged.Close()
	if err := staged.Mkdir(entry.ID, 0700); err != nil {
		return Review{}, err
	}
	candidate, err := staged.OpenRoot(entry.ID)
	if err != nil {
		return Review{}, err
	}
	defer candidate.Close()

	review := Review{Entry: entry, KeyID: keyID, CatalogSHA256: digest(indexBytes)}
	var location string
	var commit func() error
	if entry.Kind == "plugin" {
		if err := unpackPlugin(payload, candidate); err != nil {
			return Review{}, err
		}
		dir, err := plugin.ReadDirectory(stage, entry.ID)
		if err != nil {
			return Review{}, err
		}
		if dir.Manifest.Version != entry.Version || dir.Manifest.HostAPIVersion != entry.APIVersion {
			return Review{}, ErrVerification
		}
		check, err := i.Plugins.ReviewCatalog(ctx, dir)
		if err != nil {
			return Review{}, err
		}
		review.Plugin = &check
		location = path.Join("plugins", dir.CatalogPath())
		commit = func() error {
			_, err := i.Plugins.ApproveCatalog(ctx, dir, check.ExpectedVersion)
			return err
		}
	} else {
		pack, err := theme.ParsePack(entry.ID, payload)
		if err != nil {
			return Review{}, err
		}
		if pack.APIVersion != entry.APIVersion {
			return Review{}, ErrVerification
		}
		current, err := i.Themes.CatalogApproval(ctx, entry.ID)
		if err != nil {
			return Review{}, err
		}
		review.CurrentTheme, review.CandidateTheme = &current, pack.Name
		location = path.Dir(theme.CatalogPath(entry.ID, pack.Checksum))
		if err := writeFile(candidate, "theme.json", bytes.NewReader(payload), entry.Bytes); err != nil {
			return Review{}, err
		}
		commit = func() error {
			_, err := i.Themes.ApproveCatalog(ctx, entry.ID, pack.Checksum, current.Revision)
			return err
		}
	}
	details, err := json.Marshal(review)
	if err != nil {
		return Review{}, err
	}
	review.ReviewSHA256 = digest(details)
	if approval == "" {
		return review, nil
	}
	if approval != review.ReviewSHA256 {
		return Review{}, errors.New("extension review changed; preview again")
	}
	if err := publish(i.Root, filepath.Base(stage), entry.ID, location, candidate); err != nil {
		return Review{}, err
	}
	if err := commit(); err != nil {
		return Review{}, err
	}
	return review, nil
}

func publish(dataRoot, stage, id, location string, candidate *os.Root) error {
	root, err := os.OpenRoot(dataRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, dir := range []string{path.Dir(path.Dir(path.Dir(location))), path.Dir(path.Dir(location)), path.Dir(location)} {
		if err := root.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			return ErrVerification
		}
	}
	if info, err := root.Lstat(location); err == nil {
		if !info.IsDir() {
			return ErrVerification
		}
		// A retry may find the exact version already published. The approval
		// service always re-verifies the files at that location.
	} else if !os.IsNotExist(err) {
		return err
	} else {
		if err := fs.WalkDir(candidate.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return syncDirectory(candidate, name)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := root.Rename(path.Join(stage, id), location); err != nil {
			return err
		}
	}
	for dir := path.Dir(location); ; dir = path.Dir(dir) {
		if err := syncDirectory(root, dir); err != nil {
			return err
		}
		if dir == "." {
			return nil
		}
	}
}

func syncDirectory(root *os.Root, path string) error {
	dir, err := root.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

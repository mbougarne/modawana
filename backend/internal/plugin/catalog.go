package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"slices"
)

type CatalogReview struct {
	ExpectedVersion     int64    `json:"expected_version"`
	Enabled             bool     `json:"enabled"`
	Current             Manifest `json:"current"`
	Candidate           Manifest `json:"candidate"`
	AddedCapabilities   []string `json:"added_capabilities"`
	RemovedCapabilities []string `json:"removed_capabilities"`
	AppliedMigrations   int      `json:"applied_migrations"`
}

// ReviewCatalog validates WASM imports and registrations without running the
// module, then checks that the candidate preserves applied migrations.
func (s *Service) ReviewCatalog(ctx context.Context, directory Directory) (CatalogReview, error) {
	out := CatalogReview{Candidate: directory.Manifest, AddedCapabilities: slices.Clone(directory.Manifest.Capabilities)}
	slug := directory.Manifest.Slug
	if s.Host.Installed(slug) {
		return out, ErrManifest
	}
	compiled, err := s.Runtime.Compile(ctx, directory.Manifest, directory.Module)
	if err != nil {
		return out, err
	}
	if err := compiled.Close(ctx); err != nil {
		return out, err
	}
	if err := s.Host.Install(&wasmExtension{service: s, plugin: &loaded{manifest: directory.Manifest}}); err != nil {
		return out, err
	}
	s.Host.Uninstall(slug)
	record, err := s.install(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Enabled, out.ExpectedVersion = record.Enabled, record.RowVersion
	if err := json.Unmarshal(record.Manifest, &out.Current); err != nil {
		return out, err
	}
	out.AddedCapabilities = AddedCapabilities(out.Current, out.Candidate)
	out.RemovedCapabilities = AddedCapabilities(out.Candidate, out.Current)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.AppliedMigrations, err = appliedMigrations(ctx, tx, record.ID, directory.Manifest.Migrations)
	return out, err
}

// ApproveCatalog must be called offline with the runtime lease. It reads the
// published immutable candidate again before changing the SQLite approval.
func (s *Service) ApproveCatalog(ctx context.Context, directory Directory, expected int64) (Install, error) {
	slug := directory.Manifest.Slug
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return Install{}, ErrFiles
	}
	defer root.Close()
	location, err := approvedLocation(root, slug, directory.Files)
	if err != nil || location != directory.CatalogPath() {
		return Install{}, ErrFiles
	}
	published, err := readDirectory(root, location, slug)
	if err != nil {
		return Install{}, err
	}
	if !SameFiles(published.Files, directory.Files) {
		return Install{}, ErrChecksum
	}
	return s.approve(ctx, slug, "", expected, func(*sql.Tx) error { return nil }, false, &published)
}

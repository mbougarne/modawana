package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/tetratelabs/wazero"
)

// Service owns the plugin lifecycle: owner approval records in SQLite,
// checksum verification against plugins/{slug}/ under the data root, and the
// loaded modules registered on the extension host. Nothing executes before
// the owner approves the manifest and enables the plugin.
type Service struct {
	DB      *sql.DB
	Dir     string
	Host    *extension.Host
	Runtime *Runtime
	Logger  *slog.Logger

	mu     sync.Mutex
	loaded map[string]*loaded
}

type loaded struct {
	installID string
	manifest  Manifest
	compiled  wazero.CompiledModule
	enabled   atomic.Bool
	gate      chan struct{}
}

// Install is one approved plugin record.
type Install struct {
	ID         string
	Slug       string          `json:"slug"`
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	APIVersion int             `json:"api_version"`
	Manifest   json.RawMessage `json:"manifest"`
	Files      []File          `json:"-"`
	Enabled    bool            `json:"enabled"`
	RowVersion int64           `json:"row_version"`
	Updated    string          `json:"updated_at"`
}

// Status is one plugin as the dashboard sees it: the approved record, the
// state of the files on disk, and the pending manifest when they differ.
type Status struct {
	Install
	// State is installed, changed (files differ from the approved checksums),
	// missing (no files on disk), available (files but no approval yet), or
	// invalid (files present but rejected; Reason says why).
	State             string          `json:"state"`
	Running           bool            `json:"running"`
	Capabilities      []string        `json:"capabilities"`
	NextManifest      json.RawMessage `json:"next_manifest,omitempty"`
	AddedCapabilities []string        `json:"added_capabilities,omitempty"`
	Reason            string          `json:"reason,omitempty"`
}

func (s *Service) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Logger
}

func (s *Service) install(ctx context.Context, slug string) (Install, error) {
	var out Install
	var manifest, files string
	var enabled int
	err := s.DB.QueryRowContext(ctx, "SELECT id,slug,name,plugin_version,api_version,manifest,files,enabled,version,updated_at FROM plugin_installs WHERE slug=?", slug).
		Scan(&out.ID, &out.Slug, &out.Name, &out.Version, &out.APIVersion, &manifest, &files, &enabled, &out.RowVersion, &out.Updated)
	if err != nil {
		return out, err
	}
	out.Enabled = enabled == 1
	out.Manifest = json.RawMessage(manifest)
	if err = json.Unmarshal([]byte(files), &out.Files); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) installs(ctx context.Context) ([]Install, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT slug FROM plugin_installs ORDER BY slug")
	if err != nil {
		return nil, err
	}
	var slugs []string
	for rows.Next() {
		var slug string
		if err = rows.Scan(&slug); err != nil {
			rows.Close()
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]Install, 0, len(slugs))
	for _, slug := range slugs {
		record, err := s.install(ctx, slug)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

// List reports every approved plugin and every plugins/{slug} directory that
// is not approved yet, so the dashboard can show the manifest before install.
func (s *Service) List(ctx context.Context) ([]Status, error) {
	installed, err := s.installs(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	out := make([]Status, 0, len(installed))
	for _, record := range installed {
		known[record.Slug] = true
		status := Status{Install: record, State: "installed", Running: s.running(record.Slug)}
		var approved Manifest
		if err = json.Unmarshal(record.Manifest, &approved); err == nil {
			status.Capabilities = approved.Capabilities
		}
		directory, readErr := ReadDirectory(s.Dir, record.Slug)
		switch {
		case readErr != nil:
			status.State = "missing"
			status.Reason = "plugin files are missing or rejected"
		case !SameFiles(directory.Files, record.Files):
			status.State = "changed"
			status.NextManifest = json.RawMessage(directory.RawManifest)
			status.AddedCapabilities = AddedCapabilities(approved, directory.Manifest)
		}
		out = append(out, status)
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, ErrFiles
	}
	for _, entry := range entries {
		slug := entry.Name()
		if !entry.IsDir() || known[slug] || !slugPattern.MatchString(slug) {
			continue
		}
		status := Status{Install: Install{Slug: slug}, State: "available"}
		directory, readErr := ReadDirectory(s.Dir, slug)
		if readErr != nil {
			status.State = "invalid"
			status.Reason = readErr.Error()
		} else {
			status.Name = directory.Manifest.Name
			status.Version = directory.Manifest.Version
			status.Capabilities = directory.Manifest.Capabilities
			status.NextManifest = json.RawMessage(directory.RawManifest)
		}
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// Approve records the plugin directory as the owner-approved install: the
// manifest and every file checksum, enabled only if it already was. check is
// the fresh-reauthentication recheck run inside the transaction. expected is
// the optimistic row version, 0 for a first install.
func (s *Service) Approve(ctx context.Context, slug string, expected int64, check func(*sql.Tx) error) (Install, error) {
	directory, err := ReadDirectory(s.Dir, slug)
	if err != nil {
		return Install{}, err
	}
	files, err := json.Marshal(directory.Files)
	if err != nil {
		return Install{}, err
	}
	m := directory.Manifest
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Install{}, err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return Install{}, err
	}
	var id string
	now := db.UTC(time.Now())
	err = tx.QueryRowContext(ctx, "SELECT id FROM plugin_installs WHERE slug=?", slug).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if expected != 0 {
			return Install{}, db.ErrVersionConflict
		}
		if id, err = db.NewID(); err != nil {
			return Install{}, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO plugin_installs(id,slug,name,plugin_version,api_version,manifest,files,enabled,version,installed_at,updated_at) VALUES(?,?,?,?,?,?,?,0,1,?,?)",
			id, slug, m.Name, m.Version, m.HostAPIVersion, string(directory.RawManifest), string(files), now, now)
	case err == nil:
		if err = db.BumpVersion(ctx, tx, db.PluginInstalls, id, expected); err != nil {
			return Install{}, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE plugin_installs SET name=?,plugin_version=?,api_version=?,manifest=?,files=?,updated_at=? WHERE id=?",
			m.Name, m.Version, m.HostAPIVersion, string(directory.RawManifest), string(files), now, id)
	}
	if err != nil {
		return Install{}, err
	}
	if err = tx.Commit(); err != nil {
		return Install{}, err
	}
	record, err := s.install(ctx, slug)
	if err != nil {
		return record, err
	}
	// Re-approval of an enabled plugin reloads it so the running module is
	// always the approved one; a load failure disables nothing silently.
	if record.Enabled {
		if err = s.load(ctx, record); err != nil {
			return record, err
		}
	}
	return record, nil
}

// SetEnabled flips the plugin on or off with fresh reauthentication. Enabling
// verifies the files still match the approved checksums and loads the module
// before the flag commits; disabling unloads it so a disabled plugin never
// runs again, not even for an in-flight registry.
func (s *Service) SetEnabled(ctx context.Context, slug string, enable bool, expected int64, check func(*sql.Tx) error) (Install, error) {
	record, err := s.install(ctx, slug)
	if err != nil {
		return record, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return record, err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return record, err
	}
	if err = db.BumpVersion(ctx, tx, db.PluginInstalls, record.ID, expected); err != nil {
		return record, err
	}
	value := 0
	if enable {
		value = 1
	}
	if _, err = tx.ExecContext(ctx, "UPDATE plugin_installs SET enabled=?, updated_at=? WHERE id=?", value, db.UTC(time.Now()), record.ID); err != nil {
		return record, err
	}
	if enable {
		record.Enabled = true
		if err = s.load(ctx, record); err != nil {
			return record, err
		}
	} else {
		s.unload(slug)
	}
	if err = tx.Commit(); err != nil {
		if enable {
			s.unload(slug)
		}
		return record, err
	}
	return s.install(ctx, slug)
}

// Remove deletes the approval record and, through the schema's cascades, the
// plugin's storage. Files under plugins/{slug}/ stay for the operator to
// delete: the host never removes data-root files on an API call.
func (s *Service) Remove(ctx context.Context, slug string, expected int64, check func(*sql.Tx) error) error {
	s.unload(slug)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM plugin_installs WHERE slug=? AND version=?", slug, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM plugin_installs WHERE slug=?", slug).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
		return db.ErrVersionConflict
	}
	return tx.Commit()
}

// LoadEnabled loads every enabled, checksum-verified plugin at startup. A
// plugin that fails to load is logged and skipped; it stays approved but does
// not run until the owner re-approves matching files.
func (s *Service) LoadEnabled(ctx context.Context) error {
	installed, err := s.installs(ctx)
	if err != nil {
		return err
	}
	for _, record := range installed {
		if !record.Enabled {
			continue
		}
		if err = s.load(ctx, record); err != nil {
			s.logger().Error("plugin not loaded", "plugin", record.Slug, "reason", err.Error())
		}
	}
	return nil
}

func (s *Service) running(slug string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loaded[slug]
	return p != nil && p.enabled.Load()
}

// load verifies the directory against the approved checksums, compiles the
// approved module, and registers the plugin's hooks and routes on the host.
func (s *Service) load(ctx context.Context, record Install) error {
	directory, err := ReadDirectory(s.Dir, record.Slug)
	if err != nil {
		return err
	}
	if !SameFiles(directory.Files, record.Files) {
		return ErrChecksum
	}
	var approved Manifest
	if err = json.Unmarshal(record.Manifest, &approved); err != nil || approved.Slug != record.Slug {
		return ErrChecksum
	}
	compiled, err := s.Runtime.Compile(ctx, approved, directory.Module)
	if err != nil {
		return err
	}
	p := &loaded{installID: record.ID, manifest: approved, compiled: compiled, gate: make(chan struct{}, invocationGate)}
	p.enabled.Store(true)
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous := s.loaded[record.Slug]; previous != nil {
		previous.enabled.Store(false)
		s.Host.Uninstall(record.Slug)
		_ = previous.compiled.Close(ctx)
	}
	if err = s.Host.Install(&wasmExtension{service: s, plugin: p}); err != nil {
		_ = compiled.Close(ctx)
		return err
	}
	if s.loaded == nil {
		s.loaded = map[string]*loaded{}
	}
	s.loaded[record.Slug] = p
	return nil
}

func (s *Service) unload(slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loaded[slug]
	if p == nil {
		return
	}
	p.enabled.Store(false)
	s.Host.Uninstall(slug)
	_ = p.compiled.Close(context.Background())
	delete(s.loaded, slug)
}

// wasmExtension adapts one loaded plugin to the stage 0 extension contract:
// the host dispatches to it exactly as it dispatches to built-ins.
type wasmExtension struct {
	service *Service
	plugin  *loaded
}

func (x *wasmExtension) Slug() string { return x.plugin.manifest.Slug }

func (x *wasmExtension) Register(r *extension.Registrar) error {
	for _, hook := range x.plugin.manifest.Hooks {
		handler := func(ctx context.Context, name string, payload []byte) ([]byte, error) {
			if !x.plugin.enabled.Load() {
				return nil, errors.New("plugin disabled")
			}
			return x.service.Runtime.InvokeHook(ctx, x.plugin, name, hook.Version, payload)
		}
		if err := r.Hook(hook.Name, hook.Version, handler); err != nil {
			return err
		}
	}
	for _, route := range x.plugin.manifest.Routes {
		declared := route
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			x.serveRoute(w, r, declared)
		})
		if err := r.Route(extension.Route{Method: declared.Method, Path: declared.Path, Public: declared.Public, Handler: handler}); err != nil {
			return err
		}
	}
	return nil
}

func (x *wasmExtension) serveRoute(w http.ResponseWriter, r *http.Request, declared ManifestRoute) {
	if !x.plugin.enabled.Load() {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}
	envelope := routeEnvelope{Method: declared.Method, Path: declared.Path, Query: r.URL.Query(), Owner: !declared.Public}
	if r.Body != nil && declared.Method != "GET" && declared.Method != "HEAD" {
		body, err := io.ReadAll(io.LimitReader(r.Body, routeBodyLimit+1))
		if err != nil || len(body) > routeBodyLimit {
			http.Error(w, "Request too large.", http.StatusRequestEntityTooLarge)
			return
		}
		envelope.Body = string(body)
	}
	result, err := x.service.Runtime.InvokeRoute(r.Context(), x.plugin, envelope)
	if err != nil {
		x.service.logger().Error("plugin route failed", "plugin", x.plugin.manifest.Slug, "reason", err.Error())
		http.Error(w, "Plugin error.", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", result.ContentType+"; charset=utf-8")
	w.WriteHeader(result.Status)
	if r.Method != "HEAD" {
		_, _ = fmt.Fprint(w, result.Body)
	}
}

package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

type pluginReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Configuration struct {
	Version int64                      `json:"version"`
	Values  map[string]json.RawMessage `json:"values"`
	History []int64                    `json:"history"`
	Secrets map[string]bool            `json:"secrets"`
}

func readPluginSettings(ctx context.Context, q pluginReader, id string, m Manifest) (map[string]json.RawMessage, error) {
	var snapshot string
	err := q.QueryRowContext(ctx, "SELECT snapshot FROM plugin_settings_revisions WHERE plugin_id=? ORDER BY version DESC LIMIT 1", id).Scan(&snapshot)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return effectiveSettings(m, snapshot)
}

func effectiveSettings(m Manifest, snapshot string) (map[string]json.RawMessage, error) {
	old := map[string]json.RawMessage{}
	if snapshot != "" {
		if err := json.Unmarshal([]byte(snapshot), &old); err != nil {
			return nil, err
		}
	}
	out := make(map[string]json.RawMessage, len(m.Settings))
	for _, field := range m.Settings {
		value := old[field.Name]
		if !validSetting(field, value) {
			value = field.Default
		}
		out[field.Name] = value
	}
	return out, nil
}

func configManifest(ctx context.Context, q pluginReader, slug string) (string, int64, Manifest, error) {
	var id, raw string
	var version int64
	if err := q.QueryRowContext(ctx, "SELECT id,version,manifest FROM plugin_installs WHERE slug=?", slug).Scan(&id, &version, &raw); err != nil {
		return "", 0, Manifest{}, err
	}
	m, err := ParseManifest([]byte(raw), slug)
	return id, version, m, err
}

func (s *Service) Configuration(ctx context.Context, slug string) (Configuration, error) {
	out := Configuration{History: []int64{}, Secrets: map[string]bool{}}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	id, version, m, err := configManifest(ctx, tx, slug)
	if err != nil {
		return out, err
	}
	out.Version = version
	out.Values, err = readPluginSettings(ctx, tx, id, m)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT version FROM plugin_settings_revisions WHERE plugin_id=? ORDER BY version DESC LIMIT 50", id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&revision); err != nil {
			rows.Close()
			return out, err
		}
		out.History = append(out.History, revision)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, secret := range m.Secrets {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM plugin_secrets WHERE plugin_id=? AND name=?)", id, secret.Name).Scan(&exists); err != nil {
			return out, err
		}
		out.Secrets[secret.Name] = exists
	}
	return out, nil
}

type SettingsInput struct {
	ExpectedVersion int64                      `json:"expected_version"`
	Values          map[string]json.RawMessage `json:"values,omitempty"`
	RestoreVersion  int64                      `json:"restore_version,omitempty"`
}

func (s *Service) SaveSettings(ctx context.Context, slug string, in SettingsInput) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, version, m, err := configManifest(ctx, tx, slug)
	if err != nil {
		return err
	}
	if !m.Capability("settings") || in.RestoreVersion < 0 || in.RestoreVersion != 0 && in.Values != nil {
		return ErrStorageInvalid
	}
	if version != in.ExpectedVersion {
		return db.ErrVersionConflict
	}
	values := in.Values
	if in.RestoreVersion > 0 {
		var snapshot string
		if err := tx.QueryRowContext(ctx, "SELECT snapshot FROM plugin_settings_revisions WHERE plugin_id=? AND version=?", id, in.RestoreVersion).Scan(&snapshot); err != nil {
			return err
		}
		values, err = effectiveSettings(m, snapshot)
		if err != nil {
			return err
		}
	}
	if len(values) != len(m.Settings) {
		return ErrStorageInvalid
	}
	for _, field := range m.Settings {
		if !validSetting(field, values[field.Name]) {
			return ErrStorageInvalid
		}
	}
	snapshot, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if err := db.BumpVersion(ctx, tx, db.PluginInstalls, id, version); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO plugin_settings_revisions VALUES(?,?,?)", id, version+1, string(snapshot)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM plugin_settings_revisions WHERE plugin_id=? AND version NOT IN (SELECT version FROM plugin_settings_revisions WHERE plugin_id=? ORDER BY version DESC LIMIT 50)", id, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ChangeSecret(ctx context.Context, slug, name string, value *string, expected int64, check func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := check(tx); err != nil {
		return err
	}
	id, version, m, err := configManifest(ctx, tx, slug)
	if err != nil {
		return err
	}
	if !m.hasSecret(name) || value != nil && (*value == "" || !boundedText(*value, 4096)) {
		return ErrStorageInvalid
	}
	if version != expected {
		return db.ErrVersionConflict
	}
	if err := db.BumpVersion(ctx, tx, db.PluginInstalls, id, version); err != nil {
		return err
	}
	if value == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM plugin_secrets WHERE plugin_id=? AND name=?", id, name)
	} else {
		var ciphertext []byte
		ciphertext, err = s.Runtime.secretBox(id, name, []byte(*value), true)
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO plugin_secrets VALUES(?,?,?) ON CONFLICT(plugin_id,name) DO UPDATE SET ciphertext=excluded.ciphertext", id, name, ciphertext)
		}
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE plugin_installs SET updated_at=? WHERE id=?", db.UTC(time.Now()), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (m Manifest) hasSecret(name string) bool {
	for _, secret := range m.Secrets {
		if secret.Name == name {
			return m.Capability("secrets")
		}
	}
	return false
}

func pruneRemovedSecrets(ctx context.Context, tx *sql.Tx, id string, m Manifest) error {
	names := make([]string, 0, len(m.Secrets))
	for _, secret := range m.Secrets {
		names = append(names, secret.Name)
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM plugin_secrets WHERE plugin_id=? AND name NOT IN (SELECT value FROM json_each(?))", id, string(encoded))
	return err
}

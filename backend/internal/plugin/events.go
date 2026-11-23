package plugin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mbougarne/modawana/backend/internal/outbox"
)

type ManifestEvent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

func (m Manifest) validateEvents() error {
	if m.Capability("events") != (len(m.Events) > 0) || len(m.Events) > 1 ||
		(len(m.Events) > 0 && m.HostAPIVersion < 5) {
		return fmt.Errorf("%w: events require host API 5 and the events capability", ErrManifest)
	}
	for _, event := range m.Events {
		if event.Name != outbox.ContentPublished || event.Version != outbox.ContentPublishedVersion {
			return fmt.Errorf("%w: unsupported event contract", ErrManifest)
		}
	}
	return nil
}

func replaceEventSubscriptions(ctx context.Context, tx *sql.Tx, id string, events []ManifestEvent) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM outbox_subscriptions WHERE plugin_id=?", id); err != nil {
		return err
	}
	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_subscriptions (plugin_id,name,contract_version)
			VALUES(?,?,?)`, id, event.Name, event.Version); err != nil {
			return err
		}
	}
	return nil
}

type eventResult struct {
	Set *struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	} `json:"set"`
}

// PrepareEvent runs sandboxed plugin code outside the SQLite write transaction.
// The host applies the optional effect only after it fences the lease and checks
// the approved subscription again.
func (s *Service) PrepareEvent(ctx context.Context, delivery outbox.Delivery) (outbox.Effect, error) {
	s.mu.Lock()
	p := s.loaded[delivery.PluginSlug]
	s.mu.Unlock()
	if p == nil || p.installID != delivery.PluginID || !p.enabled.Load() ||
		len(p.manifest.Events) != 1 || p.manifest.Events[0] != (ManifestEvent{delivery.Event.Name, delivery.Event.Version}) {
		return nil, outbox.ErrUnavailable
	}
	settings, err := s.Runtime.invocationSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	input, err := json.Marshal(struct {
		API         int                        `json:"api"`
		Kind        string                     `json:"kind"`
		OperationID string                     `json:"operation_id"`
		Event       outbox.Event               `json:"event"`
		Settings    map[string]json.RawMessage `json:"settings,omitempty"`
	}{p.manifest.HostAPIVersion, "event", delivery.ID, delivery.Event, settings})
	if err != nil {
		return nil, err
	}
	output, err := s.Runtime.invoke(ctx, p, input, 8192, pluginReadOnly, nil)
	if err != nil {
		if !p.enabled.Load() {
			return nil, outbox.ErrUnavailable
		}
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	var result *eventResult
	if err := decoder.Decode(&result); err != nil || result == nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return nil, outbox.ErrRejected
	}
	if result.Set == nil {
		return nil, nil
	}
	if !p.manifest.Capability("storage") || result.Set.Value == nil || !kvKeyPattern.MatchString(result.Set.Key) {
		return nil, outbox.ErrRejected
	}
	return func(ctx context.Context, tx *sql.Tx) error {
		return (storage{tx: tx}).kvSet(ctx, p.installID, result.Set.Key, []byte(*result.Set.Value))
	}, nil
}

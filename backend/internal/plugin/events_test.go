package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/outbox"
)

func TestApprovedWasmSubscriptionDeliversPublication(t *testing.T) {
	manifest := Manifest{
		SchemaVersion: 1, HostAPIVersion: 5, Name: "Publication receipt", Slug: "publication-receipt",
		Version: "1", Module: "plugin.wasm", Capabilities: []string{"events", "storage"},
		Events: []ManifestEvent{{Name: outbox.ContentPublished, Version: 1}},
	}
	old := manifest
	old.HostAPIVersion = 4
	unapproved := manifest
	unapproved.Capabilities = []string{"storage"}
	for _, invalid := range []Manifest{old, unapproved} {
		raw, err := json.Marshal(invalid)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseManifest(raw, manifest.Slug); !errors.Is(err, ErrManifest) {
			t.Fatal("accepted unapproved event capability", err)
		}
	}
	s, _ := fixture(t)
	record := installStageThreeFixture(t, s, manifest, buildWASIFixture(t, "./testdata/event"))
	ctx := t.Context()
	publish := func(source string) {
		t.Helper()
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := outbox.RecordPublication(ctx, tx, source, 2, outbox.Publication{ContentID: "test-content", Locale: "ar", RevisionID: "exact-revision"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	publish("first")
	w := outbox.Worker{DB: s.DB, Handle: s.PrepareEvent}
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); w.Run(run) }()
	defer func() { stop(); <-done }()
	deadline := time.Now().Add(8 * time.Second)
	var id string
	for {
		page, err := w.List(ctx, "")
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		item := page.Items[0]
		if item.State == "succeeded" {
			id = item.ID
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin delivery did not succeed", item)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, err := s.Runtime.storage.kvGet(ctx, record.ID, "publication-receipt")
	if err != nil || string(stored) != id+":exact-revision" {
		t.Fatal("wrong receipt or revision", string(stored), err)
	}
	if _, err := s.SetEnabled(ctx, manifest.Slug, false, record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	publish("after-disable")
	page, err := w.List(ctx, "")
	if err != nil || len(page.Items) != 1 || !strings.EqualFold(page.Items[0].ID, id) {
		t.Fatal("disabled plugin received new work", page, err)
	}
}

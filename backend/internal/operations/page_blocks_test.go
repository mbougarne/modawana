package operations

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func TestBackupRestoresExactPublishedAndHistoricalPageArtifacts(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	files, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	owner, _ := db.NewID()
	if _, err := d.Exec("INSERT INTO owner(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	c := &content.Service{DB: d, Files: files}
	zero := int64(0)
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "page", PageTemplate: "standard", SourceLocale: "en",
		Slug: "home", Title: "Home", Markdown: "Old Markdown"}
	legacy, err := c.Save(ctx, owner, "home", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion, in.BodyFormat, in.Markdown = &legacy.Version, content.BlockBodyFormat, ""
	in.PageBlocks = json.RawMessage(`{"version":1,"blocks":[{"id":"hero","type":"core.heading","version":1,"properties":{"text":"Live heading","level":2}}]}`)
	publishedBody := in.PageBlocks
	saved, err := c.Save(ctx, owner, "home", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	live, err := c.ChangePublication(ctx, owner, "home", "en", "", "publish", content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion = &live.Version
	in.PageBlocks = bytes.ReplaceAll(publishedBody, []byte("Live"), []byte("Draft"))
	draft, err := c.Save(ctx, owner, "home", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	err = Backup(ctx, d, files, root, f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, archive, target); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := db.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	restoredFiles, err := storage.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredFiles.Close()
	r := &content.Service{DB: restoredDB, Files: restoredFiles}
	got, err := r.Draft(ctx, "home", "en")
	if err != nil || got.Revision.ID != draft.RevisionID || got.PublishedID != saved.RevisionID ||
		!bytes.Equal(got.Revision.PageBlocks, in.PageBlocks) || !Maintenance(target) {
		t.Fatal("draft or published pointer changed during recovery", got, err)
	}
	page, err := r.ResolvePage(ctx, "/en/pages/home")
	if err != nil || page.Content == nil || !bytes.Equal(page.Content.PageBlocks, publishedBody) {
		t.Fatal("published artifact changed during recovery", page, err)
	}
	old, err := r.Historical(ctx, "home", "en", legacy.RevisionID)
	if err != nil || old.Markdown != "Old Markdown" || old.PageTemplate != "standard" {
		t.Fatal("legacy Markdown history changed during recovery", old, err)
	}

	extracted, manifest, err := bundle.Read(archive, "backup")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(extracted)
	var artifact storage.File
	if err := d.QueryRow("SELECT markdown_path FROM revisions WHERE id=?", saved.RevisionID).Scan(&artifact.Key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extracted, artifact.Key), in.PageBlocks, 0600); err != nil {
		t.Fatal(err)
	}
	bad, err := os.Create(filepath.Join(t.TempDir(), "corrupt.zip"))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	err = bundle.Write(bad, "backup", extracted, paths)
	bad.Close()
	if err != nil {
		t.Fatal(err)
	}
	corruptTarget := filepath.Join(t.TempDir(), "corrupt")
	if err := Restore(ctx, bad.Name(), corruptTarget); !errors.Is(err, storage.ErrIntegrity) {
		t.Fatal("valid ZIP checksum concealed a mismatched revision artifact", err)
	}
	if _, err := os.Stat(corruptTarget); !os.IsNotExist(err) {
		t.Fatal("failed validation still installed a target", err)
	}
}

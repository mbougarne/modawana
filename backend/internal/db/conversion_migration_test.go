package db

import (
	"context"
	"database/sql"
	"github.com/mbougarne/modawana/backend/migrations"
	"testing"
	"time"
)

func TestTypeConversionMigrationPreservesPublishedRows(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	exec(t, d, "PRAGMA foreign_keys=ON")
	// The old schema ends right before 0012_content_type_conversion.sql, the
	// migration under test. Drop everything from 0012 on, not a fixed list,
	// so appending future migrations keeps this history contiguous.
	old := migrationTestSource(t)
	for name := range old {
		if name >= "0012" {
			delete(old, name)
		}
	}
	if err = migrate(context.Background(), d, old); err != nil {
		t.Fatal(err)
	}
	itemID, versionID, revisionID := item(t, d, "article", "en")
	now := UTC(time.Now())
	exec(t, d, "UPDATE content_versions SET published_revision_id=?,first_published_at=?,last_published_at=? WHERE id=?", revisionID, now, now, versionID)
	if err = migrate(context.Background(), d, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var item, revision, kind string
	d.QueryRow("SELECT i.id,i.kind,v.published_revision_id FROM content_items i JOIN content_versions v ON v.item_id=i.id WHERE v.id=?", versionID).Scan(&item, &kind, &revision)
	if item != itemID || kind != "article" || revision != revisionID {
		t.Fatal("upgrade changed existing published identity")
	}
	reject(t, d, "UPDATE content_items SET kind='lesson' WHERE id=?", itemID)
	reject(t, d, "UPDATE content_items SET source_locale='fr' WHERE id=?", itemID)
}

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
	old := migrationTestSource(t)
	delete(old, "0012_content_type_conversion.sql")
	delete(old, "0013_collection_removal.sql")
	delete(old, "0014_mcp_oauth.sql")
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

package content

import (
	"context"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"os"
	"testing"
)

func TestCommitFailuresAndMetadataOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	d, e := db.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	f, e := storage.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	s := Revisions{DB: d, Files: f}
	owner, _ := db.NewID()
	item, _ := db.NewID()
	version, _ := db.NewID()
	d.Exec("INSERT INTO owner(id) VALUES(?)", owner)
	d.Exec("INSERT INTO content_items(id,key,kind,source_locale) VALUES(?,?,'article','en')", item, item)
	in := SaveInput{ItemID: item, VersionID: version, OwnerID: owner, Locale: "en", Slug: "first", Title: "First", Body: []byte("durable")}
	first, e := s.Save(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = 1
	in.Title = "Metadata change"
	second, e := s.Save(ctx, in)
	if e != nil || first.RevisionID == second.RevisionID {
		t.Fatal(e)
	}
	if body, e := s.Read(ctx, first.RevisionID); e != nil || string(body) != "durable" {
		t.Fatal(e)
	}
	if _, e = s.Save(ctx, in); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal("conflict", e)
	}
	for _, stage := range []string{"before_commit", "after_commit"} {
		in.ExpectedVersion = 2
		s.fault = func(p string) error {
			if p == stage {
				return errors.New("injected")
			}
			return nil
		}
		_, e = s.Save(ctx, in)
		if e == nil {
			t.Fatal("fault ignored")
		}
		var pointer string
		var n int
		d.QueryRow("SELECT draft_revision_id,version FROM content_versions WHERE id=?", version).Scan(&pointer, &n)
		want := 2
		if stage == "after_commit" {
			want = 3
		}
		if n != want {
			t.Fatal(n)
		}
		if b, e := s.Read(ctx, pointer); e != nil || string(b) != "durable" {
			t.Fatal("undurable pointer", e)
		}
	}
	var refs []storage.File
	rows, _ := d.Query("SELECT markdown_path,sha256,byte_size FROM revisions")
	for rows.Next() {
		var f storage.File
		rows.Scan(&f.Key, &f.SHA256, &f.Size)
		refs = append(refs, f)
	}
	rows.Close()
	report, e := f.InspectLocked(refs)
	if e != nil || len(report.Invalid) != 0 || len(report.Orphans) != 2 {
		t.Fatal(report, e)
	}
}

package media

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func setup(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	f, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close(); d.Close() })
	owner, _ := db.NewID()
	if _, e = d.Exec("INSERT INTO owner(id) VALUES(?)", owner); e != nil {
		t.Fatal(e)
	}
	return New(d, f, &access.Live{DB: d}), owner
}
func picture(w, h int) []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}
func TestFormatsAndLimits(t *testing.T) {
	for _, name := range []string{"clip.mp4", "clip.webm", "clip-aac.mp4", "clip-opus.webm", "clip-vorbis.webm"} {
		b, e := os.ReadFile("testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		f, e := Validate(b, "", "content")
		if e != nil || f.Kind != "video" || f.Width != 32 {
			t.Fatalf("%s: %+v %v", name, f, e)
		}
		if _, e = Validate(b[:len(b)/2], "", "content"); e == nil {
			t.Fatal("truncated video accepted")
		}
	}
	for _, name := range []string{"clip.mp4", "clip.webm"} {
		b, _ := os.ReadFile("testdata/" + name)
		if name == "clip.mp4" {
			b = bytes.ReplaceAll(b, []byte("avc1"), []byte("hev1"))
		} else {
			b = bytes.ReplaceAll(b, []byte("V_VP9"), []byte("V_AV1"))
		}
		if _, e := Validate(b, "", "content"); e == nil {
			t.Fatal("unsupported codec accepted")
		}
	}
	good := picture(600, 600)
	if _, e := Validate(good, "image/png", "portrait"); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		b             []byte
		mime, purpose string
	}{{good, "image/jpeg", "content"}, {picture(20, 30), "", "portrait"}, {[]byte("<svg></svg>"), "image/png", "content"}, {[]byte("MZ executable"), "image/png", "logo"}, {picture(5000, 5000), "", "content"}, {[]byte("WEBVTT\n\n00:00.000 --> 00:00.000\nwrong\n"), "", "content"}, {[]byte("%PDF-1.7\nnot a PDF\n%%EOF"), "", "content"}} {
		if _, e := Validate(tc.b, tc.mime, tc.purpose); e == nil {
			t.Fatalf("accepted invalid %s", tc.purpose)
		}
	}
	if _, e := Validate([]byte("WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n"), "text/vtt", "content"); e != nil {
		t.Fatal(e)
	}
	if _, e := Validate(make([]byte, (50<<20)+1), "", "content"); !errors.Is(e, ErrTooLarge) {
		t.Fatal(e)
	}
}

func TestMP4RejectsDetachedCodecHeader(t *testing.T) {
	b, err := os.ReadFile("testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	// Strip the track hierarchy while retaining an otherwise valid avc1 payload.
	// A detached sample description must not establish a valid video track.
	i := bytes.Index(b, []byte("avc1"))
	for i >= 4 && int(binary.BigEndian.Uint32(b[i-4:i])) > len(b)-i+4 {
		next := bytes.Index(b[i+4:], []byte("avc1"))
		if next < 0 {
			t.Fatal("sample entry not found")
		}
		i += 4 + next
	}
	if i < 4 {
		t.Fatal("sample entry not found")
	}
	n := int(binary.BigEndian.Uint32(b[i-4 : i]))
	box := func(kind string, payload []byte) []byte {
		out := make([]byte, len(payload)+8)
		binary.BigEndian.PutUint32(out, uint32(len(out)))
		copy(out[4:], kind)
		copy(out[8:], payload)
		return out
	}
	bad := append(box("ftyp", []byte("isom\x00\x00\x00\x00isom")), box("mdat", []byte{0})...)
	bad = append(bad, box("moov", nil)...)
	bad = append(bad, b[i-4:i-4+n]...)
	if _, err := validateMP4(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("detached codec accepted: %v", err)
	}
}
func TestPrivacyRangesAndReferences(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	b, _ := os.ReadFile("testdata/clip.mp4")
	m, e := s.Upload(ctx, owner, "clip.mp4", "video/mp4", "content", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, "/media/"+m.ID, nil)
		r.Header.Set("Range", "bytes=0-9")
		w := httptest.NewRecorder()
		if e = s.Serve(w, r, m.ID, access.Principal{}); !errors.Is(e, sql.ErrNoRows) || w.Body.Len() != 0 {
			t.Fatal("private range", e)
		}
		if e = s.Serve(w, r, m.ID, access.Principal{Audience: access.Owner, ID: owner}); e != nil || w.Code != 206 {
			t.Fatal(e, w.Code)
		}
		if method == "GET" && !bytes.Equal(w.Body.Bytes(), b[:10]) {
			t.Fatal("bad seek")
		}
	}
	// An applied public configuration permits reads; retained history prevents deletion.
	doc, _ := db.NewID()
	rev, _ := db.NewID()
	tx, _ := s.DB.Begin()
	tx.Exec("INSERT INTO config_documents(id,kind) VALUES(?,'site')", doc)
	tx.Exec("INSERT INTO config_revisions(id,document_id,snapshot_json,schema_version,colour_algorithm_version,created_by) VALUES(?,?,'{}',1,1,?)", rev, doc, owner)
	tx.Exec("INSERT INTO config_revision_media(revision_id,media_id,role) VALUES(?,?,'logo')", rev, m.ID)
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, m.ID); !errors.Is(e, ErrReferenced) {
		t.Fatal(e)
	}
	s.DB.Exec("UPDATE config_documents SET applied_revision_id=? WHERE id=?", rev, doc)
	r := httptest.NewRequest("GET", "/media/"+m.ID, nil)
	r.Header.Set("Range", "bytes=10-19")
	w := httptest.NewRecorder()
	if e = s.Serve(w, r, m.ID, access.Principal{}); e != nil || w.Code != 206 || !bytes.Equal(w.Body.Bytes(), b[10:20]) {
		t.Fatal(e, w.Code)
	}
	s.DB.Exec("UPDATE config_documents SET applied_revision_id=NULL WHERE id=?", doc)
	if e = s.Serve(httptest.NewRecorder(), r, m.ID, access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("unapply leaked", e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.Upload(cancelled, owner, "x", "", "content", bytes.NewReader(picture(1, 1))); e == nil {
		t.Fatal("cancelled committed")
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM media").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	m2, e := s.Upload(ctx, owner, "../../image.png", "image/png", "content", bytes.NewReader(picture(1, 1)))
	if e != nil || strings.Contains(m2.Filename, "/") {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, m2.ID); e != nil {
		t.Fatal(e)
	}
}

func TestDeletePreservesUnexpectedDatabaseFailure(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	m, e := s.Upload(ctx, owner, "image.png", "image/png", "content", bytes.NewReader(picture(1, 1)))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(`CREATE TRIGGER reject_media_delete BEFORE DELETE ON media BEGIN SELECT RAISE(ABORT,'injected storage failure'); END`); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, m.ID); e == nil || errors.Is(e, ErrReferenced) {
		t.Fatal("storage failure misclassified", e)
	}
	if _, e = Get(ctx, s.DB, m.ID); e != nil {
		t.Fatal("failed delete lost metadata", e)
	}
}

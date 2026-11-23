package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

var oldTestKey = bytes.Repeat([]byte{31}, 32)
var newTestKey = bytes.Repeat([]byte{87}, 32)

func webhookFixture(t *testing.T) (*Inbox, string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "signing.json")
	saveKeys(t, file, oldTestKey, nil, time.Time{})
	d, err := db.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	configuration, err := json.Marshal([]Connection{
		{ID: "primary", Account: "account-1", Mode: "live", Adapter: standardV1, KeyFile: file},
		{ID: "secondary", Account: "account-1", Mode: "live", Adapter: standardV1, KeyFile: file},
	})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Open(t.Context(), d, root, string(configuration))
	if err != nil {
		t.Fatal(err)
	}
	return i, file, string(configuration)
}

func saveKeys(t *testing.T, file string, current, previous []byte, until time.Time) {
	t.Helper()
	encode := func(key []byte) string { return "whsec_" + base64.StdEncoding.EncodeToString(key) }
	k := keyFile{Current: encode(current), PreviousUntil: until}
	if previous != nil {
		k.Previous = encode(previous)
	}
	raw, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file+".tmp", raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".tmp", file); err != nil {
		t.Fatal(err)
	}
}

func statusBody(now time.Time, connection, account, mode string, version int, status string) []byte {
	return fmt.Appendf(nil, `{"type":"resource.status","timestamp":%q,"data":{"connection":%q,
		"account":%q,"mode":%q,"resource_id":"private-identifier","version":%d,"status":%q}}`,
		now.Format(time.RFC3339), connection, account, mode, version, status)
}

func signedHeaders(key []byte, id string, now time.Time, body []byte) http.Header {
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	h := http.Header{}
	h.Set("Webhook-Id", id)
	h.Set("Webhook-Timestamp", ts)
	h.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}

func TestWebhookVerifiesScopeAndKeyRotation(t *testing.T) {
	i, file, config := webhookFixture(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	body := statusBody(now, "primary", "account-1", "live", 1, "active")
	submit := func(target, id string, key []byte, at, signedAt time.Time, body []byte) error {
		t.Helper()
		_, err := i.accept(ctx, standardV1, target, signedHeaders(key, id, signedAt, body), body, at)
		return err
	}
	for _, test := range []struct {
		name   string
		target string
		body   []byte
		want   error
	}{
		{"connection", "secondary", body, ErrScope},
		{"account", "primary", statusBody(now, "primary", "different", "live", 1, "active"), ErrScope},
		{"mode", "primary", statusBody(now, "primary", "account-1", "sandbox", 1, "active"), ErrScope},
		{"duplicate JSON", "primary", bytes.Replace(body, []byte(`"connection":"primary"`), []byte(`"connection":"secondary","connection":"primary"`), 1), ErrInvalid},
	} {
		if err := submit(test.target, "scope-event", oldTestKey, now, now, test.body); !errors.Is(err, test.want) {
			t.Fatal(test.name, err)
		}
	}
	if err := submit("primary", "bad-signature", newTestKey, now, now, body); !errors.Is(err, ErrSignature) {
		t.Fatal(err)
	}
	if err := submit("primary", "stale-signature", oldTestKey, now, now.Add(-6*time.Minute), body); !errors.Is(err, ErrSignature) {
		t.Fatal(err)
	}
	mismatch := bytes.Replace(body, []byte("active"), []byte("inactive"), 1)
	if _, err := i.accept(ctx, standardV1, "primary", signedHeaders(oldTestKey, "modified", now, body), mismatch, now); !errors.Is(err, ErrSignature) {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := submit("primary", "during-rotation", oldTestKey, now, now, body); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	saveKeys(t, file, newTestKey, oldTestKey, now.Add(time.Minute))
	if err := submit("primary", "during-rotation", oldTestKey, now, now, body); err != nil {
		t.Fatal(err)
	}
	if err := submit("primary", "after-grace", oldTestKey, now.Add(time.Minute), now, body); !errors.Is(err, ErrSignature) {
		t.Fatal(err)
	}
	if err := submit("primary", "after-grace", newTestKey, now.Add(time.Minute), now, body); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, i.db, i.root, strings.Replace(config, "account-1", "different", 1)); !errors.Is(err, ErrScope) {
		t.Fatal("connection reassignment", err)
	}
	page, err := i.List(ctx, 0)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
}

func TestWebhookPersistenceDeduplicationAndOutOfOrderRestart(t *testing.T) {
	i, _, config := webhookFixture(t)
	now := time.Now()
	ctx := t.Context()
	submit := func(connection, id string, version int, status string) (bool, error) {
		body := statusBody(now, connection, "account-1", "live", version, status)
		return i.accept(ctx, standardV1, connection, signedHeaders(oldTestKey, id, now, body), body, now)
	}
	if _, err := i.db.Exec(`CREATE TRIGGER fail_inbox BEFORE INSERT ON webhook_receipts BEGIN SELECT RAISE(ABORT,'inbox unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := submit("primary", "new-event", 2, "active"); err == nil || duplicate {
		t.Fatal("acknowledged a failed insert", duplicate, err)
	}
	if _, err := i.db.Exec("DROP TRIGGER fail_inbox"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		connection, id, status string
		version                int
	}{
		{"primary", "new-event", "active", 2},
		{"primary", "old-event", "inactive", 1},
		{"primary", "same-version", "inactive", 2},
		{"secondary", "new-event", "inactive", 1},
	} {
		if duplicate, err := submit(item.connection, item.id, item.version, item.status); duplicate || err != nil {
			t.Fatal(duplicate, err)
		}
	}
	if duplicate, err := submit("primary", "new-event", 2, "active"); !duplicate || err != nil {
		t.Fatal(duplicate, err)
	}
	if _, err := submit("primary", "new-event", 3, "active"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := i.db.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, i.root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	i, err = Open(ctx, d, i.root, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TRIGGER fail_receipt BEFORE UPDATE ON webhook_receipts BEGIN SELECT RAISE(ABORT,'ack unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if processed, err := i.process(ctx); processed || err == nil {
		t.Fatal("failed transaction applied", processed, err)
	}
	if err := d.QueryRow("SELECT version FROM webhook_resource_status").Scan(new(int64)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("resource update survived rollback", err)
	}
	if _, err := d.Exec("DROP TRIGGER fail_receipt"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if processed, err := i.process(ctx); !processed || err != nil {
			t.Fatal(processed, err)
		}
	}
	if processed, err := i.process(ctx); processed || err != nil {
		t.Fatal(processed, err)
	}
	page, err := i.List(ctx, 0)
	want := []string{"applied", "conflict", "superseded", "applied"}
	if err != nil || len(page.Items) != len(want) {
		t.Fatal(page, err)
	}
	for n, r := range page.Items {
		if r.State != want[n] {
			t.Fatal(page)
		}
	}
	var version int
	var status string
	if err := d.QueryRow("SELECT version,status FROM webhook_resource_status WHERE connection_id='primary'").Scan(&version, &status); err != nil || version != 2 || status != "active" {
		t.Fatal(version, status, err)
	}
	raw, err := json.Marshal(page)
	if err != nil || strings.Contains(string(raw), "private-identifier") || strings.Contains(string(raw), "new-event") {
		t.Fatal(string(raw), err)
	}
}

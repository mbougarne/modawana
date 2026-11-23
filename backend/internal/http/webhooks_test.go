package httpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/webhook"
)

func TestWebhookIngressAcknowledgesDurableReceipts(t *testing.T) {
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
	key := strings.Repeat("K", 32)
	keyFile := filepath.Join(t.TempDir(), "keys.json")
	credentials := `{"current":"whsec_` + base64.StdEncoding.EncodeToString([]byte(key)) + `"}`
	if err := os.WriteFile(keyFile, []byte(credentials), 0600); err != nil {
		t.Fatal(err)
	}
	spec, err := json.Marshal([]webhook.Connection{{ID: "provider", Adapter: "standard-v1", Account: "account", Mode: "sandbox", KeyFile: keyFile}})
	if err != nil {
		t.Fatal(err)
	}
	i, err := webhook.Open(ctx, d, root, string(spec))
	if err != nil {
		t.Fatal(err)
	}
	a := auth.New(d, config.Config{BaseURL: "https://example.test"})
	server := New(Options{Auth: a, Domain: &Domain{Auth: a, Webhooks: i}})
	now := time.Now().UTC()
	body := `{"type":"resource.status","timestamp":"` + now.Format(time.RFC3339) + `","data":{"connection":"provider",
		"account":"account","mode":"sandbox","resource_id":"example-resource","version":1,"status":"active"}}`
	const path = "/api/v1/webhooks/standard-v1/provider"
	request := func(method, route, payload string, sign bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://example.test"+route, strings.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		if sign {
			ts := strconv.FormatInt(now.Unix(), 10)
			mac := hmac.New(sha256.New, []byte(key))
			mac.Write([]byte("event-1." + ts + "." + payload))
			r.Header.Set("Webhook-Id", "event-1")
			r.Header.Set("Webhook-Timestamp", ts)
			r.Header.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	for _, test := range []struct {
		method, route, body string
		sign                bool
		want                int
	}{
		{"GET", "/api/v1/webhook-receipts", "", false, 401},
		{"GET", path, "", false, 405},
		{"POST", path + "-unknown", body, true, 404},
		{"POST", path, strings.Repeat("a", webhook.MaxBody+1), false, 413},
		{"POST", path, body, false, 401},
		{"POST", path, body, true, 202},
		{"POST", path, body, true, 200},
		{"POST", path, strings.Replace(body, `"version":1`, `"version":2`, 1), true, 409},
	} {
		w := request(test.method, test.route, test.body, test.sign)
		if w.Code != test.want || strings.Contains(w.Body.String(), "example-resource") {
			t.Fatal(test.method, test.route, w.Code, w.Body.String())
		}
	}
	page, err := i.List(ctx, 0)
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "pending" {
		t.Fatal(page, err)
	}
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", path, body, true); w.Code != 503 || w.Header().Get("Retry-After") != "5" {
		t.Fatal(w.Code, w.Body.String())
	}
}

package httpserver

import (
	"net/http/httptest"
	"testing"
)

func TestMaintenanceBlocksPublicPublisherAndReady(t *testing.T) {
	h := New(Options{Maintenance: func() bool { return true }})
	for _, path := range []string{"/readyz", "/en", "/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/api/v1/public/content/en/test", "/api/v1/publisher/content/test/en"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 503 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

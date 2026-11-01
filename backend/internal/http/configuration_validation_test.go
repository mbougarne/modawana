package httpserver

import (
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"net/http/httptest"
	"testing"
)

func TestConfigurationValidationEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Request-ID", "test-request")
	domainError(w, &siteconfig.ValidationError{Fields: []siteconfig.FieldIssue{{Path: "portrait_alt.en", Code: "image_alt"}}})
	var out struct {
		Error struct {
			Code, Message string
			RequestID     string `json:"request_id"`
			Fields        []siteconfig.FieldIssue
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || w.Code != 422 || out.Error.Code != "invalid_fields" || out.Error.Message == "" || out.Error.RequestID != "test-request" || len(out.Error.Fields) != 1 {
		t.Fatalf("bad envelope: %s", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("validation response cacheable")
	}
}

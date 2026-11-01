package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestOwnerActionErrorsPreserveCauseAndSafeEnvelope(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		cause  error
		code   string
	}{
		{actionField("security.password", "password_length"), 422, ErrAuth, "invalid_fields"},
		{&OwnerActionError{Path: "security.request", Reason: "request_confirmation", Cause: ErrForbidden, HTTPStatus: 403}, 403, ErrForbidden, "forbidden"},
		{&OwnerActionError{Path: "security.passkeys", Reason: "last_factor", Conflict: true}, 409, ErrConflict, "revision_conflict"},
		{tokenField("security.token_collections", "token_collection"), 422, ErrForbidden, "invalid_fields"},
	} {
		w := httptest.NewRecorder()
		fail(w, tc.err)
		var out struct {
			Error struct {
				Code   string
				Fields []struct{ Path, Code string }
			}
		}
		if !errors.Is(tc.err, tc.cause) || w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Error.Code != tc.code || len(out.Error.Fields) != 1 {
			t.Fatalf("bad owner error: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	fail(w, ErrAuth)
	if w.Code != 401 {
		t.Fatal("anonymous credentials changed")
	}
	var out map[string]map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	if _, ok := out["error"]["fields"]; ok {
		t.Fatal("anonymous auth disclosed field details")
	}
}
func TestTokenInputErrorsIdentifyFields(t *testing.T) {
	for _, tc := range []struct {
		path string
		in   TokenInput
	}{
		{"security.token_label", TokenInput{Label: "", Scopes: []string{"draft"}, AllowArticles: true, Days: 90}},
		{"security.token_scopes", TokenInput{Label: "Token", AllowArticles: true, Days: 90}},
		{"security.token_collections", TokenInput{Label: "Token", Scopes: []string{"draft"}, Days: 90}},
		{"security.token_days", TokenInput{Label: "Token", Scopes: []string{"draft"}, AllowArticles: true, Days: 0}},
	} {
		s := &Service{}
		_, err := s.CreatePublisherToken(context.Background(), Session{}, tc.in)
		var v *OwnerActionError
		if !errors.As(err, &v) || v.Path != tc.path {
			t.Fatalf("missing token issue: %v", err)
		}
	}
}

func TestAuthenticatorSetupErrorsDoNotApplyFactors(t *testing.T) {
	s := fixture(t)
	keyFile(t, s)
	raw, session := loggedIn(t, s)
	ctx := context.Background()
	_, _, err := s.enrollTOTP(ctx, session, raw, "short")
	var v *OwnerActionError
	if !errors.As(err, &v) || v.Reason != "password_length" {
		t.Fatalf("missing password reason: %v", err)
	}
	flow, _, err := s.enrollTOTP(ctx, session, raw, "a long test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	err = s.confirmTOTP(ctx, session, flow, raw, "bad-code", "")
	if !errors.As(err, &v) || v.Reason != "otp_invalid" {
		t.Fatalf("missing code reason: %v", err)
	}
	err = s.confirmTOTP(ctx, session, "expired", raw, "123456", "")
	if !errors.As(err, &v) || v.Reason != "enrollment_expired" {
		t.Fatalf("missing setup reason: %v", err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM owner WHERE password_hash IS NULL AND totp_cipher IS NULL").Scan(&n)
	if n != 1 {
		t.Fatal("invalid enrollment applied factors")
	}
	if _, err = s.Session(ctx, raw, true); err != nil {
		t.Fatal("invalid enrollment changed the current session")
	}
}

package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
)

func TestStaffInvitationPasskeyReplayExpiryAndRevocation(t *testing.T) {
	s := fixture(t)
	ctx := t.Context()
	owner, key, cred := enrolled(t, s)
	flow, err := s.Start(ctx, "staff-root")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.beginPasskey(ctx, flow, "", "login"); err != nil {
		t.Fatal(err)
	}
	root, err := s.finishLogin(request(loginBody(t, s, flow, owner, key, cred, s.Config.BaseURL, "webauthn.get", 5, 1), flow, ""))
	if err != nil {
		t.Fatal(err)
	}
	management := httptest.NewRequest("POST", s.Config.BaseURL+"/api/v1/staff/management/invitations", nil)
	management.AddCookie(&http.Cookie{Name: ownerCookie, Value: root})
	management.Header.Set("Origin", s.Config.BaseURL)
	management.Header.Set("X-CSRF-Token", csrf(root))
	create := func(name string) (StaffInvite, string) {
		t.Helper()
		in, token, err := s.createStaffInvite(management, name, "editor")
		if err != nil || token == "" {
			t.Fatal(in, err)
		}
		return in, token
	}
	expired, token := create("Expired")
	if _, err = s.DB.Exec("UPDATE staff_invites SET expires_at=? WHERE id=?", s.now(), expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.beginStaff(ctx, "enroll", token, "test"); !errors.Is(err, ErrAuth) {
		t.Fatal("expired invite", err)
	}
	cancelled, token := create("Cancelled")
	if err = s.revokeStaff(management, cancelled.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.beginStaff(ctx, "enroll", token, "test"); !errors.Is(err, ErrAuth) {
		t.Fatal("revoked invite", err)
	}
	invite, token := create("Editor")
	first, _, err := s.beginStaff(ctx, "enroll", token, "test")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.beginStaff(ctx, "enroll", token, "test")
	if err != nil {
		t.Fatal(err)
	}
	challenge := func(flow string) string {
		t.Helper()
		var out string
		if err := s.DB.QueryRow("SELECT challenge FROM staff_flows WHERE token_hash=?", digest(flow)).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	ceremony := func(raw, body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, s.Config.BaseURL, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: staffFlowCookie, Value: raw})
		return r
	}
	editorKey, editorCredential := simulatedKey(t)
	firstRequest := ceremony(first, registrationBodyForChallenge(t, s, challenge(first), editorKey, editorCredential, s.Config.BaseURL, 0x45))
	editor, err := s.finishStaff(firstRequest, "enroll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.finishStaff(ceremony(second, "{}"), "enroll"); !errors.Is(err, ErrAuth) {
		t.Fatal("parallel invite replay", err)
	}
	if _, _, err = s.beginStaff(ctx, "enroll", token, "test"); !errors.Is(err, ErrAuth) {
		t.Fatal("consumed invite reused", err)
	}
	login, _, err := s.beginStaff(ctx, "login", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	editor, err = s.finishStaff(ceremony(login, loginBodyForChallenge(t, s, challenge(login), invite.ID, editorKey, editorCredential, s.Config.BaseURL, "webauthn.get", 5, 1)), "login")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, editor, false); !errors.Is(err, ErrAuth) {
		t.Fatal("staff authenticated as original owner", err)
	}
	staffRequest := httptest.NewRequest(http.MethodPut, s.Config.BaseURL+"/api/v1/staff/content/first/en/draft", nil)
	staffRequest.AddCookie(&http.Cookie{Name: staffCookie, Value: editor})
	staffRequest.Header.Set("Origin", s.Config.BaseURL)
	staffRequest.Header.Set("X-CSRF-Token", csrf(editor))
	if _, _, err = s.StaffRequest(staffRequest, StaffPublish); !errors.Is(err, ErrForbidden) {
		t.Fatal("editor could publish", err)
	}
	prechecked, actualOwner, err := s.StaffRequest(staffRequest, StaffWriteDraft)
	if err != nil || actualOwner != owner || access.StaffActor(prechecked.Context()) != invite.ID {
		t.Fatal(actualOwner, err)
	}
	if err = s.revokeStaff(management, invite.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.StaffRequest(staffRequest, StaffReadContent); !errors.Is(err, ErrAuth) {
		t.Fatal("revoked staff could read", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = access.CheckMutation(prechecked.Context(), tx, ""); !errors.Is(err, ErrAuth) {
		t.Fatal("in-flight staff write survived revocation", err)
	}
	_ = tx.Rollback()
	if _, err = s.Session(ctx, root, true); err != nil {
		t.Fatal("original owner session changed", err)
	}
}

func TestStaffRoleGrantsAreAllowlisted(t *testing.T) {
	for _, role := range []string{"owner", "editor", "store_manager", "unknown"} {
		for _, permission := range []StaffPermission{"permissions:write", "plugin:activate", "secrets:write", "refund:write"} {
			if staffAllows(role, permission) {
				t.Fatal(role, permission)
			}
		}
	}
	if !staffAllows("store_manager", StaffManageOrders) || staffAllows("store_manager", StaffReadContent) ||
		staffAllows("editor", StaffManageOrders) || !staffAllows("owner", StaffPublish) {
		t.Fatal("unexpected staff policy")
	}
}

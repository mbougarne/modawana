package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func simulatedKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	id := make([]byte, 32)
	rand.Read(id)
	return key, id
}
func registerBody(t *testing.T, s *Service, flow string, key *ecdsa.PrivateKey, id []byte, origin string, flags byte) string {
	t.Helper()
	f, _ := s.flow(context.Background(), flow)
	var sd struct {
		Challenge string `json:"challenge"`
	}
	json.Unmarshal([]byte(f.Challenge), &sd)
	client, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": sd.Challenge, "origin": origin})
	pub, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: key.X.FillBytes(make([]byte, 32)), -3: key.Y.FillBytes(make([]byte, 32))})
	rp := sha256.Sum256([]byte(s.Config.RPID))
	auth := append(rp[:], flags, 0, 0, 0, 0)
	auth = append(auth, make([]byte, 16)...)
	auth = append(auth, byte(len(id)>>8), byte(len(id)))
	auth = append(auth, id...)
	auth = append(auth, pub...)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "authData": auth, "attStmt": map[string]any{}})
	body, _ := json.Marshal(map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{"attestationObject": b64(att), "clientDataJSON": b64(client), "transports": []string{"internal", "hybrid"}}, "clientExtensionResults": map[string]any{}})
	return string(body)
}
func loginBody(t *testing.T, s *Service, flow, owner string, key *ecdsa.PrivateKey, id []byte, origin, kind string, flags byte, counter uint32) string {
	t.Helper()
	f, _ := s.flow(context.Background(), flow)
	var sd struct {
		Challenge string `json:"challenge"`
	}
	json.Unmarshal([]byte(f.Challenge), &sd)
	client, _ := json.Marshal(map[string]any{"type": kind, "challenge": sd.Challenge, "origin": origin})
	rp := sha256.Sum256([]byte(s.Config.RPID))
	auth := append(rp[:], flags, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(auth[33:], counter)
	ch := sha256.Sum256(client)
	signed := append(append([]byte{}, auth...), ch[:]...)
	hash := sha256.Sum256(signed)
	sig, e := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{"authenticatorData": b64(auth), "clientDataJSON": b64(client), "signature": b64(sig), "userHandle": b64([]byte(owner))}, "clientExtensionResults": map[string]any{}})
	return string(body)
}
func request(body, flow, owner string) *http.Request {
	r := httptest.NewRequest("POST", "https://blog.example", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: flowCookie, Value: flow})
	if owner != "" {
		r.AddCookie(&http.Cookie{Name: ownerCookie, Value: owner})
	}
	return r
}
func enrolled(t *testing.T, s *Service) (string, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	flow, owner := bootstrap(t, s)
	key, id := simulatedKey(t)
	opts, e := s.beginPasskey(context.Background(), flow, "", "register")
	if e != nil {
		t.Fatal(e)
	}
	o := opts.(*protocol.CredentialCreation)
	if o.Response.AuthenticatorSelection.UserVerification != protocol.VerificationRequired || o.Response.AuthenticatorSelection.ResidentKey != protocol.ResidentKeyRequirementRequired || o.Response.AuthenticatorSelection.AuthenticatorAttachment != "" {
		t.Fatal("restricted authenticator or missing UV")
	}
	body := registerBody(t, s, flow, key, id, s.Config.BaseURL, 0x45)
	if e = s.finishRegister(request(body, flow, ""), "Daily key"); e != nil {
		t.Fatal("registration", e)
	}
	return owner, key, id
}
func TestPasskeyVerificationAndReplay(t *testing.T) {
	s := fixture(t)
	owner, key, id := enrolled(t, s)
	ctx := context.Background()
	for _, tc := range []struct {
		name, origin, kind, handle string
		flags                      byte
	}{{"origin", "https://evil.test", "webauthn.get", owner, 5}, {"type", s.Config.BaseURL, "webauthn.create", owner, 5}, {"uv", s.Config.BaseURL, "webauthn.get", owner, 1}, {"owner", s.Config.BaseURL, "webauthn.get", "someone-else", 5}} {
		t.Run(tc.name, func(t *testing.T) {
			flow, _ := s.Start(ctx, tc.name)
			s.beginPasskey(ctx, flow, "", "login")
			body := loginBody(t, s, flow, tc.handle, key, id, tc.origin, tc.kind, tc.flags, 1)
			if _, e := s.finishLogin(request(body, flow, "")); e == nil {
				t.Fatal("invalid assertion accepted")
			}
			if _, _, e := s.claim(ctx, flow, "login"); e == nil {
				t.Fatal("challenge replay")
			}
		})
	}
	flow, _ := s.Start(ctx, "success")
	s.beginPasskey(ctx, flow, "", "login")
	body := loginBody(t, s, flow, owner, key, id, s.Config.BaseURL, "webauthn.get", 5, 1)
	raw, e := s.finishLogin(request(body, flow, ""))
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Session(ctx, raw, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.finishLogin(request(body, flow, "")); e == nil {
		t.Fatal("login replay")
	}
	if e = s.revokeKey(ctx, session, b64(id), ""); !errors.Is(e, ErrConflict) {
		t.Fatal("last factor removed", e)
	}
	flow, _ = s.Start(ctx, "add")
	if _, e = s.beginPasskey(ctx, flow, raw, "register"); e != nil {
		t.Fatal(e)
	}
	key2, id2 := simulatedKey(t)
	if e = s.finishRegister(request(registerBody(t, s, flow, key2, id2, s.Config.BaseURL, 0x45), flow, raw), "Backup"); e != nil {
		t.Fatal(e)
	}
	if e = s.revokeKey(ctx, session, b64(id), ""); !errors.Is(e, ErrConflict) {
		t.Fatal("untested backup allowed removal", e)
	}
	flow, _ = s.Start(ctx, "test-new")
	s.beginPasskey(ctx, flow, "", "login")
	raw2, e := s.finishLogin(request(loginBody(t, s, flow, owner, key2, id2, s.Config.BaseURL, "webauthn.get", 5, 1), flow, raw))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, raw, false); e == nil {
		t.Fatal("old session not rotated")
	}
	session, _ = s.Session(ctx, raw2, true)
	if e = s.revokeKey(ctx, session, b64(id), ""); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, raw2, false); e == nil {
		t.Fatal("factor revocation left sessions")
	}
}
func TestRegistrationRequiresUVAndSignature(t *testing.T) {
	s := fixture(t)
	flow, _ := bootstrap(t, s)
	key, id := simulatedKey(t)
	s.beginPasskey(context.Background(), flow, "", "register")
	if e := s.finishRegister(request(registerBody(t, s, flow, key, id, s.Config.BaseURL, 0x41), flow, ""), "key"); e == nil {
		t.Fatal("missing UV accepted")
	}
	s2 := fixture(t)
	owner, key, id := enrolled(t, s2)
	flow, _ = s2.Start(context.Background(), "signature")
	s2.beginPasskey(context.Background(), flow, "", "login")
	wrong, _ := simulatedKey(t)
	if _, e := s2.finishLogin(request(loginBody(t, s2, flow, owner, wrong, id, s2.Config.BaseURL, "webauthn.get", 5, 1), flow, "")); e == nil {
		t.Fatal("bad signature accepted")
	}
}

func TestWrongRPChallengeExpiryAndRevocation(t *testing.T) {
	s := fixture(t)
	owner, key, id := enrolled(t, s)
	ctx := context.Background()
	for _, mode := range []string{"rp", "challenge", "expiry", "revoked"} {
		flow, _ := s.Start(ctx, mode)
		s.beginPasskey(ctx, flow, "", "login")
		original := s.Config.RPID
		if mode == "rp" {
			s.Config.RPID = "other.test"
		}
		body := loginBody(t, s, flow, owner, key, id, s.Config.BaseURL, "webauthn.get", 5, 1)
		s.Config.RPID = original
		if mode == "challenge" {
			var v map[string]any
			json.Unmarshal([]byte(body), &v)
			response := v["response"].(map[string]any)
			client, _ := base64.RawURLEncoding.DecodeString(response["clientDataJSON"].(string))
			var c map[string]any
			json.Unmarshal(client, &c)
			c["challenge"] = b64([]byte("wrong challenge"))
			client, _ = json.Marshal(c)
			response["clientDataJSON"] = b64(client)
			raw, _ := json.Marshal(v)
			body = string(raw)
		}
		if mode == "expiry" {
			s.DB.Exec("UPDATE auth_flows SET expires_at=? WHERE token_hash=?", s.now(), digest(flow))
		}
		if mode == "revoked" {
			s.DB.Exec("UPDATE passkeys SET revoked_at=? WHERE id=?", s.now(), b64(id))
		}
		if _, e := s.finishLogin(request(body, flow, "")); e == nil {
			t.Fatal("accepted", mode)
		}
	}
}

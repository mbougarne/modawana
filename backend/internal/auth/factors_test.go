package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/pquerna/otp/totp"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func keyFile(t *testing.T, s *Service) {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	p := filepath.Join(t.TempDir(), "auth.key")
	if e := os.WriteFile(p, key, 0600); e != nil {
		t.Fatal(e)
	}
	s.Config.AuthKeyFile = p
	s.Config.DataRoot = t.TempDir()
}
func loggedIn(t *testing.T, s *Service) (string, Session) {
	t.Helper()
	owner, key, id := enrolled(t, s)
	ctx := context.Background()
	flow, _ := s.Start(ctx, "login")
	s.beginPasskey(ctx, flow, "", "login")
	raw, e := s.finishLogin(request(loginBody(t, s, flow, owner, key, id, s.Config.BaseURL, "webauthn.get", 5, 1), flow, ""))
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Session(ctx, raw, true)
	if e != nil {
		t.Fatal(e)
	}
	return raw, session
}
func TestTOTPEnrollmentPasswordReplayAndSkew(t *testing.T) {
	s := fixture(t)
	keyFile(t, s)
	raw, session := loggedIn(t, s)
	ctx := context.Background()
	password := "a long test passphrase"
	flow, seed, e := s.enrollTOTP(ctx, session, raw, password)
	if e != nil {
		t.Fatal(e)
	}
	code, _ := totp.GenerateCode(seed, s.Now())
	if e = s.confirmTOTP(ctx, session, flow, raw, code, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Session(ctx, raw, false); e == nil {
		t.Fatal("enrollment failed to revoke")
	}
	var encrypted []byte
	var hash string
	s.DB.QueryRow("SELECT password_hash,totp_cipher FROM owner").Scan(&hash, &encrypted)
	if hash == password || string(encrypted) == seed {
		t.Fatal("plaintext factor")
	}
	if !verifyPassword(hash, password) || verifyPassword(hash, "wrong") {
		t.Fatal("password hash")
	}
	login, _ := s.Start(ctx, "pw")
	pending, e := s.password(ctx, login, "owner@example.test", password, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, pending, false); e == nil {
		t.Fatal("password-only")
	}
	if _, e = s.verifyOTP(ctx, pending, code, "", ""); e == nil {
		t.Fatal("enrollment OTP replay")
	}
	now := s.Now()
	s.Now = func() time.Time { return now.Add(30 * time.Second) }
	code, _ = totp.GenerateCode(seed, s.Now())
	full, e := s.verifyOTP(ctx, pending, code, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, full, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.verifyOTP(ctx, pending, code, "", ""); e == nil {
		t.Fatal("pending replay")
	}
	if _, e = acceptedStep(seed, code, s.Now().Add(30*time.Second), -1); e != nil {
		t.Fatal("allowed skew", e)
	}
	if _, e = acceptedStep(seed, code, s.Now().Add(61*time.Second), -1); e == nil {
		t.Fatal("excess skew")
	}
	if _, e = s.unseal(encrypted, "wrong-owner"); e == nil {
		t.Fatal("AAD ignored")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, e = s.unseal(encrypted, session.Owner); e == nil {
		t.Fatal("cipher tamper")
	}
}
func TestRecoveryConcurrentUseAndLocalGrant(t *testing.T) {
	s := fixture(t)
	_, session := loggedIn(t, s)
	ctx := context.Background()
	codes, e := s.regenerateCodes(ctx, session, "")
	if e != nil || len(codes) != 10 {
		t.Fatal(e)
	}
	for _, c := range codes {
		if !validSecret(c) {
			t.Fatal("entropy")
		}
	}
	a, _ := s.Start(ctx, "a")
	b, _ := s.Start(ctx, "b")
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for _, flow := range []string{a, b} {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			raw, full, e := s.recoverCode(ctx, f, codes[0], "", "")
			if e == nil {
				if full {
					t.Error("recovery became owner")
				}
				if _, e := s.Session(ctx, raw, false); e == nil {
					t.Error("restricted owner")
				}
			}
			results <- e == nil
		}(flow)
	}
	wg.Wait()
	close(results)
	success := 0
	for ok := range results {
		if ok {
			success++
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	grant, e := s.Grant(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	f, _ := s.Start(ctx, "local")
	enrollment, e := s.Redeem(ctx, f, grant, "owner@example.test", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, enrollment, false); e == nil {
		t.Fatal("local grant owner")
	}
	if _, e = s.beginPasskey(ctx, enrollment, "", "register"); e != nil {
		t.Fatal(e)
	}
	key, id := simulatedKey(t)
	if e = s.finishRegister(request(registerBody(t, s, enrollment, key, id, s.Config.BaseURL, 0x45), enrollment, ""), "Recovered"); e != nil {
		t.Fatal(e)
	}
	f, _ = s.Start(ctx, "recovered")
	s.beginPasskey(ctx, f, "", "login")
	raw, e := s.finishLogin(request(loginBody(t, s, f, session.Owner, key, id, s.Config.BaseURL, "webauthn.get", 5, 1), f, ""))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, raw, true); e != nil {
		t.Fatal(e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM passkeys WHERE revoked_at IS NULL").Scan(&count)
	if count != 1 {
		t.Fatal("old lost factors not revoked")
	}
}
func TestFallbackRecoveryRequiresPassword(t *testing.T) {
	s := fixture(t)
	keyFile(t, s)
	_, session := loggedIn(t, s)
	ctx := context.Background()
	codes, _ := s.regenerateCodes(ctx, session, "")
	hash, _ := hashPassword("a long test password")
	encrypted, _ := s.seal("JBSWY3DPEHPK3PXP", session.Owner)
	s.DB.Exec("UPDATE owner SET password_hash=?,totp_cipher=?", hash, encrypted)
	f, _ := s.Start(ctx, "anon")
	if _, _, e := s.recoverCode(ctx, f, codes[0], "", ""); e == nil {
		t.Fatal("code bypassed password")
	}
	f, _ = s.password(ctx, f, "owner@example.test", "a long test password", "")
	raw, full, e := s.recoverCode(ctx, f, codes[0], "", "")
	if e != nil || !full {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, raw, true); e != nil {
		t.Fatal(e)
	}
}
func BenchmarkArgon2id(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, e := hashPassword("benchmark passphrase only"); e != nil {
			b.Fatal(e)
		}
	}
}

func TestKeyFileMustBeSeparatePrivateAndRegular(t *testing.T) {
	s := fixture(t)
	keyFile(t, s)
	path := s.Config.AuthKeyFile
	if _, e := s.encryptionKey(); e != nil {
		t.Fatal(e)
	}
	os.Chmod(path, 0644)
	if _, e := s.encryptionKey(); e == nil {
		t.Fatal("public key file accepted")
	}
	os.Chmod(path, 0600)
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(path, link)
	s.Config.AuthKeyFile = link
	if _, e := s.encryptionKey(); e == nil {
		t.Fatal("symlink key accepted")
	}
	s.Config.AuthKeyFile = path
	s.Config.DataRoot = filepath.Dir(path)
	if _, e := s.encryptionKey(); e == nil {
		t.Fatal("database-adjacent key accepted")
	}
}

func TestMissingEncryptionKeyCannotJustifyRemovingLastPasskey(t *testing.T) {
	s := fixture(t)
	keyFile(t, s)
	_, session := loggedIn(t, s)
	encrypted, e := s.seal("JBSWY3DPEHPK3PXP", session.Owner)
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("UPDATE owner SET password_hash='configured',totp_cipher=?", encrypted)
	os.Remove(s.Config.AuthKeyFile)
	if e = s.revokeKey(context.Background(), session, session.Credential, ""); !errors.Is(e, ErrConflict) {
		t.Fatal("removed last usable method", e)
	}
}

package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"
)

func (s *Service) encryptionKey() ([]byte, error) {
	p := s.Config.AuthKeyFile
	if !filepath.IsAbs(p) {
		return nil, ErrUnavailable
	}
	resolved, e := filepath.EvalSymlinks(p)
	if e != nil {
		return nil, ErrUnavailable
	}
	dataRoot, e := filepath.EvalSymlinks(s.Config.DataRoot)
	if e != nil {
		return nil, ErrUnavailable
	}
	rel, e := filepath.Rel(dataRoot, resolved)
	if e != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return nil, ErrUnavailable
	}
	f, e := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return nil, ErrUnavailable
	}
	key := make([]byte, 32)
	if _, e = f.ReadAt(key, 0); e != nil {
		return nil, ErrUnavailable
	}
	return key, nil
}
func (s *Service) seal(seed, owner string) ([]byte, error) {
	key, e := s.encryptionKey()
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return aead.Seal(nonce, nonce, []byte(seed), []byte("totp:"+owner)), nil
}
func (s *Service) unseal(data []byte, owner string) (string, error) {
	key, e := s.encryptionKey()
	if e != nil {
		return "", e
	}
	block, _ := aes.NewCipher(key)
	aead, e := cipher.NewGCM(block)
	if e != nil || len(data) < aead.NonceSize() {
		return "", ErrAuth
	}
	plain, e := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("totp:"+owner))
	if e != nil {
		return "", ErrAuth
	}
	return string(plain), nil
}
func hashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return "", ErrAuth
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key := argon2.IDKey([]byte(password), salt, 3, 65536, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func verifyPassword(encoded, password string) bool {
	if len(password) > 1024 {
		return false
	}
	parts := strings.Split(encoded, "$")
	valid := len(parts) == 6
	var salt, want []byte
	if valid {
		valid = parts[1] == "argon2id" && parts[2] == "v=19" && parts[3] == "m=65536,t=3,p=2"
		salt, _ = base64.RawStdEncoding.DecodeString(parts[4])
		want, _ = base64.RawStdEncoding.DecodeString(parts[5])
		valid = valid && len(salt) == 16 && len(want) == 32
	}
	if !valid {
		salt = make([]byte, 16)
		want = make([]byte, 32)
	}
	got := argon2.IDKey([]byte(password), salt, 3, 65536, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1 && valid
}
func acceptedStep(seed, code string, now time.Time, last int64) (int64, error) {
	if len(code) != 6 {
		return 0, ErrAuth
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, ErrAuth
		}
	}
	current := now.Unix() / 30
	for _, offset := range []int64{0, -1, 1} {
		step := current + offset
		if step <= last {
			continue
		}
		valid, e := totp.ValidateCustom(code, seed, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if e == nil && valid {
			return step, nil
		}
	}
	return 0, ErrAuth
}
func (s *Service) password(ctx context.Context, flowRaw, email, password, request string) (string, error) {
	release, e := s.hashSlot()
	if e != nil {
		return "", e
	}
	defer release()
	f, e := s.flow(ctx, flowRaw)
	if e != nil || f.Kind != "anonymous" {
		return "", ErrAuth
	}
	var owner, hash string
	var version int64
	e = s.DB.QueryRowContext(ctx, "SELECT id,password_hash,auth_version FROM owner WHERE login_email=? AND password_hash IS NOT NULL AND totp_cipher IS NOT NULL", strings.ToLower(email)).Scan(&owner, &hash, &version)
	valid := verifyPassword(hash, password)
	if e != nil || !valid {
		return "", ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var current int64
	if tx.QueryRowContext(ctx, "SELECT auth_version FROM owner WHERE id=?", owner).Scan(&current) != nil || current != version {
		return "", ErrAuth
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return "", e
	}
	raw, e := s.newFlow(ctx, tx, "password", owner, version)
	if e != nil {
		return "", e
	}
	if e = s.audit(ctx, tx, "password_pending", request); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return raw, nil
}
func (s *Service) verifyOTP(ctx context.Context, flowRaw, code, old, request string) (string, error) {
	f, e := s.flow(ctx, flowRaw)
	if e != nil || f.Kind != "password" {
		return "", ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var encrypted []byte
	var last, version int64
	if tx.QueryRowContext(ctx, "SELECT totp_cipher,totp_step,auth_version FROM owner WHERE id=?", f.Owner).Scan(&encrypted, &last, &version) != nil || version != f.Version {
		return "", ErrAuth
	}
	seed, e := s.unseal(encrypted, f.Owner)
	if e != nil {
		return "", e
	}
	step, e := acceptedStep(seed, code, s.Now(), last)
	if e != nil {
		return "", e
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE owner SET totp_step=? WHERE id=?", step, f.Owner); e != nil {
		return "", e
	}
	raw, e := s.complete(ctx, tx, f.Owner, "password_totp", "", old, request)
	if e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return raw, nil
}

// freshInTx closes the race between a middleware check and concurrent revocation.
func (s *Service) freshInTx(ctx context.Context, tx *sql.Tx, session Session) error {
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions s JOIN owner o ON o.id=s.owner_id AND o.auth_version=s.auth_version WHERE s.id=? AND s.kind='owner' AND s.revoked_at IS NULL AND s.expires_at>? AND coalesce(s.last_seen_at,s.created_at)>? AND s.authenticated_at>?`, session.ID, s.now(), s.nowIdle(), db.UTC(s.Now().Add(-30*time.Minute))).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrForbidden
	}
	return nil
}
func (s *Service) enrollTOTP(ctx context.Context, session Session, raw, password string) (string, string, error) {
	release, e := s.hashSlot()
	if e != nil {
		return "", "", e
	}
	defer release()
	hash, e := hashPassword(password)
	if e != nil {
		return "", "", actionField("security.password", "password_length")
	}
	key, e := totp.Generate(totp.GenerateOpts{Issuer: s.Config.RPID, AccountName: "Owner", Period: 30, SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if e != nil {
		return "", "", e
	}
	encrypted, e := s.seal(key.Secret(), session.Owner)
	if e != nil {
		return "", "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", "", e
	}
	defer tx.Rollback()
	if e = s.freshInTx(ctx, tx, session); e != nil {
		return "", "", e
	}
	next, e := s.newFlow(ctx, tx, "managed", session.Owner, session.Version)
	if e != nil {
		return "", "", e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE auth_flows SET pending_password=?,pending_totp=?,session_hash=? WHERE token_hash=?", hash, encrypted, digest(raw), digest(next)); e != nil {
		return "", "", e
	}
	if e = tx.Commit(); e != nil {
		return "", "", e
	}
	return next, key.Secret(), nil
}
func (s *Service) confirmTOTP(ctx context.Context, session Session, flowRaw, raw, code, request string) error {
	f, e := s.flow(ctx, flowRaw)
	if e != nil {
		if errors.Is(e, ErrAuth) {
			return actionField("security.code", "enrollment_expired")
		}
		return e
	}
	if f.Kind != "managed" || f.Owner != session.Owner {
		return ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.freshInTx(ctx, tx, session); e != nil {
		return e
	}
	var hash, binding string
	var encrypted []byte
	if tx.QueryRowContext(ctx, "SELECT pending_password,pending_totp,session_hash FROM auth_flows WHERE token_hash=?", f.Hash).Scan(&hash, &encrypted, &binding) != nil || binding != digest(raw) {
		return ErrAuth
	}
	seed, e := s.unseal(encrypted, session.Owner)
	if e != nil {
		return e
	}
	step, e := acceptedStep(seed, code, s.Now(), -1)
	if e != nil {
		if errors.Is(e, ErrAuth) {
			return actionField("security.code", "otp_invalid")
		}
		return e
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE owner SET password_hash=?,totp_cipher=?,totp_step=? WHERE id=?", hash, encrypted, step, session.Owner); e != nil {
		return e
	}
	if e = s.invalidate(ctx, tx); e != nil {
		return e
	}
	if e = s.audit(ctx, tx, "totp_enabled", request); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) regenerateCodes(ctx context.Context, session Session, request string) ([]string, error) {
	codes := make([]string, 10)
	for i := range codes {
		raw, e := secret()
		if e != nil {
			return nil, e
		}
		codes[i] = raw
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = s.freshInTx(ctx, tx, session); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE owner_id=?", session.Owner); e != nil {
		return nil, e
	}
	for _, code := range codes {
		if _, e = tx.ExecContext(ctx, "INSERT INTO recovery_codes(code_hash,owner_id) VALUES(?,?)", digest(code), session.Owner); e != nil {
			return nil, e
		}
	}
	if e = s.audit(ctx, tx, "recovery_codes_regenerated", request); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return codes, nil
}
func (s *Service) recoverCode(ctx context.Context, flowRaw, code, old, request string) (string, bool, error) {
	f, e := s.flow(ctx, flowRaw)
	if e != nil || !validSecret(code) || (f.Kind != "anonymous" && f.Kind != "password") {
		return "", false, ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", false, e
	}
	defer tx.Rollback()
	var owner string
	var version int64
	var fallback int
	if tx.QueryRowContext(ctx, `SELECT o.id,o.auth_version,(o.password_hash IS NOT NULL AND o.totp_cipher IS NOT NULL) FROM recovery_codes c JOIN owner o ON o.id=c.owner_id WHERE c.code_hash=? AND c.consumed_at IS NULL`, digest(code)).Scan(&owner, &version, &fallback) != nil {
		return "", false, ErrAuth
	}
	if fallback == 1 && (f.Kind != "password" || f.Owner != owner || f.Version != version) {
		return "", false, ErrAuth
	}
	if fallback == 0 && f.Kind != "anonymous" {
		return "", false, ErrAuth
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return "", false, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE recovery_codes SET consumed_at=? WHERE code_hash=? AND consumed_at IS NULL", s.now(), digest(code)); e != nil {
		return "", false, e
	}
	var raw string
	if fallback == 1 {
		raw, e = s.complete(ctx, tx, owner, "password_totp", "", old, request)
	} else {
		if e = s.invalidate(ctx, tx); e != nil {
			return "", false, e
		}
		version++
		raw, e = s.newFlow(ctx, tx, "recovery", owner, version)
	}
	if e != nil {
		return "", false, e
	}
	if e = s.audit(ctx, tx, "recovery_code_used", request); e != nil {
		return "", false, e
	}
	if e = tx.Commit(); e != nil {
		return "", false, e
	}
	return raw, fallback == 1, nil
}

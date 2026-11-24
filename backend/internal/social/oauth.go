package social

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/locale"
)

const oauthLifetime = 10 * time.Minute

type Connection struct {
	ID        string `json:"id"`
	Instance  string `json:"instance"`
	Account   string `json:"account"`
	URL       string `json:"url"`
	accountID string
	token     []byte
}

type Configuration struct {
	Instance   string      `json:"instance"`
	Connection *Connection `json:"connection,omitempty"`
}

func (s *Service) Configuration(ctx context.Context) (Configuration, error) {
	var result Configuration
	if s.provider == nil {
		return result, nil
	}
	result.Instance = s.provider.Instance()
	p, err := readConnection(ctx, s.DB, result.Instance)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err == nil {
		result.Connection = &p
	}
	return result, err
}

type rowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readConnection(ctx context.Context, q rowReader, instance string) (Connection, error) {
	var p Connection
	err := q.QueryRowContext(ctx, `SELECT id,instance,account_id,account,account_url,token FROM social_connections
		WHERE slot=1 AND instance=?`, instance).Scan(&p.ID, &p.Instance, &p.accountID, &p.Account, &p.URL, &p.token)
	return p, err
}

func randomProof() string {
	var value [32]byte
	rand.Read(value[:])
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func validProof(value string) bool {
	if len(value) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) box() (cipher.AEAD, error) {
	key, err := s.key()
	if err != nil || len(key) != 32 {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("modawana/social-publishing/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, ErrUnavailable
	}
	return cipher.NewGCMWithRandomNonce(block)
}

type Start struct {
	URL          string
	BrowserProof string
}

type FreshSession func(context.Context, *sql.Tx, string) error

func (s *Service) Start(ctx context.Context, language, session string, check FreshSession) (Start, error) {
	var result Start
	if s.provider == nil {
		return result, ErrUnavailable
	}
	if !locale.Valid(language) {
		return result, ErrInvalid
	}
	box, err := s.box()
	if err != nil {
		return result, err
	}
	state, browser, verifier := randomProof(), randomProof(), randomProof()
	hash := digest(state)
	instance := s.provider.Instance()
	sealed := box.Seal(nil, nil, []byte(verifier), []byte("oauth:"+instance+":"+hash))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := check(ctx, tx, session); err != nil {
		return result, err
	}
	// One active connection flow, including one currently exchanging a code.
	if _, err := tx.ExecContext(ctx, "DELETE FROM social_oauth_flows"); err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO social_oauth_flows(state_hash,browser_hash,session_id,instance,locale,verifier,expires_at)
		VALUES (?,?,?,?,?,?,?)`, hash, digest(browser), session, instance, language, sealed, time.Now().Add(oauthLifetime).Unix())
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	challenge := sha256.Sum256([]byte(verifier))
	return Start{s.provider.AuthorizationURL(state, base64.RawURLEncoding.EncodeToString(challenge[:])), browser}, nil
}

func (s *Service) claim(ctx context.Context, hash, browser string, check FreshSession) (string, string, []byte, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", "", nil, err
	}
	defer tx.Rollback()
	var session, language string
	var verifier []byte
	err = tx.QueryRowContext(ctx, `UPDATE social_oauth_flows SET claimed=1 WHERE state_hash=? AND browser_hash=? AND instance=?
		AND claimed=0 AND expires_at>? RETURNING session_id,locale,verifier`, hash, digest(browser), s.provider.Instance(), time.Now().Unix()).Scan(&session, &language, &verifier)
	if err != nil {
		return "", "", nil, err
	}
	if err = check(ctx, tx, session); err != nil {
		return "", "", nil, err
	}
	return session, language, verifier, tx.Commit()
}

func (s *Service) Complete(ctx context.Context, state, browser, code string, check FreshSession) (string, error) {
	if s.provider == nil {
		return "", ErrUnavailable
	}
	if !validProof(state) || !validProof(browser) || code == "" || len(code) > 2048 {
		return "", ErrInvalid
	}
	instance, hash := s.provider.Instance(), digest(state)
	session, language, sealed, err := s.claim(ctx, hash, browser, check)
	if err != nil {
		return language, err
	}
	box, err := s.box()
	if err != nil {
		return language, err
	}
	verifier, err := box.Open(nil, nil, sealed, []byte("oauth:"+instance+":"+hash))
	if err != nil {
		return language, ErrUnavailable
	}
	token, err := s.provider.Exchange(ctx, code, string(verifier))
	if err != nil {
		return language, err
	}
	account, err := s.provider.Verify(ctx, token)
	if err != nil {
		return language, err
	}
	id, err := db.NewID()
	if err != nil {
		return language, err
	}
	connection := Connection{ID: id, Instance: instance, Account: account.Name, URL: account.URL, accountID: account.ID}
	ciphertext := box.Seal(nil, nil, []byte(token), connectionAAD(connection))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return language, err
	}
	defer tx.Rollback()
	if err = check(ctx, tx, session); err != nil {
		return language, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM social_oauth_flows WHERE state_hash=? AND session_id=? AND instance=?
		AND claimed=1 AND expires_at>?`, hash, session, instance, time.Now().Unix())
	if err != nil {
		return language, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return language, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO social_connections (id,slot,instance,account_id,account,account_url,token)
		VALUES (?,1,?,?,?,?,?) ON CONFLICT(slot) DO UPDATE SET id=excluded.id,instance=excluded.instance,
		account_id=excluded.account_id,account=excluded.account,account_url=excluded.account_url,token=excluded.token`, id, instance, account.ID, account.Name, account.URL, ciphertext)
	if err != nil {
		return language, err
	}
	return language, tx.Commit()
}

func connectionAAD(c Connection) []byte {
	return []byte("connection:" + c.Instance + ":" + c.ID + ":" + c.accountID)
}

func (s *Service) Disconnect(ctx context.Context, session string, check FreshSession) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = check(ctx, tx, session); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM social_oauth_flows"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM social_connections"); err != nil {
		return err
	}
	return tx.Commit()
}

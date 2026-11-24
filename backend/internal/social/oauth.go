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
	Provider  string `json:"provider"`
	Instance  string `json:"instance"`
	Account   string `json:"account"`
	URL       string `json:"url"`
	accountID string
	token     []byte
}

type Destination struct {
	Provider      string      `json:"provider"`
	Instance      string      `json:"instance"`
	Connection    *Connection `json:"connection,omitempty"`
	TextLimit     int         `json:"text_limit"`
	RequiresImage bool        `json:"requires_image"`
}

type Configuration struct {
	Destinations []Destination `json:"destinations"`
}

func (s *Service) Configuration(ctx context.Context) (Configuration, error) {
	result := Configuration{Destinations: []Destination{}}
	for _, key := range s.providerKeys() {
		p := s.providers[key]
		_, image := p.(mediaPublisher)
		dst := Destination{Provider: key, Instance: p.Instance(), TextLimit: p.MaxTextLength(), RequiresImage: image}
		conn, err := readConnection(ctx, s.DB, "provider", key)
		if err == nil && conn.Instance == p.Instance() {
			dst.Connection = &conn
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		result.Destinations = append(result.Destinations, dst)
	}
	return result, nil
}

type rowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readConnection(ctx context.Context, q rowReader, field, value string) (Connection, error) {
	var p Connection
	where := "id"
	if field == "provider" {
		where = "provider"
	}
	err := q.QueryRowContext(ctx, `SELECT id,provider,instance,account_id,account,account_url,token FROM social_connections WHERE `+where+"=?", value).
		Scan(&p.ID, &p.Provider, &p.Instance, &p.accountID, &p.Account, &p.URL, &p.token)
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

func (s *Service) Start(ctx context.Context, key, language, session string, check FreshSession) (Start, error) {
	var result Start
	provider := s.providers[key]
	if provider == nil {
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
	instance := provider.Instance()
	sealed := box.Seal(nil, nil, []byte(verifier), flowAAD(key, instance, hash))
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
	_, err = tx.ExecContext(ctx, `INSERT INTO social_oauth_flows(state_hash,browser_hash,session_id,instance,locale,verifier,expires_at,provider)
		VALUES (?,?,?,?,?,?,?,?)`, hash, digest(browser), session, instance, language, sealed, time.Now().Add(oauthLifetime).Unix(), key)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	challenge := sha256.Sum256([]byte(verifier))
	return Start{provider.AuthorizationURL(state, base64.RawURLEncoding.EncodeToString(challenge[:])), browser}, nil
}

type oauthFlow struct {
	session, language, key, instance string
	verifier                         []byte
}

func (s *Service) claim(ctx context.Context, hash, browser string, check FreshSession) (oauthFlow, error) {
	var flow oauthFlow
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return flow, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `UPDATE social_oauth_flows SET claimed=1 WHERE state_hash=? AND browser_hash=?
		AND claimed=0 AND expires_at>? RETURNING session_id,locale,provider,instance,verifier`, hash, digest(browser), time.Now().Unix()).
		Scan(&flow.session, &flow.language, &flow.key, &flow.instance, &flow.verifier)
	if err != nil {
		return flow, err
	}
	if err = check(ctx, tx, flow.session); err != nil {
		return flow, err
	}
	return flow, tx.Commit()
}

func flowAAD(key, instance, hash string) []byte {
	if key == "mastodon" {
		return []byte("oauth:" + instance + ":" + hash)
	}
	return []byte("oauth:" + key + ":" + instance + ":" + hash)
}

func (s *Service) Complete(ctx context.Context, state, browser, code string, check FreshSession) (string, error) {
	if !validProof(state) || !validProof(browser) || code == "" || len(code) > 2048 {
		return "", ErrInvalid
	}
	hash := digest(state)
	flow, err := s.claim(ctx, hash, browser, check)
	if err != nil {
		return flow.language, err
	}
	provider := s.providers[flow.key]
	if provider == nil || provider.Instance() != flow.instance {
		return flow.language, ErrUnavailable
	}
	box, err := s.box()
	if err != nil {
		return flow.language, err
	}
	verifier, err := box.Open(nil, nil, flow.verifier, flowAAD(flow.key, flow.instance, hash))
	if err != nil {
		return flow.language, ErrUnavailable
	}
	token, err := provider.Exchange(ctx, code, string(verifier))
	if err != nil {
		return flow.language, err
	}
	account, err := provider.Verify(ctx, token)
	if err != nil {
		return flow.language, err
	}
	id, err := db.NewID()
	if err != nil {
		return flow.language, err
	}
	connection := Connection{ID: id, Provider: flow.key, Instance: flow.instance, Account: account.Name, URL: account.URL, accountID: account.ID}
	ciphertext := box.Seal(nil, nil, []byte(token), connectionAAD(connection))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return flow.language, err
	}
	defer tx.Rollback()
	if err = check(ctx, tx, flow.session); err != nil {
		return flow.language, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM social_oauth_flows WHERE state_hash=? AND session_id=? AND provider=? AND instance=?
		AND claimed=1 AND expires_at>?`, hash, flow.session, flow.key, flow.instance, time.Now().Unix())
	if err != nil {
		return flow.language, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return flow.language, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO social_connections (id,provider,instance,account_id,account,account_url,token)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(provider) DO UPDATE SET id=excluded.id,instance=excluded.instance,
		account_id=excluded.account_id,account=excluded.account,account_url=excluded.account_url,token=excluded.token`,
		id, flow.key, flow.instance, account.ID, account.Name, account.URL, ciphertext)
	if err != nil {
		return flow.language, err
	}
	return flow.language, tx.Commit()
}

func connectionAAD(c Connection) []byte {
	if c.Provider == "mastodon" {
		return []byte("connection:" + c.Instance + ":" + c.ID + ":" + c.accountID)
	}
	return []byte("connection:" + c.Provider + ":" + c.Instance + ":" + c.ID + ":" + c.accountID)
}

func (s *Service) Disconnect(ctx context.Context, key, session string, check FreshSession) error {
	if s.providers[key] == nil {
		return ErrUnavailable
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = check(ctx, tx, session); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM social_oauth_flows WHERE provider=?", key); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM social_connections WHERE provider=?", key); err != nil {
		return err
	}
	return tx.Commit()
}

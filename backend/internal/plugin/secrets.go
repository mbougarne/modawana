package plugin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
)

var ErrSecretKey = errors.New("plugin encryption key unavailable")

// SetKeySource installs the operator key loader before serving requests.
func (r *Runtime) SetKeySource(read func() ([]byte, error)) { r.keySource = read }

func (r *Runtime) secretBox(id, name string, value []byte, encrypt bool) ([]byte, error) {
	if r.keySource == nil {
		return nil, ErrSecretKey
	}
	key, err := r.keySource()
	if err != nil || len(key) != 32 {
		return nil, ErrSecretKey
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("modawana/plugin-secrets/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, ErrSecretKey
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrSecretKey
	}
	aad := []byte(id + ":" + name)
	if encrypt {
		return aead.Seal(nil, nil, value, aad), nil
	}
	cleartext, err := aead.Open(nil, nil, value, aad)
	if err != nil {
		return nil, ErrSecretKey
	}
	return cleartext, nil
}

func (r *Runtime) readSecret(ctx context.Context, inv *invocation, name string) ([]byte, error) {
	if !inv.secretAccess || !inv.manifest.hasSecret(name) {
		return nil, ErrNotFound
	}
	var ciphertext []byte
	err := r.storage.db.QueryRowContext(ctx, "SELECT ciphertext FROM plugin_secrets WHERE plugin_id=? AND name=?", inv.pluginID, name).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.secretBox(inv.pluginID, name, ciphertext, false)
}

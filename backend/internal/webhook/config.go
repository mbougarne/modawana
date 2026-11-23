// Package webhook verifies provider messages and durably accepts a normalized
// event before acknowledging its delivery. Provider adapters are host-owned.
package webhook

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
)

const standardV1 = "standard-v1"
const MaxBody = 64 << 10

var (
	ErrUnknown     = errors.New("webhook connection unavailable")
	ErrSignature   = errors.New("webhook signature rejected")
	ErrScope       = errors.New("webhook scope rejected")
	ErrInvalid     = errors.New("invalid webhook event")
	ErrUnavailable = errors.New("webhook verification unavailable")
	ErrConflict    = errors.New("webhook event conflicts with a saved receipt")
)

var connectionID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Connection struct {
	ID      string `json:"id"`
	Adapter string `json:"adapter"`
	Account string `json:"account"`
	Mode    string `json:"mode"`
	KeyFile string `json:"key_file"`
}

type Inbox struct {
	db          *sql.DB
	root        string
	connections map[string]Connection
}

// Open binds configured connection IDs to a provider adapter, account and
// mode for the lifetime of this database. Credential rotation changes only
// the key file. Reassignment uses a new ID.
func Open(ctx context.Context, d *sql.DB, root, configuration string) (*Inbox, error) {
	var connections []Connection
	if len(configuration) > 16<<10 || (configuration != "" &&
		(decodeStrict([]byte(configuration), &connections) != nil || connections == nil)) || len(connections) > 16 {
		return nil, ErrInvalid
	}
	inbox := &Inbox{db: d, root: root, connections: make(map[string]Connection, len(connections))}
	for _, c := range connections {
		if !connectionID.MatchString(c.ID) || !identifier.MatchString(c.Account) ||
			(c.Mode != "sandbox" && c.Mode != "live") || c.Adapter != standardV1 ||
			!filepath.IsAbs(c.KeyFile) || inbox.connections[c.ID].ID != "" {
			return nil, ErrInvalid
		}
		inbox.connections[c.ID] = c
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE webhook_connections SET enabled=0 WHERE enabled=1"); err != nil {
		return nil, err
	}
	for _, c := range connections {
		result, err := tx.ExecContext(ctx, `INSERT INTO webhook_connections (id,adapter,account,mode,enabled)
			VALUES(?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET enabled=1
			WHERE adapter=excluded.adapter AND account=excluded.account AND mode=excluded.mode`,
			c.ID, c.Adapter, c.Account, c.Mode)
		if err != nil {
			return nil, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, ErrScope
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return inbox, nil
}

func (i *Inbox) HasConnection(adapter, id string) bool {
	c, ok := i.connections[id]
	return ok && c.Adapter == adapter
}

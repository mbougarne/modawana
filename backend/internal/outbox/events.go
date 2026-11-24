// Package outbox persists events with the domain changes that produce them and
// delivers them after commit, using a lease per attempt and a stable job ID.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

const ContentPublished = "content.published"
const ContentPublishedVersion = 1

type Event struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       int             `json:"version"`
	SourceID      string          `json:"source_id"`
	SourceVersion int64           `json:"source_version"`
	Data          json.RawMessage `json:"data"`
}

type Publication struct {
	ContentID  string `json:"content_id"`
	Locale     string `json:"locale"`
	RevisionID string `json:"revision_id"`
}

// RecordPublication uses the publication's transaction to freeze the exact
// revision and subscriber set. It only accesses SQLite.
func RecordPublication(ctx context.Context, tx *sql.Tx, sourceID string, sourceVersion int64, data Publication, at time.Time) error {
	id, err := db.NewID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	now := at.UnixMilli()
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events
		(id,name,contract_version,source_id,source_version,payload,created_at,staff_actor_id)
		VALUES(?,?,?,?,?,?,?,nullif(?,''))`, id, ContentPublished, ContentPublishedVersion, sourceID, sourceVersion, string(payload), now, access.StaffActor(ctx)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_deliveries
		(id,event_id,plugin_id,plugin_slug,available_at,updated_at)
		SELECT lower(hex(randomblob(16))),?,p.id,p.slug,?,?
		FROM outbox_subscriptions s JOIN plugin_installs p ON p.id=s.plugin_id
		WHERE s.name=? AND s.contract_version=? AND p.enabled=1`,
		id, now, now, ContentPublished, ContentPublishedVersion)
	return err
}

const LeadCaptured = "lead.captured"
const SocialScheduled = "social.scheduled"

// RecordLead queues one private inbox notification without copying lead fields into the event.
func RecordLead(ctx context.Context, tx *sql.Tx, lead string, at time.Time) error {
	job, err := db.NewID()
	if err != nil {
		return err
	}
	return recordBuiltin(ctx, tx, job, lead, "forms", LeadCaptured, at, at)
}

// RecordSocial schedules a local delivery. The job ID is also the provider's idempotency key.
func RecordSocial(ctx context.Context, tx *sql.Tx, job string, created, due time.Time) error {
	return recordBuiltin(ctx, tx, job, job, "social", SocialScheduled, created, due)
}

func recordBuiltin(ctx context.Context, tx *sql.Tx, job, source, slug, name string, created, due time.Time) error {
	event, err := db.NewID()
	if err != nil {
		return err
	}
	now := created.UnixMilli()
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events
		(id,name,contract_version,source_id,source_version,payload,created_at)
		VALUES(?,?,1,?,1,'{}',?)`, event, name, source, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_deliveries
		(id,event_id,plugin_id,plugin_slug,available_at,updated_at)
		VALUES(?,?,'',?,?,?)`, job, event, slug, due.UnixMilli(), now)
	return err
}

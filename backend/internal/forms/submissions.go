package forms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/outbox"
)

type Submission struct {
	Reference
	Locale   string
	Page     string
	Revision string
	Block    string
	Values   map[string]string
	Consent  bool
}

type Binding func(context.Context, *sql.Tx, Submission) error

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) Allow(ctx context.Context, source string) error {
	window := time.Now().Unix() / 900
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM lead_rate WHERE window<?", window); err != nil {
		return err
	}
	var attempts int
	if err := tx.QueryRowContext(ctx, `INSERT INTO lead_rate(bucket,window,attempts) VALUES(?,?,1)
		ON CONFLICT(bucket,window) DO UPDATE SET attempts=min(attempts+1,21)
		RETURNING attempts`, digest(source), window).Scan(&attempts); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if attempts > 20 {
		return ErrLimited
	}
	return nil
}

func (s *Service) Submit(ctx context.Context, retryKey string, in Submission, check Binding) error {
	if !media.ID.MatchString(retryKey) || !media.ID.MatchString(in.Revision) || !in.Reference.Valid() || check == nil {
		return ErrInvalid
	}
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(retryKey))
	_, _ = mac.Write(data)
	fingerprint := hex.EncodeToString(mac.Sum(nil))
	key := digest(retryKey)
	now := time.Now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT fingerprint FROM lead_idempotency WHERE key_hash=? AND expires_at>?", key, now.Unix()).Scan(&previous)
	if err == nil {
		if hmac.Equal([]byte(previous), []byte(fingerprint)) {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := check(ctx, tx, in); err != nil {
		return err
	}
	form, err := Visible(ctx, tx, in.Reference)
	if err != nil {
		return err
	}
	if !form.validateSubmission(in.Values, in.Consent) {
		return ErrInvalid
	}
	values, err := json.Marshal(in.Values)
	if err != nil {
		return err
	}
	id, err := db.NewID()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO leads(id,form_key,form_version,locale,page_key,payload,consent,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, id, in.Key, in.Version, in.Locale, in.Page, string(values), form.Consent, now.Unix(),
		now.AddDate(0, 0, form.RetentionDays).Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO lead_idempotency(key_hash,fingerprint,expires_at) VALUES(?,?,?)
		ON CONFLICT(key_hash) DO UPDATE SET fingerprint=excluded.fingerprint,expires_at=excluded.expires_at`,
		key, fingerprint, now.AddDate(0, 0, 7).Unix()); err != nil {
		return err
	}
	if err := outbox.RecordLead(ctx, tx, id, now); err != nil {
		return err
	}
	return tx.Commit()
}

func PrepareNotification(ctx context.Context, job outbox.Delivery) (outbox.Effect, error) {
	if job.Event.Name != outbox.LeadCaptured || job.Event.Version != 1 || !media.ID.MatchString(job.Event.SourceID) {
		return nil, outbox.ErrRejected
	}
	return func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE leads SET notified_at=? WHERE id=? AND notified_at=0 AND expires_at>?",
			time.Now().Unix(), job.Event.SourceID, time.Now().Unix())
		return err
	}, nil
}

func (s *Service) Prune(ctx context.Context) error {
	now := time.Now().Unix()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM leads WHERE id IN (SELECT id FROM leads WHERE expires_at<=? LIMIT 500)", now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM lead_idempotency WHERE key_hash IN (SELECT key_hash FROM lead_idempotency WHERE expires_at<=? LIMIT 500)", now); err != nil {
		return err
	}
	return tx.Commit()
}

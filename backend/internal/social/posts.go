package social

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/outbox"
)

type Draft struct {
	Connection string `json:"connection_id"`
	Key        string `json:"key"`
	Locale     string `json:"locale"`
	Revision   string `json:"revision_id"`
	Text       string `json:"text"`
}

type Preview struct {
	Body               string `json:"body"`
	Path               string `json:"path"`
	Connection         string `json:"connection_id"`
	Destination        string `json:"destination"`
	Image              string `json:"image,omitempty"`
	provider, instance string
}

type Schedule struct {
	Draft
	ID    string `json:"id"`
	Path  string `json:"path"`
	Image string `json:"image,omitempty"`
	At    string `json:"scheduled_at,omitempty"`
}

func (s *Service) Preview(ctx context.Context, in Draft) (Preview, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Preview{}, err
	}
	defer tx.Rollback()
	return s.previewIn(ctx, tx, in)
}

func (s *Service) previewIn(ctx context.Context, tx *sql.Tx, in Draft) (Preview, error) {
	var out Preview
	if !media.ID.MatchString(in.Connection) || !media.ID.MatchString(in.Revision) || !locale.Valid(in.Locale) || len(in.Key) > 120 ||
		!utf8.ValidString(in.Text) || strings.TrimSpace(in.Text) == "" || strings.ContainsFunc(in.Text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) {
		return out, ErrInvalid
	}
	conn, err := readConnection(ctx, tx, "id", in.Connection)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrUnavailable
	}
	if err != nil {
		return out, err
	}
	provider := s.providers[conn.Provider]
	if provider == nil || provider.Instance() != conn.Instance {
		return out, ErrUnavailable
	}
	path, err := s.Content.PublicRevisionPath(ctx, tx, in.Key, in.Locale, in.Revision)
	if err != nil {
		return out, err
	}
	link := s.baseURL + path
	body := strings.TrimSpace(in.Text) + "\n\n" + link
	if !validPostLength(provider, strings.TrimSpace(in.Text), link, body) {
		return out, ErrInvalid
	}
	var image string
	if _, required := provider.(mediaPublisher); required {
		cover, err := s.Content.SocialCover(ctx, tx, in.Revision)
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrImage
		}
		if err != nil {
			return out, err
		}
		image = s.baseURL + cover
	}
	name := conn.Account
	if conn.Provider == "mastodon" {
		name += "@" + strings.TrimPrefix(conn.Instance, "https://")
	}
	return Preview{Body: body, Path: path, Connection: conn.ID, Destination: name, Image: image, provider: conn.Provider, instance: conn.Instance}, nil
}

func validPostLength(provider Provider, text, link, body string) bool {
	if p, ok := provider.(interface{ ValidLength(string, string) bool }); ok {
		return p.ValidLength(text, link)
	}
	return utf8.RuneCountInString(body) <= provider.MaxTextLength()
}

func (s *Service) Schedule(ctx context.Context, in Schedule) error {
	if !media.ID.MatchString(in.ID) {
		return ErrInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return ErrInvalid
	}
	fingerprint := digest(string(raw))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var saved string
	err = tx.QueryRowContext(ctx, "SELECT request_hash FROM social_posts WHERE id=?", in.ID).Scan(&saved)
	if err == nil {
		if saved == fingerprint {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now()
	due := now
	if in.At != "" {
		due, err = time.Parse(time.RFC3339, in.At)
		if err != nil || !due.After(now) || due.After(now.AddDate(0, 0, 90)) {
			return ErrInvalid
		}
	}
	preview, err := s.previewIn(ctx, tx, in.Draft)
	if err != nil {
		return err
	}
	if preview.Path != in.Path || preview.Image != in.Image {
		return ErrConflict
	}
	if err = outbox.RecordSocial(ctx, tx, in.ID, now, due); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO social_posts(id,connection_id,instance,account,content_key,locale,revision_id,path,body,request_hash,created_at,scheduled_at,provider,image)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.ID, in.Connection, preview.instance, preview.Destination, in.Key, in.Locale, in.Revision,
		preview.Path, preview.Body, fingerprint, now.UnixMilli(), due.UnixMilli(), preview.provider, preview.Image)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type Post struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	Destination string    `json:"destination"`
	Body        string    `json:"body"`
	Locale      string    `json:"locale"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	Scheduled   time.Time `json:"scheduled_at"`
	Updated     time.Time `json:"updated_at"`
	Attempts    int       `json:"attempts"`
	RemoteID    string    `json:"remote_id,omitempty"`
	RemoteURL   string    `json:"remote_url,omitempty"`
	CanCancel   bool      `json:"can_cancel"`
}

type History struct {
	Items []Post `json:"items"`
	Next  string `json:"next,omitempty"`
}

func (s *Service) History(ctx context.Context, before string) (History, error) {
	out := History{Items: []Post{}}
	if before != "" && !media.ID.MatchString(before) {
		return out, ErrInvalid
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,p.provider,p.account,p.body,p.locale,p.outcome,p.reason,p.scheduled_at,d.updated_at,d.attempts,
		p.remote_id,p.remote_url,p.first_dispatch_at,d.state,d.last_error
		FROM social_posts p JOIN outbox_deliveries d ON d.id=p.id
		WHERE (?='' OR (p.created_at,p.id)<(SELECT created_at,id FROM social_posts WHERE id=?))
		ORDER BY p.created_at DESC,p.id DESC LIMIT 26`, before, before)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == 25 {
			out.Next = out.Items[24].ID
			break
		}
		var post Post
		var due, updated, dispatched int64
		var delivery, deliveryError string
		if err = rows.Scan(&post.ID, &post.Provider, &post.Destination, &post.Body, &post.Locale, &post.Status, &post.Reason, &due, &updated, &post.Attempts,
			&post.RemoteID, &post.RemoteURL, &dispatched, &delivery, &deliveryError); err != nil {
			return out, err
		}
		post.Scheduled, post.Updated = time.UnixMilli(due).UTC(), time.UnixMilli(updated).UTC()
		if post.Status == "" {
			post.Status = "scheduled"
			post.Reason = deliveryError
			switch delivery {
			case "running":
				post.Status = "sending"
			case "failed":
				post.Status = "failed"
				if dispatched > 0 {
					post.Status = "uncertain"
				}
			case "pending":
				if post.Attempts > 0 {
					post.Status = "retrying"
				}
			}
			post.CanCancel = dispatched == 0 && (delivery == "pending" || delivery == "running")
		}
		out.Items = append(out.Items, post)
	}
	return out, rows.Err()
}

func (s *Service) Cancel(ctx context.Context, id string) error {
	if !media.ID.MatchString(id) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE social_posts SET outcome='cancelled' WHERE id=? AND outcome='' AND first_dispatch_at=0 AND
		EXISTS(SELECT 1 FROM outbox_deliveries WHERE id=? AND state IN ('pending','running'))`, id, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE outbox_deliveries SET state='failed',last_error='cancelled',updated_at=? WHERE id=? AND state='pending'`,
		time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

package forms

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/media"
)

type Lead struct {
	ID       string            `json:"id"`
	Form     string            `json:"form_key"`
	Version  int64             `json:"form_version"`
	Locale   string            `json:"locale"`
	Page     string            `json:"page_key"`
	Values   map[string]string `json:"values"`
	Consent  string            `json:"consent"`
	Status   string            `json:"status"`
	Tags     []string          `json:"tags"`
	Created  int64             `json:"created_at"`
	Notified int64             `json:"notified_at"`
}

type Inbox struct {
	Items []Lead `json:"items"`
	Next  string `json:"next,omitempty"`
}

func scanLead(rows *sql.Rows) (Lead, error) {
	var lead Lead
	var values, tags []byte
	err := rows.Scan(&lead.ID, &lead.Form, &lead.Version, &lead.Locale, &lead.Page, &values, &lead.Consent, &lead.Status, &tags, &lead.Created, &lead.Notified)
	if err != nil {
		return lead, err
	}
	if err := json.Unmarshal(values, &lead.Values); err != nil {
		return lead, err
	}
	return lead, json.Unmarshal(tags, &lead.Tags)
}

const selectLead = "SELECT id,form_key,form_version,locale,page_key,payload,consent,status,tags,created_at,notified_at FROM leads WHERE expires_at>?"

func (s *Service) Inbox(ctx context.Context, cursor string) (Inbox, error) {
	out := Inbox{Items: []Lead{}}
	query := selectLead
	args := []any{time.Now().Unix()}
	if cursor != "" {
		stamp, id, ok := strings.Cut(cursor, ":")
		before, err := strconv.ParseInt(stamp, 10, 64)
		if !ok || err != nil || before < 1 || !media.ID.MatchString(id) {
			return out, ErrInvalid
		}
		query += " AND (created_at,id)<(?,?)"
		args = append(args, before, id)
	}
	rows, err := s.DB.QueryContext(ctx, query+" ORDER BY created_at DESC,id DESC LIMIT 26", args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == 25 {
			last := out.Items[24]
			out.Next = strconv.FormatInt(last.Created, 10) + ":" + last.ID
			break
		}
		lead, err := scanLead(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, lead)
	}
	return out, rows.Err()
}

func (s *Service) Update(ctx context.Context, id, status string, tags []string) error {
	switch status {
	case "new", "contacted", "closed":
	default:
		return ErrInvalid
	}
	if !media.ID.MatchString(id) || len(tags) > 10 {
		return ErrInvalid
	}
	if tags == nil {
		tags = []string{}
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		if !validText(tag, 1, 32, false) || seen[tag] {
			return ErrInvalid
		}
		seen[tag] = true
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, "UPDATE leads SET status=?,tags=? WHERE id=? AND expires_at>?", status, string(raw), id, time.Now().Unix())
	return affected(result, err)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if !media.ID.MatchString(id) {
		return ErrInvalid
	}
	result, err := s.DB.ExecContext(ctx, "DELETE FROM leads WHERE id=?", id)
	return affected(result, err)
}

func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) Export(ctx context.Context, out io.Writer) error {
	rows, err := s.DB.QueryContext(ctx, selectLead+" ORDER BY created_at DESC,id DESC", time.Now().Unix())
	if err != nil {
		return err
	}
	defer rows.Close()
	w := csv.NewWriter(out)
	if err := w.Write([]string{"id", "form_key", "form_version", "locale", "page_key", "status", "created_at", "tags", "consent", "values"}); err != nil {
		return err
	}
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return err
		}
		values, err := json.Marshal(lead.Values)
		if err != nil {
			return err
		}
		tags, err := json.Marshal(lead.Tags)
		if err != nil {
			return err
		}
		if err := w.Write([]string{lead.ID, lead.Form, strconv.FormatInt(lead.Version, 10), lead.Locale, lead.Page, lead.Status,
			dbTime(lead.Created), string(tags), csvLiteral(lead.Consent), string(values)}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return rows.Err()
}

func dbTime(seconds int64) string { return time.Unix(seconds, 0).UTC().Format(time.RFC3339) }

func csvLiteral(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "=") || strings.HasPrefix(strings.TrimSpace(value), "+") ||
		strings.HasPrefix(strings.TrimSpace(value), "-") || strings.HasPrefix(strings.TrimSpace(value), "@") {
		return "'" + value
	}
	return value
}

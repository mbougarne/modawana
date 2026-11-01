package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"

	"github.com/mbougarne/modawana/backend/internal/access"
)

type CollectionLocale struct {
	Locale  string `json:"locale"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Visible bool   `json:"visible"`
}
type CollectionDocument struct {
	MemberTitles    map[string]string  `json:"member_titles"`
	AssignedMembers []PlanMember       `json:"assigned_members"`
	DraftGatePolicy int                `json:"draft_gate_policy"`
	ID              string             `json:"id"`
	Key             string             `json:"key"`
	Kind            string             `json:"kind"`
	Version         int64              `json:"version"`
	DraftPlanID     string             `json:"draft_plan_id"`
	ActivePlanID    string             `json:"active_plan_id"`
	Locales         []CollectionLocale `json:"locales"`
	DraftMembers    []PlanMember       `json:"draft_members"`
	ActiveMembers   []PlanMember       `json:"active_members"`
}

func readMembers(ctx context.Context, q query, id string) ([]PlanMember, error) {
	out := []PlanMember{}
	rows, e := q.QueryContext(ctx, "SELECT i.key,m.announcements_json FROM collection_plan_members m JOIN content_items i ON i.id=m.item_id WHERE m.plan_id=? ORDER BY m.position", id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var p PlanMember
		var raw string
		if e = rows.Scan(&p.Key, &raw); e != nil {
			return out, e
		}
		if e = json.Unmarshal([]byte(raw), &p.Announcements); e != nil {
			return out, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) Collection(ctx context.Context, key string) (CollectionDocument, error) {
	out := CollectionDocument{Locales: []CollectionLocale{}, AssignedMembers: []PlanMember{}, MemberTitles: map[string]string{}}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.key,c.kind,c.version,coalesce(st.draft_plan_id,''),coalesce(st.active_plan_id,''),coalesce((SELECT gate_policy_version FROM collection_plans WHERE id=st.draft_plan_id),0) FROM collections c JOIN collection_plan_state st ON st.collection_id=c.id WHERE c.key=? AND c.removed=0`, key).Scan(&out.ID, &out.Key, &out.Kind, &out.Version, &out.DraftPlanID, &out.ActivePlanID, &out.DraftGatePolicy)
	if e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT locale,slug,title,summary,visible FROM collection_locales WHERE collection_id=? ORDER BY locale", out.ID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var l CollectionLocale
		if e = rows.Scan(&l.Locale, &l.Slug, &l.Title, &l.Summary, &l.Visible); e != nil {
			rows.Close()
			return out, e
		}
		out.Locales = append(out.Locales, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = tx.QueryContext(ctx, "SELECT i.key,coalesce(r.title,i.key) FROM collection_items m JOIN content_items i ON i.id=m.item_id LEFT JOIN content_versions v ON v.item_id=i.id AND v.locale=i.source_locale LEFT JOIN revisions r ON r.id=v.draft_revision_id WHERE m.collection_id=? ORDER BY m.position", out.ID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var key, title string
		if e = rows.Scan(&key, &title); e != nil {
			rows.Close()
			return out, e
		}
		out.MemberTitles[key] = title
		out.AssignedMembers = append(out.AssignedMembers, PlanMember{Key: key, Announcements: map[string]string{}})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.DraftMembers, e = readMembers(ctx, tx, out.DraftPlanID)
	if e != nil {
		return out, e
	}
	out.ActiveMembers, e = readMembers(ctx, tx, out.ActivePlanID)
	return out, e
}

type CollectionEntry struct {
	Completed bool   `json:"completed"`
	Key       string `json:"key,omitempty"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	Position  int    `json:"position"`
	State     string `json:"state"`
	URL       string `json:"url,omitempty"`
	Slug      string `json:"slug,omitempty"`
}
type PublicCollection struct {
	URL            string            `json:"url"`
	Key            string            `json:"key"`
	Kind           string            `json:"kind"`
	Locale         string            `json:"locale"`
	Slug           string            `json:"slug"`
	Title          string            `json:"title"`
	Summary        string            `json:"summary"`
	PublishedCount int               `json:"published_count"`
	AnnouncedCount int               `json:"announced_count"`
	Total          int               `json:"total"`
	Items          []CollectionEntry `json:"items"`
}

func (s *Service) PublicCollection(ctx context.Context, locale, slug string, p access.Principal) (PublicCollection, error) {
	out := PublicCollection{Locale: locale, Slug: slug, Items: []CollectionEntry{}}
	if !localeOK(locale) || !slugPattern.MatchString(slug) {
		return out, sql.ErrNoRows
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var plan string
	e = tx.QueryRowContext(ctx, `SELECT c.key,c.kind,l.title,l.summary,st.active_plan_id FROM collection_locales l JOIN collections c ON c.id=l.collection_id JOIN collection_plan_state st ON st.collection_id=c.id WHERE l.locale=? AND l.slug=? AND l.visible=1 AND st.active_plan_id IS NOT NULL`, locale, slug).Scan(&out.Key, &out.Kind, &out.Title, &out.Summary, &plan)
	if e != nil {
		return out, e
	}
	type entry struct{ key, version, title, summary, slug, announcements string }
	all := []entry{}
	rows, e := tx.QueryContext(ctx, `SELECT i.key,coalesce(v.id,''),coalesce(r.title,''),coalesce(r.summary,''),coalesce(v.slug,''),m.announcements_json FROM collection_plan_members m JOIN content_items i ON i.id=m.item_id LEFT JOIN content_versions v ON v.item_id=i.id AND v.locale=? AND v.published_revision_id IS NOT NULL LEFT JOIN revisions r ON r.id=v.published_revision_id WHERE m.plan_id=? ORDER BY m.position`, locale, plan)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v entry
		if e = rows.Scan(&v.key, &v.version, &v.title, &v.summary, &v.slug, &v.announcements); e != nil {
			rows.Close()
			return out, e
		}
		all = append(all, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, v := range all {
		if v.version != "" {
			d, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Collection, ResourceID: v.version, Locale: locale})
			if e != nil {
				return out, e
			}
			if d.Visibility == access.Hidden {
				continue
			}
			state := "open"
			if !d.AllowsBody() {
				state = "locked"
			}
			out.Items = append(out.Items, CollectionEntry{Key: v.key, Title: v.title, Summary: v.summary, Slug: v.slug, State: state, Position: len(out.Items) + 1, URL: collectionPath(locale, out.Kind, slug) + "/" + url.PathEscape(v.slug)})
			if p.Audience == access.Learner {
				var n int
				e = tx.QueryRowContext(ctx, `SELECT count(*) FROM learning_completion c JOIN content_items i ON i.id=c.item_id JOIN collection_plan_state st ON st.collection_id=c.collection_id JOIN learning_identities identity ON identity.id=c.identity_id WHERE i.key=? AND c.identity_id=? AND c.epoch=st.epoch AND identity.expires_at>strftime('%Y-%m-%dT%H:%M:%fZ','now')`, v.key, p.ID).Scan(&n)
				if e != nil {
					return out, e
				}
				out.Items[len(out.Items)-1].Completed = n == 1
			}
			out.PublishedCount++
		} else {
			var announced map[string]string
			if e = json.Unmarshal([]byte(v.announcements), &announced); e != nil {
				return out, e
			}
			if title := announced[locale]; title != "" {
				out.Items = append(out.Items, CollectionEntry{Title: title, State: "soon", Position: len(out.Items) + 1})
				out.AnnouncedCount++
			}
		}
	}
	out.URL = collectionPath(locale, out.Kind, slug)
	out.Total = out.PublishedCount + out.AnnouncedCount
	if out.Total == 0 {
		return out, sql.ErrNoRows
	}
	return out, nil
}

type CollectionSummary struct {
	Title   string `json:"title"`
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Version int64  `json:"version"`
}

func (s *Service) Collections(ctx context.Context, cursor string, locales ...string) ([]CollectionSummary, string, error) {
	if cursor != "" && !keyPattern.MatchString(cursor) {
		return nil, "", ErrInvalid
	}
	locale := "en"
	if len(locales) > 0 {
		locale = locales[0]
	}
	if !localeOK(locale) {
		return nil, "", ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT c.key,c.kind,c.version,coalesce(l.title,en.title,(SELECT title FROM collection_locales WHERE collection_id=c.id ORDER BY locale LIMIT 1),c.key) FROM collections c LEFT JOIN collection_locales l ON l.collection_id=c.id AND l.locale=? LEFT JOIN collection_locales en ON en.collection_id=c.id AND en.locale='en' WHERE c.removed=0 AND c.key>? ORDER BY c.key LIMIT 51`, locale, cursor)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := []CollectionSummary{}
	for rows.Next() {
		var c CollectionSummary
		if e = rows.Scan(&c.Key, &c.Kind, &c.Version, &c.Title); e != nil {
			return nil, "", e
		}
		out = append(out, c)
	}
	next := ""
	if len(out) > 50 {
		out = out[:50]
		next = out[49].Key
	}
	return out, next, rows.Err()
}

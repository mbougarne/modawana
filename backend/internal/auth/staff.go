package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

const staffCookie = "__Host-staff"
const staffFlowCookie = "__Host-staff-flow"
const staffInviteDuration = 24 * time.Hour
const staffSessionDuration = 12 * time.Hour

type StaffPermission string

const (
	StaffReadContent  StaffPermission = "content:read"
	StaffWriteDraft   StaffPermission = "content:draft"
	StaffPublish      StaffPermission = "content:publish"
	StaffManageOrders StaffPermission = "store:orders"
)

func staffRoleValid(role string) bool {
	return role == "owner" || role == "editor" || role == "store_manager"
}

func staffAllows(role string, permission StaffPermission) bool {
	switch permission {
	case StaffReadContent, StaffWriteDraft:
		return role == "owner" || role == "editor"
	case StaffPublish:
		return role == "owner"
	case StaffManageOrders:
		return role == "owner" || role == "store_manager"
	}
	return false
}

type StaffMember struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Revoked string `json:"revoked_at,omitempty"`
	owner   string
}

type StaffInvite struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Expires  string `json:"expires_at"`
	Consumed string `json:"consumed_at,omitempty"`
	Revoked  string `json:"revoked_at,omitempty"`
	owner    string
}

func (s *Service) staffDirectory(ctx context.Context) (map[string]any, error) {
	members := []StaffMember{}
	invites := []StaffInvite{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,role,coalesce(revoked_at,'')
		FROM staff_members ORDER BY revoked_at IS NOT NULL,name,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m StaffMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Role, &m.Revoked); err != nil {
			rows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT id,name,role,expires_at,coalesce(consumed_at,''),coalesce(revoked_at,'')
		FROM staff_invites ORDER BY expires_at DESC,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row StaffInvite
		if err := rows.Scan(&row.ID, &row.Name, &row.Role, &row.Expires, &row.Consumed, &row.Revoked); err != nil {
			return nil, err
		}
		invites = append(invites, row)
	}
	return map[string]any{"members": members, "invitations": invites}, rows.Err()
}

func (s *Service) createStaffInvite(r *http.Request, name, role string) (StaffInvite, string, error) {
	var out StaffInvite
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) || !staffRoleValid(role) {
		return out, "", actionField("staff_invite", "invalid")
	}
	check, err := s.FreshOwnerCheck(r)
	if err != nil {
		return out, "", err
	}
	id, err := db.NewID()
	if err != nil {
		return out, "", err
	}
	raw, err := secret()
	if err != nil {
		return out, "", err
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, "", err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return out, "", err
	}
	out = StaffInvite{ID: id, Name: name, Role: role, Expires: db.UTC(s.Now().Add(staffInviteDuration))}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM staff_members WHERE revoked_at IS NULL) +
		(SELECT count(*) FROM staff_invites WHERE revoked_at IS NULL AND consumed_at IS NULL AND expires_at>?)`, s.now()).Scan(&count); err != nil {
		return out, "", err
	}
	if count >= 100 {
		return out, "", ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO staff_invites(id,token_hash,owner_id,name,role,expires_at)
		SELECT ?,?,id,?,?,? FROM owner WHERE singleton=1`, id, digest(raw), name, role, out.Expires); err != nil {
		return out, "", err
	}
	if err = s.audit(ctx, tx, "staff_invited", r.Header.Get("X-Request-ID")); err != nil {
		return out, "", err
	}
	return out, raw, tx.Commit()
}

func (s *Service) revokeStaff(r *http.Request, id string, invite bool) error {
	check, err := s.FreshOwnerCheck(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = check(tx); err != nil {
		return err
	}
	query := `UPDATE staff_members SET revoked_at=? WHERE id=? AND revoked_at IS NULL`
	event := "staff_revoked"
	if invite {
		query = `UPDATE staff_invites SET revoked_at=? WHERE id=? AND consumed_at IS NULL AND revoked_at IS NULL`
		event = "staff_invite_revoked"
	}
	result, err := tx.ExecContext(ctx, query, s.now(), id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	if !invite {
		if _, err := tx.ExecContext(ctx, "DELETE FROM staff_sessions WHERE member_id=?", id); err != nil {
			return err
		}
	}
	if err = s.audit(ctx, tx, event, r.Header.Get("X-Request-ID")); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) staffSession(ctx context.Context, q querier, raw string) (StaffMember, error) {
	var m StaffMember
	if !validSecret(raw) {
		return m, ErrAuth
	}
	err := q.QueryRowContext(ctx, `SELECT m.id,m.name,m.role,m.owner_id FROM staff_sessions s
		JOIN staff_members m ON m.id=s.member_id WHERE s.token_hash=? AND s.expires_at>? AND m.revoked_at IS NULL`,
		digest(raw), s.now()).Scan(&m.ID, &m.Name, &m.Role, &m.owner)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrAuth
	}
	return m, err
}

// StaffRequest checks an exact operation grant and attaches a transaction-time
// recheck for domain writes. An owner session never aliases a staff session.
func (s *Service) StaffRequest(r *http.Request, permission StaffPermission) (*http.Request, string, error) {
	raw := cookie(r, staffCookie)
	m, err := s.staffSession(r.Context(), s.DB, raw)
	if err != nil {
		return r, "", err
	}
	if !staffAllows(m.Role, permission) ||
		(r.Method != http.MethodGet && r.Method != http.MethodHead && !s.checkWrite(r, raw)) {
		return r, "", ErrForbidden
	}
	ctx := access.WithMutation(r.Context(), access.Mutation{
		Actor: "staff:" + m.ID, StaffID: m.ID,
		Check: func(tx *sql.Tx, _ string) error {
			current, err := s.staffSession(r.Context(), tx, raw)
			if err != nil {
				return err
			}
			if current.ID != m.ID || !staffAllows(current.Role, permission) {
				return ErrForbidden
			}
			return nil
		},
	})
	return r.WithContext(ctx), m.owner, nil
}

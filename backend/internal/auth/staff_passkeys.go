package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/mbougarne/modawana/backend/internal/db"
)

type staffUser struct {
	id, name    string
	credentials []webauthn.Credential
}

func (u staffUser) WebAuthnID() []byte                         { return []byte(u.id) }
func (u staffUser) WebAuthnName() string                       { return "staff-" + u.id }
func (u staffUser) WebAuthnDisplayName() string                { return u.name }
func (u staffUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (s *Service) inviteByToken(ctx context.Context, raw string) (StaffInvite, error) {
	var in StaffInvite
	if !validSecret(raw) {
		return in, ErrAuth
	}
	err := s.DB.QueryRowContext(ctx, `SELECT id,owner_id,name,role FROM staff_invites
		WHERE token_hash=? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>?`, digest(raw), s.now()).
		Scan(&in.ID, &in.owner, &in.Name, &in.Role)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrAuth
	}
	return in, err
}

func (s *Service) beginStaff(ctx context.Context, mode, invite, ip string) (string, any, error) {
	if err := s.limit(ctx, "staff-start:"+ip, 30); err != nil {
		return "", nil, err
	}
	wa, err := s.webauthn()
	if err != nil {
		return "", nil, err
	}
	var options any
	var data *webauthn.SessionData
	var invitationID any
	if mode == "enroll" {
		in, err := s.inviteByToken(ctx, invite)
		if err != nil {
			return "", nil, err
		}
		invitationID = in.ID
		options, data, err = wa.BeginRegistration(staffUser{id: in.ID, name: in.Name}, webauthn.WithRegistrationOrigin(s.Config.BaseURL))
		if err != nil {
			return "", nil, ErrAuth
		}
	} else {
		options, data, err = wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired), webauthn.WithLoginOrigin(s.Config.BaseURL))
		if err != nil {
			return "", nil, ErrAuth
		}
	}
	raw, err := secret()
	if err != nil {
		return "", nil, err
	}
	challenge, err := json.Marshal(data)
	if err != nil {
		return "", nil, err
	}
	if _, err = s.DB.ExecContext(ctx, "DELETE FROM staff_flows WHERE expires_at<=?", s.now()); err != nil {
		return "", nil, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO staff_flows VALUES(?,?,?,?,?)`,
		digest(raw), invitationID, mode, string(challenge), db.UTC(s.Now().Add(5*time.Minute)))
	return raw, options, err
}

func (s *Service) claimStaffFlow(ctx context.Context, raw, mode string) (string, webauthn.SessionData, error) {
	var session webauthn.SessionData
	if !validSecret(raw) {
		return "", session, ErrAuth
	}
	var invite, challenge string
	err := s.DB.QueryRowContext(ctx, `DELETE FROM staff_flows WHERE token_hash=? AND ceremony=? AND expires_at>?
		RETURNING coalesce(invite_id,''),challenge`, digest(raw), mode, s.now()).Scan(&invite, &challenge)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrAuth
	}
	if err == nil {
		err = json.Unmarshal([]byte(challenge), &session)
	}
	return invite, session, err
}

func (s *Service) finishStaff(r *http.Request, mode string) (string, error) {
	ctx := r.Context()
	invite, session, err := s.claimStaffFlow(ctx, cookie(r, staffFlowCookie), mode)
	if err != nil {
		return "", err
	}
	wa, err := s.webauthn()
	if err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	member := invite
	event := "staff_enrolled"
	if mode == "enroll" {
		var in StaffInvite
		err = tx.QueryRowContext(ctx, `SELECT id,owner_id,name,role FROM staff_invites
			WHERE id=? AND expires_at>? AND revoked_at IS NULL AND consumed_at IS NULL`, invite, s.now()).
			Scan(&in.ID, &in.owner, &in.Name, &in.Role)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrAuth
		}
		if err != nil {
			return "", err
		}
		credential, err := wa.FinishRegistration(staffUser{id: in.ID, name: in.Name}, session, r)
		if err != nil || !credential.Flags.UserVerified {
			return "", ErrAuth
		}
		encoded, err := json.Marshal(credential)
		if err != nil {
			return "", err
		}
		id := base64.RawURLEncoding.EncodeToString(credential.ID)
		var alreadyOwner bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM passkeys WHERE id=?)", id).Scan(&alreadyOwner); err != nil {
			return "", err
		}
		if alreadyOwner {
			return "", ErrAuth
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO staff_members(id,owner_id,name,role,credential_id,credential)
			VALUES(?,?,?,?,?,?)`, in.ID, in.owner, in.Name, in.Role, id, string(encoded)); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE staff_invites SET consumed_at=? WHERE id=?", s.now(), invite); err != nil {
			return "", err
		}
	} else {
		event = "staff_signed_in"
		credential, err := wa.FinishDiscoverableLogin(func(id, handle []byte) (webauthn.User, error) {
			var u staffUser
			var raw string
			err := tx.QueryRowContext(ctx, `SELECT id,name,credential FROM staff_members
				WHERE credential_id=? AND id=? AND revoked_at IS NULL`,
				base64.RawURLEncoding.EncodeToString(id), string(handle)).Scan(&u.id, &u.name, &raw)
			if err != nil {
				return nil, ErrAuth
			}
			var c webauthn.Credential
			if json.Unmarshal([]byte(raw), &c) != nil {
				return nil, ErrAuth
			}
			u.credentials = []webauthn.Credential{c}
			member = u.id
			return u, nil
		}, session, r)
		if err != nil || credential.Authenticator.CloneWarning || !credential.Flags.UserVerified {
			return "", ErrAuth
		}
		encoded, err := json.Marshal(credential)
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE staff_members SET credential=? WHERE id=?", string(encoded), member); err != nil {
			return "", err
		}
	}
	raw, err := secret()
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM staff_sessions WHERE expires_at<=? OR token_hash=?", s.now(), digest(cookie(r, staffCookie))); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO staff_sessions VALUES(?,?,?)",
		digest(raw), member, db.UTC(s.Now().Add(staffSessionDuration))); err != nil {
		return "", err
	}
	if err = s.audit(ctx, tx, event, r.Header.Get("X-Request-ID")); err != nil {
		return "", err
	}
	return raw, tx.Commit()
}

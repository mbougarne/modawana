package auth

import (
	"net/http"
	"strings"
)

func (s *Service) staffRoutes(w http.ResponseWriter, r *http.Request) bool {
	const authPrefix = "/api/v1/auth/staff/"
	const manage = "/api/v1/staff/management"
	p := r.URL.Path
	if p == manage || strings.HasPrefix(p, manage+"/") {
		s.manageStaff(w, r, strings.TrimPrefix(p, manage))
		return true
	}
	if !strings.HasPrefix(p, authPrefix) {
		return false
	}
	suffix := strings.TrimPrefix(p, authPrefix)
	if suffix == "session" {
		raw := cookie(r, staffCookie)
		switch r.Method {
		case http.MethodGet:
			m, err := s.staffSession(r.Context(), s.DB, raw)
			if err != nil {
				fail(w, err)
				return true
			}
			jsonOut(w, 200, map[string]any{"member": m, "csrf": csrf(raw)})
		case http.MethodDelete:
			if !s.checkWrite(r, raw) {
				fail(w, ErrForbidden)
				return true
			}
			if _, err := s.DB.ExecContext(r.Context(), "DELETE FROM staff_sessions WHERE token_hash=?", digest(raw)); err != nil {
				fail(w, err)
				return true
			}
			setCookie(w, staffCookie, "", -1)
			jsonOut(w, 200, map[string]bool{"signed_out": true})
		default:
			w.WriteHeader(405)
		}
		return true
	}
	mode, step, ok := strings.Cut(suffix, "/")
	if !ok || (mode != "login" && mode != "enroll") || (step != "start" && step != "finish") {
		w.WriteHeader(404)
		return true
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	if step == "start" {
		if r.Header.Get("Origin") != s.Config.BaseURL || r.Header.Get("X-Blog-Request") != "1" {
			fail(w, ErrForbidden)
			return true
		}
		var in struct {
			Invite string `json:"invite,omitempty"`
		}
		if !decode(w, r, &in) {
			return true
		}
		ip := s.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
		flow, options, err := s.beginStaff(r.Context(), mode, in.Invite, ip)
		if err != nil {
			fail(w, err)
			return true
		}
		setCookie(w, staffFlowCookie, flow, 300)
		jsonOut(w, 200, map[string]any{"options": options, "csrf": csrf(flow)})
		return true
	}
	flow := cookie(r, staffFlowCookie)
	if !s.checkWrite(r, flow) {
		fail(w, ErrForbidden)
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	raw, err := s.finishStaff(r, mode)
	if err != nil {
		fail(w, err)
		return true
	}
	setCookie(w, staffFlowCookie, "", -1)
	setCookie(w, staffCookie, raw, int(staffSessionDuration.Seconds()))
	jsonOut(w, 200, map[string]bool{"signed_in": true})
	return true
}

func (s *Service) manageStaff(w http.ResponseWriter, r *http.Request, path string) {
	if _, err := s.OwnerRequest(r); err != nil {
		fail(w, err)
		return
	}
	switch {
	case path == "" && r.Method == http.MethodGet:
		out, err := s.staffDirectory(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, 200, out)
	case path == "/invitations" && r.Method == http.MethodPost:
		var input struct {
			Name string `json:"name"`
			Role string `json:"role"`
		}
		if !decode(w, r, &input) {
			return
		}
		invite, token, err := s.createStaffInvite(r, input.Name, input.Role)
		if err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, 201, map[string]any{"invitation": invite, "token": token})
	case r.Method == http.MethodPost:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 3 || (parts[0] != "members" && parts[0] != "invitations") || parts[2] != "revoke" {
			w.WriteHeader(404)
			return
		}
		if err := s.revokeStaff(r, parts[1], parts[0] == "invitations"); err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, 200, map[string]bool{"revoked": true})
	default:
		w.WriteHeader(405)
	}
}

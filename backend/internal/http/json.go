package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

type ErrorCode string

const (
	BadRequest           ErrorCode = "bad_request"
	Unauthenticated      ErrorCode = "unauthenticated"
	Forbidden            ErrorCode = "forbidden"
	NotFound             ErrorCode = "not_found"
	MethodNotAllowed     ErrorCode = "method_not_allowed"
	RevisionConflict     ErrorCode = "revision_conflict"
	IdempotencyConflict  ErrorCode = "idempotency_conflict"
	TooLarge             ErrorCode = "too_large"
	UnsupportedMediaType ErrorCode = "unsupported_media_type"
	InvalidFields        ErrorCode = "invalid_fields"
	RateLimited          ErrorCode = "rate_limited"
	InternalError        ErrorCode = "internal_error"
	Unavailable          ErrorCode = "unavailable"
	LessonLocked         ErrorCode = "lesson_locked"
)

type APIError struct {
	Code      ErrorCode         `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"request_id"`
	Fields    map[string]string `json:"fields,omitempty"`
}
type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

var statuses = map[ErrorCode]int{BadRequest: 400, Unauthenticated: 401, Forbidden: 403, NotFound: 404, MethodNotAllowed: 405, RevisionConflict: 409, IdempotencyConflict: 409, TooLarge: 413, UnsupportedMediaType: 415, InvalidFields: 422, RateLimited: 429, InternalError: 500, Unavailable: 503, LessonLocked: 403}
var messages = map[ErrorCode]string{BadRequest: "Malformed request.", Unauthenticated: "Authentication required.", Forbidden: "Access denied.", NotFound: "Resource not found.", MethodNotAllowed: "Method not allowed.", RevisionConflict: "A newer version exists.", IdempotencyConflict: "Operation key already used.", TooLarge: "Request is too large.", UnsupportedMediaType: "JSON content type required.", InvalidFields: "Invalid fields.", RateLimited: "Try again later.", InternalError: "Request failed.", Unavailable: "Service unavailable.", LessonLocked: "Lesson prerequisites are required."}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, code ErrorCode) {
	status, ok := statuses[code]
	if !ok {
		code = InternalError
		status = 500
	}
	writeJSON(w, status, ErrorEnvelope{APIError{Code: code, Message: messages[code], RequestID: w.Header().Get("X-Request-ID")}})
}

// DecodeJSON reads at most limit bytes, rejects unknown fields, null and trailing values.
// Call with a pointer to a typed request struct, never an unbounded map.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, UnsupportedMediaType)
		return false
	}
	if limit <= 0 || r.ContentLength > limit {
		writeError(w, TooLarge)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			writeError(w, TooLarge)
		} else {
			writeError(w, BadRequest)
		}
		return false
	}
	// Validate structure before typed decoding; a null request is not an object.
	if len(raw) == 0 {
		writeError(w, BadRequest)
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		writeError(w, BadRequest)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		writeError(w, BadRequest)
		return false
	}
	return true
}

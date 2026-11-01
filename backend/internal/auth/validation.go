package auth

// OwnerActionError describes a safe field rule or a blocked owner action.
// Credential verification and anonymous sign-in still use their uniform errors.
type OwnerActionError struct {
	Path, Reason string
	Conflict     bool
	Cause        error
	HTTPStatus   int
}

func (e *OwnerActionError) Error() string { return "owner action could not be completed" }
func (e *OwnerActionError) Unwrap() error {
	if e.Conflict {
		return ErrConflict
	}
	if e.Cause != nil {
		return e.Cause
	}
	return ErrAuth
}
func actionField(path, reason string) error { return &OwnerActionError{Path: path, Reason: reason} }

func tokenField(path, reason string) error {
	return &OwnerActionError{Path: path, Reason: reason, Cause: ErrForbidden}
}

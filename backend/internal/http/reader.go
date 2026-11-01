package httpserver

import (
	"context"
	"github.com/mbougarne/modawana/backend/internal/access"
	"net/http"
)

func (d *Domain) readerPrincipal(r *http.Request) access.Principal {
	if d.Learning != nil {
		if identity, e := d.Learning.Current(r.Context(), learningToken(r), false); e == nil {
			return access.Principal{Audience: access.Learner, ID: identity.ID}
		}
	}
	return access.Principal{}
}
func (d *Domain) ReaderContext(r *http.Request) context.Context {
	return access.WithPrincipal(r.Context(), d.readerPrincipal(r))
}

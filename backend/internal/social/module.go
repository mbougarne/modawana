// Package social manages owner-approved social publishing and delivers
// explicitly scheduled links to exact published revisions.
package social

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/extension"
)

const Slug = "social-publishing"
const CallbackPath = "/api/v1/owner/social/callback"

var ErrInvalid = errors.New("invalid social publishing request")
var ErrUnavailable = errors.New("social connection unavailable")
var ErrConflict = errors.New("social operation conflicts with saved state")

type Account struct {
	ID   string `json:"id"`
	Name string `json:"username"`
	URL  string `json:"url"`
}

type Receipt struct {
	ID      string  `json:"id"`
	URL     string  `json:"url"`
	Account Account `json:"account"`
}

type providerError struct {
	status int
	retry  time.Time
}

func (e *providerError) Error() string { return "social provider request failed" }

// Provider is the contract for a first-party social adapter. The extension
// owns scheduling, revision checks, credential storage, and delivery history.
type Provider interface {
	Descriptor() extension.Contribution
	Instance() string
	MaxTextLength() int
	ReplayWindow() time.Duration
	AuthorizationURL(state, challenge string) string
	Exchange(ctx context.Context, code, verifier string) (string, error)
	Verify(ctx context.Context, token string) (Account, error)
	Send(ctx context.Context, token, operationID, text string) (Receipt, error)
}

type Service struct {
	DB       *sql.DB
	Content  *content.Service
	baseURL  string
	key      func() ([]byte, error)
	provider Provider
}

func Open(cfg config.Config, database *sql.DB, contents *content.Service, provider Provider) (*Service, error) {
	if provider != nil {
		if _, err := cfg.ReadAuthKey(); err != nil {
			return nil, ErrUnavailable
		}
	}
	return &Service{DB: database, Content: contents, baseURL: cfg.BaseURL, key: cfg.ReadAuthKey, provider: provider}, nil
}

func (*Service) Slug() string { return Slug }

func (s *Service) Register(r *extension.Registrar) error {
	if s.provider == nil {
		return nil
	}
	return r.Contribution(s.provider.Descriptor(), nil)
}

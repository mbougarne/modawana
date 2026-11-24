// Package social manages owner-approved social publishing and delivers
// explicitly scheduled links to exact published revisions.
package social

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
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
var ErrImage = errors.New("a public JPEG cover is required")

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
type Message struct {
	AccountID   string
	OperationID string
	Text        string
	Title       string
	Link        string
	Image       string
	Container   string
}

type Provider interface {
	Descriptor() extension.Contribution
	Instance() string
	MaxTextLength() int
	ReplayWindow() time.Duration
	AuthorizationURL(state, challenge string) string
	Exchange(ctx context.Context, code, verifier string) (string, error)
	Verify(ctx context.Context, token string) (Account, error)
	Send(ctx context.Context, token string, message Message) (Receipt, error)
}

type Service struct {
	DB        *sql.DB
	Content   *content.Service
	baseURL   string
	key       func() ([]byte, error)
	providers map[string]Provider
}

func Open(cfg config.Config, database *sql.DB, contents *content.Service, providers ...Provider) (*Service, error) {
	s := &Service{DB: database, Content: contents, baseURL: cfg.BaseURL, key: cfg.ReadAuthKey, providers: map[string]Provider{}}
	for _, p := range providers {
		if p == nil {
			continue
		}
		d := p.Descriptor()
		if s.providers[d.Key] != nil || d.Kind != extension.ProviderAdapter || d.Target != "social" {
			return nil, ErrInvalid
		}
		s.providers[d.Key] = p
	}
	if len(s.providers) > 0 {
		if _, err := cfg.ReadAuthKey(); err != nil {
			return nil, ErrUnavailable
		}
	}
	return s, nil
}

func (*Service) Slug() string { return Slug }

func (s *Service) providerKeys() []string {
	return slices.Sorted(maps.Keys(s.providers))
}

func (s *Service) Register(r *extension.Registrar) error {
	for _, key := range s.providerKeys() {
		if err := r.Contribution(s.providers[key].Descriptor(), nil); err != nil {
			return err
		}
	}
	return nil
}

// A credential refresher returns an updated encrypted storage payload only
// when it needs to rotate or extend the credential.
type credentialRefresher interface {
	Refresh(context.Context, string) (string, error)
}

// A media publisher creates a private provider-side container before dispatch.
type mediaPublisher interface {
	PrepareMedia(context.Context, string, Message) (string, error)
	MediaReady(context.Context, string, string) error
}

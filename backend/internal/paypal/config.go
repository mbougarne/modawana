// Package paypal supplies the first-party PayPal adapter for Store.
package paypal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/publichttp"
	"github.com/mbougarne/modawana/backend/internal/store"
)

const Slug = "paypal"

var (
	ErrInvalid     = errors.New("invalid PayPal configuration or request")
	ErrUnavailable = errors.New("PayPal operation unavailable")
	ErrMismatch    = errors.New("PayPal details did not match")
	ErrSignature   = errors.New("PayPal signature rejected")
	ErrReview      = errors.New("PayPal operation needs review")
)
var connectionID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var remoteID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
var merchantID = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{13}$`)
var clientID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,180}$`)

type connectionConfig struct {
	ID         string `json:"id"`
	Mode       string `json:"mode"`
	Merchant   string `json:"merchant_id"`
	ClientID   string `json:"client_id"`
	SecretFile string `json:"client_secret_file"`
	WebhookID  string `json:"webhook_id"`
}

type settings struct {
	Active      string             `json:"active"`
	Connections []connectionConfig `json:"connections"`
}

type Service struct {
	store       *store.Service
	connections map[string]*client
	active      string
	paused      func() bool
}

func New(ctx context.Context, catalog *store.Service, cfg config.Config, raw string, paused func() bool) (*Service, error) {
	return open(ctx, catalog, cfg, raw, paused, publichttp.NewClient(5*time.Second))
}

func open(ctx context.Context, catalog *store.Service, cfg config.Config, raw string, paused func() bool, httpClient *http.Client) (*Service, error) {
	out := &Service{store: catalog, connections: map[string]*client{}, paused: paused}
	if raw == "" {
		return out, nil
	}
	var in settings
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) > 16<<10 || !strings.HasPrefix(strings.TrimSpace(raw), "{") || dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || len(in.Connections) > 8 {
		return nil, ErrInvalid
	}
	for _, c := range in.Connections {
		if !connectionID.MatchString(c.ID) || !merchantID.MatchString(c.Merchant) || !clientID.MatchString(c.ClientID) ||
			!remoteID.MatchString(c.WebhookID) || (c.Mode != "live" && c.Mode != "sandbox") || (c.Mode == "live" && !strings.HasPrefix(cfg.BaseURL, "https://")) || out.connections[c.ID] != nil {
			return nil, ErrInvalid
		}
		if _, err := config.PrivateText(c.SecretFile, cfg.DataRoot); err != nil {
			return nil, ErrInvalid
		}
		out.connections[c.ID] = &client{config: c, root: cfg.DataRoot, site: cfg.BaseURL, http: httpClient}
	}
	if in.Active != "" && out.connections[in.Active] == nil {
		return nil, ErrInvalid
	}
	out.active = in.Active
	if err := out.persist(ctx, catalog.DB, in.Connections); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) persist(ctx context.Context, d *sql.DB, connections []connectionConfig) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range connections {
		digest := sha256.Sum256([]byte(c.ClientID))
		result, err := tx.ExecContext(ctx, `INSERT INTO store_paypal_connections(id,mode,merchant,client_hash) VALUES(?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET id=excluded.id
			WHERE mode=excluded.mode AND merchant=excluded.merchant AND client_hash=excluded.client_hash`,
			c.ID, c.Mode, c.Merchant, hex.EncodeToString(digest[:]))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return ErrMismatch
		}
	}
	return tx.Commit()
}

func (s *Service) Slug() string { return Slug }

func (s *Service) Register(r *extension.Registrar) error {
	if err := r.Contribution(extension.Contribution{Kind: extension.ProviderAdapter, Target: "payments", Key: Slug, Version: 1,
		Label: extension.LocalizedText{"en": "PayPal", "fr": "PayPal", "ar": "PayPal"}}, func() bool { return len(s.connections) > 0 }); err != nil {
		return err
	}
	return r.Job(extension.Job{Name: "reconcile", Every: 2 * time.Second, Timeout: 18 * time.Second, Run: func(ctx context.Context) error {
		if s.paused != nil && s.paused() {
			return nil
		}
		return s.processReceipt(ctx)
	}})
}

func (s *Service) Active(currency string) string {
	if (s.paused != nil && s.paused()) || !supportedCurrency(currency) {
		return ""
	}
	return s.active
}

func (s *Service) HasConnection(id string) bool { return s.connections[id] != nil }

func supportedCurrency(code string) bool {
	return len(code) == 3 && strings.Contains(" AUD BRL CAD CNY CZK DKK EUR HKD HUF ILS JPY MYR MXN TWD NZD NOK PHP PLN GBP SGD SEK CHF THB USD ", " "+code+" ")
}

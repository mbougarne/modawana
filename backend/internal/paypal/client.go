package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/store"
	"golang.org/x/text/currency"
)

type client struct {
	config     connectionConfig
	root, site string
	http       *http.Client
	mu         sync.Mutex
	token      string
	until      time.Time
}

var _ store.PaymentAdapter = (*client)(nil)

func (c *client) apiHost() string {
	if c.config.Mode == "sandbox" {
		return "api-m.sandbox.paypal.com"
	}
	return "api-m.paypal.com"
}

func (c *client) tokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.until) {
		return c.token, nil
	}
	secret, err := config.PrivateText(c.config.SecretFile, c.root)
	if err != nil {
		return "", ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+c.apiHost()+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", ErrUnavailable
	}
	req.SetBasicAuth(c.config.ClientID, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err = c.send(req, &out); err != nil || out.AccessToken == "" || out.ExpiresIn <= 0 || len(out.AccessToken) > 4096 ||
		strings.ContainsAny(out.AccessToken, "\r\n \t") {
		return "", ErrUnavailable
	}
	c.token = out.AccessToken
	c.until = time.Now().Add(time.Duration(min(out.ExpiresIn, 3600)-30) * time.Second)
	return out.AccessToken, nil
}

func (c *client) send(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == 401 && req.URL.Path != "/v1/oauth2/token" {
			c.mu.Lock()
			c.until = time.Time{}
			c.mu.Unlock()
		}
		return ErrUnavailable
	}
	if out == nil {
		return nil
	}
	const limit = 256 << 10
	raw, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(raw) > limit || json.Unmarshal(raw, out) != nil {
		return ErrUnavailable
	}
	return nil
}

func (c *client) api(ctx context.Context, method, path, requestID string, in, out any) error {
	token, err := c.tokenFor(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return ErrInvalid
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.apiHost()+path, body)
	if err != nil {
		return ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Prefer", "return=representation")
	}
	if requestID != "" {
		req.Header.Set("PayPal-Request-Id", requestID)
	}
	return c.send(req, out)
}

type money struct {
	Currency string `json:"currency_code"`
	Value    string `json:"value"`
}

func formatMoney(code string, n int64) (money, error) {
	if !supportedCurrency(code) || n <= 0 {
		return money{}, ErrInvalid
	}
	unit, err := currency.ParseISO(code)
	if err != nil {
		return money{}, ErrInvalid
	}
	digits, _ := currency.Standard.Rounding(unit)
	scale := int64(1)
	for range digits {
		scale *= 10
	}
	switch code {
	case "HUF", "JPY", "TWD":
		if n%scale != 0 {
			return money{}, ErrInvalid
		}
		digits = 0
	}
	value := strconv.FormatInt(n/scale, 10)
	if digits > 0 {
		fraction := strconv.FormatInt(n%scale, 10)
		value += "." + strings.Repeat("0", digits-len(fraction)) + fraction
	}
	return money{code, value}, nil
}

func Payable(code string, n int64) bool {
	_, err := formatMoney(code, n)
	return err == nil
}

func matchesMoney(actual money, code string, minor int64) bool {
	expected, err := formatMoney(code, minor)
	if err != nil || actual.Currency != expected.Currency || len(actual.Value) > 25 {
		return false
	}
	normalize := func(s string) string {
		if strings.Contains(s, ".") {
			return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
		}
		return s
	}
	return normalize(actual.Value) == normalize(expected.Value)
}

type link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

type capture struct {
	ID     string `json:"id"`
	Amount money  `json:"amount"`
	State  string `json:"status"`
	Final  bool   `json:"final_capture"`
}

type unit struct {
	Reference string `json:"reference_id"`
	Custom    string `json:"custom_id"`
	Amount    money  `json:"amount"`
	Payee     struct {
		Merchant string `json:"merchant_id"`
	} `json:"payee"`
	Payments struct {
		Captures []capture `json:"captures"`
	} `json:"payments"`
}

type order struct {
	ID     string `json:"id"`
	State  string `json:"status"`
	Intent string `json:"intent"`
	Units  []unit `json:"purchase_units"`
	Links  []link `json:"links"`
}

func (c *client) Create(ctx context.Context, p store.Purchase) (store.PaymentApproval, error) {
	var out store.PaymentApproval
	op := p.Operation
	amount, err := formatMoney(op.Currency, op.AmountMinor)
	if err != nil || op.Connection != c.config.ID {
		return out, ErrInvalid
	}
	delivery := p.Customer
	address := map[string]string{"address_line_1": delivery.Address, "admin_area_2": delivery.City, "country_code": p.Country}
	for key, value := range map[string]string{"address_line_2": delivery.Address2, "admin_area_1": delivery.Region, "postal_code": delivery.PostalCode} {
		if value != "" {
			address[key] = value
		}
	}
	returnURL := c.site + "/" + p.Locale + "/store/checkout/cart?order=" + url.QueryEscape(op.Order)
	in := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []any{map[string]any{
			"reference_id": op.StableID, "custom_id": op.StableID, "amount": amount,
			"payee":    map[string]string{"merchant_id": c.config.Merchant},
			"shipping": map[string]any{"name": map[string]string{"full_name": delivery.Name}, "address": address},
		}},
		"payment_source": map[string]any{"paypal": map[string]any{"experience_context": map[string]string{
			"user_action": "PAY_NOW", "shipping_preference": "SET_PROVIDED_ADDRESS",
			"return_url": returnURL + "&paypal=return", "cancel_url": returnURL + "&paypal=cancel",
		}}},
	}
	var result order
	if err = c.api(ctx, http.MethodPost, "/v2/checkout/orders", op.StableID, in, &result); err != nil {
		return out, err
	}
	if !remoteID.MatchString(result.ID) {
		return out, ErrMismatch
	}
	for _, link := range result.Links {
		if (link.Rel == "approve" || link.Rel == "payer-action") && c.safeApproval(link.Href, result.ID) {
			return store.PaymentApproval{ProviderOrder: result.ID, ApprovalURL: link.Href}, nil
		}
	}
	return out, ErrMismatch
}

func (c *client) safeApproval(raw, id string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Path != "/checkoutnow" ||
		len(u.Query()["token"]) != 1 || u.Query().Get("token") != id {
		return false
	}
	if c.config.Mode == "sandbox" {
		return u.Host == "www.sandbox.paypal.com"
	}
	return u.Host == "www.paypal.com"
}

func (c *client) Capture(ctx context.Context, op store.PaymentOperation) error {
	if !remoteID.MatchString(op.ProviderOrder) || op.Connection != c.config.ID {
		return ErrInvalid
	}
	return c.api(ctx, http.MethodPost, "/v2/checkout/orders/"+op.ProviderOrder+"/capture", "cap-"+op.StableID, struct{}{}, nil)
}

func (c *client) Verify(ctx context.Context, op store.PaymentOperation) (store.PaymentVerification, error) {
	var out store.PaymentVerification
	if !remoteID.MatchString(op.ProviderOrder) || op.Connection != c.config.ID {
		return out, ErrInvalid
	}
	var current order
	if err := c.api(ctx, http.MethodGet, "/v2/checkout/orders/"+op.ProviderOrder, "", nil, &current); err != nil {
		return out, err
	}
	if current.ID != op.ProviderOrder || current.Intent != "CAPTURE" || len(current.Units) != 1 {
		return out, ErrMismatch
	}
	line := current.Units[0]
	if line.Reference != op.StableID || line.Custom != op.StableID || line.Payee.Merchant != c.config.Merchant ||
		!matchesMoney(line.Amount, op.Currency, op.AmountMinor) || len(line.Payments.Captures) > 1 {
		return out, ErrMismatch
	}
	if len(line.Payments.Captures) == 0 {
		out.Approved = current.State == "APPROVED"
		if current.State == "VOIDED" {
			out.Evidence = &store.PaymentEvidence{Operation: op, DefinitivelyUnpaid: true}
		}
		return out, nil
	}
	paid := line.Payments.Captures[0]
	if !remoteID.MatchString(paid.ID) || !matchesMoney(paid.Amount, op.Currency, op.AmountMinor) {
		return out, ErrMismatch
	}
	switch paid.State {
	case "COMPLETED", "PARTIALLY_REFUNDED", "REFUNDED":
		out.Refunded = paid.State != "COMPLETED"
		if !paid.Final {
			return out, ErrMismatch
		}
		out.Evidence = &store.PaymentEvidence{Operation: op, CaptureID: paid.ID}
	case "DECLINED", "FAILED", "DENIED":
		out.Evidence = &store.PaymentEvidence{Operation: op, DefinitivelyUnpaid: true}
	}
	return out, nil
}

// Package publisher is the bounded, scoped API client used by the CLI and runner.
package publisher

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}
type APIError struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
}

func (e *APIError) Error() string { return fmt.Sprintf("publisher API %d: %s", e.Status, e.Code) }
func New(base, token string, ca []byte) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("invalid blog origin")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("publisher requires HTTPS (HTTP is allowed only on a loopback IP)")
		}
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("BLOG_PUBLISHER_TOKEN is required")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if len(ca) > 0 {
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(ca) {
			return nil, errors.New("invalid publisher CA file")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &Client{Base: strings.TrimSuffix(base, "/"), Token: token, HTTP: &http.Client{Timeout: 60 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Do(ctx context.Context, method, path string, in any, key string) (json.RawMessage, error) {
	var body io.Reader
	if in != nil {
		b, e := json.Marshal(in)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(b)
	}
	return c.send(ctx, method, path, body, "application/json", key)
}
func (c *Client) send(ctx context.Context, method, path string, body io.Reader, contentType, key string) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/api/v1/publisher/") || strings.ContainsAny(path, "?#\r\n") {
		return nil, errors.New("invalid publisher path")
	}
	req, e := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if e != nil {
		return nil, errors.New("invalid publisher request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", contentType)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, e := c.HTTP.Do(req)
	if e != nil {
		return nil, errors.New("publisher network failure; retry with the same idempotency key")
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if e != nil {
		return nil, errors.New("publisher response interrupted; retry with the same idempotency key")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(b, &payload)
		code := payload.Error.Code
		if len(code) > 80 || code == "" {
			code = "request_failed"
		}
		return nil, &APIError{Status: response.StatusCode, Code: code}
	}
	if !json.Valid(b) {
		return nil, errors.New("invalid publisher JSON response")
	}
	return b, nil
}
func (c *Client) Upload(ctx context.Context, name, mime string, source io.Reader, key string) (json.RawMessage, error) {
	b, e := io.ReadAll(io.LimitReader(source, (50<<20)+1))
	if e != nil || len(b) > 50<<20 {
		return nil, errors.New("cannot read bounded media file")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(name)))
	h.Set("Content-Type", mime)
	part, e := writer.CreatePart(h)
	if e != nil {
		return nil, e
	}
	if _, e = part.Write(b); e != nil {
		return nil, e
	}
	if e = writer.Close(); e != nil {
		return nil, e
	}
	return c.send(ctx, "POST", "/api/v1/publisher/media", &body, writer.FormDataContentType(), key)
}

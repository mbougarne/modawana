package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const fetchInputLimit = 64 << 10
const fetchBodyLimit = 64 << 10

type fetchRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type fetchResponse struct {
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

func pluginHTTPRequest(ctx context.Context, hosts []string, raw []byte) (*http.Request, error) {
	var input fetchRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
		return nil, ErrStorageInvalid
	}
	u, err := url.Parse(input.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" ||
		u.Host != u.Hostname() || !slices.Contains(hosts, u.Host) || len(input.URL) > 2048 {
		return nil, ErrStorageInvalid
	}
	switch input.Method {
	case "GET", "HEAD":
		if len(input.Body) != 0 {
			return nil, ErrStorageInvalid
		}
	case "POST", "PUT", "PATCH", "DELETE":
	default:
		return nil, ErrStorageInvalid
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, u.String(), bytes.NewReader(input.Body))
	if err != nil {
		return nil, ErrStorageInvalid
	}
	for key, value := range input.Headers {
		name := http.CanonicalHeaderKey(key)
		switch name {
		case "Accept", "Content-Type", "Authorization", "X-Api-Key", "Idempotency-Key":
		default:
			return nil, ErrStorageInvalid
		}
		if !boundedText(value, 4096) || req.Header.Values(name) != nil {
			return nil, ErrStorageInvalid
		}
		req.Header.Set(name, value)
	}
	return req, nil
}

func (r *Runtime) fetch(ctx context.Context, inv *invocation, raw []byte) ([]byte, error) {
	if !inv.externalAccess {
		return nil, ErrStorageInvalid
	}
	req, err := pluginHTTPRequest(ctx, inv.manifest.HTTPSHosts, raw)
	if err != nil {
		return nil, err
	}
	res, err := r.fetchClient.Do(req)
	if err != nil {
		return nil, errors.New("plugin HTTPS request failed")
	}
	defer res.Body.Close()
	if strings.TrimSpace(res.Header.Get("Content-Encoding")) != "" {
		return nil, ErrStorageInvalid
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, fetchBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > fetchBodyLimit {
		return nil, ErrQuota
	}
	return json.Marshal(fetchResponse{Status: res.StatusCode, Body: body})
}

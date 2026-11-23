package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const timestampWindow = 5 * time.Minute

type keyFile struct {
	Current       string    `json:"current"`
	Previous      string    `json:"previous,omitempty"`
	PreviousUntil time.Time `json:"previous_until,omitempty"`
}

type statusEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      struct {
		Connection string `json:"connection"`
		Account    string `json:"account"`
		Mode       string `json:"mode"`
		Subject    string `json:"resource_id"`
		Version    int64  `json:"version"`
		Status     string `json:"status"`
	} `json:"data"`
}

func decodeStrict(data []byte, target any) error {
	return json.Unmarshal(data, target, json.RejectUnknownMembers(true))
}

func signingKey(value string) []byte {
	payload, ok := strings.CutPrefix(value, "whsec_")
	if !ok || len(payload) > 88 {
		return nil
	}
	key, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil || len(key) < 24 || len(key) > 64 || base64.StdEncoding.EncodeToString(key) != payload {
		return nil
	}
	return key
}

func (i *Inbox) readKeys(path string, now time.Time) ([][]byte, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	root, err := filepath.EvalSymlinks(i.root)
	if err != nil {
		return nil, ErrUnavailable
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return nil, ErrUnavailable
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 2048 {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, 2049))
	var keys keyFile
	if err != nil || len(raw) > 2048 || decodeStrict(raw, &keys) != nil {
		return nil, ErrUnavailable
	}
	current, previous := signingKey(keys.Current), signingKey(keys.Previous)
	if current == nil || (keys.Previous == "") != keys.PreviousUntil.IsZero() ||
		(keys.Previous != "" && (previous == nil || hmac.Equal(current, previous))) {
		return nil, ErrUnavailable
	}
	if previous != nil && now.Before(keys.PreviousUntil) {
		return [][]byte{current, previous}, nil
	}
	return [][]byte{current}, nil
}

func header(h http.Header, name string) string {
	values := h.Values(name)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

// verifyStandardV1 checks the Standard Webhooks HMAC over the exact raw body.
func (i *Inbox) verifyStandardV1(c Connection, headers http.Header, raw []byte, now time.Time) (string, statusEvent, error) {
	var event statusEvent
	id := header(headers, "Webhook-Id")
	ts := header(headers, "Webhook-Timestamp")
	signatures := header(headers, "Webhook-Signature")
	seconds, err := strconv.ParseInt(ts, 10, 64)
	if !identifier.MatchString(id) || len(signatures) > 1024 || err != nil ||
		strconv.FormatInt(seconds, 10) != ts || seconds < now.Add(-timestampWindow).Unix() ||
		seconds > now.Add(timestampWindow).Unix() {
		return "", event, ErrSignature
	}
	values := strings.Fields(signatures)
	if len(values) == 0 || len(values) > 8 {
		return "", event, ErrSignature
	}
	var candidates [][]byte
	for _, signature := range values {
		tag, ok := strings.CutPrefix(signature, "v1,")
		if ok {
			decoded, err := base64.StdEncoding.Strict().DecodeString(tag)
			if err == nil && len(decoded) == sha256.Size && base64.StdEncoding.EncodeToString(decoded) == tag {
				candidates = append(candidates, decoded)
			}
		}
	}
	if len(candidates) == 0 {
		return "", event, ErrSignature
	}
	keys, err := i.readKeys(c.KeyFile, now)
	if err != nil {
		return "", event, err
	}
	matched := false
	for _, key := range keys {
		mac := hmac.New(sha256.New, key)
		_, _ = io.WriteString(mac, id+"."+ts+".")
		_, _ = mac.Write(raw)
		sum := mac.Sum(nil)
		for _, candidate := range candidates {
			matched = hmac.Equal(sum, candidate) || matched
		}
	}
	if !matched {
		return "", event, ErrSignature
	}
	if err := decodeStrict(raw, &event); err != nil || event.Type != "resource.status" ||
		event.Timestamp.IsZero() || !identifier.MatchString(event.Data.Subject) || event.Data.Version < 1 ||
		(event.Data.Status != "active" && event.Data.Status != "inactive") {
		return "", event, ErrInvalid
	}
	if event.Data.Connection != c.ID || event.Data.Account != c.Account || event.Data.Mode != c.Mode {
		return "", event, ErrScope
	}
	return id, event, nil
}

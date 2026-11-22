package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

var capabilityName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var domainLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type SettingField struct {
	Name    string            `json:"name"`
	Label   map[string]string `json:"label"`
	Type    string            `json:"type"`
	Default json.RawMessage   `json:"default"`
}

type NamedSecret struct {
	Name  string            `json:"name"`
	Label map[string]string `json:"label"`
}

type ManifestJob struct {
	Name         string `json:"name"`
	EverySeconds int    `json:"every_seconds"`
}

type Migration struct {
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

func (m Manifest) validateStageTwo() error {
	for _, declaration := range []struct {
		capability string
		count, max int
	}{
		{"settings", len(m.Settings), 24}, {"jobs", len(m.Jobs), 8},
		{"http_fetch", len(m.HTTPSHosts), 8}, {"secrets", len(m.Secrets), 8},
	} {
		if m.Capability(declaration.capability) != (declaration.count > 0) || declaration.count > declaration.max ||
			declaration.count > 0 && m.HostAPIVersion < 2 {
			return fmt.Errorf("%w: %s capability and declaration", ErrManifest, declaration.capability)
		}
	}
	if len(m.Migrations) > 32 || (len(m.Migrations) > 0 && (!m.Capability("storage") || m.HostAPIVersion < 2)) {
		return fmt.Errorf("%w: migrations require API 2 and storage", ErrManifest)
	}
	names := map[string]bool{}
	take := func(name string) bool {
		if !capabilityName.MatchString(name) || names[name] {
			return false
		}
		names[name] = true
		return true
	}
	for _, field := range m.Settings {
		if !take(field.Name) || !validLabels(field.Label) || !validSetting(field, field.Default) {
			return fmt.Errorf("%w: invalid settings field", ErrManifest)
		}
	}
	clear(names)
	for _, secret := range m.Secrets {
		if !take(secret.Name) || !validLabels(secret.Label) {
			return fmt.Errorf("%w: invalid secret declaration", ErrManifest)
		}
	}
	clear(names)
	for _, job := range m.Jobs {
		if !take(job.Name) || job.EverySeconds < 60 || job.EverySeconds > 86400 {
			return fmt.Errorf("%w: invalid job declaration", ErrManifest)
		}
	}
	clear(names)
	for _, step := range m.Migrations {
		if !take(step.Name) || !digestPattern.MatchString(step.Checksum) {
			return fmt.Errorf("%w: invalid migration declaration", ErrManifest)
		}
	}
	clear(names)
	for _, host := range m.HTTPSHosts {
		if !validHTTPSHost(host) || names[host] {
			return fmt.Errorf("%w: invalid HTTPS host declaration", ErrManifest)
		}
		names[host] = true
	}
	return nil
}

func validLabels(labels map[string]string) bool {
	if strings.TrimSpace(labels["en"]) == "" {
		return false
	}
	for locale, label := range labels {
		if (locale != "en" && locale != "fr" && locale != "ar") || !boundedText(label, 160) {
			return false
		}
	}
	return true
}

func validSetting(field SettingField, raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	switch field.Type {
	case "string":
		var value string
		return json.Unmarshal(raw, &value) == nil && boundedText(value, 1024)
	case "boolean":
		var value bool
		return json.Unmarshal(raw, &value) == nil
	case "integer":
		var value int32
		return json.Unmarshal(raw, &value) == nil
	default:
		return false
	}
}

func validHTTPSHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !domainLabel.MatchString(label) {
			return false
		}
	}
	return true
}

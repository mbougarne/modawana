// Package plugin is the ADR 0001 stage 1 WebAssembly plugin host: it loads
// owner-approved WASI modules from plugins/{slug}/ under the data root and
// installs them on the extension registries as a second implementation of
// extension.Extension. The host keeps all authority: plugins exchange bounded
// JSON bytes, receive no filesystem, network, or environment access, and every
// effect goes through capability-gated host functions.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// HostAPIVersion is the newest host API this binary serves. Manifests
// requiring a newer version are refused at install time.
const HostAPIVersion = 1

// manifestSchemaVersion is the manifest document schema this binary parses.
const manifestSchemaVersion = 1

const manifestLimit = 64 << 10
const maxHooks = 16
const maxRoutes = 16

// ErrManifest reports a manifest that failed strict validation.
var ErrManifest = errors.New("invalid plugin manifest")

// Capabilities installable in stage 1. Everything else in the ADR capability
// table (settings, jobs, http_fetch, secrets, content_read, mcp_tools) is
// refused until the stage that delivers it.
var supportedCapabilities = []string{"hooks", "routes", "storage"}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var hookNamePattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)
var modulePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,119}\.wasm$`)
var versionPattern = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,64}$`)
var routePathPattern = regexp.MustCompile(`^/[a-z0-9_-]+(/[a-z0-9_-]+)*$`)

// reservedRouteSegments are management-API verbs under /api/v1/plugins/{slug}/
// that a plugin route may not shadow.
var reservedRouteSegments = map[string]bool{"install": true, "enable": true, "disable": true, "remove": true}

var routeMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// Manifest is the versioned plugin declaration the owner approves. Parsing is
// strict: unknown fields, duplicates, and undeclared capability use are refused.
type Manifest struct {
	SchemaVersion  int             `json:"schema_version"`
	HostAPIVersion int             `json:"host_api_version"`
	Slug           string          `json:"slug"`
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	Description    string          `json:"description,omitempty"`
	Module         string          `json:"module"`
	Capabilities   []string        `json:"capabilities"`
	Hooks          []ManifestHook  `json:"hooks,omitempty"`
	Routes         []ManifestRoute `json:"routes,omitempty"`
}

// ManifestHook declares one hook subscription and its contract version.
type ManifestHook struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// ManifestRoute declares one handler mounted by the host under the plugin's
// namespace: /plugins/{slug}{path} when public, /api/v1/plugins/{slug}{path}
// (owner-only) otherwise.
type ManifestRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Public bool   `json:"public,omitempty"`
}

// Capability reports whether the manifest declares the capability.
func (m Manifest) Capability(name string) bool { return slices.Contains(m.Capabilities, name) }

// ParseManifest strictly decodes and validates one manifest document. The
// directory name is the identity: a manifest whose slug differs is refused.
func ParseManifest(raw []byte, directory string) (Manifest, error) {
	var m Manifest
	if len(raw) > manifestLimit {
		return m, fmt.Errorf("%w: document exceeds %d bytes", ErrManifest, manifestLimit)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, fmt.Errorf("%w: %s", ErrManifest, err.Error())
	}
	if decoder.More() {
		return m, fmt.Errorf("%w: trailing data", ErrManifest)
	}
	if err := m.validate(directory); err != nil {
		return m, err
	}
	return m, nil
}

func (m Manifest) validate(directory string) error {
	if m.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version %d", ErrManifest, m.SchemaVersion)
	}
	if m.HostAPIVersion < 1 {
		return fmt.Errorf("%w: host_api_version must be positive", ErrManifest)
	}
	if m.HostAPIVersion > HostAPIVersion {
		return fmt.Errorf("%w: requires host API %d, this host serves %d", ErrManifest, m.HostAPIVersion, HostAPIVersion)
	}
	if !slugPattern.MatchString(m.Slug) {
		return fmt.Errorf("%w: slug", ErrManifest)
	}
	if m.Slug != directory {
		return fmt.Errorf("%w: slug %q does not match directory %q", ErrManifest, m.Slug, directory)
	}
	if !boundedText(m.Name, 120) || !boundedText(m.Description, 500) || m.Name == "" {
		return fmt.Errorf("%w: name or description", ErrManifest)
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("%w: version", ErrManifest)
	}
	if !modulePattern.MatchString(m.Module) {
		return fmt.Errorf("%w: module", ErrManifest)
	}
	if err := m.validateCapabilities(); err != nil {
		return err
	}
	if err := m.validateHooks(); err != nil {
		return err
	}
	return m.validateRoutes()
}

func (m Manifest) validateCapabilities() error {
	seen := map[string]bool{}
	for _, capability := range m.Capabilities {
		if !slices.Contains(supportedCapabilities, capability) {
			return fmt.Errorf("%w: capability %q is not installable on this host", ErrManifest, capability)
		}
		if seen[capability] {
			return fmt.Errorf("%w: duplicate capability %q", ErrManifest, capability)
		}
		seen[capability] = true
	}
	// A declared capability must be used and a used capability declared: the
	// owner approves exactly what the plugin can reach.
	if seen["hooks"] != (len(m.Hooks) > 0) {
		return fmt.Errorf("%w: hooks and the hooks capability must be declared together", ErrManifest)
	}
	if seen["routes"] != (len(m.Routes) > 0) {
		return fmt.Errorf("%w: routes and the routes capability must be declared together", ErrManifest)
	}
	return nil
}

func (m Manifest) validateHooks() error {
	if len(m.Hooks) > maxHooks {
		return fmt.Errorf("%w: more than %d hooks", ErrManifest, maxHooks)
	}
	seen := map[string]bool{}
	for _, hook := range m.Hooks {
		if !hookNamePattern.MatchString(hook.Name) || len(hook.Name) > 120 || hook.Version < 1 {
			return fmt.Errorf("%w: hook %q", ErrManifest, hook.Name)
		}
		if seen[hook.Name] {
			return fmt.Errorf("%w: duplicate hook %q", ErrManifest, hook.Name)
		}
		seen[hook.Name] = true
	}
	return nil
}

func (m Manifest) validateRoutes() error {
	if len(m.Routes) > maxRoutes {
		return fmt.Errorf("%w: more than %d routes", ErrManifest, maxRoutes)
	}
	seen := map[string]bool{}
	for _, route := range m.Routes {
		if !routeMethods[route.Method] || len(route.Path) > 128 || !routePathPattern.MatchString(route.Path) {
			return fmt.Errorf("%w: route %q %q", ErrManifest, route.Method, route.Path)
		}
		if reservedRouteSegments[strings.SplitN(strings.TrimPrefix(route.Path, "/"), "/", 2)[0]] {
			return fmt.Errorf("%w: route path %q shadows a management route", ErrManifest, route.Path)
		}
		// Public routes are read-only in stage 1: unauthenticated writes have
		// no CSRF boundary. Owner routes keep the owner API's write checks.
		if route.Public && route.Method != "GET" && route.Method != "HEAD" {
			return fmt.Errorf("%w: public route %q %q must be GET or HEAD", ErrManifest, route.Method, route.Path)
		}
		key := route.Method + " " + route.Path
		if seen[key] {
			return fmt.Errorf("%w: duplicate route %q", ErrManifest, key)
		}
		seen[key] = true
	}
	return nil
}

func boundedText(value string, limit int) bool {
	if len(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// AddedCapabilities lists capabilities the next manifest declares that the
// approved one does not — the upgrade diff the owner must re-approve.
func AddedCapabilities(approved, next Manifest) []string {
	var added []string
	for _, capability := range next.Capabilities {
		if !approved.Capability(capability) {
			added = append(added, capability)
		}
	}
	return added
}

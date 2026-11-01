// Package extension holds the in-process registries from ADR 0001 stage 0:
// hooks, HTTP routes, scheduled jobs, settings sections, and MCP tools.
// Built-in extensions implement Extension in plain Go; the WebAssembly plugin
// host later becomes a second implementation of the same contract. Every
// payload and result crossing a registry is therefore bounded JSON bytes,
// never a shared Go pointer, and the host keeps authority over access checks,
// sanitization, and how results are used.
package extension

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
)

// ErrInvalid reports a rejected registration.
var ErrInvalid = errors.New("invalid extension registration")

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var hookPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,119}$`)

// Extension is one installable unit. Built-ins are compiled in; a Wasm-backed
// implementation will satisfy the same interface from an approved manifest.
type Extension interface {
	// Slug is the stable identifier, shaped like a plugin directory name.
	Slug() string
	// Register declares the extension's hooks, routes, jobs, settings
	// sections, and MCP tools. It runs once, before the host serves traffic.
	Register(r *Registrar) error
}

// Host owns every registry and dispatches to registered extensions.
type Host struct {
	logger   *slog.Logger
	mu       sync.RWMutex
	slugs    map[string]bool
	hooks    map[string][]hookRegistration
	jobs     []Job
	routes   []RouteRegistration
	sections []SettingsRegistration
	tools    []MCPTool
	indexed  map[string]int
}

// NewHost returns an empty host. A nil logger discards outcome records.
func NewHost(logger *slog.Logger) *Host {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Host{logger: logger, slugs: map[string]bool{}, hooks: map[string][]hookRegistration{}, indexed: map[string]int{}}
}

// Install registers one extension. Registrations are validated and committed
// atomically: a rejected declaration leaves no partial state behind.
func (h *Host) Install(x Extension) error {
	slug := x.Slug()
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("%w: slug %q", ErrInvalid, slug)
	}
	r := &Registrar{slug: slug}
	if err := x.Register(r); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.slugs[slug] {
		return fmt.Errorf("%w: slug %q already installed", ErrInvalid, slug)
	}
	for _, tool := range r.tools {
		if _, exists := h.indexed[tool.Name]; exists {
			return fmt.Errorf("%w: MCP tool %q already registered", ErrInvalid, tool.Name)
		}
	}
	h.slugs[slug] = true
	for name, registrations := range r.hooks {
		h.hooks[name] = append(h.hooks[name], registrations...)
	}
	h.jobs = append(h.jobs, r.jobs...)
	h.routes = append(h.routes, r.routes...)
	h.sections = append(h.sections, r.sections...)
	for _, tool := range r.tools {
		h.indexed[tool.Name] = len(h.tools)
		h.tools = append(h.tools, tool)
	}
	return nil
}

// Registrar collects one extension's declarations during Install.
type Registrar struct {
	slug     string
	hooks    map[string][]hookRegistration
	jobs     []Job
	routes   []RouteRegistration
	sections []SettingsRegistration
	tools    []MCPTool
}

// Hook registers a handler for a named, versioned hook point.
func (r *Registrar) Hook(name string, version int, handler HookFunc) error {
	if !hookPattern.MatchString(name) || version < 1 || handler == nil {
		return fmt.Errorf("%w: hook %q", ErrInvalid, name)
	}
	if r.hooks == nil {
		r.hooks = map[string][]hookRegistration{}
	}
	r.hooks[name] = append(r.hooks[name], hookRegistration{slug: r.slug, version: version, handler: handler})
	return nil
}

// Job registers a scheduled job run by the host job runner. A job without an
// explicit timeout is bounded by its own cadence.
func (r *Registrar) Job(j Job) error {
	if !namePattern.MatchString(j.Name) || j.Every <= 0 || j.Run == nil || j.Timeout < 0 || j.MaxBackoff < 0 {
		return fmt.Errorf("%w: job %q", ErrInvalid, j.Name)
	}
	j.slug = r.slug
	if j.Timeout == 0 {
		j.Timeout = j.Every
	}
	if j.MaxBackoff == 0 {
		j.MaxBackoff = 4 * j.Every
	}
	r.jobs = append(r.jobs, j)
	return nil
}

// Route declares a handler for the host to serve under the extension's
// namespace. Nothing registers routes in stage 0; when the host mounts them,
// every route gets the host's access checks, rate limits, and logging first.
func (r *Registrar) Route(route Route) error {
	if !routeMethods[route.Method] || !routePattern.MatchString(route.Path) || route.Handler == nil {
		return fmt.Errorf("%w: route %q %q", ErrInvalid, route.Method, route.Path)
	}
	r.routes = append(r.routes, RouteRegistration{Extension: r.slug, Route: route})
	return nil
}

// SettingsSection declares a dashboard settings section rendered generically
// by the host; values are validated by the host, never by the extension.
func (r *Registrar) SettingsSection(s SettingsSection) error {
	if !namePattern.MatchString(s.Key) || len(s.Schema) == 0 {
		return fmt.Errorf("%w: settings section %q", ErrInvalid, s.Key)
	}
	r.sections = append(r.sections, SettingsRegistration{Extension: r.slug, SettingsSection: s})
	return nil
}

// MCPTool registers one MCP tool definition and its handler.
func (r *Registrar) MCPTool(t MCPTool) error {
	if !namePattern.MatchString(t.Name) || t.Description == "" || len(t.Properties) == 0 || t.Call == nil {
		return fmt.Errorf("%w: MCP tool %q", ErrInvalid, t.Name)
	}
	for _, tool := range r.tools {
		if tool.Name == t.Name {
			return fmt.Errorf("%w: MCP tool %q already registered", ErrInvalid, t.Name)
		}
	}
	for _, required := range t.Required {
		if _, exists := t.Properties[required]; !exists {
			return fmt.Errorf("%w: MCP tool %q requires undeclared %q", ErrInvalid, t.Name, required)
		}
	}
	t.slug = r.slug
	r.tools = append(r.tools, t)
	return nil
}

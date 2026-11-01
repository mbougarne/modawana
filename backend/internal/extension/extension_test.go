package extension

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type declared struct {
	slug     string
	register func(r *Registrar) error
}

func (d declared) Slug() string                { return d.slug }
func (d declared) Register(r *Registrar) error { return d.register(r) }

func noHook(ctx context.Context, hook string, payload []byte) ([]byte, error) {
	return []byte(`{}`), nil
}

func TestInstallValidatesAndCommitsAtomically(t *testing.T) {
	h := NewHost(nil)
	ok := func(r *Registrar) error { return r.Hook("content.rendered_meta", 1, noHook) }
	if err := h.Install(declared{"Bad Slug", ok}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := h.Install(declared{"first", ok}); err != nil {
		t.Fatal(err)
	}
	if err := h.Install(declared{"first", ok}); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate slug accepted")
	}
	// A rejected declaration must leave nothing behind, including earlier
	// valid registrations from the same extension.
	partial := func(r *Registrar) error {
		if err := r.Job(Job{Name: "kept", Every: time.Hour, Run: func(context.Context) error { return nil }}); err != nil {
			return err
		}
		return r.Hook("no-dots", 1, noHook)
	}
	if err := h.Install(declared{"second", partial}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if len(h.Jobs()) != 0 {
		t.Fatal("partial registration committed")
	}
	if err := h.Install(declared{"second", ok}); err != nil {
		t.Fatal("rejected install consumed the slug:", err)
	}
	if got := len(h.DispatchHook(context.Background(), "content.rendered_meta", nil)); got != 2 {
		t.Fatal("hook registrations", got)
	}
}

func TestRegistrarRejectsInvalidDeclarations(t *testing.T) {
	run := func(context.Context) error { return nil }
	for name, register := range map[string]func(r *Registrar) error{
		"hook name":    func(r *Registrar) error { return r.Hook("nodots", 1, noHook) },
		"hook version": func(r *Registrar) error { return r.Hook("a.b", 0, noHook) },
		"hook handler": func(r *Registrar) error { return r.Hook("a.b", 1, nil) },
		"job cadence":  func(r *Registrar) error { return r.Job(Job{Name: "x", Run: run}) },
		"job run":      func(r *Registrar) error { return r.Job(Job{Name: "x", Every: time.Hour}) },
		"job name":     func(r *Registrar) error { return r.Job(Job{Name: "UPPER", Every: time.Hour, Run: run}) },
		"route method": func(r *Registrar) error {
			return r.Route(Route{Method: "TRACE", Path: "/x", Handler: http.NotFoundHandler()})
		},
		"route path": func(r *Registrar) error {
			return r.Route(Route{Method: "GET", Path: "x//y", Handler: http.NotFoundHandler()})
		},
		"route handler": func(r *Registrar) error { return r.Route(Route{Method: "GET", Path: "/x"}) },
		"settings":      func(r *Registrar) error { return r.SettingsSection(SettingsSection{Key: "x"}) },
		"tool schema": func(r *Registrar) error {
			return r.MCPTool(MCPTool{Name: "x", Description: "d", Call: func(context.Context, json.RawMessage) ([]byte, error) { return nil, nil }})
		},
		"tool required": func(r *Registrar) error {
			return r.MCPTool(MCPTool{Name: "x", Description: "d", Properties: map[string]any{"a": true}, Required: []string{"b"}, Call: func(context.Context, json.RawMessage) ([]byte, error) { return nil, nil }})
		},
	} {
		if err := NewHost(nil).Install(declared{"probe", register}); !errors.Is(err, ErrInvalid) {
			t.Fatal(name, err)
		}
	}
}

func TestDispatchHookIsNonAuthoritative(t *testing.T) {
	h := NewHost(nil)
	register := func(r *Registrar) error {
		if err := r.Hook("a.b", 1, func(context.Context, string, []byte) ([]byte, error) { return []byte(`{"n":1}`), nil }); err != nil {
			return err
		}
		if err := r.Hook("a.b", 1, func(context.Context, string, []byte) ([]byte, error) { return nil, errors.New("boom") }); err != nil {
			return err
		}
		if err := r.Hook("a.b", 1, func(context.Context, string, []byte) ([]byte, error) { panic("boom") }); err != nil {
			return err
		}
		if err := r.Hook("a.b", 1, func(context.Context, string, []byte) ([]byte, error) {
			return []byte(strings.Repeat("x", hookResultLimit+1)), nil
		}); err != nil {
			return err
		}
		return r.Hook("a.b", 1, func(context.Context, string, []byte) ([]byte, error) { return []byte(`{"n":2}`), nil })
	}
	if err := h.Install(declared{"mixed", register}); err != nil {
		t.Fatal(err)
	}
	results := h.DispatchHook(context.Background(), "a.b", []byte(`{}`))
	if len(results) != 2 || string(results[0]) != `{"n":1}` || string(results[1]) != `{"n":2}` {
		t.Fatal(results)
	}
	if got := h.DispatchHook(context.Background(), "unregistered.hook", nil); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestNextJobDelayBacksOffOnConsecutiveFailures(t *testing.T) {
	job := Job{Every: time.Hour, MaxBackoff: 4 * time.Hour}
	if got := nextJobDelay(time.Hour, job, false, true); got != time.Hour {
		t.Fatal("success cadence", got)
	}
	if got := nextJobDelay(time.Hour, job, true, false); got != time.Hour {
		t.Fatal("first failure keeps cadence", got)
	}
	delay := nextJobDelay(time.Hour, job, true, true)
	if delay != 2*time.Hour {
		t.Fatal("second consecutive failure", delay)
	}
	delay = nextJobDelay(delay, job, true, true)
	if delay != 4*time.Hour {
		t.Fatal("third consecutive failure", delay)
	}
	if got := nextJobDelay(delay, job, true, true); got != 4*time.Hour {
		t.Fatal("cap", got)
	}
	if got := nextJobDelay(delay, job, false, true); got != time.Hour {
		t.Fatal("recovery", got)
	}
}

func TestRunJobsRunsBoundsAndStops(t *testing.T) {
	h := NewHost(nil)
	var runs, panics atomic.Int64
	bounded := make(chan error, 1)
	register := func(r *Registrar) error {
		if err := r.Job(Job{Name: "counting", Every: 5 * time.Millisecond, Run: func(context.Context) error {
			runs.Add(1)
			return nil
		}}); err != nil {
			return err
		}
		if err := r.Job(Job{Name: "panicking", Every: 5 * time.Millisecond, Run: func(context.Context) error {
			panics.Add(1)
			panic("boom")
		}}); err != nil {
			return err
		}
		return r.Job(Job{Name: "bounded", Every: 5 * time.Millisecond, Timeout: 10 * time.Millisecond, Run: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				select {
				case bounded <- ctx.Err():
				default:
				}
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return errors.New("run time not bounded")
			}
		}})
	}
	if err := h.Install(declared{"jobs", register}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.RunJobs(ctx)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	for runs.Load() < 2 || panics.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("jobs did not keep running", runs.Load(), panics.Load())
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case err := <-bounded:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-deadline:
		t.Fatal("job run time not bounded")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestMCPToolRegistryDefinitionsAndLookup(t *testing.T) {
	h := NewHost(nil)
	echo := func(ctx context.Context, raw json.RawMessage) ([]byte, error) { return []byte(`{"ok":true}`), nil }
	register := func(r *Registrar) error {
		if err := r.MCPTool(MCPTool{Name: "first_tool", Description: "First.", Properties: map[string]any{"a": true}, Required: []string{"a"}, ReadOnly: true, Idempotent: true, Call: echo}); err != nil {
			return err
		}
		return r.MCPTool(MCPTool{Name: "second_tool", Description: "Second.", Properties: map[string]any{"b": true}, Call: echo})
	}
	if err := h.Install(declared{"tools", register}); err != nil {
		t.Fatal(err)
	}
	conflict := func(r *Registrar) error {
		return r.MCPTool(MCPTool{Name: "first_tool", Description: "Clash.", Properties: map[string]any{"a": true}, Call: echo})
	}
	if err := h.Install(declared{"clash", conflict}); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate tool name accepted across extensions")
	}
	tools := h.MCPTools()
	if len(tools) != 2 || tools[0].Name != "first_tool" || tools[1].Name != "second_tool" {
		t.Fatal(tools)
	}
	if _, ok := h.MCPTool("missing"); ok {
		t.Fatal("missing tool resolved")
	}
	encoded, err := json.Marshal(tools[0].Definition())
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"annotations":{"destructiveHint":false,"idempotentHint":true,"openWorldHint":false,"readOnlyHint":true},"description":"First.","inputSchema":{"additionalProperties":false,"properties":{"a":true},"required":["a"],"type":"object"},"name":"first_tool"}`
	if string(encoded) != expected {
		t.Fatal(string(encoded))
	}
}

func TestRouteAndSettingsRegistries(t *testing.T) {
	h := NewHost(nil)
	register := func(r *Registrar) error {
		if err := r.Route(Route{Method: "GET", Path: "/status", Public: true, Handler: http.NotFoundHandler()}); err != nil {
			return err
		}
		return r.SettingsSection(SettingsSection{Key: "delivery", Schema: map[string]any{"type": "object"}})
	}
	if err := h.Install(declared{"routed", register}); err != nil {
		t.Fatal(err)
	}
	routes := h.Routes()
	if len(routes) != 1 || routes[0].Extension != "routed" || routes[0].Path != "/status" || !routes[0].Public {
		t.Fatal(routes)
	}
	sections := h.SettingsSections()
	if len(sections) != 1 || sections[0].Extension != "routed" || sections[0].Key != "delivery" {
		t.Fatal(sections)
	}
}

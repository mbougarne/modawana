package extension

import (
	"context"
	"errors"
	"time"
)

// HookRenderedMeta fires while building page metadata; handlers may contribute
// social-card values that the host sanitizes and bounds.
const HookRenderedMeta = "content.rendered_meta"

const hookResultLimit = 64 << 10
const hookTimeout = 5 * time.Second

// HookFunc handles one hook invocation. The payload and the result are the
// bounded JSON documents of the hook's versioned contract — the same bytes a
// Wasm module will exchange later.
type HookFunc func(ctx context.Context, hook string, payload []byte) ([]byte, error)

type hookRegistration struct {
	slug    string
	version int
	handler HookFunc
}

// DispatchHook runs every handler registered for the hook in installation
// order and returns their results. Hooks are non-authoritative: a handler
// error, panic, timeout, or oversized result is logged and dropped, never
// surfaced to the caller, and the caller keeps final say over every result
// it chooses to use.
func (h *Host) DispatchHook(ctx context.Context, hook string, payload []byte) [][]byte {
	h.mu.RLock()
	registered := h.hooks[hook]
	h.mu.RUnlock()
	results := make([][]byte, 0, len(registered))
	for _, registration := range registered {
		out, err := runHook(ctx, hook, payload, registration)
		if err != nil {
			h.logger.Error("extension hook failed", "hook", hook, "extension", registration.slug, "reason", err.Error())
			continue
		}
		results = append(results, out)
	}
	return results
}

func runHook(ctx context.Context, hook string, payload []byte, registration hookRegistration) (out []byte, err error) {
	bounded, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			out, err = nil, errors.New("handler panic")
		}
	}()
	out, err = registration.handler(bounded, hook, payload)
	if err == nil && len(out) > hookResultLimit {
		out, err = nil, errors.New("result exceeds limit")
	}
	return out, err
}

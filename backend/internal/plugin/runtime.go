package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// hostModule is the import namespace of the versioned host functions.
const hostModule = "modawana_v1"

// Per-invocation limits. A module is instantiated fresh for every invocation:
// no state survives between calls, and one stuck call cannot block another.
// invokeTimeout is a variable only so the limit tests run in milliseconds.
var invokeTimeout = 5 * time.Second

const memoryLimitPages = 1024 // 64 MiB linear memory.
const hookResultLimit = 64 << 10
const routeBodyLimit = 256 << 10
const stderrLimit = 8 << 10
const invocationGate = 2

// ErrInvoke reports a failed plugin invocation: trap, exit code, timeout,
// memory exhaustion, oversized result, or undeclared host-function use.
var ErrInvoke = errors.New("plugin invocation failed")

// Runtime owns the wazero engine, the WASI preview 1 shim, and the host
// functions. Modules run with no preopened directories, no sockets, and no
// environment; the only effects reach the host through modawana_v1 imports.
type Runtime struct {
	wazero      wazero.Runtime
	storage     storage
	keySource   func() ([]byte, error)
	fetchClient *http.Client
}

type invocation struct {
	pluginID                        string
	manifest                        Manifest
	externalAccess                  bool
	secretAccess                    bool
	store                           storage
	fetchCount                      int
	pendingRequest, pendingResponse []byte
	mu                              sync.Mutex
	violation                       string
}

type invocationKey struct{}

func current(ctx context.Context) *invocation {
	inv, _ := ctx.Value(invocationKey{}).(*invocation)
	return inv
}

// NewRuntime builds the shared engine. Close releases its resources.
func NewRuntime(ctx context.Context, database *sql.DB) (*Runtime, error) {
	config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(memoryLimitPages)
	r := &Runtime{wazero: wazero.NewRuntimeWithConfig(ctx, config), storage: storage{db: database}, fetchClient: newFetchClient()}
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r.wazero); err != nil {
		_ = r.wazero.Close(ctx)
		return nil, err
	}
	if err := r.instantiateHostModule(ctx); err != nil {
		_ = r.wazero.Close(ctx)
		return nil, err
	}
	return r, nil
}

// Close releases compiled modules and engine resources.
func (r *Runtime) Close(ctx context.Context) error { return r.wazero.Close(ctx) }

// Compile validates and compiles one module. It refuses imports outside WASI
// preview 1 and the versioned host module, and refuses host functions whose
// capability the manifest does not declare — undeclared use never loads.
func (r *Runtime) Compile(ctx context.Context, m Manifest, module []byte) (wazero.CompiledModule, error) {
	compiled, err := r.wazero.CompileModule(ctx, module)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvoke, "module rejected")
	}
	for _, imported := range compiled.ImportedFunctions() {
		moduleName, name, _ := imported.Import()
		if moduleName == wasi_snapshot_preview1.ModuleName {
			continue
		}
		if moduleName != hostModule {
			_ = compiled.Close(ctx)
			return nil, fmt.Errorf("%w: import %q/%q is not a host function", ErrInvoke, moduleName, name)
		}
		if capability := hostFunctionCapability[name]; capability == "" || !m.Capability(capability) {
			_ = compiled.Close(ctx)
			return nil, fmt.Errorf("%w: host function %q needs an undeclared capability", ErrInvoke, name)
		}
	}
	return compiled, nil
}

// invoke instantiates the compiled command module once, feeds the envelope on
// stdin, and returns the bounded stdout bytes. The module sees argv[0] only:
// no environment, no filesystem preopens, and a hard deadline that closes the
// module when it elapses.
func (r *Runtime) invoke(ctx context.Context, p *loaded, input []byte, limit int, external bool, tx *sql.Tx) ([]byte, error) {
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: canceled", ErrInvoke)
	}
	if tx == nil && !p.enabled.Load() {
		return nil, ErrInvoke
	}
	inv := &invocation{pluginID: p.installID, manifest: p.manifest, externalAccess: external, secretAccess: external, store: storage{db: r.storage.db, tx: tx}}
	bounded, cancel := context.WithTimeout(context.WithValue(ctx, invocationKey{}, inv), invokeTimeout)
	defer cancel()
	stdout := &boundedBuffer{limit: limit}
	stderr := &boundedBuffer{limit: stderrLimit}
	config := wazero.NewModuleConfig().WithName("").
		WithStdin(bytes.NewReader(input)).WithStdout(stdout).WithStderr(stderr).
		WithArgs(p.manifest.Slug).WithRandSource(rand.Reader).WithSysNanotime().WithSysWalltime()
	module, err := r.wazero.InstantiateModule(bounded, p.compiled, config)
	if module != nil {
		_ = module.Close(ctx)
	}
	var exit *sys.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 0 {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvoke, invokeReason(bounded, err))
	}
	if inv.violation != "" {
		return nil, fmt.Errorf("%w: %s", ErrInvoke, inv.violation)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("%w: result exceeds %d bytes", ErrInvoke, limit)
	}
	return stdout.data, nil
}

func invokeReason(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "time limit exceeded"
	}
	var exit *sys.ExitError
	if errors.As(err, &exit) {
		return fmt.Sprintf("exit code %d", exit.ExitCode())
	}
	return "module trap"
}

type hookEnvelope struct {
	API         int                        `json:"api"`
	Kind        string                     `json:"kind"`
	Hook        string                     `json:"hook"`
	HookVersion int                        `json:"hook_version"`
	Payload     json.RawMessage            `json:"payload"`
	Settings    map[string]json.RawMessage `json:"settings,omitempty"`
}

// InvokeHook exchanges the same bounded JSON bytes the stage 0 registries use:
// the payload passes through unchanged and stdout is the hook result.
func (r *Runtime) InvokeHook(ctx context.Context, p *loaded, hook string, version int, payload []byte) ([]byte, error) {
	settings, err := r.invocationSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	input, err := json.Marshal(hookEnvelope{Settings: settings, API: p.manifest.HostAPIVersion, Kind: "hook", Hook: hook, HookVersion: version, Payload: payload})
	if err != nil {
		return nil, err
	}
	return r.invoke(ctx, p, input, hookResultLimit, false, nil)
}

type routeEnvelope struct {
	API      int                        `json:"api"`
	Kind     string                     `json:"kind"`
	Method   string                     `json:"method"`
	Path     string                     `json:"path"`
	Query    map[string][]string        `json:"query,omitempty"`
	Owner    bool                       `json:"owner"`
	Body     string                     `json:"body,omitempty"`
	Settings map[string]json.RawMessage `json:"settings,omitempty"`
}

// RouteResult is a plugin route response after host validation. The host
// writes the response itself and allows only safe inline content types.
type RouteResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

var routeContentTypes = map[string]bool{"application/json": true, "text/plain": true}

// InvokeRoute runs one registered route handler and validates its response.
func (r *Runtime) InvokeRoute(ctx context.Context, p *loaded, envelope routeEnvelope) (RouteResult, error) {
	envelope.API = p.manifest.HostAPIVersion
	envelope.Kind = "route"
	var err error
	envelope.Settings, err = r.invocationSettings(ctx, p)
	if err != nil {
		return RouteResult{}, err
	}
	input, err := json.Marshal(envelope)
	if err != nil {
		return RouteResult{}, err
	}
	privateWrite := envelope.Owner && (envelope.Method == "POST" || envelope.Method == "PUT" || envelope.Method == "PATCH" || envelope.Method == "DELETE")
	out, err := r.invoke(ctx, p, input, routeBodyLimit+(4<<10), privateWrite, nil)
	if err != nil {
		return RouteResult{}, err
	}
	var result RouteResult
	decoder := json.NewDecoder(bytes.NewReader(out))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return RouteResult{}, fmt.Errorf("%w: malformed route result", ErrInvoke)
	}
	if result.Status < 200 || result.Status > 599 || !routeContentTypes[result.ContentType] || len(result.Body) > routeBodyLimit {
		return RouteResult{}, fmt.Errorf("%w: route result rejected", ErrInvoke)
	}
	return result, nil
}

type boundedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

// Write accepts every byte so the module keeps running, keeps the first limit
// bytes, and remembers the overflow: the host refuses the oversized result
// after the run instead of surfacing an I/O error the module could mask.
func (b *boundedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if remaining := b.limit - len(b.data); written > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return written, nil
}

// hostFunctionCapability gates every host function on a manifest capability.
var hostFunctionCapability = map[string]string{
	"kv_get": "storage", "kv_set": "storage", "kv_delete": "storage",
	"doc_get": "storage", "doc_put": "storage", "doc_delete": "storage",
	"secret_get": "secrets", "http_fetch": "http_fetch",
}

// Result codes shared with plugins. Non-negative values are lengths.
const (
	codeOK       = 0
	codeNotFound = -1
	codeDenied   = -2
	codeInvalid  = -3
	codeInternal = -4
	codeQuota    = -5
)

func storageCode(err error) int32 {
	switch {
	case err == nil:
		return codeOK
	case errors.Is(err, ErrNotFound):
		return codeNotFound
	case errors.Is(err, ErrStorageInvalid):
		return codeInvalid
	case errors.Is(err, ErrQuota):
		return codeQuota
	default:
		return codeInternal
	}
}

// deny records undeclared host-function use. The call returns codeDenied and
// the whole invocation is refused afterwards, so a plugin cannot probe its
// way past the owner-approved capability set.
func deny(inv *invocation, name string) int32 {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if inv.violation == "" {
		inv.violation = "undeclared use of host function " + name
	}
	return codeDenied
}

func guarded(ctx context.Context, name string) (*invocation, bool) {
	inv := current(ctx)
	if inv == nil {
		return nil, false
	}
	if !inv.manifest.Capability(hostFunctionCapability[name]) {
		deny(inv, name)
		return inv, false
	}
	return inv, true
}

func readMemory(m api.Module, ptr, length uint32) ([]byte, bool) {
	if length == 0 {
		return []byte{}, true
	}
	return m.Memory().Read(ptr, length)
}

// respond writes value into the guest buffer when it fits and returns the
// value's length either way; a short buffer tells the guest to retry larger.
func respond(m api.Module, bufPtr, bufCap uint32, value []byte) int32 {
	if len(value) > int(bufCap) {
		return int32(len(value))
	}
	if len(value) > 0 && !m.Memory().Write(bufPtr, value) {
		return codeInvalid
	}
	return int32(len(value))
}

func (r *Runtime) instantiateHostModule(ctx context.Context) error {
	builder := r.wazero.NewHostModuleBuilder(hostModule)
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, keyPtr, keyLen, bufPtr, bufCap uint32) int32 {
		inv, ok := guarded(ctx, "kv_get")
		if !ok {
			return codeDenied
		}
		key, ok := readMemory(m, keyPtr, keyLen)
		if !ok {
			return codeInvalid
		}
		value, err := inv.store.kvGet(ctx, inv.pluginID, string(key))
		if err != nil {
			return storageCode(err)
		}
		return respond(m, bufPtr, bufCap, value)
	}).Export("kv_get")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, keyPtr, keyLen, valuePtr, valueLen uint32) int32 {
		inv, ok := guarded(ctx, "kv_set")
		if !ok {
			return codeDenied
		}
		key, okKey := readMemory(m, keyPtr, keyLen)
		value, okValue := readMemory(m, valuePtr, valueLen)
		if !okKey || !okValue {
			return codeInvalid
		}
		return storageCode(inv.store.kvSet(ctx, inv.pluginID, string(key), value))
	}).Export("kv_set")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, keyPtr, keyLen uint32) int32 {
		inv, ok := guarded(ctx, "kv_delete")
		if !ok {
			return codeDenied
		}
		key, ok := readMemory(m, keyPtr, keyLen)
		if !ok {
			return codeInvalid
		}
		return storageCode(inv.store.kvDelete(ctx, inv.pluginID, string(key)))
	}).Export("kv_delete")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, colPtr, colLen, idPtr, idLen, bufPtr, bufCap uint32) int32 {
		inv, ok := guarded(ctx, "doc_get")
		if !ok {
			return codeDenied
		}
		collection, okCol := readMemory(m, colPtr, colLen)
		id, okID := readMemory(m, idPtr, idLen)
		if !okCol || !okID {
			return codeInvalid
		}
		body, err := inv.store.docGet(ctx, inv.pluginID, string(collection), string(id))
		if err != nil {
			return storageCode(err)
		}
		return respond(m, bufPtr, bufCap, body)
	}).Export("doc_get")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, colPtr, colLen, idPtr, idLen, bodyPtr, bodyLen uint32) int32 {
		inv, ok := guarded(ctx, "doc_put")
		if !ok {
			return codeDenied
		}
		collection, okCol := readMemory(m, colPtr, colLen)
		id, okID := readMemory(m, idPtr, idLen)
		body, okBody := readMemory(m, bodyPtr, bodyLen)
		if !okCol || !okID || !okBody {
			return codeInvalid
		}
		return storageCode(inv.store.docPut(ctx, inv.pluginID, string(collection), string(id), body))
	}).Export("doc_put")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, colPtr, colLen, idPtr, idLen uint32) int32 {
		inv, ok := guarded(ctx, "doc_delete")
		if !ok {
			return codeDenied
		}
		collection, okCol := readMemory(m, colPtr, colLen)
		id, okID := readMemory(m, idPtr, idLen)
		if !okCol || !okID {
			return codeInvalid
		}
		return storageCode(inv.store.docDelete(ctx, inv.pluginID, string(collection), string(id)))
	}).Export("doc_delete")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, namePtr, nameLen, bufPtr, bufCap uint32) int32 {
		inv, ok := guarded(ctx, "secret_get")
		if !ok || !inv.secretAccess {
			return codeDenied
		}
		if nameLen > 48 {
			return codeInvalid
		}
		name, ok := readMemory(m, namePtr, nameLen)
		if !ok {
			return codeInvalid
		}
		value, err := r.readSecret(ctx, inv, string(name))
		if err != nil {
			return storageCode(err)
		}
		return respond(m, bufPtr, bufCap, value)
	}).Export("secret_get")
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, reqPtr, reqLen, bufPtr, bufCap uint32) int32 {
		inv, ok := guarded(ctx, "http_fetch")
		if !ok || !inv.externalAccess {
			return codeDenied
		}
		if reqLen > fetchInputLimit {
			return codeInvalid
		}
		raw, ok := readMemory(m, reqPtr, reqLen)
		if !ok {
			return codeInvalid
		}
		// A retry with a larger buffer retrieves the first response instead of
		// issuing a duplicate outbound request.
		if inv.pendingRequest != nil {
			if !bytes.Equal(raw, inv.pendingRequest) {
				return codeInvalid
			}
		} else {
			if inv.fetchCount >= 4 {
				return codeQuota
			}
			inv.fetchCount++
			out, err := r.fetch(ctx, inv, raw)
			if err != nil {
				return storageCode(err)
			}
			inv.pendingRequest, inv.pendingResponse = bytes.Clone(raw), out
		}
		n := respond(m, bufPtr, bufCap, inv.pendingResponse)
		if n >= 0 && uint32(n) <= bufCap {
			inv.pendingRequest, inv.pendingResponse = nil, nil
		}
		return n
	}).Export("http_fetch")
	_, err := builder.Instantiate(ctx)
	return err
}

func (r *Runtime) invocationSettings(ctx context.Context, p *loaded) (map[string]json.RawMessage, error) {
	if !p.manifest.Capability("settings") {
		return nil, nil
	}
	return readPluginSettings(ctx, r.storage.db, p.installID, p.manifest)
}

func (r *Runtime) invokeTask(ctx context.Context, p *loaded, kind, name string, tx *sql.Tx) error {
	var settings map[string]json.RawMessage
	if tx == nil {
		var err error
		settings, err = r.invocationSettings(ctx, p)
		if err != nil {
			return ErrInvoke
		}
	}
	input, err := json.Marshal(struct {
		API      int                        `json:"api"`
		Kind     string                     `json:"kind"`
		Name     string                     `json:"name"`
		Settings map[string]json.RawMessage `json:"settings,omitempty"`
	}{p.manifest.HostAPIVersion, kind, name, settings})
	if err != nil {
		return ErrInvoke
	}
	_, err = r.invoke(ctx, p, input, hookResultLimit, kind == "job", tx)
	return err
}

//go:build wasip1

// The first-party example plugin: a WASI command module the integration tests
// compile with GOOS=wasip1 GOARCH=wasm (node scripts/build-example-plugin.mjs
// builds it standalone). Each invocation reads one JSON envelope from stdin
// and writes the result to stdout; effects go through the modawana_v1 host
// functions imported below. The test.* hooks exercise sandboxing, limits, and
// storage on purpose — a real plugin only implements its declared contracts.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"unsafe"
)

//go:wasmimport modawana_v1 kv_get
func kvGet(keyPtr, keyLen, bufPtr, bufCap uint32) int32

//go:wasmimport modawana_v1 kv_set
func kvSet(keyPtr, keyLen, valuePtr, valueLen uint32) int32

//go:wasmimport modawana_v1 kv_delete
func kvDelete(keyPtr, keyLen uint32) int32

//go:wasmimport modawana_v1 doc_get
func docGet(colPtr, colLen, idPtr, idLen, bufPtr, bufCap uint32) int32

//go:wasmimport modawana_v1 doc_put
func docPut(colPtr, colLen, idPtr, idLen, bodyPtr, bodyLen uint32) int32

//go:wasmimport modawana_v1 doc_delete
func docDelete(colPtr, colLen, idPtr, idLen uint32) int32

func view(b []byte) (uint32, uint32) {
	if len(b) == 0 {
		return 0, 0
	}
	return uint32(uintptr(unsafe.Pointer(unsafe.SliceData(b)))), uint32(len(b))
}

type envelope struct {
	API         int                 `json:"api"`
	Kind        string              `json:"kind"`
	Hook        string              `json:"hook"`
	HookVersion int                 `json:"hook_version"`
	Payload     json.RawMessage     `json:"payload"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Query       map[string][]string `json:"query"`
	Owner       bool                `json:"owner"`
	Body        string              `json:"body"`
}

type echoResult struct {
	Hook    string          `json:"hook"`
	Version int             `json:"version"`
	Echo    json.RawMessage `json:"echo"`
}

type storageRequest struct {
	Op         string `json:"op"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	ValueSize  int    `json:"value_size"`
	Fill       int    `json:"fill"`
	Collection string `json:"collection"`
	ID         string `json:"id"`
	Body       string `json:"body"`
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	var in envelope
	if err = json.Unmarshal(raw, &in); err != nil {
		os.Exit(1)
	}
	switch {
	case in.Kind == "route":
		emit(map[string]any{"status": 200, "content_type": "application/json",
			"body": mustJSON(map[string]any{"plugin": "example", "path": in.Path, "owner": in.Owner, "query": in.Query, "body_len": len(in.Body)})})
	case in.Hook == "test.echo":
		emit(echoResult{Hook: in.Hook, Version: in.HookVersion, Echo: in.Payload})
	case in.Hook == "test.sandbox":
		emit(probeSandbox())
	case in.Hook == "test.storage":
		var request storageRequest
		if json.Unmarshal(in.Payload, &request) != nil {
			os.Exit(1)
		}
		emit(runStorage(request))
	case in.Hook == "test.spin":
		for {
		}
	case in.Hook == "test.memory":
		var sink [][]byte
		for i := 0; i < 4096; i++ {
			sink = append(sink, bytes.Repeat([]byte{byte(i)}, 1<<20))
		}
		emit(map[string]int{"allocated": len(sink)})
	case in.Hook == "test.big":
		if _, err := os.Stdout.Write(bytes.Repeat([]byte{'x'}, 80<<10)); err != nil {
			os.Exit(1)
		}
	default:
		os.Exit(1)
	}
}

func emit(v any) {
	out, err := json.Marshal(v)
	if err != nil {
		os.Exit(1)
	}
	if _, err = os.Stdout.Write(out); err != nil {
		os.Exit(1)
	}
}

func mustJSON(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		os.Exit(1)
	}
	return string(out)
}

// probeSandbox records what the module can observe: with no preopens, no
// sockets, and no environment every probe must fail or come back empty.
func probeSandbox() map[string]any {
	out := map[string]any{"env_count": len(os.Environ()), "args": len(os.Args)}
	if _, err := os.ReadFile("/etc/hosts"); err != nil {
		out["file_error"] = true
	}
	if _, err := os.ReadDir("/"); err != nil {
		out["dir_error"] = true
	}
	if _, err := net.Dial("tcp", "127.0.0.1:80"); err != nil {
		out["net_error"] = true
	}
	if _, err := os.Create("escape.txt"); err != nil {
		out["write_error"] = true
	}
	return out
}

func runStorage(request storageRequest) map[string]any {
	value := []byte(request.Value)
	if request.ValueSize > 0 {
		value = bytes.Repeat([]byte{'v'}, request.ValueSize)
	}
	switch request.Op {
	case "kv_set":
		keyPtr, keyLen := view([]byte(request.Key))
		valuePtr, valueLen := view(value)
		return map[string]any{"code": kvSet(keyPtr, keyLen, valuePtr, valueLen)}
	case "kv_get":
		buf := make([]byte, 64<<10)
		keyPtr, keyLen := view([]byte(request.Key))
		bufPtr, bufCap := view(buf)
		code := kvGet(keyPtr, keyLen, bufPtr, bufCap)
		if code < 0 {
			return map[string]any{"code": code}
		}
		return map[string]any{"code": 0, "value": string(buf[:code])}
	case "kv_delete":
		keyPtr, keyLen := view([]byte(request.Key))
		return map[string]any{"code": kvDelete(keyPtr, keyLen)}
	case "kv_fill":
		// Write keys until the host refuses one, reporting the refusal code.
		for i := 0; i < request.Fill; i++ {
			keyPtr, keyLen := view(fmt.Appendf(nil, "fill-%04d", i))
			valuePtr, valueLen := view(value)
			if code := kvSet(keyPtr, keyLen, valuePtr, valueLen); code != 0 {
				return map[string]any{"code": code, "written": i}
			}
		}
		return map[string]any{"code": 0, "written": request.Fill}
	case "doc_put":
		colPtr, colLen := view([]byte(request.Collection))
		idPtr, idLen := view([]byte(request.ID))
		bodyPtr, bodyLen := view([]byte(request.Body))
		return map[string]any{"code": docPut(colPtr, colLen, idPtr, idLen, bodyPtr, bodyLen)}
	case "doc_get":
		buf := make([]byte, 256<<10)
		colPtr, colLen := view([]byte(request.Collection))
		idPtr, idLen := view([]byte(request.ID))
		bufPtr, bufCap := view(buf)
		code := docGet(colPtr, colLen, idPtr, idLen, bufPtr, bufCap)
		if code < 0 {
			return map[string]any{"code": code}
		}
		return map[string]any{"code": 0, "value": string(buf[:code])}
	case "doc_delete":
		colPtr, colLen := view([]byte(request.Collection))
		idPtr, idLen := view([]byte(request.ID))
		return map[string]any{"code": docDelete(colPtr, colLen, idPtr, idLen)}
	}
	return map[string]any{"code": -100}
}

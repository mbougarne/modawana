//go:build wasip1

package main

import (
	"encoding/json"
	"os"
	"unsafe"
)

//go:wasmimport modawana_v1 kv_set
func kvSet(keyPtr, keyLen, valuePtr, valueLen uint32) int32

//go:wasmimport modawana_v1 secret_get
func secretGet(namePtr, nameLen, bufPtr, bufCap uint32) int32

//go:wasmimport modawana_v1 http_fetch
func httpFetch(reqPtr, reqLen, bufPtr, bufCap uint32) int32

func address(b []byte) uint32 { return uint32(uintptr(unsafe.Pointer(unsafe.SliceData(b)))) }

func put(key string, value []byte) {
	k := []byte(key)
	if kvSet(address(k), uint32(len(k)), address(value), uint32(len(value))) != 0 {
		os.Exit(2)
	}
}

func main() {
	var in struct {
		Kind     string                     `json:"kind"`
		Name     string                     `json:"name"`
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if json.NewDecoder(os.Stdin).Decode(&in) != nil {
		os.Exit(2)
	}
	switch in.Kind {
	case "migration":
		put(in.Name, []byte("migration ran"))
		if in.Name == "fail" {
			os.Exit(2)
		}
	case "job":
		put("job", in.Settings["message"])
	case "mcp":
		key, value := []byte("mcp"), []byte("write")
		_ = kvSet(address(key), uint32(len(key)), address(value), uint32(len(value)))
		_, _ = os.Stdout.WriteString("tool returned")
	case "route":
		name, secret := []byte("token"), make([]byte, 64)
		n := secretGet(address(name), uint32(len(name)), address(secret), uint32(len(secret)))
		request := []byte(`{"url":"https://api.example.com/v1","method":"POST","body":"e30="}`)
		result := make([]byte, 4096)
		size := httpFetch(address(request), uint32(len(request)), 0, 0)
		if size > 0 {
			size = httpFetch(address(request), uint32(len(request)), address(result), uint32(len(result)))
		}
		body, _ := json.Marshal(map[string]any{"secret_size": n, "response_size": size})
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": 200, "content_type": "application/json", "body": string(body)})
	default:
		os.Exit(2)
	}
}

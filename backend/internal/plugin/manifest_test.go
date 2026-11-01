package plugin

import (
	"strings"
	"testing"
)

const validManifest = `{
 "schema_version": 1, "host_api_version": 1, "slug": "example",
 "name": "Example", "version": "0.1.0", "module": "plugin.wasm",
 "capabilities": ["hooks", "routes", "storage"],
 "hooks": [{"name": "test.echo", "version": 1}],
 "routes": [{"method": "GET", "path": "/hello", "public": true}]
}`

func TestManifestStrictValidation(t *testing.T) {
	if _, err := ParseManifest([]byte(validManifest), "example"); err != nil {
		t.Fatal(err)
	}
	reject := func(label, from, to string) {
		t.Helper()
		mutated := strings.Replace(validManifest, from, to, 1)
		if mutated == validManifest {
			t.Fatal(label, "mutation did not apply")
		}
		if _, err := ParseManifest([]byte(mutated), "example"); err == nil {
			t.Fatal(label, "accepted")
		}
	}
	reject("unknown field", `"slug"`, `"surprise": true, "slug"`)
	reject("newer host API", `"host_api_version": 1`, `"host_api_version": 2`)
	reject("unknown schema", `"schema_version": 1`, `"schema_version": 7`)
	reject("unsupported capability", `"storage"]`, `"secrets"]`)
	reject("duplicate capability", `["hooks"`, `["storage", "hooks"`)
	reject("hooks without capability", `"hooks", `, ``)
	reject("routes without capability", `"routes", `, ``)
	reject("undeclared hook list", `"hooks": [{"name": "test.echo", "version": 1}],`, `"hooks": [],`)
	reject("bad hook name", `test.echo`, `TestEcho`)
	reject("bad hook version", `"version": 1}`, `"version": 0}`)
	reject("reserved route", `/hello`, `/install`)
	reject("public write route", `"method": "GET", "path": "/hello"`, `"method": "POST", "path": "/hello"`)
	reject("bad route method", `"GET"`, `"TRACE"`)
	reject("bad route path", `/hello`, `hello`)
	reject("bad module name", `plugin.wasm`, `../plugin.wasm`)
	reject("empty name", `Example`, ``)
	reject("bad version", `0.1.0`, `0.1 beta`)
	if _, err := ParseManifest([]byte(validManifest), "other"); err == nil {
		t.Fatal("slug/directory mismatch accepted")
	}
}

const hooksOnlyManifest = `{
 "schema_version": 1, "host_api_version": 1, "slug": "example",
 "name": "Example", "version": "0.1.0", "module": "plugin.wasm",
 "capabilities": ["hooks"],
 "hooks": [{"name": "test.echo", "version": 1}]
}`

func TestManifestCapabilityDiff(t *testing.T) {
	approved, err := ParseManifest([]byte(hooksOnlyManifest), "example")
	if err != nil {
		t.Fatal(err)
	}
	next, err := ParseManifest([]byte(validManifest), "example")
	if err != nil {
		t.Fatal(err)
	}
	added := AddedCapabilities(approved, next)
	if len(added) != 2 || added[0] != "routes" || added[1] != "storage" {
		t.Fatal("added capabilities", added)
	}
	if len(AddedCapabilities(next, approved)) != 0 {
		t.Fatal("downgrade reported additions")
	}
}

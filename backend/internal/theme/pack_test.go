package theme

import (
	"encoding/json"
	"strings"
	"testing"
)

const standardLayout = `{{template "header" .}}{{template "content" .}}{{template "footer" .}}`

func TestPackValidation(t *testing.T) {
	valid := manifest{Version: 1, Name: "Editorial", Layout: standardLayout, CSS: ".vb-shell, .vb-title h1 { max-inline-size: 70rem; color: var(--ink); }"}
	encode := func(m manifest) []byte {
		t.Helper()
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	pack, err := parsePack("pack-editorial", encode(valid))
	if err != nil || strings.Join(pack.Layout, ",") != "header,content,footer" ||
		pack.CSS != "[data-public-theme='pack-editorial'] .vb-shell,[data-public-theme='pack-editorial'] .vb-title h1{max-inline-size:70rem;color:var(--ink);}\n" {
		t.Fatalf("valid pack: %+v %v", pack.Appearance, err)
	}
	for name, edit := range map[string]func(*manifest){
		"version":            func(m *manifest) { m.Version++ },
		"duplicate slot":     func(m *manifest) { m.Layout += `{{template "header" .}}` },
		"missing footer":     func(m *manifest) { m.Layout = strings.ReplaceAll(m.Layout, `{{template "footer" .}}`, "") },
		"unavailable slot":   func(m *manifest) { m.Layout += `{{template "banner" .}}` },
		"expression":         func(m *manifest) { m.Layout += `{{.Title}}` },
		"markup":             func(m *manifest) { m.Layout += "<p>Literal HTML</p>" },
		"arbitrary slot":     func(m *manifest) { m.Layout += `{{template "article" .}}` },
		"remote CSS":         func(m *manifest) { m.CSS = `@import "theme.css";` },
		"external resource":  func(m *manifest) { m.CSS = `.vb-title {color: url(theme.png);}` },
		"attribute selector": func(m *manifest) { m.CSS = `a[href] {color: red;}` },
		"markup in CSS":      func(m *manifest) { m.CSS += "</style>" },
		"style escape":       func(m *manifest) { m.CSS = `.vb-title {color: r\65 d;}` },
		"invalid image":      func(m *manifest) { m.Banner = []byte("not a PNG") },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			edit(&candidate)
			if _, err := parsePack("pack-editorial", encode(candidate)); err != ErrPack {
				t.Fatal("expected invalid pack", err)
			}
		})
	}
}

package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func TestRenderedMetaHookMatchesBuiltinSelection(t *testing.T) {
	const origin = "https://blog.example"
	s := New(Options{Config: config.Config{BaseURL: origin}}).(*Server)
	settings := siteconfig.Public{Site: siteconfig.Site{LogoMode: "image", LogoID: "logo"}, Profile: siteconfig.Profile{BannerEnabled: true, BannerID: "banner", PortraitID: "portrait"}, Effective: map[string]siteconfig.Effective{"name": {Text: "Name"}, "banner_alt": {Text: "Banner"}}}
	page := content.PageRoute{Kind: "article", Content: &content.PublicContent{CoverID: "cover", Media: []content.PublicMedia{{ID: "cover", Kind: "image", MIME: "image/png", Width: 1200, Height: 630}}}}
	compare := func(label string) {
		t.Helper()
		got := s.renderedMeta(context.Background(), page, settings, origin, "Title")
		want := socialFor(page, settings, origin, "Title")
		if got != want {
			t.Fatal(label, got, want)
		}
	}
	compare("article cover")
	page.Kind = "lesson"
	compare("lesson")
	page.Kind = "article"
	page.Content.Locked = true
	compare("locked")
	page.Content.Locked = false
	page.Content.Media = nil
	compare("unlisted cover")
	settings.Profile.BannerEnabled = false
	compare("portrait")
	settings.Profile.PortraitID = ""
	compare("logo")
	settings.Site.LogoMode = "wordmark"
	compare("default card")
}

type hostileMeta struct{ result string }

func (hostileMeta) Slug() string { return "hostile-meta" }

func (x hostileMeta) Register(r *extension.Registrar) error {
	return r.Hook(extension.HookRenderedMeta, 1, func(context.Context, string, []byte) ([]byte, error) {
		return []byte(x.result), nil
	})
}

func TestRenderedMetaSanitizesHookResults(t *testing.T) {
	const origin = "https://blog.example"
	settings := siteconfig.Public{Effective: map[string]siteconfig.Effective{"name": {Text: "Name"}}}
	serve := func(result string) socialMetadata {
		host := extension.NewHost(nil)
		s := New(Options{Config: config.Config{BaseURL: origin}, Extensions: host}).(*Server)
		// Install the built-in first so the hostile handler's result arrives last.
		s.extensions()
		if err := host.Install(hostileMeta{result: result}); err != nil {
			t.Fatal(err)
		}
		return s.renderedMeta(context.Background(), content.PageRoute{Kind: "home"}, settings, origin, "Title")
	}
	hostile, _ := json.Marshal(map[string]any{"image": "https://evil.example/x.png", "alt": "line\nbreak", "type": "script", "site_name": strings.Repeat("x", 400), "width": 5, "height": 5})
	got := serve(string(hostile))
	want := socialMetadata{Image: origin + "/social-card.png", Alt: "Title", Type: "website", SiteName: "Name", Width: 1200, Height: 630}
	if got != want {
		t.Fatal("hostile result not neutralized", got)
	}
	quoted, _ := json.Marshal(map[string]any{"image": origin + `/media/x" onerror="x`, "type": "article"})
	if got := serve(string(quoted)); got.Image != origin+"/social-card.png" {
		t.Fatal("unescaped image survived", got)
	}
	allowed, _ := json.Marshal(map[string]any{"image": origin + "/media/good", "alt": "Good", "type": "article", "site_name": "Site", "width": 640, "height": 480})
	got = serve(string(allowed))
	want = socialMetadata{Image: origin + "/media/good", Alt: "Good", Type: "article", SiteName: "Site", Width: 640, Height: 480}
	if got != want {
		t.Fatal("well-formed result rejected", got)
	}
}

type echoTool struct{}

func (echoTool) Slug() string { return "echo" }

func (echoTool) Register(r *extension.Registrar) error {
	return r.MCPTool(extension.MCPTool{Name: "echo_tool", Description: "Echo.", Properties: map[string]any{"value": map[string]string{"type": "string"}}, Required: []string{"value"}, ReadOnly: true, Idempotent: true, Call: func(ctx context.Context, raw json.RawMessage) ([]byte, error) {
		return json.Marshal(map[string]any{"content": []any{map[string]string{"type": "text", "text": string(raw)}}, "isError": false})
	}})
}

func TestMCPToolListAndDispatchAreRegistryDriven(t *testing.T) {
	host := extension.NewHost(nil)
	d := &Domain{Extensions: host}
	names := []string{}
	for _, definition := range d.mcpTools() {
		names = append(names, definition.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "get_draft,save_draft,publish_revision,upload_media" {
		t.Fatal("built-in tool list changed", names)
	}
	first := d.mcpTools()[0].(map[string]any)
	if schema := first["inputSchema"].(map[string]any); schema["additionalProperties"] != false {
		t.Fatal(schema)
	}
	if annotations := first["annotations"].(map[string]bool); !annotations["readOnlyHint"] || annotations["destructiveHint"] || !annotations["idempotentHint"] {
		t.Fatal(annotations)
	}
	if err := host.Install(echoTool{}); err != nil {
		t.Fatal(err)
	}
	if got := len(d.mcpTools()); got != 5 {
		t.Fatal("registered tool not advertised", got)
	}
	r := httptest.NewRequest("POST", mcpPath, nil)
	result, err := d.mcpCall(r, "echo_tool", json.RawMessage(`{"value":"x"}`))
	if err != nil || result["isError"] != false {
		t.Fatal(result, err)
	}
	for label, arguments := range map[string]string{"unknown field": `{"value":"x","other":1}`, "missing required": `{}`, "null required": `{"value":null}`} {
		if _, err := d.mcpCall(r, "echo_tool", json.RawMessage(arguments)); err == nil {
			t.Fatal(label, "accepted")
		}
	}
	if _, err := d.mcpCall(r, "missing_tool", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown tool dispatched")
	}
}

func TestRenderedMetaKeepsLongArabicAltText(t *testing.T) {
	const origin = "https://blog.example"
	// 200 Arabic runes are twice as many bytes; the bound must count runes
	// like the site configuration limits do.
	alt := strings.Repeat("ع", 200)
	settings := siteconfig.Public{Profile: siteconfig.Profile{BannerEnabled: true, BannerID: "banner"}, Effective: map[string]siteconfig.Effective{"banner_alt": {Text: alt}}}
	s := New(Options{Config: config.Config{BaseURL: origin}}).(*Server)
	got := s.renderedMeta(context.Background(), content.PageRoute{Kind: "home"}, settings, origin, "Title")
	if got.Alt != alt || got.Image != origin+"/media/banner" {
		t.Fatal("Arabic alt text rejected", got.Image, len(got.Alt))
	}
}

func TestEagerBuiltinInstallKeepsLazyPathsIdempotent(t *testing.T) {
	host := extension.NewHost(nil)
	d := &Domain{Extensions: host}
	for _, builtin := range d.Builtins() {
		if err := host.Install(builtin); err != nil {
			t.Fatal(err)
		}
	}
	s := New(Options{Config: config.Config{BaseURL: "https://blog.example"}, Extensions: host}).(*Server)
	if s.extensions() != host || d.extensions() != host {
		t.Fatal("lazy paths replaced the shared host")
	}
	if got := len(d.mcpTools()); got != 4 {
		t.Fatal("built-in tools duplicated or missing", got)
	}
	if got := len(host.DispatchHook(context.Background(), extension.HookRenderedMeta, []byte(`{}`))); got != 1 {
		t.Fatal("hook registrations duplicated or missing", got)
	}
}

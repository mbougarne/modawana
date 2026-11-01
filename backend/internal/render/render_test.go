package render

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites the committed fixture goldens:
//
//	go test ./internal/render -run TestFixtures -update
//
// After updating, run `node scripts/markdown-parity.mjs` to prove the goldens
// still match the browser pipeline.
var update = flag.Bool("update", false, "rewrite fixture goldens")

type fixtureConfig struct {
	Locale   string `json:"locale"`
	Takeaway string `json:"takeaway"`
	Media    []struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"media"`
	Presentations []struct {
		MediaID    string `json:"media_id"`
		PosterID   string `json:"poster_media_id"`
		TrackID    string `json:"caption_track_media_id"`
		Caption    string `json:"caption"`
		Transcript string `json:"transcript"`
	} `json:"presentations"`
}

// labelsFor mirrors the reader strings in ui/src/i18n/labels.ts; the parity
// harness renders through the real I18nProvider, so drift fails the suite.
func labelsFor(locale string) Labels {
	switch locale {
	case "ar":
		return Labels{
			Takeaway:         "الخلاصة",
			Table:            "جدول قابل للتمرير",
			MediaUnavailable: "الوسائط غير متاحة",
			Captions:         "الترجمة النصية",
			Transcript:       "النص المفرّغ",
		}
	case "fr":
		return Labels{
			Takeaway:         "À retenir",
			Table:            "Tableau défilant",
			MediaUnavailable: "Média indisponible",
			Captions:         "Sous-titres",
			Transcript:       "Transcription",
		}
	default:
		return Labels{
			Takeaway:         "Takeaway",
			Table:            "Scrollable table",
			MediaUnavailable: "Media unavailable",
			Captions:         "Captions",
			Transcript:       "Transcript",
		}
	}
}

func fixtureOptions(t *testing.T, path string) (fixtureConfig, Options) {
	t.Helper()
	config := fixtureConfig{Locale: "en"}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	opts := Options{Locale: config.Locale, Labels: labelsFor(config.Locale)}
	for _, m := range config.Media {
		opts.Media = append(opts.Media, Media{ID: m.ID, Kind: m.Kind, Width: m.Width, Height: m.Height})
	}
	for _, p := range config.Presentations {
		opts.Presentations = append(opts.Presentations, Presentation{
			MediaID: p.MediaID, PosterID: p.PosterID, TrackID: p.TrackID,
			Caption: p.Caption, Transcript: p.Transcript,
		})
	}
	return config, opts
}

func TestFixtures(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join("testdata", "fixtures", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no parity fixtures found")
	}
	for _, source := range sources {
		name := strings.TrimSuffix(filepath.Base(source), ".md")
		t.Run(name, func(t *testing.T) {
			markdown, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			config, opts := fixtureOptions(t, strings.TrimSuffix(source, ".md")+".json")
			result, err := Document(string(markdown), config.Takeaway, opts)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			golden := strings.TrimSuffix(source, ".md") + ".html"
			if *update {
				if err := os.WriteFile(golden, []byte(string(result.HTML)+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			expected, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden (run go test ./internal/render -update): %v", err)
			}
			if string(result.HTML)+"\n" != string(expected) {
				t.Errorf("output differs from %s\ngot:\n%s\nwant:\n%s", golden, result.HTML, expected)
			}
		})
	}
}

func TestRejectsRawHTML(t *testing.T) {
	for _, markdown := range []string{
		"before <script>alert(1)</script> after",
		"<div>\nblock\n</div>",
		"inline <em>html</em> tag",
		"an <iframe src=\"https://evil.test\"></iframe>",
	} {
		if _, err := Document(markdown, "", Options{Locale: "en"}); !errors.Is(err, ErrRawHTML) {
			t.Errorf("%q: expected ErrRawHTML, got %v", markdown, err)
		}
	}
	if _, err := Document("body", "<b>takeaway</b>", Options{Locale: "en"}); !errors.Is(err, ErrRawHTML) {
		t.Errorf("takeaway raw HTML: expected ErrRawHTML, got %v", err)
	}
}

func TestRejectsOversizeAndInvalidText(t *testing.T) {
	if _, err := Document(strings.Repeat("a", MaxBody+1), "", Options{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize: expected ErrTooLarge, got %v", err)
	}
	if _, err := Document("a\x00b", "", Options{}); !errors.Is(err, ErrInvalidText) {
		t.Errorf("NUL byte: expected ErrInvalidText, got %v", err)
	}
	if _, err := Document("a\xffb", "", Options{}); !errors.Is(err, ErrInvalidText) {
		t.Errorf("invalid UTF-8: expected ErrInvalidText, got %v", err)
	}
}

func TestHasMath(t *testing.T) {
	cases := []struct {
		markdown, takeaway string
		want               bool
	}{
		{"plain text", "", false},
		{"inline $x^2$ math", "", true},
		{"$$\nx^2\n$$", "", true},
		{"plain", "with $y$ math", true},
		{"escaped \\$5 and \\$6", "", false},
	}
	for _, c := range cases {
		result, err := Document(c.markdown, c.takeaway, Options{Locale: "en"})
		if err != nil {
			t.Fatalf("%q: %v", c.markdown, err)
		}
		if result.HasMath != c.want {
			t.Errorf("%q + %q: HasMath = %v, want %v", c.markdown, c.takeaway, result.HasMath, c.want)
		}
	}
}

func TestFragmentOmitsWrapper(t *testing.T) {
	result, err := Fragment("plain", Options{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.HTML) != "<p>plain</p>" {
		t.Errorf("fragment = %q", result.HTML)
	}
}

func TestSafeLink(t *testing.T) {
	rejected := []string{
		"", "//evil.test", `/\evil.test`, "/%2fevil.test", "javascript:alert(1)",
		"data:text/html,x", "https://user:pw@example.test", "/a%0ab", "/a b",
		"ftp://example.test", "mailto:a@b@c", "mailto:a b@c.test", "https://",
		"%zz", "relative/path", "example.test",
	}
	for _, value := range rejected {
		if got, ok := safeLink(value); ok {
			t.Errorf("safeLink(%q) = %q, expected rejection", value, got)
		}
	}
	accepted := []string{
		"#section", "/en/articles/x", "/media/" + strings.Repeat("a", 32),
		"https://example.test/p?q=1#f", "http://example.test", "HTTPS://example.test",
		"mailto:a@b.test", "/caf%C3%A9",
	}
	for _, value := range accepted {
		if got, ok := safeLink(value); !ok || got != value {
			t.Errorf("safeLink(%q) = %q, %v; expected accepted unchanged", value, got, ok)
		}
	}
}

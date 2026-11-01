package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	imagedraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Hook results may carry attacker-shaped text once plugins exist; bound every
// emitted field beyond the host's own configuration limits. Text is counted
// in runes like the site configuration bounds (alt text and titles are at
// most 200 runes), so Arabic values pass through untouched.
const socialTextLimit = 300
const socialImageLimit = 2048
const socialDimensionLimit = 16384

type socialMetadata struct {
	Image, Alt, Type, SiteName string
	Width, Height              int
}

// socialCards is the built-in extension that keeps today's social-card
// selection behind the content.rendered_meta hook.
type socialCards struct{}

func (socialCards) Slug() string { return "social-cards" }

func (socialCards) Register(r *extension.Registrar) error {
	return r.Hook(extension.HookRenderedMeta, 1, renderedMetaHook)
}

func renderedMetaHook(ctx context.Context, hook string, payload []byte) ([]byte, error) {
	var in extension.RenderedMetaPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, err
	}
	return json.Marshal(selectRenderedMeta(in))
}

// Use only published public configuration and an already authorized public
// media manifest. A learner's unlocked lesson must never advertise its gated cover.
func selectRenderedMeta(in extension.RenderedMetaPayload) extension.RenderedMetaResult {
	out := extension.RenderedMetaResult{Image: in.Origin + "/social-card.png", Alt: in.Title, Type: "website", SiteName: in.Site.Name, Width: 1200, Height: 630}
	if out.SiteName == "" {
		out.SiteName = in.Site.Wordmark
	}
	if in.Content != nil && in.Kind != "lesson" && !in.Content.Locked {
		out.Type = "article"
		for _, m := range in.Content.Media {
			if m.ID == in.Content.CoverID && m.Kind == "image" && (m.MIME == "image/jpeg" || m.MIME == "image/png" || m.MIME == "image/webp") {
				out.Image = in.Origin + "/media/" + m.ID
				out.Width, out.Height = m.Width, m.Height
				return out
			}
		}
	}
	id := ""
	if in.Site.BannerEnabled && in.Site.BannerID != "" {
		id = in.Site.BannerID
		out.Alt = in.Site.BannerAlt
	} else if in.Site.PortraitID != "" {
		id = in.Site.PortraitID
		out.Alt = in.Site.PortraitAlt
	} else if in.Site.LogoMode == "image" {
		id = in.Site.LogoID
		out.Alt = out.SiteName
	}
	if id != "" {
		out.Image = in.Origin + "/media/" + id
		out.Width, out.Height = 0, 0
	}
	if out.Alt == "" {
		out.Alt = in.Title
	}
	return out
}

// renderedMetaPayload bounds what hooks may see: the media manifest of a
// lesson or locked page never enters the payload, so no handler can leak a
// gated cover whatever it returns.
func renderedMetaPayload(page content.PageRoute, settings siteconfig.Public, origin, title string) extension.RenderedMetaPayload {
	payload := extension.RenderedMetaPayload{Origin: origin, Title: title, Kind: page.Kind, Site: extension.RenderedMetaSite{
		Name:          settings.Effective["name"].Text,
		Wordmark:      settings.Site.Wordmark,
		BannerEnabled: settings.Profile.BannerEnabled,
		BannerID:      settings.Profile.BannerID,
		BannerAlt:     settings.Effective["banner_alt"].Text,
		PortraitID:    settings.Profile.PortraitID,
		PortraitAlt:   settings.Effective["portrait_alt"].Text,
		LogoMode:      settings.Site.LogoMode,
		LogoID:        settings.Site.LogoID,
	}}
	if page.Content != nil {
		routed := &extension.RenderedMetaContent{Locked: page.Content.Locked}
		if page.Kind != "lesson" && !page.Content.Locked {
			routed.CoverID = page.Content.CoverID
			for _, m := range page.Content.Media {
				routed.Media = append(routed.Media, extension.RenderedMetaMedia{ID: m.ID, Kind: m.Kind, MIME: m.MIME, Width: m.Width, Height: m.Height})
			}
		}
		payload.Content = routed
	}
	return payload
}

// socialFor is the built-in selection the content.rendered_meta hook serves,
// kept directly callable so tests pin the exact selection behavior.
func socialFor(page content.PageRoute, settings siteconfig.Public, origin, title string) socialMetadata {
	out := selectRenderedMeta(renderedMetaPayload(page, settings, origin, title))
	return socialMetadata{Image: out.Image, Alt: out.Alt, Type: out.Type, SiteName: out.SiteName, Width: out.Width, Height: out.Height}
}

// renderedMeta dispatches content.rendered_meta and keeps final say over the
// emitted metadata: every result is bounded, sanitized, and restricted to
// same-origin images, and a failing hook falls back to the default card.
// When several handlers return a valid result the last registered one wins,
// each sanitized against the default card rather than merged with earlier
// results, per the contract in extension.RenderedMetaResult.
func (s *Server) renderedMeta(ctx context.Context, page content.PageRoute, settings siteconfig.Public, origin, title string) socialMetadata {
	fallback := socialMetadata{Image: origin + "/social-card.png", Alt: title, Type: "website", SiteName: settings.Effective["name"].Text, Width: 1200, Height: 630}
	if fallback.SiteName == "" {
		fallback.SiteName = settings.Site.Wordmark
	}
	payload, err := json.Marshal(renderedMetaPayload(page, settings, origin, title))
	if err != nil {
		return fallback
	}
	out := fallback
	for _, raw := range s.extensions().DispatchHook(ctx, extension.HookRenderedMeta, payload) {
		var result extension.RenderedMetaResult
		if json.Unmarshal(raw, &result) == nil {
			out = sanitizeRenderedMeta(result, origin, fallback)
		}
	}
	return out
}

func sanitizeRenderedMeta(in extension.RenderedMetaResult, origin string, fallback socialMetadata) socialMetadata {
	out := fallback
	if in.Type == "article" {
		out.Type = "article"
	}
	if name := boundedSocialText(in.SiteName); name != "" {
		out.SiteName = name
	}
	if alt := boundedSocialText(in.Alt); alt != "" {
		out.Alt = alt
	}
	if in.Image == origin+"/social-card.png" {
		out.Image, out.Width, out.Height = in.Image, 1200, 630
	} else if strings.HasPrefix(in.Image, origin+"/media/") && len(in.Image) <= socialImageLimit && !strings.ContainsAny(in.Image, " \r\n\t\"'<>\\") {
		out.Image, out.Width, out.Height = in.Image, 0, 0
		if in.Width > 0 && in.Width <= socialDimensionLimit && in.Height > 0 && in.Height <= socialDimensionLimit {
			out.Width, out.Height = in.Width, in.Height
		}
	}
	return out
}

func boundedSocialText(s string) string {
	if utf8.RuneCountInString(s) > socialTextLimit {
		return ""
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return s
}

// The fallback contains only the configured public hostname, never request Host,
// content, sessions or owner data. Generate it once per process, without a browser.
func defaultSocialImage(origin string) ([]byte, error) {
	host := "Personal blog"
	if u, err := url.Parse(origin); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	img := image.NewRGBA(image.Rect(0, 0, 1200, 630))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{247, 247, 244, 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 1200, 24), image.NewUniform(color.RGBA{43, 68, 196, 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(80, 400, 260, 416), image.NewUniform(color.RGBA{242, 179, 61, 255}), image.Point{}, draw.Src)
	face := basicfont.Face7x13
	width := font.MeasureString(face, host).Ceil()
	label := image.NewRGBA(image.Rect(0, 0, width, 16))
	d := font.Drawer{Dst: label, Src: image.NewUniform(color.RGBA{21, 22, 26, 255}), Face: face, Dot: fixed.P(0, 13)}
	d.DrawString(host)
	targetWidth := min(width*5, 1040)
	targetHeight := max(1, 16*targetWidth/width)
	imagedraw.NearestNeighbor.Scale(img, image.Rect(80, 270, 80+targetWidth, 270+targetHeight), label, label.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}

func (s *Server) serveSocialImage(w http.ResponseWriter, r *http.Request) {
	s.socialOnce.Do(func() { s.socialPNG, s.socialErr = defaultSocialImage(s.options.Config.BaseURL) })
	if s.socialErr != nil {
		writeError(w, InternalError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(s.socialPNG)))
	// Do not override the application's no-store policy.
	w.WriteHeader(http.StatusOK)
	if r.Method != "HEAD" {
		_, _ = w.Write(s.socialPNG)
	}
}

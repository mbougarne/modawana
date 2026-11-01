package httpserver

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	imagedraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type socialMetadata struct {
	Image, Alt, Type, SiteName string
	Width, Height              int
}

// Use only published public configuration and an already authorized public media
// manifest. A learner's unlocked lesson must never advertise its gated cover.
func socialFor(page content.PageRoute, settings siteconfig.Public, origin, title string) socialMetadata {
	out := socialMetadata{Image: origin + "/social-card.png", Alt: title, Type: "website", Width: 1200, Height: 630}
	out.SiteName = settings.Effective["name"].Text
	if out.SiteName == "" {
		out.SiteName = settings.Site.Wordmark
	}
	if page.Content != nil && page.Kind != "lesson" && !page.Content.Locked {
		out.Type = "article"
		for _, m := range page.Content.Media {
			if m.ID == page.Content.CoverID && m.Kind == "image" && (m.MIME == "image/jpeg" || m.MIME == "image/png" || m.MIME == "image/webp") {
				out.Image = origin + "/media/" + m.ID
				out.Width, out.Height = m.Width, m.Height
				return out
			}
		}
	}
	id := ""
	if settings.Profile.BannerEnabled && settings.Profile.BannerID != "" {
		id = settings.Profile.BannerID
		out.Alt = settings.Effective["banner_alt"].Text
	} else if settings.Profile.PortraitID != "" {
		id = settings.Profile.PortraitID
		out.Alt = settings.Effective["portrait_alt"].Text
	} else if settings.Site.LogoMode == "image" {
		id = settings.Site.LogoID
		out.Alt = out.SiteName
	}
	if id != "" {
		out.Image = origin + "/media/" + id
		out.Width, out.Height = 0, 0
	}
	if out.Alt == "" {
		out.Alt = title
	}
	return out
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

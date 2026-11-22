package extension

// SelectRenderedMeta uses published configuration and an authorized public
// media manifest. A learner's unlocked lesson must never advertise its gated cover.
func SelectRenderedMeta(in RenderedMetaPayload) RenderedMetaResult {
	out := RenderedMetaResult{Image: in.Origin + "/social-card.png", Alt: in.Title, Type: "website", SiteName: in.Site.Name, Width: 1200, Height: 630}
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

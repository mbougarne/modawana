package extension

// RenderedMetaPayload is the content.rendered_meta hook payload, version 1.
// It carries only published public projections: the host never includes a
// gated body, a locked or lesson cover manifest, or private configuration.
type RenderedMetaPayload struct {
	Origin  string               `json:"origin"`
	Title   string               `json:"title"`
	Kind    string               `json:"kind"`
	Content *RenderedMetaContent `json:"content,omitempty"`
	Site    RenderedMetaSite     `json:"site"`
}

// RenderedMetaContent describes the routed public content, when there is one.
type RenderedMetaContent struct {
	Locked  bool                `json:"locked"`
	CoverID string              `json:"cover_id,omitempty"`
	Media   []RenderedMetaMedia `json:"media,omitempty"`
}

// RenderedMetaMedia is one entry of the already authorized public media manifest.
type RenderedMetaMedia struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	MIME   string `json:"mime_type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// RenderedMetaSite carries the published public site configuration the hook
// may select from.
type RenderedMetaSite struct {
	Name          string `json:"name,omitempty"`
	Wordmark      string `json:"wordmark,omitempty"`
	BannerEnabled bool   `json:"banner_enabled,omitempty"`
	BannerID      string `json:"banner_id,omitempty"`
	BannerAlt     string `json:"banner_alt,omitempty"`
	PortraitID    string `json:"portrait_id,omitempty"`
	PortraitAlt   string `json:"portrait_alt,omitempty"`
	LogoMode      string `json:"logo_mode,omitempty"`
	LogoID        string `json:"logo_id,omitempty"`
}

// RenderedMetaResult is the content.rendered_meta result contract, version 1.
// The host bounds and sanitizes every field and keeps final say over the
// metadata it emits.
type RenderedMetaResult struct {
	Image    string `json:"image"`
	Alt      string `json:"alt"`
	Type     string `json:"type"`
	SiteName string `json:"site_name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

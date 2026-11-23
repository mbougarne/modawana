package theme

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestVersionTwoPackUsesHostLayoutsTokensAndPinnedAssets(t *testing.T) {
	var asset bytes.Buffer
	if err := png.Encode(&asset, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	m := manifest{
		Version: 2, Name: "Example", Assets: map[string][]byte{"hero.png": asset.Bytes()},
		Layouts: map[string]layoutManifest{
			"home":       {Slots: []string{"header", "banner", "content", "footer"}, Banner: "hero.png"},
			"article":    {Slots: []string{"header", "content", "footer"}},
			"storefront": {Slots: []string{"header", "content", "footer"}},
		},
		Tokens: map[string]string{"content_width": "narrow", "typeface": "system"},
	}
	parse := func() (Pack, error) {
		t.Helper()
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return parsePack("pack-example", raw)
	}
	pack, err := parse()
	if err != nil {
		t.Fatal(err)
	}
	home := pack.ForView("home")
	article := pack.ForView("article")
	if strings.Join(home.Layout, ",") != "header,banner,content,footer" ||
		home.BannerURL != "/theme-assets/pack-example/"+pack.Checksum+"/hero.png" ||
		article.BannerURL != "" || len(pack.ForView("page").Layout) != 0 ||
		!bytes.Equal(pack.Asset("hero.png"), asset.Bytes()) ||
		!strings.Contains(home.CSS, "[data-public-theme='pack-example'] .public-main") {
		t.Fatal(home, article)
	}
	m.Tokens["typeface"] = "url(https://example.test)"
	if _, err := parse(); err == nil {
		t.Fatal("arbitrary style accepted")
	}
	m.Tokens["typeface"] = "serif"
	m.Layouts["home"] = layoutManifest{Slots: []string{"header", "banner", "content", "footer"}, Banner: "unapproved.png"}
	if _, err := parse(); err == nil {
		t.Fatal("unapproved image accepted")
	}
	m.Layouts["home"] = layoutManifest{Slots: []string{"header", "checkout", "footer"}}
	if _, err := parse(); err == nil {
		t.Fatal("non-host slot accepted")
	}
}

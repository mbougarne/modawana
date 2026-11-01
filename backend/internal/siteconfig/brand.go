package siteconfig

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

//go:embed palette-defaults.json
var paletteJSON []byte
var paletteDefaults = func() map[string]map[string]string {
	var p map[string]map[string]string
	if json.Unmarshal(paletteJSON, &p) != nil {
		panic("invalid palette defaults")
	}
	return p
}()

type Palette struct {
	Version  int               `json:"version"`
	Light    map[string]string `json:"light"`
	Dark     map[string]string `json:"dark"`
	Adjusted bool              `json:"adjusted"`
	Checks   []ContrastCheck   `json:"checks"`
}
type ContrastCheck struct {
	Theme string  `json:"theme"`
	Check string  `json:"check"`
	Ratio float64 `json:"ratio"`
	Pass  bool    `json:"pass"`
}

func rgb(s string) [3]float64 {
	var out [3]float64
	for i := range out {
		v, _ := strconv.ParseUint(s[1+i*2:3+i*2], 16, 8)
		out[i] = float64(v)
	}
	return out
}
func mix(a, b string, t float64) string {
	x, y := rgb(a), rgb(b)
	return fmt.Sprintf("#%02X%02X%02X", int(math.Round(x[0]+(y[0]-x[0])*t)), int(math.Round(x[1]+(y[1]-x[1])*t)), int(math.Round(x[2]+(y[2]-x[2])*t)))
}
func luminance(s string) float64 {
	c := rgb(s)
	for i, v := range c {
		v /= 255
		if v <= 0.03928 {
			c[i] = v / 12.92
		} else {
			c[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return .2126*c[0] + .7152*c[1] + .0722*c[2]
}
func Contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + .05) / (math.Min(x, y) + .05)
}
func until(c, toward string, step float64, grounds ...string) string {
	for i := 0; i < 24; i++ {
		pass := true
		for _, g := range grounds {
			if Contrast(c, g) < 4.5 {
				pass = false
			}
		}
		if pass {
			break
		}
		c = mix(c, toward, step)
	}
	return c
}
func pickOn(bg, a, b string) string {
	if Contrast(a, bg) >= Contrast(b, bg) {
		return a
	}
	return b
}
func Derive(primary, secondary string) (Palette, error) {
	out := Palette{Version: 1, Checks: []ContrastCheck{}}
	if !hexColour.MatchString(primary) || !hexColour.MatchString(secondary) {
		return out, ErrInvalid
	}
	l, d := paletteDefaults["light"], paletteDefaults["dark"]
	soft := mix(primary, "#FFFFFF", .87)
	pl := until(primary, "#000000", .08, l["paper"], l["surface"], soft)
	strong := until(mix(pl, "#000000", .25), "#000000", .1, soft)
	softD := mix(primary, d["paper"], .72)
	pd := until(primary, "#FFFFFF", .08, d["paper"], d["surface"], softD)
	strongD := until(mix(pd, "#FFFFFF", .35), "#FFFFFF", .1, softD)
	sd := mix(secondary, "#FFFFFF", .1)
	out.Adjusted = pl != primary || pd != primary
	out.Light = map[string]string{"majorelle": pl, "majorelle-strong": strong, "majorelle-soft": soft, "on-majorelle": pickOn(pl, "#FFFFFF", l["ink"]), "sun": secondary, "on-sun": pickOn(secondary, l["ink"], "#FFFFFF"), "shadow-pop": "4px 4px 0 0 " + l["ink"]}
	out.Dark = map[string]string{"majorelle": pd, "majorelle-strong": strongD, "majorelle-soft": softD, "on-majorelle": pickOn(pd, d["paper"], "#FFFFFF"), "sun": sd, "on-sun": pickOn(sd, d["paper"], "#FFFFFF"), "shadow-pop": "4px 4px 0 0 " + pd}
	themes := map[string]map[string]string{"light": out.Light, "dark": out.Dark}
	if primary == strings.ToUpper(l["majorelle"]) {
		out.Adjusted = false
		for theme, v := range themes {
			for _, k := range []string{"majorelle", "majorelle-strong", "majorelle-soft", "on-majorelle"} {
				v[k] = paletteDefaults[theme][k]
			}
		}
		out.Dark["shadow-pop"] = "4px 4px 0 0 " + d["majorelle"]
	}
	if secondary == strings.ToUpper(l["sun"]) {
		for theme, v := range themes {
			v["sun"] = paletteDefaults[theme]["sun"]
			v["on-sun"] = paletteDefaults[theme]["on-sun"]
		}
	}
	valid := true
	for _, theme := range []string{"light", "dark"} {
		c := themes[theme]
		for _, pair := range [][3]string{{"links", c["majorelle"], paletteDefaults[theme]["paper"]}, {"surface", c["majorelle"], paletteDefaults[theme]["surface"]}, {"focus", c["majorelle"], c["majorelle-soft"]}, {"button", c["on-majorelle"], c["majorelle"]}, {"soft", c["majorelle-strong"], c["majorelle-soft"]}, {"secondary", c["on-sun"], c["sun"]}} {
			ratio := Contrast(pair[1], pair[2])
			pass := ratio >= 4.5
			out.Checks = append(out.Checks, ContrastCheck{theme, pair[0], ratio, pass})
			valid = valid && pass
		}
	}
	if !valid {
		return out, ErrInvalid
	}
	return out, nil
}
func (p Palette) CSS() string {
	block := func(v map[string]string) string {
		keys := []string{}
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "--%s:%s;", k, v[k])
		}
		return b.String()
	}
	return `:root,[data-theme="light"]{` + block(p.Light) + `}[data-theme="dark"]{` + block(p.Dark) + `}@media (prefers-color-scheme: dark){:root:not([data-theme="light"]){` + block(p.Dark) + `}}`
}

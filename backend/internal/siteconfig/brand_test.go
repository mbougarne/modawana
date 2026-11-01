package siteconfig

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestSharedPaletteVectors(t *testing.T) {
	b, e := os.ReadFile("testdata/palette-vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var vectors []struct {
		Primary   string `json:"primary"`
		Secondary string `json:"secondary"`
		Palette
	}
	if json.Unmarshal(b, &vectors) != nil {
		t.Fatal("vectors")
	}
	for _, v := range vectors {
		p, e := Derive(v.Primary, v.Secondary)
		valid := true
		for _, c := range v.Checks {
			valid = valid && c.Pass
		}
		if (e == nil) != valid {
			t.Fatal(v.Primary, v.Secondary, e)
		}
		if p.Version != v.Version || p.Adjusted != v.Adjusted || !reflect.DeepEqual(p.Light, v.Light) || !reflect.DeepEqual(p.Dark, v.Dark) {
			t.Fatalf("Go/TypeScript mismatch for %s: %+v vs %+v", v.Primary, p, v.Palette)
		}
		if len(p.Checks) != len(v.Checks) {
			t.Fatal("check count")
		}
		for i, c := range p.Checks {
			other := v.Checks[i]
			if c.Theme != other.Theme || c.Check != other.Check || c.Pass != other.Pass || math.Abs(c.Ratio-other.Ratio) > 1e-10 {
				t.Fatal(c, other)
			}
		}
	}
}

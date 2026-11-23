// Package locale defines the installed editorial and interface locales.
package locale

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

//go:embed packs.json
var source []byte

type Pack struct {
	Code           string
	Name           string
	Dir            string
	Intl           string
	DefaultVisible bool
}

var packs = func() []Pack {
	var entries []Pack
	if json.Unmarshal(source, &entries) != nil || len(entries) == 0 || entries[0].Code != "en" || !entries[0].DefaultVisible {
		panic("invalid locale packs")
	}
	codePattern := regexp.MustCompile("^[a-z]{2}$")
	seen := map[string]bool{}
	for _, entry := range entries {
		if !codePattern.MatchString(entry.Code) || seen[entry.Code] || (entry.Dir != "ltr" && entry.Dir != "rtl") ||
			entry.Name == "" || entry.Intl == "" {
			panic("invalid locale pack")
		}
		seen[entry.Code] = true
	}
	return entries
}()

func Codes() []string {
	codes := make([]string, 0, len(packs))
	for _, pack := range packs {
		codes = append(codes, pack.Code)
	}
	return codes
}

func PublicDefaults() []string {
	var codes []string
	for _, pack := range packs {
		if pack.DefaultVisible {
			codes = append(codes, pack.Code)
		}
	}
	return codes
}

func Valid(code string) bool {
	return slices.ContainsFunc(packs, func(pack Pack) bool { return pack.Code == code })
}

func Direction(code string) string {
	for _, pack := range packs {
		if pack.Code == code {
			return pack.Dir
		}
	}
	return "ltr"
}

func PathPattern() string { return strings.Join(Codes(), "|") }

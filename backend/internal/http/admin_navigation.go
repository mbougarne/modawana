package httpserver

import (
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func pageLocale(value string) bool {
	return value == "en" || value == "ar" || value == "fr"
}

// Only explicit HTML requests are browser-page navigation. API requests, including
// ones accepting */*, keep their structured authentication errors.
func wantsHTML(r *http.Request) bool {
	for _, entry := range strings.Split(r.Header.Get("Accept"), ",") {
		kind, parameters, err := mime.ParseMediaType(strings.TrimSpace(entry))
		if err != nil || kind != "text/html" {
			continue
		}
		if raw, ok := parameters["q"]; ok {
			quality, err := strconv.ParseFloat(raw, 64)
			if err != nil || !(quality > 0 && quality <= 1) {
				continue
			}
		}
		return true
	}
	return false
}

func redirectOwnerLogin(w http.ResponseWriter, r *http.Request, locale, target string) {
	location := "/" + locale + "/login"
	if target != "" {
		location += "?" + url.Values{"return": {target}}.Encode()
	}
	http.Redirect(w, r, location, http.StatusFound)
}

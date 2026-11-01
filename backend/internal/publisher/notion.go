package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NotionClient struct {
	Token string
	HTTP  *http.Client
	Base  string
}

func NewNotion(token string) *NotionClient {
	return &NotionClient{Token: token, Base: "https://api.notion.com/v1", HTTP: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (n *NotionClient) request(ctx context.Context, method, path string, in any, out any) error {
	if n.Token == "" {
		return errors.New("NOTION_API_KEY is not configured for this runner")
	}
	b, e := json.Marshal(in)
	if e != nil {
		return e
	}
	var body io.Reader
	if in != nil {
		body = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, n.Base+path, body)
	if e != nil {
		return errors.New("invalid Notion request")
	}
	r.Header.Set("Authorization", "Bearer "+n.Token)
	r.Header.Set("Notion-Version", "2026-03-11")
	r.Header.Set("Content-Type", "application/json")
	response, e := n.HTTP.Do(r)
	if e != nil {
		return errors.New("Notion network outcome unknown")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Notion HTTP %d", response.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(out); e != nil {
		return errors.New("Notion response outcome unknown")
	}
	return nil
}
func (n *NotionClient) Deliver(ctx context.Context, in Delivery, uncertain bool) (Destination, error) {
	pending := Destination{State: "failed"}
	if uncertain {
		pending.State = "uncertain"
	}
	if n.Token == "" {
		return pending, errors.New("NOTION_API_KEY is not configured for this runner")
	}
	marker := "Delivery key: " + in.Key + "/" + in.Locale + "; source revision: " + in.SourceRevision + "; payload: " + fingerprint(in.Draft)
	title := in.Draft.Title + " [" + in.Key + "/" + in.Locale + "]"
	// Inspect direct child pages, including pagination. Search indexing is not a
	// safe negative proof after an interrupted write.
	cursor := ""
	found := ""
	for page := 0; page < 100; page++ {
		path := "/blocks/" + in.NotionParent + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}
		var result struct {
			Results []struct {
				ID    string `json:"id"`
				Type  string `json:"type"`
				Child struct {
					Title string `json:"title"`
				} `json:"child_page"`
			} `json:"results"`
			More   bool   `json:"has_more"`
			Cursor string `json:"next_cursor"`
		}
		if e := n.request(ctx, "GET", path, nil, &result); e != nil {
			return pending, e
		}
		for _, child := range result.Results {
			if child.Type == "child_page" && strings.HasSuffix(child.Child.Title, "["+in.Key+"/"+in.Locale+"]") {
				if found != "" {
					return pending, errors.New("multiple Notion pages match the delivery key; reconcile manually")
				}
				found = child.ID
			}
		}
		if !result.More {
			break
		}
		cursor = result.Cursor
		if cursor == "" || page == 99 {
			return pending, errors.New("Notion child listing incomplete; refusing duplicate risk")
		}
	}
	if found != "" {
		if !pageID.MatchString(found) {
			return pending, errors.New("invalid Notion page ID")
		}
		var current struct {
			Markdown  string `json:"markdown"`
			Truncated bool   `json:"truncated"`
		}
		if e := n.request(ctx, "GET", "/pages/"+found+"/markdown", nil, &current); e != nil {
			return pending, e
		}
		if current.Truncated || !strings.Contains(current.Markdown, marker) {
			return pending, errors.New("Notion copy differs from the source revision; no overwrite performed")
		}
		return Destination{State: "done", ID: found, URL: "https://www.notion.so/" + strings.ReplaceAll(found, "-", "")}, nil
	}
	if uncertain {
		return Destination{State: "uncertain"}, errors.New("previous Notion create outcome unknown; no matching page yet, refusing a duplicate create")
	}
	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	payload := map[string]any{"parent": map[string]string{"page_id": in.NotionParent}, "properties": map[string]any{"title": map[string]any{"title": []any{map[string]any{"text": map[string]string{"content": title}}}}}, "markdown": marker + "\n\n" + in.Draft.Markdown}
	encoded, e := json.Marshal(payload)
	if e != nil || len(encoded) > 500000 {
		return pending, errors.New("Notion payload exceeds the runner's 500000-byte limit")
	}
	if e := n.request(ctx, "POST", "/pages", payload, &result); e != nil {
		return Destination{State: "uncertain"}, e
	}
	if !pageID.MatchString(result.ID) {
		return Destination{State: "uncertain"}, errors.New("Notion returned no confirmed page ID")
	}
	return Destination{State: "done", ID: result.ID, URL: result.URL}, nil
}

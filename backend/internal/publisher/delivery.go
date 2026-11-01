package publisher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mbougarne/modawana/backend/internal/content"
	"os"
	"path/filepath"
	"regexp"
)

type Delivery struct {
	Key            string             `json:"key"`
	Locale         string             `json:"locale"`
	SourceRevision string             `json:"source_revision"`
	NotionParent   string             `json:"notion_parent"`
	AutoPublish    bool               `json:"auto_publish"`
	Draft          content.DraftInput `json:"draft"`
}
type Destination struct {
	State   string `json:"state"`
	ID      string `json:"id,omitempty"`
	Version int64  `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`
	Error   string `json:"error,omitempty"`
}
type DeliveryState struct {
	Key            string      `json:"key"`
	SourceRevision string      `json:"source_revision"`
	RequestHash    string      `json:"request_hash"`
	BlogOrigin     string      `json:"blog_origin"`
	NotionParent   string      `json:"notion_parent"`
	Blog           Destination `json:"blog"`
	Notion         Destination `json:"notion"`
}
type NotionDelivery interface {
	Deliver(context.Context, Delivery, bool) (Destination, error)
}

var deliveryKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,119}$`)
var sourceKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,160}$`)
var pageID = regexp.MustCompile(`^(?:[a-fA-F0-9]{32}|[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12})$`)

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func saveDelivery(path string, state DeliveryState) error {
	b, e := json.MarshalIndent(state, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".delivery-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func Deliver(ctx context.Context, client *Client, notion NotionDelivery, dir string, in Delivery) (DeliveryState, error) {
	var state DeliveryState
	if !deliveryKey.MatchString(in.Key) || !sourceKey.MatchString(in.SourceRevision) || !pageID.MatchString(in.NotionParent) || (in.Locale != "en" && in.Locale != "ar" && in.Locale != "fr") || in.Draft.ExpectedVersion == nil {
		return state, errors.New("invalid delivery manifest")
	}
	if in.AutoPublish && (in.Locale != in.Draft.SourceLocale || in.Draft.Kind != "lesson" || in.Draft.CollectionKey == "") {
		return state, errors.New("auto-publication requires an original-language lesson in an explicitly configured collection")
	}
	if !filepath.IsAbs(dir) {
		return state, errors.New("delivery state directory must be absolute")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return state, e
	}
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&0077 != 0 {
		return state, errors.New("delivery state directory must be private and not a symlink")
	}
	// The lock deliberately survives a process crash. An operator must reconcile
	// remote outcomes before removing it; guessing could duplicate a Notion page.
	lock := filepath.Join(dir, in.Key+"-"+in.Locale+".lock")
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return state, errors.New("delivery is locked; reconcile any interrupted runner first")
	}
	f.Close()
	defer os.Remove(lock)
	path := filepath.Join(dir, in.Key+"-"+in.Locale+".json")
	hash := fingerprint(in)
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode()&0077 != 0) {
		return state, errors.New("unsafe delivery state file")
	}
	b, e := os.ReadFile(path)
	if e == nil {
		if json.Unmarshal(b, &state) != nil || state.RequestHash != hash || state.BlogOrigin != client.Base {
			return state, errors.New("delivery manifest or destination changed; reconcile explicitly instead of overwriting a successful copy")
		}
	} else if os.IsNotExist(e) {
		state = DeliveryState{Key: in.Key, SourceRevision: in.SourceRevision, RequestHash: hash, BlogOrigin: client.Base, NotionParent: in.NotionParent, Blog: Destination{State: "pending"}, Notion: Destination{State: "pending"}}
	} else {
		return state, e
	}
	if e = saveDelivery(path, state); e != nil {
		return state, e
	}
	prefix := "delivery:" + fingerprint(struct{ Key, Locale, Revision string }{in.Key, in.Locale, in.SourceRevision})
	endpoint := "/api/v1/publisher/content/" + in.Key + "/" + in.Locale
	if state.Blog.State != "done" {
		// The immutable draft result is checkpointed before a possible publish, so a
		// publish retry never rewrites the draft or overwrites a later manual edit.
		if state.Blog.ID == "" {
			raw, err := client.Do(ctx, "PUT", endpoint, in.Draft, prefix+":draft")
			if err != nil {
				state.Blog.Error = err.Error()
				state.Blog.State = "failed"
			} else {
				var result content.Result
				if json.Unmarshal(raw, &result) != nil || result.RevisionID == "" {
					return state, errors.New("invalid draft result")
				}
				state.Blog = Destination{State: "draft", ID: result.RevisionID, Version: result.Version}
			}
			if e = saveDelivery(path, state); e != nil {
				return state, e
			}
		}
		if state.Blog.ID != "" {
			if in.AutoPublish {
				raw, err := client.Do(ctx, "POST", endpoint+"/publish", content.Action{ExpectedVersion: &state.Blog.Version, RevisionID: state.Blog.ID}, prefix+":publish")
				if err != nil {
					state.Blog.State = "failed"
					state.Blog.Error = err.Error()
				} else {
					var result content.Result
					if json.Unmarshal(raw, &result) != nil || result.RevisionID != state.Blog.ID {
						return state, errors.New("invalid publication result")
					}
					state.Blog.Version = result.Version
					state.Blog.State = "done"
					state.Blog.Error = ""
				}
			} else {
				state.Blog.State = "done"
				state.Blog.Error = ""
			}
			if e = saveDelivery(path, state); e != nil {
				return state, e
			}
		}
	}
	if state.Notion.State != "done" {
		uncertain := state.Notion.State == "attempting" || state.Notion.State == "uncertain"
		state.Notion.State = "attempting"
		if e = saveDelivery(path, state); e != nil {
			return state, e
		}
		result, err := notion.Deliver(ctx, in, uncertain)
		state.Notion = result
		if err != nil {
			state.Notion.Error = err.Error()
			if state.Notion.State == "" {
				state.Notion.State = "uncertain"
			}
		} else if result.State != "done" || !pageID.MatchString(result.ID) {
			return state, errors.New("invalid Notion result")
		}
		if e = saveDelivery(path, state); e != nil {
			return state, e
		}
	}
	if state.Blog.State != "done" || state.Notion.State != "done" {
		return state, fmt.Errorf("delivery incomplete: blog=%s notion=%s; successful destination retained", state.Blog.State, state.Notion.State)
	}
	return state, nil
}

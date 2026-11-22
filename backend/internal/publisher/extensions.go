package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mbougarne/modawana/backend/internal/extension"
)

// Extensions wires opt-in first-party publishing into the native extension host.
// The local CLI and the server share the same runners and checkpoint format.
func Extensions(blogOrigin string, getenv func(string) string, maintenance func() bool) ([]extension.Extension, error) {
	manual := getenv("BLOG_NOTION_DELIVERY_ENABLED")
	if manual != "" && manual != "false" && manual != "true" {
		return nil, errors.New("BLOG_NOTION_DELIVERY_ENABLED must be true or false")
	}
	schedulePath := getenv("BLOG_LESSON_SCHEDULE_FILE")
	var schedule Schedule
	if schedulePath != "" {
		if !filepath.IsAbs(schedulePath) || readPrivateJSON(schedulePath, &schedule) != nil {
			return nil, errors.New("BLOG_LESSON_SCHEDULE_FILE must be an absolute path to a private schedule JSON file")
		}
	}
	if manual != "true" && !schedule.Enabled {
		return nil, nil
	}
	dir := getenv("BLOG_LESSON_STATE_DIR")
	if !filepath.IsAbs(dir) {
		return nil, errors.New("BLOG_LESSON_STATE_DIR must be an absolute path")
	}
	var ca []byte
	if path := getenv("BLOG_PUBLISHER_CA"); path != "" {
		var err error
		ca, err = os.ReadFile(path)
		if err != nil {
			return nil, errors.New("cannot read publisher CA file")
		}
	}
	client, err := New(blogOrigin, getenv("BLOG_PUBLISHER_TOKEN"), ca)
	if err != nil {
		return nil, err
	}
	notionKey := getenv("NOTION_API_KEY")
	if notionKey == "" {
		return nil, errors.New("NOTION_API_KEY is required for in-process delivery")
	}
	delivery := &notionExtension{client: client, notion: NewNotion(notionKey), dir: dir}
	var builtins []extension.Extension
	if manual == "true" {
		builtins = append(builtins, delivery)
	}
	if schedule.Enabled {
		key := getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("OPENAI_API_KEY is required for in-process scheduling")
		}
		builtins = append(builtins, &lessonExtension{
			delivery: delivery, generator: NewLessonGenerator(key), schedulePath: schedulePath,
			maintenance: maintenance, now: time.Now,
		})
	}
	return builtins, nil
}

type notionExtension struct {
	client *Client
	notion NotionDelivery
	dir    string
}

func (*notionExtension) Slug() string { return "notion-delivery" }

func (n *notionExtension) Register(r *extension.Registrar) error {
	return r.Route(extension.Route{Method: http.MethodPost, Path: "/deliver", Handler: http.HandlerFunc(n.serve)})
}

// The extension router supplies the owner session and mutation CSRF checks.
func (n *notionExtension) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
		http.Error(w, "Use application/json.", http.StatusUnsupportedMediaType)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 2<<20)
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	var input *Delivery
	if decoder.Decode(&input) != nil || input == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "Invalid delivery manifest.", http.StatusBadRequest)
		return
	}
	// Match the original delivery runner's transport limits while bounding
	// the full sequence of blog and Notion requests.
	const budget = 2 * time.Minute
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(budget + 5*time.Second))
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	state, err := Deliver(ctx, n.client, n.notion, n.dir, *input)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		if state.Key == "" {
			http.Error(w, "Delivery rejected. Verify the manifest and checkpoint.", http.StatusUnprocessableEntity)
			return
		}
		// Per-destination outcomes let the operator retry the exact same
		// manifest while retaining a successful destination.
		w.WriteHeader(http.StatusBadGateway)
	}
	_ = json.NewEncoder(w).Encode(state)
}

type lessonExtension struct {
	delivery     *notionExtension
	generator    *LessonGenerator
	schedulePath string
	maintenance  func() bool
	now          func() time.Time
}

func (*lessonExtension) Slug() string { return "lesson-scheduler" }

func (s *lessonExtension) Register(r *extension.Registrar) error {
	return r.Job(extension.Job{
		Name: "deliver-lessons", Every: time.Minute, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			if s.maintenance != nil && s.maintenance() {
				return nil
			}
			err := RunSchedule(ctx, s.delivery.client, s.delivery.notion, s.generator, s.schedulePath, s.delivery.dir, s.now())
			if err != nil {
				// The original runner can return filesystem paths. Its
				// private checkpoints remain the source of retry details.
				return errors.New("lesson schedule tick failed; inspect private checkpoints")
			}
			return nil
		},
	})
}

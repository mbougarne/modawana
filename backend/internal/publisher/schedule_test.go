package publisher

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/content"
)

func scheduleFixture() Schedule {
	return Schedule{Enabled: true, MonthlyMicroUSD: 10000000, Jobs: []LessonJob{{ID: "cpp", StartDate: "2026-10-30", StartNumber: 38, EndNumber: 365, UTC: "19:00", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Collection: "cpp-path", NotionParent: strings.Repeat("c", 32), Locale: "en", Model: "gpt-5.4-mini", MaxOutput: 8192, Prompt: "Teach C++20 VM internals", Context: "Day 37 covered blocking reads."}}}
}

func TestScheduleCalendarAndCadence(t *testing.T) {
	j := scheduleFixture().Jobs[0]
	for _, row := range []struct {
		stamp string
		n     int
		due   bool
	}{{"2026-10-30T18:59:59Z", 38, false}, {"2026-10-30T19:00:00Z", 38, true}, {"2026-11-01T19:00:00Z", 40, true}} {
		now, _ := time.Parse(time.RFC3339, row.stamp)
		n, due, e := j.due(now)
		if e != nil || n != row.n || due != row.due {
			t.Fatal(row, n, due, e)
		}
	}
	j.Weekdays, j.StartNumber = []int{1, 3, 5}, 23
	now, _ := time.Parse(time.RFC3339, "2026-11-02T19:00:00Z")
	if n, due, e := j.due(now); e != nil || n != 24 || !due {
		t.Fatal(n, due, e)
	}
	j.Weekdays = []int{5, 5}
	if _, _, e := j.due(now); e == nil {
		t.Fatal("duplicate weekdays accepted")
	}
}

func TestSchedulePersistsGenerationAndIndependentRetryAcrossMidnight(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	config := scheduleFixture()
	configPath := filepath.Join(root, "schedule.json")
	if e := saveRunnerJSON(configPath, config); e != nil {
		t.Fatal(e)
	}
	writes, generations := 0, 0
	transport := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "api.openai.com" {
			generations++
			if r.Header.Get("Authorization") != "Bearer openai-secret" {
				t.Error("wrong generator credential")
			}
			var request map[string]any
			json.NewDecoder(r.Body).Decode(&request)
			if request["store"] != false || int(request["max_output_tokens"].(float64)) != 8192 {
				t.Error("unbounded generation")
			}
			lesson, _ := json.Marshal(map[string]string{"title": "Day 38 — VM", "summary": "VM lesson", "markdown": strings.Repeat("C++ VM lesson. ", 20)})
			json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": string(lesson)}}}}})
			return
		}
		writes++
		if r.Header.Get("Authorization") != "Bearer blog-secret" {
			t.Error("wrong publisher credential")
		}
		json.NewEncoder(w).Encode(content.Result{RevisionID: strings.Repeat("a", 32), Version: 1})
	}))
	client, _ := New(transport.URL, "blog-secret", nil)
	client.HTTP.Transport = transport
	g := NewLessonGenerator("openai-secret")
	g.HTTP.Transport = transport
	notion := &fakeNotion{fail: true}
	now, _ := time.Parse(time.RFC3339, "2026-10-30T19:00:00Z")
	state := filepath.Join(root, "state")
	if e := RunSchedule(ctx, client, notion, g, configPath, state, now); e == nil {
		t.Fatal("missing destination reported success")
	}
	notion.fail = false
	// An unfinished delivery is retried next day before the next lesson is due.
	if e := RunSchedule(ctx, client, notion, g, configPath, state, now.Add(6*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e := RunSchedule(ctx, client, notion, g, configPath, state, now.Add(7*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if generations != 1 || writes != 1 || notion.calls != 2 {
		t.Fatal(generations, writes, notion.calls)
	}
	var budget usageReservation
	if e := readPrivateJSON(filepath.Join(state, "usage-2026-10.json"), &budget); e != nil || budget.MicroUSD <= 0 {
		t.Fatal(budget, e)
	}
	config.Jobs[0].AutoPublish = true
	saveRunnerJSON(configPath, config)
	if e := RunSchedule(ctx, client, notion, g, configPath, state, now); e == nil {
		t.Fatal("changed policy reused generation")
	}
}

func TestScheduleBudgetRetryLimitAndCrashLock(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	config := scheduleFixture()
	path := filepath.Join(root, "schedule.json")
	saveRunnerJSON(path, config)
	requests := 0
	transport := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(429) }))
	client, _ := New(transport.URL, "blog", nil)
	client.HTTP.Transport = transport
	g := NewLessonGenerator("openai")
	g.HTTP.Transport = transport
	now, _ := time.Parse(time.RFC3339, "2026-10-30T19:00:00Z")
	state := filepath.Join(root, "state")
	for i := 0; i < 3; i++ {
		if e := RunSchedule(context.Background(), client, &fakeNotion{}, g, path, state, now); e == nil {
			t.Fatal("failed generation reported success")
		}
	}
	if requests != 2 {
		t.Fatal("generation retries unbounded", requests)
	}
	var usage usageReservation
	readPrivateJSON(filepath.Join(state, "usage-2026-10.json"), &usage)
	if usage.MicroUSD != 2*generationCost(config.Jobs[0], generationInput(config.Jobs[0], 38, "")) {
		t.Fatal("failed request reservation refunded")
	}
	os.WriteFile(filepath.Join(state, "schedule.lock"), nil, 0600)
	if e := RunSchedule(context.Background(), client, &fakeNotion{}, g, path, state, now); e == nil {
		t.Fatal("crash lock ignored")
	}
	os.Remove(filepath.Join(state, "schedule.lock"))
	config.MonthlyMicroUSD = 10000
	config.Jobs[0].ID = "other"
	saveRunnerJSON(path, config)
	if e := RunSchedule(context.Background(), client, &fakeNotion{}, g, path, state, now); e == nil || requests != 2 {
		t.Fatal("budget exceeded", e, requests)
	}
	config.Enabled = false
	saveRunnerJSON(path, config)
	if e := RunSchedule(context.Background(), client, &fakeNotion{}, g, path, state, now); e != nil {
		t.Fatal(e)
	}
	os.Chmod(path, 0644)
	if e := RunSchedule(context.Background(), client, &fakeNotion{}, g, path, state, now); e == nil {
		t.Fatal("public config accepted")
	}
}

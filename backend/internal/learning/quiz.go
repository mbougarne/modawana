// Package learning owns private assessment definitions and browser-scoped progress.
package learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid quiz")
var ErrReview = errors.New("assessment review required")
var stableID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Option struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type Question struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	Options []Option `json:"options"`
}
type PrivateQuestion struct {
	Question
	Correct     []string `json:"correct"`
	Explanation string   `json:"explanation"`
}
type Definition struct {
	AssessmentID     string            `json:"assessment_id,omitempty"`
	PassMark         int               `json:"pass_mark"`
	ShowExplanations bool              `json:"show_explanations"`
	Questions        []PrivateQuestion `json:"questions"`
}
type canonicalQuestion struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Options []string `json:"options"`
	Correct []string `json:"correct"`
}
type canonical struct {
	PassMark         int                 `json:"pass_mark"`
	ShowExplanations bool                `json:"show_explanations"`
	Questions        []canonicalQuestion `json:"questions"`
}
type wording struct {
	Questions    []Question `json:"questions"`
	Explanations []string   `json:"explanations"`
}

func textOK(s string, max int, empty bool) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= max && (empty || strings.TrimSpace(s) != "")
}
func split(d Definition) (canonical, wording, error) {
	c := canonical{PassMark: d.PassMark, ShowExplanations: d.ShowExplanations}
	w := wording{}
	if len(d.Questions) < 1 || len(d.Questions) > 20 || d.PassMark < 1 || d.PassMark > len(d.Questions) {
		return c, w, ErrInvalid
	}
	seen := map[string]bool{}
	for _, q := range d.Questions {
		if !stableID.MatchString(q.ID) || seen[q.ID] || !textOK(q.Text, 2000, false) || !textOK(q.Explanation, 4000, true) || len(q.Options) < 2 || len(q.Options) > 8 || (q.Type != "single" && q.Type != "multi" && q.Type != "bool") {
			return c, w, ErrInvalid
		}
		seen[q.ID] = true
		cq := canonicalQuestion{ID: q.ID, Type: q.Type}
		options := map[string]bool{}
		for _, o := range q.Options {
			if !stableID.MatchString(o.ID) || options[o.ID] || !textOK(o.Text, 1000, false) {
				return c, w, ErrInvalid
			}
			options[o.ID] = true
			cq.Options = append(cq.Options, o.ID)
		}
		if q.Type == "bool" && (len(q.Options) != 2 || q.Options[0].ID != "true" || q.Options[1].ID != "false") {
			return c, w, ErrInvalid
		}
		if len(q.Correct) < 1 || len(q.Correct) > len(q.Options) || q.Type != "multi" && len(q.Correct) != 1 {
			return c, w, ErrInvalid
		}
		answers := map[string]bool{}
		for _, id := range q.Correct {
			if !options[id] || answers[id] {
				return c, w, ErrInvalid
			}
			answers[id] = true
			cq.Correct = append(cq.Correct, id)
		}
		sort.Strings(cq.Correct)
		c.Questions = append(c.Questions, cq)
		w.Questions = append(w.Questions, q.Question)
		w.Explanations = append(w.Explanations, q.Explanation)
	}
	if len(encode(c)) > 65536 || len(encode(w)) > 262144 {
		return c, w, ErrInvalid
	}
	feedback := map[string]Feedback{}
	if d.ShowExplanations {
		for _, q := range d.Questions {
			feedback[q.ID] = Feedback{Correct: true, CorrectIDs: q.Correct, Explanation: q.Explanation}
		}
		if len(encode(feedback)) > 120000 {
			return c, w, ErrInvalid
		}
	}
	return c, w, nil
}
func digest(b []byte) string      { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Validate(d Definition) error { _, _, e := split(d); return e }

type Version struct {
	Path    string `json:"path,omitempty"`
	Epoch   int64  `json:"epoch,omitempty"`
	Quiz    string `json:"quiz"`
	Content string `json:"content"`
}
type PublicQuiz struct {
	ID               string     `json:"id"`
	Version          Version    `json:"version"`
	Locale           string     `json:"locale"`
	PassMark         int        `json:"passMark"`
	UnlocksNext      bool       `json:"unlocksNext"`
	ShowExplanations bool       `json:"showExplanations"`
	Questions        []Question `json:"questions"`
}

func Public(d Definition, revision, locale string) PublicQuiz {
	questions := make([]Question, len(d.Questions))
	for i, q := range d.Questions {
		questions[i] = q.Question
	}
	return PublicQuiz{ID: d.AssessmentID, Version: Version{Quiz: d.AssessmentID, Content: revision}, Locale: locale, PassMark: d.PassMark, ShowExplanations: d.ShowExplanations, Questions: questions}
}

type Submission struct {
	Version        Version             `json:"version"`
	Answers        map[string][]string `json:"answers"`
	IdempotencyKey string              `json:"idempotencyKey"`
}
type Feedback struct {
	Correct     bool     `json:"correct"`
	CorrectIDs  []string `json:"correctOptionIds,omitempty"`
	Explanation string   `json:"explanation,omitempty"`
}
type Result struct {
	Completed    bool                `json:"completed"`
	UnlockedHref string              `json:"unlockedHref,omitempty"`
	Version      Version             `json:"version"`
	Score        int                 `json:"score"`
	Total        int                 `json:"total"`
	Passed       bool                `json:"passed"`
	Feedback     map[string]Feedback `json:"feedback"`
}

// Grade is shared with future public attempts. It never writes progress.
func Grade(d Definition, revision string, answers map[string][]string) (Result, error) {
	out := Result{Version: Version{Quiz: d.AssessmentID, Content: revision}, Total: len(d.Questions), Feedback: map[string]Feedback{}}
	if Validate(d) != nil || len(answers) != len(d.Questions) {
		return out, ErrInvalid
	}
	for _, q := range d.Questions {
		a, ok := answers[q.ID]
		if !ok || len(a) < 1 || len(a) > len(q.Options) || q.Type != "multi" && len(a) != 1 {
			return Result{}, ErrInvalid
		}
		valid := map[string]bool{}
		for _, o := range q.Options {
			valid[o.ID] = true
		}
		seen := map[string]bool{}
		for _, id := range a {
			if !valid[id] || seen[id] {
				return Result{}, ErrInvalid
			}
			seen[id] = true
		}
		correct := len(a) == len(q.Correct)
		for _, id := range q.Correct {
			correct = correct && seen[id]
		}
		feedback := Feedback{Correct: correct}
		if d.ShowExplanations {
			feedback.CorrectIDs = append([]string{}, q.Correct...)
			feedback.Explanation = q.Explanation
		}
		out.Feedback[q.ID] = feedback
		if correct {
			out.Score++
		}
	}
	out.Passed = out.Score >= d.PassMark
	return out, nil
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }

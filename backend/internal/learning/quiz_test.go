package learning

import (
	"encoding/json"
	"strings"
	"testing"
)

func example() Definition {
	return Definition{PassMark: 2, ShowExplanations: true, Questions: []PrivateQuestion{
		{Question: Question{ID: "single", Type: "single", Text: "One", Options: []Option{{"a", "A"}, {"b", "B"}}}, Correct: []string{"a"}, Explanation: "Secret explanation"},
		{Question: Question{ID: "multi", Type: "multi", Text: "Two", Options: []Option{{"a", "A"}, {"b", "B"}, {"c", "C"}}}, Correct: []string{"a", "c"}},
		{Question: Question{ID: "bool", Type: "bool", Text: "Three", Options: []Option{{"true", "True"}, {"false", "False"}}}, Correct: []string{"false"}},
	}}
}
func TestValidationAndProjection(t *testing.T) {
	d := example()
	if e := Validate(d); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(Public(d, "revision", "en"))
	for _, secret := range []string{"correct", "explanation", "Secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal(string(raw))
		}
	}
	for _, mutate := range []func(*Definition){func(d *Definition) { d.PassMark = 0 }, func(d *Definition) { d.PassMark = 4 }, func(d *Definition) { d.Questions = nil }, func(d *Definition) { d.Questions[1].ID = "single" }, func(d *Definition) { d.Questions[0].Correct = []string{"a", "b"} }, func(d *Definition) { d.Questions[0].Text = " " }, func(d *Definition) { d.Questions[2].Options[0].ID = "yes" }, func(d *Definition) { d.Questions[1].Correct = []string{"a", "a"} }, func(d *Definition) { d.Questions[0].Options[1].ID = "a" }, func(d *Definition) { d.Questions[0].Correct = []string{"missing"} }, func(d *Definition) { d.Questions[0].Options = make([]Option, 9) }} {
		copy := example()
		mutate(&copy)
		if Validate(copy) == nil {
			t.Fatal("accepted malformed quiz", copy)
		}
	}
}
func TestGradeExactSetsAndShape(t *testing.T) {
	d := example()
	answers := map[string][]string{"single": {"a"}, "multi": {"c", "a"}, "bool": {"false"}}
	r, e := Grade(d, "r", answers)
	if e != nil || r.Score != 3 || !r.Passed || r.Feedback["single"].Explanation == "" {
		t.Fatal(r, e)
	}
	answers["multi"] = []string{"a"}
	r, e = Grade(d, "r", answers)
	if e != nil || r.Score != 2 || !r.Passed {
		t.Fatal(r, e)
	}
	for _, bad := range []map[string][]string{{"single": {"a"}, "multi": {"a", "a"}, "bool": {"false"}}, {"single": {"a"}, "multi": {"x"}, "bool": {"false"}}, {"single": {"a", "b"}, "multi": {"a"}, "bool": {"false"}}, {"single": {"a"}, "multi": {}, "bool": {"false"}}, {"single": {"a"}, "multi": {"a"}}} {
		if _, e = Grade(d, "r", bad); e == nil {
			t.Fatal(bad)
		}
	}
	d.ShowExplanations = false
	r, e = Grade(d, "r", answers)
	if e != nil || r.Feedback["single"].Explanation != "" || len(r.Feedback["single"].CorrectIDs) > 0 {
		t.Fatal(r, e)
	}
}

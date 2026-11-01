package httpserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/learning"
)

func TestAssessmentReviewHasActionableFieldReason(t *testing.T) {
	w := httptest.NewRecorder()
	domainError(w, learning.ErrReview)
	var body struct {
		Error struct {
			Code   string `json:"code"`
			Fields []struct {
				Path string `json:"path"`
				Code string `json:"code"`
			} `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 422 || body.Error.Code != "invalid_fields" || len(body.Error.Fields) != 1 || body.Error.Fields[0].Path != "quiz" || body.Error.Fields[0].Code != "assessment_review" {
		t.Fatal(w.Code, w.Body.String())
	}
}

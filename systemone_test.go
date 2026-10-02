package jev_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/oplachkoam/jev-go-sdk"
)

const fullResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "urgent": {"type": "noul", "noul": 0.95},
    "team": {
      "type": "choice",
      "choice": "billing",
      "probabilities": {"billing": 0.88, "technical": 0.12, "sales": 0.0},
      "confidence": 0.81
    },
    "frustration": {
      "type": "score",
      "score": 1.05,
      "legend": {"0": "Calm", "1": "Frustrated", "2": "Very angry"},
      "probabilities": {"0": 0.0, "1": 0.95, "2": 0.05},
      "confidence": 0.92
    }
  },
  "usage": {"input_tokens": 318, "output_tokens": 34, "cost": 0.0000134}
}`

func TestSystemOneRequestBody(t *testing.T) {
	var body string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		writeJSON(w, http.StatusOK, fullResponse)
	})

	_, err := client.SystemOne.New(context.Background(), jev.SystemOneNewParams{
		State: map[string]any{"ticket": "Help! My payouts have been failing for 3 days."},
		Model: jev.ModelJev1_13_0,
		Questions: map[string]jev.QuestionUnionParam{
			"urgent": {OfNoul: &jev.NoulQuestionParam{
				Instructions: "Does this convey urgency?",
				Criteria:     jev.NoulCriteriaParam{True: "Explicitly time-sensitive", False: "No urgency expressed"},
			}},
			// Deliberately not in alphabetical order, to see the order survive.
			"team": jev.QuestionParamOfChoice("Which team should handle this?",
				jev.ChoiceOptionParam{Name: "technical", Description: "Bugs, outages, integrations"},
				jev.ChoiceOptionParam{Name: "billing", Description: map[string]any{"covers": "Payments"}},
				jev.ChoiceOptionParam{Name: "sales"},
			),
			"frustration": jev.QuestionParamOfScore(
				map[string]any{"question": "How frustrated is the customer?"},
				"Calm", "Frustrated", "Very angry",
			),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{"state":{"ticket":"Help! My payouts have been failing for 3 days."},"model":"jev-1.13.0","questions":{` +
		`"frustration":{"type":"score","instructions":{"question":"How frustrated is the customer?"},"criteria":["Calm","Frustrated","Very angry"]},` +
		`"team":{"type":"choice","instructions":"Which team should handle this?","criteria":{"technical":"Bugs, outages, integrations","billing":{"covers":"Payments"},"sales":null}},` +
		`"urgent":{"type":"noul","instructions":"Does this convey urgency?","criteria":{"true":"Explicitly time-sensitive","false":"No urgency expressed"}}}}`
	if body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

func TestQuestionParamOmitsWhatIsUnset(t *testing.T) {
	tests := []struct {
		question jev.QuestionUnionParam
		want     string
	}{
		{jev.QuestionParamOfNoul("Is it urgent?"), `{"type":"noul","instructions":"Is it urgent?"}`},
		{jev.QuestionParamOfNoul(nil), `{"type":"noul"}`},
		{
			jev.QuestionUnionParam{OfNoul: &jev.NoulQuestionParam{Instructions: "q", Criteria: jev.NoulCriteriaParam{True: "yes means this"}}},
			`{"type":"noul","instructions":"q","criteria":{"true":"yes means this"}}`,
		},
		{
			jev.QuestionParamOfChoice(nil, jev.ChoiceOptionParam{Name: `quo"te`}),
			`{"type":"choice","criteria":{"quo\"te":null}}`,
		},
		{jev.QuestionParamOfScore(nil, "low", "high"), `{"type":"score","criteria":["low","high"]}`},
	}
	for _, tt := range tests {
		got, err := json.Marshal(tt.question)
		if err != nil {
			t.Errorf("want %s: unexpected error: %v", tt.want, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
	}
}

func TestSystemOneValidation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an invalid request must not be sent")
		writeJSON(w, http.StatusOK, fullResponse)
	})

	noul := jev.NoulQuestionParam{Instructions: "q"}
	score := jev.ScoreQuestionParam{Instructions: "q", Criteria: []any{"low", "high"}}
	tests := map[string]struct {
		questions map[string]jev.QuestionUnionParam
		want      string
	}{
		"no questions":      {nil, "at least one question"},
		"empty union":       {map[string]jev.QuestionUnionParam{"k": {}}, "exactly one of"},
		"two variants":      {map[string]jev.QuestionUnionParam{"k": {OfNoul: &noul, OfScore: &score}}, "exactly one of"},
		"one score level":   {map[string]jev.QuestionUnionParam{"k": jev.QuestionParamOfScore("q", "only")}, "at least two levels"},
		"no choice options": {map[string]jev.QuestionUnionParam{"k": jev.QuestionParamOfChoice("q")}, "at least one option"},
		"duplicate options": {map[string]jev.QuestionUnionParam{"k": jev.QuestionParamOfChoice("q",
			jev.ChoiceOptionParam{Name: "a"}, jev.ChoiceOptionParam{Name: "a"},
		)}, "more than once"},
	}
	for name, tt := range tests {
		_, err := client.SystemOne.New(context.Background(), jev.SystemOneNewParams{State: "s", Questions: tt.questions})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want an error mentioning %q", name, err, tt.want)
		}
	}
}

func TestSystemOneResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, fullResponse)
	})

	res, err := client.SystemOne.New(context.Background(), noulParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Model != "jev-1.13.0" {
		t.Errorf("Model = %q", res.Model)
	}
	if res.Usage.InputTokens != 318 || res.Usage.OutputTokens != 34 {
		t.Errorf("Usage = %+v", res.Usage)
	}
	if res.RawJSON() != fullResponse {
		t.Errorf("RawJSON() does not return the body as received")
	}
	// A field this library does not model, as sent by API gateways.
	if cost := res.Usage.JSON.ExtraFields["cost"]; !cost.Valid() || cost.Raw() != "0.0000134" {
		t.Errorf("extra field cost = %+v", cost)
	}
	if !res.JSON.Model.Valid() || res.JSON.Model.Raw() != `"jev-1.13.0"` {
		t.Errorf("metadata of Model = %+v", res.JSON.Model)
	}

	urgent := res.Answers["urgent"]
	if urgent.Type != jev.AnswerTypeNoul || urgent.Noul != 0.95 {
		t.Errorf("urgent = %+v", urgent)
	}
	if urgent.JSON.Confidence.Present() {
		t.Error("a noul answer has no confidence, but its metadata says it was sent")
	}
	if noul := urgent.AsNoul(); noul.Noul != 0.95 || noul.Type != "noul" {
		t.Errorf("AsNoul() = %+v", noul)
	}

	team := res.Answers["team"].AsChoice()
	if team.Choice != "billing" || team.Confidence != 0.81 {
		t.Errorf("AsChoice() = %+v", team)
	}
	if len(team.Probabilities) != 3 || team.Probabilities["billing"] != 0.88 || team.Probabilities["technical"] != 0.12 {
		t.Errorf("Probabilities = %v", team.Probabilities)
	}

	frustration := res.Answers["frustration"].AsScore()
	if frustration.Score != 1.05 || frustration.Confidence != 0.92 {
		t.Errorf("AsScore() = %+v", frustration)
	}
	levels := frustration.Levels()
	want := []jev.ScoreLevel{
		{Level: 0, Description: "Calm", Probability: 0},
		{Level: 1, Description: "Frustrated", Probability: 0.95},
		{Level: 2, Description: "Very angry", Probability: 0.05},
	}
	if len(levels) != len(want) {
		t.Fatalf("Levels() = %+v", levels)
	}
	for i := range want {
		if levels[i] != want[i] {
			t.Errorf("Levels()[%d] = %+v, want %+v", i, levels[i], want[i])
		}
	}

	for key, wantType := range map[string]string{"urgent": "noul", "team": "choice", "frustration": "score"} {
		var got string
		switch res.Answers[key].AsAny().(type) {
		case jev.NoulAnswer:
			got = "noul"
		case jev.ChoiceAnswer:
			got = "choice"
		case jev.ScoreAnswer:
			got = "score"
		}
		if got != wantType {
			t.Errorf("AsAny() of %q is a %q answer, want %q", key, got, wantType)
		}
	}
	if (jev.AnswerUnion{}).AsAny() != nil {
		t.Error("AsAny() of an empty union should be nil")
	}
}

func TestSystemOneRejectsMalformedResponse(t *testing.T) {
	for _, body := range []string{
		`not json`,
		`{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":"high"}},"usage":{}}`,
		`{"model":"jev-1.13.0","answers":[],"usage":{}}`,
	} {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, body)
		})
		if _, err := client.SystemOne.New(context.Background(), noulParams()); err == nil {
			t.Errorf("body %q: expected a decoding error", body)
		}
	}
}

func TestModelList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("got %s %s, want GET /v1/models", r.Method, r.URL.Path)
		}
		if r.ContentLength > 0 || r.Header.Get("Content-Type") != "" {
			t.Error("a GET without params must not carry a body")
		}
		writeJSON(w, http.StatusOK, `{"models":[
			{"name":"jev-latest","description":"The most recent stable release.","release_date":"2026-05-01"},
			{"name":"jev-preview","description":"The most recent release.","release_date":"2026-05-01"}
		]}`)
	})

	res, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Models) != 2 {
		t.Fatalf("got %d models, want 2", len(res.Models))
	}
	first := res.Models[0]
	if first.Name != jev.ModelJevLatest || first.Description != "The most recent stable release." || first.ReleaseDate != "2026-05-01" {
		t.Errorf("first model = %+v", first)
	}
	if !first.JSON.ReleaseDate.Valid() {
		t.Error("metadata of ReleaseDate was not recorded")
	}
}

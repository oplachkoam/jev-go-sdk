package jev_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/oplachkoam/jev-go-sdk"
)

type Team string

const (
	TeamBilling   Team = "billing"
	TeamTechnical Team = "technical"
	TeamSales     Team = "sales"
)

func typedQuestions() (jev.NoulQuestion, jev.ChoiceQuestion[Team], jev.ScoreQuestion) {
	urgent := jev.Noul("urgent", "Does this convey urgency?").
		WithCriteria("Explicitly time-sensitive", nil)
	team := jev.Choice("team", "Which team should handle this?",
		jev.Option(TeamTechnical, "Bugs, outages, integrations"),
		jev.Option(TeamBilling, "Payments, invoicing, refunds"),
		jev.Option(TeamSales, nil),
	)
	frustration := jev.Score("frustration", "How frustrated is the customer?", "Calm", "Frustrated", "Very angry")
	return urgent, team, frustration
}

func TestTypedQuestionsRoundTrip(t *testing.T) {
	var body string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		writeJSON(w, http.StatusOK, fullResponse)
	})

	urgent, team, frustration := typedQuestions()
	res, err := client.SystemOne.New(context.Background(), jev.SystemOneNewParams{
		State:     "Help! My payouts have been failing for 3 days.",
		Questions: jev.Questions(urgent, team, frustration),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{"state":"Help! My payouts have been failing for 3 days.","model":"jev-latest","questions":{` +
		`"frustration":{"type":"score","instructions":"How frustrated is the customer?","criteria":["Calm","Frustrated","Very angry"]},` +
		`"team":{"type":"choice","instructions":"Which team should handle this?","criteria":{"technical":"Bugs, outages, integrations","billing":"Payments, invoicing, refunds","sales":null}},` +
		`"urgent":{"type":"noul","instructions":"Does this convey urgency?","criteria":{"true":"Explicitly time-sensitive"}}}}`
	if body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}

	noul, err := urgent.Answer(res)
	if err != nil || noul.Noul != 0.95 {
		t.Errorf("urgent.Answer() = %+v, %v", noul, err)
	}

	choice, err := team.Answer(res)
	if err != nil {
		t.Fatalf("team.Answer(): unexpected error: %v", err)
	}
	// The assignments only compile if the answer has the type of the options.
	var picked Team = choice.Choice
	var probability float64 = choice.Probabilities[TeamBilling]
	if picked != TeamBilling || probability != 0.88 || choice.Confidence != 0.81 {
		t.Errorf("team.Answer() = %+v", choice)
	}
	if len(choice.Probabilities) != 3 {
		t.Errorf("Probabilities = %v", choice.Probabilities)
	}

	score, err := frustration.Answer(res)
	if err != nil || score.Score != 1.05 || len(score.Levels()) != 3 {
		t.Errorf("frustration.Answer() = %+v, %v", score, err)
	}
}

func TestTypedAnswerErrors(t *testing.T) {
	var res jev.SystemOneResponse
	if err := json.Unmarshal([]byte(fullResponse), &res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := jev.Noul("absent", "q").Answer(&res); !errors.Is(err, jev.ErrAnswerMissing) {
		t.Errorf("absent key: got %v, want ErrAnswerMissing", err)
	}
	if _, err := jev.Noul("urgent", "q").Answer(nil); !errors.Is(err, jev.ErrAnswerMissing) {
		t.Errorf("nil response: got %v, want ErrAnswerMissing", err)
	}
	// "team" holds a choice answer.
	if _, err := jev.Noul("team", "q").Answer(&res); !errors.Is(err, jev.ErrAnswerMismatch) {
		t.Errorf("noul over a choice answer: got %v, want ErrAnswerMismatch", err)
	}
	if _, err := jev.Score("team", "q", "a", "b").Answer(&res); !errors.Is(err, jev.ErrAnswerMismatch) {
		t.Errorf("score over a choice answer: got %v, want ErrAnswerMismatch", err)
	}
	if _, err := jev.Choice("urgent", "q", jev.Options(TeamBilling)...).Answer(&res); !errors.Is(err, jev.ErrAnswerMismatch) {
		t.Errorf("choice over a noul answer: got %v, want ErrAnswerMismatch", err)
	}
	// The answer picks "billing", which this question does not offer.
	narrow := jev.Choice("team", "q", jev.Options(TeamTechnical, TeamSales)...)
	if _, err := narrow.Answer(&res); !errors.Is(err, jev.ErrAnswerMismatch) {
		t.Errorf("unknown chosen option: got %v, want ErrAnswerMismatch", err)
	}
}

func TestOptionsHelper(t *testing.T) {
	question := jev.Choice("team", nil, jev.Options(TeamSales, TeamBilling)...)
	got, err := json.Marshal(question.Param())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := `{"type":"choice","criteria":{"sales":null,"billing":null}}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if question.Key() != "team" || question.Param().GetType() != jev.AnswerTypeChoice {
		t.Errorf("Key() = %q, GetType() = %q", question.Key(), question.Param().GetType())
	}
}

func TestWithCriteriaDoesNotModifyTheOriginal(t *testing.T) {
	plain := jev.Noul("k", "q")
	_ = plain.WithCriteria("yes", "no")
	got, _ := json.Marshal(plain.Param())
	if want := `{"type":"noul","instructions":"q"}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestQuestionsPanicsOnDuplicateKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a key used twice")
		}
	}()
	jev.Questions(jev.Noul("same", "first"), jev.Score("same", "second", "low", "high"))
}

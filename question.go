package jev

import (
	"errors"
	"fmt"
)

var (
	// ErrAnswerMissing is returned by the Answer method of a typed question when
	// the response holds no answer under the key of the question.
	ErrAnswerMissing = errors.New("jev: answer is missing from the response")
	// ErrAnswerMismatch is returned by the Answer method of a typed question when
	// the answer under its key does not fit the question, for example because it
	// has another type or names an option the question did not define.
	ErrAnswerMismatch = errors.New("jev: answer does not match the question")
)

// Question is a typed question: a key paired with the question asked under it.
// It is implemented by [NoulQuestion], [ChoiceQuestion] and [ScoreQuestion],
// whose Answer methods read the answer back from a response with its type.
type Question interface {
	// Key returns the name the question is asked under.
	Key() string
	// Param returns the question in its wire form.
	Param() QuestionUnionParam
}

// Questions collects typed questions into the map expected by
// [SystemOneNewParams], keyed by [Question.Key].
//
// Questions panics if two questions share a key, since one of them would
// silently replace the other.
func Questions(questions ...Question) map[string]QuestionUnionParam {
	params := make(map[string]QuestionUnionParam, len(questions))
	for _, question := range questions {
		key := question.Key()
		if _, ok := params[key]; ok {
			panic(fmt.Sprintf("jev: question key %q is used more than once", key))
		}
		params[key] = question.Param()
	}
	return params
}

// lookupAnswer finds the answer to the question asked under key and checks
// that it has the wanted type.
func lookupAnswer(res *SystemOneResponse, key string, want string) (AnswerUnion, error) {
	if res == nil {
		return AnswerUnion{}, fmt.Errorf("%w: question %q: response is nil", ErrAnswerMissing, key)
	}
	answer, ok := res.Answers[key]
	if !ok {
		return AnswerUnion{}, fmt.Errorf("%w: question %q", ErrAnswerMissing, key)
	}
	if answer.Type != want {
		return AnswerUnion{}, fmt.Errorf("%w: question %q: want a %s answer, got %q", ErrAnswerMismatch, key, want, answer.Type)
	}
	return answer, nil
}

// NoulQuestion is a typed yes/no question. Create one with [Noul].
type NoulQuestion struct {
	key   string
	param NoulQuestionParam
}

// Noul returns a yes/no question asked under the given key.
//
// The instructions are the question itself: a string, or a JSON-encodable
// object or array that holds the question together with the data it refers to.
func Noul(key string, instructions any) NoulQuestion {
	return NoulQuestion{key: key, param: NoulQuestionParam{Instructions: instructions}}
}

// WithCriteria returns a copy of the question that also describes what a yes
// and what a no mean. Either description may be nil to leave it out.
func (q NoulQuestion) WithCriteria(yes, no any) NoulQuestion {
	q.param.Criteria = NoulCriteriaParam{True: yes, False: no}
	return q
}

func (q NoulQuestion) Key() string { return q.key }

func (q NoulQuestion) Param() QuestionUnionParam {
	param := q.param
	return QuestionUnionParam{OfNoul: &param}
}

// Answer returns the answer to the question from a response to a request that
// asked it. The error wraps [ErrAnswerMissing] or [ErrAnswerMismatch].
func (q NoulQuestion) Answer(res *SystemOneResponse) (NoulAnswer, error) {
	answer, err := lookupAnswer(res, q.key, AnswerTypeNoul)
	if err != nil {
		return NoulAnswer{}, err
	}
	return answer.AsNoul(), nil
}

// ChoiceOption is one option of a typed [ChoiceQuestion].
type ChoiceOption[T ~string] struct {
	// The value of the option. It is what the answer reports when the option is
	// chosen.
	Value T
	// When the option applies: a string, or a JSON-encodable object or array.
	// Leave it nil when the value needs no explanation.
	Description any
}

// Option returns an option for [Choice]. The description says when the option
// applies and may be nil when the value needs no explanation.
func Option[T ~string](value T, description any) ChoiceOption[T] {
	return ChoiceOption[T]{Value: value, Description: description}
}

// Options returns one option without a description for each value, for choices
// whose values need no explanation:
//
//	jev.Choice("tone", "What is the tone?", jev.Options(Calm, Tense, Angry)...)
func Options[T ~string](values ...T) []ChoiceOption[T] {
	options := make([]ChoiceOption[T], len(values))
	for i, value := range values {
		options[i] = ChoiceOption[T]{Value: value}
	}
	return options
}

// ChoiceQuestion is a typed question that picks one option of type T. Create
// one with [Choice].
type ChoiceQuestion[T ~string] struct {
	key   string
	param ChoiceQuestionParam
}

// Choice returns a question, asked under the given key, that picks one of the
// options. The options are sent in the order given.
//
// T is usually a string type with one constant per option, which makes the
// answer a value of that type:
//
//	type Team string
//
//	const (
//		Billing   Team = "billing"
//		Technical Team = "technical"
//	)
//
//	team := jev.Choice("team", "Which team should handle this?",
//		jev.Option(Billing, "Payments, invoicing, refunds"),
//		jev.Option(Technical, "Bugs, outages, integrations"),
//	)
func Choice[T ~string](key string, instructions any, options ...ChoiceOption[T]) ChoiceQuestion[T] {
	criteria := make([]ChoiceOptionParam, len(options))
	for i, option := range options {
		criteria[i] = ChoiceOptionParam{Name: string(option.Value), Description: option.Description}
	}
	return ChoiceQuestion[T]{
		key:   key,
		param: ChoiceQuestionParam{Instructions: instructions, Criteria: criteria},
	}
}

func (q ChoiceQuestion[T]) Key() string { return q.key }

func (q ChoiceQuestion[T]) Param() QuestionUnionParam {
	param := q.param
	return QuestionUnionParam{OfChoice: &param}
}

// Answer returns the answer to the question from a response to a request that
// asked it. The error wraps [ErrAnswerMissing] or [ErrAnswerMismatch]; the
// latter also covers an answer that names an option the question did not
// define.
func (q ChoiceQuestion[T]) Answer(res *SystemOneResponse) (TypedChoiceAnswer[T], error) {
	answer, err := lookupAnswer(res, q.key, AnswerTypeChoice)
	if err != nil {
		return TypedChoiceAnswer[T]{}, err
	}
	defined := make(map[string]struct{}, len(q.param.Criteria))
	for _, option := range q.param.Criteria {
		defined[option.Name] = struct{}{}
	}
	if _, ok := defined[answer.Choice]; !ok {
		return TypedChoiceAnswer[T]{}, fmt.Errorf("%w: question %q: chosen option %q is not one of its options", ErrAnswerMismatch, q.key, answer.Choice)
	}
	probabilities := make(map[T]float64, len(answer.Probabilities))
	for name, probability := range answer.Probabilities {
		if _, ok := defined[name]; !ok {
			return TypedChoiceAnswer[T]{}, fmt.Errorf("%w: question %q: probability for %q, which is not one of its options", ErrAnswerMismatch, q.key, name)
		}
		probabilities[T(name)] = probability
	}
	return TypedChoiceAnswer[T]{
		Choice:        T(answer.Choice),
		Probabilities: probabilities,
		Confidence:    answer.Confidence,
	}, nil
}

// TypedChoiceAnswer is a [ChoiceAnswer] whose options have the type of the
// [ChoiceQuestion] that was asked.
type TypedChoiceAnswer[T ~string] struct {
	// The option with the highest probability.
	Choice T
	// Every option mapped to its probability. The probabilities sum to 1.
	Probabilities map[T]float64
	// How certain the model is, from 0 to 1, derived from the spread of the
	// probabilities.
	Confidence float64
}

// ScoreQuestion is a typed question that rates the state on a scale. Create one
// with [Score].
type ScoreQuestion struct {
	key   string
	param ScoreQuestionParam
}

// Score returns a question, asked under the given key, that rates the state on
// a scale.
//
// The levels are the descriptions of the points of the scale, from its low end
// to its high end. Each is a string, or a JSON-encodable object or array. A
// Score needs at least two levels.
func Score(key string, instructions any, levels ...any) ScoreQuestion {
	return ScoreQuestion{key: key, param: ScoreQuestionParam{Instructions: instructions, Criteria: levels}}
}

func (q ScoreQuestion) Key() string { return q.key }

func (q ScoreQuestion) Param() QuestionUnionParam {
	param := q.param
	return QuestionUnionParam{OfScore: &param}
}

// Answer returns the answer to the question from a response to a request that
// asked it. The error wraps [ErrAnswerMissing] or [ErrAnswerMismatch].
func (q ScoreQuestion) Answer(res *SystemOneResponse) (ScoreAnswer, error) {
	answer, err := lookupAnswer(res, q.key, AnswerTypeScore)
	if err != nil {
		return ScoreAnswer{}, err
	}
	return answer.AsScore(), nil
}

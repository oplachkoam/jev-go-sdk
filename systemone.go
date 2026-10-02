package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/oplachkoam/jev-go-sdk/internal/apijson"
	"github.com/oplachkoam/jev-go-sdk/internal/requestconfig"
	"github.com/oplachkoam/jev-go-sdk/option"
	"github.com/oplachkoam/jev-go-sdk/packages/respjson"
)

// SystemOneService contains methods and other services that help with interacting
// with the TypeSafe API.
//
// Note, unlike clients, this service does not read variables from the environment
// automatically. You should not instantiate this service directly, and instead use
// the [NewSystemOneService] method instead.
type SystemOneService struct {
	Options []option.RequestOption
}

// NewSystemOneService generates a new service that applies the given options to
// each request. These options are applied after the parent client's options (if
// there is one), and before any request-specific options.
func NewSystemOneService(opts ...option.RequestOption) (r SystemOneService) {
	r = SystemOneService{}
	r.Options = opts
	return
}

// Evaluates a state against a map of typed questions and returns one structured
// answer per question, under the same keys.
//
// The questions are validated before the request is sent: there must be at
// least one, every [QuestionUnionParam] must hold exactly one variant, a Choice
// needs at least one option and distinct option names, and a Score needs at
// least two levels.
func (r *SystemOneService) New(ctx context.Context, body SystemOneNewParams, opts ...option.RequestOption) (res *SystemOneResponse, err error) {
	opts = append(r.Options[:len(r.Options):len(r.Options)], opts...)
	if err = body.validate(); err != nil {
		return nil, err
	}
	if body.Model == "" {
		precfg, err := requestconfig.PreRequestOptions(opts...)
		if err != nil {
			return nil, err
		}
		body.Model = precfg.DefaultModel
		if body.Model == "" {
			body.Model = ModelJevLatest
		}
	}
	path := "v1/systemone"
	err = requestconfig.ExecuteNewRequest(ctx, http.MethodPost, path, body, &res, opts...)
	return
}

type SystemOneNewParams struct {
	// The content to evaluate. A plain string for text, or any value that
	// [encoding/json] encodes as an object or an array for structured data such as
	// chat logs, records, or the current state of your application.
	State any
	// The questions to answer, keyed by names you choose. Each answer comes back
	// under the key of its question. The keys are not shown to the model.
	//
	// Build the map by hand from [QuestionUnionParam] values, or with [Questions]
	// from typed questions.
	Questions map[string]QuestionUnionParam
	// The model that handles the request. Defaults to the model set with
	// [option.WithDefaultModel], and to [ModelJevLatest] without one.
	Model Model
}

func (r SystemOneNewParams) MarshalJSON() ([]byte, error) {
	type shadow struct {
		State     any                           `json:"state"`
		Model     Model                         `json:"model,omitempty"`
		Questions map[string]QuestionUnionParam `json:"questions"`
	}
	return json.Marshal(shadow{State: r.State, Model: r.Model, Questions: r.Questions})
}

func (r SystemOneNewParams) validate() error {
	if len(r.Questions) == 0 {
		return errors.New("jev: at least one question is required")
	}
	for key, question := range r.Questions {
		if err := question.validate(); err != nil {
			return fmt.Errorf("jev: question %q: %w", key, err)
		}
	}
	return nil
}

// A typed question about the state. Only one field can be non-zero.
//
// Use the helpers [QuestionParamOfNoul], [QuestionParamOfChoice] and
// [QuestionParamOfScore] to build one, or set the field of the variant you
// need.
type QuestionUnionParam struct {
	OfNoul   *NoulQuestionParam
	OfChoice *ChoiceQuestionParam
	OfScore  *ScoreQuestionParam
}

// QuestionParamOfNoul returns a yes/no question. The instructions are the
// question itself: a string, or a JSON-encodable object or array that holds the
// question together with the data it refers to.
func QuestionParamOfNoul(instructions any) QuestionUnionParam {
	return QuestionUnionParam{OfNoul: &NoulQuestionParam{Instructions: instructions}}
}

// QuestionParamOfChoice returns a question that picks one of the given options.
// The options are sent in the order given.
func QuestionParamOfChoice(instructions any, options ...ChoiceOptionParam) QuestionUnionParam {
	return QuestionUnionParam{OfChoice: &ChoiceQuestionParam{Instructions: instructions, Criteria: options}}
}

// QuestionParamOfScore returns a question that rates the state on a scale. The
// levels are the descriptions of the points of the scale, from its low end to
// its high end.
func QuestionParamOfScore(instructions any, levels ...any) QuestionUnionParam {
	return QuestionUnionParam{OfScore: &ScoreQuestionParam{Instructions: instructions, Criteria: levels}}
}

func (u QuestionUnionParam) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}
	switch {
	case u.OfNoul != nil:
		return json.Marshal(u.OfNoul)
	case u.OfChoice != nil:
		return json.Marshal(u.OfChoice)
	default:
		return json.Marshal(u.OfScore)
	}
}

// GetType returns the type of the variant that is set, one of "noul", "choice"
// or "score", or an empty string if none is.
func (u QuestionUnionParam) GetType() string {
	switch {
	case u.OfNoul != nil:
		return AnswerTypeNoul
	case u.OfChoice != nil:
		return AnswerTypeChoice
	case u.OfScore != nil:
		return AnswerTypeScore
	}
	return ""
}

func (u QuestionUnionParam) validate() error {
	set := 0
	if u.OfNoul != nil {
		set++
	}
	if u.OfChoice != nil {
		set++
	}
	if u.OfScore != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("exactly one of OfNoul, OfChoice and OfScore must be set, got %d", set)
	}
	switch {
	case u.OfChoice != nil:
		return u.OfChoice.validate()
	case u.OfScore != nil:
		return u.OfScore.validate()
	}
	return nil
}

// A yes/no question. The answer is the probability that the answer is yes.
type NoulQuestionParam struct {
	// The yes/no question to evaluate: a string, or a JSON-encodable object or
	// array. An object can hold the question in one field and the data it refers
	// to in others.
	Instructions any
	// Optional descriptions of what a yes and a no mean.
	Criteria NoulCriteriaParam
}

// Descriptions of the outcomes of a [NoulQuestionParam]. Each is a string, or a
// JSON-encodable object or array, and is left out of the request when nil.
type NoulCriteriaParam struct {
	// What a yes (a value near 1) means.
	True any
	// What a no (a value near 0) means.
	False any
}

func (r NoulQuestionParam) MarshalJSON() ([]byte, error) {
	type criteria struct {
		True  any `json:"true,omitempty"`
		False any `json:"false,omitempty"`
	}
	type shadow struct {
		Type         string    `json:"type"`
		Instructions any       `json:"instructions,omitempty"`
		Criteria     *criteria `json:"criteria,omitempty"`
	}
	out := shadow{Type: AnswerTypeNoul, Instructions: r.Instructions}
	if r.Criteria.True != nil || r.Criteria.False != nil {
		out.Criteria = &criteria{True: r.Criteria.True, False: r.Criteria.False}
	}
	return json.Marshal(out)
}

// A question that picks one option from a set. The answer is the chosen option
// together with the probability of every option.
type ChoiceQuestionParam struct {
	// What the model should decide: a string, or a JSON-encodable object or array.
	Instructions any
	// The options to choose from, sent in this order. The API accepts up to 255
	// options per question.
	Criteria []ChoiceOptionParam
}

// One option of a [ChoiceQuestionParam].
type ChoiceOptionParam struct {
	// The name of the option. It is the value the answer reports when the option
	// is chosen.
	Name string
	// When the option applies: a string, or a JSON-encodable object or array.
	// Leave it nil when the name needs no explanation.
	Description any
}

// MarshalJSON encodes the options as a JSON object whose keys keep the order of
// Criteria, which a Go map would not.
func (r ChoiceQuestionParam) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"type":"choice"`)
	if r.Instructions != nil {
		instructions, err := json.Marshal(r.Instructions)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"instructions":`)
		buf.Write(instructions)
	}
	buf.WriteString(`,"criteria":{`)
	for i, option := range r.Criteria {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(option.Name)
		if err != nil {
			return nil, err
		}
		description, err := json.Marshal(option.Description)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(description)
	}
	buf.WriteString(`}}`)
	return buf.Bytes(), nil
}

func (r ChoiceQuestionParam) validate() error {
	if len(r.Criteria) == 0 {
		return errors.New("a choice needs at least one option")
	}
	seen := make(map[string]struct{}, len(r.Criteria))
	for _, option := range r.Criteria {
		if _, ok := seen[option.Name]; ok {
			return fmt.Errorf("option %q is defined more than once", option.Name)
		}
		seen[option.Name] = struct{}{}
	}
	return nil
}

// A question that rates the state along a scale of described levels. The answer
// is a probability-weighted position on the scale.
type ScoreQuestionParam struct {
	// What the model should rate: a string, or a JSON-encodable object or array.
	Instructions any
	// The descriptions of the levels, ordered from the low end of the scale to
	// the high end. Each is a string, or a JSON-encodable object or array. The
	// number of a level is its index here, starting at 0. A Score needs at least
	// two levels; the API accepts up to 10.
	Criteria []any
}

func (r ScoreQuestionParam) MarshalJSON() ([]byte, error) {
	type shadow struct {
		Type         string `json:"type"`
		Instructions any    `json:"instructions,omitempty"`
		Criteria     []any  `json:"criteria"`
	}
	return json.Marshal(shadow{Type: AnswerTypeScore, Instructions: r.Instructions, Criteria: r.Criteria})
}

func (r ScoreQuestionParam) validate() error {
	if len(r.Criteria) < 2 {
		return fmt.Errorf("a score needs at least two levels, got %d", len(r.Criteria))
	}
	return nil
}

// The values of the type field of questions and answers.
const (
	AnswerTypeNoul   = "noul"
	AnswerTypeChoice = "choice"
	AnswerTypeScore  = "score"
)

type SystemOneResponse struct {
	// The versioned ID of the model that performed the evaluation, which differs
	// from the requested name when that was an alias.
	Model string `json:"model,required"`
	// One answer per question, keyed by the names used in the request.
	Answers map[string]AnswerUnion `json:"answers,required"`
	// Token usage for the request.
	Usage Usage `json:"usage,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		Model       respjson.Field
		Answers     respjson.Field
		Usage       respjson.Field
		ExtraFields map[string]respjson.Field
		raw         string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r SystemOneResponse) RawJSON() string { return r.JSON.raw }
func (r *SystemOneResponse) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// Token usage for a request. Requests are billed per input token.
type Usage struct {
	InputTokens  int64 `json:"input_tokens,required"`
	OutputTokens int64 `json:"output_tokens,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		InputTokens  respjson.Field
		OutputTokens respjson.Field
		ExtraFields  map[string]respjson.Field
		raw          string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r Usage) RawJSON() string { return r.JSON.raw }
func (r *Usage) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// AnswerUnion contains all possible properties and values from [NoulAnswer],
// [ChoiceAnswer], [ScoreAnswer].
//
// Use the [AnswerUnion.AsAny] method to switch on the variant.
//
// Use the methods beginning with 'As' to cast the union to one of its variants.
type AnswerUnion struct {
	// Any of "noul", "choice", "score".
	Type string `json:"type"`
	// This field is from variant [NoulAnswer].
	Noul float64 `json:"noul"`
	// This field is from variant [ChoiceAnswer].
	Choice string `json:"choice"`
	// This field is from variant [ScoreAnswer].
	Score float64 `json:"score"`
	// This field is from variant [ScoreAnswer].
	Legend map[string]any `json:"legend"`
	// This field is from variants [ChoiceAnswer] and [ScoreAnswer].
	Probabilities map[string]float64 `json:"probabilities"`
	// This field is from variants [ChoiceAnswer] and [ScoreAnswer].
	Confidence float64 `json:"confidence"`
	JSON       struct {
		Type          respjson.Field
		Noul          respjson.Field
		Choice        respjson.Field
		Score         respjson.Field
		Legend        respjson.Field
		Probabilities respjson.Field
		Confidence    respjson.Field
		ExtraFields   map[string]respjson.Field
		raw           string
	} `json:"-"`
}

// anyAnswer is implemented by each variant of [AnswerUnion] to add type safety
// for the return type of [AnswerUnion.AsAny]
type anyAnswer interface {
	implAnswerUnion()
}

func (NoulAnswer) implAnswerUnion()   {}
func (ChoiceAnswer) implAnswerUnion() {}
func (ScoreAnswer) implAnswerUnion()  {}

// Use the following switch statement to find the correct variant
//
//	switch variant := AnswerUnion.AsAny().(type) {
//	case jev.NoulAnswer:
//	case jev.ChoiceAnswer:
//	case jev.ScoreAnswer:
//	default:
//	  fmt.Errorf("no variant present")
//	}
func (u AnswerUnion) AsAny() anyAnswer {
	switch u.Type {
	case AnswerTypeNoul:
		return u.AsNoul()
	case AnswerTypeChoice:
		return u.AsChoice()
	case AnswerTypeScore:
		return u.AsScore()
	}
	return nil
}

func (u AnswerUnion) AsNoul() (v NoulAnswer) {
	json.Unmarshal([]byte(u.JSON.raw), &v)
	return
}

func (u AnswerUnion) AsChoice() (v ChoiceAnswer) {
	json.Unmarshal([]byte(u.JSON.raw), &v)
	return
}

func (u AnswerUnion) AsScore() (v ScoreAnswer) {
	json.Unmarshal([]byte(u.JSON.raw), &v)
	return
}

// Returns the unmodified JSON received from the API
func (u AnswerUnion) RawJSON() string { return u.JSON.raw }

func (r *AnswerUnion) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// The answer to a yes/no question.
//
// A Noul has no separate confidence: its distribution has only two outcomes, so
// the single value describes it completely.
type NoulAnswer struct {
	// The probability that the answer is yes, from 0 (no) to 1 (yes).
	Noul float64 `json:"noul,required"`
	// Always "noul".
	Type string `json:"type,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		Noul        respjson.Field
		Type        respjson.Field
		ExtraFields map[string]respjson.Field
		raw         string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r NoulAnswer) RawJSON() string { return r.JSON.raw }
func (r *NoulAnswer) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// The answer to a Choice question.
type ChoiceAnswer struct {
	// The option with the highest probability.
	Choice string `json:"choice,required"`
	// Every option mapped to its probability. The probabilities sum to 1.
	Probabilities map[string]float64 `json:"probabilities,required"`
	// How certain the model is, from 0 to 1, derived from the spread of the
	// probabilities.
	Confidence float64 `json:"confidence,required"`
	// Always "choice".
	Type string `json:"type,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		Choice        respjson.Field
		Probabilities respjson.Field
		Confidence    respjson.Field
		Type          respjson.Field
		ExtraFields   map[string]respjson.Field
		raw           string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r ChoiceAnswer) RawJSON() string { return r.JSON.raw }
func (r *ChoiceAnswer) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// The answer to a Score question.
type ScoreAnswer struct {
	// The probability-weighted position on the scale. It runs from 0 to the
	// number of the last level and can land between two levels.
	Score float64 `json:"score,required"`
	// Each level number mapped back to its description, which is whatever was
	// sent for that level: a string, or a decoded JSON object or array.
	Legend map[string]any `json:"legend,required"`
	// Each level number mapped to its probability. The probabilities sum to 1.
	// See [ScoreAnswer.Levels] for the same data ordered by level.
	Probabilities map[string]float64 `json:"probabilities,required"`
	// How certain the model is, from 0 to 1, derived from the spread of the
	// probabilities.
	Confidence float64 `json:"confidence,required"`
	// Always "score".
	Type string `json:"type,required"`
	// JSON contains metadata for fields, check presence with [respjson.Field.Valid].
	JSON struct {
		Score         respjson.Field
		Legend        respjson.Field
		Probabilities respjson.Field
		Confidence    respjson.Field
		Type          respjson.Field
		ExtraFields   map[string]respjson.Field
		raw           string
	} `json:"-"`
}

// Returns the unmodified JSON received from the API
func (r ScoreAnswer) RawJSON() string { return r.JSON.raw }
func (r *ScoreAnswer) UnmarshalJSON(data []byte) error {
	r.JSON.raw = string(data)
	return apijson.UnmarshalRoot(data, r)
}

// One level of the scale of a [ScoreAnswer].
type ScoreLevel struct {
	// The number of the level: its position in the criteria of the question,
	// starting at 0.
	Level int
	// The description sent for the level.
	Description any
	// The probability that the state is at this level.
	Probability float64
}

// Levels returns the levels of the scale ordered from the low end to the high
// end, pairing each description of the legend with its probability.
func (r ScoreAnswer) Levels() []ScoreLevel {
	numbers := make(map[int]struct{}, len(r.Probabilities))
	for key := range r.Probabilities {
		if n, err := strconv.Atoi(key); err == nil {
			numbers[n] = struct{}{}
		}
	}
	for key := range r.Legend {
		if n, err := strconv.Atoi(key); err == nil {
			numbers[n] = struct{}{}
		}
	}
	levels := make([]ScoreLevel, 0, len(numbers))
	for n := range numbers {
		key := strconv.Itoa(n)
		levels = append(levels, ScoreLevel{
			Level:       n,
			Description: r.Legend[key],
			Probability: r.Probabilities[key],
		})
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i].Level < levels[j].Level })
	return levels
}

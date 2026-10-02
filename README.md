# jev-go-sdk

A Go client for [Jev](https://docs.typesafe.ai/introduction), the System One model by TypeSafe AI.

Jev does not generate text. You send a **state** (the content to evaluate) and a set of named, typed **questions**, and get back one structured, probabilistic **answer** per question, usually in well under a second.

The library follows the conventions of [openai-go](https://github.com/openai/openai-go): a client with services, functional request options, typed params and responses, and union types with `Of…` and `As…()` accessors. It has no dependencies outside the standard library.

> This is a community SDK. It is not affiliated with or endorsed by TypeSafe AI.

## Installation

```sh
go get github.com/oplachkoam/jev-go-sdk
```

Requires Go 1.22 or newer.

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/oplachkoam/jev-go-sdk"
	"github.com/oplachkoam/jev-go-sdk/option"
)

type Team string

const (
	TeamBilling   Team = "billing"
	TeamTechnical Team = "technical"
)

func main() {
	client := jev.NewClient(
		option.WithAPIKey("my-api-key"), // defaults to os.Getenv("TYPESAFE_API_KEY")
	)

	urgent := jev.Noul("urgent", "Does this convey urgency?")
	team := jev.Choice("team", "Which team should handle this?",
		jev.Option(TeamBilling, "Payments, invoicing, refunds"),
		jev.Option(TeamTechnical, "Bugs, outages, integrations"),
	)
	frustration := jev.Score("frustration", "How frustrated is the customer?",
		"Calm", "Frustrated", "Very angry",
	)

	res, err := client.SystemOne.New(context.Background(), jev.SystemOneNewParams{
		State:     "Help! My payouts have been failing for 3 days.",
		Questions: jev.Questions(urgent, team, frustration),
	})
	if err != nil {
		log.Fatal(err)
	}

	isUrgent, _ := urgent.Answer(res)  // jev.NoulAnswer
	picked, _ := team.Answer(res)      // jev.TypedChoiceAnswer[Team]
	mood, _ := frustration.Answer(res) // jev.ScoreAnswer

	fmt.Println(isUrgent.Noul)                       // 0.95
	fmt.Println(picked.Choice == TeamBilling)        // true
	fmt.Println(picked.Probabilities[TeamTechnical]) // 0.12
	fmt.Println(mood.Score, mood.Confidence)         // 1.05 0.92
}
```

More complete programs are in [examples/](examples/).

## Questions and answers

| Question | Asks | Answer |
| --- | --- | --- |
| Noul | A yes/no question | `Noul`: the probability of yes, from 0 to 1 |
| Choice | To pick one option from a set | `Choice`, a probability for every option, and `Confidence` |
| Score | To place the state on an ordered scale of 2 to 10 described levels | `Score`: a probability-weighted position that can land between levels, a probability for every level, and `Confidence` |

The **state** is a string, or any value that `encoding/json` encodes as an object or an array: a struct, a map, a slice. The same goes for the **instructions** of a question and for the descriptions of options and levels, so a question can carry the data it refers to:

```go
duplicate := jev.Noul("duplicate", map[string]any{
	"question":            "Is the resume for the same person as `potential_duplicate`?",
	"potential_duplicate": map[string]any{"name": "John Smith", "location": "Oakland, California"},
})
```

There are two ways to ask questions. They produce the same requests and can be mixed.

### Typed questions

`jev.Noul`, `jev.Choice` and `jev.Score` return a question that remembers its key and knows the type of its answer. `jev.Questions` collects them into the map that the request takes, and each question reads its own answer back from the response:

```go
picked, err := team.Answer(res)
```

`Answer` returns an error wrapping `jev.ErrAnswerMissing` if the response has no answer under the key, and one wrapping `jev.ErrAnswerMismatch` if the answer does not fit the question, for instance if it names an option the question did not define.

A `Choice` is generic over the type of its options. With a string type that has a constant per option, the chosen option and the keys of `Probabilities` are values of that type, so the compiler checks the comparisons you make with them. Options are sent in the order given. When the names need no description:

```go
tone := jev.Choice("tone", "What is the tone?", jev.Options(Calm, Tense, Angry)...)
```

A Noul can describe what a yes and a no mean:

```go
repeat := jev.Noul("repeat", "Has the customer contacted us about this before?").
	WithCriteria("They mention an earlier ticket, call or email", "This is their first contact")
```

`jev.Questions` panics if two questions share a key.

### Union types

When the questions are not known at compile time, build the map yourself from `jev.QuestionUnionParam`, either with the `QuestionParamOf…` helpers or by setting one of its `OfNoul`, `OfChoice` and `OfScore` fields:

```go
res, err := client.SystemOne.New(ctx, jev.SystemOneNewParams{
	State: ticket,
	Questions: map[string]jev.QuestionUnionParam{
		"urgent": jev.QuestionParamOfNoul("Does this convey urgency?"),
		"team": {OfChoice: &jev.ChoiceQuestionParam{
			Instructions: "Which team should handle this?",
			Criteria: []jev.ChoiceOptionParam{
				{Name: "billing", Description: "Payments, invoicing, refunds"},
				{Name: "technical"},
			},
		}},
	},
})
```

Answers arrive as `jev.AnswerUnion`. Read the fields directly, cast with `AsNoul()`, `AsChoice()` and `AsScore()`, or switch on the variant:

```go
for key, answer := range res.Answers {
	switch answer := answer.AsAny().(type) {
	case jev.NoulAnswer:
		fmt.Println(key, answer.Noul)
	case jev.ChoiceAnswer:
		fmt.Println(key, answer.Choice, answer.Confidence)
	case jev.ScoreAnswer:
		fmt.Println(key, answer.Score, answer.Levels())
	}
}
```

`ScoreAnswer.Levels()` returns the levels in order, each with its description and probability; the API itself keys both by the level number as a string.

### Validation

A request is checked before it is sent: it needs at least one question, a Choice needs at least one option and distinct option names, and a Score needs at least two levels. The upper limits (255 options, 10 levels, the context length) are left to the API to enforce.

### Models

The model defaults to `jev.ModelJevLatest`. Set `Model` on the params to pick one per request, or change the default for a client with `option.WithDefaultModel` or the `TYPESAFE_DEFAULT_MODEL` environment variable. `res.Model` reports the versioned model that answered.

```go
models, err := client.Models.List(ctx)
for _, model := range models.Models {
	fmt.Println(model.Name, model.ReleaseDate, model.Description)
}
```

## Request options

Options from the `option` package configure a client, and can be passed to any method to override the client for that one call:

```go
client := jev.NewClient(
	option.WithAPIKey("my-api-key"),
	option.WithBaseURL("https://jev.internal.example.com/"),
	option.WithHeader("X-Team", "support"),
)

res, err := client.SystemOne.New(ctx, params,
	option.WithMaxRetries(5),
	option.WithRequestTimeout(2*time.Second),
)
```

| Option | Effect |
| --- | --- |
| `WithAPIKey` | Sets the API key, sent as a bearer token |
| `WithBaseURL` | Sets the API root, without the `/v1` segment |
| `WithDefaultModel` | Sets the model for requests that do not name one |
| `WithMaxRetries` | Sets how many times a failed request is retried (default 2) |
| `WithRequestTimeout` | Sets the timeout of each attempt (default 10s, 0 for none) |
| `WithHTTPClient` | Replaces `http.DefaultClient` |
| `WithMiddleware` | Wraps every attempt, for logging, tracing or metrics |
| `WithHeader`, `WithHeaderAdd`, `WithHeaderDel` | Change request headers |
| `WithQuery`, `WithQueryAdd`, `WithQueryDel` | Change query parameters |
| `WithJSONSet`, `WithJSONDel` | Change top-level fields of the JSON request body |
| `WithRequestBody` | Replaces the request body |
| `WithResponseInto` | Captures the `*http.Response` |
| `WithResponseBodyInto` | Decodes the response body into another value |

`jev.NewClient` reads its defaults from the environment. Explicit options take precedence, and blank variables are ignored.

| Variable | Option | Default |
| --- | --- | --- |
| `TYPESAFE_API_KEY` | `WithAPIKey` | none |
| `TYPESAFE_BASE_URL` | `WithBaseURL` | `https://api.typesafe.ai/` |
| `TYPESAFE_DEFAULT_MODEL` | `WithDefaultModel` | `jev-latest` |

These are the variables the official Python and JavaScript SDKs read.

### Using another endpoint

Any service that speaks the System One wire format works through `WithBaseURL`. The path `v1/systemone` is appended to the base URL, so for OpenRouter:

```go
client := jev.NewClient(
	option.WithBaseURL("https://openrouter.ai/api/"),
	option.WithAPIKey(os.Getenv("OPENROUTER_API_KEY")),
	option.WithDefaultModel("typesafe/jev-1.13"),
)
```

## Errors

When the API answers with a status of 400 or above, the method returns a `*jev.Error` with the status code, the request ID and the response body:

```go
res, err := client.SystemOne.New(ctx, params)
if err != nil {
	var apierr *jev.Error
	if errors.As(err, &apierr) {
		fmt.Println(apierr.StatusCode) // 422
		fmt.Println(apierr.RequestID)  // from the x-typesafe-request-id header
		fmt.Println(apierr.Message)    // taken from the body when it has one
		fmt.Println(apierr.RawJSON())  // the body as received
		fmt.Println(string(apierr.DumpRequest(true)))
	}
	return err
}
```

| Status | Meaning |
| --- | --- |
| 401 | Missing or invalid API key |
| 422 | The request body failed validation |
| 429 | Rate limit exceeded |
| 529 | TypeSafe is temporarily overloaded |

Anything else, such as a connection failure or an expired context, is returned as the underlying error, so `errors.Is(err, context.DeadlineExceeded)` works as usual.

### Retries

Connection errors, attempts that time out, and the statuses 408, 429 and 5xx (which includes 529) are retried, twice by default. The delay starts at half a second and doubles up to five seconds, minus up to a quarter of jitter. A `Retry-After` or `Retry-After-Ms` response header of up to a minute takes precedence.

### Timeouts

`option.WithRequestTimeout` bounds a single attempt and defaults to ten seconds. The context bounds the whole call, retries and waits between them included:

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

res, err := client.SystemOne.New(ctx, params, option.WithRequestTimeout(3*time.Second))
```

## Response metadata

Every response type has a `RawJSON()` method that returns the JSON as received, and a `JSON` field that tells a missing or null field from a zero value and exposes fields this library does not model yet:

```go
if res.JSON.Usage.Valid() {
	// usage was present and not null
}
cost := res.Usage.JSON.ExtraFields["cost"].Raw() // e.g. sent by API gateways
```

To read response headers, capture the `*http.Response`:

```go
var raw *http.Response
res, err := client.SystemOne.New(ctx, params, option.WithResponseInto(&raw))
fmt.Println(raw.Header.Get("x-typesafe-request-id"))
```

## Middleware

A middleware sees every attempt of a request, retries included:

```go
func logger(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	start := time.Now()
	res, err := next(req)
	log.Printf("%s %s took %v", req.Method, req.URL.Path, time.Since(start))
	return res, err
}

client := jev.NewClient(option.WithMiddleware(logger))
```

## Undocumented endpoints and params

To send a parameter the library does not model, add it to the request body:

```go
res, err := client.SystemOne.New(ctx, params, option.WithJSONSet("some_new_param", true))
```

To call an endpoint the library does not cover, use `client.Get`, `client.Post` and the other verbs, which keep the client's base URL, authentication and retries:

```go
var result map[string]any
err := client.Post(ctx, "v1/some/endpoint", map[string]any{"key": "value"}, &result)
```

## Development

```sh
go vet ./...
go test -race ./...
```

The tests run against a local `httptest` server and need no API key.

## License

[MIT](LICENSE)

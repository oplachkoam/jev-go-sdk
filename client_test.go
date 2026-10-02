package jev_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oplachkoam/jev-go-sdk"
	"github.com/oplachkoam/jev-go-sdk/option"
)

const noulResponse = `{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":0.95}},"usage":{"input_tokens":296,"output_tokens":20}}`

// clearEnv makes sure the developer's own environment does not leak into a test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL"} {
		t.Setenv(key, "")
	}
}

// newTestClient starts a server running handler and returns a client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...option.RequestOption) jev.Client {
	t.Helper()
	clearEnv(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts = append([]option.RequestOption{option.WithBaseURL(server.URL), option.WithAPIKey("test-key")}, opts...)
	return jev.NewClient(opts...)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func noulParams() jev.SystemOneNewParams {
	return jev.SystemOneNewParams{
		State:     "Help! My payouts have been failing for 3 days.",
		Questions: map[string]jev.QuestionUnionParam{"urgent": jev.QuestionParamOfNoul("Does this convey urgency?")},
	}
}

func TestRequestBasics(t *testing.T) {
	var got *http.Request
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		writeJSON(w, http.StatusOK, noulResponse)
	})

	if _, err := client.SystemOne.New(context.Background(), noulParams()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/v1/systemone" {
		t.Errorf("got %s %s, want POST /v1/systemone", got.Method, got.URL.Path)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer test-key" {
		t.Errorf("Authorization = %q", auth)
	}
	if ct := got.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ua := got.Header.Get("User-Agent"); !strings.HasPrefix(ua, "jev-go-sdk/") {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	clearEnv(t)
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(w, http.StatusOK, noulResponse)
	}))
	defer server.Close()

	// No trailing slash: the prefix must be kept all the same.
	client := jev.NewClient(option.WithBaseURL(server.URL+"/api"), option.WithAPIKey("k"))
	if _, err := client.SystemOne.New(context.Background(), noulParams()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/api/v1/systemone" {
		t.Errorf("path = %q, want /api/v1/systemone", path)
	}
}

func TestEnvironmentDefaults(t *testing.T) {
	var auth, model string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		model = body.Model
		writeJSON(w, http.StatusOK, noulResponse)
	}))
	defer server.Close()

	t.Setenv("TYPESAFE_BASE_URL", server.URL)
	t.Setenv("TYPESAFE_API_KEY", "  env-key  ")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "jev-preview")

	client := jev.NewClient()
	if _, err := client.SystemOne.New(context.Background(), noulParams()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != "Bearer env-key" {
		t.Errorf("Authorization = %q, want the trimmed key from the environment", auth)
	}
	if model != "jev-preview" {
		t.Errorf("model = %q, want the default from the environment", model)
	}

	// Explicit options win over the environment, per-request options over both.
	client = jev.NewClient(option.WithAPIKey("explicit-key"), option.WithDefaultModel("jev-1.13.0"))
	if _, err := client.SystemOne.New(context.Background(), noulParams()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != "Bearer explicit-key" || model != "jev-1.13.0" {
		t.Errorf("got auth %q and model %q, want the explicit options", auth, model)
	}
	if _, err := client.SystemOne.New(context.Background(), noulParams(), option.WithAPIKey("request-key")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != "Bearer request-key" {
		t.Errorf("Authorization = %q, want the per-request key", auth)
	}
}

func TestDefaultModel(t *testing.T) {
	var model string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		model = body.Model
		writeJSON(w, http.StatusOK, noulResponse)
	})

	if _, err := client.SystemOne.New(context.Background(), noulParams()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != jev.ModelJevLatest {
		t.Errorf("model = %q, want %q", model, jev.ModelJevLatest)
	}

	params := noulParams()
	params.Model = jev.ModelJev1_13_0
	if _, err := client.SystemOne.New(context.Background(), params, option.WithDefaultModel("ignored")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != jev.ModelJev1_13_0 {
		t.Errorf("model = %q, want the model of the params", model)
	}
}

func TestRetriesHonorRetryAfter(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, 529} {
		var attempts atomic.Int32
		var bodies []string
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			if attempts.Add(1) < 3 {
				w.Header().Set("Retry-After-Ms", "1")
				writeJSON(w, status, `{"detail":"try again"}`)
				return
			}
			writeJSON(w, http.StatusOK, noulResponse)
		})

		res, err := client.SystemOne.New(context.Background(), noulParams())
		if err != nil {
			t.Fatalf("status %d: unexpected error: %v", status, err)
		}
		if attempts.Load() != 3 {
			t.Errorf("status %d: %d attempts, want 3", status, attempts.Load())
		}
		if res.Answers["urgent"].Noul != 0.95 {
			t.Errorf("status %d: unexpected response %s", status, res.RawJSON())
		}
		// Every attempt must carry the whole body again.
		if len(bodies) != 3 || bodies[0] == "" || bodies[0] != bodies[1] || bodies[1] != bodies[2] {
			t.Errorf("status %d: bodies differ between attempts: %q", status, bodies)
		}
	}
}

func TestRetriesGiveUp(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After-Ms", "1")
		writeJSON(w, http.StatusTooManyRequests, `{"detail":"slow down"}`)
	})

	_, err := client.SystemOne.New(context.Background(), noulParams())
	var apierr *jev.Error
	if !errors.As(err, &apierr) || apierr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("got %v, want a 429 *jev.Error", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("%d attempts, want the initial one and two retries", attempts.Load())
	}

	attempts.Store(0)
	_, err = client.SystemOne.New(context.Background(), noulParams(), option.WithMaxRetries(0))
	if !errors.As(err, &apierr) {
		t.Fatalf("got %v, want a *jev.Error", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("%d attempts with retries disabled, want 1", attempts.Load())
	}
}

func TestErrorIsNotRetriedAndCarriesDetails(t *testing.T) {
	var attempts atomic.Int32
	const body = `{"detail":[{"loc":["body","questions"],"msg":"field required"}]}`
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("x-typesafe-request-id", "req_123")
		writeJSON(w, http.StatusUnprocessableEntity, body)
	})

	_, err := client.SystemOne.New(context.Background(), noulParams())
	var apierr *jev.Error
	if !errors.As(err, &apierr) {
		t.Fatalf("got %v, want a *jev.Error", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("%d attempts, want 1: a 422 must not be retried", attempts.Load())
	}
	if apierr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d", apierr.StatusCode)
	}
	if apierr.RequestID != "req_123" {
		t.Errorf("RequestID = %q", apierr.RequestID)
	}
	if apierr.RawJSON() != body {
		t.Errorf("RawJSON() = %q", apierr.RawJSON())
	}
	if !strings.Contains(apierr.Message, "field required") {
		t.Errorf("Message = %q", apierr.Message)
	}
	for _, want := range []string{"422", "/v1/systemone", "field required", "req_123"} {
		if !strings.Contains(apierr.Error(), want) {
			t.Errorf("Error() = %q, want it to mention %q", apierr.Error(), want)
		}
	}
	if dump := string(apierr.DumpResponse(true)); !strings.Contains(dump, "field required") {
		t.Errorf("DumpResponse lost the body: %q", dump)
	}
	if dump := string(apierr.DumpRequest(true)); !strings.Contains(dump, "Does this convey urgency?") {
		t.Errorf("DumpRequest lost the body: %q", dump)
	}
}

func TestErrorMessageShapes(t *testing.T) {
	tests := map[string]string{
		`{"detail":"Invalid API key"}`:            "Invalid API key",
		`{"message":"Invalid API key"}`:           "Invalid API key",
		`{"error":"Invalid API key"}`:             "Invalid API key",
		`{"error":{"message":"Invalid API key"}}`: "Invalid API key",
		`<html>Bad gateway</html>`:                "",
		``:                                        "",
	}
	for body, want := range tests {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusUnauthorized, body)
		})
		_, err := client.Models.List(context.Background())
		var apierr *jev.Error
		if !errors.As(err, &apierr) {
			t.Fatalf("body %q: got %v, want a *jev.Error", body, err)
		}
		if apierr.Message != want {
			t.Errorf("body %q: Message = %q, want %q", body, apierr.Message, want)
		}
	}
}

func TestAttemptTimeoutIsRetried(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			// Outlast the timeout of the first attempt. The server only notices
			// that the client hung up once the request body has been read.
			io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		writeJSON(w, http.StatusOK, noulResponse)
	}, option.WithRequestTimeout(50*time.Millisecond))

	res, err := client.SystemOne.New(context.Background(), noulParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("%d attempts, want 2", attempts.Load())
	}
	if res.Model != "jev-1.13.0" {
		t.Errorf("unexpected response %s", res.RawJSON())
	}
}

func TestConnectionErrorIsRetried(t *testing.T) {
	clearEnv(t)
	// A server that is already closed refuses every connection.
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	attempts := 0
	count := func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		attempts++
		return next(req)
	}
	client := jev.NewClient(option.WithBaseURL(server.URL), option.WithMiddleware(count), option.WithMaxRetries(1))

	_, err := client.SystemOne.New(context.Background(), noulParams())
	if err == nil {
		t.Fatal("expected a connection error")
	}
	var apierr *jev.Error
	if errors.As(err, &apierr) {
		t.Errorf("a connection error must not be reported as an API error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("%d attempts, want the initial one and one retry", attempts)
	}
}

func TestRawResponseIsHandedOverUnread(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, noulResponse)
	})

	var raw *http.Response
	res, err := client.SystemOne.New(context.Background(), noulParams(), option.WithResponseBodyInto(&raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("the body went to the raw response, yet a decoded one came back: %+v", res)
	}
	defer raw.Body.Close()
	body, err := io.ReadAll(raw.Body)
	if err != nil || string(body) != noulResponse {
		t.Errorf("body = %q, %v", body, err)
	}
}

func TestContextDeadlineStopsRetries(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		writeJSON(w, http.StatusInternalServerError, `{}`)
	})

	// The backoff after the first failure is longer than the deadline allows.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.SystemOne.New(ctx, noulParams())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("%d attempts, want 1", attempts.Load())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v to notice the deadline", elapsed)
	}
}

func TestMiddlewareAndHeaders(t *testing.T) {
	var header, query string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Trace") + "|" + r.Header.Get("X-From-Middleware")
		query = r.URL.RawQuery
		writeJSON(w, http.StatusOK, noulResponse)
	}, option.WithHeader("X-Trace", "client"))

	var order []string
	first := func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		order = append(order, "first")
		req.Header.Set("X-From-Middleware", "yes")
		return next(req)
	}
	second := func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		order = append(order, "second")
		return next(req)
	}

	_, err := client.SystemOne.New(context.Background(), noulParams(),
		option.WithMiddleware(first, second),
		option.WithHeader("X-Trace", "request"),
		option.WithQuery("debug", "1"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header != "request|yes" {
		t.Errorf("headers = %q, want the per-request header and the middleware's", header)
	}
	if query != "debug=1" {
		t.Errorf("query = %q", query)
	}
	if strings.Join(order, ",") != "first,second" {
		t.Errorf("middleware order = %v", order)
	}
}

func TestResponseInto(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "req_456")
		writeJSON(w, http.StatusOK, noulResponse)
	})

	var raw *http.Response
	res, err := client.SystemOne.New(context.Background(), noulParams(), option.WithResponseInto(&raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == nil || raw.Header.Get("x-typesafe-request-id") != "req_456" {
		t.Errorf("raw response was not captured: %v", raw)
	}
	if res.Answers["urgent"].Noul != 0.95 {
		t.Errorf("the response was not decoded alongside: %s", res.RawJSON())
	}
}

func TestJSONSetAndDel(t *testing.T) {
	var body string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		writeJSON(w, http.StatusOK, noulResponse)
	})

	params := jev.SystemOneNewParams{
		State: "s",
		Questions: map[string]jev.QuestionUnionParam{
			"team": jev.QuestionParamOfChoice("Which team?",
				jev.ChoiceOptionParam{Name: "zeta"},
				jev.ChoiceOptionParam{Name: "alpha"},
			),
		},
	}
	_, err := client.SystemOne.New(context.Background(), params,
		option.WithJSONSet("experimental", map[string]any{"depth": 2}),
		option.WithJSONDel("model"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"experimental":{"depth":2},"questions":{"team":{"type":"choice","instructions":"Which team?","criteria":{"zeta":null,"alpha":null}}},"state":"s"}`
	if body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

type recordingDoer struct {
	calls int
	next  http.Handler
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls++
	rec := httptest.NewRecorder()
	d.next.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func TestCustomHTTPClient(t *testing.T) {
	clearEnv(t)
	doer := &recordingDoer{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "https://example.test/v1/models" {
			t.Errorf("url = %q", r.URL)
		}
		writeJSON(w, http.StatusOK, `{"models":[]}`)
	})}
	client := jev.NewClient(option.WithBaseURL("https://example.test"), option.WithHTTPClient(doer))
	if _, err := client.Models.List(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.calls != 1 {
		t.Errorf("custom client was called %d times, want 1", doer.calls)
	}
}

func TestExecuteEscapeHatch(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/unknown" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, `{"ok":true}`)
	})

	var decoded struct {
		OK bool `json:"ok"`
	}
	if err := client.Get(context.Background(), "v1/unknown", nil, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decoded.OK {
		t.Error("response was not decoded")
	}

	var raw []byte
	if err := client.Get(context.Background(), "/v1/unknown", nil, &raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("raw = %q", raw)
	}
}

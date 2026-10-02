package requestconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oplachkoam/jev-go-sdk/internal"
	"github.com/oplachkoam/jev-go-sdk/internal/apierror"
)

const (
	// DefaultMaxRetries is how many times a request is retried after the
	// initial attempt unless [RequestConfig.MaxRetries] says otherwise.
	DefaultMaxRetries = 2
	// DefaultRequestTimeout bounds each attempt of a request, not the request
	// with its retries as a whole.
	DefaultRequestTimeout = 10 * time.Second

	initialRetryDelay = 500 * time.Millisecond
	maxRetryDelay     = 5 * time.Second
	// Server-requested delays longer than this fall back to backoff.
	maxRetryAfter = time.Minute
)

func getDefaultHeaders() map[string]string {
	return map[string]string{
		"User-Agent": fmt.Sprintf("jev-go-sdk/%s", internal.PackageVersion),
		"Accept":     "application/json",
	}
}

type RequestOption interface {
	Apply(*RequestConfig) error
}

type RequestOptionFunc func(*RequestConfig) error
type PreRequestOptionFunc func(*RequestConfig) error

func (s RequestOptionFunc) Apply(r *RequestConfig) error    { return s(r) }
func (s PreRequestOptionFunc) Apply(r *RequestConfig) error { return s(r) }

func NewRequestConfig(ctx context.Context, method string, u string, body any, dst any, opts ...RequestOption) (*RequestConfig, error) {
	var reader io.Reader

	contentType := "application/json"
	hasSerializationFunc := false

	if body, ok := body.(json.Marshaler); ok {
		content, err := body.MarshalJSON()
		if err != nil {
			return nil, err
		}
		reader = bytes.NewBuffer(content)
		hasSerializationFunc = true
	}
	if body, ok := body.([]byte); ok {
		reader = bytes.NewBuffer(body)
		hasSerializationFunc = true
	}
	if body, ok := body.(io.Reader); ok {
		reader = body
		hasSerializationFunc = true
	}

	// Fallback to json serialization if none of the serialization functions that we expect
	// to see is present.
	if body != nil && !hasSerializationFunc {
		content, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewBuffer(content)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	if reader != nil {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range getDefaultHeaders() {
		req.Header.Add(k, v)
	}

	cfg := RequestConfig{
		MaxRetries:     DefaultMaxRetries,
		RequestTimeout: DefaultRequestTimeout,
		Context:        ctx,
		Request:        req,
		HTTPClient:     http.DefaultClient,
		Body:           reader,
	}
	cfg.ResponseBodyInto = dst
	err = cfg.Apply(opts...)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// This interface is primarily used to describe an [*http.Client], but also
// supports custom HTTP implementations.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RequestConfig represents all the state related to one request.
//
// Editing the variables inside RequestConfig directly is unstable api. Prefer
// composing the RequestOptions instead if possible.
type RequestConfig struct {
	MaxRetries     int
	RequestTimeout time.Duration
	Context        context.Context
	Request        *http.Request
	BaseURL        *url.URL
	// DefaultBaseURL will be used if BaseURL is not explicitly overridden using
	// WithBaseURL.
	DefaultBaseURL *url.URL
	CustomHTTPDoer HTTPDoer
	HTTPClient     *http.Client
	Middlewares    []middleware
	APIKey         string
	// DefaultModel is the model used by requests that do not name one.
	DefaultModel string
	// If ResponseBodyInto not nil, then we will attempt to deserialize into
	// ResponseBodyInto. If Destination is a []byte, then it will return the body as
	// is.
	ResponseBodyInto any
	// ResponseInto copies the \*http.Response of the corresponding request into the
	// given address
	ResponseInto **http.Response
	Body         io.Reader
}

// middleware is exactly the same type as the Middleware type found in the [option] package,
// but it is redeclared here for circular dependency issues.
type middleware = func(*http.Request, middlewareNext) (*http.Response, error)

// middlewareNext is exactly the same type as the MiddlewareNext type found in the [option] package,
// but it is redeclared here for circular dependency issues.
type middlewareNext = func(*http.Request) (*http.Response, error)

func applyMiddleware(middleware middleware, next middlewareNext) middlewareNext {
	return func(req *http.Request) (res *http.Response, err error) {
		return middleware(req, next)
	}
}

// shouldRetry reports whether an attempt that ended with the given response or
// error is worth repeating.
func shouldRetry(req *http.Request, res *http.Response, err error) bool {
	// If there is no way to recover the Body, then we shouldn't retry.
	if req.Body != nil && req.GetBody == nil {
		return false
	}

	// If there is no response, that indicates that there is a connection error
	// or that the attempt timed out, so we retry the request.
	if err != nil || res == nil {
		return true
	}

	return res.StatusCode == http.StatusRequestTimeout ||
		res.StatusCode == http.StatusTooManyRequests ||
		res.StatusCode >= http.StatusInternalServerError
}

func parseRetryAfterHeader(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	type retryData struct {
		header string
		units  time.Duration

		// custom is used when the regular algorithm failed and is optional.
		// the returned duration is used verbatim (units is not applied).
		custom func(string) (time.Duration, bool)
	}

	nop := func(string) (time.Duration, bool) { return 0, false }

	// the headers are listed in order of preference
	retries := []retryData{
		{
			header: "Retry-After-Ms",
			units:  time.Millisecond,
			custom: nop,
		},
		{
			header: "Retry-After",
			units:  time.Second,

			// retry-after values are expressed in either number of
			// seconds or an HTTP-date indicating when to try again
			custom: func(ra string) (time.Duration, bool) {
				t, err := http.ParseTime(ra)
				if err != nil {
					return 0, false
				}
				return time.Until(t), true
			},
		},
	}

	for _, retry := range retries {
		v := resp.Header.Get(retry.header)
		if v == "" {
			continue
		}
		if retryAfter, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(retryAfter * float64(retry.units)), true
		}
		if d, ok := retry.custom(v); ok {
			return d, true
		}
	}

	return 0, false
}

// isBeforeContextDeadline reports whether the non-zero Time t is
// before ctx's deadline. If ctx does not have a deadline, it
// always reports true (the deadline is considered infinite).
func isBeforeContextDeadline(t time.Time, ctx context.Context) bool {
	d, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return t.Before(d)
}

// bodyWithTimeout is an io.ReadCloser which can observe a context's cancel func
// to handle timeouts etc. It wraps an existing io.ReadCloser.
type bodyWithTimeout struct {
	stop func() // stops the time.Timer waiting to cancel the request
	rc   io.ReadCloser
}

func (b *bodyWithTimeout) Read(p []byte) (n int, err error) {
	return b.rc.Read(p)
}

func (b *bodyWithTimeout) Close() error {
	err := b.rc.Close()
	b.stop()
	return err
}

func retryDelay(res *http.Response, retryCount int) time.Duration {
	// If the API asks us to wait a certain amount of time (and it's a reasonable amount),
	// just do what it says.
	if retryAfterDelay, ok := parseRetryAfterHeader(res); ok && 0 <= retryAfterDelay && retryAfterDelay <= maxRetryAfter {
		return retryAfterDelay
	}

	delay := time.Duration(float64(initialRetryDelay) * math.Pow(2, float64(retryCount)))
	if delay > maxRetryDelay || delay <= 0 {
		delay = maxRetryDelay
	}

	jitter := rand.Int63n(int64(delay / 4))
	delay -= time.Duration(jitter)
	return delay
}

// sleep waits for the delay to pass, or returns early with the context's error
// if the context ends first.
func sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// streamsResponse reports whether the caller reads the response body itself,
// in which case it is handed over unread.
func (cfg *RequestConfig) streamsResponse() bool {
	_, intoCustomResponseBody := cfg.ResponseBodyInto.(**http.Response)
	return cfg.ResponseBodyInto == nil || intoCustomResponseBody
}

func (cfg *RequestConfig) Execute() (err error) {
	if cfg.BaseURL == nil {
		if cfg.DefaultBaseURL != nil {
			cfg.BaseURL = cfg.DefaultBaseURL
		} else {
			return fmt.Errorf("requestconfig: base url is not set")
		}
	}

	cfg.Request.URL, err = cfg.BaseURL.Parse(strings.TrimLeft(cfg.Request.URL.String(), "/"))
	if err != nil {
		return err
	}

	if cfg.Body != nil && cfg.Request.Body == nil {
		switch body := cfg.Body.(type) {
		case *bytes.Buffer:
			b := body.Bytes()
			cfg.Request.ContentLength = int64(body.Len())
			cfg.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
			cfg.Request.Body, _ = cfg.Request.GetBody()
		case *bytes.Reader:
			cfg.Request.ContentLength = int64(body.Len())
			cfg.Request.GetBody = func() (io.ReadCloser, error) {
				_, err := body.Seek(0, 0)
				return io.NopCloser(body), err
			}
			cfg.Request.Body, _ = cfg.Request.GetBody()
		default:
			if rc, ok := body.(io.ReadCloser); ok {
				cfg.Request.Body = rc
			} else {
				cfg.Request.Body = io.NopCloser(body)
			}
		}
	}

	handler := cfg.HTTPClient.Do
	if cfg.CustomHTTPDoer != nil {
		handler = cfg.CustomHTTPDoer.Do
	}
	for i := len(cfg.Middlewares) - 1; i >= 0; i -= 1 {
		handler = applyMiddleware(cfg.Middlewares[i], handler)
	}

	parent := cfg.Request.Context()
	streams := cfg.streamsResponse()

	var res *http.Response
	var contents []byte
	// cancel releases the context of the latest attempt. It is nil once that
	// has happened or after it was handed off to the response body.
	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	for retryCount := 0; ; retryCount += 1 {
		ctx := parent
		if cfg.RequestTimeout != time.Duration(0) && isBeforeContextDeadline(time.Now().Add(cfg.RequestTimeout), ctx) {
			var stop context.CancelFunc
			ctx, stop = context.WithTimeout(ctx, cfg.RequestTimeout)
			cancel = stop
		}

		res, err = handler(cfg.Request.Clone(ctx))

		// A successful response that the caller reads itself is left untouched.
		// Everything else is read here, within the timeout of the attempt, so
		// that a body cut short is retried like any other connection error.
		handoff := err == nil && streams && res.StatusCode < 400
		if err == nil && !handoff {
			contents, err = io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				err = fmt.Errorf("error reading response body: %w", err)
			} else {
				// Re-populate the body so that it can still be inspected, for
				// instance through DumpResponse on an API error.
				res.Body = io.NopCloser(bytes.NewReader(contents))
			}
		}

		// The caller gave up, or its own deadline passed: there is nothing to
		// retry, and the context's error is more telling than the transport's.
		if parent.Err() != nil {
			if handoff {
				res.Body.Close()
			}
			return parent.Err()
		}

		if retryCount >= cfg.MaxRetries || !shouldRetry(cfg.Request, res, err) {
			break
		}

		// Only failed attempts get here, and their bodies were read above.
		delay := retryDelay(res, retryCount)
		if cancel != nil {
			cancel()
			cancel = nil
		}

		// Prepare next request and wait for the retry delay
		if cfg.Request.GetBody != nil {
			cfg.Request.Body, err = cfg.Request.GetBody()
			if err != nil {
				return err
			}
		}

		if err := sleep(parent, delay); err != nil {
			return err
		}
	}

	// Save *http.Response if it is requested to, even if there was an error making the request. This is
	// useful in cases where you might want to debug by inspecting the response. Note that if err != nil,
	// the response should be generally be empty, but there are edge cases.
	if cfg.ResponseInto != nil {
		*cfg.ResponseInto = res
	}
	if responseBodyInto, ok := cfg.ResponseBodyInto.(**http.Response); ok {
		*responseBodyInto = res
	}

	// If there was a connection error in the final request or any other transport error,
	// return that early without trying to coerce into an APIError.
	if err != nil {
		return err
	}

	if res.StatusCode >= 400 {
		return apierror.New(cfg.Request, res, contents)
	}

	if streams {
		// We aren't reading the response body in this scope, but whoever is will need the
		// cancel func from the context to observe request timeouts.
		// Put the cancel function in the response body so it can be handled elsewhere.
		if cancel != nil {
			res.Body = &bodyWithTimeout{rc: res.Body, stop: cancel}
			cancel = nil
		}
		return nil
	}

	switch dst := cfg.ResponseBodyInto.(type) {
	// If the response happens to be a byte array, deserialize the body as-is.
	case *[]byte:
		*dst = contents
		return nil
	case *string:
		contentType := res.Header.Get("content-type")
		mediaType, _, _ := mime.ParseMediaType(contentType)
		isJSON := strings.Contains(mediaType, "application/json") || strings.HasSuffix(mediaType, "+json")
		// If we are not json, return plaintext
		if !isJSON {
			*dst = string(contents)
			return nil
		}
	}

	err = json.Unmarshal(contents, cfg.ResponseBodyInto)
	if err != nil {
		return fmt.Errorf("error parsing response json: %w", err)
	}

	return nil
}

func ExecuteNewRequest(ctx context.Context, method string, u string, body any, dst any, opts ...RequestOption) error {
	cfg, err := NewRequestConfig(ctx, method, u, body, dst, opts...)
	if err != nil {
		return err
	}
	return cfg.Execute()
}

func (cfg *RequestConfig) Apply(opts ...RequestOption) error {
	for _, opt := range opts {
		err := opt.Apply(cfg)
		if err != nil {
			return err
		}
	}
	return nil
}

// PreRequestOptions is used to collect all the options which need to be known before
// a call to [RequestConfig.ExecuteNewRequest], such as global defaults.
// PreRequestOptions will return a [RequestConfig] with the options applied.
//
// Only request option functions of type [PreRequestOptionFunc] are applied.
func PreRequestOptions(opts ...RequestOption) (RequestConfig, error) {
	cfg := RequestConfig{}
	for _, opt := range opts {
		if opt, ok := opt.(PreRequestOptionFunc); ok {
			err := opt.Apply(&cfg)
			if err != nil {
				return cfg, err
			}
		}
	}
	return cfg, nil
}

// WithDefaultBaseURL returns a RequestOption that sets the client's default Base URL.
// This is always overridden by setting a base URL with WithBaseURL.
// WithBaseURL should be used instead of WithDefaultBaseURL except in internal code.
func WithDefaultBaseURL(baseURL string) RequestOption {
	u, err := url.Parse(baseURL)
	return RequestOptionFunc(func(r *RequestConfig) error {
		if err != nil {
			return err
		}
		r.DefaultBaseURL = u
		return nil
	})
}

// Package option provides the request options that configure a client, a
// service, or a single request.
package option

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oplachkoam/jev-go-sdk/internal/requestconfig"
)

// RequestOption is an option for the requests made by the jev API Client
// which can be supplied to clients, services, and methods. You can read more about this functional
// options pattern in our [README].
//
// [README]: https://pkg.go.dev/github.com/oplachkoam/jev-go-sdk#readme-request-options
type RequestOption = requestconfig.RequestOption

// WithBaseURL returns a RequestOption that sets the BaseURL for the client.
//
// The base URL is the API root without the version segment, for example
// "https://api.typesafe.ai/". For security reasons, ensure that the base URL
// is trusted.
func WithBaseURL(base string) RequestOption {
	u, err := url.Parse(base)
	if err == nil && u.Path != "" && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}

	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if err != nil {
			return fmt.Errorf("requestoption: WithBaseURL failed to parse url %s", err)
		}

		r.BaseURL = u
		return nil
	})
}

// HTTPClient is primarily used to describe an [*http.Client], but also
// supports custom implementations.
//
// For bespoke implementations, prefer using an [*http.Client] with a
// custom transport. See [http.RoundTripper] for further information.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// WithHTTPClient returns a RequestOption that changes the underlying http client used to make this
// request, which by default is [http.DefaultClient].
//
// For custom uses cases, it is recommended to provide an [*http.Client] with a custom
// [http.RoundTripper] as its transport, rather than directly implementing [HTTPClient].
func WithHTTPClient(client HTTPClient) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if client == nil {
			return fmt.Errorf("requestoption: custom http client cannot be nil")
		}

		if c, ok := client.(*http.Client); ok {
			// Prefer the native client if possible.
			r.HTTPClient = c
			r.CustomHTTPDoer = nil
		} else {
			r.CustomHTTPDoer = client
		}

		return nil
	})
}

// MiddlewareNext is a function which is called by a middleware to pass an HTTP request
// to the next stage in the middleware chain.
type MiddlewareNext = func(*http.Request) (*http.Response, error)

// Middleware is a function which intercepts HTTP requests, processing or modifying
// them, and then passing the request to the next middleware or handler
// in the chain by calling the provided MiddlewareNext function.
type Middleware = func(*http.Request, MiddlewareNext) (*http.Response, error)

// WithMiddleware returns a RequestOption that applies the given middleware
// to the requests made. Each middleware will execute in the order they were given.
// Middleware runs once per attempt, so a retried request passes through it again.
func WithMiddleware(middlewares ...Middleware) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Middlewares = append(r.Middlewares, middlewares...)
		return nil
	})
}

// WithMaxRetries returns a RequestOption that sets the maximum number of retries that the client
// attempts to make. When given 0, the client only makes one request. By
// default, the client retries two times.
//
// Connection errors, timeouts of a single attempt, and the status codes 408,
// 429 and 5xx (which includes 529 Overloaded) are retried with exponential
// backoff. A Retry-After or Retry-After-Ms response header of up to a minute
// takes precedence over the backoff.
//
// WithMaxRetries panics when retries is negative.
func WithMaxRetries(retries int) RequestOption {
	if retries < 0 {
		panic("option: cannot have fewer than 0 retries")
	}

	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.MaxRetries = retries
		return nil
	})
}

// WithHeader returns a RequestOption that sets the header value to the associated key. It overwrites
// any value if there was one already present.
func WithHeader(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Request.Header.Set(key, value)
		return nil
	})
}

// WithHeaderAdd returns a RequestOption that adds the header value to the associated key. It appends
// onto any existing values.
func WithHeaderAdd(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Request.Header.Add(key, value)
		return nil
	})
}

// WithHeaderDel returns a RequestOption that deletes the header value(s) associated with the given key.
func WithHeaderDel(key string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.Request.Header.Del(key)
		return nil
	})
}

// WithQuery returns a RequestOption that sets the query value to the associated key. It overwrites
// any value if there was one already present.
func WithQuery(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		query := r.Request.URL.Query()
		query.Set(key, value)
		r.Request.URL.RawQuery = query.Encode()
		return nil
	})
}

// WithQueryAdd returns a RequestOption that adds the query value to the associated key. It appends
// onto any existing values.
func WithQueryAdd(key, value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		query := r.Request.URL.Query()
		query.Add(key, value)
		r.Request.URL.RawQuery = query.Encode()
		return nil
	})
}

// WithQueryDel returns a RequestOption that deletes the query value(s) associated with the key.
func WithQueryDel(key string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		query := r.Request.URL.Query()
		query.Del(key)
		r.Request.URL.RawQuery = query.Encode()
		return nil
	})
}

// WithJSONSet returns a RequestOption that sets a top-level field of the JSON
// request body to the given value, which is encoded with [encoding/json]. It
// overwrites the field if the body already has it.
//
// Use it to send a parameter that this library does not model yet. The key
// names a field of the outermost object; nested paths are not supported.
func WithJSONSet(key string, value any) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) (err error) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("requestoption: WithJSONSet cannot encode the value of %q: %w", key, err)
		}
		return editJSONBody(r, "WithJSONSet", func(object map[string]json.RawMessage) {
			object[key] = encoded
		})
	})
}

// WithJSONDel returns a RequestOption that deletes a top-level field from the
// JSON request body. Like [WithJSONSet], it does not support nested paths.
func WithJSONDel(key string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) (err error) {
		return editJSONBody(r, "WithJSONDel", func(object map[string]json.RawMessage) {
			delete(object, key)
		})
	})
}

// editJSONBody rewrites the outermost object of the request body. Field values
// are carried over byte for byte, so the key order inside them is preserved.
func editJSONBody(r *requestconfig.RequestConfig, name string, edit func(map[string]json.RawMessage)) error {
	object := map[string]json.RawMessage{}
	if r.Body != nil {
		buffer, ok := r.Body.(*bytes.Buffer)
		if !ok {
			return fmt.Errorf("requestoption: cannot use %s on a body that is not serialized as *bytes.Buffer", name)
		}
		if err := json.Unmarshal(buffer.Bytes(), &object); err != nil {
			return fmt.Errorf("requestoption: cannot use %s on a body that is not a JSON object: %w", name, err)
		}
	}
	edit(object)
	encoded, err := json.Marshal(object)
	if err != nil {
		return err
	}
	r.Body = bytes.NewBuffer(encoded)
	if r.Request.Header.Get("Content-Type") == "" {
		r.Request.Header.Set("Content-Type", "application/json")
	}
	return nil
}

// WithResponseBodyInto returns a RequestOption that overwrites the deserialization target with
// the given destination. If provided, we don't deserialize into the default struct.
func WithResponseBodyInto(dst any) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.ResponseBodyInto = dst
		return nil
	})
}

// WithResponseInto returns a RequestOption that copies the [*http.Response] into the given address.
// Use it to read response headers such as x-typesafe-request-id.
func WithResponseInto(dst **http.Response) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.ResponseInto = dst
		return nil
	})
}

// WithRequestBody returns a RequestOption that provides a custom serialized body with the given
// content type.
//
// body accepts an io.Reader or raw []bytes.
func WithRequestBody(contentType string, body any) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		if reader, ok := body.(interface{ Read([]byte) (int, error) }); ok {
			r.Body = reader
			r.Request.Header.Set("Content-Type", contentType)
			return nil
		}

		if b, ok := body.([]byte); ok {
			r.Body = bytes.NewBuffer(b)
			r.Request.Header.Set("Content-Type", contentType)
			return nil
		}

		return errors.New("requestoption: body must be a byte slice or implement io.Reader")
	})
}

// WithRequestTimeout returns a RequestOption that sets the timeout for
// each request attempt. This should be smaller than the timeout defined in
// the context, which spans all retries. The default is 10 seconds; a timeout
// of zero removes the limit.
func WithRequestTimeout(dur time.Duration) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.RequestTimeout = dur
		return nil
	})
}

// WithEnvironmentProduction returns a RequestOption that sets the current
// environment to be the "production" environment. An environment specifies which base URL
// to use by default.
func WithEnvironmentProduction() RequestOption {
	return requestconfig.WithDefaultBaseURL("https://api.typesafe.ai/")
}

// WithAPIKey returns a RequestOption that sets the client setting "api_key".
func WithAPIKey(value string) RequestOption {
	return requestconfig.RequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.APIKey = value
		return r.Apply(WithHeader("authorization", fmt.Sprintf("Bearer %s", r.APIKey)))
	})
}

// WithDefaultModel returns a RequestOption that sets the model used by
// requests that do not name one. Without it, such requests use "jev-latest".
func WithDefaultModel(model string) requestconfig.PreRequestOptionFunc {
	return requestconfig.PreRequestOptionFunc(func(r *requestconfig.RequestConfig) error {
		r.DefaultModel = model
		return nil
	})
}

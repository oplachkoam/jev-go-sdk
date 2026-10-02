package apierror

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
)

// RequestIDHeader is the response header that carries the request ID.
const RequestIDHeader = "x-typesafe-request-id"

// Error represents an error that originates from the API, i.e. when a request is
// made and the API returns a response with a HTTP status code of 400 or above.
// Other errors, such as connection failures, are not wrapped by this SDK.
type Error struct {
	// The HTTP status code of the response.
	StatusCode int
	// A human-readable description of the failure taken from the response body,
	// or an empty string if the body did not carry one. See [Error.RawJSON] for
	// the complete body.
	Message string
	// The ID the API assigned to the request, taken from the
	// x-typesafe-request-id response header. Empty if the header was absent.
	RequestID string
	Request   *http.Request
	Response  *http.Response

	raw string
}

// New builds an Error from a response and its already-read body.
func New(req *http.Request, res *http.Response, body []byte) *Error {
	return &Error{
		StatusCode: res.StatusCode,
		Message:    extractMessage(body),
		RequestID:  res.Header.Get(RequestIDHeader),
		Request:    req,
		Response:   res,
		raw:        string(body),
	}
}

// RawJSON returns the unmodified response body received from the API. Despite
// the name it may not be JSON, for example when a proxy answers with HTML.
func (r *Error) RawJSON() string { return r.raw }

func (r *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q: %d", r.Request.Method, r.Request.URL, r.StatusCode)
	if text := statusText(r.StatusCode); text != "" {
		b.WriteString(" " + text)
	}
	if r.raw != "" {
		b.WriteString(" " + r.raw)
	}
	if r.RequestID != "" {
		fmt.Fprintf(&b, " (request id: %s)", r.RequestID)
	}
	return b.String()
}

// DumpRequest returns the request in its HTTP/1.x wire representation,
// optionally with the body. The Authorization header is included as sent.
func (r *Error) DumpRequest(body bool) []byte {
	if r.Request.GetBody != nil {
		r.Request.Body, _ = r.Request.GetBody()
	}
	out, _ := httputil.DumpRequestOut(r.Request, body)
	return out
}

// DumpResponse returns the response in its HTTP/1.x wire representation,
// optionally with the body.
func (r *Error) DumpResponse(body bool) []byte {
	out, _ := httputil.DumpResponse(r.Response, body)
	return out
}

func statusText(code int) string {
	if code == 529 {
		return "Overloaded"
	}
	return http.StatusText(code)
}

// extractMessage looks for a description of the failure in the shapes error
// bodies commonly take: {"detail": ...}, {"message": "..."},
// {"error": "..."} and {"error": {"message": "..."}}.
func extractMessage(body []byte) string {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return ""
	}
	for _, key := range []string{"message", "detail", "error"} {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if text != "" {
				return text
			}
			continue
		}
		var nested struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &nested) == nil && nested.Message != "" {
			return nested.Message
		}
		// A structured value such as a list of validation failures.
		if s := string(raw); s != "null" {
			return s
		}
	}
	return ""
}

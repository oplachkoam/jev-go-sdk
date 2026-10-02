// Package respjson holds metadata about fields decoded from API responses.
package respjson

// Field describes how a single field appeared in a JSON response. Use it to
// tell apart a field that was omitted, sent as null, or sent with a value,
// which the zero value of a Go type cannot express on its own.
type Field struct {
	raw     string
	present bool
}

// NewField returns the metadata for a field that was present in the response
// with the given raw JSON.
func NewField(raw string) Field {
	return Field{raw: raw, present: true}
}

// Valid reports whether the field was present in the response and not null.
func (f Field) Valid() bool { return f.present && f.raw != "null" }

// Present reports whether the field was present in the response, even if its
// value was null.
func (f Field) Present() bool { return f.present }

// Raw returns the unmodified JSON of the field, or an empty string if the
// field was omitted from the response.
func (f Field) Raw() string { return f.raw }

// Package apijson decodes API responses into structs while recording
// per-field metadata.
package apijson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/oplachkoam/jev-go-sdk/packages/respjson"
)

var (
	fieldType  = reflect.TypeOf(respjson.Field{})
	extrasType = reflect.TypeOf(map[string]respjson.Field{})
	nullJSON   = []byte("null")
)

// UnmarshalRoot decodes the JSON object in data into the struct that dst
// points to.
//
// Each exported field with a json tag is decoded with [encoding/json], so
// nested types may define their own UnmarshalJSON. If the struct has a field
// named JSON, its [respjson.Field] members named after the Go fields are
// filled in, and unknown keys are collected in its ExtraFields map.
//
// It is meant to be called from the UnmarshalJSON method of dst itself, which
// is why it never hands dst as a whole back to [encoding/json].
func UnmarshalRoot(data []byte, dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("apijson: destination must be a non-nil pointer to a struct, got %T", dst)
	}
	v = v.Elem()
	t := v.Type()

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("apijson: cannot decode %s: %w", t.Name(), err)
	}

	meta := v.FieldByName("JSON")
	if meta.IsValid() && meta.Kind() != reflect.Struct {
		meta = reflect.Value{}
	}

	known := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name, ok := jsonName(field)
		if !ok {
			continue
		}
		known[name] = struct{}{}

		raw, present := object[name]
		if !present {
			continue
		}
		if !bytes.Equal(raw, nullJSON) {
			if err := json.Unmarshal(raw, v.Field(i).Addr().Interface()); err != nil {
				return fmt.Errorf("apijson: cannot decode %s.%s: %w", t.Name(), name, err)
			}
		}
		if meta.IsValid() {
			if m := meta.FieldByName(field.Name); m.IsValid() && m.CanSet() && m.Type() == fieldType {
				m.Set(reflect.ValueOf(respjson.NewField(string(raw))))
			}
		}
	}

	if !meta.IsValid() {
		return nil
	}
	extras := meta.FieldByName("ExtraFields")
	if !extras.IsValid() || !extras.CanSet() || extras.Type() != extrasType {
		return nil
	}
	var unknown map[string]respjson.Field
	for name, raw := range object {
		if _, ok := known[name]; ok {
			continue
		}
		if unknown == nil {
			unknown = make(map[string]respjson.Field)
		}
		unknown[name] = respjson.NewField(string(raw))
	}
	extras.Set(reflect.ValueOf(unknown))
	return nil
}

// jsonName returns the JSON key of an exported struct field, and false if the
// field does not take part in decoding.
func jsonName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}

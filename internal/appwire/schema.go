package appwire

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// THE SCHEMA IS GENERATED FROM THE TYPES THE SERVER USES, never written by
// hand: a method's params and result are the types [typed] was built with, an
// event's data is the type [eventShapes] names, and every named type they
// reach is one definition under $defs. A field's JSON name, whether it may be
// left out, and whether it may be null come from the same struct tags
// encoding/json reads; the words a string field may hold come from its enum
// tag, which is the only thing a Go type cannot say by itself.

// Schema is the whole wire as one JSON Schema document.
func Schema() map[string]any {
	g := &schemaGen{defs: map[string]any{}}
	ms := map[string]any{}
	for name, m := range methods {
		g.params = true
		params := g.of(m.params)
		g.params = false
		ms[name] = map[string]any{"params": params, "result": g.of(m.result)}
	}
	ms["shutdown"] = map[string]any{
		"params": map[string]any{"type": "object"},
		"result": map[string]any{"type": "object"},
	}
	es := map[string]any{}
	for name, t := range eventShapes {
		es[name] = g.of(t)
	}
	return map[string]any{
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"title":    "codeaf app wire",
		"contract": Contract,
		"methods":  ms,
		"events":   es,
		"$defs":    g.defs,
	}
}

// SchemaJSON is [Schema] encoded, indented, ending in a newline.
func SchemaJSON() ([]byte, error) {
	out, err := json.MarshalIndent(Schema(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

type schemaGen struct {
	defs map[string]any
	// params is set while a method's params are described. A request may
	// leave any field out (the method itself says which it needs), so no
	// field of params is required, and a type reached from params is defined
	// apart from the same type in an answer, under "in.".
	params bool
}

var (
	timeType = reflect.TypeFor[time.Time]()
	rawType  = reflect.TypeFor[json.RawMessage]()
)

// of is the schema of one type.
func (g *schemaGen) of(t reflect.Type) map[string]any {
	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case t == rawType:
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return nullable(g.of(t.Elem()))
	case reflect.Interface:
		return map[string]any{}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}
		}
		return map[string]any{"type": "array", "items": g.of(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.of(t.Elem())}
	case reflect.Struct:
		if t.Name() == "" {
			return g.object(t)
		}
		name := t.Name()
		if g.params {
			name = "in." + name
		}
		if _, seen := g.defs[name]; !seen {
			g.defs[name] = nil
			g.defs[name] = g.object(t)
		}
		return map[string]any{"$ref": "#/$defs/" + name}
	}
	return map[string]any{}
}

// object is a struct's schema: its fields as encoding/json writes them.
func (g *schemaGen) object(t reflect.Type) map[string]any {
	props := map[string]any{}
	var need []string
	g.fields(t, props, &need)
	out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(need) > 0 {
		out["required"] = need
	}
	return out
}

// fields adds t's fields, and an embedded struct's fields as its own, the way
// encoding/json flattens them.
func (g *schemaGen) fields(t reflect.Type, props map[string]any, need *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || !f.IsExported() && !f.Anonymous {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			inner := f.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				g.fields(inner, props, need)
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		schema := g.of(f.Type)
		if words := f.Tag.Get("enum"); words != "" {
			schema = withEnum(schema, strings.Split(words, ","))
		}
		omit := strings.Contains(","+opts+",", ",omitempty,") || strings.Contains(","+opts+",", ",omitzero,")
		// A slice or map that is nil, and is not left out, is written as null.
		if !omit && (f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() != reflect.Uint8 || f.Type.Kind() == reflect.Map) {
			schema = nullable(schema)
		}
		if f.Tag.Get("nullable") == "true" && f.Type.Kind() != reflect.Pointer {
			schema = nullable(schema)
		}
		props[name] = schema
		if !omit && !g.params {
			*need = append(*need, name)
		}
	}
}

// nullable is a schema that also admits null.
func nullable(s map[string]any) map[string]any {
	if len(s) == 0 {
		return s
	}
	return map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
}

// withEnum narrows a string schema, or the string under a nullable one, to
// its words.
func withEnum(s map[string]any, words []string) map[string]any {
	if alts, ok := s["anyOf"].([]any); ok && len(alts) == 2 {
		inner := withEnum(alts[0].(map[string]any), words)
		return map[string]any{"anyOf": []any{inner, alts[1]}}
	}
	if s["type"] == "array" {
		items := withEnum(s["items"].(map[string]any), words)
		out := map[string]any{}
		for k, v := range s {
			out[k] = v
		}
		out["items"] = items
		return out
	}
	out := map[string]any{}
	for k, v := range s {
		out[k] = v
	}
	out["enum"] = words
	return out
}

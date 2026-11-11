package appwire

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// THE WIRE'S SHAPES ARE THE GO TYPES THE SERVER ANSWERS WITH.
//
// Every method is built by [typed] from a function of one params type and one
// result type, so the type the server decodes a request into and the type it
// answers with are the types the schema is generated from: there is no second
// description of a method that could say something the method does not do.
// Every event's data is the type [eventShapes] names, and the test recorders
// check each event they see against it.

// method answers one request from its params, which are already known to be a
// JSON object, and carries the Go types of both halves.
type method struct {
	call   func(Engine, json.RawMessage) (any, error)
	params reflect.Type
	result reflect.Type
}

// typed is a method from a function of its params and its result. The params
// are decoded into P, and a bad shape answers bad_params.
func typed[P, R any](fn func(Engine, P) (*R, error)) method {
	return method{
		call: func(e Engine, raw json.RawMessage) (any, error) {
			var p P
			if err := decode(raw, &p); err != nil {
				return nil, err
			}
			r, err := fn(e, p)
			if err != nil {
				return nil, err
			}
			if r == nil {
				var zero R
				return zero, nil
			}
			return r, nil
		},
		params: reflect.TypeFor[P](),
		result: reflect.TypeFor[R](),
	}
}

// shaped is a method that reads its own params, because how they are read
// matters (a key that is never quoted back, a patch whose fields are each read
// in their own shape); P is the shape they travel in.
func shaped[P, R any](fn func(Engine, json.RawMessage) (*R, error)) method {
	return method{
		call: func(e Engine, raw json.RawMessage) (any, error) {
			r, err := fn(e, raw)
			if err != nil {
				return nil, err
			}
			return r, nil
		},
		params: reflect.TypeFor[P](),
		result: reflect.TypeFor[R](),
	}
}

// Empty is the answer of a method that has nothing to say but that it was done.
type Empty struct{}

// done is the empty answer, with the error a call returned.
func done(err error) (*Empty, error) { return &Empty{}, err }

// IDParams is the params of a method that takes one id.
type IDParams struct {
	ID string `json:"id"`
}

// AgentParams is the params of a method that takes one agent.
type AgentParams struct {
	Agent string `json:"agent"`
}

// required answers the value, or bad_params naming the field when it is empty.
func required(field, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", Errorf(CodeBadParams, "%s is required", field)
	}
	return value, nil
}

// Methods is every method's name, sorted, with the shutdown the loop answers
// itself.
func Methods() []string {
	names := make([]string, 0, len(methods)+1)
	for name := range methods {
		names = append(names, name)
	}
	names = append(names, "shutdown")
	sort.Strings(names)
	return names
}

// Events is every event's name, sorted.
func Events() []string {
	names := make([]string, 0, len(eventShapes))
	for name := range eventShapes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// EventShape is the Go type an event's data is, and nil for a name that is
// not an event.
func EventShape(name string) reflect.Type { return eventShapes[name] }

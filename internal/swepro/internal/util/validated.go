// Validated function adapter — port of src/util/fn.ts:3-20
// (swe-pro 3b25a1a).
package util

type TypedValidator[T any] interface {
	Parse(T) (T, error)
}

type ValidatedFunc[T, R any] struct {
	Schema TypedValidator[T]
	cb     func(T) R
}

func Fn[T, R any](schema TypedValidator[T], cb func(T) R) *ValidatedFunc[T, R] {
	return &ValidatedFunc[T, R]{Schema: schema, cb: cb}
}

func (f *ValidatedFunc[T, R]) Call(input T) (R, error) {
	parsed, err := f.Schema.Parse(input)
	if err != nil {
		var zero R
		return zero, err
	}
	return f.cb(parsed), nil
}

func (f *ValidatedFunc[T, R]) Force(input T) R { return f.cb(input) }

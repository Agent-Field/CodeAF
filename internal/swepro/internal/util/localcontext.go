// Local context — port of src/util/local-context.ts:3-23
// (swe-pro 3b25a1a). Go has no AsyncLocalStorage; this surface uses an explicit
// context.Context, the native propagation mechanism across goroutines.
package util

import (
	"context"
	"fmt"
)

type ContextNotFound struct{ Name string }

func (e *ContextNotFound) Error() string { return "No context found for " + e.Name }

type localContextKey[T any] struct{ owner *LocalContext[T] }

type LocalContext[T any] struct {
	Name string
	key  localContextKey[T]
}

func CreateLocalContext[T any](name string) *LocalContext[T] {
	local := &LocalContext[T]{Name: name}
	local.key.owner = local
	return local
}

func (l *LocalContext[T]) Use(ctx context.Context) (T, error) {
	value, ok := ctx.Value(l.key).(T)
	if !ok {
		var zero T
		return zero, &ContextNotFound{Name: l.Name}
	}
	return value, nil
}

func (l *LocalContext[T]) Provide(ctx context.Context, value T) context.Context {
	return context.WithValue(ctx, l.key, value)
}

func (l *LocalContext[T]) MustUse(ctx context.Context) T {
	value, err := l.Use(ctx)
	if err != nil {
		panic(fmt.Sprint(err))
	}
	return value
}

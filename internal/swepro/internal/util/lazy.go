// Lazy value — port of src/util/lazy.ts:1-19 (swe-pro 3b25a1a).
package util

import "sync"

type LazyValue[T any] struct {
	mu     sync.Mutex
	fn     func() T
	value  T
	loaded bool
}

func Lazy[T any](fn func() T) *LazyValue[T] { return &LazyValue[T]{fn: fn} }

func (l *LazyValue[T]) Get() T {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.loaded {
		l.value = l.fn()
		l.loaded = true
	}
	return l.value
}

func (l *LazyValue[T]) Reset() {
	l.mu.Lock()
	var zero T
	l.value = zero
	l.loaded = false
	l.mu.Unlock()
}

func (l *LazyValue[T]) Loaded() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loaded
}

func IIFE[T any](fn func() T) T { return fn() }

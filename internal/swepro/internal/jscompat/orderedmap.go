// Package jscompat provides Go primitives that reproduce JavaScript runtime
// semantics the codeaf TS harness depends on: insertion-ordered maps, JS
// number formatting/coercion, JSON.stringify-compatible encoding, JS string
// trimming, and the mulberry32 PRNG. The port's parity contract is "behaves
// byte-for-byte like the Bun/V8 original", so anything here that looks odd is
// odd because JS is.
package jscompat

// OrderedMap reproduces JS Map semantics: iteration follows key insertion
// order; Set on an existing key updates the value in place (original position
// kept); Delete removes the key; a later Set of a deleted key appends at the
// end. Not safe for concurrent use — callers hold their own locks, mirroring
// the TS store's single-writer model.
type OrderedMap[K comparable, V any] struct {
	keys []K
	vals map[K]V
}

func NewOrderedMap[K comparable, V any]() *OrderedMap[K, V] {
	return &OrderedMap[K, V]{vals: make(map[K]V)}
}

func (m *OrderedMap[K, V]) Len() int { return len(m.keys) }

func (m *OrderedMap[K, V]) Has(k K) bool {
	_, ok := m.vals[k]
	return ok
}

func (m *OrderedMap[K, V]) Get(k K) (V, bool) {
	v, ok := m.vals[k]
	return v, ok
}

func (m *OrderedMap[K, V]) Set(k K, v V) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *OrderedMap[K, V]) Delete(k K) {
	if _, ok := m.vals[k]; !ok {
		return
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *OrderedMap[K, V]) Clear() {
	m.keys = m.keys[:0]
	m.vals = make(map[K]V)
}

// Keys returns a snapshot of the keys in insertion order. Mirrors `[...m.keys()]`.
func (m *OrderedMap[K, V]) Keys() []K {
	out := make([]K, len(m.keys))
	copy(out, m.keys)
	return out
}

// Values returns a snapshot of the values in insertion order. Mirrors `[...m.values()]`.
func (m *OrderedMap[K, V]) Values() []V {
	out := make([]V, 0, len(m.keys))
	for _, k := range m.keys {
		out = append(out, m.vals[k])
	}
	return out
}

type Entry[K comparable, V any] struct {
	Key K
	Val V
}

// Entries returns a snapshot of entries in insertion order. Mirrors `[...m.entries()]`.
func (m *OrderedMap[K, V]) Entries() []Entry[K, V] {
	out := make([]Entry[K, V], 0, len(m.keys))
	for _, k := range m.keys {
		out = append(out, Entry[K, V]{Key: k, Val: m.vals[k]})
	}
	return out
}

// First returns the first value in insertion order (JS `map.values().next().value`).
func (m *OrderedMap[K, V]) First() (V, bool) {
	if len(m.keys) == 0 {
		var zero V
		return zero, false
	}
	return m.vals[m.keys[0]], true
}

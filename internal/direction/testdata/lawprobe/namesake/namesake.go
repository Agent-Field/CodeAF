// Package namesake declares methods that share the raw doors' names and hand
// out no store: a cache's snapshot is not the collections store's.
package namesake

// Cache is an unrelated type.
type Cache struct{ rows []string }

// ReadSnapshot copies the cache.
func (c Cache) ReadSnapshot() []string { return append([]string(nil), c.rows...) }

// WriteImmediate replaces the cache.
func (c *Cache) WriteImmediate(rows []string) { c.rows = rows }

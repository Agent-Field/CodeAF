package aisearch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// CacheMost is the most answers one cache keeps; past it the oldest go first.
const CacheMost = 20000

// Cache keeps every answer a search was given, keyed by [route.AnswerKey] of
// the question and the item's own words, so an edited issue is asked again and
// an unchanged one never is. It is what makes a saved view's reopen cheap: the
// view keeps its question, the cache keeps the answers.
//
// A nil *Cache keeps nothing and every method is a no-op, so a caller that has
// nowhere to keep answers still searches.
type Cache struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	kept map[string]entry
}

// entry is one item's answers to one question. Screened is the screen's
// verdict; Judged is the judge's, absent until it was asked.
type entry struct {
	Screened bool          `json:"screened"`
	Judged   *route.Answer `json:"judged,omitempty"`
	At       time.Time     `json:"at"`
}

// OpenCache reads the cache kept at path (one JSON file; absent is empty) and
// writes it back there on every change. A file that cannot be read is started
// over, never trusted half-read.
func OpenCache(path string) *Cache {
	c := &Cache{path: path, now: time.Now, kept: map[string]entry{}}
	if path == "" {
		return c
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var doc struct {
		Version int              `json:"version"`
		Answers map[string]entry `json:"answers"`
	}
	if json.Unmarshal(raw, &doc) == nil && doc.Version == 1 && doc.Answers != nil {
		c.kept = doc.Answers
	}
	return c
}

func key(question string, it factory.Item) string {
	return route.AnswerKey(strings.ToLower(Clean(question)), it) + ":" + it.Ref()
}

func (c *Cache) screened(question string, it factory.Item) (bool, bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.kept[key(question, it)]
	return e.Screened, ok
}

func (c *Cache) judged(question string, it factory.Item) (route.Answer, bool) {
	if c == nil {
		return route.Answer{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.kept[key(question, it)]
	if !ok || e.Judged == nil {
		return route.Answer{}, false
	}
	return *e.Judged, true
}

func (c *Cache) screen(question string, it factory.Item, related bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	k := key(question, it)
	e := c.kept[k]
	e.Screened, e.At = related, c.now()
	if !related {
		e.Judged = nil
	}
	c.kept[k] = e
	c.mu.Unlock()
}

func (c *Cache) judge(question string, it factory.Item, a route.Answer) {
	if c == nil {
		return
	}
	c.mu.Lock()
	k := key(question, it)
	e := c.kept[k]
	e.Screened, e.Judged, e.At = true, &a, c.now()
	c.kept[k] = e
	c.mu.Unlock()
}

// Save writes the cache to its file, oldest answers dropped past [CacheMost].
// A cache with no file saves nothing.
func (c *Cache) Save() error {
	if c == nil || c.path == "" {
		return nil
	}
	c.mu.Lock()
	if len(c.kept) > CacheMost {
		keys := make([]string, 0, len(c.kept))
		for k := range c.kept {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return c.kept[keys[i]].At.Before(c.kept[keys[j]].At) })
		for _, k := range keys[:len(keys)-CacheMost] {
			delete(c.kept, k)
		}
	}
	raw, err := json.Marshal(struct {
		Version int              `json:"version"`
		Answers map[string]entry `json:"answers"`
	}{1, c.kept})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}

// Len is how many answers are kept.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.kept)
}

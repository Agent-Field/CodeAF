package relaybill

import (
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Counter tallies the requests the client makes, by method and the first two
// path segments, so the billed numbers can be checked against what the client
// says it sent. It wraps a transport and changes nothing on the wire.
type Counter struct {
	Next http.RoundTripper
	mu   sync.Mutex
	n    map[string]int
}

// RoundTrip counts the request and passes it on.
func (c *Counter) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[kindOf(r)]++
	c.mu.Unlock()
	return c.Next.RoundTrip(r)
}

// kindOf names a request by what it does, not by the ids in its path.
func kindOf(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return r.Method + " /" + strings.Join(parts[:min(len(parts), 3)], "/")
}

// Snapshot is the tally so far, and Since is the tally's growth from an earlier one.
func (c *Counter) Snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

// Since answers what was counted after the earlier snapshot, without zero rows.
func (c *Counter) Since(before map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range c.Snapshot() {
		if d := v - before[k]; d > 0 {
			out[k] = d
		}
	}
	return out
}

// Total is the sum of a tally.
func Total(n map[string]int) (sum int) {
	for _, v := range n {
		sum += v
	}
	return sum
}

// Sorted lists a tally's kinds in order, for a stable report.
func Sorted(n map[string]int) []string {
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

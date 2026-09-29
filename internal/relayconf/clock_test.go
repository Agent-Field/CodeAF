//go:build relayurl

package relayconf

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// relayClock is the relay's own time, read from the Codeaf-Now header every
// answer carries, an unsigned refusal included. Nothing here changes what the
// relay does, so the same reading works against any relay.
type relayClock struct {
	t    *testing.T
	base string
}

// Now asks the relay what time it is.
func (c relayClock) Now() time.Time {
	c.t.Helper()
	resp, err := httpClient.Get(c.base + "/v1/dir/list")
	if err != nil {
		c.t.Fatalf("reading the relay's clock: %v", err)
	}
	resp.Body.Close()
	return relayTime(c.t, resp)
}

func relayTime(t *testing.T, resp *http.Response) time.Time {
	t.Helper()
	ms, err := strconv.ParseInt(resp.Header.Get("Codeaf-Now"), 10, 64)
	if err != nil {
		t.Fatalf("relay answered %d without a numeric Codeaf-Now", resp.StatusCode)
	}
	return time.UnixMilli(ms)
}

// Wait sleeps until the relay's clock, not ours, has moved on by d.
func (c relayClock) Wait(d time.Duration) {
	c.t.Helper()
	until := c.Now().Add(d)
	for left := d; left > 0; left = until.Sub(c.Now()) {
		time.Sleep(left)
	}
}

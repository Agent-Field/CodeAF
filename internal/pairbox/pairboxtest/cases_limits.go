package pairboxtest

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// limitsAreTheirWord holds the mailbox to what it says it enforces: five
// positive numbers, the TTL a new mailbox is given, and, when the wire can be
// read raw, exactly the five fields of the contract and no others.
func limitsAreTheirWord(t *testing.T, e env) {
	l := e.lim
	for name, n := range map[string]int{
		"ttl": int(l.TTL), "max_msg": l.MaxMsg, "max_msgs_per_side": l.MaxMsgsPerSide,
		"create_per_hour": l.CreatePerHour, "write_per_minute": l.WritePerMinute,
	} {
		if n <= 0 {
			t.Errorf("limit %s is %d, want a positive number", name, n)
		}
	}
	key := newKey(t)
	made, err := e.Box.Create(e.ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	e.forget(t, e.Box, made.Nameplate, key)
	if made.ExpiresIn != l.TTL {
		t.Errorf("a new mailbox lives %v, but the limits say %v", made.ExpiresIn, l.TTL)
	}
	if e.URL != "" {
		limitsOnTheWire(t, e)
	}
}

func limitsOnTheWire(t *testing.T, e env) {
	t.Helper()
	status, body := e.wireOf(t).do(t, http.MethodGet, pairbox.Path+"/limits", nil, nil)
	var got map[string]int64
	if err := json.Unmarshal(body, &got); status != http.StatusOK || err != nil {
		t.Fatalf("GET limits = %d %q (%v)", status, body, err)
	}
	l := e.lim
	want := map[string]int64{
		"ttl_ms": l.TTL.Milliseconds(), "max_msg": int64(l.MaxMsg), "max_msgs_per_side": int64(l.MaxMsgsPerSide),
		"create_per_hour": int64(l.CreatePerHour), "write_per_minute": int64(l.WritePerMinute),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GET limits answered %v, but the client reads %v", got, want)
	}
}

// A message of exactly the limit is kept; one byte more is refused, and the
// refusal takes nothing from the side.
func messageTooBig(t *testing.T, e env) {
	m := e.open(t)
	_, err := e.Box.Post(e.ctx, m.plate, pairbox.SideA, m.key, make([]byte, e.lim.MaxMsg+1))
	wantIs(t, "a message one byte over", err, pairbox.ErrTooBig)
	if n := e.post(t, m, pairbox.SideA, m.key, make([]byte, e.lim.MaxMsg)); n != 0 {
		t.Errorf("the message at the limit has index %d after a refused one, want 0", n)
	}
}

// A side holds its share of messages and says so on the next; the other side
// is not touched by it.
func sideFull(t *testing.T, e env) {
	m := e.open(t)
	for range e.lim.MaxMsgsPerSide {
		e.post(t, m, pairbox.SideA, m.key, []byte("m"))
	}
	_, err := e.Box.Post(e.ctx, m.plate, pairbox.SideA, m.key, []byte("one too many"))
	wantIs(t, "a message past the side's share", err, pairbox.ErrSideFull)
	e.post(t, m, pairbox.SideB, newKey(t), []byte("b is separate"))
}

// After the hour's share of creates the next is refused with a wait of at
// least a second.
func createRateLimited(t *testing.T, e env) {
	for range e.lim.CreatePerHour {
		e.open(t)
	}
	_, err := e.Box.Create(e.ctx, newKey(t))
	wantRetry(t, "the create past the hourly limit", err)
}

// Posting to one side of one mailbox as fast as possible is slowed within one
// post of the minute's share, whatever else the posts are answered.
func writeRateLimited(t *testing.T, e env) {
	m := e.open(t)
	for range e.lim.WritePerMinute + 1 {
		_, err := e.Box.Post(e.ctx, m.plate, pairbox.SideA, m.key, []byte("x"))
		if errors.Is(err, pairbox.ErrRateLimited) {
			wantRetry(t, "the post past the minute's limit", err)
			return
		}
		if err != nil && !errors.Is(err, pairbox.ErrSideFull) {
			t.Fatalf("a post before the limit: %v", err)
		}
	}
	t.Fatalf("no post was rate limited within %d posts", e.lim.WritePerMinute+1)
}

// wantRetry checks a refusal is a RateLimited that says when to come back.
func wantRetry(t *testing.T, what string, err error) {
	t.Helper()
	var limited pairbox.RateLimited
	if !errors.As(err, &limited) || !errors.Is(err, pairbox.ErrRateLimited) {
		t.Errorf("%s: got %v, want a rate limit", what, err)
		return
	}
	if limited.RetryAfter < time.Second {
		t.Errorf("%s: retry after %v, want at least a second", what, limited.RetryAfter)
	}
}

// A service at its cap of mailboxes refuses the next and takes it again once
// one is deleted. Small is capped at two.
func relayFull(t *testing.T, e env) {
	if e.Small == nil {
		t.Skip("this rig has no small service")
	}
	l, err := e.Small.Limits(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var held []mailbox
	for range l.CreatePerHour {
		m, err := e.tryOpen(t, e.Small)
		if err != nil {
			wantIs(t, "the create past the cap", err, pairbox.ErrRelayFull)
			break
		}
		held = append(held, m)
	}
	if len(held) == 0 || len(held) == l.CreatePerHour {
		t.Fatalf("the small service took %d mailboxes and never said it was full", len(held))
	}
	if err := e.Small.Delete(e.ctx, held[0].plate, held[0].key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := e.tryOpen(t, e.Small); err != nil {
		t.Errorf("Create after a delete made room: %v", err)
	}
}

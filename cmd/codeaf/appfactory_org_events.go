package main

// ── AN ORGANIZATION'S CHANGES, RELAYED TO THE DESK ─────────────────────────
//
// An organization's server publishes every change to a team or a workstream
// on its event stream (GET /v1/events) as {"kind","id"}, already filtered to
// what the caller may see. This half follows that stream for a connection
// once the connection has answered an organization method, and relays exactly
// those three kinds as factory.org_changed, tagged with the connection, so a
// window showing that organization reads again instead of polling.
//
// THE FOLLOWER SPEAKS ONLY WITH THE SIGN-IN IT WAS STARTED WITH. A sign-in
// that changes, ends or is refused ends it before anything more is relayed,
// and the next organization method starts it again with the new one. It also
// ends when the connection is detached or the engine stops. A dropped stream
// is reopened with Last-Event-ID, so the server replays what was missed; when
// it cannot (the id has left its window) it says "changed *", which is relayed
// as each kind with an empty id: read the whole list again.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// appOrgRetryMost is the longest wait between reopening a dropped stream.
const appOrgRetryMost = 30 * time.Second

// appOrgLineMost bounds one line of the stream; the org events are tiny, and
// a line past this is a server this follower stops listening to.
const appOrgLineMost = 1 << 20

// appOrgFollow is one connection's follower and the sign-in it speaks with.
type appOrgFollow struct {
	cancel context.CancelFunc
	token  string
}

// followOrg starts following id's event stream with token unless it is
// already followed with that same sign-in. Nothing starts when the hub has
// nowhere to relay to (a test hub) or is closing.
func (h *appFactoryHub) followOrg(id, token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.orgChanged == nil {
		return
	}
	if f, ok := h.orgFollow[id]; ok {
		if f.token == token {
			return
		}
		f.cancel()
	}
	if h.orgFollow == nil {
		h.orgFollow = map[string]*appOrgFollow{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &appOrgFollow{cancel: cancel, token: token}
	h.orgFollow[id] = f
	go h.runOrgFollow(ctx, id, f)
}

// stopOrgFollowLocked ends id's follower, if any.
func (h *appFactoryHub) stopOrgFollowLocked(id string) {
	if f, ok := h.orgFollow[id]; ok {
		f.cancel()
		delete(h.orgFollow, id)
	}
}

// stopAllOrgFollowLocked ends every follower.
func (h *appFactoryHub) stopAllOrgFollowLocked() {
	for id := range h.orgFollow {
		h.stopOrgFollowLocked(id)
	}
}

// endOrgFollow removes f when it is still id's follower.
func (h *appFactoryHub) endOrgFollow(id string, f *appOrgFollow) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f.cancel()
	if h.orgFollow[id] == f {
		delete(h.orgFollow, id)
	}
}

// stillFollowing is whether f may still relay: its sign-in is still the one
// held for id. A sign-out or a new person's sign-in ends it.
func (h *appFactoryHub) stillFollowing(id string, f *appOrgFollow) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && h.orgFollow[id] == f && h.tokenLocked(id) == f.token
}

func (h *appFactoryHub) runOrgFollow(ctx context.Context, id string, f *appOrgFollow) {
	defer h.endOrgFollow(id, f)
	first := h.orgRetry
	if first <= 0 {
		first = time.Second
	}
	wait, last := first, ""
	for ctx.Err() == nil && h.stillFollowing(id, f) {
		row, _, err := h.lookup(id)
		if err != nil {
			return
		}
		opened, refused, next := h.orgStream(ctx, row.URL, id, f, last)
		last = next
		if refused || ctx.Err() != nil {
			return
		}
		if opened {
			wait = first
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		wait = min(wait*2, appOrgRetryMost)
	}
}

// orgStream reads one opening of the stream until it drops. opened is whether
// the server answered 200; refused is an answer short of a server failure,
// which following again would only repeat. next is the last event id seen.
func (h *appFactoryHub) orgStream(ctx context.Context, origin, id string, f *appOrgFollow, last string) (opened, refused bool, next string) {
	next = last
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/v1/events", nil)
	if err != nil {
		return false, true, next
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "codeaf-app")
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	if last != "" {
		req.Header.Set("Last-Event-ID", last)
	}
	// The hub's client gives every request a deadline; a stream has none.
	client := *h.client
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		return false, false, next
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, resp.StatusCode < 500, next
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), appOrgLineMost)
	var eventID, kind, data string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line != "" {
			if strings.HasPrefix(line, ":") {
				continue
			}
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "id":
				eventID = value
			case "event":
				kind = value
			case "data":
				data = value
			}
			continue
		}
		if eventID != "" {
			next = eventID
		}
		if !h.relayOrg(id, f, kind, data) {
			return true, true, next
		}
		eventID, kind, data = "", "", ""
	}
	return true, false, next
}

// relayOrg emits one frame when it is an organization event, or a resync
// after a gap the server could not replay. It is false when f may no longer
// relay, which ends the follower.
func (h *appFactoryHub) relayOrg(id string, f *appOrgFollow, kind, data string) bool {
	var out []appwire.FactoryOrgChanged
	switch kind {
	case api.TeamChangedKind, api.EventWorkstreamChanged, api.EventWorkstreamRemoved:
		var body api.OrgChanged
		if json.Unmarshal([]byte(data), &body) != nil || body.ID == "" {
			return true
		}
		out = append(out, appwire.FactoryOrgChanged{Connection: id, Kind: kind, ID: body.ID})
	case "changed":
		if data != "*" {
			return true
		}
		out = append(out,
			appwire.FactoryOrgChanged{Connection: id, Kind: api.TeamChangedKind},
			appwire.FactoryOrgChanged{Connection: id, Kind: api.EventWorkstreamChanged})
	default:
		return true
	}
	if !h.stillFollowing(id, f) {
		return false
	}
	for _, e := range out {
		h.orgChanged(e)
	}
	return true
}

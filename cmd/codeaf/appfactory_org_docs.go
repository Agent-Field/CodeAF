package main

// ── AN ORGANIZATION'S LIVE DOCUMENTS, RELAYED TO THE DESK ──────────────────
//
// factory.org_doc_follow opens one document's stream on the organization's
// server (GET /v1/docs/{doc}/events?since=) with the connection's sign-in,
// and relays what it carries as factory.org_doc_update and
// factory.org_doc_awareness, tagged with the connection and the document.
// The server sends updates in seq order and never skips one, so the follower
// keeps only the last seq it relayed: a stream that drops, or that the server
// ends because this reader fell behind (doc.lagged), is reopened from that
// seq and the server replays the rest. A document that is deleted ends the
// follow with factory.org_doc_closed "removed"; one the person can no longer
// see, or a sign-in that changes or ends, with "gone".

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// appDocLineMost bounds one line of a document's stream: a snapshot's base64
// is the largest thing on it.
const appDocLineMost = 4 << 20

// appDocFollow is one followed document and the sign-in it speaks with.
type appDocFollow struct {
	cancel context.CancelFunc
	token  string
}

func docFollowKey(id, doc string) string { return id + "\x00" + doc }

// FactoryOrgDocFollow is factory.org_doc_follow.
func (b *appBackend) FactoryOrgDocFollow(id, doc string, since int64) error {
	h := b.factoryHub()
	ctx, cancel := context.WithTimeout(context.Background(), appFactoryTimeout)
	defer cancel()
	_, token, err := h.credentialed(ctx, id)
	if err != nil {
		return err
	}
	h.followDoc(id, doc, token, since)
	return nil
}

// FactoryOrgDocUnfollow is factory.org_doc_unfollow.
func (b *appBackend) FactoryOrgDocUnfollow(id, doc string) {
	h := b.factoryHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopDocFollowLocked(docFollowKey(id, doc))
}

// followDoc starts relaying doc unless it is already followed with token.
func (h *appFactoryHub) followDoc(id, doc, token string, since int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.docEvent == nil {
		return
	}
	key := docFollowKey(id, doc)
	if f, ok := h.docFollow[key]; ok {
		if f.token == token {
			return
		}
		f.cancel()
	}
	if h.docFollow == nil {
		h.docFollow = map[string]*appDocFollow{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &appDocFollow{cancel: cancel, token: token}
	h.docFollow[key] = f
	guard.Go("app/factory org doc", func() { h.followDocStream(ctx, id, doc, f, since) })
}

func (h *appFactoryHub) stopDocFollowLocked(key string) {
	if f, ok := h.docFollow[key]; ok {
		f.cancel()
		delete(h.docFollow, key)
	}
}

// stopConnDocsLocked ends every document followed on connection id.
func (h *appFactoryHub) stopConnDocsLocked(id string) {
	for key := range h.docFollow {
		if strings.HasPrefix(key, id+"\x00") {
			h.stopDocFollowLocked(key)
		}
	}
}

func (h *appFactoryHub) stillFollowingDoc(id, doc string, f *appDocFollow) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && h.docFollow[docFollowKey(id, doc)] == f && h.tokenLocked(id) == f.token
}

// endDocFollow removes f and, when it ended on its own rather than by
// unfollow, says why.
func (h *appFactoryHub) endDocFollow(id, doc string, f *appDocFollow, reason string) {
	h.mu.Lock()
	key := docFollowKey(id, doc)
	mine := h.docFollow[key] == f
	if mine {
		delete(h.docFollow, key)
	}
	emit := h.docEvent
	h.mu.Unlock()
	f.cancel()
	if mine && reason != "" && emit != nil {
		emit(appwire.EventFactoryOrgDocClosed, appwire.FactoryOrgDocClosed{Connection: id, Doc: doc, Reason: reason})
	}
}

func (h *appFactoryHub) followDocStream(ctx context.Context, id, doc string, f *appDocFollow, since int64) {
	reason := ""
	defer func() { h.endDocFollow(id, doc, f, reason) }()
	first := h.orgRetry
	if first <= 0 {
		first = time.Second
	}
	wait := first
	for ctx.Err() == nil {
		if !h.stillFollowingDoc(id, doc, f) {
			reason = "gone"
			return
		}
		row, _, err := h.lookup(id)
		if err != nil {
			reason = "gone"
			return
		}
		opened, end, next := h.docStream(ctx, row.URL, id, doc, f, since)
		since = next
		if end != "" || ctx.Err() != nil {
			reason = end
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
		wait = min(max(wait*2, first), appOrgRetryMost)
	}
}

// docStream reads one opening of the stream. end is why the follow ends
// ("removed", "gone"), "" to open it again; next is the last seq relayed.
func (h *appFactoryHub) docStream(ctx context.Context, origin, id, doc string, f *appDocFollow, since int64) (opened bool, end string, next int64) {
	next = since
	path, err := appwire.OrgDocPath(doc)
	if err != nil {
		return false, "gone", next
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+path+"/events?since="+url.QueryEscape(strconv.FormatInt(since, 10)), nil)
	if err != nil {
		return false, "gone", next
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "codeaf-app")
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	client := *h.client
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		return false, "", next
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			h.refused(id, f.token)
		}
		if resp.StatusCode < 500 {
			return false, "gone", next
		}
		return false, "", next
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), appDocLineMost)
	var kind, data string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line != "" {
			if strings.HasPrefix(line, ":") {
				continue
			}
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				kind = value
			case "data":
				data = value
			}
			continue
		}
		if !h.stillFollowingDoc(id, doc, f) {
			return true, "gone", next
		}
		seq, stop := h.relayDoc(id, doc, kind, data)
		switch {
		case stop == "lagged":
			return true, "", next
		case stop != "":
			return true, stop, next
		case seq > 0:
			next = seq
		}
		kind, data = "", ""
	}
	return true, "", next
}

// relayDoc emits one event. seq is the update's when one was relayed; stop
// is "lagged" (reopen), "removed" (end) or "".
func (h *appFactoryHub) relayDoc(id, doc, kind, data string) (int64, string) {
	h.mu.Lock()
	emit := h.docEvent
	h.mu.Unlock()
	if emit == nil {
		return 0, "gone"
	}
	switch kind {
	case api.EventDocUpdate, api.EventDocSnapshot:
		var body struct {
			Doc    string    `json:"doc"`
			Seq    int64     `json:"seq"`
			By     string    `json:"by"`
			At     time.Time `json:"at"`
			Update string    `json:"update"`
			State  string    `json:"state"`
		}
		if json.Unmarshal([]byte(data), &body) != nil || body.Seq < 1 {
			return 0, ""
		}
		out := appwire.FactoryOrgDocUpdate{Connection: id, Doc: doc, Seq: body.Seq, By: body.By, At: body.At, Update: body.Update}
		if kind == api.EventDocSnapshot {
			out.Update, out.Snapshot = body.State, true
		}
		emit(appwire.EventFactoryOrgDocUpdate, out)
		return body.Seq, ""
	case api.EventDocAwareness:
		var body api.DocAwarenessEvent
		if json.Unmarshal([]byte(data), &body) != nil {
			return 0, ""
		}
		emit(appwire.EventFactoryOrgDocAwareness, appwire.FactoryOrgDocAwareness{Connection: id, Doc: doc, By: body.By, Client: body.Client, State: body.State, At: body.At})
	case api.EventDocLagged:
		return 0, "lagged"
	case api.EventDocRemoved:
		return 0, "removed"
	}
	return 0, ""
}

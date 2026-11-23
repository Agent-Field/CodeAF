package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// liveBytes is every byte the stream writes for one caller after the cursor.
func liveBytes(t *testing.T, h *Handler, token string, events []live.Event) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.RemoteAddr = "192.0.2.1:9"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	if !h.sendLive(w, req, events, "") {
		t.Fatal("sendLive stopped early")
	}
	return w.Body.String()
}

// PEER MACHINE INFORMATION IS FORBIDDEN on the live stream, to every caller:
// the job number on a machine event never makes it someone else's to see.
func TestLiveMachineEventsAreOnlyTheCallersOwnAtAnyJob(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}, "raj": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "priya")
	queueAs(t, h, token, false)
	// Every event carries the same id: ownership alone decides, never the id.
	mk := func(kind, handle string, job int, name string) live.Event {
		e := live.New(kind, job, api.LiveEvent{Job: job, Name: name})
		e.ID = "7"
		e.Handle = handle
		return e
	}
	var events []live.Event
	for _, kind := range []string{"machine", "runner"} {
		for _, job := range []int{0, 1, 2} {
			events = append(events,
				mk(kind, "priya", job, "mine-"+kind),
				mk(kind, "@Priya", job, "mixed-"+kind),
				mk(kind, "raj", job, "peer-"+kind),
				mk(kind, " RAJ ", job, "peercase-"+kind),
				mk(kind, "", job, "nobody-"+kind))
		}
	}
	body := liveBytes(t, h, token, events)
	for _, leak := range []string{"peer-", "peercase-", "nobody-"} {
		if strings.Contains(body, leak) {
			t.Fatalf("peer machine leaked %q: %s", leak, body)
		}
	}
	for _, kind := range []string{"machine", "runner"} {
		for _, own := range []string{"mine-", "mixed-"} {
			if got := strings.Count(body, own+kind); got != 3 {
				t.Fatalf("%s%s delivered %d times, want once per job: %s", own, kind, got, body)
			}
		}
	}
}

// An activity record is redacted for the viewer, keeps its event, actor and
// job, and one that does not parse is never forwarded as stored.
func TestLiveActivityIsRedactedForTheViewerAndFailsClosed(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}, "raj": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "priya")
	queueAs(t, h, token, false)
	activity := func(job int, handle, detail string) live.Event {
		return live.New("activity", job, record.Event{
			ID: "a" + handle, Kind: record.Took, Job: job, Handle: handle, Detail: detail,
			Actor: record.Actor{Kind: "person", Name: handle}, Verb: "took",
		})
	}
	events := []live.Event{
		activity(1, "raj", "assigned to raj-secret-mbp"),
		activity(0, "raj", "runner raj-secret-mbp came online"),
		activity(1, "priya", "assigned to priya-laptop"),
		{ID: "9", Kind: "activity", Job: 1, Data: json.RawMessage(`{"detail":"raj-broken-mbp"`)},
		activity(3, "raj", "assigned to raj-third-mbp"),
	}
	body := liveBytes(t, h, token, events)
	for _, leak := range []string{"raj-secret-mbp", "raj-broken-mbp", "raj-third-mbp"} {
		if strings.Contains(body, leak) {
			t.Fatalf("peer machine %q reached the stream: %s", leak, body)
		}
	}
	if !strings.Contains(body, "priya-laptop") {
		t.Fatalf("own machine was redacted: %s", body)
	}
	// The visible job's take is redacted, not dropped.
	if strings.Count(body, "event: activity") < 3 || !strings.Contains(body, `"job":1`) || !strings.Contains(body, `"kind":"`+record.Took+`"`) {
		t.Fatalf("redacted activity lost its shape: %s", body)
	}
}

// The same rules hold through the real handler and the replay cursor.
func TestLiveEventsOverTheWireHidePeerMachinesAndKeepCursors(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}, "raj": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "priya")
	queueAs(t, h, token, false)
	root := h.eventRoot()
	sentinel := live.New("approved", 1, api.LiveEvent{Job: 1, By: "sentinel"})
	appended := []live.Event{
		sentinel,
		live.New("machine", 1, api.LiveEvent{Job: 1, Name: "raj-wire-mbp"}),
		live.New("runner", 0, api.LiveEvent{Name: "raj-wire-runner"}),
		live.New("machine", 1, api.LiveEvent{Job: 1, Name: "priya-wire-mbp"}),
		live.New("activity", 1, record.Event{ID: "w", Kind: record.Took, Job: 1, Handle: "raj", Detail: "raj-wire-activity"}),
	}
	appended[1].Handle, appended[2].Handle, appended[3].Handle = "RAJ", "@raj", "Priya"
	var cursor string
	for i, e := range appended {
		if err := live.Append(root, e); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			_, c, err := live.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			cursor = c
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)
	req.RemoteAddr = "192.0.2.1:9"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", cursor)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != 200 || strings.Contains(body, "raj-wire") || strings.Contains(body, "sentinel") {
		t.Fatalf("wire leak or replayed the cursor: %d %s", w.Code, body)
	}
	if !strings.Contains(body, "priya-wire-mbp") || !strings.Contains(body, "event: activity") {
		t.Fatalf("own machine or redacted activity missing: %s", body)
	}
}

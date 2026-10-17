package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

func TestEventReplayUsesLastIDAndViewerFilter(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "priya")
	// Warm the access cache before canceling the finite replay request.
	queueAs(t, h, token, false)
	machineRequest := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	machineRequest.RemoteAddr = "192.0.2.1:9"
	machineRequest.Header.Set("Authorization", "Bearer "+token)
	for _, tc := range []struct {
		event live.Event
		want  bool
	}{
		{live.Event{Kind: "machine", Job: 2, Handle: "priya"}, false},
		{live.Event{Kind: "machine", Job: 1, Handle: "raj"}, true},
		{live.Event{Kind: "machine", Handle: "priya"}, true},
		{live.Event{Kind: "machine", Handle: "raj"}, false},
	} {
		if got := h.viewerWantsLive(machineRequest, tc.event); got != tc.want {
			t.Fatalf("machine filter %+v = %v", tc.event, got)
		}
	}
	root := h.eventRoot()
	var beforeCursor, afterCursor string
	for i, e := range []live.Event{
		live.New("approved", 1, api.LiveEvent{Job: 1, By: "before"}),
		live.New("taken", 2, api.LiveEvent{Job: 2, By: "hidden"}),
		live.New("approved", 1, api.LiveEvent{Job: 1, By: "after"}),
	} {
		if err := live.Append(root, e); err != nil {
			t.Fatal(err)
		}
		_, cursor, err := live.Read(root)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			beforeCursor = cursor
		}
		if i == 2 {
			afterCursor = cursor
		}
	}
	replay := func(cursor string) string {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)
		req.RemoteAddr = "192.0.2.1:9"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Last-Event-ID", cursor)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("replay: %d %s", w.Code, w.Body)
		}
		return w.Body.String()
	}
	body := replay(beforeCursor)
	if strings.Contains(body, "before") || strings.Contains(body, "hidden") || !strings.Contains(body, "id: "+afterCursor+"\nevent: approved\ndata: {\"job\":1,\"by\":\"after\"}") {
		t.Fatalf("filtered replay: %s", body)
	}
	// The same ids survive a new reader; an unknown cursor gets a refresh tick.
	body = replay("expired")
	if strings.Contains(body, "hidden") || !strings.Contains(body, "before") || !strings.Contains(body, "event: changed\ndata: *") {
		t.Fatalf("expired replay: %s", body)
	}
	if got := replay(""); got != "" {
		t.Fatalf("first connection replayed: %s", got)
	}
}

func TestKeepaliveEvery20SecondsWithoutChangeOrID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := New(factory.Seam{}, Config{})
		defer h.Close()
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)
		req.RemoteAddr = "127.0.0.1:9"
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { h.ServeHTTP(w, req); close(done) }()
		synctest.Wait()
		time.Sleep(20*time.Second - time.Nanosecond)
		synctest.Wait()
		if w.Body.Len() != 0 {
			t.Fatal("early keepalive")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if w.Body.String() != ": keepalive\n\n" {
			t.Fatalf("keepalive: %s", w.Body)
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if w.Body.String() != ": keepalive\n\n: keepalive\n\n" {
			t.Fatalf("second keepalive: %s", w.Body)
		}
		cancel()
		<-done
	})
}

func TestMachineSilencePushesOfflineAtDeadline(t *testing.T) {
	root := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		h := &Handler{config: Config{Root: root}, workers: &workerService{now: time.Now, credentials: map[[32]byte]registeredWorker{
			{}: {Name: "laptop", Last: time.Now(), Worker: lease.Worker{Handle: "priya", Machine: "laptop"}},
		}}}
		defer h.Close()
		h.machineEvents()
		time.Sleep(time.Duration(lease.OfflineMisses) * lease.HeartbeatEvery)
		synctest.Wait()
		events, _, err := live.Read(root)
		if err != nil || len(events) != 4 {
			t.Fatalf("machine transitions: %+v %v", events, err)
		}
		var p api.LiveEvent
		if err := json.Unmarshal(events[2].Data, &p); err != nil || p.Name != "laptop" || p.Status != api.MachineOffline || events[2].Handle != "priya" {
			t.Fatalf("offline: %s %v", events[2].Data, err)
		}
	})
}

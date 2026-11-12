package main

import (
	"fmt"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

func TestMediaInvalidationDeduplicatesRecentDeliveryWithoutRetainingAllSources(t *testing.T) {
	recorder := newAppRecorder()
	backend := &appBackend{}
	live := &appLive{box: appwire.NewOutbox(recorder)}
	first := session.MediaChange{ID: "src_first", Run: "run_actual", Call: "call_actual"}
	for range 20 {
		backend.mediaChanged("agent", live, first)
	}
	for i := range 1000 {
		backend.mediaChanged("agent", live, session.MediaChange{ID: fmt.Sprintf("src_%032x", i)})
	}
	if len(live.mediaSeen) != appMediaRecentMax || len(live.mediaRecent) != appMediaRecentMax {
		t.Fatalf("retained unbounded source history: %d %d", len(live.mediaSeen), len(live.mediaRecent))
	}
	backend.mediaChanged("agent", live, session.MediaChange{})
	live.box.Close()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.events) != 1001 {
		t.Fatalf("notification count %d, expected one per unique append", len(recorder.events))
	}
	firstData, ok := recorder.data[0].(appwire.MediaSourceChanged)
	if !ok || firstData.Source != first.ID || firstData.Run != first.Run || firstData.Call != first.Call || firstData.Agent != "agent" {
		t.Fatalf("lost actual source facts: %+v", recorder.data[0])
	}
}

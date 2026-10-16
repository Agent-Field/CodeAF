package remote

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

func TestMediaSourceChangeSurvivesTheHostEventWire(t *testing.T) {
	original := session.Event{Kind: session.EventMediaUpdate, Media: &session.MediaChange{ID: "src_actual", Run: "run_actual", Call: "call_actual"}}
	raw, err := json.Marshal(WireEvent(original))
	if err != nil {
		t.Fatal(err)
	}
	var wire EventWire
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	got := wire.Unwire()
	if got.Kind != original.Kind || got.Media == nil || *got.Media != *original.Media {
		t.Fatalf("lost source producer event: %+v", got)
	}
}

func TestArtifactChangeSurvivesTheHostEventWire(t *testing.T) {
	original := session.Event{Kind: session.EventArtifactUpdate, Artifact: &session.ArtifactChange{ID: "dlv_actual", Kind: "markdown", Run: "run_actual", Call: "call_actual"}}
	raw, err := json.Marshal(WireEvent(original))
	if err != nil {
		t.Fatal(err)
	}
	var wire EventWire
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	got := wire.Unwire()
	if got.Kind != original.Kind || got.Artifact == nil || *got.Artifact != *original.Artifact {
		t.Fatalf("lost deliverable event: %+v", got)
	}
}

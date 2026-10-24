package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
)

func TestStatusFollowsEngineReplacementWithoutRestartingTheWindow(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	held := "older-engine-build"
	a.build = held
	a.link.EngineBuild = func() string { return held }
	a.slash("/status")
	text := lastNote(t, a)
	for _, want := range []string{"window build", "engine build", held, buildinfo.String()} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in status %s", want, text)
		}
	}
	held = "replacement-engine-build"
	a.slash("/status")
	text = lastNote(t, a)
	if !strings.Contains(text, held) || strings.Contains(text, "older-engine-build") {
		t.Fatalf("status kept the old launch welcome: %s", text)
	}
	if json := a.statusJSON(); !strings.Contains(json, `"engine build":"replacement-engine-build"`) {
		t.Fatal(json)
	}
}

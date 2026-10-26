package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeBuildActionDoesNotHoldNavigation(t *testing.T) {
	a := placeApp(t)
	v := knowledge.Open(t.TempDir())
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: v}
	drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
	if got := placeFrameText(a); !strings.Contains(got, "Build knowledge base") {
		t.Fatal(got)
	}
	drive(t, a, key("enter"))
	if got := placeFrameText(a); !strings.Contains(got, "Building knowledge base") || !strings.Contains(got, "keep chatting") {
		t.Fatalf("working state missing: %s", got)
	}
	drive(t, a, key("esc"))
	if a.kb.open {
		t.Fatal("build held navigation in Knowledge")
	}
	state, err := v.Status()
	if err != nil {
		t.Fatal(err)
	}
	if state.Build.Phase != knowledge.BuildRunning {
		t.Fatal("navigation canceled requested build")
	}
	drive(t, a, key("alt+u"))
	if got := placeFrameText(a); !strings.Contains(got, "Building knowledge base") {
		t.Fatal("return lost build progress: " + got)
	}
}
func TestKnowledgeRefreshShowsCompletedBackgroundPages(t *testing.T) {
	a := knowledgeArticleApp(t)
	v := a.agent.(*knowledgeTestAgent).vault
	// The reader already has a publication. An asynchronous status refresh must
	// retain its article while no newer publication has committed.
	prior := a.kb.reading.Revision
	command := a.knowledgeRead(false)
	if command == nil {
		t.Fatal("missing async refresh")
	}
	msg := command()
	result, ok := msg.(knowledgeReadingMsg)
	if !ok {
		t.Fatalf("unexpected refresh %T", msg)
	}
	if result.reading.Revision != prior || len(result.reading.Pages) == 0 {
		t.Fatal("unchanged refresh discarded published pages")
	}
	drive(t, a, result)
	if got := placeFrameText(a); !strings.Contains(got, "Architecture") {
		t.Fatal(got)
	}
	if err := v.Flush(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	drive(t, a, a.knowledgeRead(false)())
	if len(a.kb.reading.Pages) != 0 {
		t.Fatal("reader did not adopt committed publication")
	}
}

func TestKnowledgeLocalEngineReadsInjectedProfileVault(t *testing.T) {
	a := placeApp(t)
	v := knowledge.Open(t.TempDir())
	// The local engine adapter has no file-backed Knowledge method; the local
	// application door supplies the engine's resolved profile explicitly.
	a.agent = &knowledgeTestAgent{fakeAgent: &fakeAgent{model: "m"}, vault: nil}
	a.knowledgeProfile = v
	drive(t, a, key(placeChord(pageMemory)), key("alt+u"))
	if got := placeFrameText(a); !strings.Contains(got, "Build knowledge base") {
		t.Fatalf("local engine lost native knowledge: %s", got)
	}
	drive(t, a, key("enter"))
	if !v.Enabled() {
		t.Fatal("local build did not use injected profile")
	}
	a.host = "remote.example"
	a.kb = knowledgePlace{}
	if a.knowledgeVault() != nil {
		t.Fatal("hosted conversation read local profile knowledge")
	}
}

func TestKnowledgeBuildShowsStageProgressWithoutClaimingCompletion(t *testing.T) {
	a := knowledgeArticleApp(t)
	a.kb.reading.Build = knowledge.BuildStatus{Phase: knowledge.BuildRunning, Chats: 303, Imported: 303, Sources: 8149}
	a.kb.reading.Pending = 7929
	a.touch()
	got := placeFrameText(a)
	for _, want := range []string{"History imported: 303 of 303", "Sources processed: 220 of 8149", "2%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	a.kb.reading.Pending = 0
	a.touch()
	if got = placeFrameText(a); !strings.Contains(got, "Organizing knowledge pages") {
		t.Fatalf("finished sources hid remaining organization:\n%s", got)
	}
}

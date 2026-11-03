package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// A SPOT'S PEEK, THROUGH THE APP'S DOOR on this machine's factory: the spot's
// hunks from the job's change and the writer's note after its hunk.
func TestAppFactoryJobBriefSpotReadsTheSpotsHunks(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	if !strings.Contains(s.hello, appwire.FeatureFactoryJobBriefSpot) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryJobBriefSpot)
	}
	it := factory.Item{Repo: "acme/api", Num: 7, Title: "Fix", Kind: factory.KindIssue, State: factory.StateRunning, Change: &factory.Change{
		Patch: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,3 @@ func Send() {\n x\n+y\n@@ -9 +10,2 @@ func Other() {\n+z\n",
	}}
	it.KeepBrief(factory.BriefVersion{Verdict: "Fixed.", Blocks: []factory.BriefBlock{{Kind: factory.BlockWhereToLook, Heading: "Where to look", Rest: "nothing else",
		Spots: []factory.BriefSpot{{ID: "1", File: "a.go", Symbol: "Send", Lines: 1, Why: "the send", Notes: []factory.BriefNote{{Hunk: "a.go:1", Text: "adds y"}}}}}}})
	kept, err := s.backend.local.host.floor.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	var got appwire.FactoryJobBriefSpot
	s.must("factory.job_brief_spot", map[string]any{"factory": st.Factory, "job": kept.ID, "spot": "1"}, &got)
	if got.Rev != 1 || got.File != "a.go" || got.Why != "the send" || len(got.Hunks) != 1 || !strings.Contains(got.Hunks[0].Text, "+y") ||
		len(got.Annotations) != 1 || got.Annotations[0].AfterHunk != 0 {
		t.Fatalf("spot = %+v", got)
	}
	s.mustFail("factory.job_brief_spot", map[string]any{"factory": st.Factory, "job": kept.ID, "spot": "2"}, appwire.CodeNotFound)
}

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// RetractedFacts was written with a doc comment describing exactly this wiring —
// "the lines a derivation prompt can be shown as already-rejected" — and then
// had zero non-test callers. The store refused the re-derivation every week, in
// silence, and the model that produced it learned nothing, so it produced it
// again the week after off the same evidence.
func TestTheDistillerIsShownWhatTheUserThrewAway(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	const refused = "the user wants every report to open with a competitive landscape"
	vetoed, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "user", store.FactPreference, refused)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(vetoed.Seq, vetoed.Seq, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}

	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model", response: `{"facts":[]}`}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	if _, err := distillFacts(settings, client, graph)(context.Background(),
		"write this week's investor update", "delivered the update", false); err != nil {
		t.Fatal(err)
	}
	body := capture.messages[len(capture.messages)-1].Content[0].Text
	if !strings.Contains(body, refused) {
		t.Fatalf("the distiller was not shown the vetoed line:\n%s", body)
	}
	if !strings.Contains(body, "Already rejected") {
		t.Fatalf("the vetoed line arrived without the instruction that makes it usable:\n%s", body)
	}
}

func TestTheRetrospectiveIsShownWhatTheUserThrewAway(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	const refused = "the user prefers a weekly cadence for competitor checks"
	vetoed, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "user", store.FactPreference, refused)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(vetoed.Seq, vetoed.Seq, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}

	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model", response: `{"facts":[]}`}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	if _, err := reflectAcrossJobs(settings, client, graph)(context.Background(), []resident.JobSketch{
		{Ask: "check what our competitors shipped", Outcome: "three launches", Age: "2 days ago"},
		{Ask: "check what our competitors shipped", Outcome: "one launch", Age: "9 days ago"},
	}); err != nil {
		t.Fatal(err)
	}
	body := capture.messages[len(capture.messages)-1].Content[0].Text
	if !strings.Contains(body, refused) {
		t.Fatalf("the retrospective was not shown the vetoed line:\n%s", body)
	}
}

// The swallow announced a write that never happened: recordFact returned the
// quarantined row with a nil error and no event, so the reconciler queued a
// learning moment for it and, when the distiller had named something to
// supersede, retired a live belief in favour of a row no retrieval can return.
func TestARefusedRederivationIsNotALearningMoment(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	const refused = "the user wants tables instead of prose"
	vetoed, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "user", store.FactPreference, refused)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(vetoed.Seq, vetoed.Seq, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}
	standing, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "user", store.FactPreference,
		"the user reads deliverables on a narrow screen")
	if err != nil {
		t.Fatal(err)
	}

	again, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "user", store.FactPreference, refused)
	if err == nil {
		t.Fatalf("the store re-derived a vetoed belief silently: %+v", again)
	}
	// Nothing new was written, so nothing may be announced and nothing may be
	// retired: the live belief a mis-fired supersession would have taken with it
	// is still standing.
	live, found, err := graph.FactBySeq(standing.Seq)
	if err != nil || !found || live.Status != store.FactActive {
		t.Fatalf("a live belief was retired by a write that never happened: %+v found=%t err=%v", live, found, err)
	}
}

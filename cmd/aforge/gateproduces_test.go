package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A deliverable that names a file the run never produced is a gate failure
// before the model judge is bought. The scripted client records zero calls —
// the absence is mechanical — and the gap names the file verbatim, in the same
// shape a judged gap takes, so the one-round repair flow the gate already owns
// runs on it unchanged.
func TestAMissingNamedProduceFailsTheGateBeforeTheJudgeIsCalled(t *testing.T) {
	graph := openCacheStore(t)
	node := store.Node{
		ID:         "job",
		Brief:      "write the weekly report",
		Provenance: store.Provenance{Intent: "write report.md, the weekly report", SessionID: "s"},
	}
	record := revision.Evidence{
		Done:     plan.Done{Produces: []string{"report.md"}},
		Observed: true,
	}
	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	judgment := revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, capture.model, capture), graph, node,
		"The report is written and verified.", "", record, "worker/model")
	if judgment.Pass || !judgment.Checked {
		t.Fatalf("judgment = %+v, want a checked fail", judgment)
	}
	if !strings.Contains(judgment.Gaps, "report.md") {
		t.Fatalf("gap = %q, want it to name report.md verbatim", judgment.Gaps)
	}
	if judgment.Quote != judgment.Gaps {
		t.Fatalf("quote = %q, want it to carry the verbatim names so the repair path grounds them", judgment.Quote)
	}
	if capture.messages != nil {
		t.Fatalf("the model judge was called %d time(s); the absence is mechanical and the round is free", len(capture.messages))
	}
}

// A present, non-empty named file reaches the judge exactly as it did before
// the mechanical check existed: the judge is called, its verdict stands, and
// nothing here fired.
func TestAPresentNamedProduceReachesTheJudgeAsBefore(t *testing.T) {
	dir := t.TempDir()
	produced := filepath.Join(dir, "report.md")
	if err := os.WriteFile(produced, []byte("the weekly report"), 0o600); err != nil {
		t.Fatal(err)
	}
	graph := openCacheStore(t)
	node := store.Node{
		ID:         "job",
		Brief:      "write the weekly report",
		Provenance: store.Provenance{Intent: "write report.md, the weekly report", SessionID: "s"},
	}
	record := revision.Evidence{
		Done:      plan.Done{Produces: []string{"report.md"}},
		Artifacts: []string{produced},
		Observed:  true,
	}
	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	judgment := revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, capture.model, capture), graph, node,
		"The report.", "", record, "worker/model")
	if !judgment.Checked || !judgment.Pass {
		t.Fatalf("judgment = %+v, want the judge's own checked pass", judgment)
	}
	if capture.messages == nil {
		t.Fatal("the model judge was not called; a present, non-empty named file must reach the judge as before")
	}
}

// A criterion that names no file — an abstract output, or nothing at all — is
// the legal everywhere-default case, and the path is byte-identical to before:
// the judge is called and its verdict stands.
func TestACriterionThatNamesNoFilesReachesTheJudgeUnchanged(t *testing.T) {
	graph := openCacheStore(t)
	node := store.Node{
		ID:         "job",
		Brief:      "summarise the findings",
		Provenance: store.Provenance{Intent: "summarise the findings as a short note", SessionID: "s"},
	}
	record := revision.Evidence{
		Done:     plan.Done{Produces: []string{"the summary"}},
		Observed: true,
	}
	settings := config.Config{Model: "worker/model"}
	capture := &gateCaptureClient{model: "worker/model"}
	revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, capture.model, capture), graph, node,
		"Here is the summary.", "", record, "worker/model")
	if capture.messages == nil {
		t.Fatal("the model judge was not called; a criterion that names no files must be unchanged")
	}
}

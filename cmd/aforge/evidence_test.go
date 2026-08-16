package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The gate judged a final message and nothing else, so the strongest sentence
// in the language cost a worker nothing to write. It now receives what the leaf
// left behind and the tail of what it ran — both already existed — and the
// records sit below the deliverable so a repair pass still moves the
// deliverable first.
func TestTheGateIsHandedTheFilesAndTheRun(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	node := store.Node{
		ID: "job", Brief: "produce the summary",
		Provenance: store.Provenance{Intent: "summarise it", SessionID: "s1"},
	}

	dir := t.TempDir()
	landed := filepath.Join(dir, "summary.md")
	if err := os.WriteFile(landed, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "never-written.md")

	capture := &gateCaptureClient{model: "worker/model"}
	client := adoptLiveClient(settings, capture.model, capture)
	revision.JudgeDeliverable(context.Background(), settings, client, graph, node,
		"Here is the summary. I verified it end to end.",
		"", revision.Evidence{
			Artifacts: []string{landed, missing},
			Ran:       []string{`write {"path":"summary.md"}`, `sh {"command":"wc -l summary.md"}`},
		}, "worker/model")

	body := capture.messages[len(capture.messages)-1].Content[0].Text
	for name, want := range map[string]string{
		"the file that exists carries its size": landed + " (5 bytes)",
		"the file that does not is named as so": missing + " (not on disk)",
		"what ran is in the prompt":             `sh {"command":"wc -l summary.md"}`,
		"and it is named as a tail":             "The last 2 things the work ran, oldest first:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the gate was not handed %s: %q missing from\n%s", name, want, body)
		}
	}
	deliverable := strings.Index(body, "Deliverable as produced:\n")
	records := strings.Index(body, "What actually happened, as recorded while it ran:\n")
	if deliverable < 0 || records < deliverable {
		t.Fatalf("the records do not sit below the deliverable: deliverable=%d records=%d", deliverable, records)
	}

	// Nothing to show shows nothing: a leaf that wrote no files and ran no
	// tools must not grow an empty block in every gate prompt it ever sees.
	bare := &gateCaptureClient{model: "worker/model"}
	revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, bare.model, bare), graph, node,
		"Here is the summary.", "", revision.Evidence{}, "worker/model")
	if got := bare.messages[len(bare.messages)-1].Content[0].Text; strings.Contains(got, "What actually happened") {
		t.Errorf("an empty evidence block reached the gate:\n%s", got)
	}
}

// The gate's answer about evidence is a field of its own rather than something
// read back out of its prose, because the prose is exactly what was not
// trustworthy in the first place.
func TestTheGateReturnsEvidenceAsItsOwnAnswer(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	node := store.Node{ID: "job", Brief: "produce it", Provenance: store.Provenance{Intent: "produce it"}}

	judge := func(reply string) revision.Judgment {
		t.Helper()
		capture := &gateCaptureClient{model: "worker/model", response: reply}
		return revision.JudgeDeliverable(context.Background(), settings,
			adoptLiveClient(settings, capture.model, capture), graph, node,
			"done", "", revision.Evidence{}, "worker/model")
	}

	// An honest gap — nothing here could run it — passes, and is not evidence.
	honest := judge(`{"pass":true,"exercised":false}`)
	if !honest.Checked || !honest.Pass || honest.Exercised {
		t.Fatalf("an honest unverified pass did not land as one: %+v", honest)
	}
	// A judge that says nothing about evidence has said nothing, and nothing
	// is false.
	silent := judge(`{"pass":true}`)
	if !silent.Checked || !silent.Pass || silent.Exercised {
		t.Fatalf("a silent judge manufactured evidence: %+v", silent)
	}
	evidenced := judge(`{"pass":true,"exercised":true}`)
	if !evidenced.Checked || !evidenced.Pass || !evidenced.Exercised {
		t.Fatalf("an evidenced pass lost its evidence: %+v", evidenced)
	}
	// A claim the records contradict is a gap like any other, and a named gap
	// is still what buys the revision.
	const gap = "the deliverable says the whole thing was run end to end, and nothing in what ran did that"
	failed := judge(`{"pass":false,"gaps":"` + gap + `"}`)
	if !failed.Checked || failed.Pass || failed.Gaps != gap {
		t.Fatalf("an unevidenced claim of verification did not draw a checked gap: %+v", failed)
	}
}

// Verdict laundering: the leaf itself never claims a verified success, and for
// a while a gate PASS overwrote its honest verdict with the strongest one there
// is — on the strength of a sentence. Only the evidenced pass may say that now.
func TestOnlyAnEvidencedGatePassRecordsAVerifiedSuccess(t *testing.T) {
	if got := revision.GateVerdict(revision.Judgment{Pass: true, Checked: true}); got != provider.VerdictUnverifiedSuccess {
		t.Errorf("an unevidenced pass recorded %q, want a plain success", got)
	}
	if got := revision.GateVerdict(revision.Judgment{Pass: true, Checked: true, Exercised: true}); got != provider.VerdictVerifiedSuccess {
		t.Errorf("an evidenced pass recorded %q, want a verified success", got)
	}
	// The two part company in exactly one place, and it is the place the
	// laundering did its damage: what may move an ability rating.
	if positive, graded := provider.VerdictUnverifiedSuccess.Graded(); graded || positive {
		t.Errorf("a plain success grades: positive=%v graded=%v", positive, graded)
	}
	if positive, graded := provider.VerdictVerifiedSuccess.Graded(); !graded || !positive {
		t.Errorf("a verified success stopped grading: positive=%v graded=%v", positive, graded)
	}
	// And nowhere else: every surface that counts operational success counts
	// both, so demoting an unevidenced pass must not read as a failure.
	if provider.VerdictUnverifiedSuccess.Escalates() {
		t.Error("a plain success now escalates, which would re-run finished work on a stronger model")
	}
}

// The evidence contract, stated as a value and never as a cue list: the gate
// checks claims against the record, and the record's silence is evidence rather
// than proof — a gate told otherwise starts failing honest work for having run
// somewhere it cannot see.
func TestTheGateWeighsTheRecordWithoutAuditingIt(t *testing.T) {
	for name, required := range map[string]string{
		"the records are named":           "two records of the run itself: what it left behind, and the tail of what it actually ran",
		"claims are read against them":    "Read the deliverable's claims against them, the way the person would",
		"an unsupported claim is a gap":   "is an element unsupported by evidence and is a gap of exactly the kind above",
		"they are partial by design":      "Both records are partial by construction",
		"silence convicts, never acquits": "they can convict a claim and never acquit one — silence in them is evidence, never proof",
		"evidence is not quality":         `"exercised" is a statement about evidence and never about quality`,
		"the honest exit still passes":    `including an honest "not verified here"`,
	} {
		if !strings.Contains(revision.DeliverablePrompt, required) {
			t.Errorf("the gate no longer holds %s: %q missing", name, required)
		}
	}
	// The blocks keep their order: the evidence paragraph rides under the
	// substance test it extends, and the JSON contract stays last.
	substance := strings.Index(revision.DeliverablePrompt, "One absence counts exactly like every other")
	records := strings.Index(revision.DeliverablePrompt, "Below the deliverable, whenever there is anything to show")
	json := strings.Index(revision.DeliverablePrompt, "Return exactly one JSON object")
	if substance < 0 || records < substance || json < records {
		t.Fatalf("the gate's blocks were reordered: substance=%d records=%d json=%d", substance, records, json)
	}
	// No cue list, here least of all: the gate must judge whether the evidence
	// is there, never whether a sentence pattern is.
	for _, forbidden := range []string{"if the text contains", "phrases such as", "the word \"verified\""} {
		if strings.Contains(strings.ToLower(revision.DeliverablePrompt), strings.ToLower(forbidden)) {
			t.Errorf("the evidence clause grew a cue list: %q", forbidden)
		}
	}
}

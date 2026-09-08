package revision

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAFaultedGateRowSaysWhy(t *testing.T) {
	const (
		rowWithoutReason      = "the review could not be read, so this delivery was never checked"
		handoverWithoutReason = "I'm handing this over unchecked: the review of it could not be read, " +
			"so nothing has confirmed this is what you asked for."
		reason = "unexpected end of JSON input"
	)
	fault := "the gate answered with nothing this could read: " + reason
	row := GateFaultWords(fault)
	handover := GateFaultHandover(fault)

	if !strings.Contains(row, rowWithoutReason) || !strings.Contains(row, reason) {
		t.Fatalf("the row does not name both the missing check and its reason: %q", row)
	}
	if !strings.Contains(handover, gateFaultHandoverLead) || !strings.Contains(handover, reason) {
		t.Fatalf("the handover does not name both the missing check and its reason: %q", handover)
	}
	for name, words := range map[string]string{"row": row, "handover": handover} {
		if strings.Contains(words, gateUnreadableWhy) {
			t.Errorf("%s repeats the folded-out lead: %q", name, words)
		}
		if strings.Count(words, reason) != 1 {
			t.Errorf("%s says its reason %d times, want once: %q", name, strings.Count(words, reason), words)
		}
		if strings.Contains(words, "work") {
			t.Errorf("%s alleges something about the work: %q", name, words)
		}
	}

	for _, empty := range []string{"", "   "} {
		if got := GateFaultWords(empty); got != rowWithoutReason {
			t.Errorf("GateFaultWords(%q) = %q, want the unchanged sentence", empty, got)
		}
		if got := GateFaultHandover(empty); got != handoverWithoutReason {
			t.Errorf("GateFaultHandover(%q) = %q, want the unchanged sentence", empty, got)
		}
	}

	const otherFault = "the checker never answered"
	if got := GateFaultWords(otherFault); !strings.Contains(got, otherFault) {
		t.Errorf("the row dropped an unfamiliar fault: %q", got)
	}
	if got := GateFaultHandover(otherFault); !strings.Contains(got, otherFault) {
		t.Errorf("the handover dropped an unfamiliar fault: %q", got)
	}
}

func TestAnUnreadableVerdictCarriesItsOwnWordsToTheRowAndTheHandover(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	const prose = "I could not evaluate this deliverable."
	proseJudge := &scriptedJudge{replies: []*ai.Response{said(prose), said(prose)}}
	proseFault := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, proseJudge.Model(), proseJudge), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")

	cutJudge := &scriptedJudge{replies: []*ai.Response{
		cutVerdict(`{"pass":false,"gaps":"the comparison never`),
		spentThinking(shaped.Room(shaped.Ask{Lane: "gate"}, "")),
	}}
	cutFault := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, cutJudge.Model(), cutJudge), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")

	for name, run := range map[string]struct {
		judgment Judgment
		ownWords string
	}{
		"prose": {judgment: proseFault, ownWords: prose},
		"cut":   {judgment: cutFault, ownWords: "finish_reason=length"},
	} {
		if run.judgment.Fault == "" {
			t.Fatalf("%s answer left no fault: %+v", name, run.judgment)
		}
		if got := GateFaultWords(run.judgment.Fault); !strings.Contains(got, run.ownWords) {
			t.Errorf("%s row dropped the answer's own failure: %q", name, got)
		}
		if got := GateFaultHandover(run.judgment.Fault); !strings.Contains(got, run.ownWords) {
			t.Errorf("%s handover dropped the answer's own failure: %q", name, got)
		}
	}
	if proseFault.Fault == cutFault.Fault {
		t.Fatalf("different unreadable answers produced the same fault: %q", proseFault.Fault)
	}
	if GateFaultWords(proseFault.Fault) == GateFaultWords(cutFault.Fault) {
		t.Fatalf("different unreadable answers produced the same row: %q", GateFaultWords(proseFault.Fault))
	}
}

func TestAGateThatCouldNotBeReachedGainsNoFaultSentence(t *testing.T) {
	weather := errors.New("dial tcp 10.0.0.1:443: connect: connection refused")
	unreached, _ := judgeTheUnreachable(context.Background(), weather)
	wantUnjudged := GateUnreached + " · " + gateAskedTwice + " · " + weather.Error()
	if !unreached.Pass || unreached.Checked || unreached.Fault != "" {
		t.Fatalf("an unreachable gate stopped being its fail-open pass: %+v", unreached)
	}
	if unreached.Unjudged != wantUnjudged {
		t.Fatalf("the unreachable gate's note changed: %q, want %q", unreached.Unjudged, wantUnjudged)
	}
	if strings.Contains(unreached.Unjudged, gateFaultSentence) {
		t.Fatalf("an unreachable gate acquired the fault sentence: %q", unreached.Unjudged)
	}

	settings := config.Config{Model: "worker/model"}
	passing := &scriptedJudge{replies: []*ai.Response{said(`{"pass":true,"exercised":true}`)}}
	passed := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, passing.Model(), passing), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if !passed.Pass || passed.Fault != "" || passed.Unjudged != "" {
		t.Fatalf("a passing gate acquired a missing-check sentence: %+v", passed)
	}
}

func TestAFaultedRowTellsTheNextWorkerWhy(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	const id = "task-1"
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: "compare the two parsers", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "compare the two parsers"}); err != nil {
		t.Fatal(err)
	}
	const reason = "unexpected end of JSON input"
	fault := gateUnreadableWhy + ": " + reason
	if err := graph.RecordDeliveryGate(id, store.DeliveryGate{
		Gap: GateFaultWords(fault), Unclosed: true,
	}); err != nil {
		t.Fatal(err)
	}

	words := resident.ReadOpenFindings(graph, id).Words()
	if !strings.Contains(words, "What the last review found missing:") || !strings.Contains(words, reason) {
		t.Fatalf("the next worker was not told why the check was missing:\n%s", words)
	}
}

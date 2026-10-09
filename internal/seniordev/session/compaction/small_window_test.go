//go:build !windows

package compaction

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

// sixteenKModel is the window senior-dev assumed for every model when its
// catalog could not be read: 16,384 tokens of context and 2,048 of output, so
// a high watermark of 8,601 and a rebuild that must land at or under 6,553.
func sixteenKModel() Model {
	model := serviceModel()
	model.Overflow = overflow.Model{Limit: calc.ModelLimit{Context: 16_384, Output: 2_048}}
	return model
}

// longSummary is a valid state record far larger than a 16K window can hold
// beside its baseline, with a marker at each end so a test can see what of
// it was carried.
func longSummary(boundary int) string {
	lines := []string{fmt.Sprintf("- FIRST-%d: reproduced the crash in src/parse.c", boundary)}
	for step := 0; step < 160; step++ {
		lines = append(lines, fmt.Sprintf("- step %d: read a file, ran a command, noted what it said about the bug", step))
	}
	lines = append(lines, fmt.Sprintf("- LAST-%d: the patch in src/parse.c builds and the PoC no longer crashes", boundary))
	return strings.Join([]string{
		"## Working State",
		"### Completed", strings.Join(lines, "\n"),
		"### Current", "- verify the patch",
		"### Verification", "- the PoC ran clean once",
		"### Next", "- write /output/fix.patch",
		"### Files", "- src/parse.c",
	}, "\n")
}

// THE 16K LOOP, REPRODUCED. A session on a 16K window compacts again and
// again, and every time the generated summary is too big to fit beside the
// four thousand tokens the system prompt and tools always cost. Before, each
// rebuild installed a stub that said "No generated completion claim survived
// the capacity rebuild" and the session started over from nothing, which is
// how a CyberGym run spent 25 minutes rediscovering the same bug. Now each
// rebuild still fits, and it carries the newest record forward, cut to the
// room the window has.
func TestASixteenKSessionKeepsItsProgressThroughRepeatedCapacityRebuilds(t *testing.T) {
	ctx := context.Background()
	messages := compactionConversation("coder")
	store := &memoryStore{messages: messages}
	deps := baseDeps(store)
	deps.Provider = &fakeProvider{model: sixteenKModel()}
	deps.ChangedFiles = func(context.Context) []string { return []string{"src/parse.c"} }
	var service *Service
	// The sizer counts what the real one does: the messages, plus a fixed
	// four thousand tokens of system prompt and tools.
	deps.Sizer = ContextSizerFunc(func(
		_ context.Context, messages []msgmodel.WithParts, model Model,
	) (float64, error) {
		tokens, err := service.Estimate(messages, model)
		return tokens + 4_000, err
	})
	decisions := []CompactionDecision{}
	deps.Decisions = DecisionSinkFunc(func(decision CompactionDecision) {
		decisions = append(decisions, decision)
	})
	boundary := 0
	deps.Processors = ProcessorFactoryFunc(func(_ context.Context, a *msgmodel.Assistant, _ string, _ Model) (SummaryProcessor, error) {
		return &fakeProcessor{message: a, process: func(ctx context.Context, _ SummaryRequest) (steploop.Result, error) {
			finish := "stop"
			a.Finish = &finish
			if err := store.UpdateMessage(ctx, *a); err != nil {
				return steploop.ResultStop, err
			}
			err := store.UpdatePart(ctx, textPart(a.ID, longSummary(boundary)))
			return steploop.ResultContinue, err
		}}, nil
	})
	service = NewService(deps)
	for boundary = 1; boundary <= 3; boundary++ {
		parent := "uc"
		if boundary > 1 {
			parent = fmt.Sprintf("uc%d", boundary)
			part := msgmodel.CompactionPart{PartBase: msgmodel.PartBase{
				ID: "pc" + parent, MessageID: parent, SessionID: "ses_1",
			}}
			user := testUser(parent, part)
			if err := store.UpdateMessage(ctx, user.Info); err != nil {
				t.Fatal(err)
			}
			if err := store.UpdatePart(ctx, part); err != nil {
				t.Fatal(err)
			}
			messages, _ = store.Messages(ctx, "ses_1")
		}
		result, err := service.Process(ctx, ProcessInput{
			ParentID: parent, Messages: messages, SessionID: "ses_1",
		})
		if err != nil || result != steploop.ResultContinue {
			t.Fatalf("boundary %d: %s %v (decisions %#v)", boundary, result, err, decisions)
		}
		decision := decisions[len(decisions)-1]
		if decision.Status != compactionStatusRebuilt || !decision.StubbedSummary || decision.StubCarriedChars == 0 {
			t.Fatalf("boundary %d: decision = %#v, want a rebuilt stub that carried the record", boundary, decision)
		}
		if decision.After > decision.High-minimumContinuationHeadroom(overflow.CompactionWatermarks{High: decision.High, Low: decision.Low}) {
			t.Fatalf("boundary %d: rebuild landed at %.0f, past the window's room", boundary, decision.After)
		}
		fresh, _ := store.Messages(ctx, "ses_1")
		text := generatedSummaryText(fresh[completedCompactions(fresh)[boundary-1].AssistantIndex])
		if text == nil {
			t.Fatalf("boundary %d: no state record", boundary)
		}
		if strings.Contains(*text, "No generated completion claim survived") {
			t.Fatalf("boundary %d erased the session's progress: %s", boundary, *text)
		}
		for _, want := range []string{fmt.Sprintf("FIRST-%d", boundary), fmt.Sprintf("LAST-%d", boundary), "- src/parse.c"} {
			if !strings.Contains(*text, want) {
				t.Fatalf("boundary %d lost %q from its state record: %s", boundary, want, *text)
			}
		}
	}
}

// A CARRY THAT DOES NOT FIT FALLS BACK TO THE BARE STUB, never to the end of
// the session: a rebuild that remembers less and fits beats one that
// remembers more and stops the run.
func TestACarriedRecordThatDoesNotFitFallsBackToTheBareStub(t *testing.T) {
	tail := "u0"
	compactionPart := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
		Auto:     true, TailStartID: &tail,
	}
	finish := "stop"
	summaryFlag := true
	summary := testAssistant("as", "uc", textPart("as", testValidSummary("Fixed src/a.ts")))
	assistant := summary.Info.(msgmodel.Assistant)
	assistant.Summary = &summaryFlag
	assistant.Finish = &finish
	summary.Info = assistant
	store := &memoryStore{messages: []msgmodel.WithParts{
		testUser("u0", textPart("u0", "Fix src/a.ts")), testUser("uc", compactionPart), summary,
	}}
	// First, tail-dropped, bare stub; then each carry overshoots and the bare
	// stub is put back.
	sizes := []float64{90_000, 70_000, 20_000, 59_000, 20_000, 58_000, 20_000}
	deps := baseDeps(store)
	deps.Sizer = ContextSizerFunc(func(context.Context, []msgmodel.WithParts, Model) (float64, error) {
		value := sizes[0]
		sizes = sizes[1:]
		return value, nil
	})
	decision, err := NewService(deps).enforceWatermarks(
		context.Background(), "ses_1", 95_000, serviceModel(), watermarkTestConfig(),
		&compactionPart, "original user request", nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != compactionStatusRebuilt || decision.After != 20_000 ||
		decision.StubCarriedChars != 0 || len(sizes) != 0 {
		t.Fatalf("decision = %#v; remaining sizes = %#v", decision, sizes)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	generated := generatedSummaryText(fresh[2])
	if generated == nil || !strings.Contains(*generated, "No generated completion claim survived") ||
		strings.Contains(*generated, "Fixed src/a.ts") {
		t.Fatalf("a carry that did not fit was kept: %v", generated)
	}
}

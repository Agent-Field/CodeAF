package run

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

type measuringMaker struct {
	*FakeMaker
	clears   int
	measured *atomic.Int32
	base     *atomic.Value
}

type measuringConversation struct {
	Conversation
	m measuringMaker
}

const measuredRed = `'go' 'test' './internal/home' '-count=1' (in /work) · failed`

func (c measuringConversation) notes() []string {
	if c.m.measured.Load() > 0 && c.m.clears > 0 {
		pin, _ := c.m.base.Load().(string)
		return []string{`Check receipt: {"exit":1,"outcome":"failed","base_commit":"` + pin + `","base_receipt_id":"factory-base-1"}`, verification.BaselineNotePrefix + pin + "."}
	}
	return []string{`Check receipt: {"exit":1,"outcome":"failed"}`, verification.FailurePrefix + " " + measuredRed}
}
func (c measuringConversation) ExecutionNotes() ([]string, *factory.TestCounts) {
	return c.notes(), nil
}
func (c measuringConversation) VerificationEvidence() ([]string, *factory.TestCounts, string) {
	return c.notes(), nil, ""
}
func (c measuringConversation) MeasureBase(_ context.Context, base string) (int, error) {
	c.m.base.Store(base)
	c.m.measured.Add(1)
	return c.m.clears, nil
}
func (m measuringMaker) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	c, err := m.FakeMaker.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return measuringConversation{Conversation: c, m: m}, nil
}

// A Test round that ends with red checks has them compared with the base by
// the factory before anyone is asked. Failures the base already has pass the
// step even though the step itself said it was not done; new ones still block.
func TestRedChecksAreComparedWithTheBaseWhenARoundEnds(t *testing.T) {
	const pin = "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct {
		name   string
		clears int
		until  string
		met    bool
		ran    int32
	}{
		{"every failure is on the base", 1, factory.UntilNoNewFailures, true, 1},
		{"a failure is new", 0, factory.UntilNoNewFailures, false, 1},
		{"a strict green step is never compared", 1, factory.UntilGreen, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maker := measuringMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: false, Exit: 1, Output: "One check is red. It fails on the base too."})
			}}, clears: tc.clears, measured: &atomic.Int32{}, base: &atomic.Value{}}
			job := reviewJob()
			job.Item.Kind, job.Item.ChangeBase = factory.KindIssue, pin
			job.Stage = factory.Stage{Name: "test", Kind: factory.StageChat, Until: tc.until}
			result, err := NewChatExecutor(maker).Run(context.Background(), job)
			if err != nil {
				t.Fatal(err)
			}
			if got := maker.measured.Load(); got != tc.ran {
				t.Fatalf("the base was measured %d times, want %d", got, tc.ran)
			}
			if factory.Met(job.Stage, result) != tc.met {
				t.Fatalf("met = %v, result = %+v", !tc.met, result)
			}
			if tc.met {
				if base, _ := maker.base.Load().(string); base != pin {
					t.Fatalf("measured against %q", base)
				}
				if !strings.Contains(result.Output, sayBaseCleared) || !strings.HasPrefix(result.Output, verification.BaselineNotePrefix+pin) || verificationReason(result) != "" || result.VerificationFailureKey != "" {
					t.Fatalf("output:\n%s\nnotes: %q", result.Output, result.Notes)
				}
				return
			}
			if verificationReason(result) == "" || !strings.HasPrefix(result.Output, "Verification blocked: ") {
				t.Fatalf("a red check stopped blocking: %+v", result)
			}
		})
	}
}

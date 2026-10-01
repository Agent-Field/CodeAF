//go:build !windows

package compaction

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

// Repeated boundaries and the capacity rebuild must preserve durable direction.
func TestSteeringSurvivesRepeatedCompactionAndCapacityRebuild(t *testing.T) {
	ctx := context.Background()
	messages := compactionConversation("coder")
	store := &memoryStore{messages: messages}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".senior-dev"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".senior-dev", "steering.md"), []byte("- the person: KEEP THIS DIRECTION\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := baseDeps(store)
	deps.Instance = InstanceContext{Directory: dir}
	deps.Provider = &fakeProvider{model: serviceModel()}
	deps.Processors = ProcessorFactoryFunc(func(_ context.Context, a *msgmodel.Assistant, _ string, _ Model) (SummaryProcessor, error) {
		return &fakeProcessor{message: a, process: func(ctx context.Context, _ SummaryRequest) (steploop.Result, error) {
			finish := "stop"
			a.Finish = &finish
			if err := store.UpdateMessage(ctx, *a); err != nil {
				return steploop.ResultStop, err
			}
			err := store.UpdatePart(ctx, textPart(a.ID, testValidSummary("continue")))
			return steploop.ResultContinue, err
		}}, nil
	})
	service := NewService(deps)
	for i := 0; i < 2; i++ {
		parent := "uc"
		if i == 1 {
			parent = "uc2"
			p := msgmodel.CompactionPart{PartBase: msgmodel.PartBase{ID: "pc2", MessageID: parent, SessionID: "ses_1"}}
			u := testUser(parent, p)
			_ = store.UpdateMessage(ctx, u.Info)
			_ = store.UpdatePart(ctx, p)
			messages, _ = store.Messages(ctx, "ses_1")
		}
		result, err := service.Process(ctx, ProcessInput{ParentID: parent, Messages: messages, SessionID: "ses_1", Auto: false})
		if err != nil || result != steploop.ResultContinue {
			t.Fatalf("boundary %d: %s %v", i+1, result, err)
		}
		fresh, _ := store.Messages(ctx, "ses_1")
		prior := completedCompactions(fresh)
		latest := fresh[prior[len(prior)-1].AssistantIndex]
		if !hasPinnedDirection(latest) {
			t.Fatalf("boundary %d lost steering", i+1)
		}
		t.Logf("boundary %d keeps steering", i+1)
	}
	if err := service.installCapacityFallback(ctx, "ses_1", nil, "original user request", 9000, overflow.CompactionWatermarks{High: 8000, Low: 6000}); err != nil {
		t.Fatal(err)
	}
	fresh, _ := store.Messages(ctx, "ses_1")
	prior := completedCompactions(fresh)
	latest := fresh[prior[len(prior)-1].AssistantIndex]
	if !hasPinnedDirection(latest) {
		t.Fatal("capacity rebuild erased the steering pin after two successful compactions")
	}
}

func hasPinnedDirection(m msgmodel.WithParts) bool {
	for _, raw := range m.Parts {
		if p, ok := raw.(msgmodel.TextPart); ok && !boolPointer(p.Ignored) && strings.Contains(p.Text, "KEEP THIS DIRECTION") {
			return true
		}
	}
	return false
}

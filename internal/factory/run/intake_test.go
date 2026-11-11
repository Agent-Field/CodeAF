package run

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAnswerAskFirstStartsOnlyWithYes(t *testing.T) {
	for _, yes := range []bool{false, true} {
		t.Run(map[bool]string{false: "no", true: "yes"}[yes], func(t *testing.T) {
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { return done("finished"), nil })}, nil)
			id := g.add("intake question", chat("plan"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindAskFirst, "asked first: auth/**. Start this job?"
				it.AskedFirstReason = "auth/**"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := g.r.Answer(id, yes, ""); err != nil {
				t.Fatal(err)
			}
			if yes {
				g.waitState(id, factory.StateLanded)
			}
			it, err := g.st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if it.AskedFirstReason != "auth/**" || it.QKind != "" {
				t.Fatalf("item: %+v", it)
			}
			if !yes && (it.State != factory.StateNew || it.Stream != nil) {
				t.Fatalf("no launched a run: %+v", it)
			}
		})
	}
}

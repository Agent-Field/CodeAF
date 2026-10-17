package serve_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// TestSoloSettledPlanApprovalNeverCoversTheNextAsk pins the old-consent law over
// HTTP in solo mode: a plan approval that settled answers the question it was
// given for, and when the worker asks the gate again the job reads "waiting"
// with Approve available, the second approve writes a SECOND receipt behind its
// own five-second window, and Undo works only inside that window.
func TestSoloSettledPlanApprovalNeverCoversTheNextAsk(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: factory.ApproveQuestion("plan"), QKind: factory.QKindApprove})

	f.post(t, it, "approve", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	if j := f.read(t, it); j.Approval == nil || j.Approval.State != "approved" {
		t.Fatalf("first approval did not settle: %+v", j.Approval)
	}

	// The worker asks the gate again, later than the consent: the stored shape
	// the report door leaves, with the settled approval still in the slot.
	f.clock.Add(int64(time.Second))
	asked := time.Unix(0, f.clock.Load()).UTC()
	q := factory.ApproveQuestion("plan")
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.State, cur.Question, cur.QKind = factory.StateNeedsYou, q, factory.QKindApprove
		cur.Asking = &factory.Asked{Stage: factory.ApproveName, Question: q, At: asked, With: factory.AskedYou}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	j := f.read(t, it)
	if j.Approval == nil || j.Approval.State != "waiting" || !j.Can.Approve {
		t.Fatalf("the old consent covers the new ask: approval=%+v can=%+v", j.Approval, j.Can)
	}

	f.post(t, it, "approve", "{}", 200)
	if j = f.read(t, it); j.Approval.State != "approving" || !j.Can.Undo {
		t.Fatalf("second approve has no undo window: %+v %+v", j.Approval, j.Can)
	}
	f.post(t, it, "undo", "", 200)
	if !f.read(t, it).Can.Approve {
		t.Fatal("Undo did not put the second ask back")
	}
	f.post(t, it, "approve", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	if j = f.read(t, it); j.Approval == nil || j.Approval.State != "approved" {
		t.Fatalf("second approval did not settle: %+v", j.Approval)
	}
	approved := 0
	events, _ := f.rec.Since(context.Background(), time.Time{})
	for _, e := range events {
		if e.Kind == record.Approved {
			approved++
		}
	}
	if approved != 2 {
		t.Fatalf("want one receipt per ask (2), got %d in %+v", approved, events)
	}
}

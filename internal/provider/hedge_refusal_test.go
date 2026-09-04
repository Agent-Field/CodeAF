package provider

import (
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// TestARescueTheAllowanceRefusedSaysSoOnTheRow is C4: the row records both
// that the wait was reported and that the second request the ceiling still
// owed was refused for money. Those are different facts, which is why the
// refusal has its own field.
func TestARescueTheAllowanceRefusedSaysSoOnTheRow(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "row/budget-refusal",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 60 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.patience(t, 50*time.Millisecond)
	SetHedgeBudget(lanes.NewBudget(0, 0))

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 12*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want the refused hedge never to reach the wire", got)
	}

	rows := ended(read())
	if len(rows) != 1 {
		t.Fatalf("ended rows = %d, want the primary call's one row", len(rows))
	}
	if got := rows[0].Action; got != "report" {
		t.Fatalf("action = %q, want the controller's report", got)
	}
	if got := rows[0].Refused; got != "budget" {
		t.Fatalf("refused = %q, want budget", got)
	}
}

// TestTheRowCarriesTheControllersOwnReason is C5: the controller's reason on
// a mid-answer stall reaches the acted arm's model-call row unchanged.
func TestTheRowCarriesTheControllersOwnReason(t *testing.T) {
	read := loggingTo(t)
	resume := make(chan struct{})
	rig := newLaneRig(t, "row/controller-reason",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000, Tokens: 60,
			StallAfter: 30, StallUntil: resume,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	t.Cleanup(func() { close(resume) })
	rig.believes("A", 2, 250)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(ended(read())) == 2 })

	want := report.Reason()
	if want == "" {
		t.Fatal("the hedge report carried no controller reason, so the row comparison proves nothing")
	}
	for _, row := range ended(read()) {
		if row.Action == "" {
			continue
		}
		if row.Reason != want {
			t.Fatalf("row reason = %q, want the report's controller reason %q", row.Reason, want)
		}
		return
	}
	t.Fatal("no finished row recorded the controller's action")
}

package prompt

import "testing"

func advanceToCheck(t *testing.T, l *Loop) {
	t.Helper()
	if err := l.PlanWritten(); err != nil {
		t.Fatal(err)
	}
	if err := l.FilesWritten(); err != nil {
		t.Fatal(err)
	}
}

func TestLoopPassesFirstTime(t *testing.T) {
	l := NewLoop()
	advanceToCheck(t, l)
	got, err := l.CheckResult(true, "")
	if err != nil || got != PhasePreview || !l.Done() || l.FixRounds() != 0 {
		t.Fatalf("phase=%s err=%v fixes=%d", got, err, l.FixRounds())
	}
}

func TestLoopFixesThenPasses(t *testing.T) {
	l := NewLoop()
	advanceToCheck(t, l)
	if got, _ := l.CheckResult(false, "r1"); got != PhaseFix || l.LastReport() != "r1" {
		t.Fatalf("phase=%s report=%q", got, l.LastReport())
	}
	if err := l.FixApplied(); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.CheckResult(true, ""); got != PhasePreview || l.FixRounds() != 1 {
		t.Fatalf("phase=%s fixes=%d", got, l.FixRounds())
	}
}

func TestLoopGivesUpAfterThreeFixRoundsKeepingTheLastReport(t *testing.T) {
	l := NewLoop()
	advanceToCheck(t, l)
	reports := []string{"r1", "r2", "r3", "r4"}
	var phase Phase
	for i, r := range reports {
		phase, _ = l.CheckResult(false, r)
		if i < MaxFixRounds {
			if phase != PhaseFix {
				t.Fatalf("round %d: phase %s", i+1, phase)
			}
			if err := l.FixApplied(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if phase != PhaseCheckFailed || !l.Done() || l.LastReport() != "r4" || l.FixRounds() != MaxFixRounds {
		t.Fatalf("phase=%s report=%q fixes=%d", phase, l.LastReport(), l.FixRounds())
	}
}

func TestLoopRefusesOutOfOrderSteps(t *testing.T) {
	l := NewLoop()
	if _, err := l.CheckResult(true, ""); err == nil {
		t.Error("check before write accepted")
	}
	if err := l.FilesWritten(); err == nil {
		t.Error("files before plan accepted")
	}
	advanceToCheck(t, l)
	if err := l.FixApplied(); err == nil {
		t.Error("fix applied with no fix pending")
	}
	l.CheckResult(true, "")
	if err := l.PlanWritten(); err == nil {
		t.Error("restart after preview accepted")
	}
}

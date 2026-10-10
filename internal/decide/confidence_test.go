package decide

import (
	"reflect"
	"testing"
)

func goTestAllows() Proposal {
	return Proposal{
		Kind:      "permission:shell",
		Subject:   "go test",
		Choice:    "allow",
		Said:      "allowed",
		Stakes:    StakesReversible,
		PlaceName: "Config parser",
	}
}

func allows(n int, choice string) []Answer {
	out := make([]Answer, n)
	for i := range out {
		out[i] = Answer{ID: idOf(i, choice), Kind: "permission:shell", Subject: "go test", Choice: choice}
	}
	return out
}

func idOf(i int, choice string) string {
	return choice + "-" + string(rune('a'+i))
}

func TestSixOfSixReadsNinetySeven(t *testing.T) {
	got := Score(goTestAllows(), Evidence{Answers: allows(6, "allow")})
	wantBecause := "You allowed go test here 6 times. Reversible."
	wantBasis := []string{"allow-a", "allow-b", "allow-c", "allow-d", "allow-e", "allow-f"}
	if got.Percent != 97 || got.Because != wantBecause || !reflect.DeepEqual(got.Basis, wantBasis) {
		t.Fatalf("got %+v", got)
	}
	if !got.Decides(0) || !got.Decides(ThresholdDefault) || !got.Decides(97) || got.Decides(98) {
		t.Fatalf("97 decides through 97, not past it: %+v", got)
	}
	if ThresholdDefault != 90 || ThresholdMin != 50 || ThresholdMax != 100 {
		t.Fatalf("threshold range %d–%d default %d", ThresholdMin, ThresholdMax, ThresholdDefault)
	}
}

func TestAMixedHistoryStaysUnderNinety(t *testing.T) {
	// Four allowances and two refusals of the same command.
	answers := append(allows(4, "allow"), allows(2, "deny")...)
	got := Score(goTestAllows(), Evidence{Answers: answers})
	if got.Percent >= ThresholdDefault || got.Decides(ThresholdDefault) {
		t.Fatalf("4 of 6 scored %d", got.Percent)
	}
	if got.Because != "You allowed go test here 4 times. Reversible." {
		t.Fatalf("because %q", got.Because)
	}
	// Five of six is still mixed, and still under the default.
	five := append(allows(5, "allow"), allows(1, "deny")...)
	if got := Score(goTestAllows(), Evidence{Answers: five}); got.Percent >= 90 {
		t.Fatalf("5 of 6 scored %d", got.Percent)
	}
}

func TestIrreversibleNeverDecides(t *testing.T) {
	p := goTestAllows()
	p.Stakes = StakesIrreversible
	got := Score(p, Evidence{Answers: allows(6, "allow")})
	if got.Percent >= ThresholdMin || got.Percent != irreversibleCeiling {
		t.Fatalf("irreversible percent %d, ceiling %d", got.Percent, irreversibleCeiling)
	}
	if got.Because != "You allowed go test here 6 times. Irreversible." {
		t.Fatalf("because %q", got.Because)
	}
	for _, threshold := range []int{0, ThresholdMin, ThresholdDefault, ThresholdMax} {
		if got.Decides(threshold) {
			t.Fatalf("decided at %d: %+v", threshold, got)
		}
	}
	// A knows line does not lift an irreversible answer over the ceiling.
	p.PlaceName = "Marketing"
	withLine := Score(p, Evidence{Knows: []Knows{{ID: "k1", Supports: true}}})
	if withLine.Decides(ThresholdMin) || withLine.Percent >= ThresholdMin {
		t.Fatalf("knows line decided an irreversible answer: %+v", withLine)
	}
	if withLine.Because != "matches what Marketing knows. Irreversible." {
		t.Fatalf("because %q", withLine.Because)
	}
}

func TestAMatchingKnowsLineReadsNinetyTwo(t *testing.T) {
	p := goTestAllows()
	p.PlaceName = "Marketing"
	got := Score(p, Evidence{Knows: []Knows{{ID: "knows-pricing", Supports: true}}})
	if got.Percent != 92 || got.Because != "matches what Marketing knows" || !reflect.DeepEqual(got.Basis, []string{"knows-pricing"}) {
		t.Fatalf("%+v", got)
	}
	if !got.Decides(ThresholdDefault) || got.Decides(93) {
		t.Fatalf("92 decides at 90 and not at 93")
	}
	// A second line does not stack, and a replaced or unmatched line does not count.
	again := Score(p, Evidence{Knows: []Knows{
		{ID: "knows-pricing", Supports: true},
		{ID: "knows-other", Supports: true},
		{ID: "struck", Supports: true, Replaced: true},
		{ID: "aside", Supports: false},
	}})
	if again.Percent != 92 || !reflect.DeepEqual(again.Basis, []string{"knows-pricing", "knows-other"}) {
		t.Fatalf("%+v", again)
	}
	if got := Score(p, Evidence{}); got.Percent != 0 || got.Because != "" || got.Basis != nil || got.Decides(ThresholdMin) {
		t.Fatalf("empty evidence scored %+v", got)
	}
}

func TestHistoryAndKnowsTakeTheHigherFigure(t *testing.T) {
	p := goTestAllows()
	p.PlaceName = "Marketing"
	ev := Evidence{
		Answers: allows(6, "allow"),
		Knows:   []Knows{{ID: "knows-test", Supports: true}},
	}
	got := Score(p, ev)
	if got.Percent != 97 || got.Because != "You allowed go test here 6 times. Reversible." {
		t.Fatalf("history should win: %+v", got)
	}
	if !reflect.DeepEqual(got.Basis, []string{"allow-a", "allow-b", "allow-c", "allow-d", "allow-e", "allow-f", "knows-test"}) {
		t.Fatalf("basis %v", got.Basis)
	}
	// A thin history loses to the line.
	thin := Score(p, Evidence{
		Answers: allows(2, "allow"),
		Knows:   []Knows{{ID: "knows-test", Supports: true}},
	})
	if thin.Percent != 92 || thin.Because != "matches what Marketing knows" || !reflect.DeepEqual(thin.Basis, []string{"knows-test"}) {
		t.Fatalf("knows should win a short history: %+v", thin)
	}
}

func TestAShortUnanimousHistoryStaysUnderNinety(t *testing.T) {
	got := Score(goTestAllows(), Evidence{Answers: allows(2, "allow")})
	if got.Percent >= 90 {
		t.Fatalf("2 of 2 scored %d, floor is %d", got.Percent, HistoryFloor)
	}
	if got.Because != "You allowed go test here 2 times. Reversible." {
		t.Fatalf("because %q", got.Because)
	}
	one := Score(goTestAllows(), Evidence{Answers: allows(1, "allow")})
	if one.Percent >= 90 || one.Because != "You allowed go test here 1 time. Reversible." {
		t.Fatalf("%+v", one)
	}
}

func TestAnotherSubjectDoesNotDiluteThisOne(t *testing.T) {
	answers := allows(6, "allow")
	for i := 0; i < 4; i++ {
		answers = append(answers, Answer{
			ID: "push", Kind: "permission:shell", Subject: "git push", Choice: "deny",
		})
	}
	answers = append(answers, Answer{
		ID: "other-kind", Kind: "permission:git", Subject: "go test", Choice: "deny",
	})
	got := Score(goTestAllows(), Evidence{Answers: answers})
	if got.Percent != 97 {
		t.Fatalf("other commands changed go test to %d", got.Percent)
	}
}

func TestAThresholdOutsideTheMenuDoesNotDecide(t *testing.T) {
	got := Score(goTestAllows(), Evidence{Answers: allows(6, "allow")})
	for _, threshold := range []int{-1, 1, 49, 101} {
		if got.Decides(threshold) {
			t.Fatalf("threshold %d decided %+v", threshold, got)
		}
	}
}

func TestCostlyCanDecideWhenTheHistoryIsSettled(t *testing.T) {
	p := goTestAllows()
	p.Stakes = StakesCostly
	got := Score(p, Evidence{Answers: allows(6, "allow")})
	if got.Percent != 97 || !got.Decides(ThresholdDefault) {
		t.Fatalf("%+v", got)
	}
	if got.Because != "You allowed go test here 6 times. Costly." {
		t.Fatalf("because %q", got.Because)
	}
}

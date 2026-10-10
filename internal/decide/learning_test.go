package decide

import (
	"errors"
	"testing"
	"time"
)

func proposal(ask, class, proposed, chosen string) ProposalAnswer {
	return ProposalAnswer{
		AskKind: ask, SubjectClass: class,
		ProposedKey: proposed, ChosenKey: chosen,
		At: t0,
	}
}

func record(t *testing.T, s *Store, ask, class, proposed, chosen string, n int) KindState {
	t.Helper()
	var st KindState
	for i := 0; i < n; i++ {
		var err error
		st, err = s.RecordProposal(proposal(ask, class, proposed, chosen))
		if err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestKindKeyJoinsAskKindAndSubjectClass(t *testing.T) {
	cases := []struct{ ask, class, want string }{
		{"permission", "shell-read", "permission:shell-read"},
		{"permission", "git", "permission:git"},
		{"choice", "", "choice"},
		{"judgement", "", "judgement"},
		{"memory-promotion", "", "memory-promotion"},
	}
	for _, c := range cases {
		if got := KindKey(c.ask, c.class); got != c.want {
			t.Errorf("KindKey(%q, %q) = %q, want %q", c.ask, c.class, got, c.want)
		}
	}
}

func TestNewPlaceStartsEveryKindInLearning(t *testing.T) {
	s := open(t, t.TempDir())
	for _, key := range []string{"permission:shell-read", "permission:git", "choice", "judgement"} {
		st, err := s.Mode(key)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode != ModeLearning || len(st.Recent) != 0 || LearningCount(st) != "" {
			t.Fatalf("%s starts as %+v count %q", key, st, LearningCount(st))
		}
	}
}

func TestSeventeenOfTwentyStaysLearning(t *testing.T) {
	s := open(t, t.TempDir())
	// A full ring, one agreement short of the bar.
	st := record(t, s, "choice", "", "yes", "no", RingSize-(GraduateAt-1))
	st = record(t, s, "choice", "", "yes", "yes", GraduateAt-1)
	if st.Mode != ModeLearning || len(st.Recent) != RingSize || Agreements(st) != GraduateAt-1 {
		t.Fatalf("17 of 20 should stay, got mode %s agreements %d len %d", st.Mode, Agreements(st), len(st.Recent))
	}
	if LearningCount(st) != "17 of 20" {
		t.Fatalf("count %q", LearningCount(st))
	}
	// Another agreement that pushes an agreement off the front stays at 17.
	// The oldest rows are the overruled ones only when they were recorded
	// first; record the agreements first on a fresh kind so the next
	// agreement drops an agreement.
	s2 := open(t, t.TempDir())
	record(t, s2, "judgement", "", "yes", "yes", GraduateAt-1)
	record(t, s2, "judgement", "", "yes", "no", RingSize-(GraduateAt-1))
	st = record(t, s2, "judgement", "", "yes", "yes", 1)
	if st.Mode != ModeLearning || Agreements(st) != GraduateAt-1 {
		t.Fatalf("replacing an agreement should stay at 17, got mode %s agreements %d", st.Mode, Agreements(st))
	}
}

func TestEighteenOfTwentyGraduates(t *testing.T) {
	s := open(t, t.TempDir())
	st := record(t, s, "permission", "shell-read", "allow", "deny", RingSize-GraduateAt)
	st = record(t, s, "permission", "shell-read", "allow", "allow", GraduateAt)
	if st.Mode != ModeDeciding || Agreements(st) != GraduateAt || len(st.Recent) != RingSize {
		t.Fatalf("18 of 20 should decide, got mode %s agreements %d len %d", st.Mode, Agreements(st), len(st.Recent))
	}
	// Nineteen of twenty also clears the bar. Order does not matter.
	st = record(t, s, "permission", "git", "allow", "allow", 10)
	st = record(t, s, "permission", "git", "allow", "deny", 1)
	st = record(t, s, "permission", "git", "allow", "allow", 9)
	if st.Mode != ModeDeciding || Agreements(st) != 19 {
		t.Fatalf("19 of 20 should decide, got mode %s agreements %d", st.Mode, Agreements(st))
	}
	// Eighteen agreements already on the ring, then the answers that fill it
	// are overruled. Crossing twenty still graduates: the bar is the count,
	// not which answer arrived last.
	st = record(t, s, "confirmation", "", "keep", "keep", GraduateAt)
	if st.Mode != ModeLearning {
		t.Fatal("a short ring must not graduate")
	}
	st = record(t, s, "confirmation", "", "keep", "change", RingSize-GraduateAt)
	if st.Mode != ModeDeciding || Agreements(st) != GraduateAt {
		t.Fatalf("filling the ring should graduate, got mode %s agreements %d", st.Mode, Agreements(st))
	}
	// All twenty. And a kind that has not been answered is untouched.
	st = record(t, s, "choice", "", "a", "a", RingSize)
	if st.Mode != ModeDeciding || Agreements(st) != RingSize {
		t.Fatalf("20 of 20 should decide, got %+v", st.Mode)
	}
	other, _ := s.Mode("judgement")
	if other.Mode != ModeLearning || len(other.Recent) != 0 {
		t.Fatalf("untouched kind %+v", other)
	}
	git, _ := s.Mode("permission:git")
	shell, _ := s.Mode("permission:shell-read")
	if git.Mode != ModeDeciding || shell.Mode != ModeDeciding {
		t.Fatalf("git %s shell %s", git.Mode, shell.Mode)
	}
}

func TestFewerThanTwentyDoesNotGraduate(t *testing.T) {
	s := open(t, t.TempDir())
	st := record(t, s, "choice", "", "yes", "yes", GraduateAt)
	if st.Mode != ModeLearning || len(st.Recent) != GraduateAt {
		t.Fatalf("18 answers are not a full ring, got mode %s len %d", st.Mode, len(st.Recent))
	}
	if LearningCount(st) != "18 of 20" {
		t.Fatalf("count %q, want the window not the number recorded", LearningCount(st))
	}
}

func TestStatusCountsFourteenOfTwenty(t *testing.T) {
	s := open(t, t.TempDir())
	st := record(t, s, "choice", "", "lead", "lead", 14)
	if st.Mode != ModeLearning || LearningCount(st) != "14 of 20" || Agreements(st) != 14 {
		t.Fatalf("mode %s count %q agreements %d", st.Mode, LearningCount(st), Agreements(st))
	}
	// Fourteen agreements inside a full ring still read "14 of 20", and stay.
	st = record(t, s, "judgement", "", "keep", "change", 6)
	st = record(t, s, "judgement", "", "keep", "keep", 14)
	if st.Mode != ModeLearning || LearningCount(st) != "14 of 20" || len(st.Recent) != RingSize {
		t.Fatalf("full ring: mode %s count %q len %d", st.Mode, LearningCount(st), len(st.Recent))
	}
	dir := t.TempDir()
	persisted := open(t, dir)
	record(t, persisted, "choice", "", "lead", "lead", 14)
	again := open(t, dir)
	st, err := again.Mode("choice")
	if err != nil {
		t.Fatal(err)
	}
	if LearningCount(st) != "14 of 20" || !st.Recent[0].Agreed {
		t.Fatalf("reopen count %q agreed %v", LearningCount(st), st.Recent[0].Agreed)
	}
}

func TestOverturnDropsOnlyThatKind(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	record(t, s, "permission", "shell-read", "allow", "deny", RingSize-GraduateAt)
	record(t, s, "permission", "shell-read", "allow", "allow", GraduateAt)
	record(t, s, "permission", "git", "allow", "deny", RingSize-GraduateAt)
	record(t, s, "permission", "git", "allow", "allow", GraduateAt)
	record(t, s, "choice", "", "a", "a", 14)

	when := t0.Add(time.Hour)
	if err := s.Append(Decision{ID: "shell", AskKind: "permission", Subject: "shell-read", At: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Decision{ID: "git", AskKind: "permission", Subject: "git", At: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Decision{ID: "pick", AskKind: "choice", At: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.OverturnDecided("shell", when); err != nil {
		t.Fatal(err)
	}
	// Overturning the choice, which is still learning, stamps it and leaves
	// the fourteen agreements where they are.
	if err := s.OverturnDecided("pick", when); err != nil {
		t.Fatal(err)
	}
	if err := s.OverturnDecided("missing", when); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	later := when.Add(time.Minute)
	if err := s.OverturnDecided("shell", later); err != nil {
		t.Fatal(err)
	}

	shell, _ := s.Mode("permission:shell-read")
	if shell.Mode != ModeLearning || len(shell.Recent) != 0 || LearningCount(shell) != "" {
		t.Fatalf("shell should be back in learning with a clear ring, got %+v", shell)
	}
	git, _ := s.Mode("permission:git")
	if git.Mode != ModeDeciding || len(git.Recent) != RingSize || Agreements(git) != GraduateAt {
		t.Fatalf("git should be untouched, got mode %s len %d agreements %d", git.Mode, len(git.Recent), Agreements(git))
	}
	choice, _ := s.Mode("choice")
	if choice.Mode != ModeLearning || LearningCount(choice) != "14 of 20" || len(choice.Recent) != 14 {
		t.Fatalf("choice ring should survive, got mode %s count %q len %d", choice.Mode, LearningCount(choice), len(choice.Recent))
	}
	got, _ := s.List()
	byID := map[string]Decision{}
	for _, d := range got {
		byID[d.ID] = d
	}
	if byID["shell"].OverturnedAt == nil || !byID["shell"].OverturnedAt.Equal(when) {
		t.Fatalf("shell overturn time %+v", byID["shell"].OverturnedAt)
	}
	if byID["git"].OverturnedAt != nil {
		t.Fatal("git decision was overturned")
	}
	if byID["pick"].OverturnedAt == nil {
		t.Fatal("choice decision not stamped")
	}

	s2 := open(t, dir)
	shell, _ = s2.Mode("permission:shell-read")
	git, _ = s2.Mode("permission:git")
	if shell.Mode != ModeLearning || len(shell.Recent) != 0 || git.Mode != ModeDeciding || Agreements(git) != GraduateAt {
		t.Fatalf("reopen shell %+v git mode %s agreements %d", shell.Mode, git.Mode, Agreements(git))
	}
}

func TestDisagreementAfterGraduatingDoesNotDropTheKind(t *testing.T) {
	s := open(t, t.TempDir())
	st := record(t, s, "choice", "", "a", "a", RingSize)
	if st.Mode != ModeDeciding {
		t.Fatalf("want deciding, got %s", st.Mode)
	}
	st = record(t, s, "choice", "", "a", "b", RingSize)
	if st.Mode != ModeDeciding || Agreements(st) != 0 || LearningCount(st) != "" {
		t.Fatalf("a bad streak stays deciding, got mode %s agreements %d count %q", st.Mode, Agreements(st), LearningCount(st))
	}
}

func TestAlwaysAskDoesNotGraduateOrReset(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.SetMode("judgement", ModeAsk); err != nil {
		t.Fatal(err)
	}
	st := record(t, s, "judgement", "", "keep", "keep", RingSize)
	if st.Mode != ModeAsk || Agreements(st) != RingSize {
		t.Fatalf("always ask should not graduate, got %s", st.Mode)
	}
	if err := s.Append(Decision{ID: "j", AskKind: "judgement", At: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.OverturnDecided("j", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Mode("judgement")
	if st.Mode != ModeAsk || len(st.Recent) != RingSize {
		t.Fatalf("overturn should leave always ask alone, got mode %s len %d", st.Mode, len(st.Recent))
	}
}

func TestProposalAnswerRequiresKeys(t *testing.T) {
	s := open(t, t.TempDir())
	for _, a := range []ProposalAnswer{
		{},
		{AskKind: "choice", ProposedKey: "a"},
		{AskKind: "choice", ChosenKey: "a"},
		{ProposedKey: "a", ChosenKey: "a"},
	} {
		if _, err := s.RecordProposal(a); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v err %v", a, err)
		}
	}
	if err := s.OverturnDecided("", t0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty id %v", err)
	}
}

func TestAgreedMeansTheProposedKey(t *testing.T) {
	s := open(t, t.TempDir())
	st, err := s.RecordProposal(ProposalAnswer{AskKind: "choice", ProposedKey: "lead", ChosenKey: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Recent) != 1 || !st.Recent[0].Agreed || !st.Recent[0].At.Equal(t0) {
		t.Fatalf("agreed %+v", st.Recent)
	}
	st, err = s.RecordProposal(proposal("choice", "", "lead", "other"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Recent[1].Agreed {
		t.Fatal("a different key is overruled")
	}
	st, err = s.RecordProposal(proposal("choice", "", "lead", "Lead"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Recent[2].Agreed {
		t.Fatal("the key must match exactly")
	}
	if LearningCount(st) != "1 of 20" || st.Mode != ModeLearning {
		t.Fatalf("mode %s count %q", st.Mode, LearningCount(st))
	}
}

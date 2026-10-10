package placegraph

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeDecider struct {
	verdict   MemoryVerdict
	err       error
	called    int
	neighbors []MemoryNeighbor
	candidate MemoryCandidate
}

func (f *fakeDecider) Decide(candidate MemoryCandidate, neighbors []MemoryNeighbor) (MemoryVerdict, error) {
	f.called++
	f.candidate = candidate
	f.neighbors = append([]MemoryNeighbor(nil), neighbors...)
	if f.err != nil {
		return MemoryVerdict{}, f.err
	}
	return f.verdict, nil
}

func TestPromoteMemoryAddWritesSaidInChat(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	said := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	fake := &fakeDecider{verdict: MemoryVerdict{
		Op: "add", Title: "pricing lead", Text: `Lead pricing with "free for one seat"`,
	}}
	promo, err := s.PromoteMemory(p.ID, MemoryCandidate{
		Title: "pricing lead", Text: "draft", ChatID: "chat_launch", At: said,
	}, fake)
	if err != nil || promo.Skipped || promo.Receipt.Noop() || fake.called != 1 {
		t.Fatalf("promo %+v err %v called %d", promo, err, fake.called)
	}
	lines := snap(t, s).Knowledge(p.ID)
	if len(lines) != 1 {
		t.Fatalf("lines %+v", lines)
	}
	got := lines[0]
	if got.Text != `Lead pricing with "free for one seat"` || got.Source.Kind != LineSaidInChat ||
		got.Source.ChatID != "chat_launch" || !got.Source.At.Equal(said) {
		t.Fatalf("line %+v", got)
	}
	if _, err := s.Undo(promo.Receipt.ID); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Knowledge(p.ID)) != 0 {
		t.Fatal("undo left the said-in-chat line")
	}
}

func TestPromoteMemoryRefineKeepsTheSource(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	line, _, err := s.AddLine(Line{PlaceID: p.ID, Text: "Ship on Fridays.", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeDecider{verdict: MemoryVerdict{Op: "update", TargetID: line.ID, Title: "ship day", Text: "Ship on Tuesdays."}}
	promo, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "ship day", Text: "Ship on Tuesdays."}, fake)
	if err != nil || promo.Skipped {
		t.Fatalf("promo %+v err %v", promo, err)
	}
	got := snap(t, s).Knowledge(p.ID)
	if len(got) != 1 || got[0].ID != line.ID || got[0].Text != "Ship on Tuesdays." || got[0].Source.Kind != LineYouWrote {
		t.Fatalf("refined %+v", got)
	}
}

func TestPromoteMemoryReplaceStrikesTheOlderLine(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	old, _, err := s.AddLine(Line{
		PlaceID: p.ID, Text: "Lead with usage pricing",
		Source:    LineSource{Kind: LineYouWrote},
		CreatedAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeDecider{verdict: MemoryVerdict{
		Op: "supersede", TargetID: old.ID, Title: "pricing lead", Text: `Lead pricing with "free for one seat"`,
	}}
	if _, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "pricing lead", Text: "x", ChatID: "chat_1"}, fake); err != nil {
		t.Fatal(err)
	}
	lines := snap(t, s).Knowledge(p.ID)
	if len(lines) != 2 {
		t.Fatalf("lines %+v", lines)
	}
	var struck, fresh Line
	for _, l := range lines {
		if l.ID == old.ID {
			struck = l
		} else {
			fresh = l
		}
	}
	if struck.ReplacedBy != fresh.ID || struck.ReplacedAt.IsZero() || fresh.Source.Kind != LineSaidInChat {
		t.Fatalf("struck %+v fresh %+v", struck, fresh)
	}
}

func TestPromoteMemorySameDayAsksOnce(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	old, _, err := s.AddLine(Line{PlaceID: p.ID, Text: "Ship on Fridays.", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeDecider{verdict: MemoryVerdict{Op: "replace", TargetID: old.ID, Title: "ship day", Text: "Ship on Tuesdays."}}
	promo, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "ship day", Text: "Ship on Tuesdays.", ChatID: "chat_1"}, fake)
	if err != nil || promo.Ask == nil || promo.Ask.A.ID != old.ID {
		t.Fatalf("promo %+v err %v", promo, err)
	}
	lines := snap(t, s).Knowledge(p.ID)
	if len(lines) != 2 || lines[0].ReplacedBy != "" || lines[1].Source.Kind != LineSaidInChat {
		t.Fatalf("lines %+v", lines)
	}
}

func TestPromoteMemorySkipUntitledAndMissingTargetWriteNothing(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	skip := &fakeDecider{verdict: MemoryVerdict{Op: "skip"}}
	promo, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "kept", Text: "already said"}, skip)
	if err != nil || !promo.Skipped || skip.called != 1 || len(snap(t, s).Lines) != 0 {
		t.Fatalf("skip promo %+v err %v", promo, err)
	}
	untitled := &fakeDecider{verdict: MemoryVerdict{Op: "add", Text: "a body with no title"}}
	promo, err = s.PromoteMemory(p.ID, MemoryCandidate{Text: "a body with no title"}, untitled)
	if err != nil || !promo.Skipped || len(snap(t, s).Lines) != 0 {
		t.Fatalf("untitled promo %+v err %v", promo, err)
	}
	gone := &fakeDecider{verdict: MemoryVerdict{Op: "refine", TargetID: "ln_missing", Title: "t", Text: "x"}}
	promo, err = s.PromoteMemory(p.ID, MemoryCandidate{Title: "t", Text: "x"}, gone)
	if err != nil || !promo.Skipped || len(snap(t, s).Lines) != 0 {
		t.Fatalf("missing target promo %+v err %v", promo, err)
	}
	quiet := &fakeDecider{}
	promo, err = s.PromoteMemory("", MemoryCandidate{Title: "t", Text: "x"}, quiet)
	if err != nil || !promo.Skipped || quiet.called != 0 {
		t.Fatalf("blank place promo %+v called %d", promo, quiet.called)
	}
	promo, err = s.PromoteMemory(p.ID, MemoryCandidate{}, quiet)
	if err != nil || !promo.Skipped || quiet.called != 0 {
		t.Fatalf("empty candidate called the decider (%d)", quiet.called)
	}
}

func TestPromoteMemoryDeciderErrorWritesNothing(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	fake := &fakeDecider{err: errors.New("the decider answered nonsense")}
	_, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "t", Text: "x"}, fake)
	if err == nil || !strings.Contains(err.Error(), "nonsense") || len(snap(t, s).Lines) != 0 {
		t.Fatalf("err %v lines %d", err, len(snap(t, s).Lines))
	}
}

func TestPromoteMemoryRefusesAMissingOrArchivedPlace(t *testing.T) {
	s, _ := newStore(t)
	fake := &fakeDecider{verdict: MemoryVerdict{Op: "add", Title: "t", Text: "x"}}
	if _, err := s.PromoteMemory("pl_missing", MemoryCandidate{Title: "t", Text: "x"}, fake); !errors.Is(err, ErrNotFound) || fake.called != 0 {
		t.Fatal("missing place", err, fake.called)
	}
	p := mk(t, s, "Marketing")
	if _, err := s.Archive(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "t", Text: "x"}, fake); !errors.Is(err, ErrArchived) || fake.called != 0 {
		t.Fatal("archived", err, fake.called)
	}
}

func TestPromoteMemoryShowsTheClosestLiveLineFirst(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	voice, _, err := s.AddLine(Line{PlaceID: p.ID, Text: "Brand voice stays short.", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	pricing, _, err := s.AddLine(Line{PlaceID: p.ID, Text: "Lead pricing with a free seat.", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := s.AddLine(Line{PlaceID: p.ID, Text: "Lead pricing with usage.", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	old.ReplacedBy = pricing.ID
	old.ReplacedAt = pricing.CreatedAt
	if _, err := s.UpdateLine(old); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDecider{verdict: MemoryVerdict{Op: "skip"}}
	if _, err := s.PromoteMemory(p.ID, MemoryCandidate{Title: "pricing", Text: "Lead pricing with a free seat"}, fake); err != nil {
		t.Fatal(err)
	}
	if len(fake.neighbors) != 2 || fake.neighbors[0].ID != pricing.ID {
		t.Fatalf("neighbors %+v voice %s", fake.neighbors, voice.ID)
	}
	for _, n := range fake.neighbors {
		if n.ID == old.ID {
			t.Fatal("struck line was shown to the decider")
		}
	}
}

func TestLearnAnswersNeedsThreeWithNoContrary(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	way := `Lead pricing with "free for one seat"`
	two := []KindAnswer{
		{Kind: "choice:pricing", Way: way},
		{Kind: "choice:pricing", Way: "lead  pricing with \"free for one seat\""},
	}
	promo, err := s.LearnAnswers(p.ID, two)
	if err != nil || !promo.Skipped || len(snap(t, s).Lines) != 0 {
		t.Fatalf("two answers wrote %+v err %v", promo, err)
	}
	broken := append(append([]KindAnswer{}, two...), KindAnswer{Kind: "choice:pricing", Way: "Usage-based, from $0"}, KindAnswer{Kind: "choice:pricing", Way: way}, KindAnswer{Kind: "choice:pricing", Way: way})
	promo, err = s.LearnAnswers(p.ID, broken)
	if err != nil || !promo.Skipped || len(snap(t, s).Lines) != 0 {
		t.Fatalf("contrary in between wrote %+v", snap(t, s).Lines)
	}
	said := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	three := []KindAnswer{
		{Kind: "choice:pricing", Way: "Lead  pricing with \"free for one seat\""},
		{Kind: "choice:pricing", Way: way},
		{Kind: "choice:pricing", Way: way + ".", ChatID: "chat_launch", At: said},
		{Kind: "permission:git", Way: "Allow", ChatID: "chat_launch"},
		{Kind: "permission:git", Way: "Allow"},
	}
	promo, err = s.LearnAnswers(p.ID, three)
	if err != nil || promo.Skipped {
		t.Fatalf("promo %+v err %v", promo, err)
	}
	lines := snap(t, s).Knowledge(p.ID)
	if len(lines) != 1 {
		t.Fatalf("lines %+v", lines)
	}
	got := lines[0]
	if got.Source.Kind != LineLearned || got.Source.Answers != LearnedAnswerCount || got.Source.ChatID != "chat_launch" || !got.Source.At.Equal(said) {
		t.Fatalf("source %+v", got.Source)
	}
	if got.Text != way+"." {
		t.Fatalf("text %q", got.Text)
	}
	if LearnedAnswerCount != 3 || LearnedFromAnswers(got.Source.Answers) != "Learned from 3 of your answers" {
		t.Fatalf("caption %q count %d", LearnedFromAnswers(got.Source.Answers), LearnedAnswerCount)
	}
}

func TestLearnAnswersFourthDoesNotDuplicate(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	answers := []KindAnswer{
		{Kind: "choice:pricing", Way: "Free for one seat"},
		{Kind: "choice:pricing", Way: "Free for one seat"},
		{Kind: "choice:pricing", Way: "Free for one seat"},
	}
	if _, err := s.LearnAnswers(p.ID, answers); err != nil {
		t.Fatal(err)
	}
	answers = append(answers, KindAnswer{Kind: "choice:pricing", Way: "free for one seat"})
	promo, err := s.LearnAnswers(p.ID, answers)
	if err != nil || !promo.Skipped {
		t.Fatal(promo, err)
	}
	if lines := snap(t, s).Knowledge(p.ID); len(lines) != 1 || lines[0].ReplacedBy != "" {
		t.Fatalf("lines %+v", lines)
	}
}

func TestLearnAnswersContraryWayStrikesTheEarlierLearnedLine(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	other := mk(t, s, "Software")
	var answers []KindAnswer
	for i := 0; i < 3; i++ {
		answers = append(answers, KindAnswer{Kind: "choice:pricing", Way: "Lead with usage pricing"})
	}
	for i := 0; i < 3; i++ {
		answers = append(answers, KindAnswer{Kind: "choice:pricing", Way: "Free for one seat"})
	}
	promo, err := s.LearnAnswers(p.ID, answers)
	if err != nil || promo.Ask != nil {
		t.Fatalf("promo %+v err %v", promo, err)
	}
	lines := snap(t, s).Knowledge(p.ID)
	if len(lines) != 2 {
		t.Fatalf("lines %+v", lines)
	}
	var old, fresh Line
	for _, l := range lines {
		if l.ReplacedBy != "" {
			old = l
		} else {
			fresh = l
		}
	}
	if old.Text != "Lead with usage pricing" || fresh.Text != "Free for one seat" || old.ReplacedBy != fresh.ID {
		t.Fatalf("old %+v fresh %+v", old, fresh)
	}
	if fresh.Source.Kind != LineLearned || LearnedFromAnswers(fresh.Source.Answers) != "Learned from 3 of your answers" {
		t.Fatalf("fresh source %+v", fresh.Source)
	}
	// The same history must not add a third line, and another place stays empty.
	again, err := s.LearnAnswers(p.ID, answers)
	if err != nil || !again.Skipped || len(snap(t, s).Knowledge(p.ID)) != 2 {
		t.Fatalf("again %+v lines %+v", again, snap(t, s).Knowledge(p.ID))
	}
	if len(snap(t, s).Knowledge(other.ID)) != 0 {
		t.Fatal("learned into the other place")
	}
	// A line the person wrote is not struck when the other way was learned.
	wrote, _, err := s.AddLine(Line{PlaceID: p.ID, Text: "Brand voice stays short.", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	var voice []KindAnswer
	for i := 0; i < 3; i++ {
		voice = append(voice, KindAnswer{Kind: "choice:voice", Way: "Long sentences are fine."})
	}
	if _, err := s.LearnAnswers(p.ID, voice); err != nil {
		t.Fatal(err)
	}
	for _, l := range snap(t, s).Knowledge(p.ID) {
		if l.ID == wrote.ID && l.ReplacedBy != "" {
			t.Fatal("a learned streak struck a line the person wrote")
		}
	}
}

func TestLearnAnswersUndoRemovesTheLine(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	var answers []KindAnswer
	for i := 0; i < 3; i++ {
		answers = append(answers, KindAnswer{Kind: "choice", Way: "Free for one seat"})
	}
	promo, err := s.LearnAnswers(p.ID, answers)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(promo.Receipt.ID); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Lines) != 0 {
		t.Fatal("undo left the learned line")
	}
}

func TestPromoteMemoryVerdictSuppliesTheTitle(t *testing.T) {
	s, _ := newStore(t)
	p := mk(t, s, "Marketing")
	fake := &fakeDecider{verdict: MemoryVerdict{Op: "add", Title: "pricing lead", Text: "Free for one seat."}}
	promo, err := s.PromoteMemory(p.ID, MemoryCandidate{Text: "Free for one seat."}, fake)
	if err != nil || promo.Skipped {
		t.Fatal(promo, err)
	}
	if lines := snap(t, s).Knowledge(p.ID); len(lines) != 1 || lines[0].Text != "Free for one seat." || lines[0].Source.Kind != LineSaidInChat {
		t.Fatalf("lines %+v", lines)
	}
}

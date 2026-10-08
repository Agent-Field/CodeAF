package mock

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// start is a fixed morning, so arrivals follow the daytime rate and no test
// reads the wall clock.
var start = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

func world(t *testing.T) factory.Seam {
	t.Helper()
	return New(7, 6, 60, 6, start)
}

func load(t *testing.T, s factory.Seam) factory.Snapshot {
	t.Helper()
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func find(t *testing.T, snap factory.Snapshot, id int) factory.Item {
	t.Helper()
	for _, it := range snap.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %d", id)
	return factory.Item{}
}

func newItem(t *testing.T, s factory.Seam, words string) int {
	t.Helper()
	id, err := s.New("agentfield/codeaf", words)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// onNames is the names of the stages that are on, in order.
func onNames(stages []factory.Stage) []string {
	var out []string
	for _, st := range stages {
		if st.On {
			out = append(out, st.Name)
		}
	}
	return out
}

// EVERY DOOR BUT TALK AND FOREMAN: an item's own conversation and the floor's
// are made by the launch over the session engine and the teams file
// (cmd/codeaf's factory_talk.go and factory_foreman.go), and the made-up floor
// has neither, so its `T` and `m` are absent rather than broken.
func TestEveryDoorIsFilled(t *testing.T) {
	v := reflect.ValueOf(world(t))
	for i := 0; i < v.NumField(); i++ {
		if name := v.Type().Field(i).Name; name == "Talk" || name == "Foreman" {
			continue
		}
		if v.Field(i).IsNil() {
			t.Errorf("door %s is nil", v.Type().Field(i).Name)
		}
	}
}

func TestLoadIsACopy(t *testing.T) {
	s := world(t)
	id := newItem(t, s, "fix the ledger")
	if err := s.Launch(id); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(20 * time.Minute); err != nil {
		t.Fatal(err)
	}
	want := load(t, s)
	got := load(t, s)
	mutations := []struct {
		name string
		do   func(*factory.Snapshot)
	}{
		{"title", func(s *factory.Snapshot) { s.Items[0].Title = "changed" }},
		{"stage", func(s *factory.Snapshot) { s.Items[0].Stages[0].Name = "changed" }},
		{"stream phase", func(s *factory.Snapshot) {
			for i := range s.Items {
				if s.Items[i].Stream != nil {
					s.Items[i].Stream.Phases[0].Name = "changed"
					s.Items[i].Stream.Log[0].Text = "changed"
					s.Items[i].Stream.Activity[0] = 99
					s.Items[i].Stream.Spent = 999
				}
			}
		}},
		{"recipe", func(s *factory.Snapshot) { s.Repos[0].Recipe.Stages[0].Ask = "changed" }},
		{"habit", func(s *factory.Snapshot) { s.Repos[0].Habits[0] = "changed" }},
		{"source repos", func(s *factory.Snapshot) { s.Sources[1].Repos[0] = "changed" }},
		{"items slice", func(s *factory.Snapshot) { s.Items = s.Items[:1] }},
	}
	for _, m := range mutations {
		m.do(&got)
	}
	if again := load(t, s); !reflect.DeepEqual(want, again) {
		t.Fatal("mutating a snapshot changed the world")
	}
}

func TestSnapshotSourcesAndSpeed(t *testing.T) {
	snap := load(t, world(t))
	if len(snap.Sources) != 2 || snap.Sources[0].Name != "chat" || snap.Sources[0].Writes {
		t.Fatalf("sources = %+v", snap.Sources)
	}
	gh := snap.Sources[1]
	if gh.Name != "github" || !gh.Writes || len(gh.Repos) != len(snap.Repos) || !gh.Polled.Equal(snap.Now) {
		t.Fatalf("github source = %+v", gh)
	}
	if snap.Speed <= 0 {
		t.Fatal("a mock clock has a speed")
	}
	chat := 0
	for _, it := range snap.Items {
		if it.Origin == factory.OriginChat {
			chat++
		}
	}
	if chat != 3 {
		t.Fatalf("chat items = %d, want 3", chat)
	}
}

func TestLaunchedIssueLandsInRecipeOrder(t *testing.T) {
	cases := []struct {
		name  string
		words string
	}{
		{"ship gate", "fix the ledger"},
		{"plan gate, two rounds, security", "fix the ledger, two rounds, security, plan first"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := world(t)
			id := newItem(t, s, c.words)
			if err := s.Launch(id); err != nil {
				t.Fatal(err)
			}
			var it factory.Item
			for i := 0; i < 600; i++ {
				if err := s.Tick(time.Minute); err != nil {
					t.Fatal(err)
				}
				it = find(t, load(t, s), id)
				if it.State == factory.StateNeedsYou {
					if err := s.Answer(id, true, ""); err != nil {
						t.Fatal(err)
					}
				}
				if it.State == factory.StateLanded || it.State == factory.StateShipped {
					break
				}
			}
			if it.State != factory.StateLanded && it.State != factory.StateShipped {
				t.Fatalf("state after 600 ticks = %s", it.State)
			}
			var phases []string
			for _, ph := range it.Stream.Phases {
				phases = append(phases, ph.Name)
				if ph.State != factory.PhaseDone {
					t.Errorf("phase %s is %s", ph.Name, ph.State)
				}
				if ph.Round < 1 || ph.Tasks < 1 {
					t.Errorf("phase %s round %d tasks %d", ph.Name, ph.Round, ph.Tasks)
				}
			}
			if want := onNames(it.Stages); !reflect.DeepEqual(phases, want) {
				t.Fatalf("phases %v, want the recipe's %v", phases, want)
			}
			if len(it.Proof) == 0 {
				t.Fatal("landed with no proof sheet")
			}
		})
	}
}

func TestCapParksAndAnswerDoublesIt(t *testing.T) {
	s := world(t)
	id := newItem(t, s, "fix the ledger")
	if err := s.SetCap(id, 0.50); err != nil {
		t.Fatal(err)
	}
	if err := s.Launch(id); err != nil {
		t.Fatal(err)
	}
	var it factory.Item
	for i := 0; i < 120; i++ {
		if err := s.Tick(time.Minute); err != nil {
			t.Fatal(err)
		}
		it = find(t, load(t, s), id)
		if it.State == factory.StateNeedsYou && it.QKind == "cap" {
			break
		}
		if it.State == factory.StateNeedsYou {
			if err := s.Answer(id, true, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if it.State != factory.StateNeedsYou || it.QKind != "cap" {
		t.Fatalf("state %s qkind %q, want parked on the cap", it.State, it.QKind)
	}
	if err := s.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it = find(t, load(t, s), id)
	if it.Cap != 1.0 || it.State != factory.StateRunning || it.QKind != "" {
		t.Fatalf("after yes: cap %v state %s qkind %q", it.Cap, it.State, it.QKind)
	}
}

func TestSleepWritesAShift(t *testing.T) {
	s := world(t)
	before := load(t, s).Now
	if err := s.Sleep(8 * time.Hour); err != nil {
		t.Fatal(err)
	}
	snap := load(t, s)
	if !snap.Shift.Since.Equal(before) {
		t.Fatalf("shift since %v, want %v", snap.Shift.Since, before)
	}
	if !snap.Now.Equal(before.Add(8 * time.Hour)) {
		t.Fatalf("now %v, want eight hours on", snap.Now)
	}
	if snap.Shift.Arrived+snap.Shift.Shipped+snap.Shift.Asked == 0 {
		t.Fatalf("an empty shift: %+v", snap.Shift)
	}
}

func TestTickCarriesPartMinutes(t *testing.T) {
	s := world(t)
	before := load(t, s).Now
	for i := 0; i < 4; i++ {
		if err := s.Tick(30 * time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if got := load(t, s).Now.Sub(before); got != 2*time.Minute {
		t.Fatalf("four half-minute ticks moved %v", got)
	}
}

func TestParseStage(t *testing.T) {
	cases := []struct {
		words, name, ask string
		before           string // the stage it must sit directly before
		afterAnchor      string // a stage it must come after
	}{
		{"before proof, screenshot the page", "screenshot page", "screenshot the page", "proof", "review"},
		{"make it neater", "make neater", "make it neater", "", "review"},
		{"after plan, write the migration down", "write migration", "write the migration down", "write", "plan"},
		{"after write, update the manual page", "update manual", "update the manual page", "test", "write"},
	}
	for _, c := range cases {
		t.Run(c.words, func(t *testing.T) {
			st := parseStage(c.words)
			if st.Name != c.name || st.Ask != c.ask || !st.On {
				t.Fatalf("parseStage = %+v", st)
			}
			stages := addStage(issueRecipe(), c.words)
			at := stageAt(stages, c.words)
			if c.before != "" && stages[at+1].Name != c.before {
				t.Fatalf("placed before %s, want before %s: %v", stages[at+1].Name, c.before, onNames(stages))
			}
			if a := stageIndex(stages, c.afterAnchor); a < 0 || at <= a {
				t.Fatalf("placed at %d, want after %s at %d", at, c.afterAnchor, a)
			}
			if at >= stageIndex(stages, "proof") {
				t.Fatalf("placed after proof: %d", at)
			}
		})
	}
}

func TestNewLiftsChips(t *testing.T) {
	cases := []struct {
		words    string
		title    string
		cap      float64
		gate     factory.Gate
		rounds   int
		security bool
	}{
		{"fix the ledger, two rounds, security, $8, plan first", "fix the ledger", 8, factory.GatePlan, 2, true},
		{"add a sitemap, $3", "add a sitemap", 3, factory.GateShip, 1, false},
		{"rename the probe, self-ship, 3 rounds", "rename the probe", 8, factory.GateNone, 3, false},
	}
	for _, c := range cases {
		t.Run(c.words, func(t *testing.T) {
			s := world(t)
			id := newItem(t, s, c.words)
			it := find(t, load(t, s), id)
			if it.Origin != factory.OriginTerminal || it.Synced {
				t.Fatalf("origin %s synced %v", it.Origin, it.Synced)
			}
			if it.Title != c.title || it.Cap != c.cap || it.Gate != c.gate {
				t.Fatalf("title %q cap %v gate %s", it.Title, it.Cap, it.Gate)
			}
			r := it.Stages[stageIndex(it.Stages, "review")]
			if r.Max != c.rounds || r.Until != "clean" {
				t.Fatalf("review stage %+v, want max %d until clean", r, c.rounds)
			}
			if sec := it.Stages[stageIndex(it.Stages, "security")]; sec.On != c.security {
				t.Fatalf("security on = %v", sec.On)
			}
			if err := s.Sync(id, true); err != nil {
				t.Fatal(err)
			}
			if !find(t, load(t, s), id).Synced {
				t.Fatal("sync did not take")
			}
		})
	}
}

func TestSteerRedoStronger(t *testing.T) {
	s := world(t)
	id := newItem(t, s, "fix the ledger")
	if err := s.Launch(id); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Steer(id, "redo stronger"); err != nil {
		t.Fatal(err)
	}
	it := find(t, load(t, s), id)
	cur := it.Stream.Phases[it.Stream.Cur].Name
	st := it.Stages[stageIndex(it.Stages, cur)]
	if st.Effort != "strong" {
		t.Fatalf("stage %s effort %q, want strong", cur, st.Effort)
	}
	logged := false
	for _, l := range it.Stream.Log {
		logged = logged || strings.Contains(l.Text, "strong")
	}
	if !logged {
		t.Fatal("the steer was not logged")
	}
}

// THE MOCK RANKS ITS ITEMS: the made-up floor spreads 1..4 with a reason on
// most, and leaves a few unranked, so the priority column shows bars.
func TestMockRanksItems(t *testing.T) {
	snap := load(t, world(t))
	seen := map[int]int{}
	for _, it := range snap.Items {
		p := it.Triage.Priority
		seen[p]++
		if p == 0 && it.Triage.Reason != "" {
			t.Fatalf("%s is unranked and has a reason", it.Ref())
		}
		if len(strings.Fields(it.Triage.Reason)) > 5 {
			t.Fatalf("%s: reason is over five words: %q", it.Ref(), it.Triage.Reason)
		}
	}
	for p := 0; p <= 4; p++ {
		if seen[p] == 0 {
			t.Fatalf("no item at priority %d: %v", p, seen)
		}
	}
	reasoned := 0
	for _, it := range snap.Items {
		if it.Triage.Reason != "" {
			reasoned++
		}
	}
	if reasoned == 0 {
		t.Fatal("no item carries a reason")
	}
}

// A RUNNING ITEM'S CHAT STAGE HAS A ROOM: a transcript the talk lane's writer
// made, under the mock's temp home.
func TestMockKeepsAStageConversation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	w := NewWorld(7, 6, 60, 6, start)
	seam := w.Seam()
	first, _ := w.Load()
	launched := 0
	for _, it := range first.Items {
		if it.State == factory.StateNew && it.Kind == factory.KindIssue && launched < 3 {
			if err := seam.Launch(it.ID); err != nil {
				t.Fatal(err)
			}
			launched++
		}
	}
	if err := seam.Tick(20 * time.Minute); err != nil {
		t.Fatal(err)
	}
	snap, err := w.Load()
	if err != nil {
		t.Fatal(err)
	}
	rooms := 0
	for _, it := range snap.Items {
		if it.State != factory.StateRunning || it.Stream == nil {
			continue
		}
		for _, ph := range it.Stream.Phases {
			if ph.Chat == "" {
				continue
			}
			rooms++
			if !strings.HasPrefix(ph.Chat, w.Home()) {
				t.Fatalf("room %q is outside the mock's home %q", ph.Chat, w.Home())
			}
			raw, err := os.ReadFile(ph.Chat)
			if err != nil || !strings.Contains(string(raw), it.Ref()) {
				t.Fatalf("room %q: %v %q", ph.Chat, err, raw)
			}
		}
	}
	if rooms == 0 {
		t.Fatal("no running item has a stage conversation")
	}
}

// THE MADE-UP WORDS READ AS REAL ONES: stages carry recipe-sounding names, no
// title doubles an article, and the opened floor has no sleep door.
func TestMockWordsReadAsReal(t *testing.T) {
	snap := load(t, world(t))
	for _, r := range snap.Repos {
		for _, st := range r.Recipe.Stages {
			if st.Name == "make code" || st.Name == "screenshot when" {
				t.Fatalf("%s: stage %q reads as a placeholder", r.Name, st.Name)
			}
		}
	}
	doubled := regexp.MustCompile(`(?i)\b(a|an|the) (a|an|the)\b`)
	for _, it := range snap.Items {
		if it.Kind == factory.KindCI {
			continue
		}
		if m := doubled.FindString(it.Title); m != "" {
			t.Fatalf("title %q doubles a word: %q", it.Title, m)
		}
	}
	s := NewAfter(7, 6, 60, 6, start, 2*time.Hour)
	if s.Sleep != nil || s.Has("sleep") {
		t.Fatal("the opened mock floor offers sleep, which the real floor does not")
	}
}

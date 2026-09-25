package teams

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

// packetTeams is harbor (managed by @boss) > dock (managed by @lead), both
// with members, saved in a fresh profile.
func packetTeams(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	teams := []Team{
		{ID: "aaaaaaaaaaaa", Name: "harbor", Manager: "hm",
			Members: []Member{{Key: "hm", Handle: "boss"}, {Key: "dm", Handle: "lead"}}},
		{ID: "bbbbbbbbbbbb", Name: "dock", Parent: "aaaaaaaaaaaa", Manager: "dm",
			Members: []Member{{Key: "dm", Handle: "lead"}, {Key: "w1", Handle: "web"}, {Key: "w2", Handle: "api"}}},
	}
	must(t, Save(dir, teams))
	return dir
}

func conflict() Packet {
	return Packet{
		Team: "bbbbbbbbbbbb", Kind: PacketConflict, RaisedBy: "web",
		Parties: []Party{
			{Key: "w1", Handle: "web", Team: "bbbbbbbbbbbb", Context: "the form posts JSON"},
			{Key: "w2", Handle: "api", Team: "bbbbbbbbbbbb", Context: "the endpoint takes form data"},
		},
		Question: "which shape does the signup form send?",
		Options: []Option{
			{Label: "JSON", Consequence: "@api changes the handler; the form stays"},
			{Label: "form data", Consequence: "@web rewrites the submit; the handler stays"},
		},
		Recommendation: &Recommendation{Option: "1", Reason: "the other endpoints all take JSON"},
	}
}

// A PACKET IS RAISED, DECIDED ONCE, AND READ BACK FOLDED. The raise mints the
// id and numbers the options; the open list for dock holds it; a decision by
// dock's manager closes it; a second decision is refused and writes nothing;
// and the file is events, not a rewritten record.
func TestAPacketIsRaisedDecidedOnceAndFolded(t *testing.T) {
	dir := packetTeams(t)
	p, err := Raise(dir, conflict())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.ID, "p") || p.State != PacketOpen || p.Origin != "bbbbbbbbbbbb" || p.Options[1].ID != "2" {
		t.Fatalf("raised %+v", p)
	}
	open, stamp, err := OpenPackets(dir, "bbbbbbbbbbbb")
	if err != nil || len(open) != 1 || open[0].ID != p.ID || stamp == MissingStamp {
		t.Fatalf("open for dock: %+v %q %v", open, stamp, err)
	}
	if mine, _, _ := OpenPackets(dir, Person); len(mine) != 0 {
		t.Fatal("a packet for dock's manager waits on the person")
	}
	if _, err := Decide(dir, p.ID, "web", "1", "mine"); !errors.Is(err, ErrNotDecider) {
		t.Fatalf("a member decided: %v", err)
	}
	got, err := Decide(dir, p.ID, FromManager, "1", "matches the rest")
	if err != nil || got.State != PacketDecided || got.DecidedBy != "lead" || got.Decision != "1" {
		t.Fatalf("decided %+v, %v", got, err)
	}
	if _, err := Decide(dir, p.ID, Person, "2", "late"); !errors.Is(err, ErrDecided) {
		t.Fatalf("a second decision: %v", err)
	}
	back, err := PacketByID(dir, p.ID)
	if err != nil || back.Decision != "1" || back.Reason != "matches the rest" {
		t.Fatalf("read back %+v, %v", back, err)
	}
	if open, next, _ := OpenPackets(dir, "bbbbbbbbbbbb"); len(open) != 0 || next == stamp {
		t.Fatalf("a decided packet is still open (%d) or the stamp did not move", len(open))
	}
	raw, _ := os.ReadFile(DecisionsPath(dir, "bbbbbbbbbbbb"))
	if lines := strings.Count(string(raw), "\n"); lines != 2 {
		t.Fatalf("the file has %d lines, want a raise and a decide:\n%s", lines, raw)
	}
	// And every change is a line of Traffic, and a conflict's ruling is a
	// directive to each of its parties (both in dock here).
	entries, _ := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	if len(entries) != 4 || entries[0].Kind != KindPacket || entries[1].State != PacketDecided || entries[1].Packet != p.ID ||
		!IsRuling(entries[2]) || entries[2].To != "web" || !IsRuling(entries[3]) || entries[3].To != "api" {
		t.Fatalf("traffic %+v", entries)
	}
}

// ESCALATION GOES UP OR TO THE PERSON, NEVER SIDEWAYS, AND KEEPS ITS TRAIL.
func TestAPacketEscalatesUpAndToThePersonOnly(t *testing.T) {
	dir := packetTeams(t)
	p, err := Raise(dir, conflict())
	must(t, err)
	if _, err := Escalate(dir, p.ID, "lead", "bbbbbbbbbbbb", "self"); !errors.Is(err, ErrSideways) {
		t.Fatalf("escalated to itself: %v", err)
	}
	up, err := Escalate(dir, p.ID, "lead", "aaaaaaaaaaaa", "it touches billing too")
	if err != nil || up.State != PacketEscalated || up.Team != "aaaaaaaaaaaa" || len(up.Trail) != 1 ||
		up.Trail[0].From != "bbbbbbbbbbbb" || up.Trail[0].By != "lead" {
		t.Fatalf("escalated %+v, %v", up, err)
	}
	if open, _, _ := OpenPackets(dir, "aaaaaaaaaaaa"); len(open) != 1 {
		t.Fatal("harbor's manager does not see the escalated packet")
	}
	if _, err := Decide(dir, p.ID, "lead", "1", ""); !errors.Is(err, ErrNotDecider) {
		t.Fatalf("the manager it left decided it: %v", err)
	}
	mine, err := Escalate(dir, p.ID, "boss", Person, "a judgement call")
	if err != nil || mine.Team != Person || len(mine.Trail) != 2 {
		t.Fatalf("to the person: %+v %v", mine, err)
	}
	if _, err := Escalate(dir, p.ID, Person, "aaaaaaaaaaaa", "back down"); !errors.Is(err, ErrSideways) {
		t.Fatalf("a packet at the person went back down: %v", err)
	}
	if all, _, _ := OpenPackets(dir, ScopeAll); len(all) != 1 {
		t.Fatal("the all scope lost it")
	}
	done, err := Decide(dir, p.ID, Person, "keep both, add a shim", "neither side should move today")
	if err != nil || done.DecidedBy != Person || done.Decision != "keep both, add a shim" {
		t.Fatalf("the person's own words: %+v %v", done, err)
	}
	// The escalated packet is logged in every team it passed through.
	if e, _ := ReadTraffic(dir, "aaaaaaaaaaaa", "", 0); len(e) < 2 {
		t.Fatalf("harbor's traffic has %d packet lines", len(e))
	}
}

// A PACKET IS COMPLETE OR REFUSED: every option says what happens, a
// recommendation names an option and a reason, it is raised from the deciding
// team or below it, and a question may have no options.
func TestARaiseRefusesAnIncompletePacket(t *testing.T) {
	dir := packetTeams(t)
	for name, bad := range map[string]func(*Packet){
		"no kind":                func(p *Packet) { p.Kind = "gossip" },
		"no question":            func(p *Packet) { p.Question = " " },
		"no raiser":              func(p *Packet) { p.RaisedBy = "" },
		"no options":             func(p *Packet) { p.Options = nil },
		"no consequence":         func(p *Packet) { p.Options[0].Consequence = "" },
		"unknown recommendation": func(p *Packet) { p.Recommendation.Option = "9" },
		"no reason":              func(p *Packet) { p.Recommendation.Reason = "" },
		"raised from above":      func(p *Packet) { p.Origin = "aaaaaaaaaaaa" },
		"decider unknown":        func(p *Packet) { p.Team = "eeeeeeeeeeee"; p.Origin = "bbbbbbbbbbbb" },
	} {
		p := conflict()
		p.Options = append([]Option(nil), p.Options...)
		r := *p.Recommendation
		p.Recommendation = &r
		bad(&p)
		if _, err := Raise(dir, p); err == nil {
			t.Errorf("%s: raised", name)
		}
	}
	q := Packet{Team: Person, Origin: "aaaaaaaaaaaa", Kind: PacketQuestion, RaisedBy: "boss",
		Question: "ship on Friday or Monday?"}
	if _, err := Raise(dir, q); err != nil {
		t.Fatalf("a bare question to the person: %v", err)
	}
	if mine, _, _ := OpenPackets(dir, Person); len(mine) != 1 {
		t.Fatal("the person's inbox is empty")
	}
}

// A CLOSING PACKET CARRIES ITS REPORT and the close options by their ids.
func TestAClosingPacketCarriesItsReport(t *testing.T) {
	dir := packetTeams(t)
	p, err := Raise(dir, Packet{Team: Person, Origin: "bbbbbbbbbbbb", Kind: PacketClosing, RaisedBy: "lead",
		Question: "close dock?",
		Report:   &ClosingReport{Done: "signup ships", Left: "the shim", Files: []string{"web/signup.ts"}, SpendUSD: 3.2},
		Options: []Option{
			{ID: OptionClose, Label: "Close", Consequence: "dock moves to Closed; members stop"},
			{ID: OptionKeepGoing, Label: "Keep going", Consequence: "nothing changes"},
		},
		Recommendation: &Recommendation{Option: OptionClose, Reason: "the work is merged"}})
	must(t, err)
	got, _ := Packets(dir, "bbbbbbbbbbbb")
	if len(got) != 1 || got[0].Report == nil || got[0].Report.Files[0] != "web/signup.ts" || got[0].ID != p.ID {
		t.Fatalf("the report did not survive: %+v", got)
	}
}

// A QUIET PACKET FILE COSTS A STAT, AND A GROWN ONE IS READ FROM WHERE THE
// LAST READ STOPPED.
func TestPacketReadsAreIncremental(t *testing.T) {
	dir := packetTeams(t)
	_, err := Raise(dir, conflict())
	must(t, err)
	_, _, _ = OpenPackets(dir, ScopeAll)
	before := packetCache.bytes()
	_, _, _ = OpenPackets(dir, ScopeAll)
	if packetCache.bytes() != before {
		t.Fatal("a quiet file was read again")
	}
	size := fileSize(t, DecisionsPath(dir, "bbbbbbbbbbbb"))
	_, err = Raise(dir, conflict())
	must(t, err)
	_, _, _ = OpenPackets(dir, ScopeAll)
	grew := fileSize(t, DecisionsPath(dir, "bbbbbbbbbbbb")) - size
	if got := packetCache.bytes() - before; got != grew {
		t.Fatalf("a grown file read %d bytes, want the %d appended", got, grew)
	}
}

// capAsk is one crossing: dock's pool, one local day, one ceiling.
func capAsk(day string, ceiling float64) Packet {
	return Packet{
		Team: Person, Origin: "bbbbbbbbbbbb", Kind: PacketCap, RaisedBy: FromSystem,
		Question: "dock reached its cap today",
		Options: []Option{
			{ID: OptionRaiseCap, Label: "Raise", Consequence: "dock goes on today"},
			{ID: OptionStopToday, Label: "Stop for today", Consequence: "nothing new starts until tomorrow"},
		},
		Cap: &CapFacts{Team: "bbbbbbbbbbbb", Day: day, CapUSD: ceiling, SpentUSD: ceiling + 0.2, RaiseTo: ceiling * 2},
	}
}

// TWO RAISERS OF ONE CROSSING WRITE ONE PACKET. Each goroutine is its own
// raiser on the same directory, the way two processes are: they share no
// packet in memory, only the file, and the second must find the first's line
// under the file lock and write nothing. A later day is a different crossing
// and writes a second packet, and so is the same day at a higher ceiling.
func TestACapCrossingIsRaisedOnceAcrossRaisers(t *testing.T) {
	dir := packetTeams(t)
	var wg sync.WaitGroup
	got := make([]Packet, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = Raise(dir, capAsk("2026-09-24", 5))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("raiser %d: %v", i, err)
		}
	}
	if got[0].ID == "" || got[0].ID != got[1].ID {
		t.Fatalf("two raisers wrote two packets: %s and %s", got[0].ID, got[1].ID)
	}
	list, err := Packets(dir, "bbbbbbbbbbbb")
	if err != nil || len(list) != 1 || list[0].Kind != PacketCap || list[0].ID != got[0].ID {
		t.Fatalf("the file holds %+v (%v)", list, err)
	}
	raw, _ := os.ReadFile(DecisionsPath(dir, "bbbbbbbbbbbb"))
	if lines := strings.Count(string(raw), "\n"); lines != 1 {
		t.Fatalf("the file has %d lines, want one raise:\n%s", lines, raw)
	}
	next, err := Raise(dir, capAsk("2026-09-25", 5))
	if err != nil || next.ID == got[0].ID {
		t.Fatalf("a later day: %+v %v", next, err)
	}
	higher, err := Raise(dir, capAsk("2026-09-24", 10))
	if err != nil || higher.ID == got[0].ID || higher.ID == next.ID {
		t.Fatalf("a higher ceiling: %+v %v", higher, err)
	}
	list, err = Packets(dir, "bbbbbbbbbbbb")
	if err != nil || len(list) != 3 {
		t.Fatalf("three crossings, got %+v (%v)", list, err)
	}
}

func (m *packetMemory) bytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	must(t, err)
	return info.Size()
}

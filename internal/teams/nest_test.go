package teams

// NESTING, AS THE STORE KEEPS IT: a conflict's ruling reaches every party as a
// directive in its own team's log whoever decides it, a party never decides its
// own case, and the global manager's members are the top-level managers.

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// siblings is P (manager pm) with two sub-teams, A (manager am; member web) and
// B (no manager; member api), saved under dir.
func siblings(t *testing.T, dir string) {
	t.Helper()
	must(t, Save(dir, []Team{
		{ID: "pppppppppppp", Name: "harbor", Manager: "pm", Members: []Member{{Key: "pm", Handle: "boss"}, {Key: "am", Handle: "front"}}},
		{ID: "aaaaaaaaaaaa", Name: "front", Parent: "pppppppppppp", Manager: "am", Members: []Member{{Key: "am", Handle: "lead"}, {Key: "web", Handle: "web"}}},
		{ID: "bbbbbbbbbbbb", Name: "back", Parent: "pppppppppppp", Members: []Member{{Key: "api", Handle: "api"}}},
	}))
}

func conflictBetween(t *testing.T, dir string) Packet {
	t.Helper()
	f, err := Load(dir)
	must(t, err)
	lca, ok := f.LCA("web", "api")
	if !ok || lca.ID != "pppppppppppp" {
		t.Fatalf("web and api meet at %+v %v, want harbor", lca, ok)
	}
	p, err := Raise(dir, Packet{Team: lca.ID, Origin: "aaaaaaaaaaaa", Kind: PacketConflict, RaisedBy: "web",
		Question: "which shape does the signup form send?",
		Parties: []Party{
			{Key: "web", Handle: "web", Team: "aaaaaaaaaaaa", Context: "the form posts JSON"},
			{Key: "api", Handle: "api", Team: "bbbbbbbbbbbb"},
		},
		Options: []Option{{Label: "JSON", Consequence: "@api changes the handler"}, {Label: "form data", Consequence: "@web rewrites the submit"}}})
	must(t, err)
	return p
}

// A CONFLICT'S LINES ARE IN EVERY INVOLVED TEAM, and its ruling is a directive
// to each party in its own team's log, carrying the packet.
func TestAConflictsRulingIsADirectiveToEveryPartyInItsOwnTeam(t *testing.T) {
	dir := t.TempDir()
	siblings(t, dir)
	p := conflictBetween(t, dir)
	for _, team := range []string{"pppppppppppp", "aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		log, _ := ReadTraffic(dir, team, "", 0)
		if len(log) != 1 || log[0].Kind != KindPacket || log[0].Packet != p.ID {
			t.Fatalf("the raise is not in %s's traffic: %+v", team, log)
		}
	}
	decided, err := Decide(dir, p.ID, "boss", "1", "the other endpoints take JSON")
	must(t, err)
	if decided.DecidedBy != "boss" {
		t.Fatalf("decided by %q", decided.DecidedBy)
	}
	for team, handle := range map[string]string{"aaaaaaaaaaaa": "web", "bbbbbbbbbbbb": "api"} {
		log, _ := ReadTraffic(dir, team, "", 0)
		var ruling Entry
		for _, e := range log {
			if IsRuling(e) {
				ruling = e
			}
		}
		if ruling.To != handle || ruling.Member != handle || ruling.From != FromManager || ruling.Packet != p.ID {
			t.Fatalf("%s's party was not directed: %+v", team, log)
		}
		for _, want := range []string{"ruling on the conflict " + p.ID, `◆ @boss (manager of "harbor")`, "JSON: @api changes the handler", "the other endpoints take JSON", "which shape"} {
			if !strings.Contains(ruling.Text, want) {
				t.Errorf("the ruling lacks %q: %s", want, ruling.Text)
			}
		}
	}
	// The decider's own team has the decision line and no directive.
	log, _ := ReadTraffic(dir, "pppppppppppp", "", 0)
	for _, e := range log {
		if IsRuling(e) {
			t.Fatalf("a ruling went to the decider's team: %+v", e)
		}
	}
}

// THE PERSON'S RULING IS THE PERSON'S, and says so.
func TestThePersonsRulingIsFromThePerson(t *testing.T) {
	dir := t.TempDir()
	siblings(t, dir)
	p := conflictBetween(t, dir)
	_, err := Decide(dir, p.ID, Person, "keep both shapes for now", "")
	must(t, err)
	log, _ := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0)
	e := log[len(log)-1]
	if !IsRuling(e) || e.From != FromYou || !strings.Contains(e.Text, "by the person: keep both shapes for now") {
		t.Fatalf("the person's ruling: %+v", e)
	}
}

// A PARTY NEVER DECIDES ITS OWN CASE, even when the packet waits on the team
// it manages.
func TestAManagerWhoIsAPartyIsNotTheDecider(t *testing.T) {
	dir := t.TempDir()
	siblings(t, dir)
	p, err := Raise(dir, Packet{Team: "pppppppppppp", Origin: "aaaaaaaaaaaa", Kind: PacketConflict, RaisedBy: "web", Question: "who owns the form?",
		Parties: []Party{{Key: "web", Handle: "web", Team: "aaaaaaaaaaaa"}, {Key: "pm", Handle: "boss", Team: "pppppppppppp"}},
		Options: []Option{{Label: "web", Consequence: "web owns it"}}})
	must(t, err)
	if _, err := Decide(dir, p.ID, "boss", "1", ""); !errors.Is(err, ErrNotDecider) {
		t.Fatalf("a party decided its own case: %v", err)
	}
	if _, err := Decide(dir, p.ID, Person, "1", ""); err != nil {
		t.Fatalf("the person could not decide it: %v", err)
	}
}

// THE GLOBAL MANAGER'S MEMBERS ARE THE TOP-LEVEL MANAGERS: seated in the root
// with their handles while the root has a manager, and TopManagers is exactly
// the ones who manage a top-level team now.
func TestTheRootSeatsTheTopLevelManagers(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{
		{ID: "aaaaaaaaaaaa", Name: "harbor", Manager: "hm", Members: []Member{{Key: "hm", Handle: "harbor"}}},
		{ID: "bbbbbbbbbbbb", Name: "yard", Manager: "ym", Members: []Member{{Key: "ym", Handle: "yard"}}},
		{ID: "cccccccccccc", Name: "dock", Parent: "aaaaaaaaaaaa", Manager: "dm", Members: []Member{{Key: "dm", Handle: "dock"}}},
		{ID: "dddddddddddd", Name: "quiet"},
	}))
	root := ""
	must(t, Update(dir, func(f *File) error {
		root = f.MakeRoot(time.Time{})
		if err := f.AddMember(root, Member{Key: "gm", Handle: "all"}); err != nil {
			return err
		}
		return f.SetManager(root, "gm")
	}))
	f, err := Load(dir)
	must(t, err)
	r, _ := f.Root()
	if !r.Holds("hm") || !r.Holds("ym") || r.Holds("dm") {
		t.Fatalf("the root holds %+v; want the two top-level managers and not dock's", r.Members)
	}
	if m, _ := r.Member("ym"); m.Handle != "yard" {
		t.Fatalf("yard's manager is %q in the root", m.Handle)
	}
	top := f.TopManagers()
	if len(top) != 2 || top[0].Key != "hm" || top[1].Key != "ym" {
		t.Fatalf("TopManagers: %+v", top)
	}
	if h, _ := f.Home("ym"); h.Team != root {
		t.Fatalf("yard's manager reports to %+v, want the root", h)
	}
	// A top-level manager replaced is no longer one of the global manager's.
	must(t, Update(dir, func(f *File) error {
		if err := f.AddMember("bbbbbbbbbbbb", Member{Key: "y2", Handle: "yard2"}); err != nil {
			return err
		}
		return f.SetManager("bbbbbbbbbbbb", "y2")
	}))
	f, _ = Load(dir)
	top = f.TopManagers()
	if len(top) != 2 || top[1].Key != "y2" {
		t.Fatalf("after yard's manager changed: %+v", top)
	}
	// With no manager on the root nothing is seated.
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "harbor", Manager: "hm", Members: []Member{{Key: "hm"}}}}))
	must(t, Update(dir, func(f *File) error { f.MakeRoot(time.Time{}); return nil }))
	f, _ = Load(dir)
	if r, _ := f.Root(); len(r.Members) != 0 {
		t.Fatalf("a root with no manager was given members: %+v", r.Members)
	}
}

package teams

import (
	"encoding/json"
	"strings"
	"testing"
)

func treeFile() *File {
	return &File{Teams: []Team{
		{ID: "top", Name: "top"}, {ID: "mid", Name: "mid"}, {ID: "low", Name: "low"}, {ID: "other", Name: "other"},
	}}
}

func names(ts []Team) string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return strings.Join(out, ",")
}

// THE TREE: one parent, which exists, and no loops.
func TestTreeRefusesLoops(t *testing.T) {
	f := treeFile()
	if err := f.SetParent("mid", "top"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetParent("low", "mid"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, parent, why string }{
		{"top", "top", "a team under itself"},
		{"top", "low", "a team under its own grandchild"},
		{"mid", "low", "a team under its own child"},
		{"mid", "nobody", "a parent that does not exist"},
		{"nobody", "top", "a team that does not exist"},
	} {
		if err := f.SetParent(c.id, c.parent); err == nil {
			t.Fatalf("%s was allowed", c.why)
		}
	}
	if got := names(f.Ancestors("low")); got != "mid,top" {
		t.Fatalf("low's ancestors are %q", got)
	}
	if got := names(f.Children("")); got != "top,other" {
		t.Fatalf("the top level is %q", got)
	}
	if got := names(f.Children("top")); got != "mid" {
		t.Fatalf("top's children are %q", got)
	}
	if err := f.SetParent("low", ""); err != nil {
		t.Fatal(err)
	}
	if got := names(f.Children("")); got != "top,low,other" {
		t.Fatalf("after moving low to the top: %q", got)
	}
}

// ONE MANAGER, ALWAYS A MEMBER.
func TestManagerSetAndClear(t *testing.T) {
	f := &File{Teams: []Team{{ID: "t1", Name: "harbor", Members: []Member{{Key: "a", Word: "alpha"}}}}}
	if err := f.SetManager("t1", "a"); err != nil {
		t.Fatal(err)
	}
	if f.Teams[0].Manager != "a" || len(f.Teams[0].Members) != 1 {
		t.Fatalf("after setting a member: %+v", f.Teams[0])
	}
	// A conversation that is not a member joins, and replaces the manager.
	if err := f.SetManager("t1", "boss"); err != nil {
		t.Fatal(err)
	}
	if f.Teams[0].Manager != "boss" || !f.Teams[0].Holds("boss") || !f.Teams[0].Holds("a") {
		t.Fatalf("after setting an outsider: %+v", f.Teams[0])
	}
	if err := f.SetManager("t1", ""); err == nil {
		t.Fatal("an empty manager was accepted")
	}
	if err := f.SetManager("nope", "a"); err == nil {
		t.Fatal("a manager for a team that does not exist was accepted")
	}
	if err := f.ClearManager("t1"); err != nil {
		t.Fatal(err)
	}
	if f.Teams[0].Manager != "" || !f.Teams[0].Holds("boss") {
		t.Fatalf("clear removed more than the role: %+v", f.Teams[0])
	}
	// Removing the manager from the team ends the role.
	_ = f.SetManager("t1", "a")
	if err := f.RemoveMember("t1", "a"); err != nil {
		t.Fatal(err)
	}
	if f.Teams[0].Manager != "" || f.Teams[0].Holds("a") {
		t.Fatalf("a removed manager is still one: %+v", f.Teams[0])
	}
	// And a manager set by hand to a stranger is cleared by the next save.
	f.Teams[0].Manager = "stranger"
	dir := t.TempDir()
	if err := Save(dir, f.Teams); err != nil {
		t.Fatal(err)
	}
	if f.Teams[0].Manager != "" {
		t.Fatalf("saved a manager who is not a member: %q", f.Teams[0].Manager)
	}
}

func TestAddMemberAssignsAHandleOnce(t *testing.T) {
	f := &File{Teams: []Team{{ID: "t1", Name: "harbor"}}}
	if err := f.AddMember("t1", Member{Key: "a", Word: "Refactor the parser"}); err != nil {
		t.Fatal(err)
	}
	if err := f.AddMember("t1", Member{Key: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := f.AddMember("t1", Member{Key: "a", Word: "again"}); err != nil {
		t.Fatal(err)
	}
	tm := f.Teams[0]
	if len(tm.Members) != 2 || tm.Members[0].Handle != "parser" || tm.Members[1].Handle != "" {
		t.Fatalf("members %+v", tm.Members)
	}
	// The untitled member takes a handle when it has a title; the titled one
	// keeps its handle when its title changes.
	f.Teams[0].Members[1].Word = "Refactor the lexer"
	f.Teams[0].Members[0].Word = "Something else entirely"
	tidy(f.Teams)
	if h := f.Teams[0].Members; h[0].Handle != "parser" || h[1].Handle != "lexer" {
		t.Fatalf("handles after titles moved: %+v", h)
	}
	if err := f.AddMember("t1", Member{}); err == nil {
		t.Fatal("a member with no key was accepted")
	}
}

// A TEAM WAKES UNLESS IT WAS TURNED OFF, and only the off is written: a file
// that never mentioned waking reads as on, and on is written as nothing.
func TestATeamWakesUnlessTurnedOff(t *testing.T) {
	var fresh Team
	if err := json.Unmarshal([]byte(`{"id":"a","name":"a"}`), &fresh); err != nil {
		t.Fatal(err)
	}
	if !fresh.Wakes() {
		t.Fatal("a team whose file says nothing about waking does not wake")
	}
	raw, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"wake"`) {
		t.Fatalf("a team that wakes wrote the field: %s", raw)
	}
	fresh.WakeOff = true
	raw, err = json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"wake":false`) {
		t.Fatalf("a team turned off did not write wake false: %s", raw)
	}
	var back Team
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Wakes() {
		t.Fatal("wake false did not survive a round trip")
	}
	if err := json.Unmarshal([]byte(`{"id":"a","name":"a","wake":true}`), &back); err != nil {
		t.Fatal(err)
	}
	if !back.Wakes() {
		t.Fatal("wake true reads as off")
	}
}

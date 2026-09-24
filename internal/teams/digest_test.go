package teams

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func digestTeam() Team {
	return Team{ID: "t1", Name: "harbor", Manager: "m", Members: []Member{
		{Key: "m", Word: "Harbor manager", Handle: "lead"},
		{Key: "a", Word: "Fix the login bug", Handle: "fix-login"},
		{Key: "b", Word: "Write the docs", Handle: "docs"},
		{Key: "c"},
	}}
}

func trafficFixture(n int) []Entry {
	var out []Entry
	at := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		out = append(out, Entry{ID: fmt.Sprintf("%012d", i), At: at.Add(time.Duration(i) * time.Minute),
			Kind: KindNote, From: "fix-login", To: ToManager, Text: fmt.Sprintf("line %d", i)})
	}
	return out
}

func TestDigestContent(t *testing.T) {
	states := map[string]MemberState{
		"a": {State: StateAsking, SinceActive: 3 * time.Minute, Question: "Which\ndatabase?",
			Files: []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"}},
		"b": {State: StateFinished, SinceActive: 26 * time.Hour},
	}
	recent := trafficFixture(3)
	recent[2].Files = []string{"auth/login.go"}
	got := Digest(digestTeam(), states, recent, 0)
	for _, want := range []string{
		`Team "harbor": 4 members, manager @lead.`,
		`- @lead (manager) "Harbor manager": unknown`,
		`- @fix-login "Fix the login bug": asking, active within the hour. Asking: "Which database?". Files: a.go, b.go, c.go, d.go, e.go +2 more`,
		`- @docs "Write the docs": finished, active 26h ago`,
		`- [c]: unknown`,
		"Recent traffic:\n- 09:01 note fix-login -> manager: line 1\n",
		`- 09:03 note fix-login -> manager: line 3 [auth/login.go]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the digest lacks %q:\n%s", want, got)
		}
	}
	// The same inputs give the same text.
	if again := Digest(digestTeam(), states, recent, 0); again != got {
		t.Fatal("the digest is not deterministic")
	}
}

// THE BUDGET CUTS TRAFFIC FROM THE OLDEST END, and never the members, until
// the members alone do not fit.
func TestDigestBudget(t *testing.T) {
	team := digestTeam()
	full := Digest(team, nil, trafficFixture(40), 0)
	if strings.Contains(full, "manager: line 20\n") || !strings.Contains(full, "manager: line 21\n") || !strings.Contains(full, "manager: line 40\n") {
		t.Fatalf("more than the last twenty lines were shown:\n%s", full)
	}
	members := Digest(team, nil, nil, 0)
	budget := utf8.RuneCountInString(members) + 200
	cut := Digest(team, nil, trafficFixture(40), budget)
	if n := utf8.RuneCountInString(cut); n > budget {
		t.Fatalf("%d characters over a budget of %d", n, budget)
	}
	if !strings.HasPrefix(cut, members) || !strings.Contains(cut, "line 40\n") || strings.Contains(cut, "line 21\n") {
		t.Fatalf("the cut kept the wrong lines:\n%s", cut)
	}
	// Too small for the members: cut at the budget, marked.
	tiny := Digest(team, nil, trafficFixture(3), 30)
	if utf8.RuneCountInString(tiny) != 30 || !strings.HasSuffix(tiny, "…") {
		t.Fatalf("a tiny budget gave %q", tiny)
	}
	// Exactly the members: no traffic heading hanging on its own.
	if exact := Digest(team, nil, trafficFixture(3), utf8.RuneCountInString(members)); exact != members {
		t.Fatalf("a members-only budget gave:\n%s", exact)
	}
}

package remote

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// askAgent is an engine agent that answers the wall's two asks and remembers
// what it was asked and how long it was given.
type askAgent struct {
	*fakeAgent
	titles   []string
	in       session.TeamProposalInput
	deadline bool
	left     time.Duration
	calls    int
}

func (a *askAgent) NameTeam(ctx context.Context, titles []string) (string, error) {
	a.titles = titles
	a.note(ctx)
	return "harbor", nil
}

func (a *askAgent) ProposeTeams(ctx context.Context, in session.TeamProposalInput) (session.TeamProposal, error) {
	a.in = in
	a.note(ctx)
	return session.TeamProposal{
		New:         []session.ProposedTeam{{Name: "parser", Members: []string{"/srv/a.jsonl", "/srv/b.jsonl"}, Reason: "one lexer"}},
		Additions:   []session.ProposedAddition{{TeamID: "0a0a0a0a0a0a", Members: []string{"/srv/c.jsonl"}}},
		Model:       "cheap",
		PromptChars: 412,
	}, nil
}

func (a *askAgent) note(ctx context.Context) {
	a.calls++
	deadline, ok := ctx.Deadline()
	a.deadline = ok
	if ok {
		a.left = time.Until(deadline)
	}
}

func TestTeamAskBudgetBoundsEveryModelCall(t *testing.T) {
	if teamAskCeiling <= 10*time.Second {
		t.Fatalf("the engine ceiling %v cuts off the wall's Organize wait", teamAskCeiling)
	}
	for _, tc := range []struct {
		name   string
		budget time.Duration
		want   time.Duration
	}{
		{"zero", 0, teamAskCeiling},
		{"huge", time.Hour, teamAskCeiling},
		{"small", 250 * time.Millisecond, 250 * time.Millisecond},
	} {
		for _, method := range []string{MethodTeamsName, MethodTeamsPropose} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				far := &askAgent{fakeAgent: &fakeAgent{model: "m"}}
				loop := askLoop(t, far)
				var err error
				if method == MethodTeamsName {
					_, err = loop.Client.call(nil, method, TeamNameArgs{Budget: tc.budget})
				} else {
					_, err = loop.Client.call(nil, method, TeamProposeArgs{Budget: tc.budget})
				}
				if err != nil {
					t.Fatal(err)
				}
				if far.calls != 1 || !far.deadline || far.left <= 0 || far.left > tc.want || far.left < tc.want-time.Second {
					t.Fatalf("budget %v called %d times with deadline=%v, remaining=%v; want at most %v", tc.budget, far.calls, far.deadline, far.left, tc.want)
				}
			})
		}
	}
}

func TestExpiredTeamAskNeverCallsTheModel(t *testing.T) {
	far := &askAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	for _, method := range []string{MethodTeamsName, MethodTeamsPropose} {
		var err error
		if method == MethodTeamsName {
			_, err = loop.Client.call(nil, method, TeamNameArgs{Budget: -time.Nanosecond})
		} else {
			_, err = loop.Client.call(nil, method, TeamProposeArgs{Budget: -time.Nanosecond})
		}
		if err == nil || err.Error() != context.DeadlineExceeded.Error() || far.calls != 0 {
			t.Fatalf("expired %s called the model %d times and returned %v", method, far.calls, err)
		}
	}
}

func askLoop(t *testing.T, agent WrappedAgent) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: agent, ProfileDir: t.TempDir()}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

// THE WALL'S TWO ASKS CROSS, AND THE ENGINE SPENDS NO LONGER THAN THE WALL
// WAITS. The welcome says the engine answers them; a name asked for over the
// wire is the engine agent's name for the titles sent; a proposal comes back
// whole, the model that answered and the size of the ask included, since the
// wall prices the ask off them; and the engine's own call is bounded by a span
// no longer than the one the wall gave.
func TestTheWallsTeamAsksCrossTheWire(t *testing.T) {
	far := &askAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	agent := loop.Client.Agent()
	if !loop.Client.Welcome().TeamAsk {
		t.Fatal("an engine whose agent answers the asks does not say so")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name, err := agent.NameTeam(ctx, []string{"the lexer", "the parser"})
	if err != nil || name != "harbor" {
		t.Fatalf("the name: %q, %v", name, err)
	}
	if strings.Join(far.titles, "|") != "the lexer|the parser" {
		t.Fatalf("the engine was asked about %q", far.titles)
	}
	if !far.deadline || far.left <= 0 || far.left > 5*time.Second {
		t.Fatalf("the engine's name ask was bounded by %v (deadline %v), not the wall's five seconds", far.left, far.deadline)
	}

	in := session.TeamProposalInput{
		Conversations: []session.TeamProposalConversation{
			{Key: "/srv/a.jsonl", Title: "the lexer", Folder: "harbor"},
			{Key: "/srv/b.jsonl", Title: "the parser", Folder: "harbor"},
			{Key: "/srv/c.jsonl", Title: "the docs", Folder: "site"},
		},
		Teams: []session.TeamProposalTeam{{ID: "0a0a0a0a0a0a", Name: "docs", Members: []string{"/srv/d.jsonl"}}},
	}
	got, err := agent.ProposeTeams(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(far.in.Conversations) != 3 || far.in.Conversations[2].Folder != "site" || len(far.in.Teams) != 1 || far.in.Teams[0].Members[0] != "/srv/d.jsonl" {
		t.Fatalf("the engine was asked about %+v", far.in)
	}
	if len(got.New) != 1 || got.New[0].Name != "parser" || len(got.New[0].Members) != 2 || got.New[0].Reason != "one lexer" ||
		len(got.Additions) != 1 || got.Additions[0].TeamID != "0a0a0a0a0a0a" || got.Model != "cheap" || got.PromptChars != 412 {
		t.Fatalf("the proposal came back as %+v", got)
	}
	if !far.deadline || far.left <= 0 || far.left > 5*time.Second {
		t.Fatalf("the engine's proposal ask was bounded by %v (deadline %v)", far.left, far.deadline)
	}
}

// AN OLDER ENGINE IS NOT ASKED, AND THE WALL'S ASK FAILS AS ANY FAILED ASK
// DOES. An engine whose agent has no asks sends no flag; both doors are refused
// at this end with nothing written onto the wire, which the wall turns into the
// word it holds and Organize's folder pass alone. And an engine asked anyway,
// by a surface that did not read the flag, refuses rather than answering
// something that is not a name.
func TestAnOlderEngineIsNotAskedForATeamName(t *testing.T) {
	loop := askLoop(t, &fakeAgent{model: "m"})
	if loop.Client.Welcome().TeamAsk {
		t.Fatal("an engine whose agent cannot ask said it could")
	}
	agent := loop.Client.Agent()
	before := loop.Client.made.Load()
	if name, err := agent.NameTeam(context.Background(), []string{"the lexer", "the parser"}); err == nil || name != "" {
		t.Fatalf("an older engine named a team: %q, %v", name, err)
	}
	if got, err := agent.ProposeTeams(context.Background(), session.TeamProposalInput{}); err == nil || len(got.New) != 0 {
		t.Fatalf("an older engine proposed teams: %+v, %v", got, err)
	}
	if after := loop.Client.made.Load(); after != before {
		t.Fatalf("%d calls went onto the wire for asks the welcome said were not there", after-before)
	}
	if _, err := loop.Client.call(nil, MethodTeamsName, TeamNameArgs{Titles: []string{"the lexer"}}); err == nil ||
		!strings.Contains(err.Error(), teamAskOffWord) {
		t.Fatalf("an engine without the ask answered it: %v", err)
	}
}

// The actual engine, rather than only a fixture, must answer both asks, or the
// flag would be false on every real engine and the wall would never ask.
var _ teamAskDoor = (*session.Agent)(nil)

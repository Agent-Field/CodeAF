package session

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// proposeInput is four conversations and one team, harbor, holding the third.
func proposeInput() TeamProposalInput {
	return TeamProposalInput{
		Conversations: []TeamProposalConversation{
			{Key: "k-cpu", Title: "cpu profiling", Folder: "nvda"},
			{Key: "k-deep", Title: "nvda deep dive", Folder: "research"},
			{Key: "k-relay", Title: "relay audit", Folder: "codeaf"},
			{Key: "k-foot", Title: "footprint table", Folder: "codeaf"},
		},
		Teams: []TeamProposalTeam{{ID: "id-harbor", Name: "harbor", Members: []string{"k-relay"}}},
	}
}

// THE ANSWER IS A PROPOSAL AND IS READ AS ONE: a ref the ask never gave is
// dropped, a ref said twice counts once, a team left with one member is no
// team, and an existing team's members are not offered to it again.
func TestProposeTeamsParsingDropsWhatTheAskNeverGave(t *testing.T) {
	in := proposeInput()
	_, refs := teamProposePrompt(in)
	raw := "```json\n" + `{"new":[
		{"name":"NVDA Research!","members":["c1","c2","c2","c99"],"reason":"both about nvda"},
		{"name":"lonely","members":["c3","c77"]},
		{"name":"","members":["c1","c2"]}
	],"add":[
		{"team":"t1","members":["c3","c4","c4"]},
		{"team":"t9","members":["c1"]}
	]}` + "\n```"
	got, ok := parseTeamProposal(raw, in, refs)
	if !ok {
		t.Fatal("a fenced JSON answer was not read")
	}
	want := TeamProposal{
		New:       []ProposedTeam{{Name: "nvda research", Members: []string{"k-cpu", "k-deep"}, Reason: "both about nvda"}},
		Additions: []ProposedAddition{{TeamID: "id-harbor", Members: []string{"k-foot"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// A NAME IS NOT THE MODEL'S TO TAKE. A new team named like an existing one is
// additions to it, and an addition that carries a name renames nothing: the
// answer has no field that could.
func TestProposeTeamsParsingIgnoresARenameAttempt(t *testing.T) {
	in := proposeInput()
	_, refs := teamProposePrompt(in)
	raw := `{"new":[{"name":"Harbor","members":["c4","c1"]}],
		"add":[{"team":"t1","name":"harbour renamed","members":[]}],
		"rename":[{"team":"t1","name":"docks"}],"remove":[{"team":"t1","members":["c3"]}]}`
	got, ok := parseTeamProposal(raw, in, refs)
	if !ok {
		t.Fatal("the answer was not read")
	}
	if len(got.New) != 0 {
		t.Fatalf("a team named like harbor was proposed as new: %+v", got.New)
	}
	want := []ProposedAddition{{TeamID: "id-harbor", Members: []string{"k-foot", "k-cpu"}}}
	if !reflect.DeepEqual(got.Additions, want) {
		t.Fatalf("additions %+v, want %+v", got.Additions, want)
	}
}

// TWO NEW TEAMS OF ONE NAME ARE ONE, and prose around the object is ignored;
// an answer with no object at all is not a proposal.
func TestProposeTeamsParsingMergesDuplicatesAndRefusesProse(t *testing.T) {
	in := proposeInput()
	_, refs := teamProposePrompt(in)
	raw := `Here you go: {"new":[{"name":"ops","members":["c1","c3"]},{"name":"OPS","members":["c4","c1"]}]} hope that helps`
	got, ok := parseTeamProposal(raw, in, refs)
	if !ok || len(got.New) != 1 || !reflect.DeepEqual(got.New[0].Members, []string{"k-cpu", "k-relay", "k-foot"}) {
		t.Fatalf("got %+v ok %v", got, ok)
	}
	if _, ok := parseTeamProposal("I would group the relay ones together.", in, refs); ok {
		t.Fatal("prose was read as a proposal")
	}
	if got, ok := parseTeamProposal(`{"new":[],"add":[]}`, in, refs); !ok || len(got.New)+len(got.Additions) != 0 {
		t.Fatalf("an empty proposal is an answer: %+v %v", got, ok)
	}
}

// THE PROMPT CARRIES TITLES, FOLDERS AND TEAMS BY REF, one line each, and a
// title's newlines never break the list.
func TestProposeTeamsPromptShape(t *testing.T) {
	in := proposeInput()
	in.Conversations[0].Title = "cpu\nprofiling"
	ask, refs := teamProposePrompt(in)
	for _, want := range []string{"c1 | cpu profiling | folder: nvda\n", "c4 | footprint table | folder: codeaf\n", "t1 | harbor | relay audit\n", teamProposeAsk} {
		if !strings.Contains(ask, want) {
			t.Fatalf("the prompt lacks %q:\n%s", want, ask)
		}
	}
	if refs.convs["c3"] != "k-relay" || refs.teams["t1"] != "id-harbor" {
		t.Fatalf("refs %+v", refs)
	}
}

// ONE CALL ON THE CHEAP TIER, BILLED OFF THE TURN, and the model that answered
// is reported so the surface can price the ask.
func TestProposeTeamsAsksTheNamingRoleOnce(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(`{"new":[{"name":"nvda","members":["c1","c2"],"reason":"one company"}],"add":[]}`), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := agent.ProposeTeams(ctx, proposeInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.New) != 1 || got.New[0].Name != "nvda" || got.Model != "cheap/model" || got.PromptChars == 0 {
		t.Fatalf("got %+v", got)
	}
	if client.requests() != 1 || client.model(0) != "cheap/model" {
		t.Fatalf("%d requests, first on %q", client.requests(), client.model(0))
	}
	if usage := agent.Usage(); usage.Calls != 1 || usage.Turns != 0 {
		t.Fatalf("usage %+v, want one call charged to no turn", usage)
	}
}

// AN ANSWER THAT IS NOT A PROPOSAL IS AN ERROR after at most one fall-through,
// so the surface falls back to the folders.
func TestProposeTeamsRefusesProseAfterOneFallThrough(t *testing.T) {
	answer := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("these look like two projects to me"), nil
	}
	steps := make([]step, roleFallThroughs+1)
	for i := range steps {
		steps[i] = answer
	}
	client := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	if _, err := agent.ProposeTeams(context.Background(), proposeInput()); err == nil {
		t.Fatal("prose was taken as a proposal")
	}
	if n := client.requests(); n > roleFallThroughs+1 {
		t.Fatalf("%d requests, want at most %d", n, roleFallThroughs+1)
	}
}

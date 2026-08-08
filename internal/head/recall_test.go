package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// "Remember that pricing analysis from January?" traced through every arm of the
// gate and opened none of them: no control verb, nothing live to point at, and
// the relevance arm started from a snapshot query that excluded the very job a
// territory had packed away. The mitigation and the gate were keyed on the same
// broken lookup, so the message fell through to a router carrying no January at
// all, under a prompt that forbade it from saying so.
func TestAPackedAwayJobOpensTheBeltInsteadOfFallingThrough(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "pricing", "January pricing analysis",
		"analyse pricing for the enterprise tier")
	claim, won, err := graph.Claim("pricing", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "the seat ladder wins above forty users"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("pricing", "the seat ladder wins above forty users", nil); err != nil {
		t.Fatal(err)
	}
	// The retrospective tidies the job away a few hours after it lands. Before
	// this wave, that single act removed it from every snapshot-derived read.
	if err := graph.FormTerritory("terr", "Pricing", "pricing work", nil, []string{"pricing"}); err != nil {
		t.Fatal(err)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolSearch, map[string]any{
			"q": "pricing analysis"})}},
		{text: "The January pricing analysis concluded the seat ladder wins above forty users."},
	}}
	session := "recall"
	user := postUser(t, graph, session, "remember that pricing analysis from January?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if _, tooled := client.counts(); tooled == 0 {
		t.Fatal("the belt never opened for a question about packed-away work")
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, "seat ladder") {
		t.Fatalf("the answer did not come from the recovered job: %q", reply.Body)
	}
}

// The other half: something that was only ever SAID. It never became a job, so
// no work-shaped arm can see it, and until the conversation was indexed nothing
// in the product could.
func TestSomethingOnlySaidIsFoundAndQuoted(t *testing.T) {
	graph := openHeadStore(t)
	session := "said"
	if _, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleUser,
		Body: "for the record, we settled on calling the migration cutover the beacon window"}); err != nil {
		t.Fatal(err)
	}
	// Bury it past everything the front desk can still see.
	for index := range 40 {
		if _, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleAgent,
			Body: "unrelated chatter " + string(rune('a'+index%26))}); err != nil {
			t.Fatal(err)
		}
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolSearch, map[string]any{
			"q": "beacon window cutover"})}},
		{text: "You settled on calling the migration cutover the beacon window."},
	}}
	user := postUser(t, graph, session, "what did we decide to call the migration cutover window?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if _, tooled := client.counts(); tooled == 0 {
		t.Fatal("a question about buried conversation never opened the belt")
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(strings.ToLower(reply.Body), "beacon window") {
		t.Fatalf("the buried decision was not recovered: %q", reply.Body)
	}
}

// An empty search is a true answer, and the tool says so in the words the loop
// is meant to reuse. The alternative the prompt used to leave was a fluent
// reconstruction of something that may never have happened.
func TestAnEmptySearchSaysSoRatherThanInventing(t *testing.T) {
	graph := openHeadStore(t)
	run := &beltRun{head: New(&beltClient{}, graph)}
	answer, isError := run.search(map[string]any{"q": "the tungsten procurement memo"})
	if isError {
		t.Fatalf("an honest miss was reported as a tool error: %q", answer)
	}
	if !strings.Contains(answer, "nothing remembered matches") {
		t.Fatalf("empty search = %q, want an explicit miss", answer)
	}
}

// The three memories are one read. A person asking what was decided has no idea
// whether the answer is a line they said, a belief you kept, or a job you ran.
func TestSearchReadsConversationNotebookAndFinishedWorkTogether(t *testing.T) {
	graph := openHeadStore(t)
	if _, err := graph.PostMessage(store.Message{SessionID: "s", Role: store.RoleUser,
		Body: "the quarterly ledger reconciliation always slips by a week"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("", "domain:ledger", store.FactLesson,
		"quarterly ledger reconciliation needs the bank export first"); err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "ledger", "Ledger reconciliation", "reconcile the quarterly ledger")
	claim, won, err := graph.Claim("ledger", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "reconciled with two adjustments"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("ledger", "reconciled with two adjustments", nil); err != nil {
		t.Fatal(err)
	}

	run := &beltRun{head: New(&beltClient{}, graph)}
	answer, isError := run.search(map[string]any{"q": "quarterly ledger reconciliation"})
	if isError {
		t.Fatalf("search errored: %q", answer)
	}
	for _, wanted := range []string{"said in conversation", "in the notebook", "in work that has already finished"} {
		if !strings.Contains(answer, wanted) {
			t.Fatalf("search omitted the %q memory:\n%s", wanted, answer)
		}
	}
}

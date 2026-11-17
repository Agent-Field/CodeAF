package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AFI'S SETUP CARD IS ITS PERSON'S ALONE, AND KEEPING IT WRITES NOTHING SHARED
// (owner decision 2026-11-17 §9). mia's Afi proposes a card: it is kept for mia,
// abir reads none, the workstream's rev, tasks and files do not move, a name
// nobody answers to is named back with who is there, and mia's PATCH with
// drop_setup_card clears it.
func TestAfisSetupCardIsKeptForItsPersonAlone(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var before api.Workstream
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &before)
	var said string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		d := turn.Door.(workDoor)
		if _, err := d.Work(ctx, "ws_propose_setup", json.RawMessage(`{"name":"x","goal":"y","tasks":[]}`)); err == nil || !strings.Contains(err.Error(), "1 to 8 tasks") {
			t.Errorf("an empty plan was kept: %v", err)
		}
		if _, err := d.Work(ctx, "ws_propose_setup", json.RawMessage(`{"name":"x","goal":"y","tasks":[{"title":"a","waits_on":[2]},{"title":"b","waits_on":[1]}]}`)); err == nil || !strings.Contains(err.Error(), "circle") {
			t.Errorf("a circle of waits was kept: %v", err)
		}
		line, err := d.Work(ctx, "ws_propose_setup", json.RawMessage(`{"name":"Webhook retries","goal":"Retries ship by Dec 1.","people":["Abir","Zed Quill"],`+
			`"rules":["Nothing ships without abir's review"],"start_without_asking_usd":5,`+
			`"tasks":[{"title":"Write the retry design"},{"title":"Review the design","doer":"person","person":"abir","waits_on":[1]}]}`))
		if err != nil {
			t.Fatalf("the card was refused: %v", err)
		}
		said = line
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Set it up."}, 202, nil)
	o.settle()

	if !strings.Contains(said, "Nothing is set up until they press Accept") || !strings.Contains(said, "Zed Quill") || strings.Contains(said, "called Abir") {
		t.Errorf("the tool answered %q", said)
	}
	var mine, theirs api.Workstream
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &mine)
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &theirs)
	card := mine.SetupCard
	if card == nil || card.Name != "Webhook retries" || card.By != "mia" || card.At == "" || len(card.Tasks) != 2 || card.Tasks[0].Doer != "afi" || card.Tasks[1].WaitsOn[0] != 1 {
		t.Fatalf("mia's card %+v", card)
	}
	if theirs.SetupCard != nil {
		t.Fatalf("abir reads mia's card %+v", theirs.SetupCard)
	}
	if mine.Rev != before.Rev || mine.Name != before.Name || mine.Purpose != before.Purpose {
		t.Fatalf("proposing moved the workstream: rev %d→%d, %q→%q", before.Rev, mine.Rev, before.Name, mine.Name)
	}
	var tasks api.WorkstreamTasks
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/tasks", -1, nil, 200, &tasks)
	if len(tasks.Tasks) != 0 {
		t.Fatalf("proposing made %d tasks", len(tasks.Tasks))
	}

	name, purpose := "Webhook retries", "Retries ship by Dec 1."
	o.do(t, "mia", "PATCH", "/v1/workstreams/"+webhook.ID, mine.Rev, api.WorkstreamPatch{Name: &name, Purpose: &purpose, DropSetupCard: true}, 200, nil)
	var after api.Workstream
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &after)
	if after.SetupCard != nil || after.Name != name || after.Purpose != purpose {
		t.Fatalf("after Accept: card %+v, %q, %q", after.SetupCard, after.Name, after.Purpose)
	}
	if mine.SetupCard == nil {
		t.Fatalf("before Accept: %+v", mine.SetupCard)
	}
}

// A TASK'S RUN NEVER PROPOSES A SETUP: nobody is there to accept it.
func TestATaskRunCannotProposeASetup(t *testing.T) {
	d := &agentDoor{wrote: &taskWrites{}}
	if _, err := d.Work(context.Background(), "ws_propose_setup", json.RawMessage(`{"name":"x","goal":"y","tasks":[{"title":"a"}]}`)); err == nil || !strings.Contains(err.Error(), "task's run") {
		t.Fatalf("a task run proposed: %v", err)
	}
	if !d.KeepsSetupCards() {
		t.Fatal("the server's door does not say it keeps setup cards")
	}
}

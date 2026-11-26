package serve

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// proposeDoor is a turn's door as these tests use it: one that works and says
// whether it proposes, whatever wraps it (the Library's door does).
type proposeDoor interface {
	workDoor
	Proposes() bool
}

// AFI'S TASKS ARRIVE AS CARDS, AND NOTHING IS MADE UNTIL THE PERSON ADDS THEM
// (Workstreams v2, screen 8). Before, the server's Afi made a plan's tasks
// the moment it was asked, for people the asker had not yet agreed to hand
// work to, and they were told before the asker had read the plan.
func TestAfisTasksArriveAsCardsAndAreMadeOnlyWhenAdded(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var said string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		d := turn.Door.(proposeDoor)
		if !d.Proposes() {
			t.Fatal("a person's conversation with Afi does not propose")
		}
		for args, want := range map[string]string{
			`{"tasks":[]}`: "1 to 8",
			`{"tasks":[{"title":"x","request":"y","owner":"nobody-here"}]}`: "not someone in this workstream",
			`{"tasks":[{"title":"x","request":"y","waits_on":[1]}]}`:        "only on earlier cards",
			`{"tasks":[{"title":"x","request":"y","due":"Friday"}]}`:        "YYYY-MM-DD",
			`{"group":"Launch","tasks":[{"title":"x","request":"y"}]}`:      "in ONE call",
			`{"tasks":[{"title":"x","request":"y","done_by":"factory"}]}`:   "afi or person",
		} {
			if line, err := d.Work(ctx, "ws_task_propose", json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s = %q, %v; want a refusal saying %q", args, line, err, want)
			}
		}
		line, err := d.Work(ctx, "ws_task_propose", json.RawMessage(`{"tasks":[`+
			`{"title":"Draft the launch post","request":"Write it.","checklist":["Outline","First draft"]},`+
			`{"title":"Review the launch post","request":"Read it.","done_by":"person","person":"santosh","waits_on":[1],"due":"2026-11-28"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		said = line
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Plan the launch post.", Cards: true}, 202, nil)
	o.settle()
	if !strings.Contains(said, "Nothing is made until they press Add 2 tasks") || !strings.Contains(said, "Priya Nair is told") {
		t.Errorf("the proposal answered %q", said)
	}
	// Sim v3 D5: the reply stays under the card after Add, so Afi is told never to say the tasks' state.
	if !strings.Contains(said, "never say whether the tasks exist, and never tell them to press Add or Start") {
		t.Errorf("the proposal does not keep Afi's reply from going stale: %q", said)
	}

	// Nothing shared yet: no task, and santosh has not been told.
	var tasks api.WorkstreamTasks
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/tasks", -1, nil, 200, &tasks)
	if len(tasks.Tasks) != 0 {
		t.Fatalf("a proposal made %d tasks", len(tasks.Tasks))
	}
	var mine api.WorkstreamAgent
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &mine)
	var cards *api.AgentEntry
	for i, e := range mine.Entries {
		if e.Kind == api.AgentEntryCards {
			cards = &mine.Entries[i]
		}
	}
	if cards == nil || cards.Cards == nil || len(cards.Cards.Tasks) != 2 || cards.Cards.Tasks[1].Person != "santosh" {
		t.Fatalf("mia's conversation %+v", mine.Entries)
	}
	// The cards are mia's alone, like the rest of her conversation.
	var abirs api.WorkstreamAgent
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &abirs)
	if len(abirs.Entries) != 0 {
		t.Fatalf("abir reads %+v", abirs.Entries)
	}
	path := "/v1/workstreams/" + webhook.ID + "/agent/cards/" + strconv.Itoa(cards.Seq)
	o.do(t, "abir", "POST", path, -1, api.AgentCardsAdd{Tasks: cards.Cards.Tasks}, 404, nil)

	// mia changes the first title, keeps both ticked, and adds them.
	add := cards.Cards.Tasks
	add[0].Title = "Draft the 2.4 launch post"
	var after api.WorkstreamAgent
	o.do(t, "mia", "POST", path, -1, api.AgentCardsAdd{Tasks: add}, 200, &after)
	note := after.Entries[len(after.Entries)-1]
	if note.Took != cards.Seq || len(note.Tasks) != 2 || note.Text != "Added 2 tasks · Priya Nair is told" {
		t.Fatalf("the note %+v", note)
	}
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/tasks", -1, nil, 200, &tasks)
	if len(tasks.Tasks) != 2 {
		t.Fatalf("adding made %d tasks", len(tasks.Tasks))
	}
	byTitle := map[string]int{}
	for i, x := range tasks.Tasks {
		byTitle[x.Title] = i
	}
	draft, review := tasks.Tasks[byTitle["Draft the 2.4 launch post"]], tasks.Tasks[byTitle["Review the launch post"]]
	if draft.CreatedBy.ID != "mia" || draft.Owner.ID != "mia" || draft.DoneBy.Kind != "afi" || draft.CameFrom.Name != "Afi" {
		t.Errorf("the draft %+v", draft)
	}
	// Afi wrote the cards' words, though Mia added them: they are never a
	// leash for web_fetch (taskLeashWords).
	if draft.TitleBy != "afi" || draft.RequestBy != "afi" || review.TitleBy != "afi" {
		t.Errorf("an added card's words read as written by %q / %q", draft.TitleBy, draft.RequestBy)
	}
	// A card's steps arrive as the task's checklist.
	if len(draft.Checklist) != 2 || draft.Checklist[0].Text != "Outline" {
		t.Errorf("the draft's checklist %+v", draft.Checklist)
	}
	if review.DoneBy.Kind != "person" || review.DoneBy.ID != "santosh" || review.Due != "2026-11-28" ||
		len(review.WaitingOn) != 1 || review.WaitingOn[0] != draft.ID {
		t.Errorf("the review %+v", review)
	}
	// Added once: a second press never makes them twice.
	o.do(t, "mia", "POST", path, -1, api.AgentCardsAdd{Tasks: add}, 409, nil)
}

// A PLAN OF CARDS IS ONE GROUP THAT WAITS FOR START, and only the ticked
// cards are made: a card left unticked is never sent.
func TestAddedCardsOfAPlanShareOneGroup(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		var cards []string
		for i := 1; i <= 6; i++ {
			cards = append(cards, `{"title":"Part `+strconv.Itoa(i)+`","request":"Do it."}`)
		}
		said, err := turn.Door.(proposeDoor).Work(ctx, "ws_task_propose", json.RawMessage(`{"group":"Release 0.9","tasks":[`+strings.Join(cards, ",")+`]}`))
		if err != nil {
			t.Error(err)
		}
		// A plan goes to the Inbox and waits for Start; it is never "press Add" (sim v3 D5).
		if !strings.Contains(said, "a plan of 6 tasks: it goes to their Inbox and nothing starts until they press Start there") || strings.Contains(said, "until they press Add") {
			t.Errorf("the plan's proposal answered %q", said)
		}
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Plan the release.", Cards: true}, 202, nil)
	o.settle()
	var mine api.WorkstreamAgent
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &mine)
	var seq int
	var proposed api.AgentCards
	for _, e := range mine.Entries {
		if e.Kind == api.AgentEntryCards {
			seq, proposed = e.Seq, *e.Cards
		}
	}
	// mia unticks the last card: five are sent.
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/cards/"+strconv.Itoa(seq), -1,
		api.AgentCardsAdd{Group: proposed.Group, Tasks: proposed.Tasks[:5]}, 200, nil)
	var tasks api.WorkstreamTasks
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/tasks", -1, nil, 200, &tasks)
	if len(tasks.Tasks) != 5 {
		t.Fatalf("%d tasks", len(tasks.Tasks))
	}
	for _, x := range tasks.Tasks {
		if x.Group == nil || x.Group.Name != "Release 0.9" || x.Group.ID != tasks.Tasks[0].Group.ID || x.State != "up_next" {
			t.Errorf("%s: group %+v state %s", x.Title, x.Group, x.State)
		}
	}
}

// A TASK'S OWN RUN NEVER PROPOSES: nobody sits in front of it to press Add,
// so its door says so and the verb is left off its belt.
func TestATaskRunDoesNotPropose(t *testing.T) {
	d := &agentDoor{wrote: &taskWrites{}, cards: true}
	if d.Proposes() {
		t.Fatal("a task run's door proposes")
	}
	if _, err := d.wsTaskPropose(context.Background(), json.RawMessage(`{"tasks":[{"title":"x","request":"y"}]}`)); err == nil || !strings.Contains(err.Error(), "cannot be proposed") {
		t.Fatalf("a task run proposed: %v", err)
	}
}

// THE MOST CARDS IS THE MOST TASKS A PLAN HOLDS: one number, in every place.
func TestTheMostCardsIsAPlansMost(t *testing.T) {
	if api.AgentCardsMost != 8 || cardsPlanAt != 5 {
		t.Fatalf("cards most %d, plan at %d; session's plan is 5 to 8", api.AgentCardsMost, cardsPlanAt)
	}
}

// AN APP THAT CANNOT DRAW A CARD NEVER GETS ONE. A message from an app that
// does not say it draws cards leaves the verb off Afi's belt, so Afi makes
// tasks the way it always did rather than proposing cards nobody can add.
func TestAnAppThatCannotDrawCardsGetsNone(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var proposes bool
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		proposes = turn.Door.(proposeDoor).Proposes()
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Plan it."}, 202, nil)
	o.settle()
	if proposes {
		t.Fatal("a message from an app that draws no cards may propose")
	}
}

// CARDS LIVE IN AFI'S OWN CONVERSATION. A person's own chat in the
// workstream never proposes: the line Add names is numbered in Afi's
// conversation, so a card drawn in a chat could not be added.
func TestAPersonsOwnChatNeverProposes(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var proposes []bool
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		proposes = append(proposes, turn.Door.(proposeDoor).Proposes())
		return AgentTurnEnd{}, nil
	})
	var side api.AgentChat
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/chats", -1, api.AgentChatNew{Title: "Side"}, 201, &side)
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Plan it.", Chat: side.ID, Cards: true}, 202, nil)
	o.settle()
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Plan it.", Cards: true}, 202, nil)
	o.settle()
	if len(proposes) != 2 || proposes[0] || !proposes[1] {
		t.Fatalf("a chat, then Afi's conversation, proposed %v; want [false true]", proposes)
	}
}

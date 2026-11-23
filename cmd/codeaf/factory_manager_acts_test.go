package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// pressedVerb is one press the fake factory was sent: its verb and its body
// as the wire carries it.
type pressedVerb struct {
	job  int
	verb string
	body string
}

// fakePress hangs a press that records what it is sent, as the app's own
// would send it, and answers refuse.
func fakePress(t *testing.T, refuse func(verb string) error) *[]pressedVerb {
	t.Helper()
	var sent []pressedVerb
	press := localJobPress(func(_ context.Context, job int, verb string, body any) error {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("the body of %s is not JSON: %v", verb, err)
		}
		sent = append(sent, pressedVerb{job, verb, string(raw)})
		if refuse != nil {
			return refuse(verb)
		}
		return nil
	})
	prev := appLocalPress.Swap(&press)
	t.Cleanup(func() { appLocalPress.Store(prev) })
	return &sent
}

// managedJob puts one job on st whose manager is a new conversation, shaped by
// set, and answers its id and that conversation.
func managedJob(t *testing.T, st *store.Store, set func(*factory.Item)) (int, string) {
	t.Helper()
	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	it, err := st.Create(factory.Item{Repo: "web", Title: "fix the ledger double count", Kind: factory.KindIssue, Tier: factory.TierOwner, Talk: talk})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(it *factory.Item) error {
		set(it)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return it.ID, talk
}

// The jobs the authority matrix presses on, one for each decision the job
// page would offer.
func waitsOnPublishApproval(it *factory.Item) {
	it.State, it.Publish, it.Assignee, it.Cap, it.HeadCommit = factory.StateLanded, "pr", "sam", 5, "abc"
	it.Proof = []factory.Claim{{Text: "tests pass", OK: true}}
	it.Stages = []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageCheck, On: true}}
	it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "write", State: factory.PhaseDone}, {Name: "proof", State: factory.PhaseDone}}}
}

func hasDraftPullRequest(it *factory.Item) {
	it.State, it.Publish, it.Assignee, it.Cap, it.HeadCommit = factory.StateShipped, "pr", "sam", 5, "abc"
	it.PullRequest = &factory.PullRequest{Number: 161, State: factory.PROpen, Draft: true, HeadSHA: "abc", Mergeable: factory.MergeableYes, ChecksRead: true, ReviewsRead: true}
}

func hasMergeablePullRequest(it *factory.Item) {
	hasDraftPullRequest(it)
	it.PullRequest.Draft = false
}

// THE AUTHORITY TABLE IS HELD AT THE MANAGER'S DOOR, EVERY CELL, whichever
// tool asks. Outside Auto, approving publication, marking the pull request
// ready and approving public acts are pressed only on a turn the person
// spoke in; in Auto the manager presses them itself; a merge is pressed only
// on the person's word in every mode. A refused press sends nothing.
func TestTheManagersDoorHoldsTheAuthorityTable(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, nil)
	door, ok := runDoor(st, web, "").(session.ManagerPresser)
	if !ok {
		t.Fatal("the floor's run door cannot press")
	}
	cells := []struct {
		action string
		shape  func(*factory.Item)
		verb   string
		// Auto may press it with no word from the person.
		autoAlone bool
	}{
		{session.ActionApprove, waitsOnPublishApproval, "approve", true},
		{session.ActionMarkReady, hasDraftPullRequest, "pr-ready", true},
		{session.ActionApproveActs, hasDraftPullRequest, "acts_approve", true},
		{session.ActionMerge, hasMergeablePullRequest, "merge", false},
	}
	for _, mode := range []string{factory.AutonomyManual, "", factory.AutonomyAuto} {
		for _, cell := range cells {
			id, talk := managedJob(t, st, func(it *factory.Item) {
				cell.shape(it)
				it.Autonomy = mode
			})
			name := fmt.Sprintf("%s in %q", cell.action, mode)
			// A TURN THE ENGINE STARTED carries no words of the person's,
			// whatever the model writes in its own.
			*sent = nil
			line, err := door.Press(context.Background(), talk, session.ManagerPress{Action: cell.action, Words: "the person told me to go ahead"})
			allowed := mode == factory.AutonomyAuto && cell.autoAlone
			switch {
			case allowed && (err != nil || len(*sent) != 1 || (*sent)[0].verb != cell.verb || (*sent)[0].job != id):
				t.Errorf("%s by the manager alone = %q (%v), sent %+v; want %s pressed", name, line, err, *sent, cell.verb)
			case !allowed && (err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: ") || !strings.Contains(err.Error(), "nothing in this turn came from them") || len(*sent) != 0):
				t.Errorf("%s with no person words = %q (%v), sent %+v; want a refusal and nothing sent", name, line, err, *sent)
			}
			// AN APPROVAL THE MANAGER GAVE BY ITSELF IS NOT SAID BACK AS THE
			// PERSON'S.
			if allowed && cell.action == session.ActionApprove && (!strings.Contains(line, "approved by you, as Auto allows") || strings.Contains(line, "as the person")) {
				t.Errorf("%s by the manager alone read back as %q", name, line)
			}
			// A TURN THE PERSON SPOKE IN presses it, in every mode.
			*sent = nil
			if cell.action == session.ActionApprove || cell.action == session.ActionMarkReady {
				// The alone-in-Auto press above already settled this job.
				id, talk = managedJob(t, st, func(it *factory.Item) {
					cell.shape(it)
					it.Autonomy = mode
				})
			}
			line, err = door.Press(context.Background(), talk, session.ManagerPress{Action: cell.action, Words: "they said yes", Said: "yes, go ahead"})
			if err != nil || len(*sent) != 1 || (*sent)[0].verb != cell.verb || (*sent)[0].job != id {
				t.Errorf("%s with the person's words = %q (%v), sent %+v; want %s pressed", name, line, err, *sent, cell.verb)
			}
			if (cell.action == session.ActionApprove || cell.action == session.ActionApproveActs) && !strings.Contains(line, "as the person") {
				t.Errorf("%s with the person's words read back as %q", name, line)
			}
		}
	}
	// THE REFUSAL SAYS WHOSE IT IS AND HOW THEY DO IT.
	_, talk := managedJob(t, st, func(it *factory.Item) {
		hasMergeablePullRequest(it)
		it.Autonomy = factory.AutonomyAuto
	})
	if _, err := door.Press(context.Background(), talk, session.ManagerPress{Action: session.ActionMerge, Words: "merge it"}); err == nil || err.Error() != factory.ManagerMergeRefusal {
		t.Errorf("a merge the person did not ask for = %v", err)
	}
	// AUTO'S OWN RESUME IS THE PERSON'S TOO, and declining acts and handing a
	// stop over are the manager's whatever the mode.
	if _, err := door.Press(context.Background(), talk, session.ManagerPress{Action: session.ActionResume, Words: "carry on"}); err == nil || err.Error() != factory.ManagerResumeRefusal {
		t.Errorf("a resume the person did not ask for = %v", err)
	}
	_, manual := managedJob(t, st, func(it *factory.Item) {
		hasDraftPullRequest(it)
		it.Autonomy = factory.AutonomyManual
	})
	*sent = nil
	for _, action := range []string{session.ActionDeclineActs, session.ActionHandToPerson} {
		if _, err := door.Press(context.Background(), manual, session.ManagerPress{Action: action, Words: "not this way"}); err != nil {
			t.Errorf("%s in Manual with no person words = %v, want pressed", action, err)
		}
	}
	if len(*sent) != 2 || (*sent)[0].verb != "acts_decline" || (*sent)[1].verb != "manager_decide" {
		t.Errorf("sent %+v", *sent)
	}
	// A CONVERSATION THAT MANAGES NO JOB PRESSES NOTHING.
	*sent = nil
	_, err := door.Press(context.Background(), filepath.Join(t.TempDir(), "other.jsonl"), session.ManagerPress{Action: session.ActionTask, Words: "do it"})
	if !errors.Is(err, factory.ErrNotManager) || !strings.HasPrefix(err.Error(), "Nothing changed: ") || len(*sent) != 0 {
		t.Errorf("another conversation pressed: %v, sent %+v", err, *sent)
	}
}

// WHAT EACH PRESS SENDS, TO THE LETTER. The factory's handlers for these
// verbs are built in other changes, so the verb and the body are the whole
// contract: this test is the record of what the manager's tools send.
func TestTheManagersPressesSendTheWireContractsVerbsAndBodies(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, nil)
	door := runDoor(st, web, "").(session.ManagerPresser)
	id, talk := managedJob(t, st, func(it *factory.Item) {
		hasDraftPullRequest(it)
		it.Autonomy = factory.AutonomyManual
	})
	// THE PERSON SPOKE IN THIS TURN, AND THE ACTS STILL GO AS THE MANAGER'S:
	// `by` is never the person's through these tools.
	presses := []struct {
		press session.ManagerPress
		verb  string
		body  string
		says  string
	}{
		{session.ManagerPress{Action: session.ActionAct, Words: "the reviewer's note is addressed", Said: "answer it and push", Acts: []session.PublicAct{
			{Kind: session.ActReply, Thread: "inline:77", Text: "Renamed it.\n\nThanks."},
			{Kind: session.ActEditPR, Title: "Fix the ledger double count", Body: "Counts each refund once."},
			{Kind: session.ActChangeBase, Base: "release/2"},
			{Kind: session.ActRerequestReview, Reviewers: []string{"kim"}},
			{Kind: session.ActPush},
		}}, "act",
			`{"acts":[{"kind":"reply","thread":"inline:77","text":"Renamed it.\n\nThanks."},{"kind":"edit_pr","title":"Fix the ledger double count","body":"Counts each refund once."},{"kind":"change_base","base":"release/2"},{"kind":"rerequest_review","reviewers":["kim"]},{"kind":"push"}],"by":"main_agent","why":"the reviewer's note is addressed"}`,
			"Proposed: 4 public acts (reply, edit_pr, change_base, rerequest_review). This job is not in Auto, so nothing is sent until the person approves. Tell them exactly that, and show them the text that will be posted. They approve by saying yes here. " + pushUnread},
		{session.ManagerPress{Action: session.ActionTask, Words: "Rebase on main.\nRun the ledger tests.", Said: "rebase it"}, "task",
			`{"words":"Rebase on main.\nRun the ledger tests.","now":false,"by":"main_agent"}`, "It runs when the round in progress ends"},
		{session.ManagerPress{Action: session.ActionTask, Words: "Fix the typo.", Now: true}, "task",
			`{"words":"Fix the typo.","now":true,"by":"main_agent"}`, "It interrupts the round in progress and runs now."},
		{session.ManagerPress{Action: session.ActionAnotherRound, Step: "test", Words: "run the ledger tests first"}, "manager_decide",
			`{"decision":"another_round","step":"test","words":"run the ledger tests first"}`, "another round"},
		{session.ManagerPress{Action: session.ActionSendBack, Words: "the plan misses refunds"}, "manager_decide",
			`{"decision":"send_back","step":"","words":"the plan misses refunds"}`, "sent back"},
		{session.ManagerPress{Action: session.ActionRerun, Step: "test"}, "manager_decide",
			`{"decision":"rerun","step":"test","words":""}`, "queued to run again"},
		{session.ManagerPress{Action: session.ActionAccept, Step: "review", Words: "the finding is about code this job does not touch"}, "manager_decide",
			`{"decision":"accept","step":"review","words":"the finding is about code this job does not touch"}`, "Accepted as it stands"},
		{session.ManagerPress{Action: session.ActionResumePipeline, Words: "take the base in first"}, "manager_decide",
			`{"decision":"resume_pipeline","step":"","words":"take the base in first"}`, "Follow PR is queued"},
		{session.ManagerPress{Action: session.ActionRetryPublish}, "manager_decide",
			`{"decision":"retry_publish","step":"","words":""}`, "tried again"},
		{session.ManagerPress{Action: session.ActionHandToPerson, Words: "the fix needs a product decision"}, "manager_decide",
			`{"decision":"hand_to_person","step":"","words":"the fix needs a product decision"}`, "handed to the person"},
		{session.ManagerPress{Action: session.ActionWaitGitHub}, "manager_decide",
			`{"decision":"wait_github","step":"","words":""}`, "waiting on GitHub"},
		{session.ManagerPress{Action: session.ActionApproveActs, Words: "they said yes", Said: "yes, post them"}, "acts_approve",
			`{}`, "The proposed public acts are approved, as the person."},
		{session.ManagerPress{Action: session.ActionDeclineActs, Words: "not before the release"}, "acts_decline",
			`{"words":"not before the release"}`, "declined. Nothing was sent."},
	}
	for _, p := range presses {
		*sent = nil
		line, err := door.Press(context.Background(), talk, p.press)
		if err != nil || len(*sent) != 1 {
			t.Errorf("%s: %q (%v), sent %+v", p.press.Action, line, err, *sent)
			continue
		}
		if got := (*sent)[0]; got.job != id || got.verb != p.verb || got.body != p.body {
			t.Errorf("%s sent %s %s\nwant      %s %s", p.press.Action, got.verb, got.body, p.verb, p.body)
		}
		if !strings.Contains(line, p.says) {
			t.Errorf("%s answered %q, want it to say %q", p.press.Action, line, p.says)
		}
	}
	// IN AUTO THE SAME ACTS ARE ACCEPTED, NOT PROPOSED, and the line says so.
	_, auto := managedJob(t, st, func(it *factory.Item) {
		hasDraftPullRequest(it)
		it.Autonomy = factory.AutonomyAuto
	})
	line, err := door.Press(context.Background(), auto, session.ManagerPress{Action: session.ActionAct, Words: "ready", Acts: []session.PublicAct{{Kind: session.ActMarkReady}}})
	if err != nil || !strings.HasPrefix(line, "Accepted: 1 public act (mark_ready). This job is in Auto, so the factory carries this out now") {
		t.Errorf("an act in Auto = %q (%v)", line, err)
	}
	// AN UNKNOWN ACTION PRESSES NOTHING.
	*sent = nil
	if _, err := door.Press(context.Background(), talk, session.ManagerPress{Action: "force_merge"}); err == nil || len(*sent) != 0 {
		t.Errorf("an unknown action = %v, sent %+v", err, *sent)
	}
}

// A MERGE IS NEVER SENT AS AN ACT. `factory_act` with kind merge shows the
// person the Merge card again through the offer_merge door and merges
// nothing, in Manual and in Auto, with the person's words or without; the
// other acts of the call still go to the factory.
func TestAMergeActOnlyOffersTheMergeToThePerson(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, nil)
	door := runDoor(st, web, "").(session.ManagerPresser)
	for _, mode := range []string{factory.AutonomyManual, factory.AutonomyAuto} {
		for _, said := range []string{"", "merge it now"} {
			_, talk := managedJob(t, st, func(it *factory.Item) {
				hasMergeablePullRequest(it)
				it.Autonomy = mode
			})
			*sent = nil
			line, err := door.Press(context.Background(), talk, session.ManagerPress{Action: session.ActionAct, Words: "it is ready", Said: said, Acts: []session.PublicAct{
				{Kind: session.ActComment, Text: "Ready for another look."}, {Kind: session.ActMerge},
			}})
			if err != nil || !strings.Contains(line, "The Merge card for pull request #161 is shown to the person again") || !strings.Contains(line, "Nothing is merged") {
				t.Fatalf("%q said %q: %q (%v)", mode, said, line, err)
			}
			if len(*sent) != 2 || (*sent)[0].verb != "act" || (*sent)[1].verb != "offer_merge" ||
				(*sent)[0].body != `{"acts":[{"kind":"comment","text":"Ready for another look."}],"by":"main_agent","why":"it is ready"}` {
				t.Fatalf("%q said %q sent %+v", mode, said, *sent)
			}
		}
	}
	// A MERGE THE GATE DOES NOT OFFER IS REFUSED IN THE GATE'S WORDS, and a
	// merge alone sends no act at all.
	_, draft := managedJob(t, st, func(it *factory.Item) {
		hasDraftPullRequest(it)
		it.Autonomy = factory.AutonomyAuto
	})
	*sent = nil
	if _, err := door.Press(context.Background(), draft, session.ManagerPress{Action: session.ActionAct, Words: "merge", Acts: []session.PublicAct{{Kind: session.ActMerge}}}); err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: merge is not offered: ") || len(*sent) != 0 {
		t.Fatalf("a merge act on a draft = %v, sent %+v", err, *sent)
	}
	// With another act beside it, the act goes and the line says the merge
	// was not offered.
	line, err := door.Press(context.Background(), draft, session.ManagerPress{Action: session.ActionAct, Words: "ready", Acts: []session.PublicAct{{Kind: session.ActMarkReady}, {Kind: session.ActMerge}}})
	if err != nil || len(*sent) != 1 || (*sent)[0].verb != "act" || !strings.Contains(line, "The merge was not offered to the person. Nothing changed: merge is not offered: ") {
		t.Fatalf("a set with a merge the gate refuses = %q (%v), sent %+v", line, err, *sent)
	}
}

// A REFUSAL IS THE FACTORY'S OWN WORDS, said as nothing changed; a window
// with no factory connection says so and presses nothing.
func TestAManagerPressTheFactoryRefusesSaysItsWords(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, func(string) error { return errors.New("the job's budget is reached") })
	door := runDoor(st, web, "").(session.ManagerPresser)
	_, talk := managedJob(t, st, func(it *factory.Item) {
		hasDraftPullRequest(it)
		it.Autonomy = factory.AutonomyAuto
	})
	for _, p := range []session.ManagerPress{
		{Action: session.ActionAct, Words: "x", Acts: []session.PublicAct{{Kind: session.ActPush}}},
		{Action: session.ActionTask, Words: "do it"},
		{Action: session.ActionRerun, Step: "test"},
		{Action: session.ActionApproveActs},
		{Action: session.ActionDeclineActs},
	} {
		if _, err := door.Press(context.Background(), talk, p); err == nil || err.Error() != "Nothing changed: the job's budget is reached" {
			t.Errorf("%s refused = %v", p.Action, err)
		}
	}
	if len(*sent) != 5 {
		t.Fatalf("sent %d presses", len(*sent))
	}
	appLocalPress.Store(nil)
	if _, err := door.Press(context.Background(), talk, session.ManagerPress{Action: session.ActionTask, Words: "do it"}); !errors.Is(err, errNoPress) {
		t.Errorf("no connection = %v", err)
	}
}

// AN INBOX TURN ANSWERS ONE QUESTION AND PRESSES NOTHING. Its door is not a
// presser, so a conversation opened for the turn carries no `factory_act` or
// `factory_task`, and a window that already carries them refuses in words
// (internal/session's TestThePressingToolsRefuseOnATurnWhoseLentDoorCannotPress).
func TestTheInboxTurnsDoorPressesNothing(t *testing.T) {
	door := session.RunDoor(&inboxDoor{stage: "plan"})
	if _, ok := door.(session.ManagerPresser); ok {
		t.Fatal("the inbox turn's door can press")
	}
	if _, ok := door.(session.InboxDoor); !ok {
		t.Fatal("the inbox turn's door does not take the reply")
	}
}

// THE BRIEF SAYS THE AUTHORITY TABLE in place of "never send one work, stop
// it or start one yourself": what the manager may settle by itself, that a
// public act follows the job's mode, and that it never merges.
func TestTheManagersBriefSaysWhatItMayDoAndWhatGoesPublic(t *testing.T) {
	if strings.Contains(talkBlockDo, "never send one work") {
		t.Error("the brief still forbids the manager from sending work")
	}
	for _, want := range []string{"you are the first to hear of every stop", "settle what you can and hand the rest to the person with one plain sentence",
		"another round", "send work back", "run a step again", "accept a check's finding with a reason", "factory_task", "factory_act"} {
		if !strings.Contains(talkBlockDo, want) {
			t.Errorf("the brief's Do block lacks %q", want)
		}
	}
	for _, want := range []string{"follows the job's mode, which factory_status says", "In Auto you carry it out and the factory logs it",
		"In Manual you show the person exactly what will go public and wait for their yes; nothing is sent before it", "You never merge", "marked untrusted", "only with the person's word"} {
		if !strings.Contains(talkBlockPublic, want) {
			t.Errorf("the brief's block on public acts lacks %q", want)
		}
	}
	found := false
	for _, block := range talkBlocks(factory.Item{ID: 1, Repo: "web"}) {
		found = found || block == talkBlockPublic
	}
	if !found {
		t.Error("the brief does not carry the block on public acts")
	}
	// THE STALL TURN IS TOLD THE SAME, because a job keeps the brief it was
	// made with.
	for _, want := range []string{"factory_task", "factory_act", "outside Auto they wait for the person's yes", "Never merge", "give a step another round"} {
		if !strings.Contains(stallAskTail, want) {
			t.Errorf("the stall report's tail lacks %q", want)
		}
	}
	for _, name := range []string{"factory_act", "factory_task", "factory_answer"} {
		if v3BuiltinApprovals()[name] != "allow" {
			t.Errorf("a turn nobody is at the keyboard of would stop to ask before %s", name)
		}
	}
}

// THE PERSON'S YES IS FOR WHAT THEY HAVE READ. Outside Auto the manager may
// not approve, on the person's words, acts it proposed after those words were
// said: it would be proposing a reply and approving it in one breath.
func TestTheManagerMayNotApproveActsItProposedInTheSameTurn(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, nil)
	door, ok := runDoor(st, web, "").(session.ManagerPresser)
	if !ok {
		t.Fatal("the floor's run door cannot press")
	}
	began := time.Now()
	for _, c := range []struct {
		name     string
		mode     string
		proposed time.Time
		pressed  bool
	}{
		{"Manual, proposed in this turn", factory.AutonomyManual, began.Add(time.Second), false},
		{"Manual, proposed before the person spoke", factory.AutonomyManual, began.Add(-time.Minute), true},
		{"Auto, proposed in this turn", factory.AutonomyAuto, began.Add(time.Second), true},
	} {
		id, talk := managedJob(t, st, func(it *factory.Item) {
			hasDraftPullRequest(it)
			it.Autonomy = c.mode
			it.Acts = []factory.PublicAct{{ID: "a1", Kind: factory.ActKindComment, Text: "Rebased on dev.", By: factory.ActByMainAgent, State: factory.ActProposed, At: c.proposed}}
		})
		*sent = nil
		_, err := door.Press(context.Background(), talk, session.ManagerPress{Action: session.ActionApproveActs, Said: "yes, post it", TurnAt: began})
		switch {
		case c.pressed && (err != nil || len(*sent) != 1 || (*sent)[0].verb != "acts_approve" || (*sent)[0].job != id):
			t.Errorf("%s: %v, sent %+v; want acts_approve pressed", c.name, err, *sent)
		case !c.pressed && (err == nil || err.Error() != managerSameTurnRefusal || len(*sent) != 0):
			t.Errorf("%s: %v, sent %+v; want the same-turn refusal and nothing sent", c.name, err, *sent)
		}
	}
}

// THE PUBLISH APPROVAL IS THE SAME YES AS APPROVING THE ACTS THAT WAIT BESIDE
// THE PUSH (serve's approveActsWithPush), so outside Auto the manager may not
// press it in a turn where it proposed an act: the reply would go out with
// the push, and the person never read it. The act stays proposed and the
// refusal tells the manager what to do instead; an act proposed before the
// person spoke can be approved with it.
func TestTheManagerMayNotApprovePublicationWhileItsActsAreUnreadFromTheSameTurn(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, nil)
	door, ok := runDoor(st, web, "").(session.ManagerPresser)
	if !ok {
		t.Fatal("the floor's run door cannot press")
	}
	began := time.Now()
	for _, c := range []struct {
		name     string
		mode     string
		proposed time.Time // zero: no act waits
		pressed  bool
	}{
		{"Manual, reply proposed in this turn", factory.AutonomyManual, began.Add(time.Second), false},
		{"Manual, reply proposed before the person spoke", factory.AutonomyManual, began.Add(-time.Minute), true},
		{"Manual, nothing proposed", factory.AutonomyManual, time.Time{}, true},
		{"Auto, reply proposed in this turn", factory.AutonomyAuto, began.Add(time.Second), true},
	} {
		id, talk := managedJob(t, st, func(it *factory.Item) {
			waitsOnPublishApproval(it)
			it.Autonomy = c.mode
			if !c.proposed.IsZero() {
				it.Acts = []factory.PublicAct{{ID: "a1", Kind: factory.ActKindReply, Text: "Fixed in the new commit.", By: factory.ActByMainAgent, State: factory.ActProposed, At: c.proposed}}
			}
		})
		*sent = nil
		_, err := door.Press(context.Background(), talk, session.ManagerPress{Action: session.ActionApprove, Words: "they said to publish", Said: "yes, publish it", TurnAt: began})
		switch {
		case c.pressed && (err != nil || len(*sent) != 1 || (*sent)[0].verb != "approve" || (*sent)[0].job != id):
			t.Errorf("%s: %v, sent %+v; want approve pressed", c.name, err, *sent)
		case !c.pressed && (err == nil || err.Error() != managerSameTurnRefusal || len(*sent) != 0):
			t.Errorf("%s: %v, sent %+v; want the same-turn refusal and nothing sent", c.name, err, *sent)
		}
		if got, err := st.Get(id); err != nil {
			t.Fatal(err)
		} else if !c.proposed.IsZero() && (len(got.Acts) != 1 || got.Acts[0].State != factory.ActProposed) {
			t.Errorf("%s: the act is %+v, want it still proposed", c.name, got.Acts)
		}
	}
}

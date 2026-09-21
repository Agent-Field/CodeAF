package session

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE BLOCK FOLLOWS THE WORDS OF THE MESSAGE, not the workspace: two skills on
// one shelf, and the one whose subject is the question is the one the turn
// carries — the catalog's window, which scores both the same against a path
// that never changes, could not have told them apart.
func TestTheBlockFollowsTheMessageText(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "tool:lint", "checks the lint rules for this repo", "/shelf/lint")
	activeSkill(t, brain, "domain:parties", "plans the office party rotation", "/shelf/party")

	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("run the lint check"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
	})

	events, err := agent.Submit(context.Background(), "how should I lint this repo?")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	notice := drainSkillsNotice(t, events)
	sent := userTextIn(completer.request(0))
	if !strings.Contains(sent, "checks the lint rules for this repo") {
		t.Fatalf("the message the model read does not carry the lint skill:\n%s", sent)
	}
	if strings.Contains(sent, "office party") {
		t.Fatalf("a skill with nothing to do with the message rode along:\n%s", sent)
	}
	if notice == "" {
		t.Fatal("the turn said nothing about the skill it carried")
	}
	if !strings.Contains(notice, "lint") {
		t.Fatalf("the carried-skills notice names the wrong skills: %q", notice)
	}
}

// AN ATTACHED SKILL IS NEVER SCORED AWAY AND NEVER WINDOWED: six of them is
// more than the turn's own bound, and all six ride, because the person asked.
func TestAttachedSkillsRideInFullPastTheBound(t *testing.T) {
	brain := openTestBrain(t)
	for index := 0; index < skillTurnMax+2; index++ {
		activeSkill(t, brain, "domain:extra", "hand-attached number "+string(rune('0'+index)), "/shelf/hand-"+string(rune('0'+index)))
	}
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
	})
	for index := 0; index < skillTurnMax+2; index++ {
		agent.AttachSkills("hand-" + string(rune('0'+index)))
	}

	events, _ := agent.Submit(context.Background(), "an ordinary question with no skill words in it")
	notice := drainSkillsNotice(t, events)
	sent := userTextIn(completer.request(0))
	for index := 0; index < skillTurnMax+2; index++ {
		name := "hand-" + string(rune('0'+index))
		if !strings.Contains(sent, name) {
			t.Fatalf("the attached skill %s was windowed away:\n%s", name, sent)
		}
		if !strings.Contains(notice, name) {
			t.Fatalf("the notice does not name the attached skill %s: %q", name, notice)
		}
	}
}

// RETRIEVAL FILLS THE ROOM THE ATTACHMENTS LEAVE, and the room is the named
// bound: three attachments leave one seat, and a shelf full of candidates may
// take it and no more.
func TestRetrievalFillsTheRoomTheAttachmentsLeave(t *testing.T) {
	brain := openTestBrain(t)
	for index := 0; index < skillTurnMax-1; index++ {
		activeSkill(t, brain, "domain:hand", "the hand-attached one", "/shelf/held-"+string(rune('0'+index)))
	}
	for index := 0; index < skillTurnMax+3; index++ {
		activeSkill(t, brain, "domain:candid", "a retrievable candidate for the lint job", "/shelf/cand-"+string(rune('0'+index)))
	}
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
	})
	for index := 0; index < skillTurnMax-1; index++ {
		agent.AttachSkills("held-" + string(rune('0'+index)))
	}

	events, _ := agent.Submit(context.Background(), "please lint the candidates")
	notice := drainSkillsNotice(t, events)
	sent := userTextIn(completer.request(0))
	carried := strings.Count(sent, "a retrievable candidate for the lint job")
	if carried > 1 {
		t.Fatalf("retrieval took %d seats beside %d attachments, wanted at most 1:\n%s", carried, skillTurnMax-1, sent)
	}
	if carried == 1 && !strings.Contains(notice, "cand-") {
		t.Fatalf("the retrieved seat was carried but not named: %q", notice)
	}
}

// A NAME THE PERSON TYPED IS PINNED like an attachment, which is the task
// road's own rule: naming a skill is the strongest relevance signal there is.
func TestANameTypedInTheMessageIsPinned(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "domain:release", "walks the release checklist", "/shelf/release")

	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
	})

	events, _ := agent.Submit(context.Background(), "cut a release now")
	notice := drainSkillsNotice(t, events)
	if !strings.Contains(userTextIn(completer.request(0)), "walks the release checklist") {
		t.Fatalf("a skill named outright in the message was not carried:\n%s", userTextIn(completer.request(0)))
	}
	if !strings.Contains(notice, "release") {
		t.Fatalf("the notice does not name the pinned skill: %q", notice)
	}
}

// ZERO SKILLS IS ZERO BYTES: a shelf-less conversation sends exactly what the
// person typed and reports nothing, which is byte-for-byte the shape before
// this file existed.
func TestAnEmptyShelfChangesNoByte(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	words := "just an ordinary question"
	events, _ := agent.Submit(context.Background(), words)
	if notice := drainSkillsNotice(t, events); notice != "" {
		t.Fatalf("a turn with no skills reported one: %q", notice)
	}
	for _, message := range completer.request(0) {
		if message.Role == "user" && messageContentText(message) != words {
			t.Fatalf("the message the model read was touched:\n%q", messageContentText(message))
		}
	}
}

// THE JOURNAL KEEPS THE PERSON'S WORDS. The block rides the copy the model
// reads; the record keeps what was typed, which is the [userMessage.said]
// shape a marked standing draft already uses.
func TestTheRecordKeepsWhatThePersonTyped(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "tool:lint", "checks the lint rules", "/shelf/lint")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Memory = brain
	})

	words := "how should I lint this repo?"
	user := userText(words)
	agent.mu.Lock()
	agent.attachTurnSkillsLocked(&user)
	agent.mu.Unlock()
	if user.said != words {
		t.Fatalf("the record's copy is %q, want the person's own words", user.said)
	}
	if !strings.Contains(messageContentText(user.message), "Skills suited to this message:") {
		t.Fatalf("the model's copy does not carry the block:\n%s", messageContentText(user.message))
	}
	if strings.Contains(user.said, "Skills suited") {
		t.Fatal("the block leaked into the person's own words")
	}
}

// drainSkillsNotice reads one turn's events to the close and returns the
// carried-skills notice, if any — the dim [EventNotice] line this feature
// reports through.
func drainSkillsNotice(t *testing.T, events <-chan Event) string {
	t.Helper()
	notice := ""
	for event := range events {
		if event.Kind == EventNotice && strings.HasPrefix(event.Text, "skills carried: ") {
			if notice != "" {
				t.Fatalf("the turn reported its skills twice: %q then %q", notice, event.Text)
			}
			notice = event.Text
		}
		if event.Kind == EventError {
			t.Fatalf("the turn errored: %v", event.Err)
		}
	}
	return notice
}

// THE NAMES ARE A FIELD AND NOT A SENTENCE. A surface that wanted to draw which
// skills a turn carried could take them back out of the notice's words, and that
// reading would break the first time somebody improved the wording or a skill
// name held a comma — silently, because a test written against the same sentence
// agrees with it. So the notice carries both and one function builds it.
func TestTheSkillsNoticeCarriesItsNamesAsAField(t *testing.T) {
	names := []string{"release-notes", "lint, with a comma"}
	notice := turnSkillsNotice(names)
	if notice.Kind != EventNotice {
		t.Fatalf("the notice is not a notice: %v", notice.Kind)
	}
	if !reflect.DeepEqual(notice.Skills, names) {
		t.Fatalf("the field lost the names: %v", notice.Skills)
	}
	notice.Skills[0] = "rewritten"
	if names[0] != "release-notes" {
		t.Fatal("the notice shares the caller's slice")
	}
	if !strings.Contains(turnSkillsNotice(names).Text, "release-notes") {
		t.Fatal("the sentence stopped naming the skills it carried")
	}
}

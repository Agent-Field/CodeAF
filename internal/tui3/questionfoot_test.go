package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A WAITING QUESTION DOES NOT TAKE THE KEYS OFF THE PLACE A PERSON IS STANDING
// ON (#840).
//
// The tasks place's foot names what `enter` does on the row under the cursor.
// A task whose run raised a question wrote `<head> · waiting in this
// conversation · alt+a` over that foot, and the row stopped saying it had a door
// — the e2e subtest that presses `enter` and `space` on the row waited thirty
// seconds for `enter open its room` and watched the chip stand where it should
// have been. Both facts are true at once, so the foot draws both: the keys where
// they always are, the sentence at the right edge.
//
// It is a STUB and not a model: the issue's own replication asks for one, because
// whether the model raises a question before the row is inspected is a race
// nobody can stage from outside.
func TestAWaitingQuestionLeavesTheTaskRowsDoorHintOnTheFoot(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 40
	a.raisePlace(pageTasks)

	// The control: nothing waiting, and the foot names the place's own keys.
	width, height := a.size()
	keys := plain(a.placeHint())
	if strings.TrimSpace(keys) == "" {
		t.Fatal("the tasks place names no keys at all on its foot")
	}
	lines, _, _, _, _ := a.placeFrameNow(width, height)
	if foot := plain(strings.Join(lines, "\n")); !strings.Contains(foot, keys) {
		t.Fatalf("the tasks place does not draw its own keys:\n%s", foot)
	}

	// And now the task raises something for the person, exactly as the delivery
	// road does when the question lands on another screen.
	a.sayWhereQuestionWent(questionWaitingLine(session.Question{
		ID: 7, Kind: session.QuestionAsk, Ask: session.AskChoice, Head: "hello.txt",
	}))
	lines, _, _, _, _ = a.placeFrameNow(width, height)
	foot := plain(strings.Join(lines, "\n"))
	if !strings.Contains(foot, questionWaitingWord) {
		t.Fatalf("the waiting question is not said anywhere on the place:\n%s", foot)
	}
	if !strings.Contains(foot, keys) {
		t.Fatalf("the waiting question displaced the place's own keys:\n%s", foot)
	}
}

// AND THE CHIP CARRIES THE QUESTION'S OWN WORDS wherever a person is standing,
// which is the other half of the same promise: the status row says WHICH
// decision is parked, not merely that one is.
func TestTheChipSaysWhichQuestionIsWaiting(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 91, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which storage for the session index?",
		Reason: "three ways work", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "SQLite"}, {Key: "2", Label: "JSONL"}},
	})
	seg := plain(lab.a.questionSegment())
	if !strings.Contains(seg, "Which storage") || !strings.Contains(seg, questionChipKey) {
		t.Fatalf("the chip does not carry the question's words: %q", seg)
	}
	// A RATIFY LINE IS NEVER COUNTED. Nothing waits on it, so a chip that counted
	// it would put a number on the status row that no key can clear.
	lab.fromLane(session.Question{
		ID: 92, Kind: session.QuestionAsk, Ask: session.AskRatify,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "renamed 12 files under src/",
		Stakes: session.StakesReversible,
	})
	if seg = plain(lab.a.questionSegment()); strings.Contains(seg, "2 questions") {
		t.Fatalf("the chip counted a line nothing is waiting on: %q", seg)
	}
	if !strings.Contains(seg, "Which storage") {
		t.Fatalf("the chip stopped naming the question that IS waiting: %q", seg)
	}
}

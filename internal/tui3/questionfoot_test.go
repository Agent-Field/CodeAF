package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE CHIP CARRIES THE QUESTION'S OWN WORDS wherever a person is standing: the
// status row says WHICH decision is parked, not merely that one is.
//
// (The other half of #840 — that a waiting question does not displace the place's
// own door hint — landed on dev as #945, which composes both into
// [app.placeMsgLine] through [hintFitBeside]. Its test is placemsgline_test.go.)
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

// The old question key now belongs to approvals, including while home has a
// pending question behind it. Only the new key brings that question forward.
func TestAltYReopensQuestionsAndAltABelongsToApprovals(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.a.showPage(pageHome)
	if _, taken := lab.a.questionChipKeyPress(key("alt+a")); taken {
		t.Fatal("the pending question intercepted the approvals key")
	}
	drive(t, lab.a, key("alt+y"))
	if lab.a.at(pageHome) {
		t.Fatal("alt+y did not bring the pending question back from home")
	}
}

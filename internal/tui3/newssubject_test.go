package tui3

// A NEWS ITEM BELONGS TO A SUBJECT, AND A WINDOW DRAWS ITS OWN SUBJECT'S NEWS.
//
// THE DEFECT THESE TESTS HOLD SHUT, measured on 2026-09-10. Both news desks
// were keyed by MODEL, which is an address and not an identity, so:
//
//   - two tasks running on one model overwrote each other's clock, and each
//     room drew whichever of them had spoken last;
//   - a node's phase could never be drawn at all, because the conversation's
//     reading dropped every role that was not [lane.RoleTalk] and there was no
//     other reading;
//   - and every rate on the frame was gated on the CONVERSATION's liveness, so
//     a room showed none whenever the conversation that launched the work was
//     idle — which is nearly always, because handing a task out ends the turn.
//
// What a person saw inside a task room, with the node writing: no provider, no
// tok/s, and a stale `running ask · 4m 55s` left over from the conversation.
//
// The fix is a SUBJECT on the news and one key function ([newsDeskKey]) — never
// a heuristic about rooms being open, which would put two tasks on one model
// back on each other's row under a new name.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// subjectApp is a room open on node 9, launched on a model of its own, from a
// conversation that is IDLE — the shape of the bug. The desks are emptied for
// each test, because a desk is process-global and a test must not inherit
// another one's turn.
//
// The conversation's own model is the fixture's (`deepseek/deepseek-v4-flash`),
// and every test below states which model it means rather than assuming.
func subjectApp(t *testing.T, model string) *app {
	t.Helper()
	t.Cleanup(forgetPhases)
	t.Cleanup(forgetLanes)
	a, _ := roomModelApp(t, model)
	if a.state == stateWorking {
		t.Fatal("the fixture's conversation is working: the defect is about an idle one")
	}
	return a
}

// nodeSubject is the name the ENGINE would mint for one of this window's nodes,
// asked for in the engine's own spelling ([session.NewsSubject]) exactly as the
// surface asks for it.
func nodeSubject(a *app, id uint64) string {
	return session.NewsSubject(a.taskSheetSelfID(), id)
}

// TWO PIECES OF WORK ON ONE MODEL DO NOT OVERWRITE EACH OTHER. This is the
// keying law by itself, at the door every phase comes through: the model is an
// address that two nodes share, and the subject is the identity that tells them
// apart.
func TestTwoSubjectsOnOneModelKeepTheirOwnNews(t *testing.T) {
	t.Cleanup(forgetPhases)
	t.Cleanup(forgetLanes)
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

	for _, c := range []struct{ subject, detail string }{
		{"conv-one#7", "go test"},
		{"conv-one#9", "gh pr create"},
	} {
		PostPhaseNews(PhaseNews{
			Phase: provider.PhaseRunning, Detail: c.detail, Since: now.Add(-8 * time.Second),
			Model: phaseModel, Role: lane.RoleLeafAttached, Subject: c.subject, At: now,
		})
		PostLaneNews(LaneNews{
			Model: phaseModel, Lane: c.detail, Role: lane.RoleLeafAttached,
			Subject: c.subject, TTFT: 600 * time.Millisecond, At: now,
		})
	}

	for _, c := range []struct{ subject, detail string }{
		{"conv-one#7", "go test"},
		{"conv-one#9", "gh pr create"},
	} {
		news, ok := phaseNewsFor(c.subject)
		if !ok {
			t.Fatalf("%s has no phase of its own: one model, one row again", c.subject)
		}
		if news.Detail != c.detail {
			t.Fatalf("%s is running %q, want %q — the newer node took its row",
				c.subject, news.Detail, c.detail)
		}
		lanes, ok := laneNewsFor(c.subject)
		if !ok || lanes.Lane != c.detail {
			t.Fatalf("%s's served news is %+v", c.subject, lanes)
		}
	}

	// AND A SUBJECT NOBODY SAID IS STILL FILED UNDER ITS MODEL, which is the
	// whole compatibility bargain: every producer that predates the field, and
	// every older wire peer, sends nothing here and must behave exactly as it
	// always did.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-time.Second),
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	news, ok := phaseNewsFor(phaseModel)
	if !ok || news.Phase != provider.PhaseWriting {
		t.Fatalf("a phase with no subject did not land on its model: %+v / %v", news, ok)
	}
	// And it took neither node's row with it.
	if news, _ := phaseNewsFor("conv-one#7"); news.Detail != "go test" {
		t.Fatalf("the conversation's own phase overwrote a node's: %+v", news)
	}
}

// A ROOM DRAWS ITS NODE'S RATE WHILE THE CONVERSATION IS IDLE. This is the
// symptom a person reported: the node was writing and the right edge was empty,
// because the gate on it read the session's state.
func TestARoomDrawsItsNodesRateWhileTheConversationIsIdle(t *testing.T) {
	a := subjectApp(t, "z-ai/glm-5.2")
	now := a.now()

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 38,
		Model: "z-ai/glm-5.2", Role: lane.RoleLeafAttached, Subject: nodeSubject(a, 9), At: now,
	})

	if got := a.liveRiderAt(-1); got != "38 tok/s" {
		t.Fatalf("the room's right edge reads %q, want the node's own rate", got)
	}
	if line := statusText(a); !strings.Contains(line, "38 tok/s") {
		t.Fatalf("the room's status line carries no rate while its node writes:\n%q", line)
	}
	// AND THE ROOM SAYS WHICH MACHINE IS ANSWERING, beside the model it is
	// answering for — the attribution that was impossible while the desk was
	// keyed by model (lanes.go's [app.roomLaneRider]).
	PostLaneNews(LaneNews{
		Model: "z-ai/glm-5.2", Lane: "Friendli", Role: lane.RoleLeafAttached,
		Subject: nodeSubject(a, 9), TTFT: 600 * time.Millisecond, Rate: 38, At: now,
	})
	line := statusText(a)
	if !strings.Contains(line, roomModelLead+"glm-5.2 · via friendli") {
		t.Fatalf("the room's identity cluster does not name the node's machine:\n%q", line)
	}
	// The figures stay at the right edge and are not said twice: `via friendli ·
	// 0.6s · 38 t/s` beside the model is the LAST answer's average, which is the
	// reading the seam gave up on 2026-09-10.
	if strings.Contains(line, "via friendli · 0.6s") {
		t.Fatalf("the room's attribution grew the sighting's own figures:\n%q", line)
	}
	if strings.Contains(line, "38 t/s") {
		t.Fatalf("the room quotes a second rate beside the model:\n%q", line)
	}

	// AND NEWS THAT HAS AGED OUT DRAWS NOTHING, which is what makes the room's
	// own liveness a real answer rather than an optimistic one: a phase that is
	// still true says itself again several times inside [phaseWindow].
	a.clock = func() time.Time { return now.Add(phaseWindow + time.Second) }
	if got := a.liveRiderAt(-1); got != "" {
		t.Fatalf("a room draws %q off news that has gone stale", got)
	}
}

// `RUNNING <TOOL>` INSIDE A ROOM IS THE NODE'S CALL AND NEVER THE
// CONVERSATION'S. Both are on the desks at once here, which is the frame the
// defect was read on.
func TestARoomsPhaseIsItsNodesAndNotTheConversations(t *testing.T) {
	a := subjectApp(t, "z-ai/glm-5.2")
	now := a.now()

	// The conversation's own last phase, still inside the window.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseRunning, Detail: "ask", Since: now.Add(-9 * time.Second),
		Model: a.model, Role: lane.RoleTalk, At: now,
	})
	// And the node's, which is what the person standing in the room is watching.
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseRunning, Detail: "go test", Since: now.Add(-41 * time.Second),
		Model: "z-ai/glm-5.2", Role: lane.RoleLeafAttached, Subject: nodeSubject(a, 9), At: now,
	})

	line := statusText(a)
	if !strings.Contains(line, "running go test") {
		t.Fatalf("the room does not say what its node is running:\n%q", line)
	}
	if strings.Contains(line, "running ask") {
		t.Fatalf("the room draws the conversation's own call:\n%q", line)
	}

	// A NODE WITH NOTHING ON THE DESK DRAWS NOTHING, rather than borrowing the
	// conversation's clock — which is the same lie the other way round.
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	if got := a.liveRiderAt(-1); got != "" {
		t.Fatalf("a room over a node with no news reads %q", got)
	}
	if line := statusText(a); strings.Contains(line, "running ask") {
		t.Fatalf("a silent node's room fell back to the conversation's call:\n%q", line)
	}
}

// AND CLOSING THE ROOM GIVES THE CONVERSATION ITS OWN LINE BACK, whole. The
// conversation's reading is untouched by all of this: it asks for no subject,
// which files it under its model exactly as it always was, and it still refuses
// every role but the talk one.
func TestClosingTheRoomRestoresTheConversationsOwnLine(t *testing.T) {
	a := subjectApp(t, "z-ai/glm-5.2")
	now := a.now()
	a.state = stateWorking
	answerArriving(a)

	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Quicksilver", Rate: 61,
		Model: a.model, Role: lane.RoleTalk, At: now,
	})
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseRunning, Detail: "go test", Since: now.Add(-41 * time.Second),
		Model: "z-ai/glm-5.2", Role: lane.RoleLeafAttached, Subject: nodeSubject(a, 9), At: now,
	})

	if line := statusText(a); !strings.Contains(line, "running go test") {
		t.Fatalf("the room is not drawing its own node:\n%q", line)
	}

	a.closeRoom()
	a.touch()
	if got := a.liveRiderAt(-1); got != "61 tok/s" {
		t.Fatalf("the closed room left the conversation reading %q, want its own rate", got)
	}
	line := statusText(a)
	if strings.Contains(line, "go test") {
		t.Fatalf("the conversation's line still carries the node's call:\n%q", line)
	}
	if strings.Contains(line, roomModelLead) {
		t.Fatalf("the closed room's model is still on the line:\n%q", line)
	}

	// AND THE NODE'S PHASE NEVER REACHED THIS ROW EVEN WHILE THE ROOM WAS SHUT,
	// which is the role test surviving the subject key rather than being
	// replaced by it: take the conversation's own news away and the row is
	// empty, not the node's.
	PostPhaseNews(PhaseNews{Model: a.model, Role: lane.RoleTalk, At: now})
	if got := a.liveRiderAt(-1); got != "" {
		t.Fatalf("with the conversation's phase cleared the row reads %q", got)
	}
}

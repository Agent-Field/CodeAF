package tui3

// WHY `via <provider>` AND tok/s WENT MISSING, one test per cause.
//
// The owner, 2026-09-10: "at some times I don't see infra provider and tok/sec,
// I am not sure when it does not show up, maybe when I have follow up or
// something". It was not one bug with a random trigger; it was six, each with a
// trigger a person would never connect to the gap:
//
//  1. any model with a reasoning level set lost the live rate, because the
//     status row lent the model field `id:level` for its draw and the rate is
//     looked up by that field;
//  2. `via` vanished whenever the vendor served its own model (`deepseek/…` by
//     DeepSeek), because one layer still kept a suppression the owner had
//     ruled away and the fallback under it is empty over an engine host;
//  3. a rescue that failed blanked `via`, and a rescue promised by a turn that
//     was then interrupted stayed promised for ten minutes;
//  4. `via` waited for a whole step to finish, so the first answer — and any
//     follow-up after ten quiet minutes — showed none while it was written;
//  5. the conversation's news was filed by model id, which drifts between the
//     engine and the window (a mid-turn pick, a fallback hop, another window);
//  6. an engine host from before the news crossed the wire sent none, and
//     nothing said so.
//
// Every test draws through the real row a person reads — the seam
// ([app.legend]), the status row ([app.statusRow]) or a room's line.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// newsApp is a conversation at rest on one model with a pinned clock and empty
// desks, over an engine host: the ledger fallback is empty in this process,
// because it is only ever filled in the engine's.
func newsApp(t *testing.T, model string) (*app, time.Time) {
	t.Helper()
	t.Cleanup(forgetPhases)
	t.Cleanup(forgetLanes)
	forgetPhases()
	forgetLanes()
	pinSighting(t, provider.Sighting{}, false)
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	a := newTestApp(&fakeAgent{model: model})
	a.model = model
	a.clock = func() time.Time { return now }
	a.width, a.height = 200, 24
	return a, now
}

// seamText is the seam as a reader sees it.
func seamText(a *app) string { return plain(a.legend(160)) }

// statusRowText is the status row as a reader sees it, through the frame's own door.
func statusRowText(a *app, width int) string { return plain(strings.Join(a.statusRow(width), "\n")) }

// ── 1. a reasoning level ────────────────────────────────────────────────────

// A LEVEL DIALLED ON THE MODEL NO LONGER TAKES THE RATE OFF THE ROW. The row
// used to lend the model field `kimi-k3:high` for the length of its draw, and
// the live rate is asked for by that field — so it asked for a name nothing is
// filed under and drew nothing, on every model with a level, local or hosted.
func TestAReasoningLevelNoLongerHidesTheLiveRate(t *testing.T) {
	a, now := newsApp(t, phaseModel)
	a.agent.(*fakeAgent).levels = map[string]string{phaseModel: "high"}
	settleLevels(a, phaseModel)
	if a.reasoningFor(phaseModel) != "high" {
		t.Fatal("the fixture has no level dialled")
	}
	a.state = stateWorking
	answerArriving(a)
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-4 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})

	if line := statusRowText(a, 200); !strings.Contains(line, "38 tok/s") {
		t.Fatalf("a model with a level dialled lost the live rate:\n%q", line)
	}
	if !strings.Contains(seamText(a), "kimi-k3 · via friendli") {
		t.Fatalf("a model with a level dialled lost its machine:\n%q", seamText(a))
	}
	// AND THE PHONE DECK STILL SPELLS THE LEVEL ON ITS CHIP, which is what the
	// lend was for — asked for as a word now, never lent through the field.
	if deck := statusRowText(a, 44); !strings.Contains(deck, "kimi-k3:high") {
		t.Fatalf("the phone deck lost the level on its chip:\n%q", deck)
	}
	if a.model != phaseModel {
		t.Fatalf("drawing the row rewrote the model to %q", a.model)
	}
}

// ── 2. the vendor serving its own model ─────────────────────────────────────

// `via` IS ON THE SEAM WHOEVER SERVED, which is the owner's ruling of
// 2026-09-09. The seam spells the model as its basename, so the vendor is not on
// the screen; the lane layer's rider still went quiet when the machine's name
// was inside the id, and over an engine host nothing under it had anything to
// say — so `deepseek/…` answered by DeepSeek drew no provider at all.
func TestTheSeamSaysViaWhenTheVendorServesItsOwnModel(t *testing.T) {
	a, now := newsApp(t, flash)
	PostLaneNews(LaneNews{Model: flash, Lane: "DeepSeek", Role: lane.RoleTalk, At: now})

	if seam := seamText(a); !strings.Contains(seam, "deepseek-v4-flash · via deepseek") {
		t.Fatalf("the vendor serving its own model took `via` off the seam:\n%q", seam)
	}
	// THE SHEET KEEPS THE OLD RULE, because the row above its `served` line is
	// the model's whole address and already says `deepseek`.
	if got := a.servedRider(); got != "" {
		t.Fatalf("the sheet names a machine its address already names: %q", got)
	}
}

// AND A ROOM SAYS IT TOO, for the same reason: its chip is `task glm-5.2`, and
// the vendor half of the node's id is not on the screen either.
func TestARoomSaysViaWhenTheVendorServesItsOwnModel(t *testing.T) {
	a := subjectApp(t, "z-ai/glm-5.2")
	PostLaneNews(LaneNews{
		Model: "z-ai/glm-5.2", Lane: "Z-AI", Role: lane.RoleLeafAttached,
		Subject: nodeSubject(a, 9), At: a.now(),
	})
	if line := statusText(a); !strings.Contains(line, roomModelLead+"glm-5.2 · via z-ai") {
		t.Fatalf("the room dropped its machine because the vendor served its own model:\n%q", line)
	}
}

// ── 3. a rescue that did not land ───────────────────────────────────────────

// A RESCUE THAT FAILS HANDS THE ROW BACK TO THE LAST ANSWER. The rescue's news
// used to overwrite the only entry the desk kept, and when the rescue then
// failed without being refused the rider had nothing left and `via` went blank
// — until an answer finished, which an interrupted step never does.
func TestAFailedRescueHandsViaBackToTheLastAnswer(t *testing.T) {
	a, now := newsApp(t, flash)
	a.state = stateWorking
	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Role: lane.RoleTalk, At: now})
	PostLaneNews(LaneNews{Model: flash, Alt: "CoreWeave", Role: lane.RoleTalk, Trying: true, Reason: provider.RescueSlow, At: now})
	if seam := seamText(a); !strings.Contains(seam, "slow · trying coreweave…") {
		t.Fatalf("the rescue in flight was never said:\n%q", seam)
	}

	PostLaneNews(LaneNews{Model: flash, Alt: "CoreWeave", Role: lane.RoleTalk, Failed: true, Reason: provider.RescueSlow, At: now})
	if seam := seamText(a); !strings.Contains(seam, "deepseek-v4-flash · via cloudflare") {
		t.Fatalf("a failed rescue blanked the machine that last answered:\n%q", seam)
	}
}

// AND A RESCUE IS NOT PROMISED OVER A TURN THAT IS OVER. `trying coreweave…` is
// the present tense, and a turn interrupted while the rescue was out never posts
// the answer that would have cleared it.
func TestAnInterruptedTurnStopsPromisingARescue(t *testing.T) {
	a, now := newsApp(t, flash)
	a.state = stateWorking
	PostLaneNews(LaneNews{Model: flash, Lane: "Cloudflare", Role: lane.RoleTalk, At: now})
	PostLaneNews(LaneNews{Model: flash, Alt: "CoreWeave", Role: lane.RoleTalk, Trying: true, At: now})

	a.state = stateIdle
	seam := seamText(a)
	if strings.Contains(seam, "trying") {
		t.Fatalf("an idle conversation still promises a rescue:\n%q", seam)
	}
	if !strings.Contains(seam, "via cloudflare") {
		t.Fatalf("an idle conversation lost the machine that last answered:\n%q", seam)
	}
}

// ── 4. the first answer ─────────────────────────────────────────────────────

// `via` IS THERE WHILE THE FIRST ANSWER IS BEING WRITTEN. The sighting is posted
// only when a whole step has finished, so the first answer in a conversation —
// and a follow-up sent after the last sighting aged out — drew no machine for as
// long as it was written. The live phase knew who was writing it the whole time.
func TestTheFirstAnswerNamesItsMachineWhileItIsWritten(t *testing.T) {
	a, now := newsApp(t, phaseModel)
	a.state = stateWorking
	answerArriving(a)
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-2 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if seam := seamText(a); !strings.Contains(seam, "kimi-k3 · via friendli") {
		t.Fatalf("the first answer is being written and the seam names no machine:\n%q", seam)
	}

	// A FOLLOW-UP ON ANOTHER MACHINE SAYS THAT MACHINE, not the one the last
	// answer came from: who is writing now outranks who wrote last.
	PostLaneNews(LaneNews{Model: phaseModel, Lane: "Parasail", Role: lane.RoleTalk, At: now.Add(-time.Minute)})
	if seam := seamText(a); !strings.Contains(seam, "via friendli") || strings.Contains(seam, "parasail") {
		t.Fatalf("the seam names the last answer's machine over the one writing now:\n%q", seam)
	}
}

// ── 5. the model moved ──────────────────────────────────────────────────────

// THE CONVERSATION'S NEWS IS FOUND BY THE CONVERSATION, NOT BY ITS MODEL. A
// model picked mid-turn changes this window's id at once while the engine
// finishes the turn on the old one; a stream-cut hop and a change made from
// another window move it the other way. Each filed the rate and the machine
// under an id this window was not asking for.
func TestTheConversationsNewsIsFoundAfterTheModelMoved(t *testing.T) {
	a, now := newsApp(t, phaseModel)
	a.file = "/home/dev/.aforge/v3/sessions/conv-one/session.jsonl"
	a.state = stateWorking
	answerArriving(a)
	// The person picked another model; the engine is still finishing on the old.
	a.model = "z-ai/glm-5.3-flash"
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-3 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, Session: "conv-one", At: now,
	})
	PostLaneNews(LaneNews{Model: phaseModel, Lane: "Friendli", Role: lane.RoleTalk, Session: "conv-one", At: now})

	if line := statusRowText(a, 200); !strings.Contains(line, "38 tok/s") {
		t.Fatalf("the rate was filed under the model the engine is on and never found:\n%q", line)
	}
	if seam := seamText(a); !strings.Contains(seam, "via friendli") {
		t.Fatalf("the machine was filed under the model the engine is on and never found:\n%q", seam)
	}
}

// AND AN OLDER ENGINE, WHICH NAMES NO CONVERSATION, IS STILL READ BY MODEL —
// the compatibility bargain, byte for byte what it always drew.
func TestAnOlderEnginesNewsIsStillFoundByModel(t *testing.T) {
	a, now := newsApp(t, phaseModel)
	a.file = "/home/dev/.aforge/v3/sessions/conv-one/session.jsonl"
	a.state = stateWorking
	answerArriving(a)
	PostPhaseNews(PhaseNews{
		Phase: provider.PhaseWriting, Since: now.Add(-3 * time.Second), Lane: "Friendli", Rate: 38,
		Model: phaseModel, Role: lane.RoleTalk, At: now,
	})
	if line := statusRowText(a, 200); !strings.Contains(line, "38 tok/s") {
		t.Fatalf("a phase naming no conversation was not found under its model:\n%q", line)
	}
}

// ── 6. an engine that sends no news ─────────────────────────────────────────

// AN ENGINE THAT SENDS NO NEWS IS NAMED, ONCE, AFTER AN ANSWER. The news frames
// rode an existing wire version on purpose, so a window attached to an engine
// from before them drew no provider and no rate and nothing saying why.
func TestAnEngineThatSendsNoNewsIsNamedOnceAfterAnAnswer(t *testing.T) {
	a, _ := newsApp(t, phaseModel)
	silent := true
	a.link = LinkSeam{NewsSilent: func() bool { return silent }}
	said := func() int {
		n := 0
		for _, e := range a.entries {
			if e.kind == entryNote && e.text == newsSilenceNote {
				n++
			}
		}
		return n
	}

	// A TURN THAT NEVER ANSWERED PROVES NOTHING and says nothing.
	a.turn = 1
	a.settle()
	if said() != 0 {
		t.Fatal("a turn with no answer named the engine as older")
	}

	a.turn = 2
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the parser is fixed", turn: 2})
	a.settle()
	if said() != 1 {
		t.Fatalf("an answer with no news beside it said the engine is older %d times, want once", said())
	}
	a.turn = 3
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "and the tests pass", turn: 3})
	a.settle()
	if said() != 1 {
		t.Fatalf("the older-engine note was said again: %d times", said())
	}
}

// AND AN ENGINE WITH THE NEWS NEVER HEARS IT.
func TestAnEngineWithTheNewsIsNeverCalledOlder(t *testing.T) {
	a, _ := newsApp(t, phaseModel)
	a.link = LinkSeam{NewsSilent: func() bool { return false }}
	a.turn = 1
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "the parser is fixed", turn: 1})
	a.settle()
	for _, e := range a.entries {
		if e.kind == entryNote && e.text == newsSilenceNote {
			t.Fatal("an engine that sends the news was called older")
		}
	}
}

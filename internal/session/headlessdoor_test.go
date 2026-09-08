package session

// THE HEADLESS DOOR OF A RUN LEFT GOING (issue #535).
//
// A ceiling has appointed the same goal owner whether a conversation has a
// screen or not. These tests keep the door from changing which endings that
// owner reads: the mark ladder is the same on both doors, a handover is read
// before it seals, and a screenless session with nobody left in charge still
// pays for none of it.

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// C1 AND C3: A HEADLESS RUN LEFT GOING CROSSES THE SAME MARKS AS A WATCHED
// CONVERSATION, AND THE WATCHED CONVERSATION DOES NOT CHANGE.
//
// Both rows run the same grinding script through the whole ladder. The door is
// the only difference that matters here: the watched row has a person reading
// its events, while the headless row has the goal owner appointed by its
// ceiling. Each must read and journal the same three rounds, move the work once,
// and say the same handover line. The watched row must still gain no principal
// decision of its own.
func TestAHeadlessRunLeftGoingCheckpointsAtTheSameRoundsAsAChatOne(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	wantRounds := []int{
		checkpointMarkAt(1),
		checkpointMarkAt(2),
		checkpointMarkAt(3),
	}
	journalRounds := make(map[string][]int, 2)

	for _, posture := range []struct {
		name     string
		headless bool
	}{
		{name: "watched conversation"},
		{name: "headless run left going with a ceiling", headless: true},
	} {
		t.Run(posture.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: grindingSteps(
				rounds+checkpointSlack,
				checkpointChainSketch,
				"Finish the four pieces\nwhat is left, and everything this turn already found out",
			)}
			var agent *Agent
			var transcript string
			if posture.headless {
				agent, transcript = stewardCheckpointAgent(t, completer, func(config *Config) {
					config.AskConsent = false
				})
			} else {
				dir := t.TempDir()
				transcript = filepath.Join(dir, "transcript.jsonl")
				agent = checkpointAgent(t, completer, func(config *Config) {
					config.Workspace = dir
					config.SessionFile = transcript
					config.Divide = true
				})
			}

			ran := make(ranNodes, 2)
			graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })
			collected := collect(t, mustSubmit(t, agent,
				"work through the four things I listed and report back"))
			ran.await(t)

			if read := marksRead(completer); read != checkpointMarks {
				t.Fatalf("the mark reader was asked %d times, want %d", read, checkpointMarks)
			}
			if count := admitted(graph); count != 1 {
				t.Fatalf("%d tasks were started, want the one the ceiling moved", count)
			}
			if said := noticeTexts(collected); !saidSomething(said, checkpointCeilingNote) {
				t.Fatalf("the ceiling never said its handover line; notices were %q", said)
			}

			lines := closedJournal(t, agent, transcript)
			marks := journaledMarks(t, transcript)
			if len(marks) != checkpointMarks {
				t.Fatalf("%d mark rows were journaled, want %d", len(marks), checkpointMarks)
			}
			gotRounds := make([]int, 0, len(marks))
			for index, mark := range marks {
				gotRounds = append(gotRounds, mark.Rounds)
				if mark.N != index+1 {
					t.Errorf("mark %d was journaled as rung %d", index+1, mark.N)
				}
				if mark.Decision != checkpointDecisionContinue {
					t.Errorf("rung %d decided %q, want %q",
						mark.N, mark.Decision, checkpointDecisionContinue)
				}
			}
			if !slices.Equal(gotRounds, wantRounds) {
				t.Errorf("marks were journaled at rounds %v, want %v", gotRounds, wantRounds)
			}
			if !posture.headless && strings.Contains(lines, `"type":"principal"`) {
				t.Fatalf("the watched conversation gained a principal row:\n%s", lines)
			}
			journalRounds[posture.name] = gotRounds
		})
	}

	if !slices.Equal(journalRounds["watched conversation"],
		journalRounds["headless run left going with a ceiling"]) {
		t.Fatalf("the two doors journaled different mark rounds: %v",
			journalRounds)
	}
}

// C2: A HANDOVER ON A HEADLESS RUN IS READ BY ITS GOAL OWNER BEFORE THE TURN
// SEALS.
//
// Work already in flight makes the answer unambiguously "carry on". The
// principal row must carry that verb and precede the non-auxiliary usage row
// that seals the turn; otherwise the headless door has ended a turn without
// putting its ending to the owner the ceiling appointed.
func TestAHandoverOnAHeadlessRunIsReadByItsGoalOwner(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), func(config *Config) {
		config.AskConsent = false
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))
	if count := admitted(graph); count != 2 {
		t.Fatalf("%d tasks were started, want the running one and the handover", count)
	}

	lines := closedJournal(t, agent, transcript)
	decidedAt, sealedAt := -1, -1
	for index, line := range strings.Split(lines, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("journal line %d is not JSON: %v", index+1, err)
		}
		if entry.Principal != nil && entry.Principal.Event == "decided" {
			decidedAt = index
			if entry.Principal.Decision != string(DecideCarryOn) {
				t.Errorf("the goal owner decided %q, want %q",
					entry.Principal.Decision, DecideCarryOn)
			}
		}
		if entry.Type == "usage" && entry.Usage != nil && !entry.Usage.Aux {
			sealedAt = index
		}
	}
	if decidedAt < 0 {
		t.Fatalf("the handover reached no principal decision row:\n%s", lines)
	}
	if sealedAt < 0 {
		t.Fatalf("the handed-over turn reached no seal:\n%s", lines)
	}
	if decidedAt >= sealedAt {
		t.Fatalf("the principal decision is on line %d and the turn seal on line %d; want the decision first:\n%s",
			decidedAt+1, sealedAt+1, lines)
	}
}

// C4: A SCREENLESS RUN WITH NOBODY LEFT IN CHARGE STILL NEVER CHECKPOINTS.
//
// `--yolo` without a ceiling and an ordinary headless turn both retain a
// Person. With nobody watching that person, neither posture has a reader for a
// checkpoint line. They therefore pay for no mark, move no work, and run the
// model's script to its own end.
func TestAScreenlessRunWithNobodyInChargeStillNeverCheckpoints(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks) + 2
	for _, posture := range []struct {
		name       string
		unattended bool
	}{
		{name: "not unattended"},
		{name: "unattended with no ceiling", unattended: true},
	} {
		t.Run(posture.name, func(t *testing.T) {
			steps := append(grindingSteps(rounds, checkpointSplitSketch, ""), finalAnswer("done"))
			completer := &scriptedCompleter{steps: steps}
			agent := checkpointAgent(t, completer, func(config *Config) {
				config.AskConsent = false
				config.Unattended = posture.unattended
				config.Budget = Budget{}
			})
			if agent.steward() != nil {
				t.Fatal("the screenless control unexpectedly has a goal owner")
			}
			graph := stubbedGraph(agent, func(node *TaskNode) {
				node.finish("done", nil, "", "")
				node.graph.complete(node, TaskDone)
			})

			collected := collect(t, mustSubmit(t, agent,
				"work through the four things I listed and report back"))

			if read := marksRead(completer); read != 0 {
				t.Errorf("the screenless run paid for %d mark readings", read)
			}
			if count := admitted(graph); count != 0 {
				t.Errorf("the screenless run had %d tasks started over it", count)
			}
			if said := noticeTexts(collected); saidSomething(said, checkpointCeilingNote) ||
				saidSomething(said, checkpointSplitNote) {
				t.Errorf("the screenless run was moved off its own turn: %q", said)
			}
			if completer.requests() <= rounds {
				t.Errorf("the screenless run made only %d requests of %d, so something ended it early",
					completer.requests(), rounds)
			}
		})
	}
}

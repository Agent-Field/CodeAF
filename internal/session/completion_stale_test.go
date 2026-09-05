package session

// WHO OWNS THE DECISION THAT A REQUEST IS FINISHED (calibration handoff-before
// cell 004-revision-midwork-aforge, and handoff-after cell 001 after it).
//
// Both cells are the same conversation: a person asked for a one-minute command
// AND a report, changed the report from Markdown to CSV while the command ran,
// and the model wrote `report.csv`, removed `report.md` and finished. Both cells
// then made a task out of it and both hit the 180-second cap.
//
// The two traces failed through the SAME line for two different reasons, which
// is what these cases pin. In 004 (`transcript.jsonl` lines 33-44) the mark's
// reader drew `B` — one part, no division — the continuation answered
// `NOTHING LEFT TO DO` (`carry rung:draft outcome:nothing-left`), and the drop
// was refused because the drawing was not the word `(done)`; the ceiling wrote
// `seam:write decision:moved carry:ask` and a cold worker was started on the
// person's ORIGINAL sentence, CSV and all. In 001 the reader could not be
// reached at all (`role:markreader message:context canceled`) and the identical
// refusal followed from a reading that never happened.
//
// So the law these cases hold: a drawing REFUSES a completion claim only when it
// says work remains, never merely by failing to agree — and the claim is bounded
// by the turn's own budget rather than by a second reader.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// finishedScript is the trace's own turn with the drawing the case wants: the
// two workspace writes that fire the write seam, then a conversation that says
// in words that the artifacts are written. The dowry is answered with the remains
// token, which is what the live model answered.
func finishedScript(count int, sketch func() (string, error)) []step {
	var writes atomic.Int64
	steps := make([]step, count)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return sketchResponse(sketch())
			}
			if askedForHandoff(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("Finish the remaining step: verify the report."), nil
			}
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			round := writes.Add(1)
			if round > writeAllowanceFiles {
				return textResponse("report.csv is written and report.md is gone."), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
				Text string `json:"content"`
			}{Path: fmt.Sprintf("report%d.csv", round), Text: "service,port\nkestrel,8431\n"})
			return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
				"Writing the revised report."), nil
		}
	}
	return steps
}

// sketchResponse turns one scripted drawing into what the reader's road expects,
// so a case can say "the reader was down" in the same breath as "it drew this".
func sketchResponse(shape string, err error) (*ai.Response, error) {
	if err != nil {
		return nil, err
	}
	return textResponse(shape), nil
}

// finishedAgent is [awaitingAgent] with a session file, because what these cases
// assert is partly what the file says the seam decided.
func finishedAgent(t *testing.T, completer Completer, path string) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.Interactive = true
		config.SessionFile = path
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
	})
	return agent
}

// ── the frozen cells, closed ────────────────────────────────────────────────

// A DRAWING THAT SHOWED NO REMAINDER NEVER STOOD IN FOR A READER SAYING WORK WAS
// LEFT — cell 004's own shape.
//
// The reader drew one part and no division; the model, holding the whole
// transcript, said the request was discharged. Nothing in the building says work
// remains, so nothing is handed to anybody: the turn's own answer stands and the
// file records the drop.
func TestADrawingWithNoRemainderInItDoesNotForceAHandover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		return "B\nB is the report the person asked for.", nil
	})}
	agent := finishedAgent(t, completer, path)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// THE ROAD WAS ENTERED, so this cannot pass for a turn that never reached the
	// door the cell failed at.
	if handoffAsks(completer) != 1 {
		t.Fatalf("the handover road was taken %d times, want exactly one", handoffAsks(completer))
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted for a request the model said was discharged", count)
	}
	if saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the person was told work was moving when none did: %q", noticeTexts(collected))
	}
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the seam journaled %+v, want one %q", ceilings, checkpointCeilingNothing)
	}
}

// AND A READER NOBODY COULD REACH IS NOT A READER SAYING WORK REMAINS — cell
// 001's own shape.
//
// The mark reader was cancelled and drew nothing whatever. A reading that never
// happened is silence, and silence used to be spent as evidence against the one
// reader that had actually read the work.
func TestAReaderNobodyCouldReachDoesNotForceAHandover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		return "", errors.New("the reader is down")
	})}
	agent := finishedAgent(t, completer, path)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if handoffAsks(completer) != 1 {
		t.Fatalf("the handover road was taken %d times, want exactly one", handoffAsks(completer))
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted behind a reader that never answered", count)
	}
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the seam journaled %+v, want one %q", ceilings, checkpointCeilingNothing)
	}
}

// AND A DIRECTION THE PERSON TYPED DURING THE DECISION IS STILL THERE TO BE READ.
//
// The drop is the outcome that KEEPS the turn, so words queued while the seam was
// deciding reach the model at the very next boundary. A handover would have sent
// them nowhere: the worker opens on a brief written before they were typed.
func TestAQueuedDirectionSurvivesTheDrop(t *testing.T) {
	const revision = "one more thing — sort the rows by port"

	var agent *Agent
	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		// The person types WHILE the seam is deciding, which is the window
		// [Agent.awaitGroundNow] exists for on the other road.
		queueDirection(agent, revision)
		return "B\nB is the report the person asked for.", nil
	})}
	agent = awaitingAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted while the person's newest words were unread", count)
	}
	// AND THE WORDS REACHED THE MODEL, in this conversation, rather than being
	// left on a queue behind an ended turn.
	if !strings.Contains(transcriptText(agent), revision) {
		t.Fatal("the direction typed during the decision never reached the conversation")
	}
}

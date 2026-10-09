package session

// THE MANAGER'S PEN: `factory_run` SHAPES THE RUN OF ITS ITEM.
//
// The manager is the author of the run (the owner's decision of 2026-10-08):
// an item's own conversation sets the stages its run will take, one lowercase
// word each, each with an ask that says what done looks like. `factory_run` is
// that act, beside `factory_item` (budget, thinking, notes), and it
// differs from it in one way that matters: IT RAISES NO CARD. Before a run
// nothing is running, so the change is made at once and the item's page
// redraws; during a run it touches only the stages not yet started, and the
// run's next round reads them. The person sees the line it answers.
//
// THE BOUNDS ARE HELD IN CODE, by [factory.Edit] behind the door: proof is
// never dropped, a fixed stage never changes, a stage that started is never
// touched, and a refusal comes back in Edit's own sentence with nothing
// changed. AN APPROVE STEP (where the run holds until the person says
// continue) is added or skipped like any stage: add one named approve.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// RunDoor is the one thing a manager conversation may do to its item's run:
// change its stages by an edit. conversation is the asking conversation's
// session file, which is how the door finds the ONE item it manages; a
// conversation that manages none is refused. The door answers the item as
// saved and the lines the edit recorded. cmd/codeaf implements it over the
// floor's seam ([factory.Seam.Edit]); a shaping turn the runner gives the
// manager implements it over the item in hand, collecting the edit for the
// runner to apply.
type RunDoor interface {
	EditRun(ctx context.Context, conversation string, e factory.RunEdit) (factory.Item, []string, error)
}

// mayRun says whether `factory_run` belongs on this belt.
func (c Config) mayRun() bool { return c.FactoryRun != nil }

// The words the tool answers with.
const (
	runRefusedLead = "nothing changed: "
	runAlready     = "nothing changed: the run already has that shape"
)

// runThinking is the schema's thinking words; `default` is the knee.
var runThinking = []string{"cheap", "strong", "default"}

const factoryRunDescription = "Shape the run of the ONE factory item this conversation manages. Its stages are one lowercase word each, at most nine. " +
	"set changes a stage's ask (what done looks like) or its thinking (cheap, strong, default); add puts in a new stage after a named one, with its ask and one line of why; on switches a stage of the recipe on; skip switches one off; why is one sentence for the whole change. " +
	"Keep the recipe's stages unless the item says otherwise, change asks before adding stages, and add a stage only for work no stage covers. Proof is never skipped. " +
	"An approve step (named approve, a second one approve2) is where the run holds until the person says continue: add one after the stage the person wants to read first, with no ask, or skip it to let the run go on; a fixed one stays. " +
	"Before the run the change is made at once and NO CARD IS SHOWN to the person: nothing is running, and the stages on the item's page redraw. During a run it changes only the stages that have not started, and the run's next round reads them. " +
	"The answer is the line the item's page shows (manager set review: … · added arch after review · why: …), or why it was refused, with nothing changed. " +
	"Budget, thinking for every stage and notes for the stages stay with factory_item."

func factoryRunSchemaJSON() string {
	quoted := make([]string, len(runThinking))
	for i, w := range runThinking {
		quoted[i] = `"` + w + `"`
	}
	return `{"type":"object","properties":{` +
		`"set":{"type":"array","description":"Stages to change, by name.","items":{"type":"object","properties":{` +
		`"stage":{"type":"string","description":"The stage's name, one lowercase word."},` +
		`"ask":{"type":"string","description":"What done looks like for this stage, in one sentence."},` +
		`"thinking":{"type":"string","enum":[` + strings.Join(quoted, ",") + `],"description":"How hard this stage thinks."}` +
		`},"required":["stage"],"additionalProperties":false}},` +
		`"add":{"type":"array","description":"New stages, each after a stage the run has.","items":{"type":"object","properties":{` +
		`"name":{"type":"string","description":"One lowercase word."},` +
		`"ask":{"type":"string","description":"What done looks like for it; none for an approve step."},` +
		`"after":{"type":"string","description":"The stage it goes after."},` +
		`"why":{"type":"string","description":"One line of why no stage covers this work."}` +
		`},"required":["name","after"],"additionalProperties":false}},` +
		`"on":{"type":"array","items":{"type":"string"},"description":"Stage names of the recipe to switch on."},` +
		`"skip":{"type":"array","items":{"type":"string"},"description":"Stage names to switch off. Never proof."},` +
		`"why":{"type":"string","description":"One sentence saying why."}` +
		`},"additionalProperties":false}`
}

// The gloss a person reads beside the call is the reason.
func init() { glossField["factory_run"] = "why" }

// runArgs is the tool's arguments as the model sends them.
type runArgs struct {
	Set []struct {
		Stage    string `json:"stage"`
		Ask      string `json:"ask"`
		Thinking string `json:"thinking"`
	} `json:"set"`
	Add []struct {
		Name  string `json:"name"`
		Ask   string `json:"ask"`
		After string `json:"after"`
		Why   string `json:"why"`
	} `json:"add"`
	On   []string `json:"on"`
	Skip []string `json:"skip"`
	Why  string   `json:"why"`
}

// runEditOf is the tool's arguments as the edit the door applies, by the
// manager, or the sentence saying what is wrong with them.
func runEditOf(args runArgs) (factory.RunEdit, error) {
	e := factory.RunEdit{By: factory.ByManager, Why: strings.Join(strings.Fields(args.Why), " ")}
	for _, s := range args.Set {
		name, err := factory.StageWord(s.Stage)
		if err != nil {
			return factory.RunEdit{}, err
		}
		ask := strings.Join(strings.Fields(s.Ask), " ")
		thinking := strings.ToLower(strings.TrimSpace(s.Thinking))
		if ask == "" && thinking == "" {
			return factory.RunEdit{}, errors.New("set " + name + " needs an ask or a thinking")
		}
		if thinking != "" && !oneOf(thinking, runThinking) {
			return factory.RunEdit{}, errors.New("thinking is one of " + strings.Join(runThinking, ", "))
		}
		if ask != "" {
			if e.Ask == nil {
				e.Ask = map[string]string{}
			}
			e.Ask[name] = ask
		}
		if thinking != "" {
			if e.Thinking == nil {
				e.Thinking = map[string]string{}
			}
			// DEFAULT IS THE KNEE, which a stage stores as no word at all.
			if thinking == "default" {
				thinking = ""
			}
			e.Thinking[name] = thinking
		}
	}
	for _, a := range args.Add {
		name, err := factory.StageWord(a.Name)
		if err != nil {
			return factory.RunEdit{}, err
		}
		ask := strings.Join(strings.Fields(a.Ask), " ")
		after := strings.ToLower(strings.TrimSpace(a.After))
		why := strings.Join(strings.Fields(a.Why), " ")
		if factory.ApproveWord(name) {
			// AN APPROVE STEP HAS NO ASK: the person is the step.
			st := factory.Approve(nil)
			st.Why, st.By = why, factory.ByManager
			e.Add = append(e.Add, factory.Added{Stage: st, After: after})
			if e.Why == "" {
				e.Why = why
			}
			continue
		}
		if ask == "" {
			return factory.RunEdit{}, errors.New("the stage " + name + " needs an ask that says what done looks like")
		}
		e.Add = append(e.Add, factory.Added{Stage: addedStage(name, ask, why), After: after})
		if e.Why == "" {
			e.Why = why
		}
	}
	e.On = runNames(args.On)
	e.Skip = runNames(args.Skip)
	return e, nil
}

// addedStage is a stage the manager adds: a conversation, on, with its ask
// and its own line of why.
func addedStage(name, ask, why string) factory.Stage {
	return factory.Stage{Name: name, Kind: factory.StageChat, Ask: ask, On: true, Why: why, By: factory.ByManager}
}

// runNames is stage names as one lowercase word each, the empty dropped.
func runNames(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// RunEditLine is an edit's lines as the one line the item's page shows:
// joined with ` · `, who made it said once, `manager set review: … · added
// arch after review · why: …`.
func RunEditLine(lines []string) string {
	var parts []string
	by := ""
	for _, l := range lines {
		if l = strings.Join(strings.Fields(l), " "); l == "" {
			continue
		}
		if len(parts) == 0 {
			if f := strings.Fields(l); len(f) > 1 && !strings.HasSuffix(f[0], ":") {
				by = f[0] + " "
			}
		} else if by != "" {
			l = strings.TrimPrefix(l, by)
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, DecisionSep)
}

// factoryRunTool is the manager's pen.
func (a *Agent) factoryRunTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_run",
		Description: factoryRunDescription,
		Schema:      json.RawMessage(factoryRunSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed runArgs
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			edit, err := runEditOf(parsed)
			if err != nil {
				return "Invalid arguments: " + err.Error() + ".", true, nil
			}
			if edit.Empty() {
				return "Invalid arguments: factory_run needs something to change: set, add, on or skip.", true, nil
			}
			_, lines, err := a.runDoorForTurn().EditRun(ctx, a.config.SessionFile, edit)
			if err != nil {
				return runRefusedLead + strings.Join(strings.Fields(err.Error()), " "), true, nil
			}
			line := RunEditLine(lines)
			if line == "" {
				return runAlready, false, nil
			}
			return line, false, nil
		},
	}
}

// SubmitRunnerNote starts a turn on words the factory's runner wrote for the
// item's manager (`Shape the run for this item now. …`): the same turn
// [Agent.Submit] starts, journaled as the session's own note rather than as
// the person's words, so the runner never reads its own ask back as a steer
// ([PersonLines] skips a note).
func (a *Agent) SubmitRunnerNote(ctx context.Context, text string) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}
	return a.submitUser(ctx, briefNote(text))
}

// ── A SHAPING TURN THROUGH A CONVERSATION THE WINDOW HOLDS ──────────────────
//
// When the person has the item's manager open in a window of this process,
// the runner's shaping turn cannot open the conversation a second time (the
// journal's lock refuses it). It runs as a turn of THAT conversation instead,
// so the window shows the manager thinking, and `factory_run` on that ONE turn
// answers through the runner's collecting door rather than the floor's: the
// runner applies the edit itself, as it does on its own road.
//
// THE DOOR BELONGS TO THE TURN, NOT TO THE AGENT. It is kept beside the turn's
// number ([Agent.teamTurnSerial]) and read under the agent's lock, so the next
// turn (the person's own, a follow-up, a wake) gets the floor's door again
// without anybody having to put it back, and there is no moment between the
// shaping turn's end and a restore in which a person's turn could reach the
// collecting door. A person's line said INTO the shaping turn steers it, and
// what the manager sets on that line is collected with the rest.

// turnRunDoor is a door lent to one turn.
type turnRunDoor struct {
	door   RunDoor
	serial uint64
}

// turnRunDoors is the door lent to an agent's turn, by *Agent.
var turnRunDoors sync.Map

// runDoorForTurn is the door `factory_run` answers through on the turn in
// flight: the one lent to it, else the conversation's own.
func (a *Agent) runDoorForTurn() RunDoor {
	a.mu.Lock()
	running, serial := a.running, a.teamTurnSerial
	a.mu.Unlock()
	if v, ok := turnRunDoors.Load(a); ok {
		lent := v.(turnRunDoor)
		if running && lent.serial == serial {
			return lent.door
		}
		turnRunDoors.CompareAndDelete(a, lent)
	}
	return a.config.FactoryRun
}

// SubmitRunnerNoteThrough starts a turn on the runner's words, as
// [Agent.SubmitRunnerNote] does, with door as `factory_run` FOR THAT TURN
// ONLY, and the turn handed to every window watching this conversation
// ([Agent.WatchWakes]) so the person sees the manager think. It never steers a
// turn in flight: a conversation mid-turn answers [ErrConversationBusy], and
// the caller waits ([Agent.WaitIdle]) and asks again. The thinking is the
// conversation's own, the person's setting; this road does not change it.
func (a *Agent) SubmitRunnerNoteThrough(ctx context.Context, text string, door RunDoor) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}
	user := briefNote(text)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("session: agent is closed")
	}
	if a.running {
		return nil, ErrConversationBusy
	}
	if err := a.resumeWorkLocked(); err != nil {
		return nil, err
	}
	a.attachTurnSkillsLocked(&user)
	if err := a.railBlockLocked(); err != nil {
		return refusedStream(err), nil
	}
	// THE WINDOW DRAWS THE TURN from its first delta, as it draws a wake: the
	// stream is handed over before the turn starts, and a lane too far behind
	// is skipped rather than waited on, for [Agent.wakeLocked]'s reason.
	watchers := make([]*eventStream, 0, len(a.wakeLanes))
	for _, lane := range a.wakeLanes {
		stream := newEventStream()
		select {
		case lane <- stream.out:
			watchers = append(watchers, stream)
		default:
			stream.close()
		}
	}
	events := a.startTurnLocked(ctx, user, nil, watchers...)
	if door != nil {
		turnRunDoors.Store(a, turnRunDoor{door: door, serial: a.teamTurnSerial})
	}
	return events, nil
}

// WaitIdle waits until no turn is in flight, or ctx ends.
func (a *Agent) WaitIdle(ctx context.Context) error {
	for {
		a.mu.Lock()
		done, closed := a.done, a.closed
		a.mu.Unlock()
		if done == nil || closed {
			return nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MANAGER'S TURNS ────────────────────────────────────────────────────
//
// The manager is the author of the run (the owner's decision of 2026-10-08),
// on the person's word, and this file holds the two doors that make it so:
//
//   - `factory_run` on the manager's own belt ([runDoor]): what the manager
//     sets while the person talks to it is made at once through the floor's
//     edit door ([factory.Seam.Edit]), before a run or on the stages a run has
//     not started;
//   - the turn a worker gives the manager on a step's question
//     ([shapeTurns.through], the inbox in factory_inbox.go): through the
//     window's conversation when this process holds it, else opened for the
//     one turn, at the cheap thinking unless the item reads as large.
//
// A LAUNCH GIVES THE MANAGER NO TURN, and nothing else asks it to reshape the
// steps: ▶ run runs the item's stages as they stand (a turn before the first
// stage held the plan behind a conversation nobody had asked anything of, the
// owner's run of 2026-10-19), and the person who wants other steps says so in
// the manager's chat or edits them on the job's page.

// managerTurn is what one manager turn needs of an open conversation; the
// session's *Agent answers it, and a test hands in a fake.
type managerTurn interface {
	SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error)
	Interrupt()
	Close() error
}

// idleWaiter is a managerTurn that can be waited on until its conversation is
// between turns: the live road's, where the person's own turn may be running.
type idleWaiter interface {
	WaitIdle(ctx context.Context) error
}

// shapeTurns gives an item's manager one turn of its conversation.
type shapeTurns struct {
	dirs func(repo string) string
	// live answers the item's manager conversation when a window of THIS
	// process holds it ([session.LiveAgentFor]), as a turn through it with
	// door lent to that turn alone.
	live func(transcript string, door session.RunDoor) (managerTurn, bool)
	// open opens the item's manager conversation for one turn, with door as
	// its `factory_run`, at the cheap thinking when cheap.
	open func(ctx context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error)
	// refusal is why the parent the next manager opens from was refused by the
	// saved Task models policy, nil when it was not or when no policy is kept.
	refusal func() error
	// liveOK says whether the window's conversation, on model, may carry a
	// manager turn. Nil allows every live conversation (Auto's road).
	liveOK func(model string) bool
}

// factoryShapeTurnsFrom reads the launch when a manager opens, because the
// worker survives the window whose launch first built its turns.
func factoryShapeTurnsFrom(st *store.Store, workspace, profileDir string, current func() session.Config) *shapeTurns {
	turns := &shapeTurns{dirs: factoryRepoDirs(st, workspace)}
	// UNDER A SAVED ONE MODEL THE WINDOW'S CONVERSATION IS NOT THE FACTORY'S: it
	// runs on whatever model the person is talking to, so liveOK refuses it and
	// the turn opens a governed factory session instead.
	turns.live = func(transcript string, door session.RunDoor) (managerTurn, bool) {
		agent, ok := session.LiveAgentFor(transcript)
		if !ok || (turns.liveOK != nil && !turns.liveOK(agent.Model())) {
			return nil, false
		}
		return liveManagerTurn{agent: agent, door: door}, true
	}
	turns.open = func(_ context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
		parent := current()
		if err := refusalOf(parent, turns.refusal); err != nil {
			return nil, err
		}
		return openManagerTurn(parent, workspace, profileDir, it, door, cheap)
	}
	return turns
}

// factoryLiveOK is the window-conversation gate for a worker whose launch is
// raw. Auto keeps the live road. Under a saved one model it is closed, because
// the window is on its own model; under an explicit --one-model launch it is
// open only for a conversation proven to be on that very pin.
func factoryLiveOK(raw func() session.Config, profileDir string) func(model string) bool {
	return func(model string) bool {
		launch := raw()
		if launch.OneModel {
			return strings.TrimSpace(model) == strings.TrimSpace(launch.Model)
		}
		return config.TaskModelsModeAt(profileDir) != crewroute.ModeOne
	}
}

// ── THE LIVE ROAD: THE WINDOW HOLDS THE MANAGER ─────────────────────────────
//
// The person often has the item's manager open in a window when a turn of the
// runner's comes for it (a step's question for the inbox).
// The window's conversation holds the journal's lock, so a second open is
// refused. The turn then runs AS A TURN OF THAT CONVERSATION: the
// window shows the manager think, and the turn's own door is lent to that one
// turn ([session.Agent.SubmitRunnerNoteThrough]), as on the road that opens.
// The thinking is the person's setting there: this road does not make it
// cheap.
// Closing the turn closes nothing; the window keeps its conversation.

// liveManagerTurn is one manager turn through a conversation open here.
type liveManagerTurn struct {
	agent *session.Agent
	door  session.RunDoor
}

func (l liveManagerTurn) SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error) {
	return l.agent.SubmitRunnerNoteThrough(ctx, text, l.door)
}
func (l liveManagerTurn) Interrupt()                         { l.agent.Interrupt() }
func (l liveManagerTurn) WaitIdle(ctx context.Context) error { return l.agent.WaitIdle(ctx) }

// Close lets the turn go and leaves the window's conversation open.
func (l liveManagerTurn) Close() error { return nil }

// liveManagerTurnFor is the live road when the transcript is open here.
func liveManagerTurnFor(transcript string, door session.RunDoor) (managerTurn, bool) {
	agent, ok := session.LiveAgentFor(transcript)
	if !ok {
		return nil, false
	}
	return liveManagerTurn{agent: agent, door: door}, true
}

// shapeCheap says whether a manager turn thinks cheap: always, unless the
// item's shown size (a label, a measured diff or a completed read) is large.
func shapeCheap(it factory.Item) bool {
	shown, _ := factory.ShownSize(it)
	return shown != "L"
}

// through is one manager turn on ask with door as its `factory_run` (and, on
// an inbox turn, its `factory_answer`): through the window's conversation
// when this process holds it, else opened for the turn. It answers what the
// manager said.
func (s *shapeTurns) through(ctx context.Context, it factory.Item, ask string, door session.RunDoor) (string, error) {
	if s == nil || s.open == nil {
		return "", errors.New("no manager to ask")
	}
	var turn managerTurn
	if s.live != nil {
		turn, _ = s.live(it.Talk, door)
	}
	if turn == nil {
		opened, err := s.open(ctx, it, door, shapeCheap(it))
		if errors.Is(err, session.ErrSessionLocked) {
			// ANOTHER PROCESS HOLDS IT (a window on the engine host): this
			// runner cannot give it a turn, and says so by name. No retry.
			return "", fmt.Errorf("%w: %v", factoryrun.ErrManagerAway, err)
		}
		if err != nil {
			return "", err
		}
		turn = opened
	}
	defer turn.Close()
	events, err := s.submit(ctx, turn, ask)
	if err != nil {
		return "", err
	}
	var reply strings.Builder
	var failure error
	for {
		select {
		case ev, open := <-events:
			if !open {
				if failure != nil {
					return "", failure
				}
				return strings.TrimSpace(reply.String()), nil
			}
			switch ev.Kind {
			case session.EventTextDelta:
				reply.WriteString(ev.Text)
			case session.EventError:
				failure = ev.Err
				if failure == nil {
					failure = errors.New("the turn failed without a reason")
				}
			}
		case <-ctx.Done():
			turn.Interrupt()
			return "", ctx.Err()
		}
	}
}

// submit puts the ask in. A conversation the window holds that is in the
// middle of a turn of its own is waited on, for at most half of what is left
// of the turn's wait so the retry has the other half to answer, and asked
// ONCE more; busy again is [factoryrun.ErrManagerBusy].
func (s *shapeTurns) submit(ctx context.Context, turn managerTurn, ask string) (<-chan session.Event, error) {
	events, err := turn.SubmitRunnerNote(context.WithoutCancel(ctx), ask)
	if !errors.Is(err, session.ErrConversationBusy) {
		return events, err
	}
	waiter, ok := turn.(idleWaiter)
	if !ok {
		return nil, fmt.Errorf("%w: %v", factoryrun.ErrManagerBusy, err)
	}
	wait := ctx
	if deadline, has := ctx.Deadline(); has {
		var cancel context.CancelFunc
		wait, cancel = context.WithTimeout(ctx, time.Until(deadline)/2)
		defer cancel()
	}
	if werr := waiter.WaitIdle(wait); werr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	events, err = turn.SubmitRunnerNote(context.WithoutCancel(ctx), ask)
	if errors.Is(err, session.ErrConversationBusy) {
		return nil, fmt.Errorf("%w: %v", factoryrun.ErrManagerBusy, err)
	}
	return events, err
}

// openManagerTurn opens the item's manager conversation for one turn. It is a
// conversation nobody is at the keyboard of for this turn: no card door is on
// its belt (a card nobody answers holds the turn until the wait ends), the
// approvals are the person's own rows, headless, and `factory_run` is door.
// A conversation a window of THIS process holds never reaches here (the live
// road above); one another process holds is refused by its lock, and the run
// goes on with the recipe, saying so by name.
func openManagerTurn(parent session.Config, workspace, profileDir string, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
	transcript := strings.TrimSpace(it.Talk)
	if transcript == "" {
		return nil, errors.New("the item has no manager conversation")
	}
	if factoryPolicyRefused(parent) {
		return nil, errFactoryPolicyRefused
	}
	cfg, err := v3Reopen(parent, transcript, workspace)
	if err != nil {
		return nil, err
	}
	where := strings.TrimSpace(cfg.Workspace)
	if where == "" {
		where = workspace
	}
	fallback := appDefaultFallback(profileDir)
	if cfg.OneModel || fallback == cfg.Model {
		fallback = ""
	}
	cfg.ConversationModels = &session.ConversationModels{Main: cfg.Model, Fallback: fallback}
	cfg.Interactive = false
	cfg.AskConsent = false
	cfg.Factory, cfg.Recipe, cfg.FactoryItem, cfg.Floor, cfg.Stage = nil, nil, nil, nil, nil
	cfg.FactoryRun = door
	if cheap {
		cfg.Effort = effort.Low
	}
	gate := v3ApprovalGate{workspace: where, profileDir: profileDir, headless: true}
	cfg.ApprovalGate = gate
	cfg.ApprovalPosture = session.PostureAsk
	if cfg.ApprovalPolicy, cfg.Guardian, err = gate.Build(session.PostureAsk); err != nil {
		return nil, err
	}
	cfg, open := v3Shape(cfg, v3Lanes{})
	agent, _, _, err := openV3Agent(cfg, where, open)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// runDoor is `factory_run`'s door for a conversation the person talks to: the
// floor's edit door over st, for the one item whose manager the asking
// conversation is. A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason.
func runDoor(st *store.Store, workspace, profileDir string) session.RunDoor {
	if st == nil {
		return nil
	}
	seam := factory.LocalSeam(st, time.Now(), factoryPipelineOption(), factory.WithRepoDirs(factoryRepoDirs(st, workspace)))
	if seam.Edit == nil {
		return nil
	}
	return seamRunDoor{st: st, workspace: workspace, profileDir: profileDir, edit: seam.Edit}
}

// seamRunDoor finds the item a conversation manages and edits it through the
// seam.
type seamRunDoor struct {
	st        *store.Store
	workspace string
	// profileDir is the profile whose GitHub token a start's checkout door
	// reads, the one this machine's runner clones with.
	profileDir string
	edit       func(ctx context.Context, id int, e factory.RunEdit) (factory.Item, []string, error)
}

// errNotAManager is `factory_run` asked by a conversation that manages no item.
var errNotAManager = factory.ErrNotManager

// EditRun edits the item whose conversation is the asking one.
func (d seamRunDoor) EditRun(ctx context.Context, conversation string, e factory.RunEdit) (factory.Item, []string, error) {
	it, err := d.managed(conversation)
	if err != nil {
		return factory.Item{}, nil, err
	}
	return d.edit(ctx, it.ID, e)
}

// managed is the item whose conversation is the asking one.
func (d seamRunDoor) managed(conversation string) (factory.Item, error) {
	want := sameFileKey(conversation)
	if want == "" || d.st == nil {
		return factory.Item{}, errNotAManager
	}
	items, err := d.st.List()
	if err != nil {
		return factory.Item{}, err
	}
	for _, it := range items {
		if strings.TrimSpace(it.Talk) != "" && sameFileKey(it.Talk) == want {
			return it, nil
		}
	}
	return factory.Item{}, errNotAManager
}

// sameFileKey is a path as one spelling, links resolved.
func sameFileKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

// ManagerStatus reads the durable job and lease without opening another agent
// or making a provider request. The saved transcript is not execution state.
func (d seamRunDoor) ManagerStatus(ctx context.Context, conversation string) (factory.ManagerStatus, error) {
	if err := ctx.Err(); err != nil {
		return factory.ManagerStatus{}, err
	}
	it, err := d.managed(conversation)
	if err != nil {
		return factory.ManagerStatus{}, err
	}
	desk, err := lease.Open(d.st, lease.Options{})
	if err != nil {
		return factory.ManagerStatus{}, err
	}
	held, err := desk.HasLiveHold(it.ID)
	if err != nil {
		return factory.ManagerStatus{}, err
	}
	return factory.StatusForManager(it, held, time.Now()), nil
}

func (d seamRunDoor) AnswerPending(ctx context.Context, conversation, questionID, answer string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	it, err := d.managed(conversation)
	if err != nil {
		return "", err
	}
	desk, err := lease.Open(d.st, lease.Options{})
	if err != nil {
		return "", err
	}
	receipt, err := lease.NewDoors(desk).AnswerPending(it.ID, it.Talk, questionID, answer)
	return managerAnswerResult(receipt, err)
}

func managerAnswerResult(receipt factory.ManagerAnswer, err error) (string, error) {
	if receipt.QuestionID == "" {
		return "", err
	}
	line := "Decision saved for " + receipt.Stage + ": " + receipt.Words + "."
	if err != nil {
		return line + " Worker notification could not be persisted: " + err.Error() + ". Retry factory_answer with the same question_id and answer to redeliver; the decision is already saved.", nil
	}
	return line + " The saved decision is confirmed. A newer pending question, if any, is unchanged.", nil
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ── A STAGE IS A CONVERSATION: THE MAKER ───────────────────────────────────
//
// internal/factory/run's chat executor opens one conversation per round of a
// chat stage through a [factoryrun.ConversationMaker]. This is the real one, built
// on the talk lane's machinery (factory_talk.go): a session folder in the
// bucket of the repository's checkout, a member of the item's own team under
// the one `factory` team, and the brief as its first turn.
//
// WHAT MAKES IT A STAGE AND NOT AN ORDINARY CONVERSATION is three settings on
// the config it is opened from, and nothing else:
//
//   - Stage is the round's door, so `stage_result` and `plan_edit` are on its
//     belt (internal/session's tools_stage.go);
//   - it is UNATTENDED: nobody is at its keyboard, so a card it raises takes
//     its default and a task's landing takes the check road's accept. It has
//     NO GOAL OWNER, because its runner already judges what it reports
//     (internal/session's newPrincipalFor): it works until it stops, its last
//     `stage_result` standing, and the runner asks for a report it forgot,
//     then makes it report on a turn that must ([stageConversation.NoteRequiring]),
//     then makes one for it ([stageConversation.Infer]). The budget's wall,
//     [factoryStageWall], bounds what it hands out to tasks, and the runner
//     bounds the round with the same figure;
//   - it runs at the ALLOW POSTURE, what --yolo gives, behind the headless
//     gate ([stageApprovalGate]): a stage that writes must be able to edit and
//     run things in its checkout, and nothing it does may wait on a person who
//     is not there;
//   - the doors that raise a card for a person (factory_add, factory_recipe,
//     factory_item) are taken off, because no person is there to answer one
//     and a card nobody answers holds the round forever.

// stageMaker opens stage conversations from parent, the launch's own config.
type stageMaker struct {
	floor *store.Store
	// jobs is where the job a step runs for is kept: the worker's mirror of
	// the job the factory leased it. Nil is floor.
	jobs       *store.Store
	parent     session.Config
	current    func() session.Config
	refusal    func() error
	workspace  string
	profileDir string
}

// factoryStageMaker is this process's maker.
func factoryStageMaker(floor *store.Store, workspace, profileDir string, parent session.Config) factoryrun.ConversationMaker {
	return stageMaker{floor: floor, parent: parent, workspace: workspace, profileDir: profileDir}
}

// factoryStageMakerFrom reads the worker's current launch for each round,
// including a resumed round whose journal predates the one-model launch.
func factoryStageMakerFrom(floor *store.Store, workspace, profileDir string, current func() session.Config) stageMaker {
	return stageMaker{floor: floor, current: current, workspace: workspace, profileDir: profileDir}
}

// withJobs reads and books a step's money on jobs, the worker's mirror.
func (m stageMaker) withJobs(jobs *store.Store) stageMaker {
	m.jobs = jobs
	return m
}

// withRefusal gives the maker the reason its parent was last refused.
func (m stageMaker) withRefusal(why func() error) stageMaker {
	m.refusal = why
	return m
}

// Open makes the conversation, joins it to the item's team, and starts its
// first turn on the brief.
func (m stageMaker) Open(ctx context.Context, spec factoryrun.ConversationSpec) (factoryrun.Conversation, error) {
	// A PARENT THE SAVED ONE-MODEL POLICY REFUSED OPENS NOTHING: no session
	// folder is minted and the person reads the reason, not "model is required".
	if m.current != nil {
		if err := refusalOf(m.current(), m.refusal); err != nil {
			return nil, err
		}
	}
	where := strings.TrimSpace(spec.Dir)
	if where == "" {
		where = strings.TrimSpace(m.workspace)
	}
	if where == "" {
		return nil, errors.New("codeaf does not know which folder this stage's conversation belongs in")
	}
	bucket, err := v3ProjectDir(where)
	if err != nil {
		return nil, err
	}
	// A RESUMED STAGE REOPENS ITS OWN CHAT: the transcript a pause cut short,
	// not a new folder. It is a member of its team already.
	resumed := strings.TrimSpace(spec.Resume) != ""
	if resumed {
		// A window's view of the paused chat lets go first (factory_embed.go).
		if err := stageTakeJournal(spec.Resume); err != nil {
			return nil, err
		}
	}
	cfg, err := m.stageConfig(spec, bucket, where)
	if err != nil {
		return nil, err
	}
	if err := markFactoryStageChat(m.floor, spec.Item, cfg.SessionFile); err != nil {
		return nil, err
	}
	cfg.Workspace = where
	// THE JOB'S OWN MODEL, WHEN A PERSON CHOSE ONE, over the launch's default.
	cfg.AutomaticModels = spec.AutomaticModels
	if model := strings.TrimSpace(spec.Model); model != "" {
		cfg.Model = model
	}
	fallback := appDefaultFallback(m.profileDir)
	if cfg.OneModel || fallback == cfg.Model {
		fallback = ""
	}
	cfg.ConversationModels = &session.ConversationModels{Main: cfg.Model, Fallback: fallback}
	verification := &receiptStageDoor{StageDoor: spec.Stage, file: cfg.SessionFile, dir: where, repair: spec.VerifyRepair, requireFresh: spec.VerifyChecks}
	cfg.Stage = verification
	cfg.Interactive = false
	cfg.Unattended = true
	cfg.Budget = session.Budget{Wall: factoryStageWall}
	cfg.AskConsent = false
	cfg.Factory, cfg.Recipe, cfg.FactoryItem, cfg.Floor = nil, nil, nil, nil
	// AND NO STAGE SHAPES THE RUN: `factory_run` is the manager's.
	cfg.FactoryRun = nil
	// Stages carry no manager tool authority. Their separate live meter charges
	// the same job before round completion, including delegated provider calls.
	cfg.JobChat = nil
	jobs := m.jobs
	if jobs == nil {
		jobs = m.floor
	}
	budget := &stageBudget{floor: jobs, item: spec.Item}
	cfg.JobBudget = budget
	gate := stageApprovalGate{rows: &v3ApprovalGate{workspace: where, profileDir: m.profileDir, headless: true}, posture: factoryStagePosture()}
	cfg.ApprovalGate = gate
	cfg.ApprovalPosture = gate.posture
	if cfg.ApprovalPolicy, cfg.Guardian, err = gate.Build(cfg.ApprovalPosture); err != nil {
		return nil, err
	}
	// NO DESIGNER, NO CARDS, NO ADAPTIVE RUNS: each of those reaches a person
	// through a lane, and this conversation has no person on any lane.
	cfg, open := v3Shape(cfg, v3Lanes{})
	agent, cfg, _, err := openV3Agent(cfg, where, open)
	if err != nil {
		return nil, err
	}
	// THE APP MAY LEND THIS AGENT, AND ONLY WHILE IT IS A RUNNING STAGE: it is
	// known from the moment its journal is open (a crew tick landing between the
	// open and a later register would find a live, unregistered agent and read
	// it as another window's) to Close or to a failed start (app_lend.go).
	if spec.Effort != "" {
		agent.SetConversationEffort(spec.Effort)
	}
	verification.workRunning = agent.StageHasChecks
	agent.ArmStageJobPark(factoryStageWall)
	rememberStageAgent(agent)
	if team := strings.TrimSpace(spec.Team); team != "" && !resumed {
		if err := stageJoinTeam(m.profileDir, team, cfg.SessionFile, where, spec.Name, spec.Handle, spec.Brief); err != nil {
			forgetStageAgent(agent)
			_ = agent.Close()
			return nil, err
		}
	}
	c := &stageConversation{
		agent:                agent,
		budget:               budget,
		file:                 cfg.SessionFile,
		dir:                  where,
		captions:             make(chan string, 256),
		progress:             spec.Progress,
		verificationProgress: spec.Verification,
		done:                 make(chan struct{}),
		verification:         verification,
		closing:              make(chan struct{}),
	}
	brief := spec.Brief + inheritFactoryChecks(spec, cfg.SessionFile, where)
	if resumed {
		brief = session.CarryOn(brief)
	}
	events, err := agent.Submit(context.WithoutCancel(ctx), brief)
	if err != nil {
		forgetStageAgent(agent)
		_ = agent.Close()
		return nil, err
	}
	c.wakes, c.stopWakes = agent.WatchWakes()
	done := c.done
	if c.verificationProgress != nil {
		c.followers.Add(1)
		guard.Go("factory/stage-check-progress", func() { defer c.followers.Done(); c.watchVerification() })
	}
	c.followers.Add(1)
	guard.Go("factory/stage-conversation", func() { defer c.followers.Done(); c.follow(events, done) })
	return c, nil
}

// stageConfig aims the launch's config at the stage's conversation: the
// folder a pause cut short when the spec resumes one, else a new folder.
func (m stageMaker) stageConfig(spec factoryrun.ConversationSpec, bucket, where string) (session.Config, error) {
	parent := m.parent
	if m.current != nil {
		parent = m.current()
	}
	if err := refusalOf(parent, m.refusal); err != nil {
		return parent, err
	}
	if resume := strings.TrimSpace(spec.Resume); resume != "" {
		if _, err := os.Stat(resume); err != nil {
			return parent, err
		}
		return v3Reopen(parent, resume, where)
	}
	place, err := v3MintSession(bucket, where, v3StampLaunchDir(v3LaunchDir(), where), false)
	if err != nil {
		return parent, err
	}
	return v3PointAt(parent, place)
}

// factoryPostureEnv pins the posture a stage conversation runs at, in the
// words `/approvals` takes. Unset, or a word that is not a posture, is
// [session.PostureAllow].
const factoryPostureEnv = "CODEAF_FACTORY_POSTURE"

// factoryStagePosture is the posture every stage conversation runs at.
func factoryStagePosture() string {
	word := strings.ToLower(strings.TrimSpace(env.Get(factoryPostureEnv)))
	for _, p := range session.ApprovalPostures {
		if word == p {
			return word
		}
	}
	return session.PostureAllow
}

// stageApprovalGate is a stage conversation's door onto its gate. Every
// posture but allow is the person's own rows, headless (rows); allow is the
// open gate WITHOUT the person's rows under it.
//
// A STAGE'S ALLOW IS NOT NARROWED BY THE PERSON'S OWN SETTINGS. A tool row or
// a bash rule a person wrote saying `ask` is a question for them at their
// keyboard, and a stage has no keyboard: under --yolo's gate that row would be
// a refusal (`needs approval but no resolver is attached`), and a write stage
// whose `edit` or `bash` is refused cannot do the one thing it is for. So the
// stage's allow is the built-in floor and nothing else. internal/approval's
// own floors (the critical-command table, the calls that act in the person's
// name) still hold, because they sit under every policy and they are not a
// person's preference. A person who wants a stage narrower says so with
// [factoryPostureEnv].
type stageApprovalGate struct {
	rows    *v3ApprovalGate
	posture string
}

// Build is the gate for one posture.
func (g stageApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	if posture != session.PostureAllow {
		return g.rows.Build(posture)
	}
	policy, err := stageAllowPolicy()
	return policy, false, err
}

// Every stage's allow policy has the same inputs and is immutable after build.
var stageAllowPolicy = sync.OnceValues(func() (*approval.Policy, error) {
	policy, err := approval.Load(map[string]any{"default": string(approval.ActionAllow), "tools": v3BuiltinApprovals()})
	return &policy, err
})

// Standing is the posture the stage stands at.
func (g stageApprovalGate) Standing() string { return g.posture }

// stageJoinTeam adds the conversation to the item's team, by the name the
// executor gave it (`#12 · review`). The membership is stamped FactoryStage and
// carries brief, the [factoryrun.ConversationSpec.Brief] the stage was really
// started on, so the app reads it back instead of guessing from the transcript.
// IT IS THE OPENING BRIEF AND ONLY THAT: a resumed stage is never joined again
// (Open skips this), because what a resume is given is a carry-on note
// ([session.CarryOn]) and not the brief the stage began on.
//
// A ROOM CLOSED UNDER A RUNNING ROUND STILL TAKES ITS STAGES. A person may
// close the job's room, or the `factory` crew above it, while a round is
// going, and a job may end with a round still opening stages: the stage joins
// the room closed, as it always has, and the room stays as it was left. The
// open crews are written first, which is where the room almost always is; only
// a room not among them is joined through the whole list.
func stageJoinTeam(profileDir, team, transcript, where, name, handle, brief string) error {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	now := time.Now()
	join := func(f *teams.File) error {
		return f.AddMember(team, teams.Member{Key: key, File: transcript, Where: where, Word: name, Handle: handle, JoinedAt: now, FactoryStage: true, FactoryBrief: redact.Secrets(brief)})
	}
	open := false
	err := teams.UpdateOpen(profileDir, func(f *teams.File) error {
		if _, open = f.Team(team); !open {
			return nil
		}
		return join(f)
	})
	if err != nil || open {
		return err
	}
	return teams.Update(profileDir, join)
}

// stageSettle is how long the conversation must have been still before it
// counts as idle: the same tick the one-message door waits out, for its
// reason (a landing hands its lane back a moment before the wake is queued).
var stageSettle = unattendedRunSettleTick

// stageConversation is one open stage conversation.
type stageConversation struct {
	budget               *stageBudget
	agent                *session.Agent
	file                 string
	dir                  string
	captions             chan string
	progress             func(string)
	verificationProgress func(*factory.StepVerification)
	wakes                <-chan (<-chan session.Event)
	stopWakes            func()
	done                 chan struct{}
	verification         *receiptStageDoor
	followers            sync.WaitGroup
	closing              chan struct{}

	mu      sync.Mutex
	failure error
	closed  bool
}

// follow drains the first turn, then every turn a landing wakes, into the
// captions, and closes done when the conversation has been idle for a whole
// [stageSettle]: its turn ended AND nothing it handed out is still running.
func (c *stageConversation) follow(first <-chan session.Event, done chan struct{}) {
	c.drain(first)
	settled := false
	for {
		select {
		case <-c.closing:
			c.closeDone(done)
			return
		default:
		}
		if c.agent.StageStillGoing() {
			settled = false
		} else if settled {
			if c.settle(done) {
				return
			}
			settled = false
		} else {
			settled = true
		}
		t := time.NewTimer(stageSettle)
		select {
		case <-c.closing:
			t.Stop()
			c.closeDone(done)
			return
		case stream, open := <-c.wakes:
			t.Stop()
			if !open {
				c.closeDone(done)
				return
			}
			settled = false
			c.drain(stream)
		case <-t.C:
		}
	}
}

// closeDone closes done under the lock [stageConversation.deliver] reads it under.
func (c *stageConversation) closeDone(done chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(done)
}

// settle closes done when the conversation is still idle, under the lock
// [stageConversation.deliver] starts a next turn under: a note that lands in the
// same breath either starts its turn first, which this reads as going, or
// finds done closed and follows its turn again.
func (c *stageConversation) settle(done chan struct{}) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.agent.SettleStage() {
		return false
	}
	close(done)
	return true
}

// drain reads one turn's events into captions and keeps its failure.
func (c *stageConversation) drain(events <-chan session.Event) {
	// Streamed text is provisional until a journal boundary. Never relay
	// reasoning, failed/retried fragments, raw tool output or partial responses.
	var prose strings.Builder
	flush := func() {
		if c.progress != nil && prose.Len() > 0 {
			c.progress(prose.String())
		}
		prose.Reset()
	}
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			if c.progress != nil && prose.Len() < 4*1024 {
				text := event.Text
				if len(text) > 4*1024-prose.Len() {
					text = text[:4*1024-prose.Len()]
				}
				prose.WriteString(text)
			}
		case session.EventAssistantDone:
			flush()
		case session.EventRetrying:
			prose.Reset()
		case session.EventToolBegin:
			flush()
			hint := c.relativeHint(event.Hint)
			if hint != "" {
				c.say(event.Tool + " " + hint)
			} else {
				c.say(event.Tool)
			}
		case session.EventToolFailed:
			reason := event.Hint
			if reason == "" && event.Err != nil {
				reason = event.Err.Error()
			}
			c.say(event.Tool + " failed: " + reason)
		case session.EventNotice:
			// THE ACCEPTANCE NOTICE IS THE STAGE'S OWN BUSINESS: every stage
			// would log a `done when · …` line, which says nothing to the
			// person reading the item's log.
			if !strings.HasPrefix(event.Text, "done when · ") {
				c.say(event.Text)
			}
		case session.EventError:
			prose.Reset()
			c.fail(event.Err)
		}
	}
}

// fail keeps a turn's failure, in words when it came with none.
//
// A TURN A SPENDING LIMIT REFUSED IS HANDED ON AS A STOP ([factoryrun.SpendStop]),
// told apart by what refused it and never by its words, so the round ends on
// the limit rather than trying a turn that would be refused again.
func (c *stageConversation) fail(err error) {
	if err == nil {
		err = errors.New("the turn failed without a reason")
	}
	if errors.Is(err, session.ErrSpendRail) || errors.Is(err, session.ErrSpendUnverified) {
		err = factoryrun.SpendStop{Daily: session.IsDailyLimit(err), Said: err.Error()}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failure = err
}

// say puts one caption on the account, dropping it rather than holding the
// conversation up when nobody is reading.
func (c *stageConversation) say(line string) {
	if line = strings.TrimSpace(line); line == "" {
		return
	}
	select {
	case c.captions <- line:
	default:
	}
}

// Send hands the person's words to the conversation: into the turn when one
// is running, as the next turn when none is.
func (c *stageConversation) Send(_ context.Context, words string) error {
	return c.deliver(words)
}

// Note hands codeaf's own words to the conversation the same way, as a
// carry-on note ([session.CarryOn]) so the chat never shows them as the
// person's.
func (c *stageConversation) Note(_ context.Context, words string) error {
	return c.deliver(session.CarryOn(words))
}

// NoteRequiring is Note on a turn of its own whose first answer must call
// tool (internal/session's FollowUpRequiring): never steered into a running
// turn, which owes nothing, but queued behind it when one is running.
func (c *stageConversation) NoteRequiring(_ context.Context, words, tool string) error {
	return c.deliverWith(session.CarryOn(words), func(text string) (<-chan session.Event, error) {
		return c.agent.FollowUpRequiring(text, tool)
	})
}

// LastWords is the last thing the conversation's model said in words.
func (c *stageConversation) LastWords() string { return c.agent.LastWords() }

// Infer makes the step's report in one short call apart from the
// conversation's turns (internal/session's InferStageResult).
func (c *stageConversation) Infer(ctx context.Context, ask string) (factory.StageResult, error) {
	return c.agent.InferStageResult(ctx, ask)
}

// deliver steers the running turn, or starts the next one. A conversation that
// had already gone idle is followed again from that turn, so [Wait] answers
// when it is idle once more rather than at once.
func (c *stageConversation) deliver(words string) error {
	return c.deliverWith(words, func(text string) (<-chan session.Event, error) {
		events, err := c.agent.Steer(text)
		if errors.Is(err, session.ErrNothingToSteer) {
			events, err = c.agent.FollowUp(text)
		}
		return events, err
	})
}

// deliverWith is deliver with the way the words reach the agent given.
func (c *stageConversation) deliverWith(words string, start func(string) (<-chan session.Event, error)) error {
	events, done, idle, err := c.startTurn(words, start)
	if err != nil {
		return err
	}
	if idle {
		guard.Go("factory/stage-conversation", func() { defer c.followers.Done(); c.follow(events, done) })
		return nil
	}
	guard.Go("factory/stage-steer", func() { defer c.followers.Done(); c.drain(events) })
	return nil
}

// startTurn hands the words to the agent through start, under the lock
// [stageConversation.settle] closes done under, and answers whether the
// conversation had gone idle (so done is a new one, to follow again).
func (c *stageConversation) startTurn(words string, start func(string) (<-chan session.Event, error)) (<-chan session.Event, chan struct{}, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, false, errors.New("this stage conversation is closed")
	}
	c.agent.ReopenStage()
	events, err := start(words)
	if err != nil {
		return nil, nil, false, err
	}
	idle := false
	select {
	case <-c.done:
		idle = true
		c.done, c.failure = make(chan struct{}), nil
	default:
	}
	c.followers.Add(1)
	return events, c.done, idle, nil
}

// currentDone is the channel the conversation's current turn closes when idle.
func (c *stageConversation) currentDone() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

// Wait returns when the conversation is idle, or when ctx ends.
func (c *stageConversation) Wait(ctx context.Context) error {
	done := c.currentDone()
	select {
	case <-done:
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.budget != nil {
			if why := c.budget.Refusal(); why != "" {
				return factoryrun.SpendStop{Job: c.budget.reachedCap(), Said: why}
			}
		}
		return c.failure
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Captions is the conversation's running account.
func (c *stageConversation) Captions(context.Context) <-chan string { return c.captions }

// VerificationStopped lets the runner close an agent that repeatedly reports
// the same rejected repair, even while its model turn is otherwise still busy.
func (c *stageConversation) VerificationStopped() <-chan string {
	if c.verification == nil {
		return nil
	}
	return c.verification.stoppedVerification()
}

// ID is the conversation's session file, which is how a surface opens it.
func (c *stageConversation) ID() string { return c.file }

// Spent is what the conversation has cost, its tasks folded in.
func (c *stageConversation) Spent() float64 {
	if c.budget != nil {
		return c.budget.total()
	}
	return c.agent.Usage().CostUSD
}
func (c *stageConversation) SpendRecorded() bool { return c.budget != nil }

// Model is the model the conversation is on now.
func (c *stageConversation) Model() string { return c.agent.Model() }

func (c *stageConversation) SetAutomaticModels(enabled bool) error {
	return c.agent.SetAutomaticModels(enabled)
}

func (c *stageConversation) LastModel() string { return c.agent.LastModel() }

// SetModel switches the conversation for its next turn, as a person's pick
// does in any chat.
func (c *stageConversation) SetModel(model string) { c.agent.SetModel(model) }

// Close interrupts a turn still in flight and lets go of the conversation;
// its folder stays where it was made, to be opened.
func (c *stageConversation) Close() {
	if !c.closeOnce() {
		return
	}
	if c.verification != nil {
		c.verification.closeVerification()
	}
	c.stopWakes()
	forgetStageAgent(c.agent)
	c.agent.Interrupt()
	_ = c.agent.Close()
	c.followers.Wait()
}

// closeOnce answers true the first time it is asked, and false after.
func (c *stageConversation) closeOnce() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.closed = true
	if c.closing != nil {
		close(c.closing)
	}
	return true
}

// factoryChatStage wraps the chat executor so every round has the item's team
// to open its conversation in: the item's Stream.Room when it carries one,
// else the item's team under `factory` (the one `T` made, by its name) or a
// new one, written onto the item before the round starts.
func factoryChatStage(st *store.Store, profileDir string, inner factoryrun.Executor) factoryrun.Executor {
	return factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
		factoryReconcileOnce(profileDir, st)
		if job.Item.Stream != nil && strings.TrimSpace(job.Item.Stream.Room) == "" {
			if team, err := factoryItemTeam(profileDir, job.Item); err == nil && team != "" {
				_ = st.Annotate(job.Item.ID, func(it *factory.Item) error {
					if it.Stream != nil {
						it.Stream.Room = team
					}
					return nil
				})
				s := *job.Item.Stream
				s.Room = team
				job.Item.Stream = &s
			}
		}
		if job.Item.Stream != nil {
			if room := strings.TrimSpace(job.Item.Stream.Room); room != "" {
				// A live job's room that its end closed opens again here, should
				// the reopen that followed the job's return have been lost
				// (factory_room_follow.go).
				if !jobOver(job.Item) {
					_ = reopenJobRoom(profileDir, job.Item)
				}
				local, err := factoryEnsureRoom(profileDir, job.Item, room)
				if err != nil {
					return factory.StageResult{}, err
				}
				if local == "" {
					// Keep the logical id on the mirror; only this round's local view is teamless.
					s := *job.Item.Stream
					s.Room = ""
					job.Item.Stream = &s
				} else {
					_ = factoryStampRoom(profileDir, local)
				}
			}
		}
		return inner.Run(ctx, job)
	})
}

// factoryStampRoom is the legacy recovery for a team made before the stamp was
// kept: an item's Stream.Room is the factory's own record of its team, so that
// team (and its parent, when that parent is the `factory` team) is marked. It
// reads the item's reference and never the team's name or path, and a Room
// that is not in the file stamps nothing.
func factoryStampRoom(profileDir, room string) error {
	return factoryStampRooms(profileDir, []string{room})
}

// factoryStampRooms is [factoryStampRoom] for several rooms in one write.
func factoryStampRooms(profileDir string, rooms []string) error {
	return teams.Update(profileDir, func(f *teams.File) error {
		for i := range f.Teams {
			if !slices.Contains(rooms, f.Teams[i].ID) {
				continue
			}
			f.Teams[i].FactoryManaged = true
			parent := f.Teams[i].Parent
			for j := range f.Teams {
				if f.Teams[j].ID == parent && f.Teams[j].Name == factoryTeamName {
					f.Teams[j].FactoryManaged = true
				}
			}
		}
		return nil
	})
}

// factoryStampStoredRooms reconciles teams the factory made before the stamp
// was kept: every item already in the local factory store at root that records
// a Stream.Room stamps that team (factoryStampRoom). It reads only a store that
// is already there, makes nothing, calls no model or network, and an item with
// no recorded Room stamps nothing. A factory on another machine is not in this
// store; its teams are stamped by the engine that runs it.
func factoryStampStoredRooms(profileDir, root string) {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return
	}
	st, err := store.Open(root)
	if err != nil {
		return
	}
	factoryStampStore(profileDir, st)
}

// factoryReconciled remembers the (profile, store) pairs whose legacy rooms
// this process has already stamped.
var factoryReconciled sync.Map

// factoryReconcileOnce runs [factoryStampStore] once per profile and store in
// this process, BEFORE any factory door makes or joins a team. An old teams
// file has an unstamped `factory` parent and item teams the stamp never
// reached; without this a headless engine would find no stamped parent, make a
// second one, and split a job across two crews. It makes nothing, calls no
// model or network, and does not touch a store it was not given.
func factoryReconcileOnce(profileDir string, st *store.Store) {
	if st == nil {
		return
	}
	type key struct{ profile, root string }
	canonical := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	// Concurrent doors must WAIT for reconciliation, including separate handles of the same store.
	once, _ := factoryReconciled.LoadOrStore(key{canonical(teams.Path(profileDir)), canonical(st.Root())}, &sync.Once{})
	once.(*sync.Once).Do(func() { factoryStampStore(profileDir, st) })
}

// factoryStampStore is the body of [factoryStampStoredRooms] over an open store.
func factoryStampStore(profileDir string, st *store.Store) {
	items, err := st.List()
	if err != nil {
		return
	}
	var rooms []string
	chats := map[string][]string{}
	local, err := teams.Snapshot(profileDir)
	if err != nil {
		return
	}
	for _, it := range items {
		if it.Stream == nil {
			continue
		}
		room := strings.TrimSpace(it.Stream.Room)
		if room == "" || !factoryRoomOwned(local, it, room) {
			continue
		}
		rooms = append(rooms, room)
		for _, ph := range it.Stream.Phases {
			if chat := strings.TrimSpace(ph.Chat); chat != "" {
				chats[room] = append(chats[room], chat)
			}
		}
	}
	// A job run before Stream.Room was persisted gets its room back only on
	// exact stage-membership proof (factory_legacy_rooms.go).
	recovered, recoveredChats := factoryRecoverLegacyRooms(profileDir, st, items)
	rooms = append(rooms, recovered...)
	for room, paths := range recoveredChats {
		chats[room] = append(chats[room], paths...)
	}
	if len(rooms) > 0 {
		_ = factoryStampRooms(profileDir, rooms)
		_ = factoryStampRoomStages(profileDir, chats)
	}
}

// factoryStampRoomStages marks as factory stages the members the factory's own
// record names: chats maps a room (team id) to the Phase.Chat values of its
// item, and Phase.Chat is the stage conversation's transcript path, the very
// path stageJoinTeam stored as the member's File (and, resolved, its Key).
// ONLY AN EXACT PATH IN THAT ROOM STAMPS: never a handle, a word or a same-name
// member, and a member of another team holding the same path is untouched.
// FactoryBrief is left as it is, because what the stage was first told is not
// in the record and is never read back out of the transcript; a membership that
// already carries a brief keeps it.
func factoryStampRoomStages(profileDir string, chats map[string][]string) error {
	return teams.Update(profileDir, func(f *teams.File) error {
		for i := range f.Teams {
			paths := chats[f.Teams[i].ID]
			if len(paths) == 0 {
				continue
			}
			want := map[string]bool{}
			for _, p := range paths {
				want[filepath.Clean(p)] = true
				if real, err := filepath.EvalSymlinks(p); err == nil {
					want[real] = true
				}
			}
			for j := range f.Teams[i].Members {
				m := &f.Teams[i].Members[j]
				if m.File != "" && m.Key != "" && want[filepath.Clean(m.File)] && want[filepath.Clean(m.Key)] {
					m.FactoryStage = true
				}
			}
		}
		return nil
	})
}

// factoryItemTeam is the item's team id: the open team named for it under the
// one `factory` team, found or made in one read-modify-write of the teams
// file.
func factoryItemTeam(profileDir string, it factory.Item) (string, error) {
	name := talkTeamName(it)
	now := time.Now()
	id := ""
	err := teams.UpdateOpen(profileDir, func(f *teams.File) error {
		id = itemTeamIn(f, name, now)
		return nil
	})
	return id, err
}

// relativeHint spends the visible log on the filename and command, not a home path.
func (c *stageConversation) relativeHint(s string) string {
	if c.dir == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.dir+string(os.PathSeparator), "")
	return strings.ReplaceAll(s, c.dir, ".")
}

// watchVerification relays bounded structured facts even while a model turn parks.
// It never wakes a model or prints receipt bookkeeping into the conversation.
func (c *stageConversation) watchVerification() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	defer c.verificationProgress(nil)
	last := ""
	for {
		current, _ := verification.Current(c.file)
		status := liveStageVerification(current, c.agent.StageChecks())
		raw, _ := json.Marshal(status)
		if string(raw) != last {
			last = string(raw)
			c.verificationProgress(status)
		}
		select {
		case <-c.closing:
			return
		case <-tick.C:
		}
	}
}

// A nullable old receipt is not liveness. Match the exact currently owned
// native tool call, including foreground checks and promoted background work.
func liveStageVerification(current []verification.CurrentCheck, live []session.StageCheckProgress) *factory.StepVerification {
	ids := make(map[string]time.Time, len(live))
	for _, check := range live {
		if check.CallID != "" {
			ids[check.CallID] = check.Started
		}
	}
	var status *factory.StepVerification
	for _, check := range current {
		started, owned := ids[check.CallID]
		if check.Outcome != "running" || check.CallID == "" || !owned {
			continue
		}
		// Provider call IDs may repeat across turns. A receipt from before the
		// current physical process cannot borrow its new liveness.
		if !started.IsZero() && (check.StartedAt.IsZero() || check.StartedAt.Before(started)) {
			continue
		}
		if status == nil {
			status = &factory.StepVerification{State: "running"}
		}
		if len(status.Checks) >= 8 {
			break
		}
		status.Checks = append(status.Checks, factory.RunningCheck{ID: check.ID, Purpose: check.Purpose, StartedAt: check.StartedAt})
	}
	return status
}

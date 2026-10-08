package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui3"
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
//   - it is UNATTENDED, under a wall of [factoryStageWall]: nobody is at its
//     keyboard, so it keeps going on its own until its work, and the tasks it
//     handed out, are done;
//   - it runs at the ALLOW POSTURE, what --yolo gives, behind the headless
//     gate ([stageApprovalGate]): a stage that writes must be able to edit and
//     run things in its checkout, and nothing it does may wait on a person who
//     is not there;
//   - the doors that raise a card for a person (factory_add, factory_recipe,
//     factory_item) are taken off, because no person is there to answer one
//     and a card nobody answers holds the round forever.

// stageMaker opens stage conversations from parent, the launch's own config.
type stageMaker struct {
	parent     session.Config
	workspace  string
	profileDir string
}

// factoryStageMaker is this process's maker.
func factoryStageMaker(_ *store.Store, workspace, profileDir string, parent session.Config) factoryrun.ConversationMaker {
	return stageMaker{parent: parent, workspace: workspace, profileDir: profileDir}
}

// Open makes the conversation, joins it to the item's team, and starts its
// first turn on the brief.
func (m stageMaker) Open(ctx context.Context, spec factoryrun.ConversationSpec) (factoryrun.Conversation, error) {
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
	place, err := v3MintSession(bucket, where, v3StampLaunchDir(v3LaunchDir(), where), false)
	if err != nil {
		return nil, err
	}
	cfg, err := v3PointAt(m.parent, place)
	if err != nil {
		return nil, err
	}
	cfg.Workspace = where
	cfg.Stage = spec.Stage
	cfg.Interactive = false
	cfg.Unattended = true
	cfg.Budget = session.Budget{Wall: factoryStageWall}
	cfg.AskConsent = false
	cfg.Factory, cfg.Recipe, cfg.FactoryItem, cfg.Floor = nil, nil, nil, nil
	gate := stageApprovalGate{rows: v3ApprovalGate{workspace: where, profileDir: m.profileDir, headless: true}, posture: factoryStagePosture()}
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
	if team := strings.TrimSpace(spec.Team); team != "" {
		if err := stageJoinTeam(m.profileDir, team, cfg.SessionFile, where, spec.Name); err != nil {
			_ = agent.Close()
			return nil, err
		}
	}
	c := &stageConversation{
		agent:    agent,
		file:     cfg.SessionFile,
		captions: make(chan string, 256),
		done:     make(chan struct{}),
	}
	events, err := agent.Submit(context.WithoutCancel(ctx), spec.Brief)
	if err != nil {
		_ = agent.Close()
		return nil, err
	}
	c.wakes, c.stopWakes = agent.WatchWakes()
	guard.Go("factory/stage-conversation", func() { c.follow(events) })
	return c, nil
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
	rows    v3ApprovalGate
	posture string
}

// Build is the gate for one posture.
func (g stageApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	if posture != session.PostureAllow {
		return g.rows.Build(posture)
	}
	policy, err := approval.Load(map[string]any{"default": string(approval.ActionAllow), "tools": v3BuiltinApprovals()})
	if err != nil {
		return nil, false, err
	}
	return &policy, false, nil
}

// Standing is the posture the stage stands at.
func (g stageApprovalGate) Standing() string { return g.posture }

// stageJoinTeam adds the conversation to the item's team, by the name the
// executor gave it (`#12 · review`).
func stageJoinTeam(profileDir, team, transcript, where, name string) error {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	key = filepath.Clean(key)
	now := time.Now()
	return teams.Update(profileDir, func(f *teams.File) error {
		return f.AddMember(team, teams.Member{Key: key, File: transcript, Where: where, Word: name, JoinedAt: now})
	})
}

// stageSettle is how long the conversation must have been still before it
// counts as idle: the same tick the one-message door waits out, for its
// reason (a landing hands its lane back a moment before the wake is queued).
var stageSettle = unattendedRunSettleTick

// stageConversation is one open stage conversation.
type stageConversation struct {
	agent     *session.Agent
	file      string
	captions  chan string
	wakes     <-chan (<-chan session.Event)
	stopWakes func()
	done      chan struct{}

	mu      sync.Mutex
	failure error
	closed  bool
}

// follow drains the first turn, then every turn a landing wakes, into the
// captions, and closes done when the conversation has been idle for a whole
// [stageSettle]: its turn ended AND nothing it handed out is still running.
func (c *stageConversation) follow(first <-chan session.Event) {
	defer close(c.done)
	c.drain(first)
	settled := false
	for {
		if c.agent.StillGoing() {
			settled = false
		} else if settled {
			return
		} else {
			settled = true
		}
		t := time.NewTimer(stageSettle)
		select {
		case stream, open := <-c.wakes:
			t.Stop()
			if !open {
				return
			}
			settled = false
			c.drain(stream)
		case <-t.C:
		}
	}
}

// drain reads one turn's events into captions and keeps its failure.
func (c *stageConversation) drain(events <-chan session.Event) {
	for event := range events {
		switch event.Kind {
		case session.EventToolBegin:
			c.say(tui3.ToolGloss(event.Tool, event.Hint))
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
			c.fail(event.Err)
		}
	}
}

// fail keeps a turn's failure, in words when it came with none.
func (c *stageConversation) fail(err error) {
	if err == nil {
		err = errors.New("the turn failed without a reason")
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
	events, err := c.agent.Steer(words)
	if errors.Is(err, session.ErrNothingToSteer) {
		events, err = c.agent.FollowUp(words)
	}
	if err != nil {
		return err
	}
	guard.Go("factory/stage-steer", func() { c.drain(events) })
	return nil
}

// Wait returns when the conversation is idle, or when ctx ends.
func (c *stageConversation) Wait(ctx context.Context) error {
	select {
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.failure
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Captions is the conversation's running account.
func (c *stageConversation) Captions(context.Context) <-chan string { return c.captions }

// ID is the conversation's session file, which is how a surface opens it.
func (c *stageConversation) ID() string { return c.file }

// Spent is what the conversation has cost, its tasks folded in.
func (c *stageConversation) Spent() float64 { return c.agent.Usage().CostUSD }

// Close interrupts a turn still in flight and lets go of the conversation;
// its folder stays where it was made, to be opened.
func (c *stageConversation) Close() {
	if !c.closeOnce() {
		return
	}
	c.stopWakes()
	c.agent.Interrupt()
	_ = c.agent.Close()
}

// closeOnce answers true the first time it is asked, and false after.
func (c *stageConversation) closeOnce() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.closed = true
	return true
}

// factoryChatStage wraps the chat executor so every round has the item's team
// to open its conversation in: the item's Stream.Room when it carries one,
// else the item's team under `factory` (the one `T` made, by its name) or a
// new one, written onto the item before the round starts.
func factoryChatStage(st *store.Store, profileDir string, inner factoryrun.Executor) factoryrun.Executor {
	return factoryrun.ExecutorFunc(func(ctx context.Context, job factoryrun.Job) (factory.StageResult, error) {
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
		return inner.Run(ctx, job)
	})
}

// factoryItemTeam is the item's team id: the open team named for it under the
// one `factory` team, found or made in one read-modify-write of the teams
// file.
func factoryItemTeam(profileDir string, it factory.Item) (string, error) {
	name := talkTeamName(it)
	now := time.Now()
	id := ""
	err := teams.Update(profileDir, func(f *teams.File) error {
		parent := factoryParentTeam(f, now)
		for _, t := range f.Teams {
			if t.Parent == parent && t.Name == name && !t.Closed() {
				id = t.ID
				return nil
			}
		}
		id = teams.NewID()
		f.Teams = append(f.Teams, teams.Team{ID: id, Name: name, Parent: parent, Made: now})
		return nil
	})
	return id, err
}

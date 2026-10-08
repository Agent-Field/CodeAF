package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MANAGER SHAPES THE RUN ─────────────────────────────────────────────
//
// The manager is the author of the run (the owner's decision of 2026-10-08).
// Two doors make it so, and this file is both:
//
//   - `factory_run` on the manager's own belt ([runDoor]): what the manager
//     sets while the person talks to it is made at once through the floor's
//     edit door ([factory.Seam.Edit]), before a run or on the stages a run has
//     not started;
//   - the runner's shaping turn ([shapeTurns], wired as Options.Shape and
//     Options.Reshape): at launch, and on a steer during a run, the runner
//     gives the manager ONE turn of its own conversation with an ask
//     (`Shape the run for this item now. …`), at the cheap thinking unless the
//     item reads as large, and collects the edit its `factory_run` call made,
//     for the runner to apply itself.
//
// THE SHAPING TURN COLLECTS, IT DOES NOT WRITE. Its `factory_run` door checks
// the edit against the item in hand ([factory.Edit]) and answers the line, so
// the manager is told what it set, but the item is changed only by the runner,
// under its own write, with the phases rebuilt in the same step.

// The runner's asks, said once so the manual and the tests quote the same
// words.
const (
	shapeAsk     = "Shape the run for this item now."
	shapeAskSaid = " What the person said: %s"
	reshapeAsk   = "The person said: %s · reshape the stages not yet started if that is what they mean, else leave them"
)

// shapeAskFor is the launch's ask, with what the person typed before the run.
func shapeAskFor(said []string) string {
	var words []string
	for _, s := range said {
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			words = append(words, s)
		}
	}
	if len(words) == 0 {
		return shapeAsk
	}
	return shapeAsk + fmt.Sprintf(shapeAskSaid, strings.Join(words, " · "))
}

// managerTurn is what one shaping turn needs of an open conversation; the
// session's *Agent answers it, and a test hands in a fake.
type managerTurn interface {
	SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error)
	Interrupt()
	Close() error
}

// shapeTurns gives an item's manager one turn to shape its run.
type shapeTurns struct {
	dirs func(repo string) string
	// open opens the item's manager conversation for one turn, with door as
	// its `factory_run`, at the cheap thinking when cheap.
	open func(ctx context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error)
}

// factoryShapeTurns is this process's shaping turns, opened from parent, the
// launch's own config.
func factoryShapeTurns(st *store.Store, workspace, profileDir string, parent session.Config) *shapeTurns {
	return &shapeTurns{
		dirs: factoryRepoDirs(st, workspace),
		open: func(_ context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
			return openManagerTurn(parent, workspace, profileDir, it, door, cheap)
		},
	}
}

// Shape is the runner's Options.Shape: the launch's one turn.
func (s *shapeTurns) Shape(ctx context.Context, it factory.Item, said []string) (factory.RunEdit, string, error) {
	return s.run(ctx, it, shapeAskFor(said), false)
}

// Reshape is the runner's Options.Reshape: one turn on a steer, the tail only.
func (s *shapeTurns) Reshape(ctx context.Context, it factory.Item, said string) (factory.RunEdit, string, error) {
	return s.run(ctx, it, fmt.Sprintf(reshapeAsk, strings.Join(strings.Fields(said), " ")), true)
}

// shapeCheap says whether the shaping turn thinks cheap: always, unless the
// item's read says it is large.
func shapeCheap(it factory.Item) bool {
	return !strings.EqualFold(strings.TrimSpace(it.Triage.Size), "L")
}

// run is one turn: the ask in, the turn drained, and the edit collected.
func (s *shapeTurns) run(ctx context.Context, it factory.Item, ask string, inRun bool) (factory.RunEdit, string, error) {
	if s == nil || s.open == nil {
		return factory.RunEdit{}, "", errors.New("no manager to ask")
	}
	door := &collectRunDoor{it: it, recipe: factoryItemRecipe(s.dirs, it), inRun: inRun}
	turn, err := s.open(ctx, it, door, shapeCheap(it))
	if err != nil {
		return factory.RunEdit{}, "", err
	}
	defer turn.Close()
	events, err := turn.SubmitRunnerNote(context.WithoutCancel(ctx), ask)
	if err != nil {
		return factory.RunEdit{}, "", err
	}
	var reply strings.Builder
	var failure error
	for {
		select {
		case ev, open := <-events:
			if !open {
				if failure != nil {
					return factory.RunEdit{}, "", failure
				}
				return door.collected(), strings.TrimSpace(reply.String()), nil
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
			return factory.RunEdit{}, "", ctx.Err()
		}
	}
}

// collectRunDoor is `factory_run` on a shaping turn: each call is checked
// against the item as the calls before it left it, answered with its line,
// and kept, and nothing is written.
type collectRunDoor struct {
	mu     sync.Mutex
	it     factory.Item
	recipe factory.Recipe
	inRun  bool
	got    factory.RunEdit
}

// EditRun checks and keeps one edit.
func (d *collectRunDoor) EditRun(_ context.Context, _ string, e factory.RunEdit) (factory.Item, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var next factory.Item
	var lines []string
	var err error
	if d.inRun {
		next, lines, err = factory.Edit(d.it, e, d.recipe, factory.EditInRun)
	} else {
		next, lines, err = factory.Edit(d.it, e, d.recipe, factory.EditBeforeRun)
	}
	if err != nil {
		return factory.Item{}, nil, err
	}
	d.it = next
	d.got = mergeRunEdit(d.got, e)
	return next, lines, nil
}

// collected is every edit the turn made, as one.
func (d *collectRunDoor) collected() factory.RunEdit {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.got
}

// mergeRunEdit is two edits as one, the later winning where both set a stage.
func mergeRunEdit(a, b factory.RunEdit) factory.RunEdit {
	out := a
	out.Add = append(append([]factory.Added(nil), a.Add...), b.Add...)
	merge := func(x, y map[string]string) map[string]string {
		if len(x) == 0 && len(y) == 0 {
			return nil
		}
		m := map[string]string{}
		for k, v := range x {
			m[k] = v
		}
		for k, v := range y {
			m[k] = v
		}
		return m
	}
	out.Ask = merge(a.Ask, b.Ask)
	out.Thinking = merge(a.Thinking, b.Thinking)
	out.On = append(append([]string(nil), a.On...), b.On...)
	out.Skip = append(append([]string(nil), a.Skip...), b.Skip...)
	if strings.TrimSpace(b.Why) != "" {
		out.Why = b.Why
	}
	if strings.TrimSpace(b.By) != "" {
		out.By = b.By
	}
	return out
}

// openManagerTurn opens the item's manager conversation for one turn. It is a
// conversation nobody is at the keyboard of for this turn: no card door is on
// its belt (a card nobody answers holds the turn until the wait ends), the
// approvals are the person's own rows, headless, and `factory_run` is door.
// A conversation another window holds is refused here, and the run goes on
// with the recipe; the window's own manager answers the person there.
func openManagerTurn(parent session.Config, workspace, profileDir string, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
	transcript := strings.TrimSpace(it.Talk)
	if transcript == "" {
		return nil, errors.New("the item has no manager conversation")
	}
	cfg, err := v3Reopen(parent, transcript, workspace)
	if err != nil {
		return nil, err
	}
	where := strings.TrimSpace(cfg.Workspace)
	if where == "" {
		where = workspace
	}
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
func runDoor(st *store.Store, workspace string) session.RunDoor {
	if st == nil {
		return nil
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, workspace)))
	if seam.Edit == nil {
		return nil
	}
	return seamRunDoor{st: st, edit: seam.Edit}
}

// seamRunDoor finds the item a conversation manages and edits it through the
// seam.
type seamRunDoor struct {
	st   *store.Store
	edit func(ctx context.Context, id int, e factory.RunEdit) (factory.Item, []string, error)
}

// errNotAManager is `factory_run` asked by a conversation that manages no item.
var errNotAManager = errors.New("this conversation is not the manager of any item on the floor")

// EditRun edits the item whose conversation is the asking one.
func (d seamRunDoor) EditRun(ctx context.Context, conversation string, e factory.RunEdit) (factory.Item, []string, error) {
	want := sameFileKey(conversation)
	if want == "" {
		return factory.Item{}, nil, errNotAManager
	}
	items, err := d.st.List()
	if err != nil {
		return factory.Item{}, nil, err
	}
	for _, it := range items {
		if strings.TrimSpace(it.Talk) != "" && sameFileKey(it.Talk) == want {
			return d.edit(ctx, it.ID, e)
		}
	}
	return factory.Item{}, nil, errNotAManager
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

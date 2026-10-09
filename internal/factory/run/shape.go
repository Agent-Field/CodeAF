package run

// The manager shapes the run.
//
// THE MANAGER IS THE AUTHOR OF THE RUN (the owner's decision of 2026-10-08).
// Shaping is the manager's first act of a run and not a stage: at launch,
// after the manager is made and before the first stage, it is given ONE turn
// ([Options.Shape]) to set the asks, add or switch the stages the item needs,
// and the runner applies what it answered through [factory.Edit] before the
// run, says the line on the item's log and into the conversation, and goes
// on. Three ways in, one machinery:
//
//   - the person does nothing and presses `r`: the manager shapes, the run
//     runs;
//   - the person says something first: the manager's own turn there applies
//     its edit at once through `factory_run` (nothing runs yet), and the
//     launch then skips shaping, because the item was already shaped by the
//     manager ([shapedByManager]);
//   - the person says something mid-run: the words are the steer, as always,
//     and the manager is given one more turn ([Options.Reshape]) that may
//     change the stages not yet started; the tail is re-read at each round
//     ([tailPhases]).
//
// A TURN THAT FAILS OR TAKES TOO LONG IS NO EDIT: the recipe stands, and the
// log says so. The bounds are [factory.Edit]'s, held in code.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE SHAPING LINES, said on the item's log and into its conversation. The
// manual quotes them (internal/manual/chat/factory.md and
// factory-stage-conversations.md); a line respelled here is respelled there.
const (
	sayShapeStands  = "the recipe stands"
	sayShapeNoReply = "the manager did not answer · the recipe stands"
	sayShapeBusy    = "the manager was busy in the window · the recipe stands"
	sayShapeAway    = "the manager is open in another window · the recipe stands"
	sayShapeRefused = "the manager's change was not applied: %s · the recipe stands"
	sayShapeAsk     = "run these stages? %s"
	sayTailRefused  = "the manager's change was not applied: %s"
)

// The two reasons a shaping turn did not happen that the person is told by
// name. Options.Shape answers them (wrapped as it likes) and the runner says
// the matching line instead of [sayShapeNoReply]:
//
//   - ErrManagerBusy: the conversation is open in this process and was in the
//     middle of a turn of its own, asked once more after it, and still was;
//   - ErrManagerAway: another process holds the conversation (a window on the
//     engine host, `--host`), so this runner cannot give it a turn. No retry.
var (
	ErrManagerBusy = errors.New("the manager was busy in the window")
	ErrManagerAway = errors.New("the manager is open in another window")
)

// The shaping lines the floor's Shape door says, the same words the launch
// says ([sayShapeStands], [shapeFailLine], [sayShapeRefused]).
const SayShapeStands = sayShapeStands

// ShapeFailLine is [shapeFailLine] for the floor's Shape door.
func ShapeFailLine(err error) string { return shapeFailLine(err) }

// ShapeRefusedLine is the line a refused shaping edit is said with.
func ShapeRefusedLine(err error) string {
	return fmt.Sprintf(sayShapeRefused, loopFirstLine(err.Error()))
}

// shapeFailLine is the line a shaping turn that did not answer is said with.
func shapeFailLine(err error) string {
	switch {
	case errors.Is(err, ErrManagerAway):
		return sayShapeAway
	case errors.Is(err, ErrManagerBusy):
		return sayShapeBusy
	}
	return sayShapeNoReply
}

// shapeWaitDefault is how long a shaping turn may take when
// [Options.ShapeWait] is zero.
const shapeWaitDefault = time.Minute

func (lp *floorLoop) shapeWait() time.Duration {
	if d := lp.r.opts.ShapeWait; d > 0 {
		return d
	}
	return shapeWaitDefault
}

// shape is the manager's first act of a run: one turn, its edit applied
// before the run, its line said, and with ask me at plan the person's yes
// before the first stage. It answers false when the item was stopped.
func (lp *floorLoop) shape(c *loopCtl) bool {
	shapeFn := lp.r.opts.Shape
	if shapeFn == nil {
		return true
	}
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return false
	}
	// THE PERSON OPENED THE ITEM AND TALKED FIRST, and the manager shaped it
	// then: that is this run's shape.
	if shapedByManager(it) {
		return true
	}
	said := append([]string(nil), c.said...)
	for {
		edit, err := lp.turn(c, func(ctx context.Context) (factory.RunEdit, string, error) {
			return shapeFn(ctx, it, said)
		})
		if c.ctx.Err() != nil {
			return false
		}
		if err != nil {
			line := shapeFailLine(err)
			lp.say(c, "fail", line)
			lp.tell(c.id, line)
			return true
		}
		line, err := lp.applyEdit(c, edit, false)
		if c.ctx.Err() != nil {
			return false
		}
		if err != nil {
			refused := fmt.Sprintf(sayShapeRefused, loopFirstLine(err.Error()))
			lp.say(c, "fail", refused)
			lp.tell(c.id, refused)
			return true
		}
		if line == "" {
			lp.say(c, "said", sayShapeStands)
			lp.tell(c.id, sayShapeStands)
			return true
		}
		lp.tell(c.id, line)
		cur, err := lp.r.opts.Store.Get(c.id)
		if err != nil {
			return false
		}
		if cur.Gate != factory.GatePlan {
			return true
		}
		// ASK ME AT PLAN IS ASKED HERE FIRST: the person sees the stages the
		// manager set before any of them runs. Words shape it again.
		a, ok := lp.ask(c, -1, "plan", "plan", fmt.Sprintf(sayShapeAsk, line), factory.PhaseWaiting, "", "")
		if !ok {
			return false
		}
		if a.yes {
			return true
		}
		if a.words == "" {
			// NO KEEPS THE RECIPE: the stages go back to what they were before
			// the manager set them, and the run goes on.
			lp.keepRecipe(c, it)
			return c.ctx.Err() == nil
		}
		lp.addNote(c.id, a.words)
		said = []string{a.words}
		if it, err = lp.r.opts.Store.Get(c.id); err != nil {
			return false
		}
	}
}

// keepRecipe puts the item's stages back as they stood before the manager's
// edit (before), says `the recipe stands`, and compiles the phases again.
func (lp *floorLoop) keepRecipe(c *loopCtl, before factory.Item) {
	now := lp.r.now()
	if lp.write(c, func(it *factory.Item) error {
		stages := before.Stages
		if len(stages) == 0 {
			kind := it.Kind
			if kind == "" {
				kind = factory.KindIssue
			}
			stages = lp.recipe(it.Repo).For(kind)
		}
		it.Stages = factory.CopyStages(stages)
		it.Adapted = append([]string(nil), before.Adapted...)
		// NOTHING HAS RUN YET, so the phases are the stages compiled afresh.
		it.Stream.Phases = loopPhases(*it)
		loopSay(it, now, "said", sayShapeStands)
		return nil
	}) == nil {
		lp.tell(c.id, sayShapeStands)
	}
}

// reshape gives the manager one turn on what the person just said during a
// run, and applies what it answers to the stages not yet started. A turn that
// fails, or answers nothing, leaves the stages as they are and says nothing:
// the words are already the steer.
func (lp *floorLoop) reshape(c *loopCtl, words string) {
	fn := lp.r.opts.Reshape
	if fn == nil || c.reverify || strings.TrimSpace(words) == "" {
		return
	}
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return
	}
	edit, err := lp.turn(c, func(ctx context.Context) (factory.RunEdit, string, error) {
		return fn(ctx, it, words)
	})
	if err != nil || c.ctx.Err() != nil {
		return
	}
	line, err := lp.applyEdit(c, edit, true)
	if c.ctx.Err() != nil {
		return
	}
	if err != nil {
		refused := fmt.Sprintf(sayTailRefused, loopFirstLine(err.Error()))
		lp.say(c, "fail", refused)
		lp.tell(c.id, refused)
		return
	}
	if line != "" {
		lp.tell(c.id, line)
	}
}

// turn runs one manager turn under the item's ctx and the shaping wait. The
// error is the turn's own, or the wait's when it did not answer in time: a
// turn that ignores its ctx is let go, never waited on.
func (lp *floorLoop) turn(c *loopCtl, call func(ctx context.Context) (factory.RunEdit, string, error)) (factory.RunEdit, error) {
	ctx, cancel := context.WithTimeout(c.ctx, lp.shapeWait())
	defer cancel()
	type answer struct {
		edit factory.RunEdit
		err  error
	}
	got := make(chan answer, 1)
	go func() {
		edit, _, err := call(ctx)
		got <- answer{edit: edit, err: err}
	}()
	select {
	case a := <-got:
		return a.edit, a.err
	case <-ctx.Done():
		return factory.RunEdit{}, ctx.Err()
	}
}

// applyEdit applies the manager's edit to the item, before the run or in it,
// and rebuilds the phases not yet started. It answers the line it said on the
// item's log (`manager set review: … · added arch after review · why: …`),
// "" when the edit changed nothing, or the refusal.
func (lp *floorLoop) applyEdit(c *loopCtl, edit factory.RunEdit, inRun bool) (string, error) {
	if edit.Empty() {
		return "", nil
	}
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return "", err
	}
	recipe := lp.recipe(it.Repo)
	now := lp.r.now()
	line := ""
	err = lp.write(c, func(it *factory.Item) error {
		l, err := ApplyShape(it, edit, recipe, inRun, now)
		line = l
		return err
	})
	return line, err
}

// ApplyShape applies the manager's edit to it in place, the one application
// of a shaping turn's edit: the runner's at launch and on a steer
// ([floorLoop.applyEdit]), and the floor's Shape door's when an item page
// first opens ([factory.Seam.Shape]). The edit is the manager's whatever it
// says; inRun bounds it to the stages not yet started. The item's run keeps
// its stream, and a stream's phases not yet started are compiled again from
// the stages; an item with no stream gets none (its launch compiles them). It
// answers the edit's one line ([EditLine]), said on the stream when there is
// one, "" when the edit changed nothing, or the refusal, which changes
// nothing.
func ApplyShape(it *factory.Item, edit factory.RunEdit, recipe factory.Recipe, inRun bool, at time.Time) (string, error) {
	if it == nil || edit.Empty() {
		return "", nil
	}
	edit.By = factory.ByManager
	mode := factory.EditBeforeRun
	if inRun {
		mode = factory.EditInRun
	}
	next, lines, err := factory.Edit(*it, edit, recipe, mode)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", nil
	}
	stream := it.Stream
	*it = next
	it.Stream = stream
	if it.Stream != nil {
		if phases, changed := tailPhases(*it, it.Stream.Phases); changed {
			it.Stream.Phases = phases
		}
	}
	line := EditLine(lines)
	loopSay(it, at, "said", line)
	return line, nil
}

// EditLine is an edit's lines as the one line the log and the manager say:
// joined with ` · `, and the first line's leading word (who made it) dropped
// from every line after it that repeats it, so `manager set review: … · added
// arch after review · why: …`. "" for no lines.
func EditLine(lines []string) string {
	var parts []string
	by := ""
	for _, l := range lines {
		if l = oneLine(l); l == "" {
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
	return strings.Join(parts, saySep)
}

// ShapedByManager is [shapedByManager] for the floor's Shape door, which
// skips an item the manager already shaped, as the launch does.
func ShapedByManager(it factory.Item) bool { return shapedByManager(it) }

// shapedByManager says whether the manager already shaped the item: a stage
// it set or added (Stage.By), or a line it recorded on the item's stages
// (`manager set …`).
func shapedByManager(it factory.Item) bool {
	for _, st := range it.Stages {
		if st.By == factory.ByManager {
			return true
		}
	}
	for _, l := range it.Adapted {
		if strings.HasPrefix(strings.TrimSpace(l), factory.ByManager+" ") {
			return true
		}
	}
	return false
}

// tailPhases is the phases with every one not yet started compiled again from
// the item's stages as they stand now, and whether that changed anything. A
// tail that holds a phase the stages do not (a request for changes' prove) is
// left as it is.
func tailPhases(it factory.Item, phases []factory.Phase) ([]factory.Phase, bool) {
	last := -1
	for i, p := range phases {
		if p.State != "" && p.State != factory.PhasePending {
			last = i
		}
	}
	for _, p := range phases[last+1:] {
		if factory.StageIndex(it.Stages, p.Name) < 0 {
			return phases, false
		}
	}
	var out []factory.Phase
	if last < 0 {
		out = loopPhases(it)
	} else {
		if factory.StageIndex(it.Stages, phases[last].Name) < 0 {
			return phases, false
		}
		out = retail(it, phases, last)
	}
	if samePhases(out, phases) {
		return phases, false
	}
	return out, true
}

// samePhases compares two phase lists by what a tail is compiled from.
func samePhases(a, b []factory.Phase) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Kind != b[i].Kind || a[i].State != b[i].State || a[i].Note != b[i].Note {
			return false
		}
	}
	return true
}

// retailTail rebuilds the phases not yet started from the item's stages at a
// round boundary, so an edit made since (the manager's, through its own
// conversation's `factory_run`) is what runs next.
func (lp *floorLoop) retailTail(c *loopCtl) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil || it.Stream == nil {
		return
	}
	if _, changed := tailPhases(it, it.Stream.Phases); !changed {
		return
	}
	_ = lp.write(c, func(it *factory.Item) error {
		if phases, changed := tailPhases(*it, it.Stream.Phases); changed {
			it.Stream.Phases = phases
		}
		return nil
	})
}

// recipe is the repository's recipe, the default when the runner has none.
func (lp *floorLoop) recipe(repo string) factory.Recipe {
	if lp.r.opts.Recipe != nil {
		return lp.r.opts.Recipe(repo)
	}
	return factory.DefaultRecipe()
}

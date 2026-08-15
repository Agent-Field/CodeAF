package chat

import (
	"image"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The notebook lens: the wiring, and none of the drawing.
//
// THE SEAM. internal/tui2/homes owns what the notebook page IS — its four
// bands, its strip, its rows, its expand law and its scroll; this file owns
// where it sits, what a keystroke on it means and what a click on one of its
// verbs runs. The two meet on [notebookPage] and on nothing else, which is the
// same split internal/tui2/rail keeps with scope.go: one side knows the facts,
// the other knows the terminal, and neither learns the other's job.
//
// It is stated as an interface rather than taken as a concrete type for one
// reason worth writing down: the page and its wiring landed in two lanes at
// once. An interface is what let this side be built, reviewed and tested against
// the API the page's own doc comment publishes, while the page itself was still
// being written next door. When both landed the assertion below became a
// formality — which is exactly what a seam that was designed rather than
// discovered looks like.

// notebookPage is the notebook lens, and the whole of what the hug asks of it.
//
// Every method here is one the page's own doc comment publishes as the wiring
// API (internal/tui2/homes/page.go). Nothing is added: the page owns its scroll,
// its drill and its sections, so this side asks it to move and never moves it.
type notebookPage interface {
	// Render draws the page at a size. The returned slice aliases the page's own
	// buffer and is valid only until the next Render.
	Render(state homes.State, sel homes.Selection, width, height int) []string
	// Click resolves a pane-local cell and applies the half the page owns — a
	// section word jumps, a door folds. A verb comes back for the host to run
	// through the registry, because the page does not know what a verb does and
	// 5.22 says it must not learn.
	Click(x, y int) (homes.Target, bool)
	// JumpTo scrolls a section's word to the top of the body.
	JumpTo(section homes.Section)
	// Enter drills into a row and reports whether it opened one; Back returns to
	// the list at the row it left and reports whether there was a drill to
	// leave. The false answers are what let this side decide that enter and esc
	// mean something else here (notebook-split.md §3).
	Enter(rowID string) bool
	Back() bool
	// ScrollBy, ScrollTop and ScrollBottom move the body, clamped.
	ScrollBy(delta int)
	ScrollTop()
	ScrollBottom()
	// SetHover lights the run the pointer rests on and ClearHover puts it
	// nowhere; both report whether the frame moved, so the shell repaints only
	// when the picture actually changed.
	//
	// They are on the seam because §16's hover law is not decoration here: a row
	// answers the pointer by going up one tier AND by showing its verbs, which
	// is what tells a reader the line is a door. A host that never calls these
	// draws a page where nothing ever hovers — see this file's report on the
	// motion event app.go still owes.
	SetHover(x, y int) bool
	ClearHover() bool
	// SetStyler re-points the page after a focus change (8.3: dimming is a
	// property of the pane).
	SetStyler(st *tokens.Styler)
}

// notebookLens is the page plus the two facts the HOST has to remember about
// it: when it last read the store, and whether the store moved while nobody was
// looking at this lens.
//
// They live in a wrapper rather than on [App] because they are this file's
// bookkeeping and app.go is another lane's file this wave — but the shape is the
// right one anyway. They are not the page's state: the page does not know what a
// poll is, and it must not learn.
type notebookLens struct {
	notebookPage
	read  time.Time
	stale bool
}

// newNotebookPage builds the page the notebook tab draws.
func newNotebookPage(st *tokens.Styler) notebookPage {
	return &notebookLens{notebookPage: homes.NewPage(st)}
}

// notebookLens is the wrapper behind [App.notebook], or nil for a host that
// injected a page of its own (a test). A nil lens simply does not cache: every
// caller below still works, it just pays the read it was going to pay anyway.
func (a *App) lens() *notebookLens {
	lens, _ := a.notebook.(*notebookLens)
	return lens
}

// Learned is the optional read that FILLS the notebook.
//
// It is asked for by type assertion off the Backend, exactly as [Receipts] and
// [Models] are and for the reason scope.go states: a capability some hosts have
// belongs behind its own assertion, and a window over a slice of rows should not
// lose the whole surface because it cannot answer one question. A backend that
// does not implement it leaves the page empty — and an empty band teaches what
// would put something in it, so an unwired window and a brand new machine draw
// the same true thing.
//
// ONE METHOD, ONE INSTANT. The page is three sections read at one moment
// (internal/command/notebook_reads.go says why), so the seam is one call rather
// than one per band. *command.Commander satisfies it with no adapter.
type Learned interface {
	NotebookPage(now time.Time) command.NotebookPage
}

// learned finds whichever seam can answer it.
//
// BOTH SEAMS ARE ASKED, and which one answers is a fact about the host rather
// than a decision here: the crafts live in a git repository beside the brain and
// only *command.Commander can read them, while everything else on the page is a
// store read — so today the engine answers and a Backend that grew the method
// would answer too. Asking both is one type assertion more and one deployment
// assumption fewer.
func (a *App) learned() (Learned, bool) {
	if reader, ok := a.commander.(Learned); ok && reader != nil {
		return reader, true
	}
	reader, ok := a.backend.(Learned)
	return reader, ok && reader != nil
}

// notebookRefreshEvery bounds how often this window will pay for that read.
//
// The read is journal-driven — it happens when the poll says the facts moved
// (rooms.go's refreshHomes) — but the journal moves on every token of every
// running job, and the notebook does not change that fast. A floor of a second
// turns a burst of fifty journal moves into one read, and nothing on this page
// is a live number that a second of lag would make wrong: the newest belief in
// the world is still one second old.
const notebookRefreshEvery = time.Second

// fillNotebook reads the learned state into the page's [homes.State].
//
// WHEN, and it is three answers rather than one:
//   - on a journal move, but only while the notebook is the lens a person is
//     looking at. A window sitting in the thread does not pay for a page nobody
//     is reading.
//   - once when the lens becomes the notebook, which is the `stale` flag: the
//     journal moved while the reader was elsewhere and the page they are about
//     to see would otherwise be the one from before they left.
//   - never per frame. [App.renderNotebook] asks this, and it costs one boolean
//     and one clock comparison on every frame that is not the first after a
//     move.
func (a *App) fillNotebook(force bool) {
	if a.source == nil || a.notebook == nil {
		return
	}
	lens := a.lens()
	if !force && a.page != pageNotebook {
		// The journal moved while the reader was somewhere else. Remembering
		// that is the whole of the catch-up: the read happens when they arrive.
		if lens != nil {
			lens.stale = true
		}
		return
	}
	now := a.now()
	if lens != nil {
		if !force && lens.read.Add(notebookRefreshEvery).After(now) {
			return
		}
		// Stamped BEFORE the read is attempted, so a window whose backend cannot
		// answer stops asking after the first frame instead of asking on every
		// one (see [App.renderNotebook]'s first-arrival condition).
		lens.read, lens.stale = now, false
	}
	reader, ok := a.learned()
	if !ok {
		return
	}
	applyNotebook(&a.source.homes, reader.NotebookPage(now))
	a.source.homeSource.SetState(a.source.homes)
}

// applyNotebook maps one read onto the surface's state.
//
// It is a mapping and NOT a judgement: every honesty decision — what is
// measured, what is absent, which class a belief is — was made by the read, and
// every wording decision belongs to the page. What happens here is that a
// command type becomes a homes type, field for field, which is the same job the
// board's row mapping does and is deliberately just as boring.
func applyNotebook(state *homes.State, page command.NotebookPage) {
	state.Notebook.Beliefs = state.Notebook.Beliefs[:0]
	for _, belief := range page.Beliefs {
		state.Notebook.Beliefs = append(state.Notebook.Beliefs, homes.Belief{
			ID:   strconv.FormatInt(belief.Seq, 10),
			Body: belief.Body, Scope: belief.Scope, Kind: belief.Kind,
			Class: notebookClass(belief.Class), Channel: notebookChannel(belief.Channel),
			Status: belief.Status, Trust: belief.Trust,
			Samples: belief.Samples, HasSamples: belief.HasSamples,
			Learned: belief.Learned, LastUsed: belief.LastUsed,
			Uses: belief.Uses, HasUses: belief.HasUses,
			Retired: belief.Retired, Provisional: belief.Provisional,
			Evidence: notebookEvidence(belief.Evidence),
		})
	}
	state.Notebook.Total = page.Total
	state.Notebook.AtCeiling = page.AtCeiling

	state.Knowhow.Crafts = state.Knowhow.Crafts[:0]
	for _, craft := range page.Crafts {
		entry := homes.Craft{
			// The NAME is the id. A workflow is a file in a git repository and
			// its name is what every door already addresses it by — a second
			// handle would be a second thing to keep in step.
			ID: craft.Name, Name: craft.Name, Description: craft.Description,
			Proved: craft.Proved, Against: craft.Against, HasRecord: craft.HasRecord,
			CostPerRun: craft.LastCostUSD, HasCost: craft.HasCost,
			Version: craft.Version, Updated: craft.Updated, Dir: craft.Dir,
			Ceilings: homes.CraftCeilings{
				CostUSD: craft.CeilingUSD, HasCost: craft.HasCeiling, WallClock: craft.Wall,
			},
		}
		for _, step := range craft.Steps {
			entry.Steps = append(entry.Steps, homes.CraftStep{
				Brief: step.Brief, Needs: step.Needs,
				Model: step.Model, Skill: step.Skill, Verify: step.Verify,
			})
		}
		for _, version := range craft.History {
			entry.History = append(entry.History, homes.CraftVersion{
				Version: version.Version, Subject: version.Subject, When: version.When,
			})
		}
		state.Knowhow.Crafts = append(state.Knowhow.Crafts, entry)
	}

	state.Knowhow.Skills = state.Knowhow.Skills[:0]
	for _, skill := range page.Skills {
		state.Knowhow.Skills = append(state.Knowhow.Skills, homes.Skill{
			ID:   strconv.FormatInt(skill.Seq, 10),
			Name: skill.Name, Body: skill.Body, Path: skill.Path,
			Uses: skill.Uses, HasUses: skill.HasUses, Learned: skill.Learned,
			Retired: skill.Retired, Note: skill.Note,
		})
	}

	state.Practice.Questions = state.Practice.Questions[:0]
	for _, question := range page.Questions {
		entry := homes.Question{
			ID:   strconv.FormatInt(question.Seq, 10),
			Body: question.Body, Scope: question.Scope,
			Life: notebookLife(question.Status),
			Runs: question.Runs, CostUSD: question.CostUSD, HasCost: question.HasCost,
			Asked: question.Asked, Note: question.Note,
		}
		for _, attempt := range question.Attempts {
			entry.Attempts = append(entry.Attempts, homes.Attempt{
				CostUSD: attempt.CostUSD, HasCost: attempt.HasCost,
				Delta: attempt.Delta, HasDelta: attempt.HasDelta, When: attempt.When,
			})
		}
		state.Practice.Questions = append(state.Practice.Questions, entry)
	}
	state.Practice.Competence = homes.Competence{
		Strongest: page.Competence.Strongest, Frontier: page.Competence.Frontier,
	}
	state.Practice.Today = homes.Today{
		SpendUSD: page.Today.SpendUSD, HasSpend: page.Today.HasSpend,
		Learned: page.Today.Learned, Practiced: page.Today.Practiced,
	}
}

// notebookEvidence carries the resolved references across. The read already did
// the resolving — a name a person can read, a handle a host can open — so this
// is the same field-for-field mapping the rest of applyNotebook is, and the only
// reason it is a function is that the two packages must not share a type.
func notebookEvidence(refs []command.NotebookEvidence) []homes.Evidence {
	if len(refs) == 0 {
		return nil
	}
	out := make([]homes.Evidence, 0, len(refs))
	for _, ref := range refs {
		out = append(out, homes.Evidence{Name: ref.Name, Room: ref.Room, When: ref.When})
	}
	return out
}

// notebookClass, notebookChannel and notebookLife are the three enum crossings.
// They are switches with no default that guesses: a word neither side knows
// resolves to the plain, unclassed reading, because a row drawn as the wrong
// kind of thing is worse than a row drawn as an ordinary one.
func notebookClass(class command.NotebookBeliefClass) homes.BeliefClass {
	switch class {
	case command.NotebookBeliefTaste:
		return homes.BeliefTaste
	case command.NotebookBeliefTrait:
		return homes.BeliefTrait
	case command.NotebookBeliefPlaybook:
		return homes.BeliefPlaybook
	}
	return homes.BeliefPlain
}

func notebookChannel(channel string) homes.BeliefChannel {
	switch store.FactChannel(channel) {
	case store.FactChannelStated:
		return homes.ChannelStated
	case store.FactChannelInferred:
		return homes.ChannelInferred
	case store.FactChannelDistilled:
		return homes.ChannelDistilled
	case store.FactChannelTrial:
		return homes.ChannelTrial
	}
	return homes.ChannelUnknown
}

func notebookLife(status string) homes.QuestionLife {
	switch status {
	case store.QuestionPracticing:
		return homes.QuestionPracticing
	case store.QuestionResolved:
		return homes.QuestionResolved
	case store.QuestionRetired:
		return homes.QuestionRetired
	}
	return homes.QuestionAsked
}

// notebookScroll is how far a wheel notch or a paging key moves the page. Three
// rows is the conventional notch, and it is the transcript's own scrollStep —
// two surfaces that scroll should not scroll at two speeds.
const notebookScroll = scrollStep

// renderNotebook paints the notebook page.
//
// The state is the SOURCE's, not a copy: [scopeSource.homes] is what the rail
// already draws the four rooms from, refreshed on the same cycle as everything
// else, so the page and the rail cannot disagree about what aforge believes.
func (a *App) renderNotebook(width, height int) string {
	if a.notebook == nil || a.source == nil {
		return ""
	}
	// The catch-up read, and the only one that happens on a render path. It
	// fires at most once per journal move — [App.notebookStale] is set by the
	// poll while this lens is not the one on screen — so the frame after the
	// reader arrives shows what the journal already knows, and every frame after
	// that costs one boolean. See [App.fillNotebook] for why the alternative
	// (reading on every move, whether or not anybody is looking) is the wrong
	// trade.
	// TWO CONDITIONS, one arrival: the page has never been filled (the reader
	// just walked in and the poll had no reason to read a lens nobody was
	// looking at), or the journal moved while they were away.
	if lens := a.lens(); lens != nil && (lens.stale || lens.read.IsZero()) {
		a.fillNotebook(true)
	}
	return strings.Join(a.notebook.Render(a.source.homes, a.homesSel, width, height), "\n")
}

// notebookKey is the page's keyboard, asked on the same terms the board's is
// (the guards live in [App.pageKey]).
//
// The digits are the page's OWN jump keys and not a second grammar invented
// here — [homes.SectionForKey] is where they are decided, and the page's doc
// comment says why they are ordinals rather than initials.
func (a *App) notebookKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.page != pageNotebook || a.notebook == nil {
		return nil, false
	}
	switch key := msg.String(); key {
	case "enter":
		// THE DRILL. The row is the one the cursor rests on, which is the rail's
		// [homes.Selection] — the same locator the page already scrolls to, so
		// enter opens what the reader is looking at rather than needing a second
		// cursor of its own (5.15: one cursor).
		return nil, a.notebook.Enter(a.homesSel.Row)
	case "esc":
		// Reached only once app.go's esc ladder offers esc to the page (see this
		// lane's report); until then [App.pageKey] answers esc first and leaves
		// the page. Back's own false is the honest signal that there was nothing
		// to leave, so the ladder can carry on to the next rung.
		return nil, a.notebook.Back()
	case "j", "down":
		a.notebook.ScrollBy(1)
	case "k", "up":
		a.notebook.ScrollBy(-1)
	case "pgdown":
		a.notebook.ScrollBy(notebookScroll)
	case "pgup":
		a.notebook.ScrollBy(-notebookScroll)
	case "home", "g":
		a.notebook.ScrollTop()
	case "end", "G":
		a.notebook.ScrollBottom()
	default:
		section, ok := homes.SectionForKey(key)
		if !ok {
			return nil, false
		}
		a.notebook.JumpTo(section)
	}
	return nil, true
}

// notebookHover answers a pointer that is only passing over.
//
// It is separate from [App.notebookPoint] because a motion event is not a click
// and must not be routed like one: nothing here selects, folds or scrolls. The
// return is whether the frame moved, so a pointer crossing forty cells of one
// row costs one comparison and no repaints.
//
// WIRING: app.go's pointer path has to hand this every tea.MouseMotionMsg that
// lands in the notebook pane, and call [App.notebookLeave] when the pointer
// leaves it. Until it does, the page's hover is dead — which is exactly the
// defect the user reported ("no hover affordance"), one layer up from the one
// the page itself carried.
func (a *App) notebookHover(local image.Point) bool {
	if a.notebook == nil {
		return false
	}
	return a.notebook.SetHover(local.X, local.Y)
}

// notebookLeave puts the pointer nowhere: the pane lost it, or the lens did.
func (a *App) notebookLeave() bool {
	if a.notebook == nil {
		return false
	}
	return a.notebook.ClearHover()
}

// notebookPoint is the page's pointer.
//
// A click is handed to the page FIRST, because navigation inside a page is the
// page's own — a jump or a fold that had to round-trip through this file would
// be navigation that could lag the pointer. What comes back is a verb, and a
// verb is the registry's: it runs through [App.runFooterVerb], the same executor
// the footer's words, the palette's rows and the `?` sheet all reach.
func (a *App) notebookPoint(msg tea.MouseMsg, local image.Point) tea.Cmd {
	if a.notebook == nil {
		return nil
	}
	switch event := msg.(type) {
	case tea.MouseWheelMsg:
		switch event.Button {
		case tea.MouseWheelUp:
			a.notebook.ScrollBy(-notebookScroll)
		case tea.MouseWheelDown:
			a.notebook.ScrollBy(notebookScroll)
		default:
			return nil
		}
		a.shell.Invalidate()
		return nil
	case tea.MouseMotionMsg:
		if a.notebookHover(local) {
			a.shell.Invalidate()
		}
		return nil
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return nil
		}
		target, ok := a.notebook.Click(local.X, local.Y)
		a.shell.Invalidate()
		if !ok {
			return nil
		}
		if target.Kind == homes.TargetRoom {
			// WHAT TAUGHT A BELIEF IS A PLACE. The page hands back the work's
			// own handle and applies nothing, exactly as it does with a verb;
			// opening the room is the host's [App.jumpTo] — the same door a
			// board row and a palette row walk through.
			return a.jumpTo(target.ID)
		}
		if target.Kind != homes.TargetVerb {
			return nil
		}
		// A verb with a Row acts on that one item and goes through the item
		// runner (confirm arming, steer seeding — itemverb.go); a verb without
		// one is the registry's ordinary window act.
		if target.Row != "" {
			return a.runItemVerb(target.ID, notebookVerbSubject(target.Row), a.notebookVerbName(target.Row))
		}
		return a.runFooterVerb(target.ID)
	}
	return nil
}

package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE TARGET — the box at home is a draft for the conversation it opens ────
//
// Home and conversations start their seams with the same model, effort and
// approvals at the left, with the project at the right:
//
//	─ glm-5.3-flash:auto · ◇ asks ───── project: ~/src/parser ─
//	 › type to search or start something new
//
// The project stays a path, so two checkouts with the same name remain
// distinguishable. The name starts at the root and gives up its right end
// when the frame is narrow; the model starts at the left on every wide frame.
//
// AND IT IS HOME'S ALONE. Only home starts things (pages.go's [place.box]), so
// only home has a box, a draft and this rule over it; every other place ends
// in its note and its hint.
//
// ── WHY A TARGET AND NOT A READING ──────────────────────────────────────────
//
// The scope chip that used to sit on home's box row was a READING: whatever
// project the cursor happened to be resting on. It never lied about itself and
// it disagreed with `enter`, which opened a conversation in this window's own
// workspace and ignored the row entirely — so the one line on the screen that
// said where a sentence would land was the one line `enter` did not read.
//
// A target is that reading with a pin on it. With nothing pinned it IS the
// reading — [app.scopeWorkspace], the cursor's own row — and `enter` honours it,
// which is what closes the disagreement. `alt+p` and `/model` pin it, and a pin
// is a decision a person made.
//
// ── OWNER RULING: WHAT PERSISTS AND WHAT IS SPENT ───────────────────────────
//
// BOTH PINS SURVIVE `esc` AND A REOPEN OF HOME. They are the session's, not the
// screen's: "let me set the model before I start" is worth nothing if walking
// into a conversation and back out forgets it.
//
// THE PROJECT PIN LASTS AS LONG AS THE WINDOW, just like the model and effort.
// Starting a conversation must not undo an explicit choice, nor may a cursor
// move or a return to home silently change the destination of the next one.

// homeTarget is the draft the box at home is for: where the next conversation
// opens, what it runs on, and the model list opened over it.
type homeTarget struct {
	// where is the folder the next conversation opens in, and "" is FOLLOW THE
	// CURSOR — [app.scopeWorkspace]'s own reading of the row a person is
	// standing on. It is a PATH and never an index into the list, for
	// [composerLayer.where]'s reason exactly: the list is read again on every
	// frame, and a project that appeared between two keystrokes would otherwise
	// shift the pin's meaning under the person's hand.
	where string
	// model is the model the next conversation opens on, and "" is this
	// window's own ([app.model]). It is pinned by `/model` at home and by
	// a press on its cell, and it lasts as long as this window.
	model string
	// effort is the rung the next conversation thinks at, "" for what the
	// install would do anyway ([app.targetEffortStanding]). It is pinned by
	// `alt+e` and a press on the cell, and — like the model — it survives the
	// conversation that uses it: how hard you think is how you work
	// (boxseam.go).
	effort string
	// approval is the posture the next conversation opens at, "" for the rows
	// as they stand ([app.targetApprovalStanding]). It is pinned by `alt+a` and
	// a press on the cell, and — unlike the model — it is SPENT by the
	// conversation that takes it, because an open gate is a safety claim about
	// one conversation and never a default for the next.
	approval string
	// pick is the model list opened over this target. It is THE picker — the
	// same type, the same rows and the same walk the composer layer and the
	// settings panel put inside their own frames ([picker.rowsOwned]) — because
	// there is one filterable model list on this surface and this is a fourth
	// door onto it rather than a fourth list that looks like it.
	pick picker
	// levels is the reasoning rung `ctrl+t` has dialled onto a model FOR THE
	// NEXT CONVERSATION, per model id, and it is here rather than on the agent
	// for [app.pinTargetModel]'s reason: nothing behind home is touched. The
	// agent this window happens to be holding is a conversation the person is
	// not looking at, and a rung written there would change that conversation's
	// setting for a model it is not using and then be forgotten by the one they
	// are about to start (`/new` is a new agent).
	//
	// It is spent in [app.applyTargetModel], after the new conversation has
	// attached and has an agent to write to.
	levels map[string]string
}

// targetWhere is the folder the next conversation will open in: the pin, and
// otherwise the row the cursor is standing on.
func (a *app) targetWhere() string {
	if pinned := strings.TrimSpace(a.target.where); pinned != "" {
		return pinned
	}
	return a.scopeWorkspace()
}

// targetModel is the model the next conversation will answer on: the pin, and
// otherwise this window's own.
func (a *app) targetModel() string {
	if pinned := strings.TrimSpace(a.target.model); pinned != "" {
		return pinned
	}
	return strings.TrimSpace(a.model)
}

// targetModelPinned reports a draft model that differs from this window's own.
// Starting a conversation applies only a pin that changes the model.
func (a *app) targetModelPinned() bool {
	pinned := strings.TrimSpace(a.target.model)
	return pinned != "" && pinned != strings.TrimSpace(a.model)
}

// targetPickFoot is home's foot while the model list is open: the walk, then
// whatever the row under the cursor answers to, then the way out.
//
// IT IS CURSOR-SHAPED FOR [picker.keysHint]'s REASON, and it is home's own
// verbs around it. The keys used to live in the filter box's placeholder, which
// is the one line that vanishes the moment somebody types — and at this door
// two of them did nothing whatever anybody typed ([app.openTargetPicker] arms
// the fold now, and [targetDraft.levels] holds the rung).
//
// `enter choose` and not `enter switch`, because nothing switches here: a choice
// made on this list is a pin on the NEXT conversation ([app.pinTargetModel]). The
// picker's own middle says `enter switch`, so the verb is swapped and home's own
// ending put back on. It said `use it` until the owner asked for one verb across
// the levels — every other row of this same fold already said `choose`, so `use
// it` was a second word for one gesture on one rung of it.
func (a *app) targetPickFoot() string {
	before, enter, after := a.target.pick.keysParts()
	// `choose` AND NOT `switch`, because nothing switches here: a choice made on
	// this list is a pin on the next conversation ([app.pinTargetModel]). The
	// other two words enter can take — `choose` a provider, `unpin` the one the
	// requests already go to — mean the same at either door and are kept.
	if enter == "switch" {
		enter = "choose"
	}
	return dotted(targetPickWalkWord, before, "enter "+enter, after, targetPickLeaveWord)
}

// targetPickShowing is whether the model list over the target is up. It is
// asked by the frame, by the router and by the foot, and it is ONE field for
// [app.composerShowing]'s reason.
func (a *app) targetPickShowing() bool { return a.at(pageHome) && a.target.pick.open }

// ── the rule ────────────────────────────────────────────────────────────────

// The sentences home's rule says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// targetProjectLead names the workspace at the right of either seam.
	targetProjectLead = "project: "
	// The draft's hints name project, effort and approval controls. The model's
	// command is `/model`, so it spends no extra shortcut on the foot.
	projectKey            = "alt+p"
	targetFolderKeyWord   = projectKey + " project"
	targetEffortKeyWord   = effortKey + " effort"
	targetApprovalKeyWord = approvalKey + " approvals"
	// The switcher reaches conversations this machine already has (hop.go).
	targetSwitcherKeyWord = "alt+k chats"
	// targetPickWord is the foot while the model list is open, in the hint
	// grammar — the same sentence the composer layer's own list says, because
	// it is the same list answering the same keys ([composerPickWord]). It is
	// the WALK and the two ways out; what the keys in the middle do depends on
	// the row the cursor is on, and [app.targetPickFoot] puts them between.
	targetPickWord = "↑↓ pick · enter choose · esc back"
	// targetPickWalkWord and targetPickLeaveWord are that sentence's two ends,
	// so the middle can be spliced in without a second spelling of either.
	targetPickWalkWord  = "↑↓ pick"
	targetPickLeaveWord = "esc back"
)

// targetChordWords is the draft's chords as a clause on home's foot
// (footswap.go: the lowest line is for keys): the folder chord where there is
// somewhere to walk to, approvals where the control can act, the switcher
// where there is anywhere to go, and `/ commands` while the box is empty.
// Nothing while the model list
// is up — it has the whole keyboard (SCREEN 3a's clause: no key does anything
// that is not drawn on screen right now), and the foot is already saying the
// keys that do ([targetPickWord]). It is spelled for this keyboard by
// [app.placeHint]'s one pass, with the rest of the line.
//
// Until 2026-09-17 these were the RULE's right-hand label; the rule's right is
// bare now, and [app.targetLegend] draws its left alone.
func (a *app) targetChordWords() string {
	if a.target.pick.open {
		return ""
	}
	right := ""
	if a.targetMovable() {
		right = targetFolderKeyWord
	}
	if _, ok := a.targetEffort(); ok {
		right = dotted(right, targetEffortKeyWord)
	}
	if _, ok := a.targetApproval(); ok {
		right = dotted(right, targetApprovalKeyWord)
	}
	// The switcher is named where it would act and nowhere else
	// ([app.hopAvailable] answers off a remembered count rather than walking
	// the disk on the paint path).
	if a.hopAvailable() {
		right = dotted(right, targetSwitcherKeyWord)
	}
	// AND `/ commands` GOES THE MOMENT SOMETHING IS TYPED, because the drop-up
	// it names is already open over the box and a pointer at a list a person is
	// looking at is furniture (render.go's [microcopy] holds the original). It
	// is home's clause alone: on the other places a `/` is a character in the
	// box and opens no list (homeslash.go is home's).
	if a.at(pageHome) && a.home.box.empty() {
		right = dotted(right, microcopy)
	}
	return right
}

// targetProject is the destination of the next conversation, written as a
// path so projects with the same basename remain distinguishable.
func (a *app) targetProject() string {
	return a.hostedPath(a.placeWord(tildePath(a.targetWhere(), a.tilde)))
}

// targetLegend keeps model, effort and approvals at the left. The project
// used to stand at its right and is on the keys row under the box now
// (hometip.go); the three doors' click spans are measured from this layout,
// so they follow the text.
func (a *app) targetLegend(width int, pal palette) (string, bool) {
	a.clearTargetSpans()
	if width < 1 {
		return "", false
	}
	if note := a.placeNoteLegend(width); note != "" {
		return a.draftNoteRule(width, pal, note)
	}
	left, model, rung, gate := a.draftSeamLeft(legendRoom(width, ""))
	// THE PROJECT LEFT THE RULE FOR THE KEYS ROW on 2026-09-22 (hometip.go's
	// [app.homeFootLine]), so the right of home's rule is bare and its door
	// is recorded where the path is drawn now. A conversation's seam still
	// names its workspace at the right (foot.go).
	line, _, ok := a.legendLinePainted(left, "", "", width, a.draftSeamPaint(pal, model, rung, gate))
	if !ok {
		return "", false
	}
	a.targetModelSpan = shiftIntoBorder(model)
	a.targetEffortSpan, a.targetApprovalSpan = shiftIntoBorder(rung), shiftIntoBorder(gate)
	return line, true
}

// draftNoteRule is the rule on a place with a note: the note whole, beside
// whichever rung of the draft's ladder fits beside it; the note alone where
// none does; and the note cut, one ellipsis, on a frame too narrow for even
// that — because a statement about the whole page outranks a draft whose
// chords still work unprinted.
func (a *app) draftNoteRule(width int, pal palette, note string) (string, bool) {
	if left, model, rung, gate := a.draftSeamLeft(legendRoom(width, note)); left != "" {
		if line, ok := a.legendLine(left, note, width, a.draftSeamPaint(pal, model, rung, gate)); ok {
			a.targetModelSpan = shiftIntoBorder(model)
			a.targetEffortSpan, a.targetApprovalSpan = shiftIntoBorder(rung), shiftIntoBorder(gate)
			return line, true
		}
	}
	// Alone, [app.legendLine] spends one cell of rule at the head, the note's
	// own frame of three, and at least one cell of fill.
	if room := width - 5; ansi.StringWidth(note) > room {
		if room < 1 {
			return "", false
		}
		note = ansi.Truncate(note, room, "…")
	}
	return a.legendLine("", note, width, pal.dim)
}

// clearTargetSpans forgets where the draft's four doors were, for a frame
// that did not draw them.
func (a *app) clearTargetSpans() {
	a.targetFolderSpan, a.targetModelSpan = hudSpan{}, hudSpan{}
	a.targetEffortSpan, a.targetApprovalSpan = hudSpan{}, hudSpan{}
}

// ── the chords ──────────────────────────────────────────────────────────────
//
// `alt+p`, `alt+e`, `alt+a` and every key the model list over the
// target takes are [app.placeTargetKey] (boxseam.go), read from
// [placeHome.owns] before the router claims a single chord and from
// [app.placeKeyPress] on the other places.

// targetDestinations uses the projects panel's order, including projects known
// only through standing work. The selected path never moves to the front: doing
// that on each press traps the cycle between the pin and the launch folder.
// Everything comes from home's caches, so the hint can ask without disk I/O.
func (a *app) targetDestinations() []string {
	world := a.home.world
	world.Projects = a.home.everyProject()
	launch := a.home.launch
	if launch == "" {
		launch = a.workspace
	}
	in := homeGridInput{world: world, launch: launch, bucket: a.home.bucket}
	out := make([]string, 0, len(world.Projects)+1)
	seen := map[string]bool{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	for _, project := range projectsOrdered(&in) {
		add(project.Path)
	}
	return out
}

// targetMovable asks the same list the key and seam click walk.
func (a *app) targetMovable() bool {
	places := a.targetDestinations()
	return len(places) > 1 || len(places) == 1 && places[0] != a.targetWhere()
}

// moveTarget walks the projects once in panel order and wraps at the end.
// Both the keyboard and the seam press use this one persistent selection.
func (a *app) moveTarget() bool {
	places := a.targetDestinations()
	if len(places) == 0 || len(places) == 1 && places[0] == a.targetWhere() {
		return false
	}
	here := a.targetWhere()
	next := places[0]
	for i, path := range places {
		if path == here {
			next = places[(i+1)%len(places)]
			break
		}
	}
	return a.pinTargetProject(next)
}

// pinTargetProject is the shared selection for cycling and clicking a project.
// A pending pasted-folder offer must yield to this explicit choice, while its
// text stays in the draft just as it does after Option+P.
func (a *app) pinTargetProject(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	a.target.where = path
	if a.home.projectPaste.path != "" {
		a.home.projectPaste.path = ""
		a.home.build()
	}
	a.touch()
	return true
}

// openTargetPicker opens the model list for a press on the name or `/model`.
// It asks the chat slot's question and points at the TARGET rather than at
// the conversation behind the screen.
//
// It opens on the target's own model for [picker.start]'s stated reason: the
// cursor sits on what you are on, so enter confirms rather than changes.
func (a *app) openTargetPicker() {
	a.noticeEvent(eventModelListOpened)
	a.target.pick.startFor(a.modelsFor(chatModel), a.targetModel(), chatModel)
	// AND THE PROVIDERS OPEN HERE TOO. The box under this list has always named
	// `→ providers`, and for one wave the key did nothing at all, because the
	// list was never handed the slot, the pin and the routing row a fold needs
	// ([app.armLanes]). A pin belongs to your home rather than to one
	// conversation (lanes.md says so in those words), so it is exactly as
	// writable from the next conversation's draft as from a live one.
	a.armLanes(&a.target.pick, laneSlotFor(a.targetModel()))
	a.touch()
}

// targetReasoningFor is the rung one model stands at FOR THE NEXT CONVERSATION:
// what `ctrl+t` has dialled onto this draft, and otherwise what this window's
// own conversation would answer.
//
// THE FALLBACK IS THE WINDOW'S BECAUSE THE DRAFT INHERITS IT. A conversation
// started from home with nothing dialled runs on whatever the model already
// stands at, so a row that drew nothing until it was touched would be telling a
// person their model thinks at `auto` when it does not.
func (a *app) targetReasoningFor(id string) string {
	if level, held := a.target.levels[session.ReasoningKey(id)]; held {
		return level
	}
	return a.reasoningFor(id)
}

// cycleTargetReasoning is `ctrl+t` over the draft's list: the same walk the
// conversation's own list takes ([app.cycleReasoning]), written to the draft
// instead of to an agent.
func (a *app) cycleTargetReasoning() {
	chosen, ok := a.target.pick.choice()
	if !ok || !chosen.Reasoning {
		return
	}
	if a.target.levels == nil {
		a.target.levels = map[string]string{}
	}
	a.target.levels[session.ReasoningKey(chosen.ID)] = nextReasoning(a.targetReasoningFor(chosen.ID))
}

// targetPickKey is every key while that list is open: the walk and the filter
// are the picker's own ([picker.navigate]), and the one decision is this
// file's — exactly the split the composer layer and the settings panel keep.
func (a *app) targetPickKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.target.pick.close()
	// ENTER CHOOSES AND LEAVES THE LIST UP, which is /model's own rule and for
	// its reason ([app.pickerKey]): the list is a table, and a table that shuts
	// on the first press cannot be compared against. `esc` is the way out.
	case "enter":
		chosen, ok := a.target.pick.choice()
		// ENTER ON A PROVIDER PINS THE PROVIDER — and pins the model under it
		// too, for the reason /model's own list gives: somebody who opened a
		// model's providers and chose one asked for that model on that machine,
		// and a lane pinned under a model they never picked is a setting that
		// takes effect the next time they happen to switch.
		row, onLane := a.target.pick.laneUnder()
		if ok && onLane {
			a.pinTargetModel(chosen.ID)
			a.applyLaneChoice(chosen.ID, row, a.target.pick.lanes)
			a.restatePicker(&a.target.pick, a.targetModel())
			a.target.pick.showChoice(row)
			a.touch()
			return nil
		}
		if ok {
			a.pinTargetModel(chosen.ID)
			a.restatePicker(&a.target.pick, a.targetModel())
		}
	// The rung the next conversation starts at, held on the draft until there
	// is an agent to spend it on ([targetDraft.levels]).
	case "ctrl+t":
		a.cycleTargetReasoning()
	// THE FOLD IS THE LIST'S OWN KEY MAP and not this door's ([picker.foldKey]),
	// exactly as /model and the settings panel read it — a fold that opened
	// from one door and not another would be two pickers again.
	default:
		if !a.target.pick.foldKey(msg.String()) {
			a.target.pick.navigate(msg)
		}
	}
	a.touch()
	return nil
}

// pinTargetModel is the one road a model pin takes — from the list, and from
// `/model <slug>` typed out — so the two cannot grow two answers to what a pin
// does or to what it says afterwards.
//
// NOTHING BEHIND HOME IS TOUCHED. `/model <slug>` used to call
// [app.switchModel] on the conversation this window was holding, which
// re-modelled a conversation the person was not looking at and wrote the answer
// into it, where it could not be read. A pin changes the DRAFT and says so on
// the line under the box.
func (a *app) pinTargetModel(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	a.target.model = id
	a.refreshCreditWarnings()
	if a.homeCreditWarning != "" {
		a.askCredits(credits.PaidSwitch)
	}
	// NO NOTE. This used to say `model · <name> · for the next conversation you
	// start here` on the line under the box, and the line it was answering —
	// "did that change the conversation behind home?" — is answered better by the
	// SEAM, which carries the pinned model beside the folder and carries it for as
	// long as the pin lasts rather than until the next note replaces it. A
	// sentence that repeats what is already on the screen is a sentence that costs
	// the row something else could have used (the owner's ruling).
	a.touch()
}

// targetPickRows is that list drawn in the body's room. It is
// [app.composerPickRows]'s own body, lifted so the two doors draw one list: a
// place takes the frame whole, so the bottom-anchored overlay has nothing under
// it to sit on and the frame draws the rows itself.
func (a *app) targetPickRows(width, room int, pal palette) []placeRow {
	return pickerRowsIn(&a.target.pick, width, room, pal, a.targetReasoningFor)
}

// pickerRowsIn is the shared body: one picker's rows, padded to the room the
// frame reserved for them.
func pickerRowsIn(p *picker, width, room int, pal palette, level func(string) string) []placeRow {
	lines := p.rows(width, room, pal, -1, level)
	rows := make([]placeRow, 0, room)
	for _, line := range lines {
		if len(rows) >= room {
			break
		}
		rows = append(rows, placeRow{text: line, hit: nil})
	}
	for len(rows) < room {
		rows = append(rows, placeRow{})
	}
	return rows
}

// ── the phone ───────────────────────────────────────────────────────────────

// targetPhoneRule uses the same model-first rule as wider home frames. The
// phone's own action bar still owns the keys below the box.
func (a *app) targetPhoneRule(width int, pal palette) string {
	if line, ok := a.targetLegend(width, pal); ok {
		return line
	}
	return pal.dim(rule(width))
}

package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	// `ctrl+v` and a press on the cell, and — like the model — it survives the
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
	// targetPinnedModelWord is what home's message line says when a model has
	// been pinned onto the draft. It names the slug and then the SCOPE of what
	// just happened, because "did that change the conversation behind home"
	// is the exact question the old silent `/model` left a person holding.
	targetPinnedModelWord = " · for the next conversation you start here"
	// targetPickWord is the foot while the model list is open, in the hint
	// grammar — the same sentence the composer layer's own list says, because
	// it is the same list answering the same keys ([composerPickWord]).
	targetPickWord = "↑↓ pick · enter use it · esc back"
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

// targetLegend keeps model, effort and approvals at the left, with the
// project at the right. A long project gives up its right end first.
// Its click span is measured from that same layout, so it follows the text.
func (a *app) targetLegend(width int, pal palette) (string, bool) {
	a.clearTargetSpans()
	if width < 1 {
		return "", false
	}
	if note := a.placeNoteLegend(width); note != "" {
		return a.draftNoteRule(width, pal, note)
	}
	left, model, rung, gate := a.draftSeamLeft(legendRoom(width, ""))
	right, project := seamProjectRight(left, "", a.targetProject(), width)
	painted := a.paintSeamProject(right, project, a.targetHover == hoverSeamProject)
	line, at, ok := a.legendLinePainted(left, right, painted, width, a.draftSeamPaint(pal, model, rung, gate))
	if !ok {
		return "", false
	}
	a.targetModelSpan = shiftIntoBorder(model)
	a.targetEffortSpan, a.targetApprovalSpan = shiftIntoBorder(rung), shiftIntoBorder(gate)
	if project.pressable() {
		a.targetFolderSpan = hudSpan{from: at + project.from, to: at + project.to}
	}
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
// `alt+p`, `ctrl+v`, `alt+a` and every key the model list over the
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
	a.target.where = next
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
	a.target.pick.startFor(a.modelsFor(chatModel), a.targetModel(), chatModel)
	a.touch()
}

// targetPickKey is every key while that list is open: the walk and the filter
// are the picker's own ([picker.navigate]), and the one decision is this
// file's — exactly the split the composer layer and the settings panel keep.
func (a *app) targetPickKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.target.pick.close()
	case "enter":
		if chosen, ok := a.target.pick.choice(); ok {
			a.target.pick.close()
			a.pinTargetModel(chosen.ID)
			a.touch()
			return nil
		}
		a.target.pick.close()
	default:
		a.target.pick.navigate(msg)
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
	a.home.say("model · "+modelBase(id)+targetPinnedModelWord, "")
	a.touch()
}

// targetPickRows is that list drawn in the body's room. It is
// [app.composerPickRows]'s own body, lifted so the two doors draw one list: a
// place takes the frame whole, so the bottom-anchored overlay has nothing under
// it to sit on and the frame draws the rows itself.
func (a *app) targetPickRows(width, room int, pal palette) []placeRow {
	return pickerRowsIn(&a.target.pick, width, room, pal, a.reasoningFor)
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

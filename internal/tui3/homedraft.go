package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE TARGET — the box at home is a draft for the conversation it opens ────
//
// SCREEN: home's rule stops being a bare line and becomes a legend, exactly as
// the conversation's seam is. Its left says WHERE THE NEXT CONVERSATION WILL
// OPEN and WHAT IT WILL RUN ON; its right says the two chords that change them.
//
//	─ → new conversation in ~/src/parser · glm-5.3-flash ──── alt+w folder · alt+o model · / commands ─
//	 › say what you want done
//
// The conversation's own seam says `porting the parser · glm-5.3-flash`; home's
// says `→ new conversation in ~/src/parser · glm-5.3-flash`. Same line, same
// position, same two chords to edit it — and THE ARROW IS THE WHOLE DIFFERENCE
// between where I am and where this is going ([tokens.GTarget] is that mark's
// slot, and it is asked for through the palette door because a literal cannot
// know which repertoire the terminal is on).
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
// which is what closes the disagreement. `alt+w` and `alt+o` pin it, and a pin
// is a decision a person made.
//
// ── OWNER RULING: WHAT PERSISTS AND WHAT IS SPENT ───────────────────────────
//
// BOTH PINS SURVIVE `esc` AND A REOPEN OF HOME. They are the session's, not the
// screen's: "let me set the model before I start" is worth nothing if walking
// into a conversation and back out forgets it.
//
// THE FOLDER PIN IS SPENT WHEN A CONVERSATION STARTS FROM HOME and the model
// pin is not. They are different kinds of decision: a folder is where THIS
// sentence goes, and once it has gone there the pin is a stale answer to a
// question nobody asked again — the cursor's own row is the honest reading from
// then on. A model is a preference about how you work, and a person who set it
// once meant it for the next one too.

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
	// `alt+o`, and — the owner's ruling above — it survives the conversation
	// that spends the folder pin.
	model string
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

// targetModelPinned is whether a person SET this model, which is the one thing
// the rule paints differently. A pin that matches the window's own model is not
// a pin worth shouting about — the accent says "you changed this", and a change
// that changed nothing would be the surface pointing at itself.
func (a *app) targetModelPinned() bool {
	pinned := strings.TrimSpace(a.target.model)
	return pinned != "" && pinned != strings.TrimSpace(a.model)
}

// targetPickShowing is whether the model list over the target is up. It is
// asked by the frame, by the router and by the foot, and it is ONE field for
// [app.composerShowing]'s reason.
func (a *app) targetPickShowing() bool { return a.at(pageHome) && a.target.pick.open }

// spendTargetWhere is the folder pin being SPENT: a conversation started from
// home has used it, and what is honest from the next frame on is the cursor's
// own row again (the owner's ruling in this file's header). The model pin is
// deliberately not touched.
func (a *app) spendTargetWhere() { a.target.where = "" }

// ── the rule ────────────────────────────────────────────────────────────────

// The sentences home's rule says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// targetLeadWord leads the left label. It is `new conversation in ` and not
	// `here` because this line is about something that does not exist yet: the
	// conversation `enter` is about to open.
	targetLeadWord = "new conversation in "
	// targetFolderKeyWord and targetModelKeyWord are the two chords, in the hint
	// grammar every other legend on this surface is written in — the key is the
	// payload and the noun says what it moves (payload.go).
	targetFolderKeyWord = "alt+w folder"
	targetModelKeyWord  = "alt+o model"
	// targetPinnedModelWord is what home's message line says when a model has
	// been pinned onto the draft. It names the slug and then the SCOPE of what
	// just happened, because "did that change the conversation behind home"
	// is the exact question the old silent `/model` left a person holding.
	targetPinnedModelWord = " · for the next conversation you start here"
	// targetMovedWord is what the same line says when `alt+w` moved the folder.
	targetMovedWord = "next conversation opens in "
	// targetPickWord is the foot while the model list is open, in the hint
	// grammar — the same sentence the composer layer's own list says, because
	// it is the same list answering the same keys ([composerPickWord]).
	targetPickWord = "↑↓ pick · enter use it · esc back"
)

// targetLegendRight is the right of home's rule: the two chords that edit the
// target, and — at rest, where there is room — the door onto the command list.
//
// A KEY IS DRAWN ONLY WHERE IT DOES SOMETHING (SCREEN 3a). `alt+w folder` is
// absent on a machine with one project, because there is nowhere to move the
// next conversation to, and a legend that named a chord nothing answered would
// be the exact defect this line exists to end.
func (a *app) targetLegendRight() string {
	// AND WHILE THE MODEL LIST IS UP THE RULE NAMES NOTHING. That list has the
	// whole keyboard (SCREEN 3a's clause: no key does anything that is not drawn
	// on screen right now), so three chords on the border would be three chords
	// that do nothing — and the foot under the box is already saying the keys
	// that do ([targetPickWord]).
	if a.target.pick.open {
		return ""
	}
	right := targetModelKeyWord
	if a.targetMovable() {
		right = dotted(targetFolderKeyWord, targetModelKeyWord)
	}
	// AND `/ commands` GOES THE MOMENT SOMETHING IS TYPED, because the drop-up
	// it names is already open over the box and a pointer at a list a person is
	// looking at is furniture (render.go's [microcopy] holds the original).
	if a.home.box.empty() {
		return dotted(right, microcopy)
	}
	return right
}

// targetLegendLeft is the left of home's rule, built to a budget, and the
// columns its folder and model segments occupy within it.
//
// THE LADDER GIVES UP THE CHEAPEST TRUE THING FIRST, and THE KEYS ARE NEVER
// DROPPED BEFORE THE LABEL IS SHORTENED — which is the opposite of the
// conversation's seam and is right for the opposite reason: a conversation's
// name cannot be reconstructed from anywhere else on the frame, and a folder
// can (the row under the cursor says it). So:
//
//	1  the model goes — the folder is the fact `enter` acts on
//	2  the folder is said shorter, at [shortPath]'s two harder strengths
//	3  what is left is cut, one ellipsis
//
// Both segments are doors — the model onto the model list, the folder onto the
// cycle — so their columns are handed back for the press
// (placemouse.go's [app.placeTargetPress]).
func (a *app) targetLegendLeft(room int) (string, hudSpan, hudSpan) {
	model := modelBase(a.targetModel())
	mark := a.icon(tokens.GTarget)
	// A rung: the arrow, the lead, the folder at one strength, and the model —
	// or "" when even the folder alone does not fit at this strength.
	rung := func(hard int, withModel bool) (string, hudSpan, hudSpan, bool) {
		where := a.hostedPath(a.placeWord(shortPath(a.targetWhere(), a.tilde, hard)))
		if where == "" {
			return "", hudSpan{}, hudSpan{}, false
		}
		head := mark + " " + targetLeadWord
		line := head + where
		folder := hudSpan{from: ansi.StringWidth(head), to: ansi.StringWidth(line)}
		var slot hudSpan
		if withModel && model != "" {
			line = dotted(line, model)
			slot = hudSpan{from: ansi.StringWidth(line) - ansi.StringWidth(model), to: ansi.StringWidth(line)}
		}
		if ansi.StringWidth(line) > room {
			return "", hudSpan{}, hudSpan{}, false
		}
		return line, folder, slot, true
	}
	for _, attempt := range []struct {
		hard  int
		model bool
	}{{0, true}, {0, false}, {1, false}, {2, false}} {
		if line, folder, slot, ok := rung(attempt.hard, attempt.model); ok {
			return line, folder, slot
		}
	}
	return "", hudSpan{}, hudSpan{}
}

// targetLegendCut is the rung below all of them, and it is reached only once
// the right has given up every clause it has: the lead goes, and what is left
// is cut to the frame.
//
// A CUT FOLDER IS STILL AN ANSWER. Half a path with an ellipsis on it says which
// machine's disk and roughly where, and a rule with nothing on it says nothing
// at all — which is the emptiness law read the right way round, because there IS
// something true to draw here.
func (a *app) targetLegendCut(room int) (string, hudSpan) {
	where := a.hostedPath(a.placeWord(shortPath(a.targetWhere(), a.tilde, 2)))
	if where == "" {
		return "", hudSpan{}
	}
	head := a.icon(tokens.GTarget) + " "
	line := fit(head+where, room)
	if ansi.StringWidth(line) <= ansi.StringWidth(head) {
		return "", hudSpan{}
	}
	return line, hudSpan{from: ansi.StringWidth(head), to: ansi.StringWidth(line)}
}

// targetLegend is home's rule as a whole line, and it reports whether it drew
// one — a frame with no room for either label falls back to the bare rule the
// other six places keep.
//
// WHERE THE DOORS LANDED IS WRITTEN HERE, as the line is laid out, for the
// reason [app.legendLine] gives about the model segment: a column read from
// anywhere else is a column from the frame before this one. The two spans are
// offset by the border's own two cells, which is what [app.legendLine] puts in
// front of the label.
func (a *app) targetLegend(width int, pal palette) (string, bool) {
	a.targetFolderSpan, a.targetModelSpan = hudSpan{}, hudSpan{}
	if width < 1 {
		return "", false
	}
	right := a.targetLegendRight()
	for {
		left, folder, model := a.targetLegendLeft(legendRoom(width, right))
		if left != "" {
			// THE PINNED MODEL WEARS THE ACCENT, and only when it differs from
			// what this window would have used anyway: the accent is the surface
			// saying "you set this", and a mark on a figure nobody chose would be
			// the screen congratulating itself.
			paint := pal.dim
			if a.targetModelPinned() && model.pressable() {
				paint = func(text string) string {
					return paintSpan(text, model, pal.dim, pal.accent, true)
				}
			}
			if line, ok := a.legendLine(left, right, width, paint); ok {
				shift := func(s hudSpan) hudSpan {
					if !s.pressable() {
						return hudSpan{}
					}
					return hudSpan{from: s.from + 2, to: s.to + 2}
				}
				a.targetFolderSpan, a.targetModelSpan = shift(folder), shift(model)
				return line, true
			}
		}
		// THE RIGHT GIVES UP ITS LAST CLAUSE AND THE LEFT IS MEASURED AGAIN FROM
		// THE TOP, in the fixed order the legend's own ladder uses — never a
		// second, shorter sentence invented for a narrow frame.
		//
		// THE LABEL IS SHORTENED BEFORE A KEY IS DROPPED, which is the whole of
		// why the two ladders are nested this way round: the left gives up its
		// model and then its path, all four rungs, and only when none of them
		// fits does the right lose a clause. It is the opposite of the
		// conversation's seam ([app.legend] spends the hint slot before it cuts
		// the name) and it is right for the opposite reason — a conversation's
		// name is written nowhere else on the frame and a folder is on the row
		// under the cursor.
		next, shorter := targetRightShorter(right)
		if !shorter {
			break
		}
		right = next
	}
	// AND THE CUT IS THE LAST RESORT OF ALL, with the keys already gone.
	if left, folder := a.targetLegendCut(legendRoom(width, "")); left != "" {
		if line, ok := a.legendLine(left, "", width, pal.dim); ok {
			if folder.pressable() {
				a.targetFolderSpan = hudSpan{from: folder.from + 2, to: folder.to + 2}
			}
			return line, true
		}
	}
	return "", false
}

// targetRightShorter drops the last clause off the rule's right, and reports
// whether there was one to drop. The order is the drop order: the command list
// first (a person who has found "/" has found it), then the folder chord, and
// `alt+o model` is the last thing standing because the model is the fact this
// line is otherwise about to stop saying.
func targetRightShorter(right string) (string, bool) {
	at := strings.LastIndex(right, legendJoin)
	if at < 0 {
		return "", right != ""
	}
	return right[:at], true
}

// ── the two chords ──────────────────────────────────────────────────────────

// homeTargetKey is `alt+w`, `alt+o` and every key the model list over the
// target takes. It is read from [placeHome.owns] — BEFORE the router claims a
// single chord — for two different reasons at once.
//
// THE MODEL LIST HAS THE WHOLE KEYBOARD while it is up, which is the same
// arbitration every other whole-keyboard layer on this surface gets.
//
// AND THE TWO CHORDS ARE HELD BACK FROM THE PLACES BY THE ROUTER
// (placekeys.go's `case "alt+w", "alt+o"` swallows both), on the argument that
// they mean one thing and only inside the composer layer. That argument is
// still true of the other six places and it stopped being true of home the day
// home grew a target: the chords edit exactly the two facts the rule above the
// box states, one row from the hand. So home claims them before the router
// swallows them, and the other six are untouched.
func (a *app) homeTargetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.at(pageHome) {
		return nil, false
	}
	if a.target.pick.open {
		return a.targetPickKey(msg), true
	}
	switch msg.String() {
	case "alt+o":
		a.openTargetPicker()
		return nil, true
	case "alt+w":
		a.moveTarget()
		return nil, true
	}
	return nil, false
}

// targetMovable is whether `alt+w` has anywhere to go, ASKED BY THE DRAW.
//
// IT MAY NOT WALK THE DISK. [app.composerDestinations] falls back to
// [app.readWorld] when the projects have not been read yet, and a walk is a
// thing a keystroke may do and a draw may not (ARCHITECTURE.md's fourth law) —
// so this counts what home has already read and never asks for more. The chord
// itself uses the full list, because a keystroke may pay for it.
func (a *app) targetMovable() bool {
	seen := map[string]bool{}
	for _, path := range append([]string{a.targetWhere(), a.workspace}, projectPaths(a.home.world)...) {
		if path = strings.TrimSpace(path); path != "" {
			seen[path] = true
		}
		if len(seen) > 1 {
			return true
		}
	}
	return false
}

// projectPaths is the world's projects as bare paths, for the count above.
func projectPaths(world session.World) []string {
	out := make([]string, 0, len(world.Projects))
	for _, project := range world.Projects {
		out = append(out, project.Path)
	}
	return out
}

// moveTarget is `alt+w`: the next folder on the list, round again from the
// last, said on home's own message line.
//
// IT IS [app.composerMove]'S OWN WALK over the same list, and it is the same
// list on purpose ([app.composerDestinations]): "everywhere a thing this window
// starts can go" is one question, and two answers to it would be two cycles a
// person has to learn separately on one screen.
func (a *app) moveTarget() bool {
	places := a.composerDestinations()
	if len(places) < 2 {
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
	a.home.say(targetMovedWord+a.hostedPath(shortPath(next, a.tilde, 0)), "")
	a.touch()
	return true
}

// openTargetPicker is `alt+o` and a bare `/model` at home: THE model list,
// asked the chat slot's own question and pointed at the TARGET rather than at
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

// targetPhoneRule is home's rule at [tierPhone]: the same fact in the cells a
// phone has for it.
//
//	─ → ~/src/parser · glm-5.3-flash ─────────────────────
//
// THE LEAD GOES AND THE ARROW STAYS. `new conversation in` is eighteen cells of
// a forty-cell frame, and the arrow is the whole of what it said — where this
// is going. The model is dropped first and the folder is shortened after it,
// which is the wide rule's own ladder ([app.targetLegendLeft]) and for its
// reason: the folder is the fact `enter` acts on.
//
// The chords are not named here at all, because a phone has no `alt` — the
// thumb bar under the box is the whole key vocabulary of this tier
// (homephone.go's [app.homeBar]).
func (a *app) targetPhoneRule(width int, pal palette) string {
	bare := pal.dim(rule(width))
	if width < 1 {
		return bare
	}
	mark := a.icon(tokens.GTarget)
	model := modelBase(a.targetModel())
	room := legendRoom(width, "")
	for _, attempt := range []struct {
		hard  int
		model bool
	}{{0, true}, {0, false}, {1, false}, {2, false}} {
		where := a.hostedPath(a.placeWord(shortPath(a.targetWhere(), a.tilde, attempt.hard)))
		if where == "" {
			return bare
		}
		left := mark + " " + where
		if attempt.model && model != "" {
			left = dotted(left, model)
		}
		if ansi.StringWidth(left) > room {
			continue
		}
		if line, ok := a.legendLine(left, "", width, pal.dim); ok {
			return line
		}
	}
	return bare
}

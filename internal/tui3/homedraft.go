package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE TARGET — the box at home is a draft for the conversation it opens ────
//
// SCREEN: home's rule stops being a bare line and becomes a legend, exactly as
// the conversation's seam is. Its left says WHERE THE NEXT CONVERSATION WILL
// OPEN and WHAT IT WILL RUN ON; its right says the two chords that change them.
//
//	─ ◎ new conversation in ~/src/parser · glm-5.3-flash · ⠿ auto · ◇ asks ── alt+w folder · alt+o model · / commands ─
//	 › say what you want done
//
// The conversation's own seam says `porting the parser · glm-5.3-flash · ⠿
// high · ◇ asks`; home's says `◎ new conversation in ~/src/parser ·
// glm-5.3-flash · ⠿ auto · ◇ asks`. Same line, same position, the same three
// cells and the same doors on them (boxseam.go is the whole of that
// argument) — and THE MARK IS THE WHOLE DIFFERENCE between where I am and
// where this is going ([tokens.GTarget] is that mark's slot, and it is asked
// for through the palette door because a literal cannot know which repertoire
// the terminal is on).
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
	// effort is the rung the next conversation thinks at, "" for what the
	// install would do anyway ([app.targetEffortStanding]). It is pinned by
	// `ctrl+v` and a press on the cell, and — like the model — it survives the
	// conversation that spends the folder: how hard you think is how you work
	// (boxseam.go).
	effort string
	// approval is the posture the next conversation opens at, "" for the rows
	// as they stand ([app.targetApprovalStanding]). It is pinned by `alt+y` and
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
	// targetSwitcherKeyWord is the third chord, and it is the only one on this
	// line that does not edit the draft the line is about — it is the way to the
	// conversations this machine already has (hop.go).
	//
	// IT SAYS `chats` WHERE THE CONVERSATION'S OWN LEGEND SAYS `switch`, and that
	// is this line's grammar rather than a second name for one door. Every clause
	// here is a key and the NOUN IT MOVES — `alt+w folder`, `alt+o model` — so
	// the noun is what the third one has to carry too, and `chats` is the word
	// the tab bar's own control used to use before it was deleted (chattabs.go).
	// The conversation's legend is a list of VERBS in the same slot (`tab last`,
	// `space space home`), which is why the same door is `alt+k switch` there.
	targetSwitcherKeyWord = "alt+k chats"
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
	// AND THE SWITCHER AFTER THE TWO THAT EDIT THIS LINE'S OWN SUBJECT, under
	// the same clause as everything else here: it is named where it would act
	// and nowhere else ([app.hopAvailable] is the conversation legend's own
	// gate, and it answers off a remembered count rather than walking the disk
	// on the paint path). Its position IS its priority — [targetRightShorter]
	// drops from the right — so a narrow frame keeps the folder and the model,
	// which are the facts this rule states, and gives up the door to elsewhere.
	if a.hopAvailable() {
		right = dotted(right, targetSwitcherKeyWord)
	}
	// AND `/ commands` GOES THE MOMENT SOMETHING IS TYPED, because the drop-up
	// it names is already open over the box and a pointer at a list a person is
	// looking at is furniture (render.go's [microcopy] holds the original). It
	// is home's clause alone: on the other places a `/` is a character in the
	// box and opens no list (homeslash.go is home's), and a door named where it
	// does nothing is the defect SCREEN 3a forbids.
	if a.at(pageHome) && a.home.box.empty() {
		right = dotted(right, microcopy)
	}
	// AND THE WHOLE LINE IS SPELLED FOR THIS KEYBOARD ON THE WAY OUT — ONCE,
	// HERE, ABOVE THE MEASURING. Every chord on this rule wears a modifier with
	// two keycaps, and this line drew them straight out of their constants: a
	// Mac that spells `opt+s` on the memory place and `opt+1…opt+7` on the map
	// was spelling `alt+w` on home, which is one modifier under two names on one
	// screen. chords.go states that [chordSpelling.say] is the one door every
	// person-facing sentence about a chord goes through, and this line was not
	// going through it. It has to happen before the return rather than at the
	// paint because `⌘` is one cell where `cmd+` is four, and the ladder in
	// [app.targetLegend] measures what it is about to draw.
	return a.chords.say(right)
}

// targetLegend is the draft's rule as a whole line — the box seam with the
// draft's four facts on its left and the draft's chords on its right
// (boxseam.go) — and it reports whether it drew one: a frame with no room for
// either label falls back to the bare rule.
//
// THE LADDER GIVES UP THE CHEAPEST TRUE THING FIRST, and THE KEYS ARE NEVER
// DROPPED BEFORE THE LABEL IS SHORTENED — which is the opposite of the
// conversation's seam and is right for the opposite reason: a conversation's
// name cannot be reconstructed from anywhere else on the frame, and a folder
// can (the row under the cursor says it). So the left walks [draftLadder] to
// its last rung before the right gives up a clause, and the cut is the last
// resort of all, with the keys already gone.
//
// WHERE THE DOORS LANDED IS WRITTEN HERE, as the line is laid out, for the
// reason [app.legendLine] gives about the model segment: a column read from
// anywhere else is a column from the frame before this one. The spans are
// offset by the border's own two cells, which is what [app.legendLine] puts in
// front of the label.
func (a *app) targetLegend(width int, pal palette) (string, bool) {
	a.clearTargetSpans()
	if width < 1 {
		return "", false
	}
	// A PLACE'S OWN NOTE TAKES THE RULE FROM THE CHORDS. The tally on tasks, a
	// receipt on memory, the "this session is on another machine" line, the
	// chord diagnosis on any of them — each is a fact about the whole page, and
	// this rule is the one row the foot has for it (boxseam.go's
	// [app.placeNoteLegend] says why it is not a row). It is a statement and not
	// a key sheet, so it is never split at a dot: it stands whole beside the
	// fullest draft that fits, then whole with the draft given up, then cut.
	if note := a.placeNoteLegend(width); note != "" {
		return a.draftNoteRule(width, pal, note)
	}
	right := a.targetLegendRight()
	for {
		left, folder, model, rung, gate := a.draftSeamLeft(legendRoom(width, right))
		if left != "" {
			if line, ok := a.legendLine(left, right, width, a.draftSeamPaint(pal, model, rung, gate)); ok {
				a.targetFolderSpan, a.targetModelSpan = shiftIntoBorder(folder), shiftIntoBorder(model)
				a.targetEffortSpan, a.targetApprovalSpan = shiftIntoBorder(rung), shiftIntoBorder(gate)
				return line, true
			}
		}
		// THE RIGHT GIVES UP ITS LAST CLAUSE AND THE LEFT IS MEASURED AGAIN FROM
		// THE TOP, in the fixed order the legend's own ladder uses — never a
		// second, shorter sentence invented for a narrow frame.
		next, shorter := targetRightShorter(right)
		if !shorter {
			break
		}
		right = next
	}
	// AND THE CUT IS THE LAST RESORT OF ALL, with the keys already gone.
	if left, folder := a.targetLegendCut(legendRoom(width, "")); left != "" {
		if line, ok := a.legendLine(left, "", width, pal.dim); ok {
			a.targetFolderSpan = shiftIntoBorder(folder)
			return line, true
		}
	}
	return "", false
}

// draftNoteRule is the rule on a place with a note: the note whole, beside
// whichever rung of the draft's ladder fits beside it; the note alone where
// none does; and the note cut, one ellipsis, on a frame too narrow for even
// that — because a statement about the whole page outranks a draft whose
// chords still work unprinted.
func (a *app) draftNoteRule(width int, pal palette, note string) (string, bool) {
	if left, folder, model, rung, gate := a.draftSeamLeft(legendRoom(width, note)); left != "" {
		if line, ok := a.legendLine(left, note, width, a.draftSeamPaint(pal, model, rung, gate)); ok {
			a.targetFolderSpan, a.targetModelSpan = shiftIntoBorder(folder), shiftIntoBorder(model)
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

// ── the chords ──────────────────────────────────────────────────────────────
//
// `alt+w`, `alt+o`, `ctrl+v`, `alt+y` and every key the model list over the
// target takes are [app.placeTargetKey] (boxseam.go), read from
// [placeHome.owns] before the router claims a single chord and from
// [app.placeKeyPress] on the other places.

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

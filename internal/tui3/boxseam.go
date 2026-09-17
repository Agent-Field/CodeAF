package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE BOX SEAM — ONE RULE ABOVE EVERY BOX A PERSON TYPES TO AN AGENT IN ────
//
// Every box on this surface that sends words to a model has the same line
// over it, in the same shape, with the same three cells and the same three
// doors on them:
//
//	─ porting the parser · glm-5.3-flash · ⠿ high · ◇ asks · via deepinfra · main* ─── / commands ─
//	 › what changed in the relay this week
//
//	─ ◎ new conversation in ~/src/parser · glm-5.3-flash · ⠿ auto · ◇ asks ── alt+w folder · alt+o model · / commands ─
//	 › say what you want done
//
// The first is a conversation's seam (foot.go's THE SEAM IS WHO AND WHERE).
// The second is the rule over the box on home — the draft for a conversation
// that does not exist yet, and the one box on a place, because only home
// starts things (pages.go's [place.box]). Left to right, both say
// the same four things about the next turn: WHERE it goes, WHAT answers,
// HOW HARD it thinks, and WHAT IT MAY RUN WITHOUT ASKING. The model is a door
// onto the model list, the rung walks the thinking ladder (`ctrl+v`, or a
// press), and the gate walks the approvals wheel (`alt+y`, or a press). One
// shape, learned once.
//
// ── WHY THE DRAFT CARRIES THE RUNG AND THE GATE ────────────────────────────
//
// Until this file the rule on home said the folder and the model and nothing
// else; `alt+y` and `ctrl+v` did nothing outside a conversation; and the
// `YOLO` word was a chip on one screen and a badge on another. A person who
// learned the seam in a conversation arrived on home and found nothing they
// knew. (The six other places had a box of their own then, under a note or a
// bare rule with a `here ~/codeaf` chip; those boxes are gone — only home
// starts things.)
//
// The draft is the right place for the two dials because the question a
// person has at home — "start this one on the big model, thinking hard, and
// stop asking me" — is a question about the conversation `enter` is about to
// open, and the only honest moment to answer it is before it opens. So the
// draft has a rung pin and a gate pin beside its model pin, moved by the same
// chords the conversation's cells answer to, and [app.applyTargetPins] puts
// all of them onto the conversation the box opens.
//
// ── WHAT STICKS AND WHAT IS SPENT ──────────────────────────────────────────
//
// homedraft.go's ruling stands: the FOLDER pin is spent by the conversation
// that uses it and the MODEL pin survives, because one is where this sentence
// goes and the other is how a person works. The rung follows the model — how
// hard you think is how you work. The GATE is spent, like the folder: an open
// gate is the one pin on this line that is a safety claim, and a claim made
// about one conversation must not quietly become the default for every one
// after it. The cell is drawn always, so opening it again is one chord.
//
// ── WHAT THE UNPINNED CELLS SAY ────────────────────────────────────────────
//
// Both cells say what a fresh conversation WOULD run at on this install, and
// never what the conversation behind the screen is running at: the rung is
// the install's row folded with the level dialled onto the target model
// ([app.targetEffort]), and the gate is the rows as they stand, or the
// launch's `--yolo` ([session.Agent.StandingApprovalPosture]). A draft that
// echoed the conversation behind home would promise a gate the next
// conversation is not going to have.
//
// ── ABSENCE IS STILL ABSENCE ───────────────────────────────────────────────
//
// A window whose session has no dial — a `--host` connection to an engine
// without the doors, a surface booted on an agent that cannot say the
// install's rung — draws neither cell on the draft, on effortchip.go's own
// terms: a control with nothing behind it is left off rather than drawn and
// refused. The folder and the model are still there; they never needed a
// dial. Over `--host` the two facts are the FAR install's, carried once at the
// door (internal/remote's [Welcome.DefaultEffort] and
// [Welcome.StandingApproval]), because a conversation started from a hosted
// home opens on that machine.

// draftDialer is what the draft asks of the window's session to say and set
// what the next conversation opens at. Both halves are asserted on the agent
// rather than added to [Agent]: a session without them simply has no cell.
type draftEffortDialer interface {
	// DefaultEffort is the install's own row, the floor a fresh conversation
	// thinks at when nobody has dialled it.
	DefaultEffort() string
}

type draftApprovalDialer interface {
	// StandingApprovalPosture is what a conversation nobody has touched would
	// open at: the launch's word, else the rows as they stand.
	StandingApprovalPosture() string
}

// ── the rung ────────────────────────────────────────────────────────────────

// targetEffort is the rung the next conversation opens on: the pin, else what a
// fresh conversation on the target model resolves to on this install. "" is a
// window that cannot say.
func (a *app) targetEffort() (string, bool) {
	if pinned := strings.TrimSpace(a.target.effort); pinned != "" {
		return pinned, true
	}
	return a.targetEffortStanding()
}

// targetEffortStanding is the unpinned answer: the same resolver the engine
// runs at the top of a turn (internal/session's effort.go), fed the two scopes
// a conversation that does not exist yet already has — the level dialled onto
// the model it will open on, and the install's row.
func (a *app) targetEffortStanding() (string, bool) {
	if a.agent == nil {
		return "", false
	}
	dial, ok := a.agent.(draftEffortDialer)
	if !ok {
		return "", false
	}
	// OVER --host THE ROW IS THE FAR INSTALL'S, carried once at the door
	// (internal/remote's [Welcome.DefaultEffort]); an engine that could not say
	// answers "", and "" from a far install is absence rather than `auto`.
	if _, hosted := a.agent.(interface{ EffortSupported() bool }); hosted && dial.DefaultEffort() == "" {
		return "", false
	}
	turn, _ := effort.Parse(a.reasoningFor(a.targetModel()))
	install, _ := effort.Parse(dial.DefaultEffort())
	return effort.Resolve(effort.Scope{Turn: turn, Default: install}).String(), true
}

// targetEffortPinned is whether a person SET the rung, and set it to something
// the install would not have done anyway — the model pin's own test, for its
// reason: the accent says "you changed this".
func (a *app) targetEffortPinned() bool {
	pinned := strings.TrimSpace(a.target.effort)
	if pinned == "" {
		return false
	}
	standing, _ := a.targetEffortStanding()
	return pinned != standing
}

// targetEffortChip is the draft's thinking cell, in the conversation's own
// spelling, or "" where the window cannot say.
func (a *app) targetEffortChip() string {
	word, ok := a.targetEffort()
	if !ok {
		return ""
	}
	return a.effortChip(word)
}

// cycleTargetEffort is `ctrl+v` on a place with a draft, and the press on the
// cell: one step up the same wheel the conversation's rung walks, from the
// word the cell shows, back to `auto` off the top.
func (a *app) cycleTargetEffort() tea.Cmd {
	word, ok := a.targetEffort()
	if !ok {
		return nil
	}
	next := effortNextClearing(effort.Rung(word))
	a.target.effort = next.String()
	a.effortLit = effortMoved{where: effortScopeDraft, at: a.now()}
	said := next.String()
	if next == effort.None {
		said = effortAutoWord
	}
	a.placeSay("thinking · " + said + targetPinnedTail)
	a.touch()
	return surfaceTick(effortFlashFor, func(time.Time) tea.Msg { return effortFlashMsg{} })
}

// ── the gate ────────────────────────────────────────────────────────────────

// targetApproval is the posture the next conversation opens at: the pin, else
// what an untouched conversation stands at on this install. "" is a window
// that cannot say.
func (a *app) targetApproval() (string, bool) {
	if pinned := strings.TrimSpace(a.target.approval); pinned != "" {
		return pinned, true
	}
	return a.targetApprovalStanding()
}

func (a *app) targetApprovalStanding() (string, bool) {
	if a.agent == nil {
		return "", false
	}
	dial, ok := a.agent.(draftApprovalDialer)
	if !ok {
		return "", false
	}
	word := dial.StandingApprovalPosture()
	return word, word != ""
}

// targetApprovalPinned is [app.targetEffortPinned] for the gate.
func (a *app) targetApprovalPinned() bool {
	pinned := strings.TrimSpace(a.target.approval)
	if pinned == "" {
		return false
	}
	standing, _ := a.targetApprovalStanding()
	return pinned != standing
}

// targetApprovalOpen is whether the draft's gate is at a posture a person must
// not be able to forget — the conversation cell's own test ([app.approvalOpen]).
func (a *app) targetApprovalOpen() bool {
	word, _ := a.targetApproval()
	return word == session.PostureAllow || word == session.PostureDeny
}

// targetApprovalChip is the draft's gate cell, or "" where the window cannot
// say.
func (a *app) targetApprovalChip() string {
	word, ok := a.targetApproval()
	if !ok {
		return ""
	}
	return a.approvalChip(word)
}

// cycleTargetApproval is `alt+y` on a place with a draft, and the press on the
// cell: one stop round the same wheel the conversation's cell walks
// (approvalchip.go's [approvalNext]), so `deny` is never landed on by a press
// here either.
func (a *app) cycleTargetApproval() tea.Cmd {
	word, ok := a.targetApproval()
	if !ok {
		return nil
	}
	next := approvalNext(word)
	a.target.approval = next
	a.approvalLit = a.now()
	a.placeSay("approvals · " + approvalWordFor(next) + " · " + approvalLines[next] + targetPinnedTail)
	a.touch()
	return surfaceTick(approvalFlashFor, func(time.Time) tea.Msg { return approvalFlashMsg{} })
}

// targetPinnedTail is the scope clause every pin's note ends with, so a person
// reading "thinking · high" on home is told it is about the NEXT conversation
// and not the one behind the screen — the question the model pin's own note
// was written to answer ([targetPinnedModelWord]).
const targetPinnedTail = targetPinnedModelWord

// ── the pins, carried ───────────────────────────────────────────────────────

// applyTargetPins puts every pin onto the conversation the box has just
// opened, and spends the two that are spent. It is called AFTER the attach, for
// [app.applyTargetModel]'s reason: the agent it writes to has to be the new
// conversation's. The command it answers is the gate's move, and a caller
// batches it with the attach's own.
//
// THE RUNG AND THE GATE GO THROUGH THE SESSION'S OWN DOORS — the same
// [effortDialer] and [approvalDialer] the chord in a conversation uses — so a
// pin lands exactly where a press on the cell inside the conversation would
// have put it, sticky in that session's meta.json. A pin the new session cannot
// take (no dial) is dropped silently: the cell was never drawn on a window
// whose session has none, so there is nothing a person was promised.
//
// THE RUNG AND THE GATE ARE SET OFF THE LOOP (offloop.go's law), through the
// same doors [app.setEffortRung] and [app.setApprovalPosture] use: the engine
// rebuilds the gate and, over a connection, a far machine does, and neither is
// a thing this window may wait on under a keystroke. The pins are spent HERE,
// on the loop, so a door that is slow or refuses still leaves the draft honest
// on the next frame.
func (a *app) applyTargetPins() tea.Cmd {
	a.spendTargetWhere()
	a.applyTargetModel()
	rung, posture := strings.TrimSpace(a.target.effort), strings.TrimSpace(a.target.approval)
	a.target.approval = ""
	effortDial, hasRung := a.effortDial()
	hasRung = hasRung && rung != ""
	approvalDial, hasGate := a.approvalDial()
	hasGate = hasGate && posture != ""
	if !hasRung && !hasGate {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		if hasRung {
			effortDial.SetConversationEffort(rung)
		}
		var err error
		if hasGate {
			err = approvalDial.SetApprovalPosture(posture)
		}
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if hasGate && err == nil {
				a.approval = a.approvalPosture()
			}
			a.touch()
			return nil
		}
	})
}

// ── the keys ────────────────────────────────────────────────────────────────

// placeHasDraft reports whether the standing place's box is a draft for a
// conversation, which is home and home alone: only home starts things
// (pages.go's [place.box]), so only home draws the draft's rule and takes its
// chords.
func (a *app) placeHasDraft() bool { return a.at(pageHome) }

// placeTargetKey is the four chords that edit the draft on home: `alt+w`
// moves the folder, `alt+o` opens the model list, `ctrl+v` walks the rung and
// `alt+y` walks the gate. They are the conversation's own chords wherever the
// conversation has them — `alt+w`/`alt+o` are the layer's two
// (composerlayer.go), lifted to the draft they edit.
//
// It is read from home's [placeHome.owns], after the phone sheet and before
// the grid; no other place has a draft ([app.placeHasDraft]).
//
// `ctrl+v` ON A STANDING ITEM'S CARD IS THAT ITEM'S. The card names the key
// for the item's own rung (homeband_keys.go), and a card that named a key the
// draft then took would be the surface lying about the next keystroke; so on
// that one row the chord falls through to the card ([app.cycleHomeEffort]),
// and the draft's rung is still one press on its cell.
func (a *app) placeTargetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.placeHasDraft() {
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
	case effortKey:
		if a.at(pageHome) {
			if subject, ok := a.homeSubject(); ok && subject.kind == bandKindItem {
				return nil, false
			}
		}
		return a.cycleTargetEffort(), true
	case approvalKey:
		return a.cycleTargetApproval(), true
	}
	return nil, false
}

// placeSay is one line under the box about what a chord just did, on home's
// own field where home is standing and on the router's everywhere else — the
// two fields [app.placeMsgLine] already reads.
func (a *app) placeSay(msg string) {
	if a.at(pageHome) {
		a.home.say(msg, "")
		return
	}
	a.pageMsg = msg
}

// ── the rule ────────────────────────────────────────────────────────────────

// placeNoteLegend is the standing place's note as the seam's right label: the
// rows a place would have said under the rule, led and joined the way
// [placeNoteRule] joins them, with their paint taken off because the slot
// paints its own (render.go's [app.legendLine]). "" is a place with nothing to
// say, which is most of them most of the time.
//
// IT IS ON THE RULE AND NOT UNDER IT for PLACES-AUDIT.md finding 1's reason:
// the foot is the one edge a person's eye finds without looking, and a rule
// that stood one row higher on a place with a note moved under the eye on every
// `tab`. The chords the slot would otherwise name are still bound, and the
// cells they move are still doors under the pointer; a note is the place's own
// sentence and outranks a hint a person has read once.
func (a *app) placeNoteLegend(width int) string {
	parts := make([]string, 0, 2)
	for _, line := range a.placeNote(width - placeNoteRuleFrame + 2) {
		if text := strings.TrimSpace(ansi.Strip(line)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, railSep)
}

// draftTry is one rung of the draft's ladder: which cells stand beside the
// folder, and how hard the folder is shortened.
type draftTry struct {
	hard              int
	model, rung, gate bool
}

// draftLadder is THE LADDER, top to bottom, and it is the conversation seam's
// order for the three cells — the rung goes first, then the gate, then the
// model (foot.go's [seamLadder] says why the gate outranks the rung) — under
// home's own ruling that THE FOLDER IS NEVER SHORTENED WHILE A CELL COULD GO
// INSTEAD: the folder is the fact `enter` acts on, and a shorter spelling of it
// is worth less than a whole one with the model beside it is worth
// (homedraft.go's [app.targetLegendLeft] had the argument).
var draftLadder = []draftTry{
	{0, true, true, true},
	{0, true, false, true},
	{0, true, false, false},
	{0, false, false, false},
	{1, false, false, false},
	{2, false, false, false},
}

// draftPieces is the draft as the seam's own layout takes it: the folder line
// as the name, the target's model, and the two chips — with no host, no rider
// and no branch, because a conversation that does not exist yet has none.
func (a *app) draftPieces(hard int) (seamPieces, hudSpan) {
	where := a.hostedPath(a.placeWord(shortPath(a.targetWhere(), a.tilde, hard)))
	if where == "" {
		return seamPieces{}, hudSpan{}
	}
	head := a.icon(tokens.GTarget) + " " + targetLeadWord
	name := head + where
	folder := hudSpan{from: ansi.StringWidth(head), to: ansi.StringWidth(name)}
	return seamPieces{
		name:  name,
		model: modelBase(a.targetModel()),
		rung:  a.targetEffortChip(),
		gate:  a.targetApprovalChip(),
	}, folder
}

// draftSeamLeft is the draft's left label built to a budget, and the columns
// its four doors occupy: the folder inside the name, then the model, the rung
// and the gate as the seam records them ([seamSpans]).
func (a *app) draftSeamLeft(room int) (string, hudSpan, hudSpan, hudSpan, hudSpan) {
	none := hudSpan{}
	for _, try := range draftLadder {
		pieces, folder := a.draftPieces(try.hard)
		if pieces.name == "" {
			return "", none, none, none, none
		}
		cluster, model, rung, gate, ok := pieces.lay(seamTry{name: true, model: try.model, rung: try.rung, gate: try.gate}, room)
		if ok {
			return cluster, folder, model, rung, gate
		}
	}
	return "", none, none, none, none
}

// draftSeamPaint is the draft's line painted the way the conversation's seam
// is (render.go's [app.legend]): dim furniture, the pinned cells in the accent
// because a pin is a thing a person set, the open gate in the bad hue for as
// long as it is true, and the cell that just moved wearing THE EMPHASIS LAW's
// two moves for two seconds. The lifts are disjoint and painted side by side,
// never nested ([paintSpans]).
func (a *app) draftSeamPaint(pal palette, model, rung, gate hudSpan) func(string) string {
	return func(text string) string {
		return paintSpans(text, pal.dim,
			spanLift{span: model, lift: pal.accent, on: a.targetModelPinned()},
			spanLift{span: rung, lift: a.paintDraftEffortChip, on: a.draftEffortLit()},
			spanLift{span: gate, lift: a.paintDraftApprovalChip, on: a.draftApprovalLit()})
	}
}

func (a *app) draftEffortLit() bool {
	return a.effortFlashingIn(effortScopeDraft) || a.targetEffortPinned()
}

func (a *app) draftApprovalLit() bool {
	return a.approvalFlashing() || a.targetApprovalOpen() || a.targetApprovalPinned()
}

// paintDraftEffortChip is the conversation cell's own paint with the draft's
// facts fed in: the flash, else the pin's accent.
func (a *app) paintDraftEffortChip(text string) string {
	if a.effortFlashingIn(effortScopeDraft) {
		return a.paintChipFlash(text)
	}
	return a.pal.accent(text)
}

// paintDraftApprovalChip is [app.paintApprovalChipAs] with the draft's gate,
// and the pin's accent under both.
func (a *app) paintDraftApprovalChip(text string) string {
	switch {
	case a.approvalFlashing():
		return a.paintChipFlash(text)
	case a.targetApprovalOpen():
		return a.pal.bad(text)
	}
	return a.pal.accent(text)
}

// effortScopeDraft names the draft's rung in [effortMoved]'s namespace, beside
// [effortScopeConversation]: a NUL-led word no id can be.
const effortScopeDraft = "\x00draft"

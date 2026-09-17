package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE APPROVALS CHIP — what this conversation runs without asking, said on the
// seam beside the model and the rung, and moved from there.
//
//	─ porting the parser · glm-5.3-flash · ⠿ high · ◇ asks · via deepinfra · main* ── / commands ─
//	› what changed in the relay this week
//
// ── WHY IT EXISTS ──────────────────────────────────────────────────────────
//
// The tool gate had two doors and neither was in the conversation. The settings
// row (`ask before running`) sets the INSTALL and, until this wave, landed on
// the next session; `--yolo` opens the gate for a whole process and has to be
// typed before there is a conversation to type it about. So "stop asking me
// for this stretch of work" — the sentence people actually say — had no answer
// short of quitting, which is the same hole the thinking rung had before
// effortchip.go, and it is answered the same way: the conversation's own scope
// ([session.Agent.SetApprovalPosture], sticky in meta.json), a cell on the
// seam, a chord that walks it and a command that names it.
//
// ── IT IS DRAWN ALWAYS, AND THE HUE CARRIES THE WARNING ────────────────────
//
// The status row used to carry `YOLO` and NOTHING ELSE — drawn only when the
// gate was open, on the reasoning that a permanent "safe" badge is a badge
// nobody reads (render.go's old NEGATIVE-SPACE SAFETY). That law was right
// about a badge and wrong about a control: A CONTROL THAT IS INVISIBLE UNTIL
// YOU HAVE ALREADY USED IT IS NOT A CONTROL, which is the ruling that put
// `⠿ auto` on every fresh conversation (owner ruling 2026-09-11, effortchip.go).
// So the cell is drawn at every posture, and what the old law protected is
// kept by the PAINT rather than by the presence: `asks` and `guardian` are dim
// furniture like the rest of the seam, and `YOLO` and `refuses` wear the bad
// hue for as long as they are true — loud for what they mean, exactly as the
// badge was. The word `YOLO` is kept because it is the one the flag, the
// manual and the owner all use; a chip that said `allow` would be a third
// spelling of one fact.
//
// THE BADGE IS OFF THE STATUS ROW WHILE THE SEAM CARRIES THE CHIP. One fact
// spelled in two places on one frame is the defect the colon suffix made once
// (effortchip.go), so while this cell is drawn the row says nothing. The seam
// is not always there: the welcome box stands where the conversation will be,
// and a task's room replaces the seam's left with the way out. On those frames
// the old badge — `YOLO`, only when the gate is open — is back on the status
// row ([app.approvalSegment]), because the welcome is exactly where a person
// who typed `--yolo` reads whether it took, and a room is where the work the
// gate governs is running. Two places, never at once.
//
// ── THE WHEEL HAS THREE STOPS AND `deny` IS NOT ONE OF THEM ────────────────
//
// asks → guardian → YOLO → asks. The stops are monotonically more autonomy,
// which is the shape of every wheel like it in every other tool on this
// machine; `refuses` one press past `YOLO` would be a wheel that breaks a
// session by accident, so `deny` is reached by name (`/approvals deny`) and
// never by a press. `auto` — hand the conversation back to the settings rows —
// is by name too, for [session.ApprovalWheel]'s stated reason.
//
// ── THE CHORD IS alt+y, AND WHY NOT THE OBVIOUS TWO ────────────────────────
//
// `ctrl+y` is the copy-a-path key on home and in `/files`, and one chord means
// one verb on this surface (effortscope.go): a key that copies a path on one
// screen and opens the gate on another is two things to remember for one
// motion. `shift+tab` walks backwards through the fields of every question
// card and place, and on the terminals that cannot tell it from `tab` it would
// move the field instead of the gate. `alt+y` is bound nowhere, arrives on the
// terminals that already deliver `alt+o` and `alt+i` to this box, and the
// letter is the one on the flag.
//
// ── OVER A CONNECTION THE DIAL IS THE FAR MACHINE'S, AND IT CROSSES ────────
//
// The gate that decides whether a tool runs on a `--host` session is the far
// engine's, so the posture is set THERE (internal/remote's approval.go) and the
// word on this seam is the one that machine resolved. An engine too old to have
// the door says so at the welcome: the cell is then a reading of the posture
// that travelled once ([app.handedApproval]) — so the safety claim is on every
// frame — and the chord, the press and the command say the far machine's rules
// decide, because a knob that did nothing would be worse than none.

// approvalKey is the chord that walks the wheel, written down once: the router
// binds it, the manual prints it and the command row names it.
const approvalKey = "alt+y"

// The words a PERSON reads for each posture. `YOLO` is the flag's own word and
// the manual's; the other three are the permissions panel's vocabulary
// (permissions.go's `asks every time`, `refused`) said in one word.
const (
	approvalAsksWord     = "asks"
	approvalGuardianWord = "guardian"
	approvalYoloWord     = "YOLO"
	approvalRefusesWord  = "refuses"
)

// approvalFlashFor is how long the chip wears its change — the thinking chip's
// own two seconds, for its stated reason.
const approvalFlashFor = effortFlashFor

// approvalFlashMsg is the flash expiring: one repaint, so the chip comes down.
type approvalFlashMsg struct{}

// approvalDialer is the narrow slice of the session this surface needs to draw
// and move the conversation's posture (internal/session's approvalposture.go).
// It is asserted on the agent rather than added to [Agent], on [effortDialer]'s
// terms: a session that cannot move its gate simply has no control.
type approvalDialer interface {
	// ApprovalDial reports whether this session has a door onto its gate at
	// all — a worker or a headless run has the methods and no door.
	ApprovalDial() bool
	// ResolvedApprovalPosture is the posture the gate is standing at, whichever
	// scope decided it.
	ResolvedApprovalPosture() string
	// SetApprovalPosture moves it and says why not.
	SetApprovalPosture(posture string) error
}

// approvalDial is the session's dial, and false where there is none: an agent
// without the door, a session with the door and no gate behind it, or a far
// engine that said at the welcome it has none ([approvalDialer.ApprovalDial]
// answers for all three).
func (a *app) approvalDial() (approvalDialer, bool) {
	if a.agent == nil {
		return nil, false
	}
	dial, ok := a.agent.(approvalDialer)
	if !ok || !dial.ApprovalDial() {
		return nil, false
	}
	return dial, true
}

// approvalPostureWord is the posture in force in the ladder's own words: the
// dial's resolved answer where there is a dial, and the launch's handed-down or
// profile-read mode where there is not (a hosted session, or a surface booted
// on an agent with no door). "" is a surface with nothing to say.
func (a *app) approvalPostureWord() string {
	if dial, ok := a.approvalDial(); ok {
		return dial.ResolvedApprovalPosture()
	}
	switch a.approval {
	case "allow":
		return session.PostureAllow
	case "deny":
		return session.PostureDeny
	case "prompt":
		return session.PostureAsk
	}
	return ""
}

// approvalWord is the posture as a person reads it, and "" for a surface with
// nothing to say. It is what the chip, the phone sheet's `approvals` row and
// /status all print, so the three cannot disagree.
func (a *app) approvalWord() string { return approvalWordFor(a.approvalPostureWord()) }

// approvalWordFor is the person's word for one posture, "" for a word that is
// not one. It is the one spelling the conversation's cell and the draft's
// share (boxseam.go).
func approvalWordFor(posture string) string {
	switch posture {
	case session.PostureAsk:
		return approvalAsksWord
	case session.PostureGuardian:
		return approvalGuardianWord
	case session.PostureAllow:
		return approvalYoloWord
	case session.PostureDeny:
		return approvalRefusesWord
	}
	return ""
}

// approvalOpen reports whether the posture in force is one a person must not
// be able to forget: the gate open, or everything refused.
func (a *app) approvalOpen() bool {
	switch a.approvalPostureWord() {
	case session.PostureAllow, session.PostureDeny:
		return true
	}
	return false
}

// approvalChipText is the chip unpainted: the permissions panel's own mark for
// "a whole tool" and the word beside it. It is the panel's mark rather than a
// new one because the two are about the same thing — what runs without asking
// — and a second mark for one subject is a mark that has to be learned twice.
//
// "" WHERE THERE IS NO DIAL AND NO FAR MACHINE, on [effortChipText]'s terms: a
// local session that cannot move its gate draws no control for it. The hosted
// session is the exception the header states — the cell is a reading there —
// and the status row's badge covers the frames the seam does not.
func (a *app) approvalChipText() string {
	if _, ok := a.approvalDial(); !ok && !a.hosted() {
		return ""
	}
	return a.approvalChip(a.approvalPostureWord())
}

// approvalChip is the cell for one posture, unpainted, and "" for a word that
// is not one — the spelling the seam and the draft's rule share.
func (a *app) approvalChip(posture string) string {
	word := approvalWordFor(posture)
	if word == "" {
		return ""
	}
	mark := glyphPermTool
	if a.pal.ascii || a.pal.linear {
		mark = glyphPermToolASCII
	}
	return mark + " " + word
}

// paintApprovalChip is the chip's cell of colour, in the states that are not
// the resting one. The open gate is loud for what it means, on the badge's own
// terms; the flash after a change takes THE EMPHASIS LAW's two moves; and under
// the pointer the cell takes the cursor step, for hover.go's law.
func (a *app) paintApprovalChip(text string) string {
	if a.approvalFlashing() {
		return a.paintChipFlash(text)
	}
	if a.approvalOpen() {
		if a.hoveringApproval() {
			return a.pal.ink(text)
		}
		return a.pal.bad(text)
	}
	return a.pal.cursor(a.pal.dim(text), 0)
}

// seamCarriesChip reports whether the chip is on the seam this frame, which is
// the whole of what decides whether the status row says the posture instead.
func (a *app) seamCarriesChip() bool {
	return !a.welcome.open && !a.roomOpen() && a.approvalChipText() != ""
}

// approvalSegment is the status row's own word for the gate, and it is the
// OLD BADGE exactly — `YOLO`, drawn only when the gate is open — on the frames
// where the seam is not carrying the chip, and nothing at all where it is (the
// header says which frames those are). Absence is the safe state on this row
// because the row is not a control; the chip is, and it says every posture.
func (a *app) approvalSegment() string {
	if a.seamCarriesChip() {
		return ""
	}
	if a.approvalPostureWord() == session.PostureAllow {
		return approvalYoloWord
	}
	return ""
}

// approvalSeamLit reports whether the cell wears anything but the seam's own
// tier this frame: the bad hue over an open gate, the flash, or the pointer.
func (a *app) approvalSeamLit() bool {
	return a.approvalOpen() || a.hoveringApproval() || a.approvalFlashing()
}

func (a *app) hoveringApproval() bool { return a.hot.kind == hoverApproval }

func (a *app) approvalFlashing() bool {
	return !a.approvalLit.IsZero() && a.now().Sub(a.approvalLit) < approvalFlashFor
}

// ── the chord and the press ─────────────────────────────────────────────────

// cycleApproval is [approvalKey] and the press on the cell: the wheel walks one
// stop, from what the chip says. A posture off the wheel — `refuses`, reached
// by name — steps onto its first stop, so a press always lands somewhere a
// press can reach again.
func (a *app) cycleApproval() tea.Cmd {
	dial, ok := a.approvalDial()
	if !ok {
		a.noteApprovalUnavailable()
		return nil
	}
	return a.setApprovalPosture(dial, approvalNext(dial.ResolvedApprovalPosture()))
}

// approvalNext is one step of the wheel, read off [session.ApprovalWheel] so
// the stops are written down once.
func approvalNext(posture string) string {
	wheel := session.ApprovalWheel
	for at, stop := range wheel {
		if stop == posture {
			return wheel[(at+1)%len(wheel)]
		}
	}
	return wheel[0]
}

// setApprovalPosture is the ONE path from the chord, the press and the command
// to the session, so the three cannot mean slightly different things. The note
// it writes says the posture and what it buys, in the flat dotted grammar the
// other notes on this surface are written in.
//
// THE DOOR IS ASKED OFF THE LOOP (offloop.go's law): the engine rebuilds the
// gate and, over a connection, a far machine does — neither is a thing this
// window may wait on under a keystroke. What the person sees lands in the fold,
// on the next pass, and only if this window is still looking at the
// conversation the key was pressed in.
func (a *app) setApprovalPosture(dial approvalDialer, posture string) tea.Cmd {
	return a.offLoop(func() func(bool) tea.Cmd {
		err := dial.SetApprovalPosture(posture)
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if err != nil {
				a.note("approvals · " + err.Error())
				return nil
			}
			a.approvalLit = a.now()
			a.approval = a.approvalPosture()
			a.touch()
			word := a.approvalWord()
			a.noteFacts("approvals · "+word+" · "+approvalLines[a.approvalPostureWord()], word)
			return surfaceTick(approvalFlashFor, func(time.Time) tea.Msg { return approvalFlashMsg{} })
		}
	})
}

// approvalLines is what each posture buys, in a person's words, said in the
// note after a move and in the list `/approvals` prints.
var approvalLines = map[string]string{
	session.PostureAsk:      "every call the rules say to ask about is asked about",
	session.PostureGuardian: "a small model answers the plainly safe ones, you get the rest",
	session.PostureAllow:    "every tool runs without asking · dangerous commands still ask",
	session.PostureDeny:     "every call the rules do not name is refused",
}

// noteApprovalUnavailable is what a conversation with no dial answers, and it
// says WHY in the one case a person can act on: over a connection to an engine
// without the door, the far machine's rows decide.
func (a *app) noteApprovalUnavailable() {
	if a.hosted() {
		a.note(approvalHostedWord)
		return
	}
	a.note(approvalUnavailableWord)
}

const (
	approvalHostedWord      = "what runs without asking is decided on the machine the conversation runs on — its engine has no dial for this window · change it in that machine's /settings"
	approvalUnavailableWord = "what runs without asking is unavailable — this session has no dial onto it"
)

// ── the command ─────────────────────────────────────────────────────────────

// runApprovals is `/approvals` (and `/yolo`, which is what fingers type). Bare,
// it prints the stops with what each one buys and the one in force marked —
// the wheel alone can only be walked blind, and the sentences are how a person
// chooses between words that all mean "ask me less". With a word after it, it
// sets that posture outright through the one path the chord uses.
//
// AN UNKNOWN WORD CHANGES NOTHING AND SAYS THE FIVE, which is the shape every
// choice this surface refuses takes (effortchip.go's [app.runEffort]).
func (a *app) runApprovals(arg string) tea.Cmd {
	dial, ok := a.approvalDial()
	if !ok {
		a.noteApprovalUnavailable()
		return nil
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		a.noteApprovalLadder(dial)
		return nil
	}
	posture := approvalArgument(arg)
	if posture == "" {
		a.noteFacts("/approvals "+arg+" · not a posture · "+strings.Join(approvalCommandWords(), " · "),
			approvalCommandWords()...)
		return nil
	}
	return a.setApprovalPosture(dial, posture)
}

// approvalArgument reads the word a person typed into the door's own: the
// ladder's words, the flag's, and the settings row's, so `/approvals yolo`,
// `/approvals allow` and `/approvals prompt` all land where they plainly mean to.
func approvalArgument(word string) string {
	switch word {
	case session.PostureAsk, "prompt", approvalAsksWord:
		return session.PostureAsk
	case session.PostureGuardian:
		return session.PostureGuardian
	case session.PostureAllow, "yolo":
		return session.PostureAllow
	case session.PostureDeny, "refuse", approvalRefusesWord:
		return session.PostureDeny
	case session.PostureAuto, "off":
		return session.PostureAuto
	}
	return ""
}

// approvalCommandWords is every word the door takes as the refusal lists them:
// the wheel, then the two by name.
func approvalCommandWords() []string {
	return []string{session.PostureAsk, session.PostureGuardian, "yolo", session.PostureDeny, session.PostureAuto}
}

// noteApprovalLadder is the bare command's answer: one line per posture, the
// one in force marked, and the way back to the rows named last.
func (a *app) noteApprovalLadder(dial approvalDialer) {
	current := dial.ResolvedApprovalPosture()
	a.noteFacts("approvals · what this conversation runs without asking · "+approvalKey+" walks it", approvalKey)
	for _, posture := range []string{session.PostureAsk, session.PostureGuardian, session.PostureAllow, session.PostureDeny} {
		mark := "  "
		if posture == current {
			mark = a.icon(tokens.GPointer) + " "
		}
		word := approvalCommandWord(posture)
		a.noteFacts(mark+word+strings.Repeat(" ", 9-len(word))+approvalLines[posture], word)
	}
	a.noteFacts("  "+session.PostureAuto+"     the settings rows decide · /approvals auto", session.PostureAuto)
}

// approvalCommandWord is the word the list prints for a posture — the one the
// command takes, so a person can type what they read.
func approvalCommandWord(posture string) string {
	if posture == session.PostureAllow {
		return "yolo"
	}
	return posture
}

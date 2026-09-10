package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE APPROVAL QUESTION ───────────────────────────────────────────────────
//
// internal/session's consent.go decides that one tool call needs a person, and
// then BLOCKS that call until somebody answers. This file is what is left of
// the surface's half of that once the drawing moved.
//
//	  ╰─▶ bash rm -rf build
//	? needs your ok to run bash [1] allow once · [2] always, this command · [3] deny · [esc] later · 7s
//	  bash pattern "rm -rf *"
//	  2 more
//
// THE BLOCK DRAWS IT NOW (question.go), and everything about those four rows
// belongs to the block: the row it re-uses, the answers, the queue count, the
// narrow sheet, the clock, the keyboard and the receipt. docs/design/questions/
// DESIGN.md is why — one renderer for every decision this engine hands a
// person — and the 1388 lines that used to be here were the strongest argument
// for it, because every law they carried had to be argued again in eight other
// blocks that never got round to it.
//
// What is left is the three things that are the LANE'S and not the block's:
//
//   - THE ROW. The question is about a call the transcript has already drawn
//     (session sends the consent request AFTER the batch's EventToolBegin
//     rows), so this marks that row as stopped and draws one where a surface
//     that attached mid-batch has none. A question about a call nobody can see
//     is a question nobody can answer.
//   - THE WIDENING YES IS WRITTEN DOWN ([app.consentAnswered]) — the tool's
//     allow, or, for the one tool judged by its arguments, the SHAPE the person
//     picked out of the block's second beat. It is the only thing this surface
//     does that outlives the process, so it is the only thing it prints a
//     receipt for, and the receipt says where to undo it.
//   - THE READING CLOCK'S LENGTH, which is a setting ([app.consentWait]) and is
//     handed to the question when it is raised. What the clock DOES is the
//     block's ([app.tickQuestion]), and what it does is hold: silence is not a
//     no, and F41 — a hidden ~10s timer that recorded "denied" and killed work
//     nobody refused — is the reason that sentence is a law.
//
// After an answer the ROW STAYS, annotated dim with what was decided. The
// transcript is what happened, and "you were asked about this and said yes" is
// part of what happened — one of the few things this surface records that the
// session file never will (consent is events, never journal).

// askConsent takes one session.EventConsentRequest.
//
// It puts the call's row into the question state and raises the question on the
// block. THE SAME QUESTION ARRIVES TWICE ON PURPOSE: the engine also sends it
// whole on the questions lane ([session.Agent.WatchQuestions]), and the block
// replaces by token — so whichever gets here first draws, and the second is the
// same decision rather than a second one. What this one carries that the lane's
// cannot is everything below: the shapes, the write seam, the clock.
func (a *app) askConsent(ev session.Event) {
	at := a.questionSubjectAt(a.consentQuestion(ev))
	if at < 0 {
		// The row should already exist. When it does not — a surface that
		// attached mid-batch, a tool whose begin was dropped — the call is drawn
		// now rather than asked about invisibly.
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
			status: toolConsent, callID: ev.CallID, detail: toolDetail{Args: ev.Args},
		})
		at = len(a.entries) - 1
	}
	// The row stops claiming to be working. Its spinner was the second half of
	// the defect this wave fixes: a call parked on a question that nobody could
	// see was a question turned exactly like a call doing work.
	a.entries[at].status = toolConsent
	a.entries[at].stale = true
	// The typed lists follow the draft, and a list left open under a question
	// answering to digits is two readers for one keystroke.
	a.closeLists()
	// AND THE MODEL OVERLAY GOES, for the harder version of the same reason:
	// where there are hotkeys there is no fuzzy filter, anywhere on this
	// surface.
	if a.pick.open {
		a.pick.close()
	}
	a.raiseQuestion(a.consentShown(ev))
	a.follow()
	a.touch()
}

// consentQuestion is one approval request as the object every surface draws.
//
// IT IS THE ENGINE'S OWN BUILDER, SAID AGAIN (session's [Agent.consentAsk]),
// and the two must stay one sentence: the block keys a question by its lane and
// its id, so this one and the one that arrives on the questions lane a moment
// later are the SAME question — and two builders that drifted would make them
// two, one replacing the other on screen while somebody was reading it.
//
// The one thing said differently is the widening answer's WORD, because how far
// an always reaches is a fact about this surface's write seams and not about the
// engine ([app.alwaysWord]).
func (a *app) consentQuestion(ev session.Event) session.Question {
	options := make([]session.AnswerOption, 0, 3)
	for _, option := range session.AnswerOptions(session.QuestionConsent) {
		if option.Widening {
			if !ev.Memo {
				// THE ONE ANSWER A QUESTION MAY NOT OFFER AT ALL. The same lane
				// carries the stuck-turn question, whose tool-session scope the
				// engine drops, and an offer that does nothing is worse than a
				// missing one — a person who presses it believes they have
				// stopped being asked.
				continue
			}
			option.Label = a.alwaysWord(ev.Tool)
		}
		options = append(options, option)
	}
	scope := []session.AnswerScope{session.ScopeOnce}
	if ev.Memo {
		scope = append(scope, session.ScopeAlways)
	}
	reason := strings.TrimSpace(ev.Rule)
	if reason == "" {
		reason = session.ConsentFallbackReason
	}
	return session.Question{
		ID:       ev.ID,
		Kind:     session.QuestionConsent,
		Ask:      session.AskPermission,
		Form:     session.FormLine,
		Asker:    session.Asker{Kind: session.AskerEngine},
		Head:     consentHead(ev.Tool),
		Reason:   reason,
		Subject:  session.SubjectRef{Kind: session.SubjectCall, CallID: ev.CallID, Name: ev.Tool},
		Options:  options,
		Stakes:   session.StakesCostly,
		Blocking: session.Blocking{Turn: true},
		Scope:    scope,
	}
}

// consentHead is the question's own sentence, and it is the line another window
// already answers from (session's [Agent.ask] writes the same one).
func consentHead(tool string) string {
	return "needs your ok to run " + strings.TrimSpace(tool)
}

// consentShown is that question dressed with the three things the object cannot
// carry: what its widening answer could be banked as, what this program does
// about an answer, and how long the reading clock runs.
func (a *app) consentShown(ev session.Event) questionShown {
	tool := ev.Tool
	shown := questionShown{
		question: a.consentQuestion(ev),
		shapes:   func() []string { return a.askShapes(tool) },
		answered: func(answer session.Answer) session.Answer {
			return a.consentAnswered(ev.ID, tool, answer)
		},
	}
	shown.clockAt, shown.clockFor = a.now(), a.askWait
	// A QUESTION COMING BACK FROM ANOTHER CONVERSATION KEEPS THE READING TIME IT
	// HAD. A switch stops drawing the question and the clock stops with it —
	// there is nobody reading a conversation that is not on screen, which is the
	// same argument [app.tickQuestion] makes about an unfocused window — so what
	// the sidecar carried is the REMAINDER, and this rebases it (switcher.go).
	if a.askResume > 0 {
		shown.clockAt = a.now().Add(a.askResume - a.askWait)
		shown.clockHeld = a.askResumePaused
		a.askResume, a.askResumePaused = 0, false
	}
	return shown
}

// consentWait is the configured countdown as a duration. Zero — the setting's
// own off — is a question that waits forever and says `waiting` while it does.
//
// It is read at boot and re-read at every turn end ([app.settle]), on the terms
// the gate's posture and the mouse row are read on: a row that only ever changes
// by hand does not need to be resolved off disk once per question.
func (a *app) consentWait() time.Duration {
	seconds := config.ConsentTimeoutAt(a.profileDir)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// consentExpiredWord is what a PREVIOUS build wrote on the row when the clock
// answered no for the person (F41). This build never writes it: expiry holds.
const consentExpiredWord = "denied · no answer"

// ── the widening yes ────────────────────────────────────────────────────────

// consentAnswered is the lane's own hand on an answer: it writes down whatever
// a widening yes bought, and annotates the row with what was decided.
//
// NEVER A DENY. A no is a one-time no, and a standing never is a line somebody
// types into the settings sheet on purpose. A surface that turned a keystroke
// under a countdown into a permanent refusal would be writing policy out of
// impatience.
//
// A FAILED WRITE IS DROPPED, exactly as the rail's is (cmd/aforge's
// chatv2_rail.go states the reasoning): the answer has already been given, the
// session already stops asking, and an unwritable profile directory must not put
// a config error on a line in the middle of somebody's work. What it costs is
// the receipt — the row says "allowed" instead of "saved", which is the truth.
func (a *app) consentAnswered(id uint64, tool string, answer session.Answer) session.Answer {
	widening := false
	if option, ok := a.consentOption(id, tool, answer.FirstKey()); ok {
		widening = option.Widening
	}
	word := decisionWord(!consentDenies(id, tool, answer.FirstKey()))
	if widening {
		banked, saved := a.rememberAlways(tool, strings.TrimSpace(answer.Comments[session.AnswerBanked]))
		if saved {
			word = consentSavedWord
		}
		// THE COMMENT IS THE TRUTH ABOUT WHAT WAS WRITTEN and is rewritten
		// rather than trusted: a shape that could not be banked must not tell
		// the engine a rule exists, because the answer it would then apply is a
		// rule nobody can find and nothing enforces
		// ([session.AnswerBanked] says what it buys).
		answer.Comments = consentComment(answer.Comments, banked)
	}
	a.markConsentRow(id, tool, word)
	return answer
}

// consentComment puts the banked rule on the answer, or takes the key off
// entirely where nothing was written. It never leaves an empty one: a comment
// with nothing in it is a claim with nothing behind it.
func consentComment(comments map[string]string, banked string) map[string]string {
	if banked == "" {
		delete(comments, session.AnswerBanked)
		if len(comments) == 0 {
			return nil
		}
		return comments
	}
	if comments == nil {
		comments = map[string]string{}
	}
	comments[session.AnswerBanked] = banked
	return comments
}

// consentOption is one answer's option on the question this id names, read back
// off the block rather than rebuilt: the widening answer is the one the block
// actually drew, and rebuilding it here would be a second place deciding which
// answer widens anything.
func (a *app) consentOption(id uint64, tool, key string) (session.AnswerOption, bool) {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionConsent || open.question.ID != id {
			continue
		}
		return open.question.Option(key)
	}
	return session.AnswerOption{}, false
}

// consentDenies reports whether this key is the refusal. It reads the engine's
// own mapping ([session.AnswerFromKey]) rather than a second one, so the word
// the row keeps and the answer the session is given cannot come apart.
func consentDenies(id uint64, tool, key string) bool {
	action, ok := session.AnswerFromKey(session.QuestionConsent, key)
	return ok && !action.Allow
}

// consentSavedWord is what the row keeps when the always was persisted: what
// happened, and where to undo it. It is the block's memo voice — one line, dim,
// in the [entry.decision] slot every other answer lands in — because a person
// who has just changed a setting by pressing a key has to be told BOTH that it
// changed and that the change has an address.
const consentSavedWord = "always · saved — /permissions to change"

// consentBash is the one tool whose answer is about its ARGUMENT and not its
// name. Everywhere else a question is about a tool; here it is about the command
// line, which is why the offer says "this command" and why what gets written is
// a rule and not a name.
const consentBash = "bash"

// rememberAlways writes a widening answer to the person's settings and answers
// what it wrote — the shell rule, where there is one — and whether it landed.
//
// THE RUNNING SESSION NEEDS NOTHING FROM THIS for a plain tool: the engine writes
// its own memo for the tool the moment the answer lands (internal/session's
// askAnswer), which is what actually stops the asking for the rest of this
// conversation. So that seam is only ever about the NEXT session. The SHELL rule
// is different and is why this answers what it wrote: a memo keyed by the tool's
// name means every command for the rest of the conversation, and a person who
// read `git status*` must not buy silence for `rm -rf`.
func (a *app) rememberAlways(tool, shape string) (string, bool) {
	if tool == consentBash {
		if a.saveBashApproval == nil {
			return "", false
		}
		// The shape the person picked in the second beat, and the line itself
		// where there was no beat to pick in — a command that never arrived
		// whole, a compound line nothing can be derived from.
		command := strings.TrimSpace(shape)
		if command == "" {
			command = a.askCommand(tool)
		}
		if command == "" {
			return "", false
		}
		if a.saveBashApproval(command) != nil {
			return "", false
		}
		return command, true
	}
	if a.saveApproval == nil {
		return "", false
	}
	// A PLAIN TOOL BANKS NO RULE, only a setting keyed by the same name the
	// engine's own memo is keyed by — so the two agree and the engine writes its
	// memo as it always did ([session.AnswerBanked] is what would stop it).
	return "", a.saveApproval(tool) == nil
}

// askCommand is the exact command line a bash question is about.
//
// IT READS THE ARGUMENTS AND NEVER THE LINE ON SCREEN. The row's text is a gloss
// — clipped for a column, sometimes the tool's own name and nothing else — and a
// rule written from a gloss would be a standing approval for a command that was
// never run. The arguments are what arrived; when they did not arrive whole
// (session caps them) they do not parse, this answers empty, and nothing is
// written at all.
func (a *app) askCommand(tool string) string {
	at := a.consentSubjectAt(tool)
	if at < 0 {
		return ""
	}
	e := &a.entries[at]
	if e.kind != entryTool {
		return ""
	}
	return strings.TrimSpace(argString(argsOf(e.detail.Args), "command"))
}

// consentSubjectAt is the transcript row the head consent question is about, or
// -1. It goes through the block's own pairing ([app.questionSubjectAt]) so the
// row a person READ and the row a rule is written from are the same row — a
// question that landed on the wrong one lets somebody read command A, press
// always, and bank a standing rule for command B.
func (a *app) consentSubjectAt(tool string) int {
	head, ok := a.questionHead()
	if !ok || head.question.Kind != session.QuestionConsent {
		return -1
	}
	at := a.questionSubjectAt(head.question)
	if at < 0 || at >= len(a.entries) {
		return -1
	}
	return at
}

// askShapes is what a bash always could be banked as, or nothing at all.
//
// It reads the arguments through [app.askCommand] and derives from those, so a
// call whose payload never arrived whole has no shapes and gets no beat — the
// same silence that seam has always kept about a rule it cannot write honestly.
func (a *app) askShapes(tool string) []string {
	if tool != consentBash || a.saveBashApproval == nil {
		return nil
	}
	return config.BashShapes(a.askCommand(tool))
}

// remembering reports whether a widening yes on this tool would actually write
// something down. It is what decides the answer's WORDS — an offer that said
// "always, this command" on a surface that cannot remember one would be
// promising a file it is not going to write.
func (a *app) remembering(tool string) bool {
	if tool == consentBash {
		return a.saveBashApproval != nil
	}
	return a.saveApproval != nil
}

// alwaysWord is the widening answer's name: what it reaches, in the fewest words
// that are true.
//
// Three spellings and each says exactly what will happen. With nothing wired the
// answer lasts for this agent's life and the parenthetical says so. With a write
// seam behind it the answer is PERSISTED, the parenthetical would be a lie, and
// the object it is persisted against is named instead: the tool, or — for the
// one tool judged by its arguments — this command.
func (a *app) alwaysWord(tool string) string {
	if !a.remembering(tool) {
		return "always, this tool (session)"
	}
	if tool == consentBash {
		return "always, this command"
	}
	return "always, this tool"
}

// ── the row ─────────────────────────────────────────────────────────────────

// markConsentRow puts what was decided on the call's own row and takes the
// question state off it.
func (a *app) markConsentRow(id uint64, tool, word string) {
	at := a.consentRowAt(id, tool)
	if at < 0 {
		return
	}
	e := &a.entries[at]
	e.decision = word
	// The question is over, and the row goes back to being a call: allowed, it
	// runs and spins; denied, session hands the model a refusal and the close
	// event that follows lands on the same row either way. Leaving it in the
	// question state would leave a marked row on screen for a question nobody is
	// being asked.
	if e.status == toolConsent {
		e.status = toolRunning
		e.began = time.Now()
	}
	e.stale = true
}

// consentRowAt is the row one open consent question is about, by id.
func (a *app) consentRowAt(id uint64, tool string) int {
	for _, open := range a.questions {
		if open.question.Kind != session.QuestionConsent || open.question.ID != id {
			continue
		}
		at := a.questionSubjectAt(open.question)
		if at >= 0 && at < len(a.entries) {
			return at
		}
	}
	return -1
}

func decisionWord(allow bool) string {
	if allow {
		return "allowed"
	}
	return "denied"
}

// ── what other files ask this lane ──────────────────────────────────────────

// asking reports whether an approval question is on the block. It is what the
// rest of this surface reads to mean "the session is stopped on somebody".
func (a *app) asking() bool { return a.consentOpen() != nil }

// consentOpen is the head open approval question, or nil.
func (a *app) consentOpen() *questionShown {
	for i := range a.questions {
		if a.questions[i].question.Kind == session.QuestionConsent {
			return &a.questions[i]
		}
	}
	return nil
}

// consentAsking reports whether THIS window holds the approval question with
// that id — which is what home asks before answering one from its own row
// (homeband_answer.go).
func (a *app) consentAsking(id uint64) bool {
	for _, open := range a.questions {
		if open.question.Kind == session.QuestionConsent && open.question.ID == id {
			return true
		}
	}
	return false
}

// shaping reports whether the widening answer's second beat is on screen. It is
// the block's ([app.questionBeating]) and is named here for the readers that
// have always asked this file (render.go's hint, input.go's chords).
func (a *app) shaping() bool { return a.questionBeating() }

// answerWith answers this window's own approval question from somewhere that is
// not the block — home's row, today ([app.answerHere]).
//
// IT GOES THROUGH THE ONE DOOR ([app.answerQuestion]) and not past it, so an
// answer given on home leaves the same receipt, the same record and the same
// annotated row as the same answer given here.
func (a *app) answerWith(allow bool, scope session.ConsentScope) {
	open := a.consentOpen()
	if open == nil {
		return
	}
	key := consentKeyFor(allow, scope)
	if key == "" {
		return
	}
	head := *open
	a.answerQuestion(head, session.Answer{
		Key: key, Picked: []string{key}, Scope: questionScopeOf(head.question, key),
	})
}

// consentKeyFor is which answer a bare allow-and-scope names, read back through
// the engine's own mapping so there is no second table of what a key means.
func consentKeyFor(allow bool, scope session.ConsentScope) string {
	for _, option := range session.AnswerOptions(session.QuestionConsent) {
		action, ok := session.AnswerFromKey(session.QuestionConsent, option.Key)
		if ok && action.Allow == allow && action.Scope == scope {
			return option.Key
		}
	}
	return ""
}

// dropAsks forgets every unanswered approval question. It runs when the turn
// that raised them ends: the session released those calls when its context died,
// so the answers are late and the questions are about work that is over.
func (a *app) dropAsks() {
	kept, dropped := a.questions[:0], false
	for _, open := range a.questions {
		if open.question.Kind == session.QuestionConsent {
			delete(a.questionFolded, open.token())
			dropped = true
			continue
		}
		kept = append(kept, open)
	}
	a.questions = kept
	if dropped {
		a.touch()
	}
}

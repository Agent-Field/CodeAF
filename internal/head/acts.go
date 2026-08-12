package head

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The acts the cue ladder used to reach terminally, as tools.
//
// Every one of these was a recognizer's payload: a prefix test decided the
// sentence was a correction, or a rule edit, or a service command, and then
// journaled and spoke without anything with judgment seeing the message. The
// machinery underneath is untouched — the same correction block with the same
// dispute line, the same charter transition table, the same service kinds, the
// same gates. What moved is who decides, and the difference shows up on exactly
// the sentences the prefix tests were never going to cover.

// charterAcknowledgement is acknowledgeCharterCommand's sentence without the
// posting. The question-answer path still posts it directly, because an answer
// to a durable question is settled where it is answered; a tool hands it back
// for the loop to say in its own words.
func charterAcknowledgement(kind store.CommandKind) string {
	switch kind {
	case store.CommandCharterRatify:
		return "Standing it up."
	case store.CommandCharterPause:
		return "Pausing that rule."
	case store.CommandCharterRetire:
		return "Retiring that rule."
	case store.CommandCharterOnce:
		return "Keeping it one-time."
	case store.CommandCharterCadence:
		return "Changing when that runs."
	case store.CommandCharterWording:
		return "Changing what it says."
	case store.CommandCharterProbation:
		return "Asking before firing again."
	}
	return "Updating that standing rule."
}

// retirementReason is why something was taken off the shelf: their own sentence
// when there is one, and the plain fact of it when they clicked instead.
func retirementReason(said string) string {
	if said = strings.TrimSpace(said); said != "" {
		return said
	}
	return "you asked me to stop using this"
}

func craftAcknowledgement(kind store.CommandKind, name string) string {
	switch kind {
	case store.CommandCraftRun:
		return "Doing " + name + " the way you have before."
	case store.CommandCraftRevert:
		return "Putting " + name + " back to the version before this one."
	}
	return "Not working the " + name + " way any more."
}

func serviceReceipt(kind store.CommandKind, service store.Service, action string) string {
	switch kind {
	case store.CommandServiceRestart:
		return "Restarting " + service.Name + "."
	case store.CommandServiceAutoRestart:
		if action == "disable-auto-restart" {
			return "Disabling auto-restart for " + service.Name + "."
		}
		return "Enabling auto-restart for " + service.Name + "."
	}
	return "Stopping " + service.Name + "."
}

// answerQuestion is Part 6 decision 1 held open on purpose.
//
// 4.1 lists the tool; 5.7 says a task orchestrator may settle its own workers'
// INFORMATIONAL questions while consent-class ones always escalate; 9.4 says the
// class axis must exist first with a conservative default — unlabeled means
// consent means escalate. All three landed. What 12.1.4 then found is the fact
// that decides this lane: every existing AskQuestion producer in the product is
// genuinely consent-bearing and NONE is labeled informational. So the autonomy
// half of this tool has, today, no question it could legitimately answer, and
// granting it anyway would mean the head settling consent in the user's name.
//
// The tool exists anyway, and it is not theatre. Before it, the head could not
// SEE an open question at all — OpenQuestions was a TUI backend capability that
// never appeared in any head prompt — so a worker blocked on a question was
// invisible to the one party talking to the person who could answer it. Reading
// is the whole of what it does today, and reading was the missing half.
func (run *beltRun) answerQuestion(args map[string]any) (string, bool) {
	if run.head == nil || run.head.store == nil {
		return "open questions are not available on this surface.", false
	}
	seq := beltInt(args, "question")
	if seq <= 0 {
		return run.head.renderOpenQuestions(run.user.SessionID), false
	}
	question, found, err := run.head.store.AgentQuestionBySeq(seq)
	if err != nil {
		return "that question could not be read: " + err.Error(), true
	}
	// Open means unresolved, not unsurfaced. A question the person has already
	// been shown is still waiting on an answer — that is the whole state this
	// tool exists to see — and refusing it as "not open" would make the one
	// question the head can legitimately settle the one it cannot reach.
	if !found || (question.Status != store.QuestionPending && question.Status != store.QuestionAsked) {
		return fmt.Sprintf("there is no open question numbered %d — read the open questions again", seq), true
	}
	if question.Class != store.QuestionInformational {
		return fmt.Sprintf("question %d is a CONSENT question and is not yours to answer. Put it to the user in your own words and leave it open: %q",
			seq, truncateBytes(firstLine(question.Text), openQuestionTextBytes)), true
	}
	answer := strings.TrimSpace(beltString(args, "answer"))
	if answer == "" {
		return "answer must say what the worker should use", true
	}
	if err := run.head.store.ResolveQuestion(seq, store.QuestionAnswered, answer, run.user.Seq); err != nil {
		return "that could not be answered: " + err.Error(), true
	}
	run.record(0, "Answered a question the work was waiting on.")
	return "answered; the work it was blocking carries on", false
}

const (
	// openQuestionCap bounds one read of what is waiting. More than a handful is
	// a queue rather than a thing to answer in a sentence.
	openQuestionCap = 6
	// openQuestionTextBytes keeps one question to a clause.
	openQuestionTextBytes = 200
)

func (h *Head) renderOpenQuestions(sessionID string) string {
	questions, err := h.store.OpenQuestions(sessionID, openQuestionCap)
	if err != nil {
		return "the open questions could not be read: " + err.Error()
	}
	if len(questions) == 0 {
		return "nothing is waiting on an answer."
	}
	lines := make([]string, 0, len(questions))
	for _, question := range questions {
		class := string(question.Class)
		if class == string(store.QuestionConsent) {
			class = "consent — the user's to answer, never yours"
		}
		line := fmt.Sprintf("- #%d [%s] %s", question.Seq, class,
			truncateBytes(firstLine(question.Text), openQuestionTextBytes))
		if origin := strings.TrimSpace(question.OriginNodeID); origin != "" {
			if node, found, nodeErr := h.store.Node(origin); nodeErr == nil && found {
				line += " | asked by " + surgeryTargetLabel(node)
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// sayBytes bounds one interim line. This is a sentence said in passing, not a
// place to put an answer: a paragraph posted here would be the turn delivering
// its reply early and then delivering it again at the end.
const sayBytes = 600

// say is the turn talking while it is still working.
//
// The tool that used to stand here was `await`: three seconds of blocking on a
// receipt, because the turn had exactly one chance to speak and could not
// observe anything that happened after it. Both halves of that are gone — the
// receipt comes back as a wake (wake.go), and the turn can now speak more than
// once. What is left is the simple half: put one line in front of them now.
//
// It goes through the ordinary posting door, unannotated and unmarked, because
// it is an ordinary thing for the head to say. Every surface already draws it:
// there is no new class of message here, only a message that arrives before the
// turn is over.
func (run *beltRun) say(args map[string]any) (string, bool) {
	line := strings.TrimSpace(beltString(args, "text"))
	if line == "" {
		return "text must be the one line to say now, in the person's own terms", true
	}
	if err := run.head.postAgent(run.user.SessionID, truncateBytes(line, sayBytes), 0); err != nil {
		return "that line could not be posted: " + err.Error(), true
	}
	// Deliberately not `acted`: saying something changes nothing in the world, and
	// a turn that only spoke owes no receipt. `said` is the narrower fact — this
	// turn has already put words on their screen — and the only thing it buys is
	// the right to end without a second message when there is nothing to add.
	run.said = true
	return "said — they are reading that now. The turn continues; do not say it again, " +
		"and your closing words are still their own message", false
}

// controlCandidates answers a verb that knows what it wants to do and not what
// to do it to. It is a READ: the union of the live jobs and the standing rules
// the words reach, handed back for the loop to name or to ask about. The old
// path chose for the user when exactly one thing matched, which is how a request
// to withdraw fourteen queued tasks became "Cancelling line-scan."
func (run *beltRun) controlCandidates(kind store.CommandKind, describes string) (string, bool) {
	describes = strings.TrimSpace(describes)
	if describes == "" {
		return "ids must name at least one id from a board read, or describes must carry the user's own words for the work", true
	}
	candidates, err := run.head.describedTargets(kind, describes)
	if err != nil {
		return "that could not be looked up: " + err.Error(), true
	}
	if len(candidates) == 0 {
		return "nothing on the board and no standing rule matches those words", false
	}
	if len(candidates) > describedTargetCap {
		candidates = candidates[:describedTargetCap]
	}
	lines := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.isRule() {
			lines = append(lines, "- standing rule "+candidate.rule.ID+" | "+firstLine(candidate.rule.Invariant)+
				" | a standing rule, not work")
			continue
		}
		lines = append(lines, "- "+candidate.job.Node.ID+" | "+surgeryTargetLabel(candidate.job.Node)+
			" | "+surgeryTargetHint(candidate.job))
	}
	verdict := "these match those words — name the id you mean"
	if len(lines) > 1 {
		verdict = "more than one thing matches those words — ask the user which, as numbered options, or name the id if one is plainly right"
	}
	return verdict + ":\n" + strings.Join(lines, "\n"), false
}

// askOptionPrefix marks an option this loop minted rather than one a durable
// action path encoded. It is what tells the question machinery to hand the
// answer back to the loop instead of trying to apply it: the loop asked, so the
// loop is the only party that knows what the answer settles.
const askOptionPrefix = "ask:"

// askOptionCap bounds one question. More than a handful of rows is a list to
// search rather than a choice to make — the same cap every askback in this
// package has always used.
const askOptionCap = RedirectCandidateLimit

// isAskQuestion reports that a question was minted by the ask tool.
func isAskQuestion(options []store.QuestionOption) bool {
	for _, option := range options {
		if strings.HasPrefix(strings.TrimSpace(option.Value), askOptionPrefix) {
			return true
		}
	}
	return false
}

// ask posts one numbered question and ends the turn.
//
// It is deliberately the narrowest possible question: a prompt and some labels.
// It cannot carry an action, cannot pre-approve anything, and cannot stand in
// for a consent gate — the gates mint their own questions with their own
// options, because only they know what the consent covers. This one exists so
// that "which of these did you mean" keeps being a row a person can click
// instead of a sentence they have to retype.
func (run *beltRun) ask(args map[string]any) (string, bool) {
	prompt := strings.TrimSpace(beltString(args, "question"))
	if prompt == "" {
		return "question must be one short question in the user's terms", true
	}
	labels := beltStrings(args, "options")
	if len(labels) < 2 {
		return "options must offer at least two choices; with one candidate there is nothing to ask", true
	}
	if len(labels) > askOptionCap {
		labels = labels[:askOptionCap]
	}
	options := make([]store.QuestionOption, 0, len(labels))
	for index, label := range labels {
		options = append(options, store.QuestionOption{
			Label: label, Value: fmt.Sprintf("%s%d", askOptionPrefix, index+1),
		})
	}
	if err := run.head.postQuestion(run.user.SessionID, prompt, 0, options); err != nil {
		return "that question could not be asked: " + err.Error(), true
	}
	// The question IS the reply. A second voice over the top of it would be the
	// thread answering its own question.
	run.acted, run.spoke = true, true
	return "asked; their next message is the answer and you will have both in front of you", false
}

// beltBool reads a boolean argument through the shapes providers actually emit
// for one: a JSON bool, and the same value spelled as a string.
func beltBool(args map[string]any, key string) bool {
	switch value := args[key].(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "yes", "1":
			return true
		}
	}
	return false
}

// forget is note's opposite, and the one memory operation that destroys rather
// than accumulates — which is why everything checkable is checked here and the
// only judgment left to the model is which line the person meant.
//
// It is separate from note's `replaces` on purpose, and the difference is the
// person's own intent. Replacing is being given the new version of a belief and
// retiring the old one as evidence for it; forgetting is being told the belief
// should not exist. Collapsing the two would mean a retraction silently
// creating a successor belief nobody stated.
func (run *beltRun) forget(args map[string]any) (string, bool) {
	seq := beltInt(args, "belief")
	if seq <= 0 {
		return "belief must be the #number shown beside one notebook line", true
	}
	fact, found, err := run.head.store.FactBySeq(seq)
	if err != nil {
		return "that belief could not be read: " + err.Error(), true
	}
	if !found || fact.Status != store.FactActive {
		return fmt.Sprintf("there is no active notebook belief #%d — name a number you were actually shown", seq), true
	}
	// A forged tool is a belief with an executable hanging off it, so "forget
	// that" aimed at one has to take the executable off the shelf too — a belief
	// that went quiet while its command stayed on the person's PATH is exactly
	// the half-done retirement this routes around. The command kind does both,
	// in one place, and the notebook's own retire verb journals the same one.
	if fact.Kind == store.FactSkill {
		command, cmdErr := run.head.store.RequestCommand(store.Command{
			SessionID: run.user.SessionID, Kind: store.CommandSkillRetire,
			Target:      strconv.FormatInt(seq, 10),
			Instruction: retirementReason(run.user.Body),
		})
		if cmdErr != nil {
			return "that could not be let go: " + cmdErr.Error(), true
		}
		run.record(command.Seq, "Taking that off the shelf — "+firstLine(fact.Body))
		return fmt.Sprintf("tool #%d is being retired: %q. It comes off the shelf and stops being offered",
			seq, truncateBytes(firstLine(fact.Body), beltNoteBytes)), false
	}
	if err := run.head.store.QuarantineFact(seq, run.user.Seq, store.FactOriginUser); err != nil {
		return "that belief could not be let go: " + err.Error(), true
	}
	// Retraction is reversible, so it is confirmed plainly rather than turned
	// into new work. The receipt quotes the line so the person can see which
	// belief actually went.
	run.record(0, "Let go — "+firstLine(fact.Body))
	return fmt.Sprintf("notebook belief #%d is retired: %q. Say so plainly and make nothing else of it",
		seq, truncateBytes(firstLine(fact.Body), beltNoteBytes)), false
}

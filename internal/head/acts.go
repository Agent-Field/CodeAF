package head

import (
	"fmt"
	"strings"
	"time"

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

// correct is the settled-work half of revision, and it is one tool rather than
// two because a blunt rejection and a polite adjustment are the same event said
// in two registers. "That's wrong" and "make it warmer, less legal" both mean:
// the thing you handed me is not right, here is what is wrong with it, do it
// again. Three revisions of one email used to be four separate jobs, each
// re-planned, re-priced, without the previous version in hand.
func (run *beltRun) correct(args map[string]any) (string, bool) {
	id := strings.TrimSpace(beltString(args, "job"))
	if id == "" {
		return "job must name the finished work whose deliverable was wrong, from a board or search read", true
	}
	node, found, err := run.head.store.Node(id)
	if err != nil {
		return "that job could not be read: " + err.Error(), true
	}
	if !found || !correctable(node) {
		return fmt.Sprintf("there is no finished work of the user's with id %q — correction is for work that already delivered; read the board again", id), true
	}
	previous := run.head.jobResult(node)
	if strings.TrimSpace(previous) == "" {
		return surgeryTargetLabel(node) + " recorded nothing, so there is no deliverable to be wrong. If they want something new, spawn it", true
	}
	words := strings.TrimSpace(beltString(args, "words"))
	if words == "" {
		words = strings.TrimSpace(run.user.Body)
	}
	if words == "" {
		return "words must say what is wrong with it, in the user's own terms", true
	}
	command, err := run.head.store.RequestCommand(store.Command{
		SessionID:   run.user.SessionID,
		Kind:        store.CommandSplice,
		Target:      node.ID,
		Instruction: SpliceCorrection(words, node, previous, resultFiles(node)),
		Attachments: append([]string(nil), run.user.Attachments...),
	})
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(command.Seq, "Taking that back to "+surgeryTargetLabel(node)+
		" — redoing it with the previous version and the correction in hand.")
	return "queued as a revision of " + surgeryTargetLabel(node) +
		": it goes again with its previous version and those words, and keeps everything they did not object to", false
}

// ruleVerbs is the charter transition table, read from the words a tool call
// carries rather than from the words a sentence opened with. Retiring and
// holding are also what a "stop" or "pause" aimed at a rule means, which is why
// charterTransition maps the graph verbs onto two of these.
func ruleVerbKind(verb string) (store.CommandKind, bool) {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case "retire", "stop":
		return store.CommandCharterRetire, true
	case "pause", "hold":
		return store.CommandCharterPause, true
	case "cadence", "retime":
		return store.CommandCharterCadence, true
	case "wording", "reword":
		return store.CommandCharterWording, true
	case "probation", "ask":
		return store.CommandCharterProbation, true
	}
	return "", false
}

func (run *beltRun) rule(args map[string]any) (string, bool) {
	kind, known := ruleVerbKind(beltString(args, "verb"))
	if !known {
		return "verb must be one of retire, pause, cadence, wording, probation", true
	}
	id := strings.TrimSpace(beltString(args, "id"))
	if id == "" {
		describes := strings.TrimSpace(beltString(args, "describes"))
		candidates, err := run.head.charterCandidates(charterReference(strings.ToLower(describes), ""))
		if err != nil {
			return "the standing rules could not be read: " + err.Error(), true
		}
		switch len(candidates) {
		case 0:
			return "no standing rule matches that. Read standing to see what is on watch", true
		case 1:
			id = candidates[0].ID
		default:
			lines := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				lines = append(lines, "- "+candidate.ID+" | "+firstLine(candidate.Invariant))
			}
			return "more than one standing rule matches those words — name one id, or ask which they mean:\n" +
				strings.Join(lines, "\n"), false
		}
	}
	words := strings.TrimSpace(beltString(args, "words"))
	switch kind {
	case store.CommandCharterCadence:
		if cadence := extractCadence(words); cadence != "" {
			words = cadence
		}
		if words == "" {
			return "words must say the new rhythm, in the user's own terms", true
		}
	case store.CommandCharterWording:
		if words == "" {
			return "words must say what the rule should say now", true
		}
	default:
		words = string(kind)
	}
	command, err := run.head.store.RequestCommand(store.Command{
		SessionID: run.user.SessionID, Kind: kind, Target: id, Instruction: words,
	})
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(command.Seq, charterAcknowledgement(kind))
	return "queued: " + charterAcknowledgement(kind), false
}

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

func (run *beltRun) service(args map[string]any) (string, bool) {
	verb := strings.ToLower(strings.TrimSpace(beltString(args, "verb")))
	if verb == "stop_everything" {
		// The one total phrasing, and it keeps its own gate: services are stopped
		// unconditionally because they are the person's own persistent effects,
		// and the live jobs are asked about once with what they cost quoted. The
		// gate owns the words — the loop must not speak over a consent question.
		if err := run.head.shutDownEverything(run.user); err != nil {
			return "that could not be done: " + err.Error(), true
		}
		run.acted, run.spoke = true, true
		return "every running service is being stopped, and the user has been asked about the live jobs; nothing else to say", false
	}
	action, known := serviceVerbAction(verb)
	if !known {
		return "verb must be one of stop, restart, auto_restart_on, auto_restart_off, stop_everything", true
	}
	describes := strings.TrimSpace(beltString(args, "describes"))
	var matches []store.Service
	var err error
	if action == "restart" {
		matches, err = run.head.store.SearchRestartableServices(describes)
	} else {
		matches, err = run.head.store.SearchServices(describes)
	}
	if err != nil {
		return "the services could not be read: " + err.Error(), true
	}
	switch len(matches) {
	case 0:
		return "no running service matches that", true
	case 1:
	default:
		lines := make([]string, 0, len(matches))
		for _, service := range matches {
			lines = append(lines, "- "+service.ID+" | "+service.Name+" | "+service.Health.Suffix())
		}
		return "more than one service matches those words — narrow it, or ask which they mean:\n" +
			strings.Join(lines, "\n"), false
	}
	service := matches[0]
	kind, ok := serviceCommandKind(action)
	if !ok {
		return "that is not something that can be done to a service", true
	}
	command, err := run.head.store.RequestCommand(store.Command{
		SessionID: run.user.SessionID, Kind: kind, Target: service.ID, Instruction: action,
	})
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(command.Seq, serviceReceipt(kind, service, action))
	return strings.ToLower(serviceReceipt(kind, service, action)), false
}

// serviceVerbAction maps the tool's vocabulary onto services.go's own action
// words, so one table decides what a service command means.
func serviceVerbAction(verb string) (string, bool) {
	switch verb {
	case "stop":
		return "stop", true
	case "restart":
		return "restart", true
	case "auto_restart_on":
		return "auto-restart", true
	case "auto_restart_off":
		return "disable-auto-restart", true
	}
	return "", false
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

const (
	// beltAwaitWindow is how long one await may hold the turn. It is short on
	// purpose: this watches for a RECEIPT, which the reconciler writes within a
	// tick or two, not for work to finish, which takes minutes. A turn that
	// blocked for minutes would be a conversation that stopped answering.
	beltAwaitWindow = 3 * time.Second
	// beltAwaitPoll is how often the receipt is looked for. It matches the head's
	// own poll so an await never spins faster than the loop it lives in.
	beltAwaitPoll = pollInterval
)

// await closes the loop that async commands never had. control, revise, spawn
// and the rest return "queueing" and the outcome lands later as a reconciler
// receipt, so a turn could not observe the result of a command it had just
// issued and re-plan on it — Part 2.6. This is that observation, bounded.
func (run *beltRun) await(args map[string]any) (string, bool) {
	seq := beltInt(args, "command")
	if seq <= 0 {
		if len(run.issued) == 0 {
			return "nothing has been queued in this turn to wait for", true
		}
		seq = run.issued[len(run.issued)-1]
	}
	deadline := time.Now().Add(beltAwaitWindow)
	for {
		command, found, err := run.head.store.CommandBySeq(seq)
		if err != nil {
			return "that command could not be read: " + err.Error(), true
		}
		if !found {
			return fmt.Sprintf("there is no command numbered %d", seq), true
		}
		if command.Status != store.CommandPending {
			return awaitReceipt(command), false
		}
		if !time.Now().Before(deadline) {
			return fmt.Sprintf("command %d is still queued — the workforce has not reached it yet. Say it is in hand, not that it is done", seq), false
		}
		time.Sleep(beltAwaitPoll)
	}
}

func awaitReceipt(command store.Command) string {
	if command.Status == store.CommandRejected {
		reason := strings.TrimSpace(command.Result)
		if reason == "" {
			reason = "no reason recorded"
		}
		return fmt.Sprintf("command %d was REFUSED: %s. Nothing changed — say so plainly", command.Seq, reason)
	}
	return fmt.Sprintf("command %d was applied", command.Seq)
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
				" | use the rule tool, not control")
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

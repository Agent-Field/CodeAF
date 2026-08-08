package head

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A person names existing work two ways. Usually they name what it is — "the
// audio job" — and BM25 over briefs finds it. Sometimes they name a set by its
// status — "the queued ones", "everything" — which is not a content reference
// at all. Scoring those words against briefs is how a request to withdraw
// fourteen queued tasks became "Cancelling line-scan.": the words matched a
// node that had nothing to do with what was asked, and one fuzzy match acted
// without asking. A class reference is answered by a filter over the snapshot
// instead, and a set larger than the cascade gate is confirmed once before
// anything moves.

// Class names. They are the vocabulary's meaning, not its spelling: several
// words map onto each one.
const (
	classQueued  = "queued"
	classRunning = "running"
	classFailed  = "failed"
	classAll     = "all"
)

const (
	// ClassSetNameLimit is how many titles a set receipt names before it counts
	// the remainder. Three is a sentence; ten is a list.
	ClassSetNameLimit = 3
	// ClassFallbackFloor is the score a lone content match must clear before it
	// may act on a message that also named a status class. It is redirection's
	// anchor floor — roughly one solid content word against a job's own brief —
	// because the doubt is the same one: does this sentence really point here?
	ClassFallbackFloor = RedirectAnchorScore
	// classNoScope marks an unscoped set inside a durable option value, whose
	// target field has no room for emptiness.
	classNoScope = "-"
	// charterNodeGroup is the marker the store keeps charter furniture under.
	// It is never executable work.
	charterNodeGroup = "charter"
)

// classVocabulary maps the words people actually use onto the four classes.
// "in progress" arrives here as its content word: "in" is a filler.
var classVocabulary = map[string]string{
	"queued": classQueued, "waiting": classQueued, "pending": classQueued,
	"unstarted": classQueued, "unclaimed": classQueued, "unqueued": classQueued,
	"running": classRunning, "active": classRunning, "progress": classRunning,
	"inflight": classRunning, "ongoing": classRunning,
	"failed": classFailed, "failing": classFailed,
	"everything": classAll, "all": classAll, "every": classAll, "rest": classAll,
}

// classSetMarkers are the generic plurals that mark a reference as a set. The
// plural is load-bearing: "the failed one" is one node the user has in mind and
// stays with ordinary content resolution, while "the failed ones" is a class.
var classSetMarkers = map[string]bool{
	"ones": true, "tasks": true, "jobs": true, "things": true, "stuff": true,
	"them": true, "those": true, "these": true, "items": true, "steps": true,
	"work": true, "everything": true, "all": true, "rest": true,
}

// classFillers carry no reference of their own: the command's own verbs, the
// articles around a noun, and the generic singulars that make a phrase point at
// exactly one thing.
var classFillers = map[string]bool{
	"please": true, "cancel": true, "stop": true, "kill": true, "abort": true,
	"pause": true, "hold": true, "resume": true, "unpause": true,
	"restart": true, "retry": true, "rerun": true, "try": true, "again": true,
	"actually": true, "amend": true, "change": true, "update": true, "make": true,
	"prioritize": true, "reprioritize": true, "do": true, "first": true,
	"the": true, "a": true, "an": true, "of": true, "in": true, "on": true,
	"and": true, "or": true, "my": true, "our": true, "any": true, "some": true,
	"is": true, "are": true, "was": true, "were": true, "still": true,
	"just": true, "now": true, "up": true, "out": true, "off": true,
	"one": true, "it": true, "that": true, "this": true, "job": true,
	"task": true, "step": true, "leaf": true, "part": true, "thing": true,
	"i": true, "want": true, "you": true, "to": true, "me": true,
}

// classSweepingWord is the one word that reaches past the user's own work into
// the resident's practice and charter internals. Everything short of it —
// "all", "the rest", "them all" — means all of *my* work.
const classSweepingWord = "everything"

// classIntent is one recognized set selector: which class, whether it sweeps
// the resident's own internals too, and the content words that scope it to a
// single job when the user named one.
type classIntent struct {
	Class    string
	Sweeping bool
	Scope    string
}

// classSet is the resolved set: the nodes a command is issued against, the
// count of work those commands reach, and how many jobs it spans.
type classSet struct {
	Units    []store.Node
	Affected int
	Jobs     int
}

// classSelector decides whether a surgery message names a set by status rather
// than a job by content. A set marker is required: without one the sentence is
// about a single thing the user has in mind, and guessing a whole set from it
// is exactly the over-reach this path exists to prevent.
func classSelector(message string) (classIntent, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	marked := false
	sweeping := false
	classes := make([]string, 0, 2)
	content := make([]string, 0, 4)
	for _, word := range surgeryWords(lower) {
		if word == classSweepingWord {
			sweeping = true
		}
		if classSetMarkers[word] {
			marked = true
		}
		if class, named := classVocabulary[word]; named {
			classes = appendClass(classes, class)
			continue
		}
		if classSetMarkers[word] || classFillers[word] {
			continue
		}
		content = append(content, word)
	}
	if !marked {
		return classIntent{}, false
	}
	intent := classIntent{Class: classAll, Sweeping: sweeping, Scope: strings.Join(content, " ")}
	if len(classes) == 1 {
		intent.Class = classes[0]
	}
	return intent, true
}

// mentionsClass reports whether class vocabulary appeared at all. A message
// that mentions a class but does not resolve as one is the uncertain middle,
// and that is where the wrong-target guard lives.
func mentionsClass(message string) bool {
	for _, word := range surgeryWords(strings.ToLower(message)) {
		if _, named := classVocabulary[word]; named {
			return true
		}
	}
	return false
}

func appendClass(classes []string, class string) []string {
	for _, existing := range classes {
		if existing == class {
			return classes
		}
	}
	return append(classes, class)
}

// classStatuses narrows the verb's own allowed-status table to the class the
// user named. An empty result means the two disagree — "restart the queued
// ones" — and that is said out loud rather than resolved to nothing.
func classStatuses(class classIntent, allowed []store.Status) []store.Status {
	var wanted []store.Status
	switch class.Class {
	case classQueued:
		wanted = []store.Status{store.Pending}
	case classRunning:
		wanted = []store.Status{store.Claimed, store.Running}
	case classFailed:
		wanted = []store.Status{store.Failed}
	default:
		return allowed
	}
	narrowed := make([]store.Status, 0, len(wanted))
	for _, status := range wanted {
		for _, candidate := range allowed {
			if status == candidate {
				narrowed = append(narrowed, status)
				break
			}
		}
	}
	return narrowed
}

// classSet reads the set straight out of the snapshot. This is the whole point
// of the class path: a set named by status is answered by a filter over the
// graph, never by scoring words against what the nodes happen to say.
//
// The unit is the outermost node whose whole open subtree is in the class. A
// job that has not started anywhere is one unit and its leaves are dropped,
// because the command already cascades to them. A job with a leaf running is
// not a unit at any depth above that leaf — its queued leaves become units
// individually and the running leaf is left alone. That is what "cancel the
// queued ones" has to mean to be safe: withdraw what has not started, never
// reach through it into what has.
func (h *Head) classSet(class classIntent, scopeRoot string, statuses []store.Status) (classSet, error) {
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return classSet{}, err
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	wanted := make(map[store.Status]bool, len(statuses))
	for _, status := range statuses {
		wanted[status] = true
	}
	matchedID := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node.ID == store.RootID || node.Folded || !wanted[node.Status] {
			continue
		}
		if node.Group == store.TerritoryGroup || node.Group == charterNodeGroup {
			continue
		}
		if !class.Sweeping &&
			(node.Group == store.PracticeGroup || node.Provenance.Origin == store.OriginSelf) {
			continue
		}
		if scopeRoot != "" && !classWithinScope(byID, node, scopeRoot) {
			continue
		}
		matchedID[node.ID] = true
	}
	return classUnits(nodes, matchedID), nil
}

// classUnits is the unit rule itself, over any matched set. It is separated
// from the class filter because the toolbelt arrives at its set from explicit
// ids rather than from a status word, and the rule that makes a set safe to act
// on must be the same one either way.
func classUnits(nodes []store.Node, matchedID map[string]bool) classSet {
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	children := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if _, ok := byID[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}
	matched := make([]store.Node, 0, len(matchedID))
	for _, node := range nodes {
		if matchedID[node.ID] {
			matched = append(matched, node)
		}
	}
	whole := make(map[string]bool, len(matched))
	for _, node := range matched {
		whole[node.ID] = classWholeSubtree(byID, children, matchedID, node.ID, make(map[string]bool, len(nodes)))
	}
	unit := make(map[string]bool, len(matched))
	jobs := make(map[string]bool, len(matched))
	set := classSet{Units: make([]store.Node, 0, len(matched))}
	for _, node := range matched {
		if !whole[node.ID] || classCoveredByUnit(byID, matchedID, whole, node) {
			continue
		}
		unit[node.ID] = true
		set.Units = append(set.Units, node)
		jobs[classJobRoot(byID, node)] = true
	}
	for _, node := range matched {
		if unit[node.ID] || classCoveredByUnit(byID, matchedID, whole, node) {
			set.Affected++
		}
	}
	set.Jobs = len(jobs)
	return set
}

// classWholeSubtree reports that nothing open below this node falls outside the
// class. It is the safety property: a command may only cascade through work the
// user's words actually named.
func classWholeSubtree(byID map[string]store.Node, children map[string][]string,
	matched map[string]bool, id string, seen map[string]bool) bool {
	if seen[id] {
		return true
	}
	seen[id] = true
	for _, child := range children[id] {
		node := byID[child]
		if classOpen(node.Status) && !matched[child] {
			return false
		}
		if !classWholeSubtree(byID, children, matched, child, seen) {
			return false
		}
	}
	return true
}

// classCoveredByUnit reports that an ancestor already carries this node, so
// naming it again would double-count and double-journal it.
func classCoveredByUnit(byID map[string]store.Node, matched map[string]bool,
	whole map[string]bool, node store.Node) bool {
	for steps := 0; steps <= len(byID); steps++ {
		parent, ok := byID[node.Parent]
		if !ok {
			return false
		}
		if matched[parent.ID] && whole[parent.ID] {
			return true
		}
		node = parent
	}
	return false
}

func classOpen(status store.Status) bool {
	switch status {
	case store.Done, store.Failed, store.Cancelled:
		return false
	default:
		return true
	}
}

func classWithinScope(byID map[string]store.Node, node store.Node, scopeRoot string) bool {
	for steps := 0; steps <= len(byID); steps++ {
		if node.ID == scopeRoot {
			return true
		}
		parent, ok := byID[node.Parent]
		if !ok {
			return false
		}
		node = parent
	}
	return false
}

func classJobRoot(byID map[string]store.Node, node store.Node) string {
	for steps := 0; steps <= len(byID); steps++ {
		if node.Parent == "" || node.Parent == store.RootID {
			return node.ID
		}
		parent, ok := byID[node.Parent]
		if !ok || parent.Group == store.TerritoryGroup {
			return node.ID
		}
		node = parent
	}
	return node.ID
}

// classImpact aggregates the same loss the single-node gate reads. OpenNodes
// carries the set's own count so the cascade gate governs the set rather than
// whichever member happens to be biggest.
func (h *Head) classImpact(set classSet) (store.SurgeryImpact, error) {
	aggregate := store.SurgeryImpact{Nodes: set.Affected, OpenNodes: set.Affected}
	now := time.Now()
	for _, node := range set.Units {
		impact, err := h.store.Impact(node.ID, now)
		if err != nil {
			return store.SurgeryImpact{}, err
		}
		aggregate.Cost += impact.Cost
		aggregate.Running += impact.Running
		if impact.RunningFor > aggregate.RunningFor {
			aggregate.RunningFor = impact.RunningFor
		}
	}
	return aggregate, nil
}

// resolveClassSurgery is the class path end to end: narrow the statuses, settle
// the scope if the user named one, read the set, gate it once, act.
func (h *Head) resolveClassSurgery(user store.Message, kind store.CommandKind, instruction string,
	class classIntent, scopeRoot string, confirmed bool) error {
	statuses := classStatuses(class, surgeryAllowedStatuses(kind))
	if len(statuses) == 0 {
		return h.postAgent(user.SessionID,
			fmt.Sprintf("I can't %s %s work.", surgeryVerb(kind), class.Class), 0)
	}
	if scopeRoot == "" && class.Scope != "" {
		matches, err := h.surgeryMatches(surgeryIntent{Kind: kind, Reference: class.Scope})
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return h.postAgent(user.SessionID,
				fmt.Sprintf("I couldn't find any current work matching %q.", class.Scope), 0)
		}
		if len(matches) > 1 {
			options := make([]store.QuestionOption, 0, len(matches))
			for _, match := range matches {
				options = append(options, store.QuestionOption{
					Label: surgeryTargetLabel(match.Node),
					Hint:  surgeryTargetHint(match),
					Value: encodeSurgeryOption("classscope", kind, match.Node.ID, instruction),
				})
			}
			return h.postQuestion(user.SessionID, "Which job do you mean?", 0, options)
		}
		scopeRoot = matches[0].Node.ID
	}
	set, err := h.classSet(class, scopeRoot, statuses)
	if err != nil {
		return err
	}
	if len(set.Units) == 0 {
		return h.postAgent(user.SessionID, classEmptyReply(kind, class), 0)
	}
	if !confirmed {
		impact, err := h.classImpact(set)
		if err != nil {
			return err
		}
		if surgeryNeedsConfirmation(kind, impact) {
			return h.askClassConfirm(user, kind, instruction, class, scopeRoot, set, impact)
		}
	}
	return h.commitClassSurgery(user, kind, instruction, class, set)
}

// askClassConfirm is the one question a set is allowed. It names the count
// before anything moves, because the difference between one node and fourteen
// is the whole reason the user would want to be asked.
func (h *Head) askClassConfirm(user store.Message, kind store.CommandKind, instruction string,
	class classIntent, scopeRoot string, set classSet, impact store.SurgeryImpact) error {
	verb := surgeryVerb(kind)
	prompt := fmt.Sprintf("%s %s?", upperFirst(verb), classSetPhrase(set, class))
	if impact.Cost > 0 || impact.RunningFor > 0 {
		// The set's own count is already in the prompt, so the loss clause must
		// not repeat it.
		lossOnly := impact
		lossOnly.OpenNodes, lossOnly.Nodes = 0, 0
		prompt += " " + surgeryLoss(kind, lossOnly)
	}
	if scopeRoot == "" {
		scopeRoot = classNoScope
	}
	return h.askSurgerySetConfirm(user, prompt, set,
		store.QuestionOption{Label: fmt.Sprintf("yes, %s all %d", verb, set.Affected),
			Value: encodeSurgeryOption("class", kind, scopeRoot, instruction)},
		store.QuestionOption{Label: classKeepLabel(kind, class),
			Value: encodeSurgeryOption("classkeep", kind, scopeRoot, instruction)})
}

// askSurgerySetConfirm posts the one blocking question a set is allowed. Every
// set path shares it — the class path and the toolbelt differ only in how they
// arrived at the set, never in how consent is asked for or how a muted question
// falls back to keeping things as they are.
func (h *Head) askSurgerySetConfirm(user store.Message, prompt string, set classSet,
	yes, keep store.QuestionOption) error {
	allowFree := false
	options := []store.QuestionOption{yes, keep}
	if ask, _, err := h.store.ShouldAsk(store.QuestionCategorySurgeryConfirm); err == nil && !ask {
		if err := h.store.RecordAssumedWithDefault(store.QuestionCategorySurgeryConfirm, "2",
			user.SessionID, prompt); err == nil {
			return h.postAgent(user.SessionID, "Assuming the default: "+keep.Label+".", 0)
		}
	}
	body := store.QuestionMessageBody(prompt, options, store.QuestionConfig{
		Kind: store.QuestionConfirm, Category: store.QuestionCategorySurgeryConfirm,
		Default: "2", AllowFree: &allowFree,
	})
	question, err := h.store.AskQuestion(store.AgentQuestion{
		SessionID: user.SessionID, Text: body, OriginNodeID: set.Units[0].ID,
		Urgency: store.QuestionBlocking, Category: store.QuestionCategorySurgeryConfirm,
		DefaultAnswer: "2", Options: options,
	})
	if err != nil {
		return err
	}
	_, err = h.store.SurfaceQuestion(question.Seq)
	return err
}

// commitClassSurgery journals one ordinary command per unit rather than a new
// batched kind. Every replay, gate and receipt path already knows this shape,
// so a set costs Rebuild nothing it has not replayed before — and a unit that
// settled between the question and the answer simply fails validation and drops
// out instead of poisoning a batch.
func (h *Head) commitClassSurgery(user store.Message, kind store.CommandKind, instruction string,
	class classIntent, set classSet) error {
	labels, first := h.journalUnits(user, kind, instruction, set.Units)
	if len(labels) == 0 {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	if len(labels) < len(set.Units) {
		set.Affected = len(labels)
		set.Jobs = 0
	}
	return h.postAgent(user.SessionID, classReceipt(kind, class, set, labels), first)
}

// journalUnits is the one commit path every set shares: one ordinary command
// per unit, and the first seq so the reply can be tied to durable work. A unit
// that settled since the set was read simply fails validation and drops out
// instead of poisoning the rest.
func (h *Head) journalUnits(user store.Message, kind store.CommandKind, instruction string,
	units []store.Node) ([]string, int64) {
	labels := make([]string, 0, len(units))
	var first int64
	// The set path reaches restart too — "rerun the failed ones on the better
	// model" is one sentence — so the model reading rides the same funnel here
	// as it does on the single-target path.
	instruction = restartInstruction(kind, instruction)
	for _, node := range units {
		command, err := h.store.RequestCommand(store.Command{
			SessionID: user.SessionID, Kind: kind, Target: node.ID, Instruction: instruction,
		})
		if err != nil {
			continue
		}
		if first == 0 {
			first = command.Seq
		}
		labels = append(labels, surgeryTargetLabel(node))
	}
	return labels, first
}

// applyClassOption settles every answer the class path can produce.
// Both re-resolve from the user's verbatim words rather than from a frozen list
// of ids: the set the user agreed to is the set as it stands when they agree.
func (h *Head) applyClassOption(user store.Message, action string, kind store.CommandKind,
	target, instruction string) (bool, error) {
	// The toolbelt's confirmations ride the same codec and the same two dispatch
	// sites; they carry ids where a class answer carries a scope root.
	if handled, err := h.applyBeltOption(user, action, kind, target, instruction); handled {
		return true, err
	}
	class, ok := classSelector(instruction)
	if !ok {
		return false, nil
	}
	scopeRoot := strings.TrimSpace(target)
	if scopeRoot == classNoScope {
		scopeRoot = ""
	}
	switch action {
	case "class":
		return true, h.resolveClassSurgery(user, kind, instruction, class, scopeRoot, true)
	case "classkeep":
		return true, h.postAgent(user.SessionID, "Keeping them as they are.", 0)
	case "classscope":
		return true, h.resolveClassSurgery(user, kind, instruction, class, scopeRoot, false)
	}
	return false, nil
}

func classReceipt(kind store.CommandKind, class classIntent, set classSet, labels []string) string {
	named := labels
	suffix := ""
	if len(named) > ClassSetNameLimit {
		suffix = fmt.Sprintf(" and %d more", len(named)-ClassSetNameLimit)
		named = named[:ClassSetNameLimit]
	}
	phrase := classSetPhrase(set, class)
	if set.Jobs > 1 && set.Jobs < set.Affected {
		return fmt.Sprintf("%s %s across %d jobs — %s%s.",
			surgeryProgressive(kind), phrase, set.Jobs, strings.Join(named, ", "), suffix)
	}
	return fmt.Sprintf("%s %s — %s%s.",
		surgeryProgressive(kind), phrase, strings.Join(named, ", "), suffix)
}

// classSetPhrase is the counted noun both the question and the receipt use, so
// the number the user agreed to is the number they are told about.
func classSetPhrase(set classSet, class classIntent) string {
	noun := pluralWord(set.Affected, "task", "tasks")
	if class.Class == classAll {
		return fmt.Sprintf("%d %s", set.Affected, noun)
	}
	phrase := fmt.Sprintf("%d %s %s", set.Affected, class.Class, noun)
	if set.Jobs > 1 && set.Jobs < set.Affected {
		phrase += fmt.Sprintf(" across %d jobs", set.Jobs)
	}
	return phrase
}

func classKeepLabel(kind store.CommandKind, class classIntent) string {
	if kind == store.CommandRestart {
		return "keep them failed"
	}
	if class.Class == classAll {
		return "keep them going"
	}
	return "keep them " + class.Class
}

func classEmptyReply(kind store.CommandKind, class classIntent) string {
	switch class.Class {
	case classQueued:
		return "Nothing is queued right now."
	case classRunning:
		return "Nothing is running right now."
	case classFailed:
		return "Nothing has failed right now."
	default:
		return "There's no open work to " + surgeryVerb(kind) + " right now."
	}
}

func surgeryProgressive(kind store.CommandKind) string {
	switch kind {
	case store.CommandCancel:
		return "Cancelling"
	case store.CommandPause:
		return "Pausing"
	case store.CommandResume:
		return "Resuming"
	case store.CommandAmend:
		return "Amending"
	case store.CommandReprioritize:
		return "Prioritizing"
	case store.CommandRestart:
		return "Restarting"
	default:
		return "Updating"
	}
}

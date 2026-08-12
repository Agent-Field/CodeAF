package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// change: "this thing, but different."
//
// Five tools used to stand here — steer, revise, expedite, and the change halves
// of rule, service and craft — and the split was machinery-shaped rather than
// person-shaped. "use latex please" should both tell the workers already running
// AND fix the remaining plan, and the redirect path has always done both
// (redirect.go broadcasts first, then revises); the belt merely made the model
// choose between two halves of one intent. Worse, the model had to know which
// half it wanted BEFORE anything with the plan in hand had read the sentence.
//
// So the critical move is that change carries NO VERB (chat-simplify §2.3). The
// head binds the person's intent to a target and journals their words verbatim;
// what the words mean is decided on the far side, by the party that can see the
// plan. The revision judge already owns that decision — plan edit, broadcast-only
// constraint, or both — and it keeps it.
//
// The one thing the head still decides is the store kind, and only where the
// store's own verbs are exact: resume, restart and expedite are lifecycle
// transitions with no interpretation in them, and a redirect could not perform
// them however well it read the sentence. Everything else — every sentence about
// what the work IS — goes as CommandRedirect with the words untouched.
//
// Every consent gate and legality check stays where it is: the store's legality
// table, the charter transition table, the craft repository's own refusals.

func (run *beltRun) change(args map[string]any) (string, bool) {
	target := strings.TrimSpace(beltString(args, "target"))
	if target == "" {
		return "target must name one id from a read — a job, a standing rule, a service, or a learned way of working", true
	}
	words := strings.TrimSpace(beltString(args, "words"))
	if words == "" {
		words = strings.TrimSpace(run.user.Body)
	}
	if words == "" {
		return "words must carry what they said, verbatim", true
	}
	if run.confirm != nil {
		return "the user has already been asked to confirm a change; nothing else may act until they answer", true
	}
	switch found := run.head.resolveTarget(target); found.kind {
	case targetJob:
		return run.changeJob(found.job, words)
	case targetRule:
		return run.changeRule(found.rule, words)
	case targetService:
		return run.changeService(found.service, words)
	default:
		return run.changeCraft(target, words)
	}
}

// changeJob is the whole of what the head decides about live work: the three
// exact lifecycle verbs, and otherwise the person's own words handed to the
// revision judge.
func (run *beltRun) changeJob(job store.Node, words string) (string, bool) {
	kind, exact := changeLifecycle(job, words)
	if !exact {
		if !classOpen(job.Status) {
			// Settled work cannot be redirected — there is no remaining plan to
			// edit. What they are asking for is another attempt with the criticism
			// in hand, which is the other verb's job.
			return surgeryTargetLabel(job) + " has already finished, so there is no plan left to change. " +
				"If they are unhappy with what it delivered, commission a task with amends set to " + job.ID, true
		}
		seq, err := run.head.journalRevision(run.user, store.CommandRedirect, job.ID, words)
		if err != nil {
			return "that could not be queued: " + err.Error(), true
		}
		// The fallback receipt promises only the handoff. What actually changed in
		// the plan, and who was told, is written a moment later by the reconciler
		// that did it — the only party that honestly knows.
		run.record(seq, "Taking that to "+surgeryTargetLabel(job)+" — I'll say what changed once it lands.")
		return "the workers already under way on " + surgeryTargetLabel(job) +
			" hear those words, and its remaining plan is revised against them; " +
			"what that came to is said here when it lands", false
	}
	if !beltLegal(job, kind) {
		return quoted(job.ID) + " is " + beltStatusWord(job) + ", and " + surgeryVerb(kind) +
			" only applies to " + beltAllowedWords(kind), true
	}
	seq, err := run.head.journalRevision(run.user, kind, job.ID, restartInstruction(kind, words))
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(seq, surgeryQueuedReceipt(kind, job, words))
	if kind == store.CommandExpedite {
		return surgeryTargetLabel(job) + " moves up the claim order and its unstarted tail is trimmed", false
	}
	return strings.ToLower(surgeryProgressive(kind)) + " " + surgeryTargetLabel(job), false
}

// changeLifecycle is the narrow deterministic half: the store's own verbs, where
// the words are the verb and there is nothing left to interpret. Everything it
// declines goes to the judge, which is the safe direction — a redirect carrying
// "resume it" is at worst a no-op broadcast, while a resume carrying "make it
// about the sea" would silently throw the sentence away.
func changeLifecycle(job store.Node, words string) (store.CommandKind, bool) {
	lower := strings.ToLower(strings.TrimSpace(words))
	switch {
	case changeSays(lower, "resume", "unpause", "unfreeze", "carry on", "keep going", "pick it back up"):
		return store.CommandResume, true
	case !classOpen(job.Status) &&
		changeSays(lower, "restart", "re-run", "rerun", "run it again", "try again", "go again", "do it again"):
		// Only for work that has stopped. A restart of live work is not a thing
		// the store allows, and the same words about a running job mean "change
		// how you are doing it" — which is the judge's.
		return store.CommandRestart, true
	case classOpen(job.Status) && urgencyCued(lower):
		// Impatience asks for the same deliverable sooner. Compiling it as a
		// change to the work does the opposite of what was asked.
		return store.CommandExpedite, true
	}
	return "", false
}

// changeSays is a prefix-or-phrase test rather than a bag of words, because
// these three readings only fire on sentences that are essentially the verb:
// "resume the finance one" is a resume, "the resume parser is broken" is not.
func changeSays(lower string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.HasPrefix(lower, phrase) || strings.Contains(lower, " "+phrase) {
			return true
		}
	}
	return false
}

// changeRule resolves the person's words onto the charter transitions that
// change a rule without withdrawing it: when it runs, what it says, and whether
// it asks first. The resolution is charterManagement's — the same reading the
// deterministic charter path has always used — and where it cannot tell, the
// candidates come back exactly as every other ambiguity in this package does.
func (run *beltRun) changeRule(rule store.Charter, words string) (string, bool) {
	intent, read := charterManagement(words)
	kind := intent.Kind
	switch {
	case !read, kind == store.CommandCharterRetire, kind == store.CommandCharterPause:
		// Retiring and holding are withdrawal and belong to stop; an unreadable
		// sentence is not one to guess at. Both come back as choices.
		return "those words do not say plainly which change to " + firstLine(rule.Invariant) +
			" they mean. Ask which, with these as the options:\n" +
			"- when it runs (say the new rhythm)\n" +
			"- what it says (say the new message)\n" +
			"- going back to asking before each run\n" +
			"- retiring or holding it — that is stop, not change", false
	}
	instruction := words
	switch kind {
	case store.CommandCharterCadence:
		if intent.Cadence != "" {
			instruction = intent.Cadence
		}
	case store.CommandCharterWording:
		if intent.Wording != "" {
			instruction = intent.Wording
		}
	default:
		instruction = string(kind)
	}
	command, err := run.head.store.RequestCommand(store.Command{
		SessionID: run.user.SessionID, Kind: kind, Target: rule.ID, Instruction: instruction,
	})
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(command.Seq, charterAcknowledgement(kind))
	return "queued: " + charterAcknowledgement(kind), false
}

// changeService reads the two things a person changes about something they are
// running — start it over, or stop it deciding for itself whether to. Stopping
// it is withdrawal and belongs to stop.
func (run *beltRun) changeService(service store.Service, words string) (string, bool) {
	lower := strings.ToLower(words)
	action := ""
	switch {
	case changeSays(lower, "restart", "start it again", "bounce", "reboot", "bring it back"):
		action = "restart"
	case changeSays(lower, "stop restarting", "don't restart", "dont restart", "do not restart",
		"no auto", "stop bringing it back", "leave it down"):
		action = "disable-auto-restart"
	case changeSays(lower, "auto", "keep it up", "bring it back automatically", "restart itself",
		"restart it automatically"):
		action = "auto-restart"
	default:
		return "those words do not say plainly what to change about " + service.Name +
			". Ask which, with these as the options:\n" +
			"- restarting it now\n" +
			"- having it come back by itself when it falls over\n" +
			"- leaving it down when it falls over\n" +
			"- stopping it — that is stop, not change", false
	}
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

// changeCraft is the last resort, and it is deliberately the last resort: a
// learned way of working is named rather than identified, because the craft
// repository is a git repo and not a table in this store, so nothing here can
// confirm that a name exists. The one change a craft takes is going back a
// version, and the repository refuses one that cannot say why.
func (run *beltRun) changeCraft(name, words string) (string, bool) {
	if len(strings.Fields(words)) < craft.MinReasonWords {
		return "nothing on the board, no standing rule and no service is called " + quoted(name) + ". " +
			"If that is a learned way of working, say what the newer version got wrong — a few words at least. " +
			"Otherwise read again for the id", true
	}
	command, err := run.head.store.RequestCommand(store.Command{
		SessionID: run.user.SessionID, Kind: store.CommandCraftRevert,
		Target: name, Instruction: words,
	})
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(command.Seq, craftAcknowledgement(store.CommandCraftRevert, name))
	return "queued: " + strings.ToLower(craftAcknowledgement(store.CommandCraftRevert, name)), false
}

// targetKind is what one id the model named turned out to be. The person owns
// four kinds of thing and says one id for any of them; this is the one place
// that decides which.
type targetKind int

const (
	targetCraft targetKind = iota
	targetJob
	targetRule
	targetService
)

type resolvedTarget struct {
	kind    targetKind
	job     store.Node
	rule    store.Charter
	service store.Service
}

// resolveTarget reads one id in the order the ids are trustworthy: a graph node
// is exact, a charter id is exact, a service is exact by id or by name — and a
// craft is a NAME the store has never seen, so it is what is left. A mistyped id
// therefore lands as a craft and is refused in words by the repository, which is
// the same shape a missing service has always had.
func (h *Head) resolveTarget(id string) resolvedTarget {
	id = strings.TrimSpace(id)
	if h == nil || h.store == nil || id == "" {
		return resolvedTarget{}
	}
	if node, found, err := h.store.Node(id); err == nil && found &&
		node.ID != store.RootID && beltAddressable(node) {
		return resolvedTarget{kind: targetJob, job: node}
	}
	if rule, found, err := h.store.Charter(id); err == nil && found {
		return resolvedTarget{kind: targetRule, rule: rule}
	}
	if service, found, err := h.store.Service(id); err == nil && found {
		return resolvedTarget{kind: targetService, service: service}
	}
	if service, found, err := h.store.ServiceByName(id); err == nil && found {
		return resolvedTarget{kind: targetService, service: service}
	}
	return resolvedTarget{kind: targetCraft}
}

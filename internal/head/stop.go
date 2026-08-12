package head

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// stop: withdraw something.
//
// It stays its own verb rather than folding into change (chat-simplify §4.2)
// because withdrawal is definite and consent-gated: a destructive intent
// deserves an unambiguous door, and a gate that fires on a sentence the judge
// was still interpreting would be a gate on nothing.
//
// One tool covers four kinds because the person's intent is one intent: they
// point at things they own and say stop. What "stop" means for each kind is the
// store's own vocabulary — cancel work, retire a rule or a learned way, stop a
// service — and the only reading taken from the words is whether they want it
// HELD rather than ended, which the two kinds that support holding honour and
// the two that do not ignore.
//
// The consent machinery is untouched and reached by the same road control used:
// beltSet resolves the ids over their open subtrees under the store's legality
// table, classImpact prices the whole set once, surgeryNeedsConfirmation decides,
// and journalUnits is the single commit path. Nothing about the gate knows this
// tool is new.

// stopEverythingTargets are the two ways the total withdrawal is named. It is a
// reserved word rather than an argument because it is the one stop that is not
// about ids at all — it reaches every service the person is running, and asks
// once about the live work.
var stopEverythingTargets = map[string]bool{"everything": true, "all": true}

func (run *beltRun) stop(args map[string]any) (string, bool) {
	targets := beltStrings(args, "targets")
	words := strings.TrimSpace(beltString(args, "words"))
	if words == "" {
		words = strings.TrimSpace(run.user.Body)
	}
	if len(targets) == 0 {
		// A withdrawal that knows what it wants to do and not what to do it to is
		// an ordinary thing to say. The union of the live jobs and the standing
		// rules the words reach is the honest answer — handed back as candidates
		// rather than acted on, because choosing for the person is how a request
		// to withdraw fourteen queued tasks became "Cancelling line-scan."
		return run.controlCandidates(store.CommandCancel, strings.TrimSpace(beltString(args, "words")))
	}
	if len(targets) > beltControlIDCap {
		return fmt.Sprintf("that is %d ids; name the jobs rather than their steps", len(targets)), true
	}
	if run.confirm != nil {
		return "the user has already been asked to confirm a change; nothing else may act until they answer", true
	}
	holding := stopHolds(words)

	var jobs []string
	var rules []store.Charter
	var services []store.Service
	var crafts []string
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if stopEverythingTargets[strings.ToLower(target)] {
			// The one total phrasing, and it keeps its own gate: services are
			// stopped unconditionally because they are the person's own persistent
			// effects, and the live jobs are asked about once with what they cost
			// quoted. The gate owns the words — the loop must not speak over a
			// consent question.
			if err := run.head.shutDownEverything(run.user); err != nil {
				return "that could not be done: " + err.Error(), true
			}
			run.acted, run.spoke = true, true
			return "every running service is being stopped, and the user has been asked about the live jobs; nothing else to say", false
		}
		switch found := run.head.resolveTarget(target); found.kind {
		case targetJob:
			jobs = append(jobs, found.job.ID)
		case targetRule:
			rules = append(rules, found.rule)
		case targetService:
			services = append(services, found.service)
		default:
			crafts = append(crafts, target)
		}
	}

	said := make([]string, 0, 4)
	// Work goes first and alone if it raises the gate. Nothing else may act while
	// the person is being asked, which is the same rule every confirmation in this
	// package obeys.
	if len(jobs) > 0 {
		answer, gated, failed := run.stopJobs(jobs, holding)
		if failed || gated {
			return answer, failed
		}
		said = append(said, answer)
	}
	for _, rule := range rules {
		kind := store.CommandCharterRetire
		if holding {
			kind = store.CommandCharterPause
		}
		command, err := run.head.store.RequestCommand(store.Command{
			SessionID: run.user.SessionID, Kind: kind, Target: rule.ID, Instruction: string(kind),
		})
		if err != nil {
			return "that could not be queued: " + err.Error(), true
		}
		run.record(command.Seq, charterAcknowledgement(kind))
		said = append(said, strings.ToLower(strings.TrimSuffix(charterAcknowledgement(kind), ".")))
	}
	for _, service := range services {
		command, err := run.head.store.RequestCommand(store.Command{
			SessionID: run.user.SessionID, Kind: store.CommandServiceStop,
			Target: service.ID, Instruction: "stop",
		})
		if err != nil {
			return "that could not be queued: " + err.Error(), true
		}
		run.record(command.Seq, serviceReceipt(store.CommandServiceStop, service, "stop"))
		said = append(said, "stopping "+service.Name)
	}
	for _, name := range crafts {
		// Nothing is deleted: the file and its history stay where they are and the
		// recognizer simply stops offering it. A name nobody has forged is refused
		// in words by the repository, which is where the authority on names lives.
		command, err := run.head.store.RequestCommand(store.Command{
			SessionID: run.user.SessionID, Kind: store.CommandCraftRetire,
			Target: name, Instruction: retirementReason(words),
		})
		if err != nil {
			return "that could not be queued: " + err.Error(), true
		}
		run.record(command.Seq, craftAcknowledgement(store.CommandCraftRetire, name))
		said = append(said, "not working the "+name+" way any more")
	}
	if len(said) == 0 {
		return "none of that could be withdrawn", true
	}
	return strings.Join(said, "; "), false
}

// stopJobs is control's cancel arm, unchanged in every part that matters. The
// second return says the gate fired: nothing was journaled, the person is being
// asked, and no other target may act until they answer.
func (run *beltRun) stopJobs(ids []string, holding bool) (string, bool, bool) {
	kind := store.CommandCancel
	if holding {
		kind = store.CommandPause
	}
	set, err := run.head.beltSet(ids, kind, true)
	if err != nil {
		return err.Error(), false, true
	}
	if len(set.Units) == 0 {
		return fmt.Sprintf("there is nothing there that %s can touch right now", surgeryVerb(kind)), false, true
	}
	impact, err := run.head.classImpact(set)
	if err != nil {
		return "the cost of that could not be read: " + err.Error(), false, true
	}
	if surgeryNeedsConfirmation(kind, impact) {
		run.confirm = &beltConfirm{kind: kind, ids: ids, set: set, impact: impact}
		return fmt.Sprintf("needs_confirmation: %s %s crosses the consent gate. NOTHING has changed. The user is being asked and their answer settles it.",
			surgeryVerb(kind), classSetPhrase(set, classIntent{Class: classAll})), true, false
	}
	labels, seq := run.head.journalUnits(run.user, kind, run.user.Body, set.Units)
	if len(labels) == 0 {
		return "none of that could be queued", false, true
	}
	run.record(seq, classReceipt(kind, classIntent{Class: classAll}, set, labels))
	return fmt.Sprintf("%s %d: %s", surgeryProgressive(kind), len(labels), strings.Join(labels, ", ")), false, false
}

// stopHolds reads the one distinction stop takes from the words: held, or ended.
// It is a small closed reading and it stays small — the kinds that cannot be
// held ignore it, so a wrong reading costs a cancel that should have been a
// pause, which the person undoes with one word.
func stopHolds(words string) bool {
	lower := strings.ToLower(strings.TrimSpace(words))
	for _, phrase := range []string{"pause", "hold", "freeze", "suspend", "park", "on ice", "for now"} {
		if strings.HasPrefix(lower, phrase) || strings.Contains(lower, " "+phrase) {
			return true
		}
	}
	return false
}

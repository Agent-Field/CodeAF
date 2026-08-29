package config

import "fmt"

// THE SELF-SERVICE LAW: A SETTING THAT EXISTS TO RESTRAIN THE MODEL IS NOT THE
// MODEL'S TO CHANGE.
//
// The chat can change a person's settings for them — "use deepseek for
// planning", "stop drawing timestamps" — through a belt tool that is a door onto
// this registry (internal/session's tools_settings.go). That door needs one
// thing this file gives it: a list of the rows it must refuse.
//
// The reasoning is the plainest there is. Some of these rows exist for no other
// purpose than to stand between the model and something the person wanted to be
// asked about first — the tool gate, the shell rules, the day's budget, the
// auditor that checks a node's work before it merges. A model that can widen its
// own restraints has none, and it does not have to be malicious to do it: a
// model asked to "make the tests stop nagging me" will reach for the approval
// mode because it is the shortest road, and the person who asked will have lost
// the gate without ever deciding to.
//
// So the list lives HERE, beside the rows it names, rather than in the package
// that holds the tool. A row is declared once in [Settings.build], and who may
// turn it is a fact about the row rather than a fact about one caller — a second
// copy in a tool file would be a list that goes stale the first time a rail is
// added, and a rail nobody guarded is a rail that guards nothing.
//
// ERR TOWARD REFUSING. A row wrongly refused costs the person eight keystrokes
// in /settings. A row wrongly allowed costs them the thing the row was for.

// The reasons a row is refused, each naming the KIND of restraint rather than
// the row. They are grouped this way because the person reading a refusal wants
// to know what they are being protected from, and "spending" said once about six
// rails is a sentence somebody can hold; six bespoke sentences are six things to
// read and no rule to learn.
const (
	// guardConsent is every row that decides whether the person is asked before
	// the model acts, and how long they get to answer.
	guardConsent = "decides what I may do without asking you first"
	// guardSpending is every rail on money. The day's ceiling and the session's
	// are the obvious two; the repair loop and the two context caps are the same
	// thing at a finer grain — each is a number that multiplies what one piece of
	// unattended work costs.
	guardSpending = "is one of the rails on what may be spent without you being asked"
	// guardPressure is the pair of ceilings this MACHINE has, plus the count
	// above them. Widening these does not spend money directly; it puts the
	// laptop into swap while nobody is watching, which is the same kind of harm
	// in a different currency.
	guardPressure = "is one of the brakes on how much work may run at once on this machine"
	// guardProof is the auditor. A model that can switch off the check on its own
	// work can call anything done.
	guardProof = "decides whether my own work is checked before it lands"
	// guardSignature is the trailer aforge puts on commits and PRs it writes. A
	// model taking its own name off work that leaves the machine is the one
	// change nobody downstream can see was made.
	guardSignature = "decides how work that leaves this machine is signed"
	// guardCredential is every [Setting.Secret] row and the plain-text fields
	// that name sign-in applications. These are refused for a different reason
	// from the rest — they restrain nothing — but the outcome of getting one
	// wrong is the same shape: a key overwritten with something a model invented
	// is a working account broken silently, and nothing on screen can tell the
	// difference between that and a key that expired.
	guardCredential = "holds a credential of yours"
)

// selfServiceGuards names every row a model may not write, and why.
//
// [Setting.Secret] is deliberately NOT listed key by key: the flag already marks
// a credential row, and a guard derived from it covers the next secret row
// somebody adds without anyone remembering this file exists. Everything else is
// named, because there is no flag on a row that says "this one is a rail" and
// inventing one would put the whole list into [Settings.build] where it could
// not be read as a list.
//
// settings_test.go asserts every key here is a real row, so the list cannot rot
// past a rename.
var selfServiceGuards = map[string]string{
	// The gate itself, the two rule rows under it, and the three clocks and
	// stand-ins around it. Together they are the whole answer to "am I asked
	// before this runs".
	KeyToolApprovalMode: guardConsent,
	KeyToolApprovals:    guardConsent,
	KeyBashApprovals:    guardConsent,
	KeyGuardian:         guardConsent,
	KeyConsentTimeout:   guardConsent,
	KeyTaskAutoApprove:  guardConsent,

	// The rails on money, coarse to fine: the day, the ask-first threshold, the
	// practice carve-out, one conversation, one node's retries, and the two caps
	// on how much context a single worker may send and re-send at full price.
	KeyDailyBudget:      guardSpending,
	KeyPlanConsent:      guardSpending,
	KeyPracticeBudget:   guardSpending,
	KeySpendRail:        guardSpending,
	KeyTaskRepairRounds: guardSpending,
	KeyWorkingSet:       guardSpending,
	KeyContextReuse:     guardSpending,

	// The roster of workers. It is a spending rail rather than a preference:
	// the specialists are the expensive way of taking a job — a whole pipeline
	// of model calls where the generalist is one sitting — and a model that
	// could put one back onto its own roster could then choose it for its own
	// next piece of work. The person who wrote the roster wrote it to stop
	// exactly that, so the row is theirs and not the model's.
	KeyWorkers: guardSpending,

	// The machine's own ceilings.
	KeyTaskParallel:  guardPressure,
	KeyTaskMaxLoad:   guardPressure,
	KeyTaskMinFreeMB: guardPressure,

	KeyTaskAudit:   guardProof,
	KeyAttribution: guardSignature,

	// The credential fields that are not [Setting.Secret] rows. Google's id is
	// useless without the secret beside it; Slack's public application needs
	// only its id. A model that could rewrite either could silently break the
	// person's connection or send their next sign-in somewhere they did not
	// choose.
	KeyGoogleOAuthClient: guardCredential,
	KeySlackOAuthClient:  guardCredential,
}

// SelfService reports whether a model changing settings on the person's behalf
// may write this row. A surface rendering the sheet can read it too — a row the
// chat will refuse is a fact worth showing beside the row.
func (s Setting) SelfService() bool {
	_, guarded := s.guarded()
	return !guarded
}

// SelfServiceRefusal is the sentence a guarded row answers a model with, and the
// empty string for a row the model may write.
//
// It names the row twice — the person's label and the key — because the two
// readers of this sentence want different halves: the person recognises the
// label they see in the panel, and the model needs the key to be sure it is
// talking about the same row when it explains itself. And it says where to go,
// because a refusal that does not name the door is a refusal that ends the
// conversation.
func (s Setting) SelfServiceRefusal() string {
	why, guarded := s.guarded()
	if !guarded {
		return ""
	}
	return fmt.Sprintf("%q (%s) %s, so it is not mine to change. Open /settings and change it yourself.", s.Label, s.Key, why)
}

// guarded is the lookup both of the above run: the secret flag first, because it
// is the rule that covers rows nobody listed.
func (s Setting) guarded() (string, bool) {
	if s.Secret {
		return guardCredential, true
	}
	why, guarded := selfServiceGuards[s.Key]
	return why, guarded
}

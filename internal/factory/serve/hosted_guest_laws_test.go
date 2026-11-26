package serve

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// There are two guest laws in this program, and they answer two different questions about the same wire. The personal
// one (remote.GuestMay) is what a person's OWN paired computer may do in a chat another of their computers runs: it is
// one switch pair per computer, watch and type, kept on the computer that runs the chat. The organization's (this
// package's hostedGuestMethods) is what a MEMBER may do in a hosted chat somebody shares with them: verbs per chat, kept
// on the organization's server. They never meet, because a hosted chat is never on a paired computer, and they are NOT
// meant to be the same table: the one is the person's own hands and the other is a colleague's.
//
// What they must never do is drift apart without anyone deciding it. A method added to the wire gets a class on both
// sides, and the two lists below say, method by method and in words, where the answer differs on purpose. A method that
// differs and is not listed fails here, and so does a list line that has stopped being true.

// hostedWiderThanPersonal are the methods a member holding the verb may call in a hosted chat while a person's own
// computer, even with typing on, may not. Each differs for a reason of context.
var hostedWiderThanPersonal = map[string]string{
	// Reading: a member reads what the owner chose to share, and nothing else holds the history for them. The personal law
	// keeps the whole transcript on the computer that runs the chat and serves it a page at a time (Transcript.Page).
	remote.MethodTranscript:   "view: the whole transcript is how a member's window opens",
	remote.MethodEarlier:      "view: older history of a shared chat",
	remote.MethodRewindPoints: "view: read-only list",
	remote.MethodPlanTasks:    "view: read-only list",
	remote.MethodPlanSpend:    "view: read-only list",
	remote.MethodTaskPending:  "view: read-only list",
	remote.MethodTitleWatch:   "view: read-only watch",
	remote.MethodDesignWatch:  "view: read-only watch",
	remote.MethodReasoningFor: "view: read-only setting",
	// Sending: a member with run acts as the chat's owner would, by the organization's own design (docs/organizations.md,
	// "The owner owns the chat"). A person's own computer sends words and nothing else.
	remote.MethodSubmitBash:      "run: a member may run a command the chat's own way, as the owner would",
	remote.MethodSubmitFiles:     "run: a member may send files",
	remote.MethodSubmitImage:     "run: a member may send images",
	remote.MethodQuestionReplace: "run: a member may replace a held question's words",
}

// personalWiderThanHosted are the methods a person's own computer may call that a member of a hosted chat may not,
// whatever verb they hold. The hosted default is owner-only, which is the fail-closed answer.
var personalWiderThanHosted = map[string]string{
	remote.MethodObserve:        "a chat's own observer door: not offered to a member, nothing on the hosted side calls it",
	remote.MethodUnobserve:      "the other half of Observe",
	remote.MethodHarness:        "reading the harness list: owner-only on the hosted side until a member needs it",
	remote.MethodStopWork:       "stopping the chat's work is a manage act on the hosted side (its own door), and steer holders interrupt instead",
	remote.MethodConsent:        "a one-turn approval: owner-only on the hosted side unless the organization's policy says answerers may (chats.approvals_by)",
	remote.MethodTranscriptPage: "paging a transcript: nothing on the hosted side calls it yet",
}

func personalClassOf(method string) string {
	switch {
	case remote.GuestMay(method, false):
		return "watch"
	case remote.GuestMay(method, true):
		return "drive"
	}
	return "none"
}

// Contract RT4: the personal and the organization's guest laws differ only where a line above says so.
func TestRT4_TheTwoGuestLawsOnlyDifferWhereSaid(t *testing.T) {
	var unsaidWider, unsaidNarrower, staleWider, staleNarrower []string
	for method, class := range hostedGuestMethods {
		personal := personalClassOf(method)
		hostedMay := class != hostedGuestOwner
		personalMay := personal != "none"
		_, saidWider := hostedWiderThanPersonal[method]
		_, saidNarrower := personalWiderThanHosted[method]
		switch {
		case hostedMay && !personalMay && !saidWider:
			unsaidWider = append(unsaidWider, method+" ("+class+")")
		case !hostedMay && personalMay && !saidNarrower:
			unsaidNarrower = append(unsaidNarrower, method+" ("+personal+")")
		}
		if saidWider && !(hostedMay && !personalMay) {
			staleWider = append(staleWider, method)
		}
		if saidNarrower && !(!hostedMay && personalMay) {
			staleNarrower = append(staleNarrower, method)
		}
	}
	for method := range hostedWiderThanPersonal {
		if _, ok := hostedGuestMethods[method]; !ok {
			staleWider = append(staleWider, method)
		}
	}
	for method := range personalWiderThanHosted {
		if _, ok := hostedGuestMethods[method]; !ok {
			staleNarrower = append(staleNarrower, method)
		}
	}
	for _, list := range []struct {
		name  string
		items []string
	}{
		{"hosted allows, personal refuses, and no line in hostedWiderThanPersonal says why", unsaidWider},
		{"personal allows, hosted keeps for the owner, and no line in personalWiderThanHosted says why", unsaidNarrower},
		{"hostedWiderThanPersonal lines that are no longer true", staleWider},
		{"personalWiderThanHosted lines that are no longer true", staleNarrower},
	} {
		if len(list.items) != 0 {
			sort.Strings(list.items)
			t.Errorf("%s:\n  %s", list.name, strings.Join(slices.Compact(list.items), "\n  "))
		}
	}
}

// Contract RT5: whatever a watch-only viewer of a person's own computers may call, a hosted guest holding only `view`
// may call too, so that watching means the same thing on both sides; and the verbs a method needs on the hosted side
// are never narrower than the personal law's own idea of watching a chat (a view-class method for watch).
func TestRT5_WatchingMeansTheSameOnBothSides(t *testing.T) {
	for method, class := range hostedGuestMethods {
		if personalClassOf(method) != "watch" {
			continue
		}
		if _, said := personalWiderThanHosted[method]; said {
			continue
		}
		if class != acl.VerbView {
			t.Errorf("%s: a watch-only viewer of one's own computers may call it, but a hosted guest needs %q for it", method, class)
		}
	}
}

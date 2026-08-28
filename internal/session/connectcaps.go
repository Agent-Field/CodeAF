package session

// WHAT AN ACCOUNT MAY BE USED FOR, at the two moments a conversation touches it.
//
// internal/connect owns the model and the memory: a handful of sentences per
// service — "read your mail", "send mail as you" — each carrying yes, ask or off,
// and a file with only what somebody actually said in it. This file is the other
// half, the half that wave left to the wiring: the two places those answers have
// to bite, and the one place they are written from a conversation.
//
//   - ARMING. A capability that is off takes its tools OFF THE BELT, and the
//     account's line in `services` stops advertising it. Off is not a refusal at
//     the gate: a refused call costs a turn, teaches the model to try again in
//     different words, and puts a question in front of somebody who already
//     answered it. What is off is a thing the conversation never learns exists.
//   - JUDGING. The gate reads the state again at the moment of the call, so a
//     word changed in the settings sheet while a conversation is open takes
//     effect on the next call and not on the next process. Settings and chat
//     share one process and the store's lock is on the FILE, so that reading
//     costs a file read and nothing else.
//
// ── THE STATE IS READ FRESH, ALWAYS ──
//
// Nothing here caches an answer for the life of a turn, a belt or a session. The
// whole point of the panel is that a person changes their mind in it; a session
// that had read the answers once at arming would go on sending mail for an hour
// after they turned that off, which is the one failure this feature exists to
// prevent.
//
// ── ONE TOOL, TWO CAPABILITIES ──
//
// The hand-written families are one tool, one sentence: gmail_send is "send mail
// as you" and nothing else. A key account is different — it brings ONE tool that
// makes every call, so what it is doing is in the verb rather than in the name.
// A GET reads; every other verb writes at the far end in the person's name. So
// the capability that governs such a call is chosen by the verb here, using the
// same reading internal/approval already takes for its own floor
// ([approval.ActsInThePersonsName]), which is what keeps the panel's word and
// the gate's floor from ever disagreeing about what a POST is.

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// googleFamily is every tool the Google account brings, as a set.
//
// It is the INVERSE of [Agent.familyTools]'s first case and it is written here
// rather than derived from it because the derivation would need an agent and a
// status to build five closures for the sake of five names. A test walks one
// against the other, so the two cannot drift.
var googleFamily = map[string]bool{
	"gmail_search":    true,
	"gmail_read":      true,
	"gmail_send":      true,
	"calendar_list":   true,
	"calendar_create": true,
}

// slackFamily is the inverse of [Agent.familyTools]'s Slack case, held by the
// same test that keeps Google's inverse exact.
var slackFamily = map[string]bool{
	"slack_search":        true,
	"slack_read_thread":   true,
	"slack_list_channels": true,
	"slack_send":          true,
}

// toolService names the account one tool belongs to, and "" for a tool that
// belongs to no account — which is most of the belt.
//
// It is a reading of the NAME rather than a map filled in when a family is
// armed. A map would have to be kept in step with every door onto the belt (the
// tool call, the panel, a resumed session), and a tool whose owner had been
// forgotten would be a tool with no capability at all — which is precisely the
// reading that must never happen by accident.
func toolService(tool string) string {
	tool = strings.TrimSpace(tool)
	if googleFamily[tool] {
		return "google"
	}
	if slackFamily[tool] {
		return "slack"
	}
	// A key account's tool is named after the account: stripe_request. The
	// suffix comes from the package that matches on it, for
	// [serviceRequestName]'s reason.
	if id, cut := strings.CutSuffix(tool, approval.ServiceRequestSuffix); cut && id != "" {
		return id
	}
	return ""
}

// serviceOf is [toolService] with the one case a name cannot answer: a tool the
// ACCOUNT named, armed from what it said it serves (served.go).
//
// The record is asked first and the name second, so a served tool whose name
// happens to end in the key accounts' suffix is still read as what it is. Every
// caller that judges a call takes this reading rather than the bare function,
// which is what keeps a served tool from being a tool with no capability at all.
func (a *Agent) serviceOf(tool string) string {
	if record, served := a.servedRecord(tool); served {
		return record.service
	}
	return toolService(tool)
}

// actsInThePersonsName is [approval.ActsInThePersonsName] with the same case
// added: a served tool acts when the account said it is not read-only, and no
// table written in advance could have held the name.
//
// It exists for the guardian (guardian.go), which may stand in for a person on a
// read and must never stand in for one on something that leaves the machine in
// their name.
func (a *Agent) actsInThePersonsName(tool string, args json.RawMessage) bool {
	if record, served := a.servedRecord(tool); served {
		return record.acts()
	}
	return approval.ActsInThePersonsName(tool, args)
}

// rawCall reports whether a tool is a key account's one raw call, which is the
// tool the verb has to choose a capability for.
func rawCall(tool string) bool {
	id, cut := strings.CutSuffix(strings.TrimSpace(tool), approval.ServiceRequestSuffix)
	return cut && id != ""
}

// capabilityOf names the capability that governs one call, and "" where no
// sentence in this build covers the tool.
//
// AN EMPTY ANSWER IS NOT AN OFF ONE. It means this build has nothing to say
// about that tool, so the tool is judged and armed exactly as it was before this
// file existed — the law [connect.Manager.ToolCapability] states, kept here.
func (a *Agent) capabilityOf(service, tool string, args json.RawMessage) string {
	if a.connect == nil || service == "" {
		return ""
	}
	// A SERVED TOOL CARRIES ITS OWN ANSWER. The account said whether that tool
	// only looks, this build wrote it down when it armed it, and there is no
	// registry entry to read instead: the names were not known when the
	// registry was filled in. It is the verb split again — one generic pair,
	// the half chosen per call — decided by what the account said rather than
	// by a method on a request.
	if record, served := a.servedRecord(tool); served {
		return record.capability
	}
	owner := a.connect.ToolCapability(service, tool)
	if owner == "" {
		return ""
	}
	if rawCall(tool) {
		// The verb chooses. The registry points this tool at the half that
		// acts, because that is the safe answer to read in a hurry; a read is
		// the one case that gets the other half.
		if approval.ActsInThePersonsName(tool, args) {
			return connect.CapabilityAct
		}
		return connect.CapabilityRead
	}
	return owner
}

// capabilityLive reports whether a tool has anything left to do — whether the
// person has left at least one of the capabilities it can run under on.
//
// It is the arming question, and it is asked WITHOUT arguments because there are
// none yet: a key account's tool is armed while either half is on and refuses
// the calls that fall on the off half at the moment they are made.
func (a *Agent) capabilityLive(service, tool string) bool {
	if a.connect == nil || service == "" {
		return true
	}
	if record, served := a.servedRecord(tool); served {
		return a.connect.CapabilityState(service, record.capability) != connect.StateOff
	}
	owner := a.connect.ToolCapability(service, tool)
	if owner == "" {
		return true
	}
	if rawCall(tool) {
		return a.connect.CapabilityState(service, connect.CapabilityRead) != connect.StateOff ||
			a.connect.CapabilityState(service, connect.CapabilityAct) != connect.StateOff
	}
	return a.connect.CapabilityState(service, owner) != connect.StateOff
}

// capabilityAllows reports whether one named capability is anything other than
// off. It is the plain question, for the callers that already know which
// sentence they mean — the raw call's description, which says what each verb is
// worth before a verb has been chosen.
func (a *Agent) capabilityAllows(service, capability string) bool {
	if a.connect == nil {
		return true
	}
	return a.connect.CapabilityState(service, capability) != connect.StateOff
}

// liveTools is the family with the tools the person has turned off left out.
//
// THE OFF ONES ARE ABSENT AND NOT REFUSED, which is the whole of the arming law:
// a tool that is not on the belt is a tool no model plans around, asks about, or
// wastes a turn discovering it cannot use.
func (a *Agent) liveTools(service string, tools []bare.Tool) []bare.Tool {
	if a.connect == nil || len(tools) == 0 {
		return tools
	}
	live := make([]bare.Tool, 0, len(tools))
	for _, tool := range tools {
		if a.capabilityLive(service, tool.Name) {
			live = append(live, tool)
		}
	}
	return live
}

// livePhrases is what a connected account may be used for right now, in the
// person's own sentences and in the order the service declared them.
//
// The second answer is how many sentences there are altogether, so a caller can
// tell "all of it" from "some of it" — a service with nothing taken away should
// go on reading exactly as it read before anybody had an opinion.
func (a *Agent) livePhrases(service string) ([]string, int) {
	if a.connect == nil {
		return nil, 0
	}
	declared := a.connect.Capabilities(service)
	live := make([]string, 0, len(declared))
	for _, capability := range declared {
		if a.connect.CapabilityState(service, capability.ID) == connect.StateOff {
			continue
		}
		if phrase := strings.TrimSpace(capability.Phrase); phrase != "" {
			live = append(live, phrase)
		}
	}
	return live, len(declared)
}

// connectedFor is the tail of one connected account's line in the `services`
// listing: what it may be used for, as of right now.
//
// THE BLURB STANDS WHILE IT IS STILL TRUE. A person who has said nothing gets
// the line the plug wrote about itself, which is the better sentence — it says
// where a key account answers, which is a fact no capability phrase carries. It
// is only when something has been taken away that the blurb becomes a promise
// this build cannot keep, and then it is replaced by what is actually left.
func (a *Agent) connectedFor(service connectStatus) string {
	live, declared := a.livePhrases(service.ID)
	switch {
	case declared == 0, len(live) == declared:
		return strings.TrimSpace(service.Blurb)
	case len(live) == 0:
		return "the person has turned off everything this account can do"
	}
	return "you may " + strings.Join(live, ", ") + ", and nothing else — the person has turned the rest off"
}

// capabilityPhrase is one capability's sentence, or the empty string for a
// capability nobody declared a sentence for.
func (a *Agent) capabilityPhrase(service, capability string) string {
	if a.connect == nil {
		return ""
	}
	for _, declared := range a.connect.Capabilities(service) {
		if strings.EqualFold(declared.ID, capability) {
			return strings.TrimSpace(declared.Phrase)
		}
	}
	return ""
}

// ── the refusal ─────────────────────────────────────────────────────────────

// capabilityRefusal is the sentence for a call the person has taken away, and
// "" for every other call.
//
// ── WHY THIS EXISTS AT ALL, GIVEN THAT OFF MEANS ABSENT ──
//
// The belt is armed once and a person's answer can change afterwards: they open
// the settings sheet in the middle of a conversation and turn sending off while
// the model is holding gmail_send. Arming cannot un-arm — the definition block
// rides at the front of every request and a definition that moves invalidates
// the whole prompt cache behind it (connect.go's append law) — so the tool stays
// on the belt and this is what happens when it is called: nothing runs, and the
// model is told why in a sentence it can act on.
//
// It is checked in the GATE (consent.go) rather than inside each tool, because
// the gate is the one chokepoint every execution passes through (loop.go's
// [Agent.executeTool]) — including the early start that runs a read while the
// response is still streaming. A check written into the five tools would be a
// check the sixth one, added later, would not have.
func (a *Agent) capabilityRefusal(tool string, args json.RawMessage) string {
	service := a.serviceOf(tool)
	if a.connect == nil || service == "" {
		return ""
	}
	capability := a.capabilityOf(service, tool, args)
	if capability == "" {
		return ""
	}
	if a.connect.CapabilityState(service, capability) != connect.StateOff {
		return ""
	}
	return capabilityOffWord(a.serviceName(service), a.capabilityPhrase(service, capability))
}

// capabilityOffWord is what the model reads.
//
// IT IS SAID IN THE PERSON'S WORDS AND NOT IN THE MACHINERY'S. There is no
// capability in it, no state, no policy and no settings path: what happened is
// that somebody decided their account is not to be used this way, and the only
// useful thing a model can do with that is get on without it. It also says not
// to try again, because the shape of this failure — a tool that is on the belt
// and answers no — is exactly the one that invites the same call in different
// words.
func capabilityOffWord(name, phrase string) string {
	line := "The person has turned this off for their " + name + " account"
	if phrase != "" {
		line = "The person has turned off " + strconv.Quote(phrase) + " for their " + name + " account"
	}
	return line + ". Nothing was done. Do the work without it and say so plainly; " +
		"calling again, or calling it another way, will not change their answer."
}

// serviceName is the word a person owns an account by, falling back to the id
// this build knows it as.
func (a *Agent) serviceName(service string) string {
	if status, known := a.service(service); known && strings.TrimSpace(status.Name) != "" {
		return status.Name
	}
	return service
}

// ── the mid-chat answer ─────────────────────────────────────────────────────

// rememberCapability is where a person's "always" lands when the question was
// about one of their accounts.
//
// ONE VOCABULARY, ONE STORE. Somebody who says always in a conversation must
// find that capability set to yes in the settings sheet, and somebody who sets it
// to off in the sheet must stop being asked in the conversation. A second
// remembered-allow list of its own would drift from the panel the first time
// anybody used both, and the drift would be invisible: two rooms, two answers,
// and no way to tell which one the gate is reading.
//
// It reports whether it took the answer. False means this was an ordinary tool
// and the session memo (consent.go) keeps it, exactly as it always did.
//
// ── ONLY THE YES ──
//
// A "no, and stop asking" answer stays on the session memo and is NOT written
// here as off, and the asymmetry is deliberate. Yes and off are not opposites in
// this vocabulary: yes is an allow that lasts, and off TAKES THE TOOL OFF THE
// BELT for every future conversation on this machine. Somebody refusing one
// message is refusing one message; taking a capability away for good is a
// decision with its own control, on a page they can see, and it should not be a
// side effect of a keystroke in a prompt.
func (a *Agent) rememberCapability(tool string, args json.RawMessage, allow bool) bool {
	if !allow || a.connect == nil {
		return false
	}
	service := a.serviceOf(tool)
	if service == "" {
		return false
	}
	capability := a.capabilityOf(service, tool, args)
	if capability == "" {
		return false
	}
	if err := a.connect.SetCapabilityState(service, capability, connect.StateYes); err != nil {
		// The answer could not be written down. The call it was given for still
		// runs — that is what the person just said — and the memo takes the
		// standing half of it for this session rather than losing it entirely.
		return false
	}
	return true
}

// A failure is the one moment where this system is most tempted to hand a
// person its own insides. The work stopped, the only thing anybody has is
// whatever the transport said, and the cheapest thing to do is forward it: node
// ids, wrapper chains, escaped JSON and all. One real room got exactly that,
// twice over —
//
//	⚑ did not finish — node craft-3799~launch: after 3 node call attempts:
//	API error (404): {"error":{"message":"No endpoints found that support tool
//	use. Try disabling \"sh\"...
//
// — which tells the reader nothing they can act on and says four things they
// were never meant to see. What a person is owed when work dies is small and
// fixed: WHAT was being attempted, in their own words; that it stopped; WHY, in
// one plain clause; and what happens next. That is four typed parts, and this
// file is the composition of them.
//
// The transport is not deleted. It is put below the first line, which is where
// the room's own disclosure grammar already folds detail — the headline is what
// a reader sees, the rest is one keystroke away, and nothing has to be believed
// on faith.
package resident

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Failed is a terminal failure in the parts a person is owed. Every field is
// something that is true; an empty one is something nobody knew, and the
// composition simply leaves that clause out rather than inventing it.
type Failed struct {
	// Ask is what was being attempted, in the words the person used. It is
	// never a node id and never a step brief written by the machine.
	Ask string
	// Cause is one plain clause about why it stopped — the innermost thing that
	// actually refused, in its own words. Empty when nothing said.
	Cause string
	// Next is what happens now: a fallback that is already running, or that
	// nothing is and how to ask again.
	Next string
	// Detail is the transport underneath, kept whole and unedited. It rides
	// below the headline so the room folds it.
	Detail string
}

// failureAskLimit bounds the ask inside one sentence. The ask is a phrase in
// this row, not the specification — the whole of it is on the work's own page.
const failureAskLimit = 80

// failureCauseLimit bounds the cause clause for the same reason. What is cut is
// still whole in Detail, one fold away.
const failureCauseLimit = 200

// Room composes the row a person reads.
//
// The headline is one sentence built from the parts, in the order a reader
// needs them: the thing they asked for, that it stopped, why, and what now. The
// transport follows after a blank line, where every long body on this surface is
// already folded behind the disclosure mark.
func (f Failed) Room() string {
	ask := clipLabel(firstLine(f.Ask), failureAskLimit)
	cause := clipLabel(firstLine(f.Cause), failureCauseLimit)

	var line strings.Builder
	if ask != "" {
		line.WriteString("I couldn't finish ")
		line.WriteString(strconvQuote(ask))
	} else {
		line.WriteString("Some of this work stopped before it finished")
	}
	if cause != "" {
		line.WriteString(" — ")
		line.WriteString(strings.TrimRight(cause, "."))
		line.WriteString(".")
	} else {
		line.WriteString(", and nothing was recorded about why.")
	}
	if next := firstLine(f.Next); next != "" {
		line.WriteString(" ")
		line.WriteString(next)
		if !strings.HasSuffix(next, ".") && !strings.HasSuffix(next, "?") {
			line.WriteString(".")
		}
	}

	body := line.String()
	if detail := strings.TrimSpace(f.Detail); detail != "" {
		body += "\n\n" + detail
	}
	return body
}

// strconvQuote is the ask in quotes without strconv's escaping. A person's own
// sentence is not a Go literal, and running it through %q turned an apostrophe
// or a quoted phrase into backslashes on a row that exists to be readable.
func strconvQuote(text string) string {
	return "\"" + text + "\""
}

// FailedNode reads one failed node into the parts a person is owed. next is the
// caller's — only the caller knows whether anything is happening now.
func FailedNode(node store.Node, next string) Failed {
	raw := strings.TrimSpace(node.Error)
	return Failed{
		Ask:    failureAsk(node),
		Cause:  FailureCause(StripNodeStamp(node.ID, raw)),
		Next:   next,
		Detail: raw,
	}
}

// failureAsk is what was being attempted, in the person's words.
//
// Intent first, and for every node rather than only for a job root. A craft
// run's leaves carry the run's provenance, so the intent is the ask on a leaf
// too — and a craft step's brief is exactly the machine-written phrase this row
// must not lead with ("Launch the analysis fan"). A title and then a brief are
// what remain for work that has no recorded ask at all.
func failureAsk(node store.Node) string {
	for _, candidate := range []string{node.Provenance.Intent, node.Title, node.Brief} {
		if line := firstLine(candidate); line != "" {
			return line
		}
	}
	return ""
}

// StripNodeStamp removes the executor's own `node <id>: ` stamp from the front
// of an error.
//
// It is given the id rather than a pattern, which is the whole difference
// between this and scrubbing: it removes one exact string that this system
// wrote, and it cannot touch anything a provider said. A node whose real error
// happened to begin with those bytes is a node whose id is in its own error,
// which is the stamp.
func StripNodeStamp(nodeID, text string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return text
	}
	stamp := "node " + nodeID + ": "
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), stamp))
}

// FailureCause reduces a transport failure to the clause a person can act on.
//
// It is a DECODER, not a cleaner. The only thing it knows how to do is open the
// envelopes this system actually stamps — a JSON error body, and the sentence
// inside it — and stop at the innermost thing that carries its own words.
// Nothing is matched against a list of phrases, nothing is rewritten, and a
// failure it cannot open comes back exactly as it went in. Inventing a cause
// would be worse than forwarding a blob: a blob is unreadable, a wrong
// explanation is believed.
func FailureCause(raw string) string {
	line := firstLine(raw)
	open := strings.IndexByte(line, '{')
	if open < 0 {
		return line
	}
	if message := decodeErrorEnvelope(line[open:]); message != "" {
		return firstSentence(message)
	}
	// The body did not decode, so there is no sentence in it to quote. What is
	// left is the prose this system wrapped it in, which at least names the
	// stage that failed; the bytes themselves stay in Detail where a reader who
	// wants them can open them.
	prefix := strings.TrimSpace(line[:open])
	return strings.TrimSpace(strings.TrimSuffix(prefix, ":"))
}

// errorEnvelope is the shape a provider's error body comes in. Both spellings
// are accepted because both are in the wild: an object under "error" with a
// message, and a bare message at the top level.
type errorEnvelope struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

// decodeErrorEnvelope reads a provider's own sentence out of an error body, or
// says there is none. It tolerates trailing bytes — an error string is often a
// body that was clipped mid-JSON on its way through a bounded field, and half a
// blob is exactly the case a reader most needs saved from.
func decodeErrorEnvelope(body string) string {
	decoder := json.NewDecoder(strings.NewReader(body))
	var envelope errorEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return ""
	}
	if message := strings.TrimSpace(envelope.Error.Message); message != "" {
		return message
	}
	return strings.TrimSpace(envelope.Message)
}

// firstSentence is one clause of somebody else's paragraph. A provider that
// answers with a diagnosis and then three lines of advice about which options to
// change has said the thing a reader needs in its first sentence; the rest is
// still in Detail.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	for index, char := range text {
		if char != '.' && char != '!' && char != '?' {
			continue
		}
		rest := text[index+1:]
		if rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\n") {
			return strings.TrimSpace(text[:index+1])
		}
	}
	return text
}

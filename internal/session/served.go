package session

// THE ACCOUNTS THAT BRING THEIR OWN TOOLS.
//
// Mail and a calendar are hands this build wrote: five tools, five sentences,
// all of them in tools_connect.go where anybody can read them. A key account is
// the other extreme: one raw call, and the service's own documentation is the
// schema. This file is the third shape, and it is neither — the account is asked
// WHAT IT BRINGS, it answers with a list of tools and the shape of each one's
// arguments, and those become the belt.
//
// Nothing here knows the names in advance and nothing here may. The list is the
// service's, read live at the moment an account is picked up, and a service that
// serves five tools today and seven tomorrow is a service this build carries
// seven of tomorrow without a line changing. The seam is
// [connect.Manager.MCPTools] and [connect.Manager.MCPCall]; everything past it —
// which protocol, which endpoint, how a session with the provider is held open —
// is that package's business and never this one's.
//
// ── THE FOUR THINGS THAT ARE DECIDED HERE ──
//
//   - THE NAME. A served tool is armed as `<account>_<its own name>`, folded to
//     the casing a belt can carry. The service's name for it is kept beside the
//     belt's, because that is what a call has to be made with.
//   - THE RECORD. What the belt calls it, what the service calls it, whose
//     account it is, and WHICH CAPABILITY GOVERNS IT. That last one is the whole
//     reason a record exists at all: read and act are told apart by what the
//     service said about the tool, and no reading of a name could recover it.
//   - THE CEILING. A service that serves more than a conversation can carry is
//     not silently trimmed. See [mcpToolCeiling].
//   - WHAT IS LEFT OFF, AND SAID SO. A tool whose arguments cannot be read, or
//     whose name cannot be told from another's, is skipped and NAMED in the
//     reply. A dynamic list must never be able to fail the whole arming: an
//     account that serves nine good tools and one broken one arms nine.
//
// ── WHY THE RECORD IS SAFE HERE AND WOULD NOT BE ANYWHERE ELSE ──
//
// connectcaps.go reads a tool's owner OUT OF ITS NAME and says why: a map would
// have to be kept in step with every door onto the belt, and a tool whose owner
// had been forgotten would be a tool with no capability at all. That reasoning
// stands, and this is the one case it cannot serve — the service's answer about
// a tool is a fact no name carries. So the record is written AT THE ONE DOOR
// ONTO THE BELT, in [Agent.rememberServed], immediately before the append that
// puts the tool there, and it is written for a tool that then fails to arm
// rather than the other way round. A record with no tool is dead weight nobody
// reads; a tool with no record would be a tool judged by nothing.
//
// It lives for the session and is never journaled. A resumed conversation starts
// with a belt built from this binary's own literals and asks the account again
// the first time it needs it, which is exactly what makes a served list that
// changed overnight harmless: the records are rebuilt from what the service says
// today.

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// mcpToolCeiling is how many served tools one account may put on the belt.
//
// ── THE HONEST MECHANISM, AND THE ONES IT WAS CHOSEN OVER ──
//
// Some servers list sixty tools. Every one of them is a schema at the front of
// every request for the rest of the conversation, so carrying them all is a bill
// the person pays on every turn for hands the work never touches.
//
// Trimming to the first thirty-two SILENTLY would be the cheap answer and it is
// the wrong one: the model would plan around a list it was never told was cut,
// and the tool it needed would be missing with no sentence anywhere saying why.
// Sorting the read-only ones to the front first would be the same lie with a
// better-looking list. So the rule is the blunt one: under the ceiling
// everything the account serves arrives, and over it NOTHING does — the reply
// names them all and says to ask again for the ones the work needs, which
// use_service's `tools` argument is for. A model that reads that list picks
// three, and the conversation carries three.
const mcpToolCeiling = 32

// mcpNameLimit is the longest name a served tool may be armed under. It is the
// narrowest of the shapes a provider will accept for a tool name, and a folded
// name is cut to it rather than refused — a long name still names a tool, and
// the service's own name for it is what the call is made with anyway.
const mcpNameLimit = 64

// mcpFactLimit is how many of a served tool's arguments are worth naming in a
// question about it. Two: what it is doing to, and which one — more than that is
// a JSON dump on a line somebody has to read at a glance.
const mcpFactLimit = 2

// mcpFetchCeiling bounds the ask that happens with no tool call waiting on it —
// the account connected from the surface (see [Agent.NoteConnected]). The tool
// path rides the turn's own context and needs none.
const mcpFetchCeiling = 30 * time.Second

// servedTool is one armed tool's record: everything about it that its name on
// the belt cannot carry.
type servedTool struct {
	// service is the account's id and account is the word the person owns it
	// by — "notion" and "Notion".
	service string
	account string
	// tool is the SERVICE'S own name for it, which is what a call is made
	// with. The belt's name is the key this record is found under.
	tool string
	// capability is [connect.CapabilityRead] or [connect.CapabilityAct], as
	// the service said. It is the whole reason this record exists.
	capability string
	// facts are the arguments worth naming in a question about one of these
	// calls, in the order the service put them: its own required list. Empty
	// where the service named none, and the question falls back to reading
	// whatever arguments actually arrived.
	facts []string
}

// acts reports whether this one leaves the machine in the person's name.
func (s servedTool) acts() bool { return s.capability == connect.CapabilityAct }

// rememberServed writes one arming record. It is called under no lock and takes
// armMu itself, immediately before the belt append that arms the same tool — see
// this file's head for why that order and not the other.
func (a *Agent) rememberServed(name string, record servedTool) {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	if a.served == nil {
		a.served = make(map[string]servedTool, mcpToolCeiling)
	}
	a.served[name] = record
}

// servedRecord finds one armed tool's record. The bool is false for every tool
// on this belt that is not a served one, which is almost all of them.
func (a *Agent) servedRecord(tool string) (servedTool, bool) {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	record, served := a.served[strings.TrimSpace(tool)]
	return record, served
}

// ── the arming ──────────────────────────────────────────────────────────────

// armServed asks one account what it brings and puts as much of it on the belt
// as the person's answers and the ceiling allow. It answers with the sentence
// the model reads, never with an error, for [Agent.useService]'s reason.
//
// connected is the first half of that sentence, already written by the caller.
// want is what the model named in use_service's `tools`, empty for everything.
func (a *Agent) armServed(ctx context.Context, service connectStatus, connected, want string) string {
	served, err := a.connect.MCPTools(ctx, service.ID)
	if err != nil {
		return connected + ", but what it brings could not be listed: " + err.Error() +
			" Do the work without it and say so plainly; use_service asks it again."
	}
	if len(served) == 0 {
		// Either this build has no reader for the account, or the account
		// serves nothing. They are the same fact to a model: there is nothing
		// here to call.
		return connected + ", and this build has no tools for it. Do the work without it and say so plainly."
	}

	chosen, missing := chooseServed(service.ID, served, want)
	if len(chosen) == 0 {
		return connected + ", and it serves nothing called " + namedList(missing) + ". What it does serve:\n" +
			wrapList(servedNames(served), servicesLineWidth) +
			"\n\nCall use_service again with tools naming one of these."
	}

	// AND THE OFF ONES ARE NOT HERE, which is connectcaps.go's arming law
	// applied to a list nobody wrote down: a service's tools divide into the
	// ones that only look and the ones that act in the person's name, the
	// person has a word about each half, and a half they have turned off is a
	// half this conversation never learns about.
	live := make([]connect.MCPTool, 0, len(chosen))
	for _, tool := range chosen {
		if a.capabilityAllows(service.ID, connect.MCPToolCapability(tool)) {
			live = append(live, tool)
		}
	}
	if len(live) == 0 {
		return connected + ", and the person has turned off everything it can do. " +
			"Do the work without it and say so plainly; asking again will not change their answer."
	}
	if len(live) > mcpToolCeiling {
		return connected + ", and it serves " + strconv.Itoa(len(live)) +
			" tools of its own — more than one conversation carries at once. Nothing was loaded. " +
			"Call use_service again for " + service.ID +
			" with tools naming the few this work needs, separated by commas. What it serves:\n" +
			wrapList(servedNames(live), servicesLineWidth)
	}

	tools, unreadable, doubled := a.servedBelt(service, live)
	if len(tools) == 0 {
		return connected + ", and none of what it serves could be read: " +
			strings.Join(append(unreadable, doubled...), ", ") +
			". Do the work without it and say so plainly."
	}
	armed, err := a.armFamily(tools)
	if err != nil {
		return connected + ", but its tools could not be loaded: " + err.Error()
	}
	line := connected + ". Loaded for your next turn and every turn after: " + strings.Join(toolNames(tools), ", ") +
		". Their full descriptions are in your tool list from here on."
	if len(armed) == 0 {
		line = "Already loaded — " + strings.Join(toolNames(tools), ", ") +
			" are in your tool list now. Use them; do not ask again."
	}
	return line + servedLeftOff(service.Name, unreadable, doubled) + servedNotServed(service.Name, missing)
}

// servedBelt turns the tools an account serves into the tools a belt carries,
// and says which ones it could not.
//
// NOTHING HERE IS ALLOWED TO FAIL THE ARMING. The shapes arriving are another
// program's, written by somebody who has never seen this one, and the belt is
// built from them at run time rather than from literals in this binary — so a
// schema that does not parse is a tool left off with a sentence, not the
// construction error toolDefinitions raises for a schema written here (tools.go
// says why that one is a build bug).
func (a *Agent) servedBelt(service connectStatus, served []connect.MCPTool) (tools []bare.Tool, unreadable, doubled []string) {
	taken := make(map[string]bool, len(served))
	for _, tool := range served {
		name := servedName(service.ID, tool.Name)
		if name == "" {
			doubled = append(doubled, strconv.Quote(tool.Name))
			continue
		}
		if taken[name] {
			// TWO NAMES THAT FOLD TO ONE ARE ONE NAME HERE, and arming both
			// would hand the model two tools it cannot tell apart. The first
			// stands and the second is named in the reply, so the work can be
			// asked for by the name that did survive.
			doubled = append(doubled, strconv.Quote(tool.Name))
			continue
		}
		schema, ok := servedSchema(tool.Schema)
		if !ok {
			unreadable = append(unreadable, strconv.Quote(tool.Name))
			continue
		}
		taken[name] = true
		a.rememberServed(name, servedTool{
			service:    service.ID,
			account:    service.Name,
			tool:       tool.Name,
			capability: connect.MCPToolCapability(tool),
			facts:      servedFacts(schema),
		})
		tools = append(tools, a.servedBeltTool(service, tool, name, schema))
	}
	return tools, unreadable, doubled
}

// servedBeltTool is one served tool as the belt holds it.
//
// The call goes back out through the seam under the SERVICE'S own name for the
// tool, which the record and this closure both keep: the belt's name is a thing
// this program invented so that a model could say it, and the service has never
// heard of it.
func (a *Agent) servedBeltTool(service connectStatus, served connect.MCPTool, name string, schema json.RawMessage) bare.Tool {
	id, account, remote := service.ID, service.Name, served.Name
	return bare.Tool{
		Name:        name,
		Description: servedDescription(served, account),
		Schema:      schema,
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			if failed := a.serviceStanding(ctx, id, account); failed != "" {
				return failed, true, nil
			}
			text, err := a.connect.MCPCall(ctx, id, remote, args)
			if err != nil {
				return "That call to " + account + " failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

// servedDescription is the one line a model reads about a tool nobody here
// wrote.
//
// IT IS THE SERVICE'S OWN SENTENCE, trimmed and otherwise untouched. Nothing
// here knows what the tool does, and a sentence invented for it would be a guess
// riding at the front of every request. What is added is what the service cannot
// know: that an answer arriving here is bounded, so a model plans a second call
// instead of trusting a cut list — and, for the tools that act, that the person
// stands between the call and the far end.
func servedDescription(served connect.MCPTool, account string) string {
	line := strings.TrimSpace(served.Description)
	if line == "" {
		line = "One of the person's own " + account + " tools, which " + account +
			" calls " + strconv.Quote(strings.TrimSpace(served.Name)) + ". It describes itself nowhere this build can read, " +
			"so follow " + account + "'s own documentation for what it takes."
	}
	if !strings.HasSuffix(line, ".") && !strings.HasSuffix(line, "!") && !strings.HasSuffix(line, "?") {
		line += "."
	}
	line += " It runs in the person's own " + account + " account. Long answers are shortened and say so."
	if connect.MCPToolCapability(served) == connect.CapabilityAct {
		line += " It changes something in their account in their name, and the person is asked before it goes."
	}
	return line
}

// servedSchema is the service's own argument shape, checked well enough that the
// belt can carry it.
//
// A schema that does not parse is a tool this build cannot offer: ridden as
// Parameters:nil it would be a tool the model is told takes no arguments, and
// every call it then made would fail as if the model had written it wrong
// (tools.go). A service that sends NO schema at all is different and is not a
// failure — plenty of tools take nothing — so it gets the shape that says so.
func servedSchema(schema json.RawMessage) (json.RawMessage, bool) {
	text := strings.TrimSpace(string(schema))
	if text == "" || text == "null" {
		return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), true
	}
	var shape map[string]any
	if err := json.Unmarshal([]byte(text), &shape); err != nil {
		return nil, false
	}
	return json.RawMessage(text), true
}

// servedFacts reads which arguments say what a call is doing, out of the shape
// the service declared: its required list, in its own order.
//
// THE SERVICE ALREADY ANSWERED THIS QUESTION. A required argument is the one
// somebody decided a call is meaningless without, which is exactly the one a
// person reading a question about that call needs to see. Where nothing is
// required the question reads whatever arrived instead (see [servedGloss]).
func servedFacts(schema json.RawMessage) []string {
	var shape struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &shape); err != nil {
		return nil
	}
	facts := make([]string, 0, mcpFactLimit)
	for _, field := range shape.Required {
		if field = strings.TrimSpace(field); field == "" {
			continue
		}
		facts = append(facts, field)
		if len(facts) == mcpFactLimit {
			break
		}
	}
	return facts
}

// ── the names ───────────────────────────────────────────────────────────────

// servedName is what the belt calls one served tool: the account's id, an
// underscore, and the service's own name folded to the casing a tool name can
// be written in.
//
// It is PREFIXED for the reason a key account's tool is (tools_connect.go's
// serviceRequestName): two accounts may both serve a tool called `search`, and a
// belt with two of those has two tools the model cannot choose between. The
// prefix is the account, so they are notion_search and linear_search and the
// model reads which account it is asking without a lookup.
//
// An empty answer means the service's name for the tool has nothing in it a
// belt can carry — the caller leaves that tool off and says so.
func servedName(service, tool string) string {
	folded := foldToolName(tool)
	if folded == "" {
		return ""
	}
	name := foldToolName(service) + "_" + folded
	if len(name) > mcpNameLimit {
		name = strings.TrimRight(name[:mcpNameLimit], "_")
	}
	return name
}

// foldToolName writes one name the way a tool name is written: lower case,
// letters, digits and single underscores, with everything else read as a break
// between words.
//
// It is lossy ON PURPOSE and the loss is why every armed tool keeps the
// service's own name beside the belt's: `Create Page`, `create-page` and
// `create.page` all fold to `create_page`, so the fold is a thing to make a
// name out of and never a thing to make a call with.
func foldToolName(name string) string {
	var folded strings.Builder
	folded.Grow(len(name))
	broken := false
	for _, letter := range strings.TrimSpace(name) {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= '0' && letter <= '9':
			if broken && folded.Len() > 0 {
				folded.WriteByte('_')
			}
			broken = false
			folded.WriteRune(letter)
		case letter >= 'A' && letter <= 'Z':
			if broken && folded.Len() > 0 {
				folded.WriteByte('_')
			}
			broken = false
			folded.WriteRune(letter + ('a' - 'A'))
		default:
			broken = true
		}
	}
	return folded.String()
}

// servedNames is what a service calls its own tools, for a list a model reads
// and answers with.
func servedNames(served []connect.MCPTool) []string {
	names := make([]string, 0, len(served))
	for _, tool := range served {
		names = append(names, tool.Name)
	}
	return names
}

// ── the choosing ────────────────────────────────────────────────────────────

// chooseServed narrows a served list to what the model named, and says which of
// the names it gave match nothing.
//
// AN EMPTY ASK IS EVERYTHING, because that is what picking up an account has
// always meant. The order is the SERVICE'S rather than the order the names were
// given in: a list somebody else wrote is in the order they wrote it, and a
// model that named three tools was choosing which ones, not which order.
//
// A name is matched loosely: the service's own spelling, or the belt's. The
// model gets a list of the service's names when an account is over the ceiling
// and reads the belt's names everywhere else, so both are things it can honestly
// have in hand, and a fold on each side is what makes them the same word.
func chooseServed(service string, served []connect.MCPTool, want string) ([]connect.MCPTool, []string) {
	names := commaList(want)
	if len(names) == 0 {
		return served, nil
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[foldToolName(name)] = true
	}
	chosen := make([]connect.MCPTool, 0, len(names))
	found := make(map[string]bool, len(names))
	for _, tool := range served {
		own, belt := foldToolName(tool.Name), foldToolName(servedName(service, tool.Name))
		switch {
		case wanted[own]:
			found[own] = true
		case wanted[belt]:
			found[belt] = true
		default:
			continue
		}
		chosen = append(chosen, tool)
	}
	var missing []string
	for _, name := range names {
		if !found[foldToolName(name)] {
			missing = append(missing, name)
		}
	}
	return chosen, missing
}

// commaList reads one comma-separated argument as the words in it.
func commaList(text string) []string {
	var names []string
	for _, part := range strings.Split(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			names = append(names, part)
		}
	}
	return names
}

// namedList quotes a handful of names for the middle of a sentence.
func namedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}
	return strings.Join(quoted, ", ")
}

// servedLeftOff is the sentence about the tools that did not make it, and the
// empty string when they all did.
//
// IT IS SAID, ALWAYS. A model that asked for an account and got eight of its
// nine tools has to know which one is missing, or it will plan around a hand it
// does not have and spend a turn finding out.
func servedLeftOff(account string, unreadable, doubled []string) string {
	var lines []string
	if len(unreadable) > 0 {
		lines = append(lines, " "+account+" also serves "+strings.Join(unreadable, ", ")+
			", left off because it describes their arguments in a way this build cannot read.")
	}
	if len(doubled) > 0 {
		lines = append(lines, " "+account+" also serves "+strings.Join(doubled, ", ")+
			", left off because the name is already taken here by another of its tools.")
	}
	return strings.Join(lines, "")
}

// servedNotServed names what the model asked for and the account does not have.
func servedNotServed(account string, missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	return " " + account + " serves nothing called " + namedList(missing) + "."
}

// ── the question ────────────────────────────────────────────────────────────

// servedGloss is one served call as a person reads it in a question about it:
// the account, the service's own name for the tool, and what it is being done
// to.
//
// IT NAMES THE ACCOUNT. Every other gloss on this surface is about work on the
// person's own machine, where the machine is not worth saying; this one is about
// somebody else's system, in their name, and "create_page" without "Notion" in
// front of it is a question about nothing they can picture.
func servedGloss(record servedTool, arguments string) string {
	said := []string{record.account, record.tool}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &args); err == nil {
		for _, fact := range servedFactFields(record, args) {
			if value := glossValue(args, fact); value != "" {
				said = append(said, value)
			}
		}
	}
	return clip(strings.Join(said, " · "), hintLimit)
}

// servedFactFields is which arguments a question about this call reads: the ones
// the service said it cannot run without, or — where it said nothing — whatever
// arrived, in an order that does not change between two identical calls.
func servedFactFields(record servedTool, args map[string]json.RawMessage) []string {
	if len(record.facts) > 0 {
		return record.facts
	}
	fields := make([]string, 0, len(args))
	for field := range args {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	if len(fields) > mcpFactLimit {
		fields = fields[:mcpFactLimit]
	}
	return fields
}

package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// The hands that reach a person's own accounts: see what they have, pick one
// up, then read it.
//
// They are two tools and a family rather than one tool per account, and the
// split is the same one tools_search.go makes for a different reason. There, two
// tools because the work goes that way — search, then fetch. Here, TWO STANDING
// TOOLS AND A FAMILY THAT ARRIVES, because a mailbox is not a capability every
// conversation needs: gmail_search on the belt of a session that will never
// touch mail is five schemas at the front of every request, paid for on every
// turn, for hands that are never used. services and use_service are cheap, and
// what they buy is that the expensive part of the belt is only ever carried by
// the conversations that asked for it.
//
// Like the web pair, this whole file is CONDITIONAL: no manager, no tools. See
// [Config.Connect] for why a belt must never carry a promise it cannot keep.

const servicesDescription = "List the accounts the person can connect to this conversation and which of them are connected already, with the address each one is connected as. Hundreds can be connected, so the ones that are not yet are listed by id only; pass filter to search them by name. Cheap; call it when you are about to need something that lives in one of their accounts — mail, a calendar, a billing system — or when they ask what is connected."

const servicesSchemaJSON = `{"type":"object","properties":{"filter":{"description":"Show only the accounts whose name or id contains this, for example stripe or fresh","type":"string"}},"additionalProperties":false}`

const useServiceDescription = "Pick up one account's tools. If it is connected, its tools arrive in your tool list on your next turn. If it is not, the person is asked whether to connect it, and told what they are agreeing to — so call it only when the work actually needs that account, and never twice for the same one. Some accounts serve tools of their own and a few serve too many to carry at once: that answer lists them and you call again with tools naming the ones the work needs. Use services first if you do not know the id."

const useServiceSchemaJSON = `{"type":"object","properties":{"service":{"type":"string","description":"The id of the account, as services lists it — for example google"},"tools":{"type":"string","description":"Only these of the tools the account serves, by name, separated by commas. Leave it out to take everything it brings; an account with too many to carry says so and lists them"}},"required":["service"],"additionalProperties":false}`

const gmailSearchDescription = "Search the person's mail and get back a numbered list of matching messages: who each one is from, when it arrived, and its subject. Uses Gmail's own search syntax (from:, subject:, has:attachment, newer_than:7d). Follow it with gmail_read on the ids worth opening."

const gmailSearchSchemaJSON = `{"type":"object","properties":{"query":{"type":"string","description":"What to look for, in Gmail's search syntax"},"max":{"type":"number","description":"How many messages to return (default: 10)"}},"required":["query"],"additionalProperties":false}`

const gmailReadDescription = "Read one message whole: the sender, the date, the subject, and the body with the markup stripped. The id is one gmail_search returned. Long messages are truncated and say so."

const gmailReadSchemaJSON = `{"type":"object","properties":{"id":{"type":"string","description":"The id of the message, as gmail_search returned it"}},"required":["id"],"additionalProperties":false}`

const calendarListDescription = "List the person's calendar events between two days, inclusive, one line each: when, how long, and what it is called. Use it before answering anything about their availability, and never guess at a schedule you have not read."

const calendarListSchemaJSON = `{"type":"object","properties":{"from":{"type":"string","description":"The first day, as YYYY-MM-DD"},"to":{"type":"string","description":"The last day, inclusive, as YYYY-MM-DD"}},"required":["from","to"],"additionalProperties":false}`

const gmailSendDescription = "Send one message from the person's own address. It leaves as them, it reaches the people you name, and nothing can call it back — so write what they would have written, and expect them to be asked before it goes. Several recipients are one comma-separated string. Say afterwards what went and to whom."

const gmailSendSchemaJSON = `{"type":"object","properties":{"to":{"type":"string","description":"Who it goes to: one address, or several separated by commas"},"cc":{"type":"string","description":"Who is copied, separated by commas"},"subject":{"type":"string","description":"The subject line"},"body":{"type":"string","description":"The message itself, as plain text"}},"required":["to","subject","body"],"additionalProperties":false}`

const calendarCreateDescription = "Put one event on the person's calendar. Anyone you name as an attendee is invited by Google there and then, so this reaches other people and the person is asked before it happens. Times are full timestamps (2026-08-18T09:00:00Z) or a bare YYYY-MM-DD for something that takes the whole day; leave the end out for an hour-long meeting or a single day. Read the calendar first when the time has to be free."

const calendarCreateSchemaJSON = `{"type":"object","properties":{"title":{"type":"string","description":"What the event is called"},"start":{"type":"string","description":"When it starts, as 2026-08-18T09:00:00Z or as YYYY-MM-DD for a whole day"},"end":{"type":"string","description":"When it ends, in the same shape as start (optional)"},"attendees":{"type":"string","description":"Who to invite: addresses separated by commas (optional)"},"location":{"type":"string","description":"Where it is (optional)"},"description":{"type":"string","description":"What to say in the invitation (optional)"}},"required":["title","start"],"additionalProperties":false}`

// gmailSearchDefaultMax is what a model that asks for no number gets. The helper
// bounds the ask itself — this is the sensible default, not the ceiling.
const gmailSearchDefaultMax = 10

// connectTools is the accounts half of the belt, and it is CONDITIONAL for the
// reason searchTools is: a hub that is not there contributes no tool at all
// rather than a tool that answers "nothing is configured".
func (a *Agent) connectTools() []bare.Tool {
	if a.connect == nil {
		return nil
	}
	return []bare.Tool{a.servicesTool(), a.useServiceTool()}
}

func (a *Agent) servicesTool() bare.Tool {
	return bare.Tool{
		Name:        "services",
		Description: servicesDescription,
		Schema:      json.RawMessage(servicesSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Filter string `json:"filter"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			return renderServices(a.connect.Services(), parsed.Filter, a.connectedFor), false, nil
		},
	}
}

// servicesLineWidth is how wide the block of ids is wrapped. It is a reading
// width, not a terminal width: the reader is a model, and a list that runs to
// one enormous line is harder for it to hold than a list in rows.
const servicesLineWidth = 76

// renderServices is the live list as the model reads it.
//
// ── THE CONNECTED ONES IN FULL, THE REST BY NAME ──
//
// There are a couple of hundred accounts on this list and one of them matters
// to the conversation. A line each — id, name, and a sentence about what
// connecting it would buy — would be a page and a half of a model's context
// spent, every time it wondered what was connected, on services nobody has
// mentioned. So the ones already connected are written out, because those are
// the ones the work is actually about, and the rest are a block of ids with a
// filter to search them by.
//
// THE EMPTINESS LAW: a build with nothing to offer says so in one sentence
// rather than returning a heading over a blank list, and a filter that matches
// nothing says THAT rather than pretending the list is empty.
//
// ── THE CONNECTED LINE IS WRITTEN FROM THE LIVE ANSWERS ──
//
// `connected` answers what one account may be used for RIGHT NOW, which is not
// always what the plug's own blurb says: Google's line promises reading and
// sending mail, and somebody who has turned sending off has a build that cannot
// send. A listing that repeated the blurb would be advertising a hand the model
// does not have, and the turn that discovers it is a turn spent. So the tail of
// a connected line comes from `describe`, which is [Agent.connectedFor] in a
// running session — and a nil one is the blurb, which is what a caller with no
// capabilities behind it honestly has.
func renderServices(services []connectStatus, filter string, describe func(connectStatus) string) string {
	if len(services) == 0 {
		return "No accounts can be connected to this conversation."
	}
	filter = strings.ToLower(strings.TrimSpace(filter))

	var connected []string
	var available []string
	for _, service := range services {
		if filter != "" && !strings.Contains(strings.ToLower(service.ID+" "+service.Name), filter) {
			continue
		}
		if service.Connected {
			line := service.ID + " — " + service.Name + ", connected"
			if account := strings.TrimSpace(service.Account); account != "" {
				line += " as " + account
			}
			tail := strings.TrimSpace(service.Blurb)
			if describe != nil {
				tail = strings.TrimSpace(describe(service))
			}
			if tail != "" {
				line += ": " + tail
			}
			connected = append(connected, line)
			continue
		}
		available = append(available, service.ID)
	}

	if len(connected) == 0 && len(available) == 0 {
		return "No account here matches " + strconv.Quote(filter) +
			". Call services with no filter to see what is connected, or with a shorter one."
	}

	var blocks []string
	if len(connected) > 0 {
		blocks = append(blocks, "Connected:\n"+strings.Join(connected, "\n"))
	}
	if len(available) > 0 {
		heading := "Not connected yet (" + strconv.Itoa(len(available)) + "), by id:"
		blocks = append(blocks, heading+"\n"+wrapList(available, servicesLineWidth))
	}
	trailer := "Call use_service with one of these ids to pick up its tools.\n" +
		"An account that is not connected yet is connected by the person, when you ask for it."
	if filter == "" && len(available) > 0 {
		trailer += "\nPass filter to search this list by name."
	}
	return strings.Join(blocks, "\n\n") + "\n\n" + trailer
}

// wrapList lays a run of short words out as comma-separated rows.
func wrapList(items []string, width int) string {
	var rows []string
	row := ""
	for index, item := range items {
		piece := item
		if index < len(items)-1 {
			piece += ","
		}
		switch {
		case row == "":
			row = piece
		case len(row)+1+len(piece) <= width:
			row += " " + piece
		default:
			rows = append(rows, row)
			row = piece
		}
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (a *Agent) useServiceTool() bare.Tool {
	return bare.Tool{
		Name:        "use_service",
		Description: useServiceDescription,
		Schema:      json.RawMessage(useServiceSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Service string `json:"service"`
				Tools   string `json:"tools"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			id := strings.TrimSpace(parsed.Service)
			if id == "" {
				return "Invalid arguments: service is required", true, nil
			}
			return a.useService(ctx, id, parsed.Tools)
		},
	}
}

// useService is the whole of what picking up an account means: find it, connect
// it if it is not connected, then arm what it brings.
//
// It never returns a Go error. Every way this can go wrong — an id nobody has, a
// person who said no, a person who said nothing, an attempt that broke — is a
// TOOL ERROR the model can read and act on, exactly as a failed search is
// (tools_search.go). The one thing it must never do is end the turn over an
// account.
//
// want is what the model named in `tools`, and it is EMPTY almost always: it
// exists for the one account that serves more tools than a conversation carries
// (served.go's ceiling), where picking up everything is not on offer and the
// model is handed the list to choose from. It is ignored by every account whose
// tools this build wrote.
func (a *Agent) useService(ctx context.Context, id, want string) (string, bool, error) {
	if a.connect == nil {
		// Unreachable from the belt — the tool is not on it without a hub — and
		// written anyway, so that a call site added later cannot turn the nil law
		// into a panic.
		return "No accounts can be connected to this conversation.", true, nil
	}
	service, known := a.service(id)
	if !known {
		// The WHOLE list, not one filtered by the id that was wrong: a model
		// that guessed a name needs to see what there is, and filtering by the
		// guess is exactly the search that has already failed.
		return "No account with the id " + strconv.Quote(id) + ". " +
			renderServices(a.connect.Services(), "", a.connectedFor), true, nil
	}
	if service.Connected {
		return a.armService(ctx, service, "", want), false, nil
	}
	account, failed := a.connectService(ctx, service)
	if failed != "" {
		return failed, true, nil
	}
	// The account may know more about itself now than it did a moment ago —
	// where it answers, for one — so the arming reads it fresh rather than
	// describing a tool from what was true before the person answered.
	if fresh, known := a.service(id); known {
		service = fresh
	}
	return a.armService(ctx, service, account, want), false, nil
}

// connectService is the whole browser round-trip: the question, the page, the
// wait, and the reports on the way. It answers with the account that was
// connected, or with the sentence to hand the model when nothing was — never
// with a Go error, for [Agent.useService]'s reason.
//
// It is separate from the tool because the same trip is made from two places:
// picking an account up, and finding mid-work that the account no longer stands
// (see [Agent.serviceClient]). Both must ask in exactly the same words.
func (a *Agent) connectService(ctx context.Context, service connectStatus) (account string, failed string) {
	answer, err := a.askConnect(ctx, service)
	switch {
	case errors.Is(err, errNobodyWatching):
		// Nobody is there to say yes. It is the same answer consent.go gives a
		// headless run and for the same reason: a question with no reader is a
		// hang, not a safeguard.
		return "", "Connecting " + service.Name + " needs the person to say yes, and nobody is watching this " +
			"conversation. Do what you can without their " + service.Name + " account and say plainly that you could not reach it."
	case err != nil:
		return "", "The turn ended before the person answered about connecting " + service.Name + "."
	case !answer.approved:
		return "", "The person did not agree to connect " + service.Name + ". Do the work without it and say so plainly; do not ask again this turn."
	}

	// An account opened with a key has no page to send anybody to: the person
	// has already done the only step there is, and the whole of what is left is
	// one call that either works or says why not.
	if service.keyed() {
		status, err := a.connect.ConnectKey(ctx, service.ID, answer.key)
		if err != nil {
			a.sendConnect(Event{Kind: EventConnectDone, Service: service.ID, Failed: true})
			return "", "Connecting " + service.Name + " did not work: " + err.Error() +
				" Say so plainly; use_service asks the person again."
		}
		a.sendConnect(Event{Kind: EventConnectDone, Service: service.ID, Account: status.Account})
		return status.Account, ""
	}

	url, wait, err := a.connect.BeginAuth(ctx, service.ID, answer.key)
	if err != nil {
		a.sendConnect(Event{Kind: EventConnectDone, Service: service.ID, Failed: true})
		return "", "Connecting " + service.Name + " did not work: " + err.Error()
	}
	a.sendConnect(Event{Kind: EventConnectAuth, Service: service.ID, AuthURL: url})

	// The ceiling is on a context of this call's own, so the wait ends on the
	// turn being interrupted OR on the person never finishing — and the two are
	// answered in different words below, because "you interrupted me" and "the
	// account did not connect" are different facts.
	waitCtx, cancel := context.WithTimeout(ctx, connectAuthCeiling)
	defer cancel()
	status, err := wait(waitCtx)
	if err != nil {
		a.sendConnect(Event{Kind: EventConnectDone, Service: service.ID, Failed: true})
		if ctx.Err() != nil {
			return "", "The turn ended before " + service.Name + " finished connecting."
		}
		return "", service.Name + " did not finish connecting: " + err.Error()
	}
	a.sendConnect(Event{Kind: EventConnectDone, Service: service.ID, Account: status.Account})
	return status.Account, ""
}

// armService puts one service's family on the belt and says what arrived.
//
// The reply names the TOOLS rather than the account, because the next turn's
// tool list is what the model will actually be holding — and it says WHEN,
// because the belt it is reading right now does not have them yet and a model
// that calls gmail_search this turn gets an unknown-tool error for its trouble.
func (a *Agent) armService(ctx context.Context, service connectStatus, account, want string) string {
	connected := connectedLine(service.Name, account)
	// AN ACCOUNT THIS BUILD HAS NO FAMILY FOR IS ASKED WHAT IT BRINGS, and that
	// question is the whole of served.go. It is asked of the accounts that are
	// left over rather than of a list of ids written down here, because a list
	// of ids is a thing that goes stale the week somebody adds the sixth
	// service: what this build actually knows is which accounts it wrote tools
	// for, and every other connected account is one whose tools are its own to
	// name. An account that serves nothing answers nothing, and the sentence
	// below is the one it gets.
	if a.servesItsOwn(service) {
		return a.armServed(ctx, service, connected, want)
	}
	tools := a.familyTools(service)
	if len(tools) == 0 {
		// Connected, and nothing in this build reads it. Honest, and short: a
		// model told this stops planning around the account instead of calling
		// again in different words.
		return connected + ", and this build has no tools for it. Do the work without it and say so plainly."
	}
	// AND THE OFF ONES ARE NOT HERE. What a person has taken away is absent from
	// the belt and absent from this sentence, so the model is never told about a
	// hand it does not have (connectcaps.go).
	tools = a.liveTools(service.ID, tools)
	if len(tools) == 0 {
		// Connected, and the person has turned everything it can do off. It is a
		// different fact from the one above and it gets a different sentence:
		// this build HAS the tools, and they are not on offer.
		return connected + ", and the person has turned off everything it can do. " +
			"Do the work without it and say so plainly; asking again will not change their answer."
	}
	armed, err := a.armFamily(tools)
	if err != nil {
		return connected + ", but its tools could not be loaded: " + err.Error()
	}
	if len(armed) == 0 {
		return "Already loaded — " + strings.Join(toolNames(tools), ", ") +
			" are in your tool list now. Use them; do not ask again."
	}
	return connected + ". Loaded for your next turn and every turn after: " + strings.Join(armed, ", ") +
		". Their full descriptions are in your tool list from here on."
}

func toolNames(tools []bare.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// sendConnect puts one report on the turn's fan-out, if there is a turn. A
// connect attempt runs inside a tool call, so there almost always is; the check
// is for the paths that reach here from outside one.
func (a *Agent) sendConnect(event Event) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		hub.send(event)
	}
}

// ── the armed families ──────────────────────────────────────────────────────

// familyTools is what one account brings.
//
// The hand-written families are a SWITCH over ids rather than a registry because
// the code owns that grouping: those are its own tools, grouped the way it wrote
// them. Under the switch is the general answer, and it is general because it has
// to be: a couple of hundred accounts open on a key, no two of them agree on
// what a contact is or how a page is asked for, and a pair of hand-written tools
// each would be hundreds of files nobody can keep true. So a keyed account
// brings ONE tool, named after itself, that makes the calls its own
// documentation describes.
//
// An account this build has no reader for answers nothing, and armService says
// so in a sentence.
func (a *Agent) familyTools(service connectStatus) []bare.Tool {
	switch strings.ToLower(strings.TrimSpace(service.ID)) {
	case "google":
		return []bare.Tool{
			a.gmailSearchTool(), a.gmailReadTool(), a.gmailSendTool(),
			a.calendarListTool(), a.calendarCreateTool(),
		}
	}
	if service.keyed() {
		return []bare.Tool{a.serviceRequestTool(service)}
	}
	return nil
}

// servesItsOwn reports that an account's tools are the ACCOUNT'S to name rather
// than this build's: it is connected in a browser, and there is no family here
// written for it.
//
// It is a question about what is missing on purpose. The hand-written families
// are a switch over ids and the keyed accounts are one raw call each; what falls
// through both is a service that answers for itself, and asking it is strictly
// better than the sentence this build used to end on ("no tools for it") because
// an account that has nothing to serve still ends on exactly that sentence.
func (a *Agent) servesItsOwn(service connectStatus) bool {
	return a.connect != nil && !service.keyed() && len(a.familyTools(service)) == 0
}

// serviceRequestName is what one keyed account's tool is called: its own id and
// the suffix internal/approval matches on, so that the consent floor and the
// belt cannot drift apart on which tools these are.
func serviceRequestName(id string) string {
	return strings.TrimSpace(id) + approval.ServiceRequestSuffix
}

// serviceRequestSchemaJSON is the same four fields for every account.
const serviceRequestSchemaJSON = `{"type":"object","properties":{"method":{"type":"string","description":"get, post, put, patch or delete (default: get)"},"path":{"type":"string","description":"The path under the address in this tool's description, for example /v1/customers"},"query":{"type":"string","description":"What goes after the question mark, for example limit=10&status=open (optional)"},"body":{"type":"string","description":"What to send, usually JSON (optional, and never on a get)"}},"required":["path"],"additionalProperties":false}`

// serviceRequestDescription is the one line a model reads about one account.
//
// IT NAMES THE ADDRESS. The path is relative and nothing here knows the
// service's own shapes, so the address is the one fact that lets a model line up
// what it already knows about a service with what it is about to call.
//
// ── AND IT NAMES ONLY THE HALF THE PERSON LEFT ON ──
//
// This one tool is two capabilities: reading, and changing something at the far
// end in the person's name (connectcaps.go). Where one of the two is off the
// tool is still armed for the other, and the description says so — a sentence
// promising that `get` reads, to a model whose reads will all be refused, buys
// exactly one wasted call and a confused turn.
func serviceRequestDescription(service connectStatus, reads, acts bool) string {
	line := "Make one call to the person's own " + service.Name + " account."
	if address := strings.TrimSpace(service.Address); address != "" {
		line += " Paths are relative to " + address + " — for example /v1/things."
	}
	line += " Follow " + service.Name + "'s own published documentation for paths, parameters and shapes; " +
		"nothing here knows them, so guessing costs a failed call. Long answers are shortened and say so. "
	switch {
	case reads && acts:
		return line + "get reads; post, put, patch and delete change something in their account, " +
			"and the person is asked before one goes."
	case reads:
		return line + "get reads, and that is all this account may be used for: " +
			"the person has turned off changing anything in it, so post, put, patch and delete will not run."
	default:
		return line + "post, put, patch and delete change something in their account, and the person is asked " +
			"before one goes. Reading is turned off for this account, so get will not run."
	}
}

func (a *Agent) serviceRequestTool(service connectStatus) bare.Tool {
	id, name := service.ID, service.Name
	return bare.Tool{
		Name: serviceRequestName(id),
		// The two halves are read HERE, when the tool is built, which is the
		// moment the account is picked up. A person who changes their mind
		// afterwards changes what the call DOES (the gate answers that on every
		// call); the sentence in front of the model is the one it was armed
		// with, because a definition that is rewritten in place invalidates the
		// prompt cache for the whole conversation behind it (connect.go).
		Description: serviceRequestDescription(service,
			a.capabilityAllows(id, connect.CapabilityRead),
			a.capabilityAllows(id, connect.CapabilityAct)),
		Schema: json.RawMessage(serviceRequestSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Method string `json:"method"`
				Path   string `json:"path"`
				Query  string `json:"query"`
				Body   string `json:"body"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			if strings.TrimSpace(parsed.Path) == "" {
				return "Invalid arguments: path is required", true, nil
			}
			if failed := a.serviceStanding(ctx, id, name); failed != "" {
				return failed, true, nil
			}
			text, err := a.connect.Request(ctx, id, parsed.Method, parsed.Path, parsed.Query, parsed.Body)
			if err != nil {
				return "That call to " + name + " failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

func (a *Agent) gmailSearchTool() bare.Tool {
	return bare.Tool{
		Name:        "gmail_search",
		Description: gmailSearchDescription,
		Schema:      json.RawMessage(gmailSearchSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Query string `json:"query"`
				Max   *int   `json:"max"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			query := strings.TrimSpace(parsed.Query)
			if query == "" {
				return "Invalid arguments: query is required", true, nil
			}
			limit := gmailSearchDefaultMax
			if parsed.Max != nil && *parsed.Max > 0 {
				limit = *parsed.Max
			}
			client, failed := a.serviceClient(ctx, "google", "Google")
			if failed != "" {
				return failed, true, nil
			}
			text, err := connect.GmailSearch(ctx, client, query, limit)
			if err != nil {
				return "Searching your mail failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

func (a *Agent) gmailReadTool() bare.Tool {
	return bare.Tool{
		Name:        "gmail_read",
		Description: gmailReadDescription,
		Schema:      json.RawMessage(gmailReadSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			id := strings.TrimSpace(parsed.ID)
			if id == "" {
				return "Invalid arguments: id is required", true, nil
			}
			client, failed := a.serviceClient(ctx, "google", "Google")
			if failed != "" {
				return failed, true, nil
			}
			text, err := connect.GmailRead(ctx, client, id)
			if err != nil {
				return "Opening that message failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

// ── the two hands that act ──────────────────────────────────────────────────
//
// THESE TWO LEAVE THE MACHINE IN THE PERSON'S NAME, and that is the whole
// difference between them and the three above. A search that was not wanted
// costs a moment; a message that was not wanted has been read by somebody else
// by the time anyone notices. So they are ASKED ABOUT BY DEFAULT, and not by a
// check written here: the names are in internal/approval's table of tools a
// blanket allow cannot vouch for, so the ordinary gate (consent.go) puts the
// question — with the recipient and the subject in it (loop.go's gloss) — and
// the person's own allow rule, and their "always" answer, go on working exactly
// as they do for every other tool.

func (a *Agent) gmailSendTool() bare.Tool {
	return bare.Tool{
		Name:        "gmail_send",
		Description: gmailSendDescription,
		Schema:      json.RawMessage(gmailSendSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				To      string `json:"to"`
				Cc      string `json:"cc"`
				Subject string `json:"subject"`
				Body    string `json:"body"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			if strings.TrimSpace(parsed.To) == "" {
				return "Invalid arguments: to is required", true, nil
			}
			if strings.TrimSpace(parsed.Subject) == "" && strings.TrimSpace(parsed.Body) == "" {
				return "Invalid arguments: a message needs a subject or a body", true, nil
			}
			client, failed := a.serviceClient(ctx, "google", "Google")
			if failed != "" {
				return failed, true, nil
			}
			text, err := connect.GmailSend(ctx, client, parsed.To, parsed.Cc, parsed.Subject, parsed.Body)
			if err != nil {
				return "Sending that message failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

func (a *Agent) calendarCreateTool() bare.Tool {
	return bare.Tool{
		Name:        "calendar_create",
		Description: calendarCreateDescription,
		Schema:      json.RawMessage(calendarCreateSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Title       string `json:"title"`
				Start       string `json:"start"`
				End         string `json:"end"`
				Attendees   string `json:"attendees"`
				Location    string `json:"location"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			if strings.TrimSpace(parsed.Title) == "" {
				return "Invalid arguments: title is required", true, nil
			}
			if strings.TrimSpace(parsed.Start) == "" {
				return "Invalid arguments: start is required, as 2026-08-18T09:00:00Z or YYYY-MM-DD", true, nil
			}
			client, failed := a.serviceClient(ctx, "google", "Google")
			if failed != "" {
				return failed, true, nil
			}
			text, err := connect.CalendarCreate(ctx, client,
				parsed.Title, parsed.Start, parsed.End, parsed.Attendees, parsed.Location, parsed.Description)
			if err != nil {
				return "Putting that on your calendar failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

func (a *Agent) calendarListTool() bare.Tool {
	return bare.Tool{
		Name:        "calendar_list",
		Description: calendarListDescription,
		Schema:      json.RawMessage(calendarListSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			from, to := strings.TrimSpace(parsed.From), strings.TrimSpace(parsed.To)
			if from == "" || to == "" {
				return "Invalid arguments: from and to are both required, as YYYY-MM-DD", true, nil
			}
			client, failed := a.serviceClient(ctx, "google", "Google")
			if failed != "" {
				return failed, true, nil
			}
			text, err := connect.CalendarList(ctx, client, from, to)
			if err != nil {
				return "Reading your calendar failed: " + err.Error(), true, nil
			}
			return text, false, nil
		},
	}
}

// serviceClient is the one way an armed tool reaches an account. The second
// return is the tool result to hand back when there is no client, and it is
// EMPTY when there is one — the caller reads it as "did this fail", so a failure
// can never be mistaken for a client nobody checked.
//
// AN ACCOUNT THAT NO LONGER STANDS IS A QUESTION, NOT AN ERROR. The person
// disconnected it, or — far more often — they signed in when aforge could only
// read their mail and it is about to send some, so what they agreed to no longer
// covers the work (internal/connect keeps that record). Either way the honest
// next move is the sign-in they already know, in the same words use_service
// asks it, rather than an error that costs a turn before the model asks the
// same question itself. Everything past that point is answered plainly: an
// attempt that fails, a person who says no, a service having a bad minute.
func (a *Agent) serviceClient(ctx context.Context, id, name string) (*http.Client, string) {
	if failed := a.serviceStanding(ctx, id, name); failed != "" {
		return nil, failed
	}
	client, err := a.connect.Client(ctx, id)
	if err != nil {
		return nil, name + " is not connected any more: " + err.Error() +
			". Say so plainly; use_service asks the person to connect it again."
	}
	return client, ""
}

// serviceStanding makes sure the account still stands before a tool leans on
// it, asking the person again where it does not. It answers with the sentence to
// hand the model when it could not be put right, and with NOTHING when the
// account is ready — see [Agent.serviceClient] for the law it holds.
func (a *Agent) serviceStanding(ctx context.Context, id, name string) string {
	if a.connect == nil {
		return name + " is not reachable from this conversation."
	}
	if a.connect.Connected(id) {
		return ""
	}
	service, known := a.service(id)
	if !known {
		return name + " is not reachable from this conversation."
	}
	if _, failed := a.connectService(ctx, service); failed != "" {
		return failed
	}
	return ""
}

// errNobodyWatching is askConnect's one error that is not the turn ending: there
// is no surface subscribed to this session's events, so the question would be
// asked into an empty room.
var errNobodyWatching = errors.New("session: nobody is watching this session")

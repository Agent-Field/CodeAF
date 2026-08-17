package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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

const servicesDescription = "List the accounts the person can connect to this conversation and which of them are connected already, with the address each one is connected as. Cheap; call it when you are about to need something that lives in one of their accounts — mail, a calendar — or when they ask what is connected."

const servicesSchemaJSON = `{"type":"object","properties":{},"additionalProperties":false}`

const useServiceDescription = "Pick up one account's tools. If it is connected, its tools arrive in your tool list on your next turn. If it is not, the person is asked whether to connect it, and told what they are agreeing to — so call it only when the work actually needs that account, and never twice for the same one. Use services first if you do not know the id."

const useServiceSchemaJSON = `{"type":"object","properties":{"service":{"type":"string","description":"The id of the account, as services lists it — for example google"}},"required":["service"],"additionalProperties":false}`

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
			return renderServices(a.connect.Services()), false, nil
		},
	}
}

// renderServices is the live list as the model reads it. A connected service
// says who it is connected as; an unconnected one says what connecting it would
// buy and how to ask.
//
// THE EMPTINESS LAW: a build with nothing to offer says so in one sentence
// rather than returning a heading over a blank list.
func renderServices(services []connectStatus) string {
	if len(services) == 0 {
		return "No accounts can be connected to this conversation."
	}
	lines := make([]string, 0, len(services))
	for _, service := range services {
		switch {
		case service.Connected && strings.TrimSpace(service.Account) != "":
			lines = append(lines, service.ID+" — "+service.Name+", connected as "+service.Account)
		case service.Connected:
			lines = append(lines, service.ID+" — "+service.Name+", connected")
		default:
			line := service.ID + " — " + service.Name + ", available"
			if blurb := strings.TrimSpace(service.Blurb); blurb != "" {
				line += ": " + blurb
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n") +
		"\n\nCall use_service with one of these ids to pick up its tools. " +
		"An account that is not connected yet is connected by the person, when you ask for it."
}

func (a *Agent) useServiceTool() bare.Tool {
	return bare.Tool{
		Name:        "use_service",
		Description: useServiceDescription,
		Schema:      json.RawMessage(useServiceSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Service string `json:"service"`
			}
			if err := json.Unmarshal(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			id := strings.TrimSpace(parsed.Service)
			if id == "" {
				return "Invalid arguments: service is required", true, nil
			}
			return a.useService(ctx, id)
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
func (a *Agent) useService(ctx context.Context, id string) (string, bool, error) {
	if a.connect == nil {
		// Unreachable from the belt — the tool is not on it without a hub — and
		// written anyway, so that a call site added later cannot turn the nil law
		// into a panic.
		return "No accounts can be connected to this conversation.", true, nil
	}
	service, known := a.service(id)
	if !known {
		return "No account with the id " + strconv.Quote(id) + ". " +
			renderServices(a.connect.Services()), true, nil
	}
	if service.Connected {
		return a.armService(service, ""), false, nil
	}
	account, failed := a.connectService(ctx, service)
	if failed != "" {
		return failed, true, nil
	}
	return a.armService(service, account), false, nil
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
	approved, err := a.askConnect(ctx, service)
	switch {
	case errors.Is(err, errNobodyWatching):
		// Nobody is there to say yes. It is the same answer consent.go gives a
		// headless run and for the same reason: a question with no reader is a
		// hang, not a safeguard.
		return "", "Connecting " + service.Name + " needs the person to say yes, and nobody is watching this " +
			"conversation. Do what you can without their " + service.Name + " account and say plainly that you could not reach it."
	case err != nil:
		return "", "The turn ended before the person answered about connecting " + service.Name + "."
	case !approved:
		return "", "The person did not agree to connect " + service.Name + ". Do the work without it and say so plainly; do not ask again this turn."
	}

	url, wait, err := a.connect.BeginAuth(ctx, service.ID)
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
func (a *Agent) armService(service connectStatus, account string) string {
	connected := service.Name + " is connected"
	if account = strings.TrimSpace(account); account != "" {
		connected += " as " + account
	}
	tools := a.familyTools(service.ID)
	if len(tools) == 0 {
		// Connected, and nothing in this build reads it. Honest, and short: a
		// model told this stops planning around the account instead of calling
		// again in different words.
		return connected + ", and this build has no tools for it. Do the work without it and say so plainly."
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

// familyTools is what one account brings, and it is a SWITCH over ids rather
// than a registry because the code owns this grouping: these are its own tools,
// grouped the way it wrote them. An account this build has no reader for
// answers nothing, and armService says so in a sentence.
func (a *Agent) familyTools(id string) []bare.Tool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "google":
		return []bare.Tool{
			a.gmailSearchTool(), a.gmailReadTool(), a.gmailSendTool(),
			a.calendarListTool(), a.calendarCreateTool(),
		}
	}
	return nil
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
	if a.connect == nil {
		return nil, name + " is not reachable from this conversation."
	}
	if !a.connect.Connected(id) {
		service, known := a.service(id)
		if !known {
			return nil, name + " is not reachable from this conversation."
		}
		if _, failed := a.connectService(ctx, service); failed != "" {
			return nil, failed
		}
	}
	client, err := a.connect.Client(ctx, id)
	if err != nil {
		return nil, name + " is not connected any more: " + err.Error() +
			". Say so plainly; use_service asks the person to connect it again."
	}
	return client, ""
}

// errNobodyWatching is askConnect's one error that is not the turn ending: there
// is no surface subscribed to this session's events, so the question would be
// asked into an empty room.
var errNobodyWatching = errors.New("session: nobody is watching this session")

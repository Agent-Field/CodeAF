package session

// AFI SETS A NEW ORGANIZATION UP: ONE CARD, ONE PRESS (v2 designs J3).
//
// A new organization is empty. Afi asks its founder what is on their plate
// this month, and answers with ONE card through org_propose_setup: the
// workstreams it would make, with what each is for, who is in it and the
// workstreams inside it; the teams; and the people to invite with their email
// and team. The person changes the card in the app and presses "Create N
// workstreams and invite M people", which makes all of it on the
// organization's server (factory/serve/org_setup.go).
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - PROPOSING WRITES NOTHING SHARED. The door keeps the card for the person
//     who asked; nobody is invited and nothing is made until the press, which
//     is the person's, in the app. A newer card replaces the older one.
//
//   - THE CONVERSATION THAT PROPOSES IT CARRIES THIS VERB AND NO OTHER. It is
//     the organization's setup conversation on its server, with no workstream
//     yet to read or write, so the belt is the one tool (tools.go).
//
//   - A DOOR THAT CANNOT KEEP A CARD GIVES NO TOOL. [Config.OrgSetup] nil is
//     no verb at all (the design law: absent, never broken).

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// OrgProposeSetupTool is Afi's one verb for setting an organization up.
const OrgProposeSetupTool = "org_propose_setup"

// OrgSetupDoor is the organization server's door for its setup conversation.
type OrgSetupDoor interface {
	// ProposeOrgSetup keeps args (the card) for the person being answered
	// and answers one line saying what the person sees.
	ProposeOrgSetup(ctx context.Context, args json.RawMessage) (string, error)
}

// OrgSetupRule is what Afi is told about setting an organization up.
var OrgSetupRule = "- The organization is new and has no workstreams. When the person tells you what is on their plate, propose the whole set-up at once with " + OrgProposeSetupTool + ": " +
	"the workstreams, the teams, and the people to invite. Ask no questions first unless nothing they said names any work; then ask one. " +
	"A top-level workstream is a standing area with stable people (the product, marketing, the company, a client, an event). " +
	"A piece of work by the SAME people as an area goes inside that area (Launch 0.9 inside Marketing, Seed round inside Company), never beside it; different people make a top-level workstream. " +
	"Give each workstream what it is for in one line, with its date when one was said. Put \"me\" in every workstream's people; add the others by the names the person used. " +
	"Invite every person they named, with their email when they gave one (never invent one), and a team when they said people work together (\"Marco and Sam are the engineers\" is an Engineering team). " +
	"After proposing, reply with exactly: \"Here is a starting set-up. Untick or rename anything. Nothing runs until you start it.\" Never repeat the card in your reply. " +
	"When they ask for a change, propose the whole card again with the change."

var orgSetupSchema = `{"type":"object","properties":{` +
	`"workstreams":{"type":"array","maxItems":` + strconv.Itoa(factory.OrgSetupMostWorkstreams) + `,"items":{"type":"object","properties":{` +
	`"name":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(factory.OrgSetupNameMost) + `,"description":"A short name, as the person would say it."},` +
	`"purpose":{"type":"string","maxLength":` + strconv.Itoa(factory.OrgSetupPurposeMost) + `,"description":"What it is for, in one line, with the date when one was given."},` +
	`"people":{"type":"array","maxItems":` + strconv.Itoa(factory.OrgSetupMostPeople) + `,"items":{"type":"string","minLength":1,"maxLength":80},"description":"Who is in it, by name; \"me\" is the person you are talking to."},` +
	`"inside":{"type":"array","maxItems":` + strconv.Itoa(factory.OrgSetupMostInside) + `,"items":{"type":"object","properties":{` +
	`"name":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(factory.OrgSetupNameMost) + `},` +
	`"purpose":{"type":"string","maxLength":` + strconv.Itoa(factory.OrgSetupPurposeMost) + `}` +
	`},"required":["name"],"additionalProperties":false},"description":"Workstreams inside this one: pieces of its work by the same people."}` +
	`},"required":["name"],"additionalProperties":false}},` +
	`"teams":{"type":"array","maxItems":` + strconv.Itoa(factory.OrgSetupMostTeams) + `,"items":{"type":"object","properties":{` +
	`"name":{"type":"string","minLength":1,"maxLength":80,"description":"e.g. Engineering team"},` +
	`"people":{"type":"array","items":{"type":"string","minLength":1,"maxLength":80},"description":"The invited people in it, by name."}` +
	`},"required":["name"],"additionalProperties":false}},` +
	`"invites":{"type":"array","maxItems":` + strconv.Itoa(factory.OrgSetupMostInvites) + `,"items":{"type":"object","properties":{` +
	`"name":{"type":"string","minLength":1,"maxLength":80},` +
	`"email":{"type":"string","maxLength":254,"description":"Only an address the person gave; leave out otherwise."},` +
	`"team":{"type":"string","maxLength":80,"description":"The team's name, when they are in one."}` +
	`},"required":["name"],"additionalProperties":false}}` +
	`},"required":["workstreams"],"additionalProperties":false}`

const orgSetupDescription = "Propose how this new organization is set up, as one card the person changes and accepts: the workstreams (what each is for, who is in it, the workstreams inside it), the teams, and the people to invite with their email and team. " +
	"Nothing is made and nobody is invited until the person presses the card's button, which makes all of it at once; a newer card replaces this one."

func init() {
	glossField[OrgProposeSetupTool] = "workstreams"
}

// mayOrgSetup says this conversation is an organization's setup conversation.
func (c Config) mayOrgSetup() bool { return c.OrgSetup != nil }

// orgSetupTool is the verb onto [Config.OrgSetup].
func (a *Agent) orgSetupTool() bare.Tool {
	door := a.config.OrgSetup
	return bare.Tool{
		Name: OrgProposeSetupTool, Description: orgSetupDescription, Schema: json.RawMessage(orgSetupSchema),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			if len(args) == 0 || strings.TrimSpace(string(args)) == "null" {
				args = json.RawMessage(`{}`)
			}
			var probe map[string]json.RawMessage
			if err := decodeToolArguments(args, &probe); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			line, err := door.ProposeOrgSetup(ctx, args)
			if err != nil {
				return "Nothing changed: " + oneLine(err.Error()), true, nil
			}
			return line, false, nil
		},
	}
}

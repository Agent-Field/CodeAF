package session

// AFI SETS A NEW WORKSTREAM UP: ONE CARD THE PERSON CHANGES AND ACCEPTS.
//
// Owner decision 2026-11-17 §9: "New workstream" opens an empty workstream
// with Afi's pane already asking what the work is about. Once Afi
// understands, it proposes a workstream card (name and goal, people and
// teams, who answers, rules and spending, a first plan of tasks, files to
// bring along) that the person edits in place and accepts. The setup work
// never falls on the person, and nothing is written until they accept.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE CARD IS SPELLED HERE ONCE. Its name, schema and rules are the same
//     on both doors that serve it: the desktop app on a Personal workstream
//     (codeaf-desk pins [WorkstreamSetupTools]' JSON beside the work tools)
//     and the organization's server through its [WorkstreamWorkDoor].
//
//   - PROPOSING WRITES NOTHING SHARED. The door keeps the card as the
//     workstream's one pending card for the person who asked, and the app
//     draws it; Accept is the person's, in the app. A newer card replaces the
//     older one.
//
//   - A DOOR THAT CANNOT KEEP A CARD GIVES NO TOOL. The verb rides the belt
//     only when the door also says [WorkstreamSetupDoor]; absent, never present
//     and refusing (the design law).

import (
	"encoding/json"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// WSProposeSetupTool is Afi's one verb for setting a workstream up.
const WSProposeSetupTool = "ws_propose_setup"

// WorkstreamSetupMostTasks is the most tasks a first plan carries: a request
// becomes 5 to 8 deliverables, never twenty crumbs (owner decision §7).
const WorkstreamSetupMostTasks = 8

// WorkstreamSetupMostQuestions is how many follow-up questions Afi may ask
// before it proposes. It never interrogates (owner decision §9).
const WorkstreamSetupMostQuestions = 2

// WorkstreamSetupDoor is a [WorkstreamWorkDoor] that also keeps a setup
// card. Its Work answers [WSProposeSetupTool] by name like any other tool.
type WorkstreamSetupDoor interface {
	WorkstreamWorkDoor
	KeepsSetupCards() bool
}

// WorkstreamSetupRule is what Afi is told about a new workstream, beside the
// work rules. The desktop app pins it with the tool.
var WorkstreamSetupRule = "- A workstream with no goal and no tasks is new. When the person tells you what it is for, or asks you to set it up (again), propose its setup with ws_propose_setup: a name, the goal in one or two sentences, who is involved, who answers, rules, and a first plan of 5 to " + strconv.Itoa(WorkstreamSetupMostTasks) + " deliverable tasks (fewer only for small personal work). " +
	"Propose right away when you can. Ask at most " + []string{"zero", "one", "two", "three"}[WorkstreamSetupMostQuestions] + " short questions first, in one message, and only when the answer changes the card and cannot be guessed (who decides, a date). Never ask about rules, spending, names or files: draft sensible ones; the person changes them on the card. " +
	"After proposing, say in one line that the setup is ready to change and accept, on the left. Never repeat the card in your reply, and never make its tasks or files yourself: Accept does that."

var workstreamSetupTools = []WorkstreamWorkTool{
	{Name: WSProposeSetupTool, Category: "create",
		Description: "Propose how this workstream is set up, as one card the person changes and accepts: its name, goal, people and teams, who answers, rules and spending, a first plan of tasks, and a brief. " +
			"Nothing is written until the person presses Accept, which makes the tasks, the brief and the rules all at once; a newer card replaces this one. " +
			"Use it for a new workstream (no goal and no tasks yet) once the person has said what it is for, or when they ask you to set it up again. " +
			"Tasks are deliverables someone could review (a document, a decision, a booked thing), hours to days of work each, never steps. Each has who does it: afi, or a person by name. " +
			"Name people exactly as the person did; the app matches them to people in the workstream's organization. On a Personal workstream leave people, teams and fallback out: it is the person's alone.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"name":{"type":"string","minLength":1,"maxLength":80,"description":"A short name for the work, as the person would say it."},` +
			`"goal":{"type":"string","minLength":1,"maxLength":400,"description":"What done looks like, in one or two sentences, with the date if one was given."},` +
			`"brief":{"type":"string","maxLength":8000,"description":"The workstream's brief as markdown: the why, what is in and out, and what was said that a newcomer needs. It becomes a document in Files."},` +
			`"people":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1,"maxLength":80},"description":"People to add, by name. Organization only."},` +
			`"teams":{"type":"array","maxItems":10,"items":{"type":"string","minLength":1,"maxLength":80},"description":"Teams to add, by name. Organization only."},` +
			`"incoming_owner":{"type":"string","maxLength":80,"description":"Who owns work nobody asked for directly: \"me\" (the default) or a person's name."},` +
			`"decider":{"type":"string","maxLength":80,"description":"Who looks after the workstream (its people, rules and archiving): \"me\" (the default) or a person's name. Organization only."},` +
			`"fallback":{"type":"string","maxLength":80,"description":"Who is asked when an Ask waits a day: a person's name. Organization only; leave out for nobody."},` +
			`"start_without_asking_usd":{"type":"number","minimum":0,"maximum":1000,"description":"The most an agent may spend on a task before it must ask; 0 means always ask."},` +
			`"rules":{"type":"array","maxItems":5,"items":{"type":"string","minLength":1,"maxLength":200},"description":"Plain statements the people hold each other to (\"Nothing goes to a candidate without Priya's OK\"). Each becomes a decision on Accept."},` +
			`"tasks":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","properties":{` +
			`"title":{"type":"string","minLength":1,"maxLength":120},` +
			`"request":{"type":"string","maxLength":1000,"description":"What is to be done, in a few lines."},` +
			`"doer":{"type":"string","enum":["afi","person"],"description":"afi (the default) or a person, named in person."},` +
			`"person":{"type":"string","maxLength":80,"description":"With doer person: who does it, by name; \"me\" for the person you are talking to."},` +
			`"waits_on":{"type":"array","maxItems":7,"items":{"type":"integer","minimum":1,"maximum":8},"description":"The positions (1 is the first task) of tasks that must finish first."},` +
			`"due":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$","description":"YYYY-MM-DD, only when the person gave a date."}` +
			`},"required":["title"],"additionalProperties":false}},` +
			`"inside":{"type":"string","maxLength":80,"description":"The name of a top-level workstream this one belongs inside, only when the person said so."}` +
			`},"required":["name","goal","tasks"],"additionalProperties":false}`)},
}

// WorkstreamSetupTools is the setup verb, which Afi alone carries. The slice
// is a copy.
func WorkstreamSetupTools() []WorkstreamWorkTool {
	return append([]WorkstreamWorkTool(nil), workstreamSetupTools...)
}

func init() {
	glossField[WSProposeSetupTool] = "name"
}

// workstreamSetupDoor is the conversation's door when it keeps setup cards.
func (c Config) workstreamSetupDoor() WorkstreamWorkDoor {
	door, ok := c.Workstream.(WorkstreamSetupDoor)
	if !ok || !door.KeepsSetupCards() {
		return nil
	}
	return door
}

// workstreamSetupBelt is the setup verb onto door, answered like the work
// tools (one object of arguments, a refusal said in words).
func workstreamSetupBelt(door WorkstreamWorkDoor) []bare.Tool {
	return workstreamToolsOnto(door, workstreamSetupTools, nil)
}

package session

// AFI PROPOSES TASKS AS CARDS (Workstreams v2, screen 8; DELTA §4.1 L4).
//
// In the person's own conversation with Afi, tasks that involve someone else,
// carry a date, or make a plan arrive as cards: the person unticks, changes
// and presses "Add N tasks", and NOTHING IS MADE UNTIL THEY DO. The tool writes
// nothing shared; the door keeps the proposal as a line of the conversation,
// and the person's press makes the tasks, as them.
//
// THE VERB IS ABSENT WHERE NOTHING CAN DRAW A CARD. It rides the belt only
// when the conversation's door implements [WorkstreamProposeDoor] (an
// organization's server, for a person's conversation with Afi). A task's own
// run, and a Personal workstream until the desktop app draws the cards, never
// carry it, so the model never has a verb whose cards nobody sees.

import (
	"encoding/json"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WSTaskProposeTool proposes tasks as cards.
const WSTaskProposeTool = "ws_task_propose"

// WorkstreamProposeDoor is a work door that can keep a proposal of task cards
// in the conversation it answers. Proposes is false where it cannot (a task's
// own run), and the verb is then left off the belt.
type WorkstreamProposeDoor interface {
	WorkstreamWorkDoor
	Proposes() bool
}

// WorkstreamCardsRule is when tasks are proposed rather than made. It names
// the tool, so it is given only beside it.
var WorkstreamCardsRule = "- Propose, then add. When the tasks you would make involve someone else (a person does or owns one), carry a due date, or are a plan of " + strconv.Itoa(WorkstreamPlanFewest) + " or more, " +
	"propose them all in ONE call to " + WSTaskProposeTool + " instead of making them: the person sees them as cards, unticks or changes them, and presses Add, and nothing is made until then. " +
	"Then say in one line what you proposed and who will be told when it is added; never also make the same tasks with ws_task_create, and never say they exist yet. " +
	"One task that is only yours and theirs, with no date, you make with ws_task_create and name in your reply.\n"

var workstreamProposeTool = WorkstreamWorkTool{Name: WSTaskProposeTool, Category: "read",
	Description: "Propose tasks to the person you are talking to, as cards they can untick, change and add. Nothing is made and nobody is told until they press Add; then the tasks are made as them, and the people named are told. " +
		"Use it for tasks that involve someone else, carry a due date, or are a plan of " + strconv.Itoa(WorkstreamPlanFewest) + " to " + strconv.Itoa(WorkstreamPlanMost) + " tasks (give a plan one group name). " +
		"Each card is a deliverable (hours to days of work), never anything under " + strconv.Itoa(WorkstreamTaskSmallestMinutes) + " minutes; its steps are its checklist, never cards of their own. " +
		"Propose them all in ONE call, waits included (a card waits only on earlier cards of the same call); a second call is a second set of cards for the person to add. A group is only for a plan of " + strconv.Itoa(WorkstreamPlanFewest) + " or more.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{` +
		`"group":{"type":"string","maxLength":200,"description":"A plan's name, for ` + strconv.Itoa(WorkstreamPlanFewest) + ` or more tasks: a short name for the work."},` +
		`"tasks":{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(WorkstreamPlanMost) + `,"items":{"type":"object","properties":{` +
		`"title":{"type":"string","minLength":1,"maxLength":200},` +
		`"request":{"type":"string","minLength":1,"maxLength":4000,"description":"What is to be done, in a few lines. Never the conversation."},` +
		`"checklist":{"type":"array","maxItems":` + strconv.Itoa(factory.TaskChecklistMost) + `,"items":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(factory.TaskChecklistLineMost) + `},"description":"The steps of this one deliverable, one line each; lines inside the task once added, never cards of their own."},` +
		`"done_by":{"type":"string","enum":["afi","person"],"description":"Who does it: Afi (the default), or a person (person names who; the default is the person you are talking to)."},` +
		`"person":{"type":"string","description":"With done_by person: who does it, a person's id from ws_tasks; empty is the person you are talking to."},` +
		`"owner":{"type":"string","description":"Who answers for it: a person's id from ws_tasks; empty is the person you are talking to."},` +
		`"due":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$","description":"YYYY-MM-DD, only when the person gave a date."},` +
		`"waits_on":{"type":"array","maxItems":` + strconv.Itoa(WorkstreamPlanMost) + `,"items":{"type":"integer","minimum":1},"description":"The numbers (1 is the first card) of earlier cards in this call it waits on."}` +
		`},"required":["title","request"],"additionalProperties":false}}` +
		`},"required":["tasks"],"additionalProperties":false}`)}

// WorkstreamProposeTool is the cards verb's definition, for a door that
// proposes.
func WorkstreamProposeTool() WorkstreamWorkTool { return workstreamProposeTool }

func init() { glossField[WSTaskProposeTool] = "group" }

// proposes is whether door keeps proposals.
func proposes(door WorkstreamWorkDoor) bool {
	p, ok := door.(WorkstreamProposeDoor)
	return ok && p.Proposes()
}

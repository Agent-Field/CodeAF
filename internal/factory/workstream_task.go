package factory

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// ── A WORKSTREAM'S TASKS (Workstreams v5, SPEC §2.2) ──────────────────────
//
// A task is one piece of work with ONE OWNER. Who does it varies (Afi, a chat,
// the Factory, a person); who answers for it does not. The desk keeps the same
// shape in its own SQLite for a Personal workstream (codeaf-desk
// crates/workstreams), and both stores enforce the rules in this file:
//
//   - ONLY THE OWNER ANSWERS AN ASK. With owner Everyone, anyone who can edit
//     the workstream may, the first answer decides, and AnsweredBy records who.
//     An Ask unanswered for a day is also the workstream's fallback person's.
//   - ONLY THE OWNER OR THE PERSON DOING IT TICKS A PERSON TASK. Ticking it
//     releases the tasks that wait on it. A task Afi's cards made for someone
//     else is owned by whoever pressed Add and done by the person named, who
//     works it without its being handed over first (blackmac fill v2).
//   - WAITING ON IS THE ONLY ORDERING TOOL: tasks of the same workstream, never
//     itself, never a circle.
//   - A REPLY ON A DONE TASK REOPENS IT, so the history stays one thread.
//   - A FACTORY JOB IN THE WORKSTREAM IS A TASK. It is derived from the job on
//     every read (JobTask) and never copied; what a person sets on it (its
//     owner, its heading, what it waits on, its due date) is kept as a small
//     overlay under the job's stable id, "job:<n>".
//   - AFI NEVER ACTS ON A COMMENT. It may hang an offer under one; anyone may
//     take it, and the owner is told and may undo it for five seconds.
//
// The words a person reads for a state are the app's own: Failed, Interrupted,
// Stopped by you, Didn't report back.

// The task states.
const (
	TaskUpNext      = "up_next"
	TaskRunning     = "running"
	TaskNeeds       = "needs"
	TaskDone        = "done"
	TaskFailed      = "failed"
	TaskInterrupted = "interrupted"
	TaskStopped     = "stopped"
	TaskNoReport    = "no_report"
)

// The kinds of owner, doer and origin.
const (
	TaskOwnerPerson   = "person"
	TaskOwnerEveryone = "everyone"

	TaskByAfi     = "afi"
	TaskByChat    = "chat"
	TaskByFactory = "factory"
	TaskByPerson  = "person"
	TaskByOrder   = "order"
	// TaskByAgent is a named agent or crew (org-space DESIGN §4.1), by the
	// id the workstream's agent list knows it under.
	TaskByAgent = "agent"
)

// The kinds of event in a task's thread.
const (
	TaskEventComment     = "comment"
	TaskEventWorked      = "worked"
	TaskEventRequestNote = "request_note"
	TaskEventOffer       = "offer"
	TaskEventAnswered    = "answered"
	TaskEventPosted      = "posted"
	TaskEventState       = "state"
	TaskEventHanded      = "handed"
	TaskEventMoved       = "moved"
	TaskEventChecked     = "checked"
	// TaskEventMade is the one line a long piece of an agent's output leaves
	// on the thread once it has been kept as a document: its File is the
	// document, and the line reads as its name with Open (build spec v2, P1).
	TaskEventMade = "made"
)

// TaskCommentLongest and TaskCommentLines are how much an agent may write
// on a task's thread as a comment. WHAT AN AGENT MAKES IS A FILE: anything
// longer is kept as a document made by the task, and the thread gets one
// line naming it (build spec v2, priority 1: Afi posted whole drafts as
// comments, and the thread became the only copy of the work).
const (
	TaskCommentLongest = 400
	TaskCommentLines   = 3
	// TaskCommentWordsMost is the most ws_task_comment takes in one call;
	// past TaskCommentLongest it is kept as a document, not posted.
	TaskCommentWordsMost = 16000
)

// MadeFileName names the document a long comment is kept as: its first
// Markdown heading when it opens with one, otherwise the task's title.
func MadeFileName(markdown, taskTitle string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(markdown), "\n")
	if h := strings.TrimSpace(strings.TrimLeft(first, "#")); strings.HasPrefix(first, "#") && h != "" {
		return cutName(h)
	}
	if t := strings.TrimSpace(taskTitle); t != "" {
		return cutName(t)
	}
	return "Notes"
}

func cutName(s string) string {
	if r := []rune(s); len(r) > 120 {
		return strings.TrimSpace(string(r[:120]))
	}
	return s
}

// TaskCommentTooLong reports whether text is more than an agent may leave
// on a thread as a comment: over [TaskCommentLongest] characters or
// [TaskCommentLines] lines.
func TaskCommentTooLong(text string) bool {
	text = strings.TrimSpace(text)
	return utf8.RuneCountInString(text) > TaskCommentLongest || strings.Count(text, "\n")+1 > TaskCommentLines
}

// The states of an offer.
const (
	OfferOpen      = "open"
	OfferTaken     = "taken"
	OfferDismissed = "dismissed"
)

// The numbers the rules are made of. ONE SOURCE: the desk's wsTasksModel.ts
// spells the same three.
const (
	// TaskFallbackAfter is how long an Ask waits on its owner before the
	// workstream's fallback person may answer it too.
	TaskFallbackAfter = 24 * time.Hour
	// TaskOfferUndo is how long the owner may undo a taken offer.
	TaskOfferUndo = 5 * time.Second
	// TaskJobPrefix is the id a Factory job is shown under: "job:<n>".
	TaskJobPrefix = "job:"
)

// The most one task or one thread may hold.
const (
	taskTitleMost   = 300
	taskRequestMost = 8 << 10
	taskTextMost    = 16 << 10
	taskWaitsMost   = 50
	taskResultsMost = 50
	taskOptionsMost = 8
	// TasksMost is how many tasks one workstream keeps.
	TasksMost = 5000
)

// PersonRef names a person: their handle on an organization, and the words
// to show (the handle until a client knows their name).
type PersonRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TaskOwner is who answers for a task: a person, or everyone.
type TaskOwner struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// TaskDoneBy is who does the work: a person, Afi, a named agent or crew, a
// Factory job, or a chat (whose agent does it).
type TaskDoneBy struct {
	Kind   string `json:"kind"`
	ChatID string `json:"chat_id,omitempty"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Job    int    `json:"job,omitempty"`
	// AgentID and Crew name a named agent or crew (Kind agent).
	AgentID string `json:"agent_id,omitempty"`
	Crew    bool   `json:"crew,omitempty"`
}

// AgentDoes is whether an agent does t: Afi, a chat's agent or a named agent
// or crew. These are the tasks a Start wakes; a person does their own and a
// Factory job starts at the Factory.
func AgentDoes(t WorkstreamTask) bool {
	switch t.DoneBy.Kind {
	case TaskByAfi, TaskByChat, TaskByAgent:
		return true
	}
	return false
}

// DoerWords is who does the work, as a sentence names it: "Afi", "Lena",
// "Copywriter", "the Factory".
func DoerWords(d TaskDoneBy) string {
	switch d.Kind {
	case TaskByAfi:
		return "Afi"
	case TaskByFactory:
		return "the Factory"
	}
	if d.Name != "" {
		return d.Name
	}
	return firstNonEmptyWords(d.ID, d.AgentID, d.ChatID, d.Kind)
}

// TaskCameFrom is where a task came from.
type TaskCameFrom struct {
	Kind    string `json:"kind"`
	ChatID  string `json:"chat_id,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Issue   string `json:"issue,omitempty"`
	OrderID string `json:"order_id,omitempty"`
	// Run is the standing order's run that made the task, so one run makes
	// exactly one task however often its holder's desk looks.
	Run int64 `json:"run,omitempty"`
	// WS and WSName name the workstream a request came from, when someone
	// asked from another one (SPEC "Work for another team is a request into
	// their workstream"): "From Priya · Marketing".
	WS     string `json:"ws,omitempty"`
	WSName string `json:"ws_name,omitempty"`
}

// TaskGroup is a heading that groups tasks. It is not a task.
type TaskGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// TaskResult is one thing a task made: a file in Files, or an address.
type TaskResult struct {
	Kind   string `json:"kind"`
	FileID string `json:"file_id,omitempty"`
	URL    string `json:"url,omitempty"`
	Label  string `json:"label,omitempty"`
}

// TaskAskOption is one answer an Ask offers.
type TaskAskOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
	// Decides is the decision this answer records, in one line ("The March
	// notes go on the website"). Absent, the decision reads as the question
	// and the answer chosen.
	Decides string `json:"decides,omitempty"`
	// Words is whether choosing this answer means the person gives
	// something in their own words (dates, a price list, a text to paste).
	// Such an answer is never one click: the app asks for the words, and an
	// Ask holding one is never quick (live test 2026-11-23 #37).
	Words bool `json:"words,omitempty"`
}

// NeedsWords is whether any of ask's answers needs the person's own words.
func NeedsWords(ask TaskAsk) bool {
	for _, o := range ask.Options {
		if o.Words {
			return true
		}
	}
	return false
}

// TaskAsk is a question a task waits on. The task's own Ask (Task.Ask) is
// decided by its owner; each of Task.Asks names its own Decider.
type TaskAsk struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Detail     string          `json:"detail"`
	Options    []TaskAskOption `json:"options"`
	AskedAt    string          `json:"asked_at"`
	LaterUntil string          `json:"later_until,omitempty"`
	// Decider is the ONE person who answers it. Absent on Task.Ask, whose
	// decider is the task's owner.
	Decider *PersonRef `json:"decider,omitempty"`
	// By is who raised it: Afi, a chat, the factory or a person.
	By *TaskEventBy `json:"by,omitempty"`
	// Views are the short lines others attached for the decider to read, one
	// per person, never the decider's own. A view is not a vote.
	Views []TaskView `json:"views,omitempty"`
	// Against is the decision the work went against, on the Ask the decision
	// check raised to whoever made it.
	Against *TaskAgainst `json:"against,omitempty"`
	// Then is the state the task goes back to once this, its last open Ask,
	// is answered: a person's Ask on work that is up next leaves it up next
	// when answered, rather than saying running with nothing behind it.
	// Absent, the answered task runs, as an agent's own Ask always has.
	Then string `json:"then,omitempty"`
	// Reversible is whether whatever the answer lets happen can be undone
	// (OWNER-DECISIONS 2026-11-17 §13). WHOEVER RAISES THE ASK SETS IT: an
	// agent is told to set it honestly, and a person's Ask is false unless
	// they say otherwise. Only a reversible Ask joins the Inbox's "Quick, and
	// can be undone" group and its "Accept Afi's answer on all", and only its
	// answer is kept in WorkstreamTask.Undoable, to be undone from the task.
	Reversible bool `json:"reversible,omitempty"`
	// Kind is what sort of question it is, in a few words the agent that
	// raised it chose ("docs wording", "release date"). The ask rule's
	// exceptions are named by it ([AskException]), and Afi suggests one when
	// many waiting Asks share it. A person's Ask has none.
	Kind string `json:"kind,omitempty"`
	// CoveredBy is the workstream's fallback person once this Ask has waited
	// past the rules' wait on its decider: they may answer it too, and it
	// reaches their Inbox. FILLED AT READ TIME, never kept ([FillCoveredBy]).
	CoveredBy *PersonRef `json:"covered_by,omitempty"`
}

// TaskAnswered is one answer given to a reversible Ask or a person's Ask, kept on the task so
// whoever gave it can undo it (TaskPatch.UndoAnswer): the Ask as it was, the
// answer, who gave it and when, and the decision it kept.
type TaskAnswered struct {
	Ask      TaskAsk   `json:"ask"`
	Option   string    `json:"option,omitempty"`
	Answer   string    `json:"answer"`
	By       PersonRef `json:"by"`
	At       string    `json:"at"`
	Decision string    `json:"decision,omitempty"`
}

// AskFromPerson is whether a person raised a, not an agent, Afi or the
// factory. Its answer can be undone like a reversible Ask's ([TaskAnswered]).
// The invite chase and the can't-do Ask are not: their answers move the task
// to someone else, which an undo does not move back.
func AskFromPerson(a TaskAsk) bool {
	return a.By != nil && a.By.Kind == TaskByPerson && !IsInviteChaseAsk(a) && !IsCantDoAsk(a)
}

// TaskUndoableMost is how many answers a task keeps to undo; the oldest goes
// first. Ten is more than any task has had open at once.
const TaskUndoableMost = 10

// TaskAskHand hands one open Ask to another decider (TaskPatch.AskHand).
type TaskAskHand struct {
	Ask string    `json:"ask"`
	To  PersonRef `json:"to"`
}

// TaskView is one person's view on someone else's Ask.
type TaskView struct {
	By   PersonRef `json:"by"`
	Text string    `json:"text"`
	At   string    `json:"at"`
}

// TaskAgainst names the decision a conflict Ask is about.
type TaskAgainst struct {
	Decision string    `json:"decision"`
	Text     string    `json:"text"`
	By       PersonRef `json:"by"`
	// Why is the check's one line on how the work goes against it.
	Why string `json:"why,omitempty"`
}

// TaskAssumed is one thing an agent decided without asking: cheap,
// reversible work that goes against no decision, done and told after. Send
// back overturns it (OverturnedBy).
type TaskAssumed struct {
	ID           string      `json:"id"`
	Text         string      `json:"text"`
	At           string      `json:"at"`
	By           TaskEventBy `json:"by"`
	OverturnedBy *PersonRef  `json:"overturned_by,omitempty"`
}

// TaskBrief is what an agent needs to do a task cold (DESIGN §11.1, L7): why
// it matters, what comes back, how it is checked, what it builds on, and the
// lines it must keep to. Request stays the one-paragraph summary a person
// reads in a list; a task made before briefs existed has none.
//
// A BRIEF IS WRITTEN FOR AN AGENT THAT NEVER SAW THE CONVERSATION. Every
// input is a reference (a file, a task's Result, a decision, an address),
// never prose such as "building on the release notes", so the run that does
// the task starts with their words in its opening context (the server's
// taskPacket) instead of hunting for them.
type TaskBrief struct {
	Goal        string           `json:"goal,omitempty"`
	Deliverable string           `json:"deliverable,omitempty"`
	Acceptance  []string         `json:"acceptance,omitempty"`
	Inputs      []TaskBriefInput `json:"inputs,omitempty"`
	Constraints []string         `json:"constraints,omitempty"`
}

// The kinds of a brief's input.
const (
	TaskInputFile     = "file"
	TaskInputTask     = "task"
	TaskInputDecision = "decision"
	TaskInputURL      = "url"
)

// TaskBriefInput is one thing a task's work builds on. A task input means
// that task's Result: its result files, its check line and its hand-off note.
type TaskBriefInput struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	URL  string `json:"url,omitempty"`
	Why  string `json:"why,omitempty"`
}

// The most a brief holds. ONE SOURCE: the tool schema interpolates these.
const (
	TaskAcceptanceFewest = 2
	TaskAcceptanceMost   = 6
	TaskInputsMost       = 12
	TaskConstraintsMost  = 8
	taskBriefLineMost    = 600
	// TaskHandoffMost is the most a hand-off note holds, in characters.
	TaskHandoffMost = 1200
)

// IsEmpty is whether b says nothing.
func (b *TaskBrief) IsEmpty() bool {
	return b == nil || (b.Goal == "" && b.Deliverable == "" && len(b.Acceptance) == 0 && len(b.Inputs) == 0 && len(b.Constraints) == 0)
}

// CleanBrief trims b and checks its sizes and input kinds. It answers nil for
// a brief that says nothing.
func CleanBrief(b *TaskBrief) (*TaskBrief, error) {
	if b == nil {
		return nil, nil
	}
	lines := func(in []string, what string, most int) ([]string, error) {
		out := []string{}
		for _, l := range in {
			if l = strings.TrimSpace(l); l == "" {
				continue
			}
			if len([]rune(l)) > taskBriefLineMost {
				return nil, taskInvalidf("a line of %s is longer than %d characters", what, taskBriefLineMost)
			}
			out = append(out, l)
		}
		if len(out) > most {
			return nil, taskInvalidf("%s has at most %d lines", what, most)
		}
		return out, nil
	}
	c := TaskBrief{Goal: strings.TrimSpace(b.Goal), Deliverable: strings.TrimSpace(b.Deliverable)}
	if len([]rune(c.Goal)) > taskBriefLineMost || len([]rune(c.Deliverable)) > taskBriefLineMost {
		return nil, taskInvalidf("a brief's goal and deliverable are at most %d characters each", taskBriefLineMost)
	}
	var err error
	if c.Acceptance, err = lines(b.Acceptance, "acceptance", TaskAcceptanceMost); err != nil {
		return nil, err
	}
	if c.Constraints, err = lines(b.Constraints, "constraints", TaskConstraintsMost); err != nil {
		return nil, err
	}
	if len(b.Inputs) > TaskInputsMost {
		return nil, taskInvalidf("a brief has at most %d inputs", TaskInputsMost)
	}
	for _, in := range b.Inputs {
		in.Kind, in.ID, in.URL, in.Why = strings.TrimSpace(in.Kind), strings.TrimSpace(in.ID), strings.TrimSpace(in.URL), strings.TrimSpace(in.Why)
		switch in.Kind {
		case TaskInputFile, TaskInputTask, TaskInputDecision:
			if in.ID == "" {
				return nil, taskInvalidf("a %s input names its id", in.Kind)
			}
		case TaskInputURL:
			if in.URL == "" {
				in.URL, in.ID = in.ID, ""
			}
			if in.URL == "" {
				return nil, TaskInvalid("a url input names its address")
			}
		default:
			return nil, taskInvalidf("an input is a file, task, decision or url, not %q", in.Kind)
		}
		in.Why = cutWords(in.Why, 300)
		if !slices.Contains(c.Inputs, in) {
			c.Inputs = append(c.Inputs, in)
		}
	}
	if c.IsEmpty() {
		return nil, nil
	}
	return &c, nil
}

// cutWords cuts s to most runes.
func cutWords(s string, most int) string {
	if r := []rune(s); len(r) > most {
		return string(r[:most])
	}
	return s
}

// WorkstreamTask is one task.
type WorkstreamTask struct {
	ID      string `json:"id"`
	WS      string `json:"ws"`
	Rev     int    `json:"rev"`
	Title   string `json:"title"`
	Request string `json:"request"`
	// Brief is what an agent needs to do the task cold; absent on a task
	// made before briefs, which reads by its Request.
	Brief       *TaskBrief   `json:"brief,omitempty"`
	Owner       TaskOwner    `json:"owner"`
	DoneBy      TaskDoneBy   `json:"done_by"`
	CameFrom    TaskCameFrom `json:"came_from"`
	State       string       `json:"state"`
	StateReason string       `json:"state_reason,omitempty"`
	// Step is where a Factory task's job is, "Step 4 of 4, Review"; absent on
	// every other task and on a job that is not running or waiting.
	Step      string       `json:"step,omitempty"`
	WaitingOn []string     `json:"waiting_on"`
	Due       string       `json:"due,omitempty"`
	Group     *TaskGroup   `json:"group,omitempty"`
	Results   []TaskResult `json:"results"`
	Check     string       `json:"check,omitempty"`
	// Handoff is the doer's note when it finished: what it made, what it
	// assumed, and what the next task should know. It goes into the opening
	// context of every task that waits on this one or names it as an input.
	Handoff string   `json:"handoff,omitempty"`
	Ask     *TaskAsk `json:"ask,omitempty"`
	// Asks are the task's other open Asks, each with its own decider. The
	// task waits on all of them and on Ask: approval by several people is
	// several Asks.
	Asks []TaskAsk `json:"asks,omitempty"`
	// Assumed is what the agent decided without asking.
	Assumed []TaskAssumed `json:"assumed,omitempty"`
	// InviteChasedAt is when the owner was last asked to hand on this task
	// because the person it waits on was invited and has not joined
	// ([ChaseInvite]); the next ask is another [InviteChaseAfter] away.
	InviteChasedAt string `json:"invite_chased_at,omitempty"`
	// Checklist is the steps of this one deliverable (org-space DESIGN §5.2):
	// lines inside the task, each ticked or not. A line is never a task of
	// its own: it has no owner, page, due date or state beyond its tick.
	Checklist  []TaskCheckItem `json:"checklist,omitempty"`
	CostUSD    *float64        `json:"cost_usd,omitempty"`
	CreatedBy  PersonRef       `json:"created_by"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
	DoneAt     string          `json:"done_at,omitempty"`
	AnsweredBy *PersonRef      `json:"answered_by,omitempty"`
	// TitleBy and RequestBy are who last wrote the task's title and its
	// request, each as the kind of writer (TaskByPerson, TaskByAfi, …), and
	// only ever written by the server; empty on a task made before they were
	// kept, which reads as not a person. The server agent's web_fetch may read
	// an address in one of them only when a person wrote it (serve's
	// taskLeashWords, session's tools_web_leash.go).
	TitleBy   string `json:"title_by,omitempty"`
	RequestBy string `json:"request_by,omitempty"`
	// Undoable are the answers given to its reversible Asks, oldest first, at
	// most TaskUndoableMost: each can be undone from the task by whoever gave
	// it, which opens the Ask again (TaskPatch.UndoAnswer).
	Undoable []TaskAnswered `json:"undoable,omitempty"`
	// HandedBy is who last handed the task to its owner, when that was
	// someone other than the owner themselves, so the new owner's Inbox can
	// say "handed to you by Marco" (SPEC "Handing work on"). Cleared when the
	// owner takes it back by their own hand.
	HandedBy *PersonRef `json:"handed_by,omitempty"`
	// ForYou is the reader's own: the comments on it that are for them and
	// not yet read. It is FILLED AT READ TIME for one viewer and never kept.
	ForYou *TaskForYou `json:"for_you,omitempty"`
	// Unopened is the reader's own too: a task handed to them that they have
	// not opened since, which Work lists under "Handed to you" once. FILLED AT
	// READ TIME for one viewer and never kept.
	Unopened bool `json:"unopened,omitempty"`
	// AsksFirst is the step that will make its agent ask the owner before it
	// goes on ("posting", "sending"), from the workstream's ask rule
	// ([AsksFirst]). FILLED AT READ TIME and never kept: a change of the rule
	// changes it for every task at once.
	AsksFirst string `json:"asks_first,omitempty"`
	// StoppedBy is who pressed Stop, while the task is stopped, so the list
	// says "Stopped by you" to that person alone and "Stopped by Marco" to
	// everyone else.
	StoppedBy *PersonRef `json:"stopped_by,omitempty"`
	// Run is what the server knows of the run behind a running task an agent
	// does: whether one is going right now, and since when. It is FILLED AT
	// READ TIME by the organization's server and never kept, so a task that
	// says running with nothing behind it can offer Start (L6).
	Run *TaskRunNow `json:"run,omitempty"`
	// Starts is the task's own word on starting once nothing it waits on is
	// unfinished: TaskStartsWhenReady or TaskStartsByHand. Empty follows the
	// default (StartsBySelf): the plan it belongs to, else its owner's
	// "Start tasks when what they wait on is done".
	Starts string `json:"starts,omitempty"`
	// StartsWhenReady is what Starts resolves to for an Afi or chat task, so
	// the task's facts line can say "Starts when ready" or "Waits for you to
	// start". It is FILLED AT READ TIME and never kept: the owner's default
	// can change without the task changing.
	StartsWhenReady *bool `json:"starts_when_ready,omitempty"`
	// Queue is a started task's place while its run waits for room under the
	// limits (3 runs at once in a workstream, 10 in an organization), read
	// as "Queued · 2nd". FILLED AT READ TIME and never kept.
	Queue *TaskQueue `json:"queue,omitempty"`
}

// TaskQueue is a started task's place in its workstream's queue.
type TaskQueue struct {
	// Place counts from 1: the next of the workstream's tasks to start.
	Place int `json:"place"`
}

// TaskRunNow is a running task's run, as its server sees it now.
type TaskRunNow struct {
	// Live is whether a run is working on the task in the server now.
	Live bool `json:"live"`
	// Since is when the run began (RFC 3339), when the server kept it.
	Since string `json:"since,omitempty"`
	// Now is what the run is doing, in a person's words: its last step
	// ("read the workstream", "made the page Release notes"). Absent before
	// its first step.
	Now string `json:"now,omitempty"`
	// Steps is how many steps the run has taken, and USD what it has spent
	// so far; each is absent while it is zero (the emptiness law).
	Steps int     `json:"steps,omitempty"`
	USD   float64 `json:"usd,omitempty"`
}

// TaskCheckItem is one line of a task's checklist. Who is who ticks it, when
// that is not the task's doer; absent, the doer does.
type TaskCheckItem struct {
	ID   string      `json:"id"`
	Text string      `json:"text"`
	Done bool        `json:"done,omitempty"`
	Who  *TaskDoneBy `json:"who,omitempty"`
}

// The most one checklist holds, and the longest line. ONE SOURCE: the desk's
// wsTasksModel.ts and the tool schemas read these.
const (
	TaskChecklistMost     = 30
	TaskChecklistLineMost = 200
)

// The two words a task's Starts may hold.
const (
	TaskStartsWhenReady = "when_ready"
	TaskStartsByHand    = "by_hand"
)

// TaskForYou is what a task holds for the person reading it (SPEC "Comments
// for you"): the unread comments on a task they own, or that name them with
// @, and the newest of them.
type TaskForYou struct {
	Comments  int                 `json:"comments"`
	Mentioned bool                `json:"mentioned,omitempty"`
	Last      WorkstreamTaskEvent `json:"last"`
}

// TaskEventBy is who an event in the thread is from.
type TaskEventBy struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// TaskOffer is Afi's offer under a comment.
type TaskOffer struct {
	ForEvent string     `json:"for_event"`
	State    string     `json:"state"`
	TakenBy  *PersonRef `json:"taken_by,omitempty"`
}

// WorkstreamTaskEvent is one line of a task's thread.
type WorkstreamTaskEvent struct {
	// Question and Answer retain the two parts of an answered Ask for every surface.
	Question  string      `json:"question,omitempty"`
	Answer    string      `json:"answer,omitempty"`
	ID        string      `json:"id"`
	Task      string      `json:"task"`
	At        string      `json:"at"`
	By        TaskEventBy `json:"by"`
	Kind      string      `json:"kind"`
	Text      string      `json:"text"`
	Steps     int         `json:"steps,omitempty"`
	DurationS int         `json:"duration_s,omitempty"`
	Offer     *TaskOffer  `json:"offer,omitempty"`
	UndoUntil string      `json:"undo_until,omitempty"`
	// Option is the id of the answer an "answered" event chose, so whoever
	// asked (a standing order's clock, on its holder's computer) can act on it.
	Option string `json:"option,omitempty"`
	// Mentions are the handles a comment names with @, among the
	// workstream's people: the comment reaches each of them (SPEC "A comment
	// with @name reaches that person").
	Mentions []string `json:"mentions,omitempty"`
	// File is, on a "made" event, the id of the document the line names.
	File string `json:"file,omitempty"`
	// Typed is, on an "answered" event, what the person typed with their
	// answer (never an option's label, which the asker wrote), so a run
	// started from the answer later may follow an address in it.
	Typed string `json:"typed,omitempty"`
}

// TaskInvalid is a task the store will not keep, answered 422.
type TaskInvalid string

func (e TaskInvalid) Error() string { return string(e) }

func taskInvalidf(format string, args ...any) error { return TaskInvalid(fmt.Sprintf(format, args...)) }

// TaskRefused is a rule refusing who the caller is, answered 403.
type TaskRefused string

func (e TaskRefused) Error() string { return string(e) }

// The refusals a person reads.
var (
	ErrTaskNotOwner     error = TaskRefused("only the task's owner can do this")
	ErrTaskNotAnswerer  error = TaskRefused("only the task's owner answers its question")
	ErrTaskNotStopper   error = TaskRefused("only the task's owner or whoever looks after this workstream can stop or move it")
	ErrTaskNotStarter   error = TaskRefused("only the task's owner or whoever looks after this workstream can say whether it starts by itself")
	ErrTaskNoAsk              = errors.New("this task is not waiting on a question")
	ErrTaskOfferGone          = errors.New("that offer is not open")
	ErrTaskUndoOver           = errors.New("the five seconds to undo this have passed")
	ErrTaskFactoryState       = errors.New("a Factory task's state is its job's: stop, start or answer the job instead")
)

// ── who may do what ──

// TaskActor is the caller as the task rules see them.
type TaskActor struct {
	Handle string
	// Edit is whether they may edit the workstream; view alone reads tasks.
	Edit bool
	// Govern is whether they look after the workstream (its steward, or
	// anyone who may change its people and rules): they may stop or move any
	// of its tasks, which nobody else but the task's owner may.
	Govern bool
}

func (a TaskActor) owns(t WorkstreamTask) bool {
	if t.Owner.Kind == TaskOwnerEveryone {
		return a.Edit
	}
	return a.Edit && a.Handle != "" && NormalizeHandle(t.Owner.ID) == a.Handle
}

// Standin is who answers for a decider who has not answered: the workstream's
// fallback person (ID, a handle; "" for none), and how long an Ask waits on
// its decider before they may (After; zero is [TaskFallbackAfter]).
type Standin struct {
	ID    string
	After time.Duration
}

// Wait is how long an Ask waits before s may answer it.
func (s Standin) Wait() time.Duration {
	if s.After <= 0 {
		return TaskFallbackAfter
	}
	return s.After
}

// Covers is whether s may answer an Ask asked at askedAt, by now: there is a
// stand-in and the Ask has waited past the wait.
func (s Standin) Covers(askedAt string, now time.Time) bool {
	if strings.TrimSpace(s.ID) == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, askedAt)
	return err == nil && now.Sub(at) >= s.Wait()
}

// StandinFor is r's stand-in: its fallback person and the wait it set.
func StandinFor(r WorkstreamRules) Standin {
	s := Standin{After: time.Duration(r.FallbackAfterHours) * time.Hour}
	if r.Fallback != nil {
		s.ID = r.Fallback.ID
	}
	return s
}

// AskPastFallback is whether the task's Ask has waited long enough for the
// fallback person to answer it too, at the default wait.
func AskPastFallback(t WorkstreamTask, now time.Time) bool {
	if t.Ask == nil {
		return false
	}
	return Standin{ID: "-"}.Covers(t.Ask.AskedAt, now)
}

// MayAnswer is whether a may answer t's Ask: its owner (anyone who edits, for
// Everyone), or the stand-in once the Ask has waited past the rules' wait.
func MayAnswer(t WorkstreamTask, a TaskActor, fallback Standin, now time.Time) bool {
	if t.Ask == nil || t.State != TaskNeeds || !a.Edit {
		return false
	}
	if a.owns(t) {
		return true
	}
	return fallback.ID != "" && NormalizeHandle(fallback.ID) == a.Handle && fallback.Covers(t.Ask.AskedAt, now)
}

// FillCoveredBy names, on every open Ask of tasks that has waited past the
// wait, who answers for its decider: the stand-in, unless they are the
// decider. At read time only, so a change of the rules moves every Ask at
// once and nothing stale is ever kept.
func FillCoveredBy(s Standin, name string, tasks []WorkstreamTask, now time.Time) {
	for i := range tasks {
		t := &tasks[i]
		if t.State != TaskNeeds {
			continue
		}
		cover := func(ask *TaskAsk) {
			ask.CoveredBy = nil
			if !s.Covers(ask.AskedAt, now) || NormalizeHandle(AskDecider(*t, *ask).ID) == NormalizeHandle(s.ID) {
				return
			}
			ask.CoveredBy = &PersonRef{ID: NormalizeHandle(s.ID), Name: firstNonEmpty(name, s.ID)}
		}
		if t.Ask != nil {
			a := *t.Ask
			cover(&a)
			t.Ask = &a
		}
		if len(t.Asks) > 0 {
			t.Asks = slices.Clone(t.Asks)
			for k := range t.Asks {
				cover(&t.Asks[k])
			}
		}
	}
}

// MayTick is whether a may tick (or untick) t: a person task, by its owner
// or by the person named as its doer.
// MayStop is whether a may stop or move t: its owner, or whoever looks after
// the workstream (Workstreams final spec, "Who can do what"). Anyone else who
// edits comments, hands it on or sends it back, and asks them to stop it.
func MayStop(t WorkstreamTask, a TaskActor) bool {
	return a.owns(t) || (a.Edit && a.Govern)
}

func MayTick(t WorkstreamTask, a TaskActor) bool {
	if t.DoneBy.Kind != TaskByPerson {
		return false
	}
	return a.owns(t) || (a.Edit && a.Handle != "" && NormalizeHandle(t.DoneBy.ID) == a.Handle)
}

// askToDecider routes a new Ask that names its decider to that decider. The
// task's own Ask is its owner's, so an "ask" naming someone else is raised as
// one of the task's Asks to them (add_ask), and one naming the owner stays the
// task's own. A FIRST ASK GOES TO THE DECIDER THAT WAS NAMED: it used to go to
// the owner whoever it named, so "Priya asks Marco" landed in Priya's own
// Inbox (blackmac fill v2). An answer or a "later" on the open Ask (its id)
// is left alone.
func askToDecider(t WorkstreamTask, p TaskPatch) TaskPatch {
	if p.Ask == nil || p.ClearAsk || p.AddAsk != nil || p.Ask.Decider == nil || strings.TrimSpace(p.Ask.Decider.ID) == "" {
		return p
	}
	if t.Ask != nil && p.Ask.ID != "" && t.Ask.ID == p.Ask.ID {
		return p
	}
	ask := *p.Ask
	if t.Owner.Kind == TaskOwnerPerson && NormalizeHandle(t.Owner.ID) == NormalizeHandle(ask.Decider.ID) {
		ask.Decider = nil
		p.Ask = &ask
		return p
	}
	p.Ask, p.AddAsk = nil, &ask
	return p
}

// IsJobTask is whether id names a Factory job's task, and which job.
func IsJobTask(id string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, TaskJobPrefix))
	return n, strings.HasPrefix(id, TaskJobPrefix) && err == nil && n > 0
}

// JobTaskID is the stable id job n is shown under.
func JobTaskID(n int) string { return TaskJobPrefix + strconv.Itoa(n) }

// ── checks ──

// TaskCreate is what a new task is made from.
type TaskCreate struct {
	Title     string        `json:"title"`
	Request   string        `json:"request,omitempty"`
	Brief     *TaskBrief    `json:"brief,omitempty"`
	Owner     *TaskOwner    `json:"owner,omitempty"`
	DoneBy    TaskDoneBy    `json:"done_by"`
	CameFrom  *TaskCameFrom `json:"came_from,omitempty"`
	WaitingOn []string      `json:"waiting_on,omitempty"`
	Due       string        `json:"due,omitempty"`
	Group     *TaskGroup    `json:"group,omitempty"`
	State     string        `json:"state,omitempty"`
	// Checklist is the deliverable's steps, one line each, unticked.
	Checklist []string `json:"checklist,omitempty"`
	// WordsBy, when a client sends it, says its title and request were not
	// the person's own words ("afi": a desk's Afi wrote them through the
	// person's token). It only ever lowers trust (WordsAuthor): "person", or
	// leaving it out, changes nothing, and older clients send none.
	WordsBy string `json:"words_by,omitempty"`
}

// WordsAuthor is who wrote words sent by a caller of kind by (TaskByPerson,
// TaskByAfi, …) that says claimed of them (a create's or patch's WordsBy). A
// claim only lowers trust: a person's request that says "afi" is Afi's words,
// and nothing makes words a person's that a person's request did not carry.
func WordsAuthor(by, claimed string) string {
	claimed = strings.ToLower(strings.TrimSpace(claimed))
	if by != TaskByPerson || claimed == "" || claimed == TaskByPerson {
		return by
	}
	switch claimed {
	case TaskByAfi, TaskByAgent, TaskByChat, TaskByFactory, TaskByOrder:
		return claimed
	}
	return TaskByAfi
}

// StampWords records author as the writer of t's title and request.
func StampWords(t *WorkstreamTask, author string) { t.TitleBy, t.RequestBy = author, author }

// NewTask makes a task from in, checked. id is the store's; by is the caller;
// defaultOwner is the owner of work nobody asked for directly (the workstream's
// rule, else its steward). Whoever asked owns it by default: the caller, when
// it came from a person or a chat; the default owner when it came from the
// Factory or a standing order.
func NewTask(ws, id string, in TaskCreate, by PersonRef, defaultOwner TaskOwner, all []WorkstreamTask, now time.Time) (WorkstreamTask, error) {
	stamp := now.UTC().Format(time.RFC3339Nano)
	t := WorkstreamTask{
		ID: id, WS: ws, Rev: 1, Title: strings.TrimSpace(in.Title), Request: strings.TrimSpace(in.Request),
		DoneBy: in.DoneBy, State: TaskUpNext, WaitingOn: cleanIDs(in.WaitingOn), Due: strings.TrimSpace(in.Due),
		Results: []TaskResult{}, CreatedBy: by, CreatedAt: stamp, UpdatedAt: stamp,
	}
	brief, err := CleanBrief(in.Brief)
	if err != nil {
		return t, err
	}
	t.Brief = brief
	if in.CameFrom != nil {
		t.CameFrom = *in.CameFrom
	} else {
		t.CameFrom = TaskCameFrom{Kind: TaskByPerson, ID: by.ID, Name: by.Name}
	}
	switch {
	case in.Owner != nil:
		t.Owner = *in.Owner
	case t.CameFrom.Kind == TaskByFactory || t.CameFrom.Kind == TaskByOrder:
		t.Owner = defaultOwner
	default:
		t.Owner = TaskOwner{Kind: TaskOwnerPerson, ID: by.ID, Name: by.Name}
	}
	switch in.State {
	case "", TaskUpNext:
	case TaskRunning:
		if t.DoneBy.Kind == TaskByPerson {
			return t, TaskInvalid("a person task is ticked when done; it does not run")
		}
		t.State = TaskRunning
	default:
		return t, TaskInvalid(`a new task is "up_next" or "running"`)
	}
	if in.Group != nil {
		g := groupFor(*in.Group, all)
		t.Group = &g
	}
	if len(in.Checklist) > 0 {
		lines := make([]TaskCheckItem, 0, len(in.Checklist))
		for _, text := range in.Checklist {
			lines = append(lines, TaskCheckItem{Text: text})
		}
		list, err := cleanChecklist(lines)
		if err != nil {
			return t, err
		}
		t.Checklist = list
	}
	if err := CheckTask(t, all); err != nil {
		return t, err
	}
	return t, nil
}

// groupFor is the group a new task joins. A group named with no id joins the
// workstream's group of that name and kind, or starts one: Afi's batch of five
// tasks names "Release 2.4" five times and gets one group.
func groupFor(g TaskGroup, all []WorkstreamTask) TaskGroup {
	g.Name, g.Kind = strings.TrimSpace(g.Name), strings.TrimSpace(g.Kind)
	if g.Kind == "" {
		g.Kind = "batch"
	}
	if g.ID != "" {
		return g
	}
	for _, t := range all {
		if t.Group != nil && t.Group.Kind == g.Kind && strings.EqualFold(t.Group.Name, g.Name) {
			return *t.Group
		}
	}
	g.ID = "grp_" + strings.ToLower(strings.Join(strings.Fields(g.Kind+" "+g.Name), "-"))
	return g
}

// CheckTask checks one task against the workstream's others (all may hold t
// itself, by id, which is ignored).
func CheckTask(t WorkstreamTask, all []WorkstreamTask) error {
	switch {
	case t.Title == "":
		return TaskInvalid("a task needs a title")
	case len(t.Title) > taskTitleMost:
		return taskInvalidf("a task's title is at most %d characters", taskTitleMost)
	case strings.ContainsAny(t.Title, "\n\r"):
		return TaskInvalid("a task's title is one line")
	case len(t.Request) > taskRequestMost:
		return taskInvalidf("a task's request is at most %d characters", taskRequestMost)
	case len(t.Results) > taskResultsMost:
		return taskInvalidf("a task keeps at most %d results", taskResultsMost)
	}
	if err := checkOwner(t.Owner); err != nil {
		return err
	}
	if err := checkDoneBy(t.DoneBy); err != nil {
		return err
	}
	if t.Due != "" {
		if _, err := time.Parse(time.DateOnly, t.Due); err != nil {
			return TaskInvalid("a due date is YYYY-MM-DD")
		}
	}
	if g := t.Group; g != nil {
		if g.Name == "" || (g.Kind != "batch" && g.Kind != "factory" && g.Kind != "order") {
			return TaskInvalid(`a group has a name and a kind: "batch", "factory" or "order"`)
		}
	}
	for _, r := range t.Results {
		if (r.Kind == "file" && r.FileID == "") || (r.Kind == "url" && r.URL == "") || (r.Kind != "file" && r.Kind != "url") {
			return TaskInvalid(`a result is {"kind":"file","file_id"} or {"kind":"url","url","label"}`)
		}
	}
	return CheckWaits(t.ID, t.WaitingOn, all)
}

func checkOwner(o TaskOwner) error {
	switch o.Kind {
	case TaskOwnerEveryone:
		return nil
	case TaskOwnerPerson:
		if strings.TrimSpace(o.ID) == "" {
			return TaskInvalid("a person owner needs their id")
		}
		return nil
	}
	return TaskInvalid(`an owner is {"kind":"person","id","name"} or {"kind":"everyone"}`)
}

func checkDoneBy(d TaskDoneBy) error {
	switch d.Kind {
	case TaskByAfi:
	case TaskByChat:
		if d.ChatID == "" {
			return TaskInvalid("a chat's task names the chat")
		}
	case TaskByFactory:
		if d.Job <= 0 {
			return TaskInvalid("a Factory task names its job")
		}
	case TaskByPerson:
		if d.ID == "" {
			return TaskInvalid("a person's task names the person")
		}
	case TaskByAgent:
		if strings.TrimSpace(d.AgentID) == "" || strings.TrimSpace(d.Name) == "" {
			return TaskInvalid("an agent's task names the agent: its agent_id and its name")
		}
	default:
		return TaskInvalid(`done_by is afi, agent, chat, factory or person`)
	}
	return nil
}

// MayChangeDoer is whether a may change who does t: its owner, or whoever
// looks after the workstream (org-space DESIGN §4.5).
func MayChangeDoer(t WorkstreamTask, a TaskActor) bool {
	return a.owns(t) || (a.Edit && a.Govern)
}

// CheckWaits checks what task id waits on: tasks of the same workstream (all),
// never itself, never a circle.
func CheckWaits(id string, waits []string, all []WorkstreamTask) error {
	if len(waits) > taskWaitsMost {
		return taskInvalidf("a task waits on at most %d others", taskWaitsMost)
	}
	byID := make(map[string]WorkstreamTask, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}
	for _, w := range waits {
		if w == id {
			return TaskInvalid("a task cannot wait on itself")
		}
		if _, ok := byID[w]; !ok {
			return taskInvalidf("%s is not a task in this workstream", w)
		}
	}
	// A circle is a path from one of waits back to id.
	seen := map[string]bool{}
	var reaches func(from string) bool
	reaches = func(from string) bool {
		if from == id {
			return true
		}
		if seen[from] {
			return false
		}
		seen[from] = true
		return slices.ContainsFunc(byID[from].WaitingOn, reaches)
	}
	if id != "" && slices.ContainsFunc(waits, reaches) {
		return TaskInvalid("that would make tasks wait on each other in a circle")
	}
	return nil
}

func cleanIDs(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ── changes ──

// TaskPatch is any subset of a task. Due "" clears the date; ClearGroup and
// ClearAsk clear those. State "done" on a person task is the tick.
type TaskPatch struct {
	Title   *string `json:"title,omitempty"`
	Request *string `json:"request,omitempty"`
	// WordsBy is TaskCreate's WordsBy for a change of the title or request:
	// it only ever lowers trust.
	WordsBy     string        `json:"words_by,omitempty"`
	Owner       *TaskOwner    `json:"owner,omitempty"`
	WaitingOn   *[]string     `json:"waiting_on,omitempty"`
	Due         *string       `json:"due,omitempty"`
	Group       *TaskGroup    `json:"group,omitempty"`
	ClearGroup  bool          `json:"-"`
	State       *string       `json:"state,omitempty"`
	StateReason *string       `json:"state_reason,omitempty"`
	Results     *[]TaskResult `json:"results,omitempty"`
	Check       *string       `json:"check,omitempty"`
	// Brief replaces the task's brief whole; Handoff sets the doer's note.
	Brief    *TaskBrief `json:"brief,omitempty"`
	Handoff  *string    `json:"handoff,omitempty"`
	Ask      *TaskAsk   `json:"ask,omitempty"`
	ClearAsk bool       `json:"-"`
	MoveTo   string     `json:"move_to,omitempty"`
	// AddAsk raises another Ask, to its Decider: the task waits on it too.
	AddAsk *TaskAsk `json:"add_ask,omitempty"`
	// AskLater is Later on one of Asks, by its decider.
	AskLater *TaskAskLater `json:"ask_later,omitempty"`
	// AskHand hands an open Ask to someone else to decide, by its decider
	// ("Hand to someone else" in the Inbox). The caller checks the new decider
	// is in the workstream.
	AskHand *TaskAskHand `json:"ask_hand,omitempty"`
	// UndoAnswer undoes the answer given to a reversible Ask, by the Ask's id
	// (WorkstreamTask.Undoable): the Ask is open again, the task waits on it,
	// and the decision the answer kept is withdrawn (TaskChange.Withdrawn).
	UndoAnswer string `json:"undo_answer,omitempty"`
	// TakeBack is the owner taking a task back from the agent doing it ("Take
	// it back" in Running for you): Done by becomes the owner, the agent
	// stops, and what it made stays.
	TakeBack bool `json:"take_back,omitempty"`
	// Assumed adds lines to what the agent decided without asking.
	Assumed []string `json:"assumed,omitempty"`
	// Overturn is the id of one of Assumed that Send back overturns, with
	// StateReason the why. A done task goes back to its doer.
	Overturn string `json:"overturn,omitempty"`
	// Checklist replaces the whole checklist (a person adding, rewording,
	// reordering or removing lines). A line with no id is new.
	Checklist *[]TaskCheckItem `json:"checklist,omitempty"`
	// CheckDone and CheckUndone tick and untick lines by id: an agent ticks
	// its own lines as it works, a person ticks theirs.
	CheckDone   []string `json:"check_done,omitempty"`
	CheckUndone []string `json:"check_undone,omitempty"`
	// Starts sets the task's own word on starting by itself (see
	// WorkstreamTask.Starts); "" goes back to the default.
	Starts *string `json:"starts,omitempty"`
	// DoneBy is Change who does it: the owner's or whoever looks after the
	// workstream's, never while the task runs (Stop it first), never on a
	// Factory task (its job is its doer) and never once it is done.
	DoneBy *TaskDoneBy `json:"done_by,omitempty"`
}

// TaskAskLater is Later on one of a task's Asks.
type TaskAskLater struct {
	Ask   string `json:"ask"`
	Until string `json:"until"`
}

// TaskChange is what one change did, for the thread and for the tasks it
// released.
type TaskChange struct {
	Task     WorkstreamTask
	Events   []WorkstreamTaskEvent
	Released []WorkstreamTask
	// Rewait is what waits on a task that was done and is not any more (sent
	// back, unticked), and what waits on a done one that reopens with it:
	// each waits again (see WaitAgain). RewaitOn names, by task id, the
	// reopened task each one waits on, for its "Waiting again" line.
	Rewait   []WorkstreamTask
	RewaitOn map[string]string
	// Decisions are what the change settled, for the workstream's Decisions:
	// an answered Ask, or a request that changed shared work.
	Decisions []WorkstreamDecision
	// Withdrawn are the ids of decisions the change takes back: the one an
	// undone answer kept (TaskPatch.UndoAnswer).
	Withdrawn []string
}

// ApplyPatch changes t by p for actor a, under the rules, and answers the
// events the thread gains. all is the workstream's tasks (t among them), so
// that ticking releases what waits on it. MoveTo is not applied here: the
// caller moves the task between workstreams. newID makes event ids.
//
// Who may change what (DESIGN "What a task contains"): the title and request,
// its owner and whoever asked; the owner (handing it on), anyone who edits;
// the order (waiting on, due date) and its heading, the owner; the results and
// the check line, whoever does the work, which on a server is anyone who
// edits; the state of a person task, its owner alone.
func ApplyPatch(t WorkstreamTask, p TaskPatch, a TaskActor, by TaskEventBy, all []WorkstreamTask, newID func() string, now time.Time) (TaskChange, error) {
	if !a.Edit {
		return TaskChange{}, ErrTaskNotOwner
	}
	p = askToDecider(t, p)
	stamp := now.UTC().Format(time.RFC3339Nano)
	before := t
	asker := a.Handle != "" && NormalizeHandle(t.CreatedBy.ID) == a.Handle
	var events []WorkstreamTaskEvent
	event := func(kind, text string) {
		events = append(events, WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: kind, Text: text})
	}
	if p.Title != nil || p.Request != nil || p.Brief != nil {
		if !a.owns(t) && !asker {
			return TaskChange{}, TaskRefused("only the task's owner and whoever asked for it can change what it asks")
		}
		if p.Title != nil {
			t.Title = strings.TrimSpace(*p.Title)
		}
		if p.Request != nil {
			t.Request = strings.TrimSpace(*p.Request)
		}
		// Each field keeps its own writer: a person fixing a title's typo
		// does not make the request Afi wrote theirs.
		author := WordsAuthor(by.Kind, p.WordsBy)
		if p.Title != nil && t.Title != before.Title {
			t.TitleBy = author
		}
		if p.Request != nil && t.Request != before.Request {
			t.RequestBy = author
		}
		if p.Brief != nil {
			brief, err := CleanBrief(p.Brief)
			if err != nil {
				return TaskChange{}, err
			}
			t.Brief = brief
		}
	}
	if p.Owner != nil && *p.Owner != t.Owner {
		if err := checkOwner(*p.Owner); err != nil {
			return TaskChange{}, err
		}
		t.Owner = *p.Owner
		t.HandedBy = handedBy(t.Owner, by)
		event(TaskEventHanded, "Handed to "+ownerWords(t.Owner))
	}
	if p.TakeBack {
		// TAKE IT BACK (build spec v2, screen 2): the owner does it now. The
		// agent stops (the caller ends its run once the task is not running),
		// what it made stays, and the task waits in Up next on its owner's
		// checkbox, or on whatever a person still asks of it.
		switch {
		case p.DoneBy != nil || p.State != nil:
			return TaskChange{}, TaskInvalid("take it back is its own change: send it without a doer or a state")
		case !a.owns(t) || t.Owner.Kind != TaskOwnerPerson:
			return TaskChange{}, TaskRefused("only the task's owner takes it back")
		case !AgentDoes(t):
			return TaskChange{}, TaskInvalid("no agent is doing this task, so there is nothing to take back")
		case t.State == TaskDone:
			return TaskChange{}, TaskRefused("this task is done: send it back first, then change who does it")
		}
		from := DoerWords(t.DoneBy)
		t.DoneBy = TaskDoneBy{Kind: TaskByPerson, ID: NormalizeHandle(t.Owner.ID), Name: firstNonEmpty(t.Owner.Name, t.Owner.ID)}
		// The agent's own Ask goes with it: nobody is left to act on its
		// answer. A person's Ask stays, and the task keeps waiting on it.
		if t.Ask != nil && (t.Ask.By == nil || t.Ask.By.Kind != TaskByPerson) {
			t.Ask = nil
		}
		t.Asks = slices.DeleteFunc(slices.Clone(t.Asks), func(x TaskAsk) bool { return x.By == nil || x.By.Kind != TaskByPerson })
		if len(t.Asks) == 0 {
			t.Asks = nil
		}
		t.State, t.StateReason, t.StoppedBy = TaskUpNext, "", nil
		if t.Ask != nil || len(t.Asks) > 0 {
			t.State = TaskNeeds
		}
		event(TaskEventHanded, firstNonEmpty(by.Name, by.ID)+" took it back from "+from+". What it made stays")
	}
	if p.DoneBy != nil && *p.DoneBy != t.DoneBy {
		// CHANGE WHO DOES IT (org-space DESIGN §4.5). The brief, the results
		// and the thread stay; work that ended without finishing goes back to
		// Up next for its new doer to start, and one line says who handed it
		// from whom to whom.
		d := *p.DoneBy
		switch {
		case !MayChangeDoer(t, a):
			return TaskChange{}, TaskRefused("only the task's owner or whoever looks after the workstream changes who does it")
		case t.DoneBy.Kind == TaskByFactory:
			return TaskChange{}, TaskRefused("a Factory task is its job: the Factory does it")
		case d.Kind == TaskByFactory:
			return TaskChange{}, TaskInvalid("a Factory task is a Factory job: add the job to the workstream instead")
		case t.State == TaskRunning:
			return TaskChange{}, TaskRefused("this task is running: stop it, then hand it to someone else")
		case t.State == TaskDone:
			return TaskChange{}, TaskRefused("this task is done: send it back first, then change who does it")
		}
		if err := checkDoneBy(d); err != nil {
			return TaskChange{}, err
		}
		from := DoerWords(t.DoneBy)
		t.DoneBy = d
		switch t.State {
		case TaskFailed, TaskInterrupted, TaskStopped, TaskNoReport:
			t.State, t.StateReason, t.StoppedBy = TaskUpNext, "", nil
		}
		event(TaskEventHanded, "from "+from+" to "+DoerWords(d))
	}
	if p.WaitingOn != nil || p.Due != nil || p.Group != nil || p.ClearGroup {
		if !a.owns(t) {
			return TaskChange{}, TaskRefused("only the task's owner changes its order and heading")
		}
		if p.WaitingOn != nil {
			t.WaitingOn = cleanIDs(*p.WaitingOn)
		}
		if p.Due != nil {
			t.Due = strings.TrimSpace(*p.Due)
		}
		if p.ClearGroup {
			t.Group = nil
		} else if p.Group != nil {
			g := groupFor(*p.Group, all)
			t.Group = &g
		}
	}
	if p.Starts != nil && *p.Starts != t.Starts {
		// WHETHER A TASK STARTS BY ITSELF IS THE OWNER'S, or whoever looks
		// after the workstream's (owner decisions 2026-11-17, 1), the same
		// people who may stop it.
		if !MayStop(t, a) {
			return TaskChange{}, ErrTaskNotStarter
		}
		switch *p.Starts {
		case "", TaskStartsWhenReady, TaskStartsByHand:
		default:
			return TaskChange{}, taskInvalidf("starts is %q, %q or empty", TaskStartsWhenReady, TaskStartsByHand)
		}
		t.Starts = *p.Starts
		event(TaskEventState, by.Name+" set it to “"+StartsWords(t.Starts)+"”")
	}
	if p.Results != nil {
		t.Results = slices.Clone(*p.Results)
	}
	if p.Checklist != nil {
		list, err := cleanChecklist(*p.Checklist)
		if err != nil {
			return TaskChange{}, err
		}
		t.Checklist = list
	}
	if len(p.CheckDone) > 0 || len(p.CheckUndone) > 0 {
		list, err := tickChecklist(t.Checklist, p.CheckDone, p.CheckUndone)
		if err != nil {
			return TaskChange{}, err
		}
		t.Checklist = list
	}
	if p.Check != nil {
		t.Check = strings.TrimSpace(*p.Check)
		if t.Check != "" && t.Check != before.Check {
			event(TaskEventChecked, t.Check)
		}
	}
	if p.Handoff != nil {
		t.Handoff = cutWords(strings.TrimSpace(*p.Handoff), TaskHandoffMost)
	}
	if p.ClearAsk {
		t.Ask = nil
	} else if p.Ask != nil {
		ask := *p.Ask
		if t.Ask != nil && t.Ask.ID == ask.ID && !a.owns(t) && ask.LaterUntil != t.Ask.LaterUntil {
			return TaskChange{}, ErrTaskNotAnswerer
		}
		if err := checkAsk(&ask, stamp); err != nil {
			return TaskChange{}, err
		}
		if ask.By == nil && (t.Ask == nil || t.Ask.ID != ask.ID) {
			b := by
			ask.By = &b
		}
		ask.Kind = AskKind(ask.Kind)
		t.Ask = &ask
		if t.State != TaskDone {
			t.State = TaskNeeds
		}
	}
	if p.AddAsk != nil {
		ask := *p.AddAsk
		if ask.Decider == nil || strings.TrimSpace(ask.Decider.ID) == "" {
			return TaskChange{}, TaskInvalid("an Ask names its one decider")
		}
		// AN ASK MAKES THE TASK WAIT, so finished work takes none: there is
		// nothing left to wait, and a done task's Asks close with it. A
		// question about finished work is a comment that names the person.
		if t.State == TaskDone {
			return TaskChange{}, TaskInvalid("this task is done, so nothing waits on an answer; comment and @ them instead")
		}
		if by.Kind == TaskByPerson && NormalizeHandle(ask.Decider.ID) == NormalizeHandle(a.Handle) {
			return TaskChange{}, TaskInvalid("an Ask goes to someone else; you decide your own questions")
		}
		if by.Kind == TaskByPerson && len(ask.Options) == 0 {
			// A PERSON'S ASK IS ANSWERED YES, NO OR IN WORDS (J7: "Yes / No /
			// Reply…"); the answer chosen is the decision, in the question's words.
			ask.Options = []TaskAskOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}}
		}
		if err := checkAsk(&ask, stamp); err != nil {
			return TaskChange{}, err
		}
		if _, _, dup := findAsk(t, ask.ID); dup {
			return TaskChange{}, TaskInvalid("this task already has that Ask")
		}
		ask.Kind = AskKind(ask.Kind)
		if ask.By == nil {
			b := by
			ask.By = &b
		}
		// What the task goes back to once answered: what it was before it
		// waited, or what the Asks it already waits on go back to.
		if t.State != TaskNeeds {
			ask.Then = t.State
		} else if i := slices.IndexFunc(OpenAsks(t), func(x TaskAsk) bool { return x.Then != "" }); i >= 0 {
			ask.Then = OpenAsks(t)[i].Then
		}
		t.Asks = append(slices.Clone(t.Asks), ask)
		t.State = TaskNeeds
		if by.Kind == TaskByPerson {
			who := firstNonEmpty(ask.Decider.Name, ask.Decider.ID)
			event(TaskEventState, firstNonEmpty(by.Name, by.ID)+" asks "+who+": "+ask.Title)
		}
	}
	if l := p.AskLater; l != nil {
		ask, i, ok := findAsk(t, l.Ask)
		if !ok || i < 0 {
			return TaskChange{}, ErrTaskNoAsk
		}
		if ask.Decider == nil || NormalizeHandle(ask.Decider.ID) != a.Handle {
			return TaskChange{}, ErrTaskNotAnswerer
		}
		t.Asks = slices.Clone(t.Asks)
		t.Asks[i].LaterUntil = strings.TrimSpace(l.Until)
	}
	var withdrawn []string
	if h := p.AskHand; h != nil {
		// HAND TO SOMEONE ELSE (build spec v2, screen 1) changes who decides
		// one open Ask, and nothing else: the task, its owner and the Ask's
		// words stay. The task's own Ask becomes one of its Asks, decided by
		// the new person, because the owner still owns the task.
		ask, i, ok := findAsk(t, strings.TrimSpace(h.Ask))
		if !ok || strings.TrimSpace(h.Ask) == "" {
			return TaskChange{}, ErrTaskNoAsk
		}
		decides := a.owns(t)
		if i >= 0 {
			decides = ask.Decider != nil && a.Handle != "" && NormalizeHandle(ask.Decider.ID) == a.Handle
		}
		if !decides {
			return TaskChange{}, TaskRefused("only whoever decides this Ask hands it to someone else")
		}
		to := PersonRef{ID: NormalizeHandle(h.To.ID), Name: strings.TrimSpace(h.To.Name)}
		switch {
		case to.ID == "":
			return TaskChange{}, TaskInvalid("an Ask is handed to one person")
		case to.ID == a.Handle:
			return TaskChange{}, TaskInvalid("you already decide this Ask")
		}
		ask.Decider, ask.LaterUntil = &to, ""
		if i < 0 {
			t.Ask = nil
			t.Asks = append(slices.Clone(t.Asks), ask)
		} else {
			t.Asks = slices.Clone(t.Asks)
			t.Asks[i] = ask
		}
		event(TaskEventState, firstNonEmpty(by.Name, by.ID)+" handed “"+ask.Title+"” to "+firstNonEmpty(to.Name, to.ID)+" to decide")
	}
	if id := strings.TrimSpace(p.UndoAnswer); id != "" {
		// UNDO AN ANSWER (OWNER-DECISIONS 2026-11-17 §13): only a reversible
		// Ask's answer (or a person's Ask's, [AskFromPerson]), only by whoever gave it, and only while the task is
		// not done (a done task is sent back instead). The Ask opens again
		// as it was asked, the task waits on it, and its decision is withdrawn.
		k := slices.IndexFunc(t.Undoable, func(x TaskAnswered) bool { return x.Ask.ID == id })
		if k < 0 {
			return TaskChange{}, TaskInvalid("that answer cannot be undone")
		}
		ans := t.Undoable[k]
		switch {
		case NormalizeHandle(ans.By.ID) != a.Handle:
			return TaskChange{}, TaskRefused("only " + firstNonEmpty(ans.By.Name, ans.By.ID) + " can undo their answer")
		case t.State == TaskDone:
			return TaskChange{}, TaskRefused("this task is done: send it back instead")
		}
		if _, _, open := findAsk(t, id); open {
			return TaskChange{}, TaskInvalid("that Ask is open already")
		}
		ask := ans.Ask
		ask.LaterUntil, ask.Then = "", ""
		if t.State != TaskNeeds {
			ask.Then = t.State
		}
		if ask.Decider == nil && t.Ask != nil {
			// The owner's Ask slot is taken by a newer one: this one waits
			// beside it, decided by whoever answered it.
			who := ans.By
			ask.Decider = &who
		}
		if ask.Decider == nil {
			t.Ask, t.AnsweredBy = &ask, nil
		} else {
			t.Asks = append(slices.Clone(t.Asks), ask)
		}
		t.Undoable = slices.Delete(slices.Clone(t.Undoable), k, k+1)
		if len(t.Undoable) == 0 {
			t.Undoable = nil
		}
		t.State = TaskNeeds
		if ans.Decision != "" {
			withdrawn = append(withdrawn, ans.Decision)
		}
		event(TaskEventState, firstNonEmpty(by.Name, by.ID)+" undid the answer “"+ans.Answer+"”: “"+ask.Title+"” is open again")
	}
	if len(p.Assumed) > 0 {
		t.Assumed = slices.Clone(t.Assumed)
		for _, line := range p.Assumed {
			line = strings.Join(strings.Fields(line), " ")
			if line == "" {
				continue
			}
			if len(line) > taskLineMost {
				return TaskChange{}, taskInvalidf("an assumption is one line, at most %d characters", taskLineMost)
			}
			t.Assumed = append(t.Assumed, TaskAssumed{ID: "as_" + newID(), Text: line, At: stamp, By: by})
		}
		if len(t.Assumed) > taskAssumedMost {
			return TaskChange{}, taskInvalidf("a task lists at most %d assumptions", taskAssumedMost)
		}
	}
	overturned := ""
	if p.Overturn != "" {
		i := slices.IndexFunc(t.Assumed, func(x TaskAssumed) bool { return x.ID == p.Overturn })
		if i < 0 || t.Assumed[i].OverturnedBy != nil {
			return TaskChange{}, TaskInvalid("that assumption is not open")
		}
		t.Assumed = slices.Clone(t.Assumed)
		t.Assumed[i].OverturnedBy = &PersonRef{ID: a.Handle, Name: by.Name}
		overturned = t.Assumed[i].Text
		why := ""
		if p.StateReason != nil {
			why = strings.TrimSpace(*p.StateReason)
		}
		reason := "instead of “" + overturned + "”"
		if why != "" {
			reason += ", " + why
		}
		if t.State == TaskDone {
			// Overturning finished work is Send back: it goes back to its doer.
			up := TaskUpNext
			p.State, p.StateReason = &up, &reason
		} else {
			event(TaskEventState, by.Name+" sent back an assumption: "+reason)
		}
	}
	sentBack := false
	if p.State != nil && *p.State != t.State {
		// A TASK DONE BY SOMEONE INVITED WAITS UNTIL THEY JOIN: it is not
		// failed and nobody starts it for them.
		if *p.State == TaskRunning && t.DoneBy.Kind == TaskByPerson && InvitedPerson(t.DoneBy.ID) {
			return TaskChange{}, taskInvalidf("%s has not joined yet; this waits until they do", t.DoneBy.Name)
		}
		if t.DoneBy.Kind == TaskByFactory {
			return TaskChange{}, ErrTaskFactoryState
		}
		if !validState(*p.State) {
			return TaskChange{}, taskInvalidf("%q is not a task state", *p.State)
		}
		// SEND BACK: a done task back to up next, by someone other than its
		// owner or with a reason. It is an act of its own, never a comment's
		// side effect (live use, L2 #8 and L4 F3): anyone who edits may do it,
		// a reviewer included, and the thread and the task's reason say who
		// sent it back and why, so the doer sees it in the Inbox.
		why := ""
		if p.StateReason != nil {
			why = strings.TrimSpace(*p.StateReason)
		}
		back := t.State == TaskDone && *p.State == TaskUpNext && (!MayTick(t, a) || why != "")
		// WHY IT GOES BACK IS THE POINT OF SEND BACK (live2/E1 F8): a person
		// sending work back says what to change, or the doer's Inbox cannot.
		if back && why == "" && by.Kind == TaskByPerson {
			return TaskChange{}, TaskInvalid("say why it goes back")
		}
		if *p.State == TaskStopped && !MayStop(t, a) {
			return TaskChange{}, ErrTaskNotStopper
		}
		if t.DoneBy.Kind == TaskByPerson && !MayTick(t, a) && !back {
			return TaskChange{}, TaskRefused("only the task's owner or the person doing it ticks it")
		}
		t.State = *p.State
		if back {
			words := "Sent back by " + by.Name
			if why != "" {
				words += ": " + why
			}
			event(TaskEventState, words)
			t.StateReason = words
			sentBack = true
		} else if t.State == TaskStopped && by.Kind == TaskByPerson {
			// WHO STOPPED IT IS KEPT (final spec, States): the thread names
			// them and the list says "you" to them alone.
			event(TaskEventState, "Stopped by "+by.Name)
			t.StoppedBy = &PersonRef{ID: by.ID, Name: by.Name}
		} else {
			event(TaskEventState, StateWords(t.State))
		}
	}
	if t.State != TaskStopped {
		t.StoppedBy = nil
	}
	if p.StateReason != nil && !sentBack {
		t.StateReason = strings.TrimSpace(*p.StateReason)
	}
	switch {
	case t.State == TaskDone && before.State != TaskDone:
		t.DoneAt, t.Ask, t.Asks, t.Undoable = stamp, nil, nil, nil
	case t.State != TaskDone:
		t.DoneAt = ""
	}
	if t.State != TaskFailed && !sentBack {
		t.StateReason = strings.TrimSpace(t.StateReason)
		if p.StateReason == nil {
			t.StateReason = ""
		}
	}
	if err := CheckTask(t, all); err != nil {
		return TaskChange{}, err
	}
	t.Rev++
	t.UpdatedAt = stamp
	ch := TaskChange{Task: t, Events: events, Withdrawn: withdrawn}
	if t.State == TaskDone && before.State != TaskDone {
		ch.Released = Released(t.ID, replaced(all, t), by, newID, now)
	}
	if before.State == TaskDone && t.State != TaskDone {
		ch.Rewait, ch.RewaitOn = rewaiting(t, all)
	}
	return ch, nil
}

// rewaiting is every task that waits on t, which was just reopened, and on
// what. REOPENING A TASK MAKES EVERY TASK THAT WAITS ON IT WAIT AGAIN, a done
// one included (final spec, Tasks › Rules; live2/E1 F7): a done dependant is
// reopened by it, so what waits on THAT one waits again too. A Factory task
// keeps its job's state, so nothing past it is reopened through it. on maps
// each task's id to the title of the reopened task it waits on.
func rewaiting(t WorkstreamTask, all []WorkstreamTask) ([]WorkstreamTask, map[string]string) {
	var out []WorkstreamTask
	on := map[string]string{}
	seen := map[string]bool{t.ID: true}
	queue := []WorkstreamTask{t}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, o := range all {
			if seen[o.ID] || !slices.Contains(o.WaitingOn, cur.ID) {
				continue
			}
			seen[o.ID] = true
			out = append(out, o)
			on[o.ID] = cur.Title
			if o.State == TaskDone && o.DoneBy.Kind != TaskByFactory {
				queue = append(queue, o)
			}
		}
	}
	return out, on
}

// WaitAgain is o, which waits on a task that was just reopened, waiting
// again. REOPENING A TASK MAKES EVERY TASK THAT WAITS ON IT WAIT AGAIN (final
// spec, Tasks › Rules): one already running, or already done, goes back to Up
// next, where its wait shows (live2/E1 F7: a done one used to stay Done
// resting on work that was not); a Factory task keeps its job's state.
// changed says whether o itself changed; the caller writes it and the
// "Waiting again" line either way.
func WaitAgain(o WorkstreamTask, stamp string) (WorkstreamTask, bool) {
	if (o.State != TaskRunning && o.State != TaskDone) || o.DoneBy.Kind == TaskByFactory {
		return o, false
	}
	o.State, o.Rev, o.UpdatedAt, o.DoneAt = TaskUpNext, o.Rev+1, stamp, ""
	return o, true
}

func validState(s string) bool {
	switch s {
	case TaskUpNext, TaskRunning, TaskNeeds, TaskDone, TaskFailed, TaskInterrupted, TaskStopped, TaskNoReport:
		return true
	}
	return false
}

func checkAsk(a *TaskAsk, stamp string) error {
	a.Title, a.Detail = strings.TrimSpace(a.Title), strings.TrimSpace(a.Detail)
	if a.Title == "" {
		return TaskInvalid("a question needs its words")
	}
	if len(a.Options) > taskOptionsMost {
		return taskInvalidf("a question offers at most %d answers", taskOptionsMost)
	}
	if a.ID == "" {
		a.ID = "ask_" + stamp
	}
	if a.AskedAt == "" {
		a.AskedAt = stamp
	}
	if a.Options == nil {
		a.Options = []TaskAskOption{}
	}
	return nil
}

func replaced(all []WorkstreamTask, t WorkstreamTask) []WorkstreamTask {
	out := slices.Clone(all)
	for i := range out {
		if out[i].ID == t.ID {
			out[i] = t
		}
	}
	return out
}

// Released is what finishing task id lets start: every Afi or chat task that
// waited on it, is up next, and now waits on nothing unfinished. THEY STAY UP
// NEXT. Running means something is actually doing the work, and the store
// cannot wake a doer; a task flipped to running here sat there for good with
// nothing behind it (live use, L1 F5, L2 #4, L4 B6). Whoever wakes the doer
// marks it running. The answer is those tasks, unchanged, for the thread's
// "Ready to start" line. A person task is only unblocked (it shows no wait),
// and a Factory task's state stays its job's.
func Released(id string, all []WorkstreamTask, by TaskEventBy, newID func() string, now time.Time) []WorkstreamTask {
	done := map[string]bool{}
	for _, t := range all {
		if t.State == TaskDone {
			done[t.ID] = true
		}
	}
	var out []WorkstreamTask
	for _, t := range all {
		if !slices.Contains(t.WaitingOn, id) || t.State != TaskUpNext || !AgentDoes(t) {
			continue
		}
		if slices.ContainsFunc(t.WaitingOn, func(w string) bool { return !done[w] }) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// AnswerTask answers the Ask askID names on t (empty: the task's own) with
// option (an id the Ask offers) or words. The first answer decides: an Ask
// already answered is ErrTaskNoAsk. The task runs again once nothing else is
// asked; while another Ask is open it keeps waiting. The answer records a
// decision (TaskChange.Decisions), made by whoever answered. The caller holds
// a task that still waits on unfinished work in Up next (AnsweredWaiting),
// with the workstream's tasks as they are at the write.
func AnswerTask(t WorkstreamTask, askID, option, words string, a TaskActor, by TaskEventBy, fallback Standin, newID func() string, now time.Time) (TaskChange, error) {
	ask, i, ok := findAsk(t, askID)
	if !ok || t.State != TaskNeeds {
		return TaskChange{}, ErrTaskNoAsk
	}
	if !MayAnswerAsk(t, ask.ID, a, fallback, now) {
		if i >= 0 {
			return TaskChange{}, TaskRefused("only " + firstNonEmpty(ask.Decider.Name, ask.Decider.ID) + " answers this Ask")
		}
		return TaskChange{}, ErrTaskNotAnswerer
	}
	words = strings.TrimSpace(words)
	chose := ""
	var opt *TaskAskOption
	if option != "" {
		k := slices.IndexFunc(ask.Options, func(o TaskAskOption) bool { return o.ID == option })
		if k < 0 {
			return TaskChange{}, taskInvalidf("%q is not one of this question's answers", option)
		}
		opt = &ask.Options[k]
		chose = opt.Label
		if opt.Words && words == "" {
			return TaskChange{}, taskInvalidf("%q needs your own words with it", opt.Label)
		}
	}
	if chose == "" && words == "" {
		return TaskChange{}, TaskInvalid("an answer is one of the question's answers, or words")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	text := ask.Title + " · " + strings.TrimSpace(strings.Join(nonEmpty(chose, words), " · "))
	who := PersonRef{ID: a.Handle, Name: by.Name}
	if i < 0 {
		t.Ask, t.AnsweredBy = nil, &who
	} else {
		t.Asks = slices.Delete(slices.Clone(t.Asks), i, i+1)
		if len(t.Asks) == 0 {
			t.Asks = nil
		}
	}
	if t.Ask == nil && len(t.Asks) == 0 {
		t.State = TaskRunning
		if ask.Then != "" {
			t.State = ask.Then
		}
	}
	if i < 0 && IsCantDoAsk(ask) && t.Ask == nil && len(t.Asks) == 0 {
		switch option {
		case CantDoPerson:
			// The owner does it: their checkbox, up next.
			doer := who
			if t.Owner.Kind == TaskOwnerPerson && t.Owner.ID != "" {
				doer = PersonRef{ID: t.Owner.ID, Name: firstNonEmpty(t.Owner.Name, t.Owner.ID)}
			}
			t.DoneBy = TaskDoneBy{Kind: TaskByPerson, ID: doer.ID, Name: doer.Name}
			t.State = TaskUpNext
		case CantDoDrop:
			t.State, t.StoppedBy = TaskStopped, &who
		}
	}
	if IsInviteChaseAsk(ask) {
		t = answerInviteChase(t, option, who)
	}
	t.Rev, t.UpdatedAt = t.Rev+1, stamp
	ch := TaskChange{Task: t, Events: []WorkstreamTaskEvent{{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventAnswered, Text: text, Option: option, Question: ask.Title, Answer: strings.TrimSpace(strings.Join(nonEmpty(chose, words), " · "))}}}
	d, kept := answerDecision(t, ask, opt, words, who, "dc_"+newID(), now)
	if kept {
		ch.Decisions = append(ch.Decisions, d)
	}
	// An answer given in the person's own words (the option that asks for
	// them) is not taken back by a click, even on a reversible Ask: its plain
	// buttons stay undoable, it does not.
	wordsAnswer := opt != nil && opt.Words
	if (ask.Reversible && !wordsAnswer || AskFromPerson(ask)) && t.State != TaskDone {
		// A REVERSIBLE ASK'S ANSWER CAN BE UNDONE FROM ITS TASK: kept here
		// with the decision it made, so the undo withdraws exactly that one.
		// So can the answer to a person's question: it lets nothing happen by
		// itself, it only tells them, so a slip is taken back before they act.
		keep := TaskAnswered{Ask: ask, Option: option, Answer: strings.Join(nonEmpty(chose, words), " · "), By: who, At: stamp}
		if kept {
			keep.Decision = d.ID
		}
		list := append(slices.Clone(ch.Task.Undoable), keep)
		if len(list) > TaskUndoableMost {
			list = list[len(list)-TaskUndoableMost:]
		}
		ch.Task.Undoable = list
	}
	return ch, nil
}

// UnfinishedWaits is what t waits on that is not done, in t's own order. A
// task it names that is not in all (gone, or another workstream's) counts as
// unfinished: nothing says it is done.
func UnfinishedWaits(t WorkstreamTask, all []WorkstreamTask) []WorkstreamTask {
	byID := map[string]WorkstreamTask{}
	for _, o := range all {
		byID[o.ID] = o
	}
	var out []WorkstreamTask
	for _, id := range t.WaitingOn {
		o, ok := byID[id]
		if !ok {
			o = WorkstreamTask{ID: id}
		}
		if o.State != TaskDone {
			out = append(out, o)
		}
	}
	return out
}

// AnsweredWaiting is t, just answered (AnswerTask), held in Up next while it
// still waits on unfinished work (unfinished, from UnfinishedWaits). AN ANSWER
// RECORDS THE DECISION; IT NEVER STARTS A TASK WHOSE WAITS ARE NOT DONE (the
// blackmac demo, 2026-11-17: answering the launch-date task's Ask started the
// blog post before the date was confirmed). Up next is where a wait shows, and
// the task starts by itself, the answer in its thread, once that work is done
// (Released). ok says whether t was held; line is its thread's words.
func AnsweredWaiting(t WorkstreamTask, unfinished []WorkstreamTask) (held WorkstreamTask, line string, ok bool) {
	if t.State != TaskRunning || len(unfinished) == 0 {
		return t, "", false
	}
	var names []string
	for _, o := range unfinished {
		names = append(names, firstNonEmpty(strings.TrimSpace(o.Title), "another task"))
	}
	t.State = TaskUpNext
	return t, "Up next: waits for " + strings.Join(names, ", ") + " before it starts", true
}

func nonEmpty(s ...string) []string {
	return slices.DeleteFunc(slices.Clone(s), func(x string) bool { return x == "" })
}

// TaskNote is a line someone adds to a thread: a comment, Afi's offer under
// one (OfferFor), a private request's one note, or an answer posted from a
// private chat.
type TaskNote struct {
	Kind string `json:"kind,omitempty"`
	// Decision is, on a request_note, what the request settles for shared
	// work in one line; it is kept in the workstream's Decisions.
	Decision string `json:"decision,omitempty"`
	Text     string `json:"text"`
	OfferFor string `json:"offer_for,omitempty"`
	// File is, on a "made" line, the document it names.
	File      string `json:"file,omitempty"`
	Steps     int    `json:"steps,omitempty"`
	DurationS int    `json:"duration_s,omitempty"`
}

// Note adds n to t's thread and never changes t's state (the law below). An offer hangs under a comment of the same thread and only Afi
// or a chat makes one.
func Note(t WorkstreamTask, n TaskNote, a TaskActor, by TaskEventBy, thread []WorkstreamTaskEvent, newID func() string, now time.Time) (TaskChange, error) {
	if !a.Edit {
		return TaskChange{}, TaskRefused("commenting on a task needs edit on its workstream")
	}
	if n.Kind == "" {
		n.Kind = TaskEventComment
	}
	n.Text = strings.TrimSpace(n.Text)
	switch {
	case n.Text == "":
		return TaskChange{}, TaskInvalid("a comment needs words")
	case len(n.Text) > taskTextMost:
		return TaskChange{}, taskInvalidf("a comment is at most %d characters", taskTextMost)
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	ev := WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: n.Kind, Text: n.Text}
	switch n.Kind {
	case TaskEventComment, TaskEventPosted, TaskEventRequestNote:
	case TaskEventWorked:
		ev.Steps, ev.DurationS = max(0, n.Steps), max(0, n.DurationS)
	case TaskEventOffer:
		if by.Kind != TaskByAfi && by.Kind != TaskByChat {
			return TaskChange{}, TaskInvalid("only Afi or a chat offers")
		}
		if !slices.ContainsFunc(thread, func(e WorkstreamTaskEvent) bool { return e.ID == n.OfferFor && e.Kind == TaskEventComment }) {
			return TaskChange{}, TaskInvalid("an offer hangs under a comment on the same task")
		}
		ev.Offer = &TaskOffer{ForEvent: n.OfferFor, State: OfferOpen}
	case TaskEventMade:
		if by.Kind == TaskByPerson {
			return TaskChange{}, TaskInvalid("only an agent's work leaves a made line")
		}
		if ev.File = strings.TrimSpace(n.File); ev.File == "" {
			return TaskChange{}, TaskInvalid("a made line names the file it made")
		}
	default:
		return TaskChange{}, taskInvalidf("%q is not a line a thread takes", n.Kind)
	}
	// A COMMENT NEVER CHANGES A TASK'S STATE. A person logs a receipt or news
	// on finished work and must not find it undone, with everything after it
	// blocked again (live use, L4 F3). Reopening is its own act: the owner's
	// untick, or Send back (ApplyPatch).
	return TaskChange{Task: t, Events: []WorkstreamTaskEvent{ev}}, nil
}

// SetOffer takes, dismisses or undoes Afi's offer ev. Anyone who edits may
// take or dismiss an open offer; taking it gives the owner five seconds to
// undo (UndoUntil); an undo puts it back open and only the owner, or the one
// who took it, undoes, and only within those seconds.
func SetOffer(t WorkstreamTask, ev WorkstreamTaskEvent, state string, a TaskActor, by TaskEventBy, now time.Time) (WorkstreamTaskEvent, error) {
	if ev.Offer == nil || ev.Kind != TaskEventOffer {
		return ev, ErrTaskOfferGone
	}
	if !a.Edit {
		return ev, ErrTaskNotOwner
	}
	o := *ev.Offer
	switch state {
	case OfferTaken, OfferDismissed:
		if o.State != OfferOpen {
			return ev, ErrTaskOfferGone
		}
		o.State = state
		ev.UndoUntil = ""
		if state == OfferTaken {
			o.TakenBy = &PersonRef{ID: a.Handle, Name: by.Name}
			ev.UndoUntil = now.Add(TaskOfferUndo).UTC().Format(time.RFC3339Nano)
		}
	case OfferOpen:
		if o.State != OfferTaken {
			return ev, ErrTaskOfferGone
		}
		took := o.TakenBy != nil && NormalizeHandle(o.TakenBy.ID) == a.Handle
		if !a.owns(t) && !took {
			return ev, ErrTaskNotOwner
		}
		until, err := time.Parse(time.RFC3339Nano, ev.UndoUntil)
		if err != nil || now.After(until) {
			return ev, ErrTaskUndoOver
		}
		o.State, o.TakenBy, ev.UndoUntil = OfferOpen, nil, ""
	default:
		return ev, TaskInvalid(`an offer is "taken", "dismissed" or "open" (undo)`)
	}
	ev.Offer = &o
	return ev, nil
}

// TASKS START BY THEMSELVES WHEN THEIR WAITS CLEAR (owner decisions
// 2026-11-17, 1: "the person decides once, and the work carries on").
// StartsBySelf is whether t, released by the last task it waited on, starts
// without another click. In order:
//
//   - only an Afi or chat task starts by itself: a person's task lands in
//     that person's Waiting on you and Due, and a Factory job starts at the
//     Factory;
//   - the task's own word (Starts) wins;
//   - a plan nobody has started yet waits for its Start (StartAllAt or more
//     tasks in one batch, every one still up next), and a plan that was
//     started is consent for the whole plan;
//   - otherwise the owner's "Start tasks when what they wait on is done",
//     which is on unless they turned it off (ownerDefault).
//
// Nothing else changes: an Ask, the spending rule and the decision check
// still pause the task once it runs.
func StartsBySelf(t WorkstreamTask, all []WorkstreamTask, ownerDefault bool) bool {
	if t.DoneBy.Kind != TaskByAfi && t.DoneBy.Kind != TaskByChat {
		return false
	}
	switch t.Starts {
	case TaskStartsWhenReady:
		return true
	case TaskStartsByHand:
		return false
	}
	if started, plan := planStarted(t, all); plan {
		return started
	}
	return ownerDefault
}

// StartsWords is the facts line's words for a task's own Starts; empty is
// the default's words, which the reader resolves.
func StartsWords(starts string) string {
	switch starts {
	case TaskStartsWhenReady:
		return "Starts when ready"
	case TaskStartsByHand:
		return "Waits for you to start"
	}
	return "Starts as its owner's settings say"
}

// StartAllAt is how many tasks of one batch make it a plan that waits for
// Start (the spec's "five or more tasks from one request").
const StartAllAt = 5

// planStarted says whether t belongs to a plan, and if so whether anything in
// it has started.
func planStarted(t WorkstreamTask, all []WorkstreamTask) (started, plan bool) {
	if t.Group == nil || t.Group.Kind != "batch" {
		return false, false
	}
	n := 0
	for _, o := range all {
		if o.Group == nil || o.Group.ID != t.Group.ID {
			continue
		}
		n++
		if o.State != TaskUpNext {
			started = true
		}
	}
	return started, n >= StartAllAt
}

// StartAll names a group's Afi and chat tasks that are up next and wait on
// nothing unfinished: the ones Start all can start. It changes nothing.
// Running means a doer is actually at work, and only whoever wakes that doer
// can say so; marking them running here left them running for good with
// nothing behind them (live use, L4 B6). Person tasks and Factory tasks are
// never named (a person starts their own; a Factory job starts at the Factory).
func StartAll(group string, all []WorkstreamTask, now time.Time) []WorkstreamTask {
	done := map[string]bool{}
	for _, t := range all {
		if t.State == TaskDone {
			done[t.ID] = true
		}
	}
	var out []WorkstreamTask
	for _, t := range all {
		if t.Group == nil || t.Group.ID != group || t.State != TaskUpNext || !AgentDoes(t) {
			continue
		}
		if slices.ContainsFunc(t.WaitingOn, func(w string) bool { return !done[w] }) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// StateWords are the words a person reads for a state: the app's own for
// unfinished work.
func StateWords(s string) string {
	switch s {
	case TaskUpNext:
		return "Up next"
	case TaskRunning:
		return "Running"
	case TaskNeeds:
		return "Needs someone"
	case TaskDone:
		return "Done"
	case TaskFailed:
		return "Failed"
	case TaskInterrupted:
		return "Interrupted"
	case TaskStopped:
		return "Stopped by you"
	case TaskNoReport:
		return "Didn't report back"
	}
	return s
}

func ownerWords(o TaskOwner) string {
	if o.Kind == TaskOwnerEveryone {
		return "everyone"
	}
	if o.Name != "" {
		return o.Name
	}
	return o.ID
}

// ── a Factory job, read as a task ──

// JobTaskState is a job's task state and, for Failed, its reason. KEEP IN STEP
// WITH the desk's wsTasksModel.ts jobTaskState: the same job reads the same in
// Personal and on an organization.
//
//   - done → Done; a person's stop → Stopped by you;
//   - a failure: the machine went silent (machine_lost) → Didn't report back;
//     a crash or a lost connection → Interrupted; anything else → Failed;
//   - waiting on a person → Needs someone; running → Running;
//   - queued or in Backlog → Up next.
func JobTaskState(it Item) (string, string) {
	js := JobState(it)
	switch {
	case js == "done":
		return TaskDone, ""
	case StoppedByPerson(it):
		return TaskStopped, ""
	case it.Failure != nil:
		switch it.Failure.Code {
		case CodeMachineLost:
			return TaskNoReport, ""
		case CodeCrash, CodeTransportError:
			return TaskInterrupted, ""
		}
		reason := it.Failure.Summary
		if reason == "" {
			reason = it.Failure.Detail
		}
		return TaskFailed, reason
	case js == "needs_you":
		return TaskNeeds, ""
	case js == "running":
		return TaskRunning, ""
	}
	return TaskUpNext, ""
}

// JobTaskAsk is a job's open question as its task's Ask, nil when it waits on
// nobody. A step's question offers its own answers; any other wait is a yes or
// no ("Yes" is the factory's recommendation).
func JobTaskAsk(it Item) *TaskAsk {
	if st, _ := JobTaskState(it); st != TaskNeeds {
		return nil
	}
	at := NeedsSince(it)
	if at.IsZero() {
		at = it.Created
	}
	ask := &TaskAsk{ID: fmt.Sprintf("job:%d:%s", it.ID, at.UTC().Format(time.RFC3339Nano)), AskedAt: at.UTC().Format(time.RFC3339Nano)}
	if q := it.Asking; q != nil && q.With == AskedYou && strings.TrimSpace(q.Question) != "" {
		ask.Title, ask.Detail = strings.TrimSpace(q.Question), strings.TrimSpace(q.Why)
		for _, o := range q.Options {
			ask.Options = append(ask.Options, TaskAskOption{ID: o, Label: o, Recommended: o == q.Pick})
		}
	} else if NeedsKind(it) == NeedsPublishFailed {
		// A PUBLISH THAT FAILED SAYS WHY ON THE TASK: the person answering
		// sees what is needed and where (connect GitHub, say), never a bare
		// "Go on with". Yes is Retry publishing, as on the job.
		ask.Title, ask.Detail = "Publishing "+it.Ref()+" failed", strings.TrimSpace(it.PublishError)
		if code := publishCode(it); code == PublishGitHubNotConnected || code == PublishGitHubOtherAccount {
			ask.Title = "Connect GitHub to publish " + it.Ref()
		}
		if ask.Detail == "" && it.PublishStatus != nil {
			ask.Detail, _, _ = strings.Cut(strings.TrimSpace(it.PublishStatus.Detail), "\n")
		}
		ask.Options = []TaskAskOption{{ID: "yes", Label: "Retry publishing", Recommended: true}, {ID: "no", Label: "Not now"}}
	} else {
		ask.Title = strings.TrimSpace(it.Question)
		if ask.Title == "" {
			ask.Title = "Go on with " + it.Ref() + "?"
		}
	}
	if len(ask.Options) == 0 {
		ask.Options = []TaskAskOption{{ID: "yes", Label: "Yes", Recommended: true}, {ID: "no", Label: "No"}}
	}
	return ask
}

// The publish codes whose remedy is a GitHub connection. They are spelled
// here, where a task reads them, and the publish vocabulary names them from
// here ([publish.CodeGitHubNotConnected]); the factory cannot import publish.
const (
	PublishGitHubNotConnected = "github_not_connected"
	PublishGitHubOtherAccount = "github_other_account"
)

// publishCode is the code of the job's last failed publish, "" when none.
func publishCode(it Item) string {
	if it.PublishStatus == nil {
		return ""
	}
	return it.PublishStatus.Code
}

// JobTask is job it read as a task of workstream ws. overlay is what a person
// set on it (owner, heading, waits, due), nil for none; defaultOwner owns a
// job nobody holds. Nothing of the job is copied: every read derives it again.
func JobTask(it Item, ws string, overlay *WorkstreamTask, defaultOwner TaskOwner) WorkstreamTask {
	state, reason := JobTaskState(it)
	created := it.Created.UTC().Format(time.RFC3339Nano)
	t := WorkstreamTask{
		ID: JobTaskID(it.ID), WS: ws, Title: it.Title, Request: firstLines(it.Body, 600),
		DoneBy: TaskDoneBy{Kind: TaskByFactory, Job: it.ID}, CameFrom: TaskCameFrom{Kind: TaskByFactory, Issue: jobIssue(it)},
		State: state, StateReason: reason, WaitingOn: []string{}, Results: []TaskResult{},
		Ask: JobTaskAsk(it), CreatedBy: PersonRef{ID: "factory", Name: "the factory"}, CreatedAt: created, UpdatedAt: created,
		Group: &TaskGroup{ID: "grp_factory", Name: "Factory", Kind: "factory"},
	}
	if who := NormalizeHandle(it.Assignee); who != "" {
		t.Owner = TaskOwner{Kind: TaskOwnerPerson, ID: who, Name: who}
	} else {
		t.Owner = defaultOwner
	}
	if spent := JobLifetimeSpent(it); spent > 0 {
		t.CostUSD = &spent
	}
	if pr := it.PullRequest; pr != nil && pr.URL != "" {
		t.Results = append(t.Results, TaskResult{Kind: "url", URL: pr.URL, Label: fmt.Sprintf("Pull request %d", pr.Number)})
	}
	var claims []string
	for _, c := range it.Proof {
		if c.OK && strings.TrimSpace(c.Text) != "" {
			claims = append(claims, strings.TrimSpace(strings.Join(nonEmpty(strings.TrimSpace(c.Text), strings.TrimSpace(c.Evidence)), " · ")))
		}
	}
	t.Check = strings.Join(claims, "\n")
	if state == TaskRunning || state == TaskNeeds {
		t.Step = jobStepWords(it)
	}
	if state == TaskDone && !it.DoneAt.IsZero() {
		t.DoneAt = it.DoneAt.UTC().Format(time.RFC3339Nano)
		t.UpdatedAt = t.DoneAt
	}
	if o := overlay; o != nil {
		t.Rev, t.Owner, t.WaitingOn, t.Due, t.Group = o.Rev, o.Owner, nonNilIDs(o.WaitingOn), o.Due, o.Group
		if o.UpdatedAt > t.UpdatedAt {
			t.UpdatedAt = o.UpdatedAt
		}
		if o.AnsweredBy != nil {
			t.AnsweredBy = o.AnsweredBy
		}
		// Views on the job's own question, and any other Ask raised on it,
		// are the overlay's: the job knows neither.
		if o.Ask != nil && t.Ask != nil && o.Ask.ID == t.Ask.ID {
			t.Ask.Views = o.Ask.Views
		}
		t.Asks, t.Assumed = o.Asks, o.Assumed
	}
	return t
}

// JobOverlay is the part of a Factory task a person may set, kept under the
// job's id: its owner, what it waits on, its due date and heading.
func JobOverlay(t WorkstreamTask) WorkstreamTask {
	return WorkstreamTask{
		ID: t.ID, WS: t.WS, Rev: t.Rev, Owner: t.Owner, DoneBy: t.DoneBy, CameFrom: t.CameFrom, State: TaskUpNext,
		WaitingOn: nonNilIDs(t.WaitingOn), Due: t.Due, Group: t.Group, Results: []TaskResult{},
		Title: t.Title, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, AnsweredBy: t.AnsweredBy,
		Ask: overlayAsk(t.Ask), Asks: t.Asks, Assumed: t.Assumed,
	}
}

// overlayAsk is what an overlay keeps of a job's own question: its id and
// the views on it, nothing the job itself says.
func overlayAsk(a *TaskAsk) *TaskAsk {
	if a == nil || len(a.Views) == 0 {
		return nil
	}
	return &TaskAsk{ID: a.ID, Views: a.Views, Options: []TaskAskOption{}}
}

func nonNilIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// jobStepWords is "Step 4 of 4, Review": the one step count ([StepCount])
// and the step the run is on, by its name.
func jobStepWords(it Item) string {
	done, total := StepCount(it)
	if total == 0 {
		return ""
	}
	at := min(done+1, total)
	name := ""
	if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		name = s.Phases[s.Cur].Name
	}
	if name == "" {
		return fmt.Sprintf("Step %d of %d", at, total)
	}
	return fmt.Sprintf("Step %d of %d, %s", at, total, strings.ToUpper(name[:1])+name[1:])
}

func jobIssue(it Item) string {
	if it.SourceIssue != "" {
		return it.SourceIssue
	}
	if it.Kind == KindIssue {
		return it.Ref()
	}
	return ""
}

func firstLines(s string, most int) string {
	s = strings.TrimSpace(s)
	if len(s) <= most {
		return s
	}
	cut := strings.LastIndexAny(s[:most], " \n")
	if cut < most/2 {
		cut = most
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// ── when the people or the files change under a task ──

// HandOn hands t to `to` when its owner, or the person who does it, has left
// the workstream (removed, by normalized handle): the rules' fallback, else
// the steward. Done work keeps who did it; a task nobody left is unchanged
// (false). The thread says "Handed to <to>", as a hand-off by hand does.
func HandOn(t WorkstreamTask, removed map[string]bool, to, name string, by TaskEventBy, newID func() string, now time.Time) (WorkstreamTask, []WorkstreamTaskEvent, bool) {
	to = NormalizeHandle(to)
	name = firstNonEmpty(name, to)
	if t.State == TaskDone || to == "" || removed[to] {
		return t, nil, false
	}
	changed := false
	if t.Owner.Kind == TaskOwnerPerson && removed[NormalizeHandle(t.Owner.ID)] {
		t.Owner, changed = TaskOwner{Kind: TaskOwnerPerson, ID: to, Name: name}, true
		t.HandedBy = handedBy(t.Owner, by)
	}
	if t.DoneBy.Kind == TaskByPerson && removed[NormalizeHandle(t.DoneBy.ID)] {
		t.DoneBy, changed = TaskDoneBy{Kind: TaskByPerson, ID: to, Name: name}, true
	}
	if !changed {
		return t, nil, false
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	t.Rev, t.UpdatedAt = t.Rev+1, stamp
	return t, []WorkstreamTaskEvent{{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventHanded, Text: "Handed to " + name}}, true
}

// TaskRequest is a request into a workstream: what is wanted, and the work
// it came from, when it came from a task elsewhere.
type TaskRequest struct {
	Title   string `json:"title"`
	Request string `json:"request,omitempty"`
	// WordsBy is TaskCreate's WordsBy: it only ever lowers trust.
	WordsBy  string `json:"words_by,omitempty"`
	FromWS   string `json:"from_ws,omitempty"`
	FromTask string `json:"from_task,omitempty"`
}

// RequestTask is the task a request into ws becomes: owned by ws's owner of
// incoming work, who also does it when that is a person (they decide how),
// Afi when nobody in particular owns it; up next, never started on its own.
func RequestTask(ws, id string, in TaskRequest, by PersonRef, from TaskCameFrom, incoming TaskOwner, all []WorkstreamTask, now time.Time) (WorkstreamTask, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return WorkstreamTask{}, TaskInvalid("a request needs a title")
	}
	doer := TaskDoneBy{Kind: TaskByAfi}
	if incoming.Kind == TaskOwnerPerson {
		doer = TaskDoneBy{Kind: TaskByPerson, ID: incoming.ID, Name: incoming.Name}
	}
	owner := incoming
	return NewTask(ws, id, TaskCreate{Title: in.Title, Request: in.Request, Owner: &owner, DoneBy: doer, CameFrom: &from}, by, incoming, all, now)
}

// handedBy is who handed a task to owner: whoever did it, unless the new
// owner took it themselves or nobody in particular owns it now.
func handedBy(owner TaskOwner, by TaskEventBy) *PersonRef {
	if owner.Kind != TaskOwnerPerson || (by.Kind == TaskByPerson && NormalizeHandle(by.ID) == NormalizeHandle(owner.ID)) {
		return nil
	}
	return &PersonRef{ID: by.ID, Name: by.Name}
}

// Mentions are the people a comment names with @, as handles, each once and
// in the order written. `people` maps each handle in the workstream to the
// name they go by; "@abir", "@Abir" and "@AbirDas" all reach abir, and an @
// that names nobody in the workstream reaches nobody.
func Mentions(text string, people map[string]string) []string {
	key := func(s string) string {
		return strings.ToLower(strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
				return r
			}
			return -1
		}, s))
	}
	who := map[string]string{}
	for handle, name := range people {
		h := NormalizeHandle(handle)
		first := ""
		if f := strings.Fields(name); len(f) > 0 {
			first = f[0]
		}
		for _, k := range []string{key(handle), key(name), key(first)} {
			if k != "" {
				if _, taken := who[k]; !taken || k == key(handle) {
					who[k] = h
				}
			}
		}
	}
	var out []string
	for _, word := range strings.Fields(text) {
		i := strings.IndexByte(word, '@')
		if i < 0 || (i > 0 && !strings.ContainsRune(" ([{\"'", rune(word[i-1]))) {
			continue
		}
		k := strings.TrimRight(key(word[i+1:]), ".-")
		if h, ok := who[k]; ok && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// ── "CAN'T DO IT" IS AN ASK, NOT FAILED (owner decisions §10, Q4) ──
//
// An agent that lacks an input or a capability (the server's Afi has no web)
// used to set its task Failed with an honest reason, which turned it red when
// it was really a question for the owner. Now the doer's door turns that into
// this Ask on the task, to its owner, and Failed is kept for errors. Its three
// answers act, and none of them is a decision of the workstream's: the reason
// an agent could not go on is not something its people settled.
//
//   - Paste what it needs: the owner's words go back to the doer, and the task
//     runs again with them.
//   - Do it myself: the owner takes it back (Done by becomes them, as the
//     owner-decisions v2 board "Who does what" spells taking work back); it
//     becomes their checkbox.
//   - Drop it: the task is stopped, by the owner.

// CantDoAskID prefixes the Ask a doer raises when it cannot do its task.
const CantDoAskID = "ask_cant_"

// The answers a can't-do Ask offers.
const (
	CantDoPaste  = "paste"
	CantDoPerson = "person"
	CantDoDrop   = "drop"
)

// CantDoAsk is the Ask that replaces Failed when doer could not do the task,
// with why in its own words. The owner may answer with words alone too.
func CantDoAsk(doer, why, id string) TaskAsk {
	return TaskAsk{
		ID: CantDoAskID + id, Title: "Can you give " + firstNonEmpty(doer, "the agent") + " what it needs?",
		Detail: cutWords(strings.TrimSpace(why), 1000),
		Options: []TaskAskOption{
			{ID: CantDoPaste, Label: "Paste what it needs", Recommended: true, Words: true},
			{ID: CantDoPerson, Label: "Do it myself"},
			{ID: CantDoDrop, Label: "Drop it"},
		},
	}
}

// IsCantDoAsk is whether ask is a doer's can't-do Ask.
func IsCantDoAsk(ask TaskAsk) bool { return strings.HasPrefix(ask.ID, CantDoAskID) }

// FileChangedAskID prefixes the Ask a changed file puts on a running task.
const FileChangedAskID = "ask_file_"

// FlagForFile marks a RUNNING task whose agent read `file` before someone
// changed it (DESIGN: "Marco's running task is flagged because a file it read
// has changed, and asks him whether to continue"). An Afi, chat or person task
// stops on a question to its owner, either answer carries on with the new
// version and the thread says which; a Factory task's state is its job's, so
// it only gains the line. Anything not running is unchanged (false).
func FlagForFile(t WorkstreamTask, file string, by TaskEventBy, newID func() string, now time.Time) (WorkstreamTask, []WorkstreamTaskEvent, bool) {
	if t.State != TaskRunning {
		return t, nil, false
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	line := WorkstreamTaskEvent{ID: newID(), Task: t.ID, At: stamp, By: by, Kind: TaskEventState, Text: file + " changed while this ran"}
	if _, job := IsJobTask(t.ID); job || t.DoneBy.Kind == TaskByFactory {
		return t, []WorkstreamTaskEvent{line}, true
	}
	t.State, t.Rev, t.UpdatedAt = TaskNeeds, t.Rev+1, stamp
	t.Ask = &TaskAsk{
		ID: FileChangedAskID + newID(), Title: file + " changed while this ran. Carry on?",
		Detail: "The work so far read the earlier version.",
		Options: []TaskAskOption{
			{ID: "carry_on", Label: "Carry on with the new version", Recommended: true},
			{ID: "start_again", Label: "Start again from the new version"},
		},
		AskedAt: stamp,
	}
	return t, []WorkstreamTaskEvent{line}, true
}

// cleanChecklist checks a whole checklist and gives each new line its id: one
// line each, trimmed, none empty, at most [TaskChecklistMost] lines of at most
// [TaskChecklistLineMost] characters, every id once.
func cleanChecklist(in []TaskCheckItem) ([]TaskCheckItem, error) {
	if len(in) > TaskChecklistMost {
		return nil, taskInvalidf("a checklist has at most %d lines; past that the work is more than one task", TaskChecklistMost)
	}
	out := make([]TaskCheckItem, 0, len(in))
	seen := map[string]bool{}
	next := 1
	for _, it := range in {
		if it.ID != "" {
			seen[it.ID] = true
		}
	}
	for _, it := range in {
		it.Text = strings.Join(strings.Fields(it.Text), " ")
		if it.Text == "" {
			continue
		}
		if len(it.Text) > TaskChecklistLineMost {
			return nil, taskInvalidf("a checklist line is at most %d characters", TaskChecklistLineMost)
		}
		if it.ID == "" {
			for seen["c"+strconv.Itoa(next)] {
				next++
			}
			it.ID = "c" + strconv.Itoa(next)
			seen[it.ID] = true
		}
		if slices.ContainsFunc(out, func(x TaskCheckItem) bool { return x.ID == it.ID }) {
			return nil, TaskInvalid("each checklist line has its own id")
		}
		out = append(out, it)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// tickChecklist ticks the lines in done and unticks those in undone. A line
// that is not on the checklist is refused by its id, so an agent learns it
// named a line that is not there rather than believing it ticked one.
func tickChecklist(list []TaskCheckItem, done, undone []string) ([]TaskCheckItem, error) {
	out := slices.Clone(list)
	set := func(ids []string, to bool) error {
		for _, id := range ids {
			i := slices.IndexFunc(out, func(x TaskCheckItem) bool { return x.ID == strings.TrimSpace(id) })
			if i < 0 {
				return taskInvalidf("this task's checklist has no line %q", id)
			}
			out[i].Done = to
		}
		return nil
	}
	if err := set(done, true); err != nil {
		return nil, err
	}
	if err := set(undone, false); err != nil {
		return nil, err
	}
	return out, nil
}

// InvitedPerson is whether id names a person who has been invited and has not
// joined yet ("invite:<id>"). A task they own or do waits until they join.
func InvitedPerson(id string) bool { return strings.HasPrefix(id, tokens.InviteRefPrefix) }

// RebindPerson gives an open task from `from` to `to` (named name): its owner
// and the person who does it, where either is `from`. Who handed it over is
// kept, so a joiner reads the tasks they were given before they joined as
// handed to them. Done work keeps who did it; an unchanged task is false.
func RebindPerson(t WorkstreamTask, from, to, name string, now time.Time) (WorkstreamTask, bool) {
	if t.State == TaskDone || from == "" || to == "" || from == to {
		return t, false
	}
	changed := false
	if t.Owner.Kind == TaskOwnerPerson && NormalizeHandle(t.Owner.ID) == NormalizeHandle(from) {
		t.Owner, changed = TaskOwner{Kind: TaskOwnerPerson, ID: to, Name: name}, true
	}
	if t.DoneBy.Kind == TaskByPerson && NormalizeHandle(t.DoneBy.ID) == NormalizeHandle(from) {
		t.DoneBy, changed = TaskDoneBy{Kind: TaskByPerson, ID: to, Name: name}, true
	}
	if !changed {
		return t, false
	}
	// THE PERSON ARRIVED, SO NOTHING IS LEFT TO HAND ON: a "Hand to someone
	// else?" Ask still open closes, and the task goes back to what it was.
	if InvitedPerson(from) && InviteHeld(t) == "" {
		t = closeInviteChases(t)
	}
	t.Rev, t.UpdatedAt = t.Rev+1, now.UTC().Format(time.RFC3339Nano)
	return t, true
}

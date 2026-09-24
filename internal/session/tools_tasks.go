package session

// THE MODEL'S WAY BACK TO OLD WORK.
//
// The "@" mention (internal/tui3) is the person's half of this: they remember a
// task, they point at it, and a pointer block goes into the prompt. This file is
// the other half, and it exists because THE COMMON CASE IS THAT THEY DO NOT
// POINT. "Do that thing we did to the reconciler last week, but for the
// scheduler" is a sentence with no id in it, no title in it and no path in it,
// and until this tool the only honest answer a model had was to ask.
//
// It is a BELT TOOL and not a hub op, and the choice is worth stating because
// the design this implements names a hub. This build has no hub: the model
// reaches everything it can reach through bare.Tool (tools.go), and the two
// nearest neighbours of this op — `jobs`, which looks at background work, and
// `watch`, which is told about it — are both belt tools with an action-shaped
// schema. A second entry-point mechanism for one op would be a second
// vocabulary for the model to learn and a second place for this build to keep
// its wire discipline. So `tasks` sits on the belt beside `jobs`, and reads the
// same way.
//
// A NODE GETS IT SCOPED TO ITS OWN FAMILY, and never wider. A node's brief is
// its whole world by contract (task_contract.go), so a node rummaging through
// the project's history is a node reading the conversation it was deliberately
// given none of. What a node DOES have a right to is the work it handed out
// itself (task.go's fan-out law): the pieces it split off, how they are going,
// what they found, and one line into one that is going the wrong way. So inside
// a node every op here answers about its own children and about nothing else —
// the search lists them, an id outside them is refused by name, and a node that
// handed nothing out gets an empty list rather than the project's.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// AND THE POLLING SENTENCE IS PAID FOR RATHER THAN ADDED. `Never to WAIT for
// handed-off work` is here because a model polled this tool eleven times in one
// turn for a report that was going to be delivered to it (tasklook.go). Its
// bytes come out of the URI gloss this string used to carry — what an artifact
// URI and a transcript URI ARE is said in full by prompts/system.md, in the same
// fixed prefix, three lines from where it says to call this tool. One fact, one
// place.
//
// WRITTEN FOR DENSITY, BECAUSE THIS STRING IS BILLED ON EVERY REQUEST OF EVERY
// TURN. The tool-schema block rides in front of each request the model makes —
// dozens per task — so a paragraph here is paid for dozens of times while a Go
// comment beside it is free. Every rule the old description stated is still
// stated; what went is the rhetoric, and the sentences the schema's own fields
// say better. A rule belongs in the field it governs and appears ONCE.
//
// AND THE STOP VERB IS SPELLED OUT IN IT rather than left to the field, because
// the sentence a model has to have BEFORE it reaches for a field is which field
// ends work. Asked to stop task 2, a model with no stop verb said "stop, do not
// continue" into the task with `say`; the worker wrote down that it had been
// told to stop, delivered nothing, and the check read that as an ordinary
// unfinished run and opened a repair round on it. The task went on spending for
// as long as it took somebody to notice.
// AND THE ROUTING SENTENCE LEFT (2026-09-10, the prompt diet). "Use it when the
// person means earlier work without pointing at it, or to look inside running
// work" is a WHEN-TO-REACH rule, and a description is a CONTRACT: what the tool
// does, what its fields take, what comes back. Which verb a request routes to is
// stated once, on the page's own routing table, rather than once per tool here —
// eighteen descriptions each carrying their own routing clause is the same table
// written eighteen times and billed on every request of every turn.
const tasksDescription = "Find tasks. A finished task is asked about with `tasks` and is never redone or rechecked by hand. No id searches; an id reads, steers, stops, forwards, continues or settles one. To END running work use stop; say may be ignored. Never to WAIT for handed-off work. Search includes other windows' live work as `another window`, with no id here."

// The schema's `resolve` enum is INTERPOLATED from [TaskResolutions] rather
// than typed out, because the landing note offers the same three words to the
// same reader (task_run.go's [settleClause]) and a hand-kept second copy is a
// copy that drifts. The prose around it still spells each verb, because a
// description is where the model learns what they mean.
//
// THE BOUNDS ARE INTERPOLATED FOR THE SAME REASON. `limit` and `lines` are
// clamped by [SearchTaskIndex] and [taskTailLines] against constants, and a
// digit typed here is the second copy that drifts — propose_task's schema once
// said 40 where the executor applied 200, and every model that read it reasoned
// from the wrong figure.
var tasksSchemaJSON = `{"type":"object","properties":{` +
	`"query":{"type":"string","description":"Matched against titles, ids and outcomes; omit for the newest."},` +
	`"limit":{"type":"integer","description":"Rows to return (default: ` + strconv.Itoa(taskSearchLimit) + `, maximum: ` + strconv.Itoa(taskSearchCeiling) + `)"},` +
	`"id":{"type":"string","description":"A task's id (\"7\") or name (\"fix-the-nil-map-crash\")."},` +
	// The live answer is read off the running work itself (task_live.go) and
	// names what it is doing this second, how long it has been at it, its steps,
	// its spend and its last lines. That list is not spelled in the schema
	// because the answer itself carries it, and this string is paid for on every
	// request of every turn while this comment is free.
	`"lines":{"type":"integer","description":"Tail lines of a running task (default: ` + strconv.Itoa(taskLiveDefaultTail) + `, maximum: ` + strconv.Itoa(taskLiveMaxTail) + `)"},` +
	`"scope":{"type":"string","enum":` + taskScopeEnum + `,"description":"\"` + taskScopeProject + `\" (default) is this project alone; \"` + taskScopeEverywhere + `\" also lists live work in every other project, grouped by project and unreachable from here. A search only."},` +
	`"say":{"type":"string","description":"A line into the running task named by id: a correction, or a fact it lacks. It ends nothing; stop is the door that ends work. With stop or resolve it is the reason, with continue this round's finding."},` +
	`"stop":{"type":"boolean","description":"Ends the running task named by id, through the same door the person's own stop pulls: work halts where it stands, the branch is kept, nothing re-runs it. It asks no confirmation; say is the reason."},` +
	`"continue":{"type":"boolean","description":"The door for \"continue task N\": re-arms that settled task with the same node, brief and working copy."},` +
	`"resolve":{"type":"string","enum":` + TaskResolveEnum() + `,"description":"Settles a task nobody could check, only on evidence you read. accept: done, branch merged. reaudit: a fresh checker, and the answer when none came. refute: it and its dependents fail."},` +
	// AND THE ONE OP THAT CARRIES SOMEBODY ELSE'S AUTHORITY says in its own
	// description that the words are not yours to write, because that is the
	// rule a model has to know BEFORE it reaches for the field. What it cannot
	// do — supply the text, name a message, forward from a turn the person did
	// not open — is refused by the runtime with its reason (task_forward.go), so
	// the schema spends its bytes on the choice rather than on the law.
	`"forward":{"type":"boolean","description":"Sends what the person just said into the running task named by id, verbatim and as theirs: the one door by which a correction typed here moves what that task is judged by. Their words go alone."}` +
	`},"additionalProperties":false}`

// tasksSchemaWithNote is [tasksSchemaJSON] with the plan road's own field on
// it, and it is built rather than typed twice so the two cannot drift.
//
// THE FIELD IS ABSENT WHERE IT CANNOT WORK, which is this package's own law
// stated in beltfacts.go and applied here: a note lands in a plan store, and a
// conversation whose hand-offs are nodes of the session tree has no plan store
// at all. Carrying the field there would be a verb the model would reach for
// once and be refused by forever, and — the reason it matters more than tidiness
// — the node road's request bytes would change, which is a thing this change is
// not allowed to do.
var tasksSchemaWithNote = strings.TrimSuffix(tasksSchemaJSON, `},"additionalProperties":false}`) +
	// A NOTE IS NOT `say`, AND THE DESCRIPTION IS WHERE THAT IS SETTLED. `say`
	// is a correction aimed at a node of this session's own tree; a note is a
	// fact written onto a row of the run's plan, which the worker is handed the
	// moment its current step ends ([Agent.Steer] carries it, so a reply being
	// written is cut and re-asked) and every other worker and the person can
	// read too. It says what it cannot do in the same breath, because a model
	// that believed a note moved a task's work order would be changing what the
	// work is judged by with no version check behind it.
	//
	// AND IT NAMES NO DOOR THAT DOES. It used to end "revise_assignment does
	// that", and `revise_assignment` is a worker's verb that no conversation
	// carries ([Config.mayRevise]): the schema was pointing the model at a tool
	// it did not have.
	`,"note":{"type":"string","description":"A fact written onto the run row named by id (\"2\", \"2.1\"): its worker is handed it when its current step ends, and it stays on the row for the person and the other workers. It is information, not an order: it cannot change what that task was asked for or is judged by. It ends nothing."}` +
	`},"additionalProperties":false}`

// tasksArguments is the wire form. The id is RAW because a model that has just
// read "7 · fix-the-nil-map-crash" will send either `"7"` or `7`, and both of
// them mean task seven: a schema type is a request, not a guarantee, and
// refusing the number would be this tool failing a call that named exactly what
// it meant.
type tasksArguments struct {
	Query    string          `json:"query"`
	Limit    int             `json:"limit"`
	Scope    string          `json:"scope"`
	ID       json.RawMessage `json:"id"`
	Lines    int             `json:"lines"`
	Say      string          `json:"say"`
	Stop     bool            `json:"stop"`
	Forward  bool            `json:"forward"`
	Continue bool            `json:"continue"`
	Resolve  string          `json:"resolve"`
	// Note is the plan road's own field: a fact written onto one row of the
	// live run, which that row's worker is handed when its current step ends.
	// It is decoded on every road, because a model that sent it where the schema
	// does not offer it is better answered with a sentence than with a parse
	// error.
	Note string `json:"note"`
}

// The two words `scope` takes. They are constants because the schema's enum,
// the refusal below and the branch in [Agent.taskSearchText] are three readings
// of one word, and a hand-typed third copy is the one that drifts.
const (
	taskScopeProject    = "project"
	taskScopeEverywhere = "everywhere"
)

// taskScopeEnum is the schema's list of them, built from the constants rather
// than typed out beside them — the same discipline [TaskResolveEnum] keeps for
// the same reason.
var taskScopeEnum = `["` + taskScopeProject + `","` + taskScopeEverywhere + `"]`

// taskScopeWord reads the scope argument back, and answers false for a word
// that is neither. An absent scope is `project` — the behaviour every caller
// had before this argument existed, so a model that has never heard of it gets
// exactly what it used to.
func taskScopeWord(raw string) (string, bool) {
	switch word := strings.ToLower(strings.TrimSpace(raw)); word {
	case "":
		return taskScopeProject, true
	case taskScopeProject, taskScopeEverywhere:
		return word, true
	default:
		return "", false
	}
}

// tasksTool is the window onto the project's task index (task_index.go) AND
// onto the work that is running right now. It is the ONLY way the model reaches
// either, for [Agent.jobsTool]'s reason: one vocabulary per kind of thing.
//
// ── FOUR OPS, ONE NOUN ──
//
// The tool is shaped by what the model does with it, and what it does is a
// sequence: PULL the state of work it handed off, DECIDE whether it is going
// the right way, STEER it if it is not, and CONTINUE it when it has ended.
// Those are one conversation about one task, so they are one tool with one
// id — not a search tool, a read tool, a steering tool and a continue tool,
// each with its own spelling of "which task".
//
//   - no id: the search, unchanged. The person referred to earlier work and
//     pointed at nothing.
//   - id: that one task. Running, and the answer is read off the GRAPH — the
//     same live source the surface's rail draws (task_live.go) — never off the
//     index file, which by construction holds only work that is over.
//   - id and say: a line into a running node ([Agent.relayToTask]). Same
//     mechanics as the person's own door and the same law — talk to the worker,
//     never a new target — but it arrives named as this conversation speaking,
//     because the model is not the person and a worker that cannot tell them
//     apart will read coordination as permission.
//   - id and continue: the same node again ([Agent.ContinueTask]), after it
//     failed or finished. Same brief, same working copy, last report as this
//     round's finding. A new propose_task is the wrong door.
func (a *Agent) tasksTool() bare.Tool {
	// THE SCHEMA THE CALL CARRIES IS THE ONE THIS AGENT CAN ANSWER. The plan
	// road's `note` field is offered only where a plan store exists to hold it
	// ([Config.oneTaskRoad]), which is the same predicate the hand-off facts
	// branch on, so the page and the schema cannot disagree about the verb.
	schema := tasksSchemaJSON
	if a.config.oneTaskRoad() {
		schema = tasksSchemaWithNote
	}
	return bare.Tool{
		Name:        "tasks",
		Description: tasksDescription,
		Schema:      json.RawMessage(schema),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed tasksArguments
			// An absent argument object is a valid call — "what has been going
			// on" takes no arguments — so only malformed bytes are an error.
			if len(args) > 0 {
				if err := decodeToolArguments(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			scope, known := taskScopeWord(parsed.Scope)
			if !known {
				return fmt.Sprintf("Invalid arguments: scope takes %q or %q.", taskScopeProject, taskScopeEverywhere), true, nil
			}
			token := taskToken(parsed.ID)
			if token == "" {
				if strings.TrimSpace(parsed.Say) != "" {
					return "Invalid arguments: say needs an id — it goes to one running task, not to a search", true, nil
				}
				if parsed.Continue {
					return "Invalid arguments: continue needs an id — it re-arms one settled task, not a search", true, nil
				}
				if strings.TrimSpace(parsed.Resolve) != "" {
					return "Invalid arguments: resolve needs an id — it settles one task that needs a look, not a search", true, nil
				}
				if parsed.Forward {
					return "Invalid arguments: forward needs an id — the person's words go to one running task, not to a search", true, nil
				}
				if parsed.Stop {
					return "Invalid arguments: stop needs an id — it ends one running task, not a search", true, nil
				}
				if strings.TrimSpace(parsed.Note) != "" {
					return "Invalid arguments: note needs an id — it is written onto one row of the run, not onto a search", true, nil
				}
				// THE RUN'S TASKS LEAD AND THE SHIPPED LISTING FOLLOWS. A conversation
				// that has handed work to a run still has the tasks of earlier
				// sittings and of other windows, and a listing that showed only the
				// run would have made them unfindable the day the first run started.
				listing := a.taskSearchText(parsed.Query, parsed.Limit, scope)
				if run := a.planTasksText(a.runPlanTasks(), parsed.Query); run != "" {
					if strings.HasPrefix(listing, "No task") {
						listing = run
					} else {
						listing = run + "\n" + listing
					}
				}
				return markTaskLook(ctx, listing), false, nil
			}
			// A STOP ON A ROW OF THE LIVE RUN IS THE RUN'S OWN STOP, and it is
			// routed here because the reader below cannot find these rows at all.
			//
			// MEASURED ON THE REAL BINARY, 2026-09-22: a person said "I do not want
			// division in this package at all" while three hand-offs were running;
			// the conversation read the digest, worked out that row #2 was the one
			// that had gone wrong — which is the whole of what change 2 is for — and
			// called `tasks {"id":"2","stop":true}`. It was answered `No task "2" in
			// this project`, four times, under four spellings of the same row. Then
			// it told the person "Task #2 stopped", and task 2 ran to completion,
			// was checked, and landed. A false report to a person, from a door that
			// refuses the one thing the turn had correctly decided to do.
			//
			// The cause is that a hand-off on this road publishes a run ROW and
			// never a node of the session tree ([Agent.startKnownTaskRun]), so
			// [Agent.taskByToken] misses and [Agent.oneTask] refuses before
			// stoprun.go is ever reached. The person's own stop does reach it, by
			// the number the row wears ([Agent.CancelWithReason]), and this is the
			// model taking that same door with the same number.
			if parsed.Stop {
				if answer, ok := a.stopPlanRow(token, parsed.Say); ok {
					return answer, false, nil
				}
			}
			// A NOTE IS WRITTEN ONTO THE RUN'S OWN ROW, and only there. It is the
			// chat's half of the note channel: a worker writes to a sibling's row
			// through `plandb task note`, the person writes from the task's page,
			// and this is the one mind holding the conversation writing what the
			// plan does not know. The row is named the way the model has just read
			// it in the listing (`#2`, `#2.1`), which is the only spelling of a run
			// row that exists outside the store.
			if note := strings.TrimSpace(parsed.Note); note != "" {
				return a.planNoteFromChat(token, note)
			}
			// `say` AND `forward` ON A ROW OF THE RUN ARE ANSWERED HERE, for the
			// reason `stop` is: the reader below cannot find these rows at all, so
			// both used to come back `No task "2" in this project` about a row the
			// digest had just shown the conversation, which is a refusal that
			// denies the row exists rather than saying what the door can do.
			//
			// `say` is a line to the work and a note IS that line on this road, so
			// it is delivered as one and the answer says it went that way. `forward`
			// is refused: it promises to move what a task is judged by, and nothing
			// a conversation holds moves that for a row of the run — so it says so,
			// and names the doors that do exist. A `say` that rides with `continue`
			// or `resolve` is that verb's reason, not a line to the work, and goes
			// on to the reader below as before.
			if parsed.Forward || (strings.TrimSpace(parsed.Say) != "" && !parsed.Continue && strings.TrimSpace(parsed.Resolve) == "") {
				if row, want, ok := a.planRowNamed(token); ok {
					if parsed.Forward {
						return fmt.Sprintf(planForwardRefusal, want), true, nil
					}
					return a.writePlanNote(row, want, strings.TrimSpace(parsed.Say), planSayLead)
				}
			}
			// A READ OF A TASK THE RUN'S STORE HOLDS IS ANSWERED FROM THE STORE, by the
			// number the rail shows for it. Anything else about it (stop,
			// continue, settle) and any id the store does not hold go on to the
			// shipped reader below, exactly as before.
			if reading := !parsed.Continue && !parsed.Forward && !parsed.Stop && strings.TrimSpace(parsed.Say) == "" && strings.TrimSpace(parsed.Resolve) == ""; reading {
				if answer, ok := a.planTaskText(a.runPlanTasks(), token); ok {
					return markTaskLook(ctx, answer), false, nil
				}
			}
			// THE SAME ANSWER TWICE IN ONE TURN SAYS SO (tasklook.go). It is the
			// half of the polling fix that reaches a model already mid-poll: the
			// receipt and the description say the report comes to it, and this
			// says the same thing again on the answer it is staring at. A refusal
			// is left alone — it is not a look at anything.
			answer, failed, err := a.oneTask(ctx, token, parsed)
			if failed || err != nil {
				return answer, failed, err
			}
			return markTaskLook(ctx, answer), false, nil
		},
	}
}

// taskSearchText is the no-id answer: this project's own rows, and under them
// the work every OTHER codeaf window on this project has out right now.
//
// THE SECOND HALF IS WHY THIS FUNCTION EXISTS. The index is what work CAME TO,
// and an ordinary task writes no row into it until it lands (taskelsewhere.go's
// header) — so a model asked "what else is running on this project" could read
// the whole file and truthfully find nothing, while two windows beside it were
// mid-run. It answered by denying, and the denial was wrong. The other windows
// say what they have out in their own presence files, and this is the model's
// door onto them.
//
// IT IS A CONVERSATION'S ANSWER AND NEVER A NODE'S ([Agent.tellsElsewhere]): a
// node sees the pieces it handed out itself and nothing wider, which is this
// tool's own scope law said about a different set of rows.
func (a *Agent) taskSearchText(query string, limit int, scope string) string {
	// A NODE'S ROWS ARE ITS WHOLE FAMILY ([Agent.taskRows]), and a family is
	// bounded by [taskFanLimit], so a node that names no limit is shown all of
	// it. The project's default ([taskSearchLimit]) is for a conversation reading
	// history; a parent shown half the pieces it handed out plans around pieces
	// it believes it never started.
	if limit <= 0 && a.config.taskID != 0 {
		limit = taskFanLimit
	}
	rows := a.taskRows()
	if a.config.taskID == 0 {
		// ONE RUN, ONE ROW: a run's own plan rows lead this answer, so its row
		// in the project's record is not listed a second time
		// ([Agent.withoutListedRuns]).
		rows = a.withoutListedRuns(rows)
	}
	out := taskRowsTextLimit(rows, query, limit)
	if !a.tellsElsewhere() {
		return a.taskConversationHint(out)
	}
	now := time.Now()
	if section := taskElsewhereText(a.Elsewhere().Tasks(), query, now); section != "" {
		out += "\n" + section
	}
	// THE WIDER READING IS TAKEN ONLY WHEN IT WAS ASKED FOR BY NAME. Every
	// other turn pays nothing for this argument existing: no directory is read,
	// no file is opened, and nothing new is cached — [Agent.OtherProjects] is
	// world.go's reading of the machine, cheap enough to take on a keystroke
	// and still not a thing to take on a turn nobody asked the question in.
	if scope != taskScopeEverywhere {
		return a.taskConversationHint(out)
	}
	if section := taskEverywhereText(a.OtherProjects(now), query, now); section != "" {
		out += "\n" + section
	}
	return a.taskConversationHint(out)
}

// A miss in this project is not a miss everywhere. Only a final empty result
// points at conversations, and only when that reader is on the caller's belt.
func (a *Agent) taskConversationHint(out string) string {
	if a.config.hasConversationHistory() && strings.HasPrefix(out, "No task matches ") && !strings.Contains(out, "\n") {
		out += " If this was a conversation rather than handed-off work, search_conversations searches saved chats across places. A saved memory may suggest the answer but does not locate the original conversation; look up the source before answering."
	}
	return out
}

// taskElsewhereText draws the other windows' running work in this tool's own row
// grammar, matched against the same query the search used.
//
//	A TASK'S PARTS RIDE ON ITS ROW THE WAY THE BLOCK DRAWS THEM
//	([foldElsewhere]): a window running one quick task with three parts is
//	ONE row for that family — its own title, `3 quick parts running` — and
//	no row of its own for any part. The overflow line counts families, not
//	parts, so a window with work handed out cannot push another window's
//	whole task out of the answer.
//
// THE ROW LEADS WITH `another window` AND CARRIES NO ID, and that is the one
// deliberate break from [taskRowText]. Ids restart with every conversation
// ([TaskIndexEntry.ID]), so a row printing "7" here would invite `tasks id 7`
// and reach THIS project's task seven, which is a different piece of work
// entirely. The words are the task page's own (`another window`, tasks.md), so
// the model and the screen say the same thing about the same row.
//
// `files so far` is spelled that way because a live claim is what a run has
// ALREADY written, never what it means to write ([PresenceTask.Files]).
func taskElsewhereText(rows []ElsewhereTask, query string, now time.Time) string {
	query = strings.ToLower(strings.TrimSpace(query))
	var matched []ElsewhereTask
	for _, at := range rows {
		if query != "" && !strings.Contains(strings.ToLower(at.Task.Title+" "+at.Session), query) {
			continue
		}
		matched = append(matched, at)
	}
	if len(matched) == 0 {
		return ""
	}
	// THE CAP IS TAKEN OVER FAMILIES AND NOT ROWS. Parts counted against it
	// meant a window running one task with four parts could spend the whole
	// budget and push a different window's task out of the answer — a number
	// the same reading gives the <elsewhere> block the other way ([deltaLiveRows]).
	families := foldElsewhere(matched)
	over := 0
	if len(families) > taskSearchLimit {
		over = len(families) - taskSearchLimit
		families = families[:taskSearchLimit]
	}
	var out strings.Builder
	out.WriteString("running in other codeaf windows on this project:\n")
	for _, family := range families {
		taskAwayFamilyRow(&out, "", "another window", family, now)
	}
	if over > 0 {
		fmt.Fprintf(&out, "… and %d more running elsewhere.\n", over)
	}
	out.WriteString("These have no id in this conversation: work running in another window cannot be read, steered or resolved from here, and it lands in that window rather than this one.\n")
	return out.String()
}

// taskAwayRow writes ONE piece of work somebody else has out, in the grammar
// both away sections share: a lead word saying whose window it is, the title,
// the node's own state word, and how long it has been going.
//
// IT IS ONE WRITER FOR BOTH SECTIONS ON PURPOSE. The project's other windows
// and the machine's other projects are the same fact at two distances, and two
// loops spelling the same row would be the two places a `files so far` clause
// or a quoted window name could come to be drawn differently for the same node.
//
// indent puts the row under a heading that owns it — the machine-wide section
// groups its rows by project — and lead is `another window` or `open here`.
func taskAwayRow(out *strings.Builder, indent, lead string, at ElsewhereTask, now time.Time) {
	// FLATTENED, because these words were written by ANOTHER window's model and
	// reach this one unedited: a title carrying a newline would turn one row of
	// this answer into three ([deltaLine] is the same stop the <elsewhere>
	// block uses on the same strings).
	title := deltaLine(at.Task.Title)
	if title == "" {
		title = "untitled work"
	}
	parts := []string{lead, title}
	if state := strings.TrimSpace(at.Task.State); state != "" {
		parts = append(parts, state)
	}
	// A QUEUED NODE HAS NO CLOCK, exactly as [taskWhenWord] has it: its
	// StartedAt is zero because it has not started, and an age of zero beside
	// the word "queued" is a row arguing with itself.
	if !at.Task.StartedAt.IsZero() {
		if span := taskSpanWord(now.Sub(at.Task.StartedAt)); span != "" {
			parts = append(parts, "running for "+span)
		}
	}
	out.WriteString(indent + strings.Join(parts, " · ") + "\n")
	if name := deltaLine(at.Session); name != "" {
		out.WriteString(indent + "  in the window called " + strconv.Quote(name) + "\n")
	}
	files := at.Task.Files
	if len(files) > deltaRowFiles {
		files = files[:deltaRowFiles]
	}
	if word := deltaFilesWord(files, len(at.Task.Files)); word != "" {
		out.WriteString(indent + "  files so far: " + word + "\n")
	}
}

// taskAwayFamilyRow writes ONE family of work somebody else has out — the
// family's head row in [taskAwayRow]'s grammar, with its parts counted on it
// and no row of its own for any part.
//
// IT IS THE BLOCK'S OWN FOLD, REUSED ([foldElsewhere]) rather than restated,
// because the model reads both in one turn: `<elsewhere>` says `3 quick parts
// running` and then this tool said four unrelated jobs, and a model deciding
// whether to start the same work believed whichever reading it saw last. One
// writer is the only way the two stay the same sentence.
func taskAwayFamilyRow(out *strings.Builder, indent, lead string, family elsewhereFamily, now time.Time) {
	at := family.head
	// THE PARTS COUNT RIDES ON THE HEAD'S OWN ROW, after the clauses that say
	// what it is and how long it has been going, so a family with nothing handed
	// out draws exactly the row it always drew.
	word := elsewherePartsWord(family.parts)
	if word == "" {
		taskAwayRow(out, indent, lead, at, now)
		return
	}
	title := deltaLine(at.Task.Title)
	if title == "" {
		title = "untitled work"
	}
	parts := []string{lead, title}
	if state := strings.TrimSpace(at.Task.State); state != "" {
		parts = append(parts, state)
	}
	// A QUEUED NODE HAS NO CLOCK, exactly as [taskWhenWord] has it.
	if !at.Task.StartedAt.IsZero() {
		if span := taskSpanWord(now.Sub(at.Task.StartedAt)); span != "" {
			parts = append(parts, "running for "+span)
		}
	}
	parts = append(parts, word)
	out.WriteString(indent + strings.Join(parts, " · ") + "\n")
	if name := deltaLine(at.Session); name != "" {
		out.WriteString(indent + "  in the window called " + strconv.Quote(name) + "\n")
	}
	// THE FAMILY'S FILES AND NOT THE HEAD'S, because a part that has written is
	// work that window has out, and a reader deciding what to leave alone needs
	// every path the family is in — merged in the order the work touched them.
	files := family.files()
	if len(files) > deltaRowFiles {
		files = files[:deltaRowFiles]
	}
	if word := deltaFilesWord(files, len(family.files())); word != "" {
		out.WriteString(indent + "  files so far: " + word + "\n")
	}
}

// everyFamily is one piece of work running in another project, folded the way
// this project's own other windows are folded: the head, and the parts it
// handed out counted on its row. `head` is the head's own [OtherProjectTask] so
// the wide answer keeps the one fact that is about the head's conversation and
// not the family's shape — whether this terminal is the one holding it.
type everyFamily struct {
	elsewhere elsewhereFamily
	head      OtherProjectTask
}

// otherProjectRow finds the folded head's own row in its project, so
// [OtherProjectTask.Mine] survives the fold; a head nothing matched carries no
// such fact, which is every row a build older than the fold would have written.
func otherProjectRow(held []OtherProjectTask, family elsewhereFamily) OtherProjectTask {
	for _, at := range held {
		if at.SessionID == family.head.SessionID && at.Task.ID == family.head.Task.ID {
			return at
		}
	}
	return OtherProjectTask{ElsewhereTask: family.head}
}

// otherProjectFamilies is one project's kept rows with its families folded and
// the query's matches kept, so the cap and the writer below read one shape
// rather than re-deriving the fold.
type otherProjectFamilies struct {
	OtherProject
	families []everyFamily
}

// everyFamilyCount is how many folded rows the wide answer holds across its
// projects — the number the overflow line counts, so it counts families and
// never parts.
func everyFamilyCount(groups []otherProjectFamilies) int {
	total := 0
	for _, group := range groups {
		total += len(group.families)
	}
	return total
}

// taskEverywhereText draws what is running in every OTHER project on this
// machine, grouped by project, under the same query the search used.
//
//	running in other projects on this machine:
//	wisp · /Users/ada/code/wisp
//	  another window · Port the parser · running · running for 2m 3s
//	    in the window called "parser work"
//	  open here · Rewrite the docs · queued
//	  another window · Survey the config loaders · running · running for 5m · 3 quick parts running
//
// TWO LEAD WORDS, BECAUSE TWO OF THESE ARE NOT THE SAME THING TO A PERSON.
// `another window` is a terminal somewhere else that they have to go and find.
// `open here` is a conversation THIS terminal is already holding behind this
// one (internal/tui3's keeper), and telling somebody to go looking for a window
// that is two keystrokes away in the terminal they are sitting in is the
// refusal [ReadElsewhere] exists to stop this build making — so the row says
// which it is. Whose process it is comes off the presence file's pid; see
// [OtherProjectTask.Mine] for why that number is safe to ask this question of
// and never safe to ask about liveness.
//
// A PROJECT WITH NOTHING RUNNING IS NOT DRAWN AT ALL — no heading, no count, no
// line. That is the emptiness law, and it is also the only thing that keeps
// this section short on a machine with forty buckets under its state root.
func taskEverywhereText(groups []OtherProject, query string, now time.Time) string {
	query = strings.ToLower(strings.TrimSpace(query))
	var kept []otherProjectFamilies
	for _, group := range groups {
		var rows []OtherProjectTask
		for _, at := range group.Tasks {
			// The project's own name is matched as well as the task's, because
			// "what is wisp doing" is the same question asked about the place
			// rather than about the work. A PART IS MATCHED ON ITS OWN TITLE TOO,
			// so a query naming a family's work still finds the head it hangs
			// under — the fold below would otherwise drop it silently.
			hay := strings.ToLower(at.Task.Title + " " + at.Session + " " + group.Name + " " + group.Path)
			if query != "" && !strings.Contains(hay, query) {
				continue
			}
			rows = append(rows, at)
		}
		if len(rows) == 0 {
			continue
		}
		group.Tasks = rows
		kept = append(kept, otherProjectFamilies{OtherProject: group})
	}
	if len(kept) == 0 {
		return ""
	}
	// THE SAME FOLD, THE SAME CAP, THE SAME REASON as this project's own
	// section: families and not parts, so a window with work handed out cannot
	// push another project's whole task out of the wide answer either. The fold
	// is taken INSIDE one project at a time and never across the groups,
	// because a family is keyed by window and id and each project's rows came
	// out of that project's own presence files — a shared id under two projects
	// is two pieces of work exactly as it was before this existed.
	for i, group := range kept {
		rows := make([]ElsewhereTask, 0, len(group.Tasks))
		for _, at := range group.Tasks {
			rows = append(rows, at.ElsewhereTask)
		}
		folded := foldElsewhere(rows)
		families := make([]everyFamily, 0, len(folded))
		for _, family := range folded {
			families = append(families, everyFamily{elsewhere: family, head: otherProjectRow(group.Tasks, family)})
		}
		kept[i] = otherProjectFamilies{OtherProject: kept[i].OtherProject, families: families}
	}
	over := 0
	if families := everyFamilyCount(kept); families > taskSearchLimit {
		over = families - taskSearchLimit
		budget := taskSearchLimit
		var capped []otherProjectFamilies
		for _, group := range kept {
			if budget <= 0 {
				break
			}
			if len(group.families) > budget {
				group.families = group.families[:budget]
			}
			budget -= len(group.families)
			capped = append(capped, group)
		}
		kept = capped
	}
	mine := false
	var out strings.Builder
	out.WriteString("running in other projects on this machine:\n")
	for _, group := range kept {
		// THE PATH IS DRAWN ONLY WHEN THERE IS ONE. A bucket whose sessions
		// never recorded a workspace is named by the bucket itself (world.go's
		// [projectName]), and repeating that encoded string as though it were a
		// path would be dressing an address up as a fact.
		head := deltaLine(group.Name)
		if path := deltaLine(group.Path); path != "" && path != head {
			head += " · " + path
		}
		out.WriteString(head + "\n")
		for _, family := range group.families {
			lead := "another window"
			if family.head.Mine {
				lead = "open here"
				mine = true
			}
			taskAwayFamilyRow(&out, "  ", lead, family.elsewhere, now)
		}
	}
	if over > 0 {
		fmt.Fprintf(&out, "… and %d more running in other projects.\n", over)
	}
	out.WriteString("These have no id in this conversation: work running in another project cannot be read, steered or resolved from here, and it lands where it is running rather than in this conversation.\n")
	if mine {
		out.WriteString("A row marked `open here` is a conversation this same terminal is already holding — it is reached with tab or from /home, not by opening another window.\n")
	}
	return out.String()
}

// taskToken reads the id argument back as the handle a person or a model would
// say: `7`, `"7"`, `"@fix-the-nil-map-crash"`, `"task 7"`. Everything it strips
// is decoration around a name that was already right.
func taskToken(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	if strings.HasPrefix(text, `"`) {
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err == nil {
			text = quoted
		}
	}
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimPrefix(text, "@")
	text = strings.TrimSpace(strings.TrimPrefix(text, "task "))
	return strings.TrimSpace(text)
}

// oneTask is the id half of the tool: read that task, say something to it,
// continue it, or resolve it.
//
// THE ROW COMES FROM THE INDEX AND THE PRESENT COMES FROM THE GRAPH, and the
// join is here because they answer different halves of one question. The index
// knows every task this project ever ran, including the ones from conversations
// this process never saw; the graph knows what is happening inside the ones
// running now, and knows nothing at all about last Tuesday's. A task from an
// earlier conversation therefore reads as a row and says so, rather than being
// reported as a task with nothing happening in it.
//
// RESOLVE IS READ BEFORE SAY, and they are not two ways of doing one thing: say
// talks to a worker that is still there, resolve decides about work that is
// over. A call carrying both means the second, and `say` is its reason.
func (a *Agent) oneTask(ctx context.Context, token string, parsed tasksArguments) (string, bool, error) {
	if parsed.Forward && (parsed.Continue || strings.TrimSpace(parsed.Resolve) != "") {
		return "forward cannot be combined with continue or resolve; send one action at a time.", true, nil
	}
	// AND STOP IS THE ONE THAT CANNOT SHARE A CALL WITH ANYTHING. It ends the
	// work; continue puts it back on, resolve settles what it produced, forward
	// sends the person's words into it. A call carrying stop and one of those is
	// two decisions about the same task in one breath, and there is no order to
	// take them in that answers what the caller meant.
	if parsed.Stop && (parsed.Continue || parsed.Forward || strings.TrimSpace(parsed.Resolve) != "") {
		return "stop ends the task, so it cannot be combined with continue, resolve or forward; send one action at a time.", true, nil
	}
	// A LIVE RUN HAS NO PROJECT-INDEX ROW YET. Its rows are kept beside the
	// graph's nodes and are written to the finished-work index only when the run
	// ends, so a stop must resolve that live owner before asking the index. Every
	// other operation keeps its existing reader: run details come from the plan
	// store and ordinary tasks come from the graph and project index below.
	if parsed.Stop {
		if entry, id, found := a.liveBeltTaskByToken(token); found {
			return a.stopOneTask(entry, id, true, parsed.Say)
		}
	}
	rows := a.taskRows()
	entry, found := a.taskByToken(rows, token)
	if !found {
		// CONTINUE ON A MISS IS NOT "NO TASK". The person named a number and
		// asked to re-arm it; answering as a search miss lets the model
		// narrate the work as if it had resumed (R1). The honest fact is
		// that this session has no graph for that id.
		if parsed.Continue {
			return continueNoGraph(token, TaskIndexEntry{}), true, nil
		}
		if parsed.Stop {
			if a.config.taskID != 0 {
				return fmt.Sprintf("Could not stop task %q: no task by that id is running among the pieces you handed out. Call tasks with no arguments to see them.", token), true, nil
			}
			return fmt.Sprintf("Could not stop task %q: no task by that id is running in this project. Call tasks with no arguments to see the most recent ones.", token), true, nil
		}
		if a.config.taskID != 0 {
			// Scoped, so the miss is a different fact: the id may well name real
			// work, and what it does not name is anything this node handed out.
			return fmt.Sprintf("No task %q among the pieces you handed out. Call tasks with no arguments to see them; work you did not ask for is not yours to read from here.", token), true, nil
		}
		return fmt.Sprintf("No task %q in this project. Call tasks with no arguments to see the most recent ones.", token), true, nil
	}
	id, here := a.thisSessionTask(entry)
	// STOP IS READ BEFORE SAY, because a call carrying both means one thing: end
	// it, and here is why. Reading say first would relay the reason into a worker
	// this call is about to cut, which is the exact move this verb exists to
	// replace.
	if parsed.Stop {
		return a.stopOneTask(entry, id, here, parsed.Say)
	}
	if resolution := strings.TrimSpace(parsed.Resolve); resolution != "" {
		return a.resolveOneTask(entry, id, here, TaskResolution(strings.ToLower(resolution)), parsed.Say)
	}
	if parsed.Continue {
		return a.continueOneTask(entry, id, here, parsed.Say)
	}
	// FORWARD IS READ BEFORE SAY AND REFUSES TO SHARE A CALL WITH IT. They send
	// two different people's words, and a call carrying both is a call whose
	// author cannot be answered for (task_forward.go).
	if parsed.Forward {
		if strings.TrimSpace(parsed.Say) != "" {
			return forwardNotYours, true, nil
		}
		return a.forwardOneTask(ctx, entry, id, here)
	}
	if say := strings.TrimSpace(parsed.Say); say != "" {
		if !here {
			return fmt.Sprintf("Task %s ran in an earlier conversation, so there is nobody left to say it to. Propose the work again if it needs doing differently.", entry.ID), true, nil
		}
		// THE MODEL IS NOT THE PERSON, AND THE WORKER MUST BE ABLE TO TELL. This
		// tool call is coordination between two conversations in one session; it
		// took [Agent.SteerTask] until now, which is the door the person's own
		// words come through, so the worker journaled the model's sentence as the
		// person's correction and could read "you may change the schema" as a
		// grant nobody with authority had given ([Agent.relayToTask]). On the
		// node's record the same origin is what keeps this line from ever being
		// cited to move the done-condition (assignment.go).
		receipt, err := a.relayToTask(id, say)
		if err != nil {
			return capitalized(err.Error()) + ".", true, nil
		}
		waiting := receipt.Waiting
		if receipt.Held {
			return fmt.Sprintf("said to task %s: %s\nIts work is being checked, so nobody read it yet; it is on the task's record and its next round reads it. It cannot change what that task is judged by — only the person's own direction does that.", entry.ID, say), false, nil
		}
		// AND WHICH KIND OF WAIT IT LANDED IN. A task that has handed its own
		// pieces out is parked on their reports and has no step coming
		// (task_run.go's [TaskGraph.park]), so the line wakes it instead of
		// riding a turn already running — which is the difference between an
		// answer now and an answer the model would otherwise expect at the next
		// step of a task that is not taking one.
		//
		// AND IT ARRIVES AS YOURS. The answer says so plainly, because a model
		// told its line lands "as the person's own words" will use this tool to
		// give itself permissions: the worker reads it named as another agent in
		// this session ([relayNote]), and only the person's own door speaks for
		// the person.
		arrival := "It arrives in its loop named as this conversation speaking, not as the person."
		if waiting {
			arrival = "It was waiting on the pieces it handed out; your line wakes it, and arrives named as this conversation speaking, not as the person."
		}
		return fmt.Sprintf("said to task %s: %s\n%s Nothing you say here changes what it is allowed to do, and its done-condition is unchanged: only the person's own direction can move that.", entry.ID, say, arrival), false, nil
	}
	if !here {
		// Its row, and the truth about why there is no more: the graph that ran
		// it died with the conversation that owned it, and its transcript is the
		// whole of what is left.
		if entry.Live() {
			return taskRowText(entry) + "\nIt was running when the conversation that owns it ended, so this session cannot see it work. Its transcript is the whole of it.\n", false, nil
		}
		return taskRowText(entry), false, nil
	}
	live, state, ok := a.taskLiveRead(id, taskTailLines(parsed.Lines))
	if !ok {
		return taskRowText(entry), false, nil
	}
	return taskLiveText(live, state), false, nil
}

// stopOneTask is the stop verb: the model ending running work through the very
// door a person's own stop pulls ([Agent.CancelWithReason]).
//
// ONE DOOR, SO THE CONSEQUENCES CANNOT DIVERGE. The cut, the kept branch, the
// `stopped` row on the rail and the landing that arrives a moment later are the
// same whichever hand asked for them — which is the whole reason this reaches for
// Cancel rather than settling the node here.
//
// IT ASKS NOBODY, AND THAT IS THE ONE DIFFERENCE WORTH KNOWING. The person's
// road draws a card and waits for an answer (internal/tui3's stop.go); this call
// IS the answer to a person who has already said stop, and a second question
// would be asking them to confirm their own sentence.
//
// AND A TASK THAT IS NOT RUNNING IS NOT AN ERROR. "Stop task 2" over work that
// landed a minute ago is a reasonable thing to have said, and what the asker is
// owed back is what task 2 actually IS — done, stopped, your call — in the words
// they are reading on the screen (task_status.go's tier words), rather than a
// refusal that leaves the model guessing whether it ended anything.
func (a *Agent) stopOneTask(entry TaskIndexEntry, id uint64, here bool, why string) (string, bool, error) {
	if !here {
		// A ROW STILL CLAIMING TO BE LIVE IS THE ONE REFUSAL HERE. The stop door
		// is this session's graph and the id belongs to another conversation's, so
		// there is nothing here to end and nobody to end it — the same fact
		// steering and resolving already give for the same row.
		if entry.Live() {
			return fmt.Sprintf("Could not stop task %s: it is running in another conversation and has to be stopped in that window.", entry.ID), true, nil
		}
		return fmt.Sprintf("task %s ran in an earlier conversation and is %s; there is nothing to stop.", entry.ID, taskEntryWord(entry)), false, nil
	}
	if node := a.taskNode(id); node != nil {
		if status := ProjectTask(node.notice().StatusFacts()); status.Settled() {
			return fmt.Sprintf("task %s is %s; there is nothing to stop.", entry.ID, status.RowWord()), false, nil
		}
	}
	line, err := a.CancelWithReason(CancelTask+":"+strconv.FormatUint(id, 10), why)
	if err != nil {
		return fmt.Sprintf("Could not stop task %s: %s.", entry.ID, strings.TrimSuffix(err.Error(), ".")), true, nil
	}
	if strings.TrimSpace(line) == "" {
		return fmt.Sprintf("Could not stop task %s: codeaf received no answer that the work stopped.", entry.ID), true, nil
	}
	// AND THE ANSWER SAYS THE THING THE OLD WORKAROUND GOT WRONG. A task told in
	// words to stop still ended as an unfinished run and was sent back to close
	// its gaps; a task STOPPED never reaches the check at all
	// (task_run.go's [Agent.settleUnfinished]), and the model has to know that so
	// it does not go looking for a landing that will never be judged.
	return line + ". Its landing arrives the way every task's does; it is not checked and nothing re-runs it.", false, nil
}

// continueOneTask is the continue verb: the model re-arming a settled node
// ([Agent.ContinueTask]) instead of proposing a new one.
//
// IT IS THIS SESSION'S GRAPH OR NOTHING, exactly as steering is. A failed
// task from a conversation that has ended is a row in the index and a
// branch in the repository; there is no graph left to put it back on, and
// telling the model it had continued something would be this tool inventing
// an effect it did not have.
func (a *Agent) continueOneTask(entry TaskIndexEntry, id uint64, here bool, words string) (string, bool, error) {
	if !here {
		return continueNoGraph(entry.ID, entry), true, nil
	}
	if err := a.ContinueTask(id, words); err != nil {
		return capitalized(err.Error()) + ".", true, nil
	}
	if strings.TrimSpace(words) != "" {
		return fmt.Sprintf("continuing task %s from where it left off, with your words as this round's finding. Same node, same working copy, same brief. %s",
			entry.ID, taskHandoffWakeSentence), false, nil
	}
	return fmt.Sprintf("continuing task %s from where it left off. Same node, same working copy, same brief; the last report is this round's finding. %s",
		entry.ID, taskHandoffWakeSentence), false, nil
}

// resolveOneTask is the resolve verb: the model settling a node whose auditor
// never gave a verdict ([Agent.ResolveUnverified]).
//
// IT IS THIS SESSION'S GRAPH OR NOTHING, exactly as steering is. An unverified
// node from a conversation that has ended is a row in the index and a branch in
// the repository; there is no graph left to move it in, no dependents left
// waiting on it, and telling the model it had settled something would be this
// tool inventing an effect it did not have.
func (a *Agent) resolveOneTask(entry TaskIndexEntry, id uint64, here bool, resolution TaskResolution, why string) (string, bool, error) {
	if !here {
		return fmt.Sprintf("Task %s ran in an earlier conversation, so there is no graph left to settle it in. Its work is at %s — read it, and propose what still needs doing.",
			entry.ID, taskWhereWord(entry)), true, nil
	}
	// THE MODEL'S OWN VERB GOES THROUGH THE MODEL'S OWN DOOR, so the receipt on
	// the work says who spent it (task_audit.go's [Agent.resolveUnverifiedBy]).
	if err := a.resolveUnverifiedBy(id, resolution, why, TaskAskOwnerModel); err != nil {
		return capitalized(err.Error()) + ".", true, nil
	}
	return resolvedReply(entry.ID, a.taskNode(id), resolution), false, nil
}

// resolvedReply is WHAT ACTUALLY HAPPENED, read off the node after the door
// returned rather than written from what was asked for.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A divided task landed `your call`, the model spent `accept` on it, and this
// reply said "Its branch has come home and anything waiting on it can start."
// The branch had not come home: the merge was refused by the person's own
// uncommitted copies of the very files the task wrote, the node was still
// unverified with its branch kept, and the model — told the work had landed —
// told the person "Task 1 is done and accepted" (#767).
//
// ── THE LAW ──
//
// A TOOL REPLY IS A READING OF THE WORLD AND NEVER A RESTATEMENT OF THE REQUEST.
// [Agent.acceptTask] can settle a node three ways — home and done, kept where it
// is and done, or still somebody's call with the branch kept — and each of them
// is a different next move for whoever reads this. So the state and the merge
// are read back, the projection's own sentence says what is still being asked
// (task_status.go's [TaskAsk]), and the words are the words every surface uses.
func resolvedReply(id string, node *TaskNode, resolution TaskResolution) string {
	if node == nil {
		// The node went out from under the answer — another window, a late check.
		// Nothing here can say what became of it, and saying anything would be a
		// guess about somebody else's landing.
		return fmt.Sprintf("task %s took that answer. Call tasks with its number to see where it stands.", id)
	}
	notice := node.notice()
	status := ProjectTask(notice.StatusFacts())
	if resolution == TaskReaudit {
		return fmt.Sprintf("a fresh checker is looking at task %s again. It stays waiting until that answer lands, and you will hear the outcome the way every other task lands.", id)
	}
	// STILL SOMEBODY'S CALL IS THE HEADLINE WHEN IT IS TRUE. It is the one
	// outcome a reader must not miss, whichever verb was spent: nothing merged,
	// nothing failed, and the question is standing exactly where it was.
	if status.Tier == TaskTierYourCall {
		said := fmt.Sprintf("task %s is still your call", id)
		if reason := strings.TrimSpace(status.Ask.Reason); reason != "" {
			said += " — " + reason
		}
		if branch := strings.TrimSpace(notice.Branch); branch != "" {
			said += " · its branch " + branch + " was kept"
		}
		return said + ". Nothing merged and nothing failed: say what is in the way and leave the choice with the person."
	}
	if resolution == TaskRefute {
		said := fmt.Sprintf("task %s is incomplete on your word", id)
		if branch := strings.TrimSpace(notice.Branch); branch != "" {
			said += ": its branch " + branch + " is kept"
		}
		return said + ", and anything waiting on it stops with it."
	}
	said := fmt.Sprintf("accepted task %s as done on your reading of it, with nobody else's check behind it.", id)
	if notice.Merge == mergeMerged {
		if branch := strings.TrimSpace(notice.Branch); branch != "" {
			return said + " Its branch " + branch + " merged into yours and anything waiting on it can start."
		}
		return said + " Its work is home and anything waiting on it can start."
	}
	// DONE WITHOUT A MERGE IS ITS OWN FACT AND IS SAID. A tree that would not take
	// the work settles the node anyway rather than asking the same question again
	// (task_audit.go), and where the work is sitting is the whole of what is left
	// to say about it.
	if branch := strings.TrimSpace(notice.Branch); branch != "" {
		return said + " Its branch " + branch + " did not merge and was kept, so the work is there rather than in the person's checkout."
	}
	return said + " Anything waiting on it can start."
}

// continueNoGraph is the honest refusal when this session cannot re-arm the
// named task: unknown id, another conversation's row, a number this graph
// never held. It names the miss and the real road — read the branch or
// working copy the row still points at — and never a sentence that reads as
// progress. The two call sites (a lookup miss, and a row from an earlier
// window) share one string so the model cannot be taught two stories about
// the same fact.
func continueNoGraph(token string, entry TaskIndexEntry) string {
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		id = strings.TrimSpace(token)
	}
	if where := taskWhereWord(entry); where != "" && where != taskWhereNowhere {
		return fmt.Sprintf("There is no graph in this session for task %s, so it cannot be continued here. Its work is at %s — read that branch or working copy; do not narrate progress as if the task resumed. Propose what still needs doing only if the objective itself changed.",
			id, where)
	}
	return fmt.Sprintf("There is no graph in this session for task %s, so it cannot be continued here. Call tasks with no arguments to see what this conversation holds; a number from another window is not this session's to re-arm.",
		id)
}

// taskWhereNowhere is the emptiness-law stand-in [taskWhereWord] returns when
// a row names no artifact and no transcript. [continueNoGraph] treats it as
// no road, so the refusal does not point at a place that is not there.
const taskWhereNowhere = "nowhere this session can point at"

// taskWhereWord names where a task's work is, for a reader who has just been
// told this session cannot touch it: the artifact if the row has one, else the
// transcript, else nothing worth pointing at.
func taskWhereWord(entry TaskIndexEntry) string {
	if entry.ArtifactURI != "" {
		return entry.ArtifactURI
	}
	if entry.TranscriptURI != "" {
		return entry.TranscriptURI
	}
	return taskWhereNowhere
}

// taskRows is what this tool answers from: the project's whole index in a
// conversation, and in a node ONLY THE CHILDREN IT HANDED OUT ITSELF.
//
// The scope is applied here, at the one door, rather than in each of the three
// ops: a search, a read, a steer and a resolve are four things to do with a
// task, and "which tasks can this agent see" is one answer for all of them.
func (a *Agent) taskRows() []TaskIndexEntry {
	parent := a.config.taskID
	if parent == 0 {
		return a.TaskIndex()
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	kids := a.tasker().children(parent)
	rows := make([]TaskIndexEntry, 0, len(kids))
	for _, kid := range kids {
		kid.graph.mu.Lock()
		rows = append(rows, kid.indexEntryLocked(session))
		kid.graph.mu.Unlock()
	}
	sortTaskIndex(rows)
	return rows
}

// taskByToken finds the ONE task a token names, and it asks THIS CONVERSATION'S
// LIVE GRAPH BEFORE IT ASKS THE FILE.
//
// THE INDEX IS THE PROJECT'S AND NOT THE CONVERSATION'S, which is the whole
// reason this function exists. Every conversation in a project appends to one
// file (task_index.go's [Config.taskIndexFile]) and ids restart with every
// conversation, so the project's rows hold as many task 2s as it has had
// conversations — and [LookupTask] answers with the NEWEST of them, which is
// right for a slug and wrong for a number. On 2026-09-09 a conversation holding
// a recovered task 2 asked to settle it, the number matched a DIFFERENT
// conversation's newer row, and the model was refused with "there is no graph
// left to settle it in" and pointed at a stranger's worktree — over a node this
// session was holding all along.
//
// A NAME STILL MEANS THE NEWEST. A slug is derived from a title and carries no
// conversation in it, so "the nil-map task" said out loud means the last one
// whoever ran it, exactly as it always did.
//
// AND A NODE ASKS FOR NOTHING WIDER THAN ITS OWN FAMILY. Inside a node
// [Agent.taskRows] is already only the pieces it handed out, and reaching into
// the graph by id here would walk straight past that scope — so this preference
// is a conversation's alone.
func (a *Agent) taskByToken(rows []TaskIndexEntry, token string) (TaskIndexEntry, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return TaskIndexEntry{}, false
	}
	if a.config.taskID == 0 {
		if id := taskIDNumber(token); id != 0 && a.taskNode(id) != nil {
			a.mu.Lock()
			session := a.sessionID()
			a.mu.Unlock()
			for _, entry := range rows {
				if entry.SessionID == session && strings.TrimSpace(entry.ID) == token {
					return entry, true
				}
			}
		}
	}
	return LookupTask(rows, token)
}

// thisSessionTask resolves a row to a node of THIS session's graph. A row from
// another conversation, or one whose id this graph never held, is not one: ids
// restart with every conversation (see [TaskIndexEntry.ID]), so the pair is what
// identifies a node and matching on the number alone would read a live task 7 as
// last month's task 7.
//
// A RECOVERED GRAPH IS THIS SESSION'S GRAPH. A conversation that reopened a
// checkpoint is holding those nodes now — the person's own doors reach them by
// id ([Agent.HandUnverifiedToModel], [Agent.Cancel]) — and the rows the graph
// writes for them carry THIS session's id ([Agent.liveTaskRows]), so the test
// below is already true for them. What was not true was the row this reached:
// see [Agent.taskByToken], which is where that was fixed.
func (a *Agent) thisSessionTask(entry TaskIndexEntry) (uint64, bool) {
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	if entry.SessionID != session {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(entry.ID), 10, 64)
	if err != nil || a.taskNode(id) == nil {
		return 0, false
	}
	return id, true
}

// taskTailLines bounds one live read's tail, clamping rather than refusing for
// [parseWatchArguments]'s reason: a model asking for a thousand lines means "as
// much as I can have", and the nearest legal answer is what it meant.
func taskTailLines(asked int) int {
	switch {
	case asked <= 0:
		return taskLiveDefaultTail
	case asked > taskLiveMaxTail:
		return taskLiveMaxTail
	default:
		return asked
	}
}

// taskLiveText is one running task, in full: its row, then the tail of what it
// has been doing, then the one thing the reader can do about it.
//
//	7 · fix-the-nil-map-crash · running · running for 4m 12s · 2 files · $0.31
//	  Fix the nil-map crash in the reconciler
//	  live: bash go test ./internal/reconciler/… · 12s
//	  artifact file:///…/task-7 · transcript file:///…/20260816-101500_7.jsonl
//
//	its last 4 lines:
//	· read internal/reconciler/state.go
//	The map is written without the guard; I will add it and a test.
//	· edit internal/reconciler/state.go
//	· bash go test ./internal/reconciler/…
//
//	Steer it with tasks id 7 say "…". The whole story is in its transcript.
//
// THE TAIL IS NOT THE WORK. It is the last few lines of a node's narrative and
// its calls — enough to tell working from circling — and the transcript URI in
// the row above it is where the rest is. A tool that answered with everything a
// node had done would put a session's worth of somebody else's greps into a
// context window over the question "how is it going".
func taskLiveText(entry TaskIndexEntry, state taskLiveState) string {
	out := taskRowText(entry)
	if entry.Status != string(TaskRunning) {
		// Either it landed between the model deciding to ask and the read
		// happening — the row carries the outcome and the URIs, which is the
		// whole answer — or it has not started, and a node with no worker in it
		// has nothing to quote and nobody to steer.
		if entry.Status == string(TaskQueued) {
			return out + "\nIt has not started: it is waiting on the work it depends on, or on a slot.\n"
		}
		return out
	}
	var rendered strings.Builder
	rendered.WriteString(out)
	if len(state.Lines) == 0 {
		rendered.WriteString("\nIt has not said anything yet.\n")
	} else {
		fmt.Fprintf(&rendered, "\nits last %s:\n", countedLines(len(state.Lines)))
		for _, line := range state.Lines {
			rendered.WriteString(line + "\n")
		}
	}
	fmt.Fprintf(&rendered, "\nSteer it with tasks id %s say \"…\". The whole story is in its transcript.\n", entry.ID)
	return rendered.String()
}

// capitalized starts a sentence the way the rest of this tool's answers start.
// The doors in task_room.go phrase their refusals for a caller, lower case; a
// tool result is read as prose.
func capitalized(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// taskRowsText is the answer, and it is written for a reader that has to decide
// what to open next.
//
// ROOTS KEEP THEIR FULL ROW. Children sit under their root in one line because
// the root's transcript names the family; a queried child also carries its own
// URI so the caller can go straight to the matching work. With no query a
// family is collapsed to its node count.
func taskRowsText(rows []TaskIndexEntry, query string) string {
	return taskRowsTextLimit(rows, query, taskSearchLimit)
}

func taskRowsTextLimit(rows []TaskIndexEntry, query string, limit int) string {
	query = strings.TrimSpace(query)
	matches := SearchTaskIndex(rows, query, limit)
	if len(matches) == 0 {
		if strings.TrimSpace(query) == "" {
			return "No tasks have run in this project yet."
		}
		return fmt.Sprintf("No task matches %q. Try fewer words, or call tasks with no query to see the most recent ones.", strings.TrimSpace(query))
	}
	byKey := make(map[string]TaskIndexEntry, len(rows))
	children := make(map[string][]TaskIndexEntry)
	for _, entry := range rows {
		key := taskFamilyKey(entry.SessionID, entry.ID)
		byKey[key] = entry
		if entry.Parent != "" {
			parent := taskFamilyKey(entry.SessionID, entry.Parent)
			children[parent] = append(children[parent], entry)
		}
	}

	// A query returns matching rows, but a matching child is read in the family
	// it belongs to. Roots are inserted once, immediately before their hits.
	var display []TaskIndexEntry
	seen := make(map[string]bool, len(matches))
	for _, entry := range matches {
		if entry.Parent != "" {
			parentKey := taskFamilyKey(entry.SessionID, entry.Parent)
			if root, ok := byKey[parentKey]; ok {
				if !seen[parentKey] {
					display = append(display, root)
					seen[parentKey] = true
				}
				if query == "" {
					continue
				}
			}
		}
		key := taskFamilyKey(entry.SessionID, entry.ID)
		if !seen[key] {
			display = append(display, entry)
			seen[key] = true
		}
	}

	var out strings.Builder
	for at, entry := range display {
		if at > 0 {
			out.WriteString("\n")
		}
		_, parentPresent := byKey[taskFamilyKey(entry.SessionID, entry.Parent)]
		if entry.Parent != "" && parentPresent {
			out.WriteString(taskChildRowText(entry, query != ""))
			continue
		}
		out.WriteString(taskRowText(entry))
		if query == "" {
			if count := len(children[taskFamilyKey(entry.SessionID, entry.ID)]); count > 0 {
				fmt.Fprintf(&out, "  … %s under it — tasks %s for the family\n", taskNodeCount(count), entry.ID)
			}
		}
	}
	return out.String()
}

func taskFamilyKey(session, id string) string { return session + "\x00" + id }

// taskEntryWord is the word a row wears, in the SAME vocabulary the person
// reading over the model's shoulder is looking at: `done`, `stopped`,
// `incomplete` with its reason, `your call` (task_status.go's tier words). The
// engine's own state word is what this used to print, and a model that read
// `failed` off a row told somebody their work had failed when a connection had
// dropped.
//
// A ROW THE READING CANNOT PLACE KEEPS THE STATE IT CLAIMS. A record written by
// a build this one does not know is still a row, and printing nothing for it
// would lose the only thing it says about itself.
func taskEntryWord(entry TaskIndexEntry) string {
	// held: the record is not an authority on whether anything is behind a
	// live-looking row, and answering false here would call every running row in
	// the file dead (task_status.go's [TaskIndexEntry.StatusFacts]).
	if word := ProjectTask(entry.StatusFacts(true)).RowWord(); word != "" {
		return word
	}
	return entry.Status
}

func taskNodeCount(count int) string {
	if count == 1 {
		return "1 node"
	}
	return strconv.Itoa(count) + " nodes"
}

func taskChildRowText(entry TaskIndexEntry, withURI bool) string {
	parts := []string{entry.Title, taskEntryWord(entry)}
	if word := taskWhenWord(entry); word != "" {
		parts = append(parts, word)
	}
	out := "  " + strings.Join(parts, " · ") + "\n"
	if withURI {
		if where := taskWhereClauses(entry); len(where) > 0 {
			out += "    " + strings.Join(where, " · ") + "\n"
		}
	}
	return out
}

// taskRowText is one task, in the shape a person would read out.
//
//	7 · fix-the-nil-map-crash · done · ended 3h ago · 4 files · 2m 10s · $0.31
//	  Added the guard and the regression test; the parser suite passes.
//	  artifact git:task/fix-the-nil-map-crash-9c1a2f · transcript file:///…/20260816-101500_7.jsonl
//
// A RUNNING ROW CARRIES ONE MORE LINE — what it is doing right now — and no
// landed row ever does: see [TaskIndexEntry.Activity].
//
// Every clause that has nothing to say is DROPPED rather than written empty. A
// row reading "· 0 files · · $0.00" is three facts this build does not have,
// stated as though it did.
func taskRowText(entry TaskIndexEntry) string {
	parts := []string{entry.ID, entry.Name, taskEntryWord(entry)}
	if word := taskWhenWord(entry); word != "" {
		parts = append(parts, word)
	}
	if entry.FilesChanged > 0 {
		parts = append(parts, taskFilesWord(entry.FilesChanged))
	} else if strings.TrimSpace(entry.FilesUnread) != "" {
		// A LIST NOBODY COULD READ IS SAID, not dropped: dropped, it reads the
		// same as work that wrote nothing ([TaskIndexEntry.FilesUnread]).
		parts = append(parts, deltaFilesUnknown)
	}
	if entry.DurationMS > 0 {
		parts = append(parts, taskSpanWord(entry.Duration()))
	}
	if entry.Cost > 0 {
		parts = append(parts, "$"+strconv.FormatFloat(entry.Cost, 'f', 2, 64))
	}
	out := strings.Join(parts, " · ") + "\n  " + entry.Title
	if entry.Outcome != "" {
		out += "\n  " + entry.Outcome
	}
	// A RUNNING ROW SAYS WHAT IS HAPPENING IN IT, in the place a landed row says
	// what came of it. "running for 4m" is equally true of a node calling a tool
	// every second and of one stuck on the same `go test` since minute one, and
	// a model deciding whether to leave work alone cannot tell those apart from
	// a clock (task_live.go).
	if entry.Activity != "" {
		out += "\n  live: " + entry.Activity
	}
	if where := taskWhereClauses(entry); len(where) > 0 {
		out += "\n  " + strings.Join(where, " · ")
	}
	return out + "\n"
}

// taskWhereClauses is the trailing line a row may carry: where the work IS, the
// record's own verdict when the work did not settle whole, and where the STORY
// is. It is ONE builder for a root row and a queried child, so the two can never
// name a kept branch or a verdict differently for the same work.
//
// THE KEPT BRANCH IS NAMED BEFORE THE ARTIFACT, and the bare artifact is dropped
// when it only repeats it: a row whose work did not land spells the same branch
// as `git:<b>` in its artifact URI, and the explicit `kept branch` clause is the
// one a reader told "1 incomplete" can act on.
//
// THE VERDICT IS THE RECORD'S OWN WORD — `failed` or `unverified`
// (task_contract.go's [TaskFailed]/[TaskUnverified]), read off the row's own
// [TaskIndexEntry.Status] — the SAME word #1182 put on the headless envelope, so
// a reader joining the two reads one vocabulary and not two. It is shown even
// when no branch was kept, because the word is the record's and does not depend
// on git.
func taskWhereClauses(entry TaskIndexEntry) []string {
	var where []string
	branch := strings.TrimSpace(entry.Branch)
	if branch != "" {
		where = append(where, "kept branch "+branch)
	}
	if entry.ArtifactURI != "" && entry.ArtifactURI != "git:"+branch {
		where = append(where, "artifact "+entry.ArtifactURI)
	}
	if word := taskVerdictWord(entry); word != "" {
		where = append(where, "verdict "+word)
	}
	if entry.TranscriptURI != "" {
		where = append(where, "transcript "+entry.TranscriptURI)
	}
	return where
}

// taskVerdictWord is the record's own word for a row that did not settle whole,
// or "" for one that did or has not settled. Its two words are the engine's own
// states and not a second vocabulary: [TaskFailed] and [TaskUnverified] are
// exactly what #1182's envelope carries as `verdict`.
func taskVerdictWord(entry TaskIndexEntry) string {
	switch TaskState(entry.Status) {
	case TaskFailed, TaskUnverified:
		return entry.Status
	}
	return ""
}

// taskWhenWord says when, in the tense the row is in: a live task has been
// running for a while, a landed one ended a while ago.
func taskWhenWord(entry TaskIndexEntry) string {
	if entry.Live() {
		// A QUEUED NODE HAS NO CLOCK YET. Its age is zero because it has not
		// started, and "running" beside a status that says "queued" is the row
		// contradicting itself in the space of four words.
		if entry.Status == string(TaskQueued) {
			return ""
		}
		if span := taskSpanWord(entry.Duration()); span != "" {
			return "running for " + span
		}
		return "running"
	}
	if entry.EndedAt.IsZero() {
		return ""
	}
	return "ended " + TaskAgeWord(time.Since(entry.EndedAt)) + " ago"
}

func taskFilesWord(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

// taskSpanWord spells how long something took: seconds, then minutes and
// seconds, then hours and minutes. It is [countUpWord]'s grammar in
// internal/tui3, restated here because that one is a surface's and this one goes
// on the wire.
func taskSpanWord(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m " + strconv.Itoa(int(d%time.Minute/time.Second)) + "s"
	default:
		return strconv.Itoa(int(d/time.Hour)) + "h " + strconv.Itoa(int(d%time.Hour/time.Minute)) + "m"
	}
}

// TaskAgeWord spells how long ago, coarsely, in the one unit that matters at
// that distance. It is exported because the "@" drop-up and the pointer block
// (internal/tui3) say the same ages about the same rows, and two spellings of
// "3h" on one screen is two vocabularies for one fact.
func TaskAgeWord(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	default:
		return strconv.Itoa(int(d/(30*24*time.Hour))) + "mo"
	}
}

// runPlanTasks is the rows of the run this conversation handed work to, read
// from the run's own store, and nil for a conversation with no run.
//
// THE `tasks` TOOL WAS BLIND TO THEM (2026-09-18, the real binary): a hand-off
// ran, split in two, was checked and landed, and one turn later `tasks {"id":
// "1"}` answered `No task "1" in this project` beside a rail showing `#1 done`.
// Told no task existed, the conversation set out to verify the work by running
// the suite itself. The rows are read where the surface reads them
// ([Agent.PlanTasks]), so the tool and the rail cannot disagree about what ran.
func (a *Agent) runPlanTasks() []PlanTaskRow {
	g := a.graph()
	if g == nil {
		return nil
	}
	plan := g.planIfArmed()
	if plan == nil || plan.chat == "" {
		return nil
	}
	return a.PlanTasks()
}

// planTaskLabels names every row of a run THE WAY A PERSON SEES IT, and never by
// a store id. A task the conversation handed off is stored under the number its
// card and the rail show, so it is `#2`; a part the run made for itself has no
// number of its own, so it is its parent's name and its place under that parent,
// `#2.1`, in the store's own order, which does not move between reads.
func planTaskLabels(rows []PlanTaskRow) map[string]string {
	labels := make(map[string]string, len(rows))
	parts := make(map[string]int, len(rows))
	for _, row := range rows {
		bare := planTaskID(row.ID)
		if _, err := strconv.ParseUint(bare, 10, 64); err == nil {
			labels[row.ID] = "#" + bare
			continue
		}
		parent, known := labels[row.Parent]
		if !known {
			parent = "#"
		}
		parts[row.Parent]++
		labels[row.ID] = strings.TrimSuffix(parent, ".") + "." + strconv.Itoa(parts[row.Parent])
	}
	return labels
}

// planTasksText is the run's tasks as a listing: the name a person sees, the
// title, the state, the first line of what came back, and the newest note
// anybody left on it. Empty when there is no run or nothing in it matches, so
// the caller's own listing stands alone.
//
// THE NOTE IS ON THE ROW BECAUSE NOBODY WAS READING IT. A note is the channel a
// worker uses to say that another task's premise is wrong, and a person uses to
// tell a task something the plan does not hold; both were drawn on the task's
// page and nowhere else, so the conversation — the one mind holding what the
// person actually asked for — could list every row of a run and never learn
// that one of them had found the plan wrong. The row carries the newest note
// alone, cut to a line: the whole of them is [Agent.planTaskText]'s job, and a
// listing that grew with every note would be the context this tool exists to
// spend sparingly.
func (a *Agent) planTasksText(rows []PlanTaskRow, query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	labels := planTaskLabels(rows)
	var b strings.Builder
	for _, row := range rows {
		page, _ := a.PlanTaskPage(row.ID)
		// THE ROW SAYS THE RAIL'S WORD ([PlanTaskRow.StateWord]), the one the
		// digest in front of the person's sentence says of the same row. A search
		// still matches the store's own word too, because a model that has read
		// `claimed` in a worker's own output will search for it.
		word := row.StateWord()
		if query != "" && !strings.Contains(strings.ToLower(row.Title+" "+word+" "+row.Status+" "+page.Result+" "+row.Note), query) {
			continue
		}
		fmt.Fprintf(&b, "%s · %s", labels[row.ID], cutChars(row.Title, runAskLineChars))
		if word != "" {
			fmt.Fprintf(&b, " · %s", word)
		}
		if line := summaryFirstLine(page.Result, runAskLineChars); line != "" {
			fmt.Fprintf(&b, " · %s", line)
		}
		if note := summaryFirstLine(row.Note, runAskLineChars); note != "" {
			fmt.Fprintf(&b, " · note: %s", note)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// stopPlanRow ends one row of the live run, named the way the model read it in
// the listing or the digest — `#2`, `#2.1` — and answers false for a token that
// names no row of this run, so the caller goes on to the session tree's own
// reader exactly as before.
//
// TWO ROADS, BECAUSE THE TWO KINDS OF ROW ARE DIFFERENT THINGS. A row whose id
// is a number is a hand-off: the run publishes it under that number, and the
// number is what a person's own stop names ([Agent.CancelWithReason] →
// stoprun.go), which ends the run itself when the number is the run's and one
// joined hand-off when it is not — settling the row so a stopped part does not
// go on spinning until everything beside it finishes. A part the run made for
// itself has no number of its own, so it is ended through the store the way the
// task page's own `x stop it` ends one ([Agent.PlanCancel]), which cascades to
// its descendants and to the work hard-depending on it.
//
// THE ANSWER SAYS WHAT A STOP DOES AND DOES NOT DO, for stopOneTask's reason: a
// model that goes looking for a landing to judge is a model spending a turn on
// work nothing will check.
func (a *Agent) stopPlanRow(token, why string) (string, bool) {
	rows := a.runPlanTasks()
	labels := planTaskLabels(rows)
	want := "#" + strings.TrimPrefix(strings.TrimSpace(token), "#")
	for _, row := range rows {
		if labels[row.ID] != want {
			continue
		}
		bare := planTaskID(row.ID)
		// The parse is the QUESTION and not the value: a row whose id is a number
		// is a hand-off the run published under that number, and the number is
		// what the person's own stop names. Anything else is a part the run made
		// for itself, which has no number and takes the store's road below.
		if _, err := strconv.ParseUint(bare, 10, 64); err == nil {
			line, stopErr := a.CancelWithReason(CancelTask+":"+bare, strings.TrimSpace(why))
			if stopErr != nil {
				return capitalized(stopErr.Error()) + ".", true
			}
			return line + ". It is not checked and nothing re-runs it.", true
		}
		if err := a.PlanCancel(row.ID); err != nil {
			return capitalized(err.Error()) + ".", true
		}
		return fmt.Sprintf("stopped %s · %s%s. Everything under it and the work waiting on it ends with it; it is not checked and nothing re-runs it.",
			want, cutChars(row.Title, runAskLineChars), stopReasonClause(why)), true
	}
	return "", false
}

// stopReasonClause is the reason a stop carried, said the way the answer reads
// it, and nothing at all when the caller gave none — the emptiness law applied
// to a sentence rather than to a screen.
func stopReasonClause(why string) string {
	if why = strings.TrimSpace(why); why != "" {
		return " — " + why
	}
	return ""
}

// planNoteFromChat writes one note onto a row of the live run, named the way
// the model read it — `#2`, `#2.1` — and answers the sentence the model reads
// back. A token that names no row of this run falls through to false, and the
// caller says so in the words it already has for an id it does not hold.
//
// THE RECEIPT SAYS WHAT THE NOTE CANNOT DO, in the same breath as the delivery.
// A model told only that its words arrived will reach for this field again the
// next time it wants a task to do something different, and a note that was read
// as a direction is a task quietly working to a contract nobody versioned. So
// the answer says that nothing the conversation holds moves a work order, and
// names what it does hold ([Agent.writePlanNote]).
func (a *Agent) planNoteFromChat(token, note string) (string, bool, error) {
	row, want, ok := a.planRowNamed(token)
	if !ok {
		return fmt.Sprintf("No task %q in this run. Call tasks with no arguments to see its rows.", token), true, nil
	}
	return a.writePlanNote(row, want, note, "")
}

// writePlanNote puts one note onto a row this conversation has already found,
// and answers the receipt. lead is a sentence said before the receipt — the
// one `say` owes about the door its words actually went through.
//
// THE RECEIPT NAMES NO VERB THIS CONVERSATION HAS NOT GOT. It used to send the
// model to `revise_assignment`, which is a WORKER'S verb and is never on a
// conversation's belt ([Config.mayRevise]): a model told the door was there went
// looking for a tool it did not hold. What the conversation does hold over a row
// whose work is wrong is `stop`, and a fresh hand-off with the right ask.
func (a *Agent) writePlanNote(row PlanTaskRow, want, note, lead string) (string, bool, error) {
	if err := a.PlanNoteFromChat(row.ID, note); err != nil {
		return capitalized(err.Error()) + ".", true, nil
	}
	return fmt.Sprintf("%snoted on %s · %s: %s\nIts worker is handed it as soon as the step it is on ends, and it stays on the row for the person and the other workers. It does not change what that task was asked for, and nothing you hold does: if the work itself is wrong, stop it and hand off the right ask.", lead, want, cutChars(row.Title, runAskLineChars), note), false, nil
}

// planRowNamed finds the row of this conversation's runs that a token names,
// spelled the way the model read it in the listing or the digest — `2`, `#2`,
// `#2.1` — and answers that row, its label, and false for a token that names
// none, so the caller goes on to the session tree's own reader as before.
func (a *Agent) planRowNamed(token string) (PlanTaskRow, string, bool) {
	rows := a.runPlanTasks()
	labels := planTaskLabels(rows)
	want := "#" + strings.TrimPrefix(strings.TrimSpace(token), "#")
	for _, row := range rows {
		if labels[row.ID] == want {
			return row, want, true
		}
	}
	return PlanTaskRow{}, "", false
}

// planSayLead is what a `say` routed onto a run row answers before its
// receipt. The model asked for one door and was given another, and it must know
// which, because the two promise different things: a line into a node of this
// session's own tree, against a note on a row of the run that its worker is
// handed at its next step.
const planSayLead = "A row of the run takes `say` as a note, so your line went through `note`: "

// planForwardRefusal is what `forward` answers for a row of the run. It is a
// refusal, and it says why in the words the model needs to choose again: the
// person's words cannot move what a row of the run is judged by, because
// nothing this conversation holds can, and `note` is the door that carries
// their words to the worker as information.
const planForwardRefusal = "%s is a row of the run, and `forward` does not reach one: nothing you hold can move what a run's task is judged by. Put the person's words on it with `note` — its worker is handed them as soon as the step it is on ends, as information and not as an order — or, if the work itself is now wrong, `stop` it and hand off the right ask."

// planTaskText is ONE task of the run, answered from the store: what it was
// asked, what came back in full, what the run's checks found, and its last
// steps, each bounded the way [Agent.AskRun] bounds a read. It answers false for
// a name the run does not hold, and the caller goes on to the shipped reader.
func (a *Agent) planTaskText(rows []PlanTaskRow, token string) (string, bool) {
	labels := planTaskLabels(rows)
	want := "#" + strings.TrimPrefix(strings.TrimSpace(token), "#")
	id := ""
	for _, row := range rows {
		if labels[row.ID] == want {
			id = row.ID
			break
		}
	}
	if id == "" {
		return "", false
	}
	page, ok := a.PlanTaskPage(id)
	if !ok {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s", labels[id], cutChars(page.Row.Title, runAskLineChars))
	if word := page.Row.StateWord(); word != "" {
		fmt.Fprintf(&b, " · %s", word)
	}
	fmt.Fprintf(&b, "\n\nbrief:\n%s\n", cutChars(page.Description, runAskBodyChars))
	if page.Result != "" {
		fmt.Fprintf(&b, "\nresult:\n%s\n", cutChars(page.Result, runAskBodyChars))
	}
	for _, row := range rows {
		if row.Seat != "check" || row.ID == id {
			continue
		}
		if check, _ := a.PlanTaskPage(row.ID); check.Result != "" {
			fmt.Fprintf(&b, "\n%s · %s:\n%s\n", labels[row.ID], cutChars(row.Title, runAskLineChars), cutChars(check.Result, runAskBodyChars))
		}
	}
	// EVERY NOTE ON THE TASK, oldest first and bounded, between what came back
	// and the steps that made it. The page is where the whole channel is read:
	// the listing carries only the newest note, and a conversation that has seen
	// one and wants the rest opens the task rather than being handed all of them
	// on every row. The bound is [runAskNotesPerTask]'s, the same figure the run
	// summary spends on one task's notes, and the oldest go first because a
	// worker's later note usually answers its earlier one.
	notes := page.Notes
	if len(notes) > runAskNotesPerTask {
		notes = notes[len(notes)-runAskNotesPerTask:]
	}
	if len(notes) > 0 {
		b.WriteString("\nnotes:\n")
	}
	for _, note := range notes {
		fmt.Fprintf(&b, "%s · %s\n", planNoteAuthorWord(note), cutChars(note.Body, runAskLineChars))
	}
	steps := page.Steps
	if len(steps) > runAskNotesPerTask*4 {
		steps = steps[len(steps)-runAskNotesPerTask*4:]
	}
	if len(steps) > 0 {
		b.WriteString("\nlast steps:\n")
	}
	for _, step := range steps {
		fmt.Fprintf(&b, "%d · %s\n%s\n", step.Step, cutChars(step.Command, runAskLineChars), cutChars(step.Observation, runAskLineChars))
	}
	return b.String(), true
}

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

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
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
const tasksDescription = "Every task this project ever ran, and what runs now. No id searches; an id reads, steers, continues or settles one. \"continue task N\" / \"keep going on task N\" is id plus continue on a settled task — never a narration and never a new propose_task. Use it when the person means earlier work without pointing at it, or to look inside running work. Never to WAIT for handed-off work. A search also lists other windows' live work here, marked `another window`: no id in this conversation, so it cannot be read, steered, continued or resolved. Rows carry artifact and transcript URIs that read takes verbatim."

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
	`"id":{"type":"string","description":"A task's id (\"7\") or name (\"fix-the-nil-map-crash\"). Running, it answers with its LIVE state."},` +
	// The live answer is read off the running work itself (task_live.go) and
	// names what it is doing this second, how long it has been at it, its steps,
	// its spend and its last lines. That list is not spelled in the schema
	// because the answer itself carries it, and this string is paid for on every
	// request of every turn while this comment is free.
	`"lines":{"type":"integer","description":"Tail lines of a running task (default: ` + strconv.Itoa(taskLiveDefaultTail) + `, maximum: ` + strconv.Itoa(taskLiveMaxTail) + `)"},` +
	`"scope":{"type":"string","enum":` + taskScopeEnum + `,"description":"\"` + taskScopeProject + `\" (default) is this project alone; \"` + taskScopeEverywhere + `\" also lists live work in every OTHER project, grouped by project and as unreachable from here. A search only."},` +
	`"say":{"type":"string","description":"A line into the RUNNING task named by id: a correction, or a fact it lacks. With resolve, it is the REASON; with continue, this round's finding."},` +
	`"continue":{"type":"boolean","description":"The door for \"continue task N\" / \"keep going on task N\". Re-arm that settled task: same node, brief and working copy. A new propose_task is the wrong door."},` +
	`"resolve":{"type":"string","enum":` + TaskResolveEnum() + `,"description":"Settles a task nobody could check. accept: done on your own reading, branch merged. reaudit: a fresh checker, task still waiting. refute: it and its dependents fail. Ask accept or refute only on evidence you read; prefer reaudit when the checker never answered."}` +
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
	Continue bool            `json:"continue"`
	Resolve  string          `json:"resolve"`
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
//   - id and say: the person's door into a running node ([Agent.SteerTask]),
//     opened for the model. It is the same door and the same law: talk to the
//     worker, never a new target.
//   - id and continue: the same node again ([Agent.ContinueTask]), after it
//     failed or finished. Same brief, same working copy, last report as this
//     round's finding. A new propose_task is the wrong door.
func (a *Agent) tasksTool() bare.Tool {
	return bare.Tool{
		Name:        "tasks",
		Description: tasksDescription,
		Schema:      json.RawMessage(tasksSchemaJSON),
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
				return markTaskLook(ctx, a.taskSearchText(parsed.Query, parsed.Limit, scope)), false, nil
			}
			// THE SAME ANSWER TWICE IN ONE TURN SAYS SO (tasklook.go). It is the
			// half of the polling fix that reaches a model already mid-poll: the
			// receipt and the description say the report comes to it, and this
			// says the same thing again on the answer it is staring at. A refusal
			// is left alone — it is not a look at anything.
			answer, failed, err := a.oneTask(token, parsed)
			if failed || err != nil {
				return answer, failed, err
			}
			return markTaskLook(ctx, answer), false, nil
		},
	}
}

// taskSearchText is the no-id answer: this project's own rows, and under them
// the work every OTHER aforge window on this project has out right now.
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
	out := taskRowsTextLimit(a.taskRows(), query, limit)
	if !a.tellsElsewhere() {
		return out
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
		return out
	}
	if section := taskEverywhereText(a.OtherProjects(now), query, now); section != "" {
		out += "\n" + section
	}
	return out
}

// taskElsewhereText draws the other windows' running work in this tool's own row
// grammar, matched against the same query the search used.
//
//	running in other aforge windows on this project:
//	another window · Sweep the call sites · running · running for 4m 12s
//	  in the window called "docs pass"
//	  files so far: internal/session/agent.go, internal/session/task.go
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
	over := 0
	if len(matched) > taskSearchLimit {
		over = len(matched) - taskSearchLimit
		matched = matched[:taskSearchLimit]
	}
	var out strings.Builder
	out.WriteString("running in other aforge windows on this project:\n")
	for _, at := range matched {
		taskAwayRow(&out, "", "another window", at, now)
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

// taskEverywhereText draws what is running in every OTHER project on this
// machine, grouped by project, under the same query the search used.
//
//	running in other projects on this machine:
//	wisp · /Users/ada/code/wisp
//	  another window · Port the parser · running · running for 2m 3s
//	    in the window called "parser work"
//	  open here · Rewrite the docs · queued
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
	var kept []OtherProject
	total := 0
	for _, group := range groups {
		var rows []OtherProjectTask
		for _, at := range group.Tasks {
			// The project's own name is matched as well as the task's, because
			// "what is wisp doing" is the same question asked about the place
			// rather than about the work.
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
		kept = append(kept, group)
		total += len(rows)
	}
	if len(kept) == 0 {
		return ""
	}
	over := 0
	if total > taskSearchLimit {
		over = total - taskSearchLimit
		budget := taskSearchLimit
		var capped []OtherProject
		for _, group := range kept {
			if budget <= 0 {
				break
			}
			if len(group.Tasks) > budget {
				group.Tasks = group.Tasks[:budget]
			}
			budget -= len(group.Tasks)
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
		for _, at := range group.Tasks {
			lead := "another window"
			if at.Mine {
				lead = "open here"
				mine = true
			}
			taskAwayRow(&out, "  ", lead, at.ElsewhereTask, now)
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
func (a *Agent) oneTask(token string, parsed tasksArguments) (string, bool, error) {
	rows := a.taskRows()
	entry, found := LookupTask(rows, token)
	if !found {
		// CONTINUE ON A MISS IS NOT "NO TASK". The person named a number and
		// asked to re-arm it; answering as a search miss lets the model
		// narrate the work as if it had resumed (R1). The honest fact is
		// that this session has no graph for that id.
		if parsed.Continue {
			return continueNoGraph(token, TaskIndexEntry{}), true, nil
		}
		if a.config.taskID != 0 {
			// Scoped, so the miss is a different fact: the id may well name real
			// work, and what it does not name is anything this node handed out.
			return fmt.Sprintf("No task %q among the pieces you handed out. Call tasks with no arguments to see them; work you did not ask for is not yours to read from here.", token), true, nil
		}
		return fmt.Sprintf("No task %q in this project. Call tasks with no arguments to see the most recent ones.", token), true, nil
	}
	id, here := a.thisSessionTask(entry)
	if resolution := strings.TrimSpace(parsed.Resolve); resolution != "" {
		return a.resolveOneTask(entry, id, here, TaskResolution(strings.ToLower(resolution)), parsed.Say)
	}
	if parsed.Continue {
		return a.continueOneTask(entry, id, here, parsed.Say)
	}
	if say := strings.TrimSpace(parsed.Say); say != "" {
		if !here {
			return fmt.Sprintf("Task %s ran in an earlier conversation, so there is nobody left to say it to. Propose the work again if it needs doing differently.", entry.ID), true, nil
		}
		waiting, err := a.SteerTask(id, say)
		if err != nil {
			return capitalized(err.Error()) + ".", true, nil
		}
		// AND WHICH KIND OF WAIT IT LANDED IN. A task that has handed its own
		// pieces out is parked on their reports and has no step coming
		// (task_run.go's [TaskGraph.park]), so the line wakes it instead of
		// riding a turn already running — which is the difference between an
		// answer now and an answer the model would otherwise expect at the next
		// step of a task that is not taking one.
		arrival := "It arrives in its loop as the person's own words."
		if waiting {
			arrival = "It was waiting on the pieces it handed out; your line wakes it, and arrives as the person's own words."
		}
		return fmt.Sprintf("said to task %s: %s\n%s Its brief and its acceptance are unchanged — they were frozen when it started.", entry.ID, say, arrival), false, nil
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
	if err := a.ResolveUnverified(id, resolution, why); err != nil {
		return capitalized(err.Error()) + ".", true, nil
	}
	switch resolution {
	case TaskAccept:
		return fmt.Sprintf("accepted task %s as done on your reading of it, with nobody else's check behind it. Its branch has come home and anything waiting on it can start.", entry.ID), false, nil
	case TaskRefute:
		return fmt.Sprintf("refuted task %s. It is failed, its branch is kept, and anything waiting on it fails with it.", entry.ID), false, nil
	default:
		return fmt.Sprintf("a fresh checker is looking at task %s again. It stays waiting until that answer lands, and you will hear the outcome the way every other task lands.", entry.ID), false, nil
	}
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

// thisSessionTask resolves a row to a node of THIS session's graph. A row from
// an earlier conversation, or one whose id this graph never held, is not one:
// ids restart with every conversation (see [TaskIndexEntry.ID]), so the pair is
// what identifies a node and matching on the number alone would read a live
// task 7 as last month's task 7.
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

func taskNodeCount(count int) string {
	if count == 1 {
		return "1 node"
	}
	return strconv.Itoa(count) + " nodes"
}

func taskChildRowText(entry TaskIndexEntry, withURI bool) string {
	parts := []string{entry.Title, entry.Status}
	if word := taskWhenWord(entry); word != "" {
		parts = append(parts, word)
	}
	out := "  " + strings.Join(parts, " · ") + "\n"
	if withURI {
		var where []string
		if entry.ArtifactURI != "" {
			where = append(where, "artifact "+entry.ArtifactURI)
		}
		if entry.TranscriptURI != "" {
			where = append(where, "transcript "+entry.TranscriptURI)
		}
		if len(where) > 0 {
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
	parts := []string{entry.ID, entry.Name, entry.Status}
	if word := taskWhenWord(entry); word != "" {
		parts = append(parts, word)
	}
	if entry.FilesChanged > 0 {
		parts = append(parts, taskFilesWord(entry.FilesChanged))
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
	var where []string
	if entry.ArtifactURI != "" {
		where = append(where, "artifact "+entry.ArtifactURI)
	}
	if entry.TranscriptURI != "" {
		where = append(where, "transcript "+entry.TranscriptURI)
	}
	if len(where) > 0 {
		out += "\n  " + strings.Join(where, " · ")
	}
	return out + "\n"
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

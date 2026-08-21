package session

// THE TWO OR THREE WORDS A PIECE OF WORK IS CALLED.
//
// A node's title is what every surface that draws work draws: the rail's column,
// the home card's work band, the tasks list on a phone, the header of the card
// that lands. The rail is twenty-four columns wide, so what it actually shows is
// the first three words of that title ([taskTitleOf], internal/tui3) — and until
// this file existed, four of the five doors into the graph put a RAW SENTENCE
// there for those three words to be cut out of:
//
//	/task with no shaper   the person's own first eight words   "read /Users…"
//	the route judge        the judge's goal, first line          "Look into why the…"
//	an adaptive run's root the goal, first line                  "/var/folders/j7/…"
//	a resumed node         whatever the checkpoint kept of those
//
// Three words off the front of a sentence is not a name. It is whatever the
// sentence happened to open with — how somebody cleared their throat, or the
// path they pasted — so a column of them names every piece of work after its
// first noise and a person tracking one of them has to read every row.
//
// So a node is NAMED, by one small call, and the design is four properties:
//
//   - ONE CALL, ONCE, PER NODE, AND ONLY WHERE NOTHING NAMED IT. A title a model
//     WROTE as a name is left exactly as it is ([taskSpec.named]): the model that
//     groomed a proposal and called it "Fix the nil-map crash" has already done
//     this work, and the shaper's own name for a `/task` (task_shape.go) is the
//     same answer arriving on a call somebody was already paying for. A second
//     call to rename either of those would be the harness disagreeing with
//     itself and billing for it. A title that is already short and has no path
//     in it is left alone too ([taskNameNeeded]), which is what stops a door
//     that names its work well from paying for a call that changes nothing.
//
//   - THE CHEAP MODEL. It resolves through internal/roles as RoleTaskName, on
//     the low tier, beside the session's own namer: naming in a few words is the
//     archetypal cheap call, and it is one of the calls that must NOT think —
//     the resolved level is deliberately not put on the request.
//
//   - IT NEVER BLOCKS THE WORK. The node is admitted, checkpointed and on the
//     frontier before this call is made; it runs on a goroutine of its own with
//     its own deadline, and the surfaces draw the fallback they draw today until
//     the answer lands. A name that never arrives costs a good name and nothing
//     else — there is no state for "a small thing did not work" and inventing
//     one would report a fault about work nobody asked for.
//
//   - THE NAME IS THE TITLE, NOT A SECOND FIELD BESIDE IT. It is written into
//     the node's spec, which means the checkpoint keeps it (task_store.go), a
//     resume reads it back, the project index rows are cut from it, presence
//     publishes it, and every surface that already draws a title draws the name
//     with no change of its own. One update is published so a row that is
//     already on screen learns it (internal/tui3's taskRenames handles exactly
//     this: a row may be published before its name is known and again after).
//
// WHAT IS DELIBERATELY NOT NAMED. A sub-harness design node (TaskKindHarness)
// keeps the title harness_task.go gives it, because its row is read as a design
// and not as a task. And an adaptive run's INNER nodes are not graph nodes at
// all — orchestrate.go publishes those rows itself — so they keep the planner's
// own words for them.

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The namer is a ROLE, registered from the file that makes the call, as
// internal/roles asks. LOW, for the session title's reason rather than the
// shaper's: a wrong name costs a glance at a column and is not a decision
// anything downstream is made from. A person who disagrees pins it
// (`roles.taskname: <model>`).
func init() { roles.Register(roles.RoleTaskName, roles.TierLow) }

// TaskNameWords is how long a piece of work's name is allowed to be.
//
// THREE IS THE LENGTH A PERSON READS AS A LABEL rather than as a sentence, and
// it is a cap and not a target — a two-word name is left at two. It is exported
// because the surface that draws the name cuts to the same figure, and a namer
// asked for more words than the column can show would be paying for words that
// are thrown away on the way to the screen (internal/tui3's taskTitleWords).
const TaskNameWords = 3

// taskNamePrompt is the whole instruction, and the shape of the answer IS the
// requirement: a label for a narrow column, in the lowercase every other label
// on this surface is drawn in.
const taskNamePrompt = "Name this piece of work in two or three words — a label for a narrow column, not a sentence. Lowercase, no quotes, no full stop, no file paths, no ids. Answer with the name and nothing else."

const (
	// taskNameSummaryClip and taskNameBriefClip bound what the namer is shown.
	// The gloss says what the work is in a line and the brief says it properly;
	// past the first page of the brief everything is detail, and sending a
	// six-thousand-character contract to produce three words would pay for a
	// whole context per node.
	taskNameSummaryClip = 400
	taskNameBriefClip   = 1500

	// taskNameTokens is the ceiling on the answer. Three words is a handful of
	// tokens; this is that with room for a model that says "Title: …" first,
	// which [cleanTitle] strips.
	taskNameTokens = 32

	// taskNameTemp is zero because the same work should be called the same thing
	// twice. A name that changed between a resume and the row above it would be
	// two pieces of work as far as anybody reading the column is concerned.
	taskNameTemp = 0

	// taskNameWindow is how long the call is given. Nobody is waiting for it —
	// the node is already running — so this is not a person's patience but a
	// bound on a goroutine holding a provider slot for work that has stopped
	// mattering: the row has been drawn under its fallback for twenty seconds by
	// then and a name arriving after that is a column changing under somebody's
	// eyes for no reason they can see.
	taskNameWindow = 20 * time.Second
)

// nameNode gives one freshly admitted node a name, if it needs one.
//
// It is called from [TaskGraph.admit] — the ONE door every task in this package
// goes through, whoever opened it — so a new way of starting work inherits the
// name without knowing this file exists.
func (g *TaskGraph) nameNode(node *TaskNode) {
	if g == nil || node == nil {
		return
	}
	g.mu.Lock()
	home, spec, kind := g.home, node.spec, node.kind
	g.mu.Unlock()
	// No conversation behind the graph is every scripted graph in the tests and
	// every graph a headless caller built: there is no client to ask and nothing
	// to bill it to. A DESIGN KEEPS ITS OWN TITLE (see the file comment).
	if home == nil || kind == TaskKindHarness || spec.named || !taskNameNeeded(spec.title) {
		return
	}
	subject := taskNameSubject(spec)
	if subject == "" {
		return
	}
	// IT DOES NOT RIDE THE TURN'S CONTEXT. The turn that admitted this node ends
	// in a moment and the node outlives it by minutes; a namer cancelled with the
	// turn would only ever land for work admitted at the very end of one.
	go func() {
		if name := home.taskName(context.Background(), subject); name != "" {
			g.rename(node, name)
		}
	}()
}

// taskNameNeeded reports whether a title still wants a name made for it.
//
// A TITLE THAT IS ALREADY A NAME IS LEFT ALONE, which is what keeps this from
// being a call on every task: short, and with no path in it, is the whole test.
// A path fails it however short it is, because the one thing a name must not be
// is the machine's own filing.
func taskNameNeeded(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return true
	}
	if strings.ContainsAny(title, "/\\") {
		return true
	}
	return len(strings.Fields(title)) > TaskNameWords
}

// taskNameSubject is what the namer reads: the gloss, then the front of the
// brief. Both, because they answer different halves of "what is this" — the
// gloss says what somebody would call it and the brief says what it actually
// involves — and a node admitted with only one of them still has something to
// be named from.
func taskNameSubject(spec taskSpec) string {
	parts := make([]string, 0, 2)
	if summary := strings.TrimSpace(spec.summary); summary != "" {
		parts = append(parts, clip(summary, taskNameSummaryClip))
	}
	if brief := strings.TrimSpace(spec.brief); brief != "" {
		parts = append(parts, clip(brief, taskNameBriefClip))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// taskName asks the cheap model for the name. Every failure answers "", and the
// caller's only response to that is to leave the title where it was.
func (a *Agent) taskName(ctx context.Context, subject string) string {
	a.mu.Lock()
	call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), roles.RoleTaskName, a.model)
	client, closed := a.client, a.closed
	a.mu.Unlock()
	if closed || err != nil || client == nil || strings.TrimSpace(call.Model) == "" {
		return ""
	}
	// IT CARRIES ITS OWN DEADLINE for the shaper's reason: the provider's client
	// is built with no timeout, so a stalled namer would be a goroutine and a
	// provider slot held for the life of the session.
	ctx, cancel := context.WithTimeout(ctx, taskNameWindow)
	defer cancel()

	// NO EFFORT IS PUT ON THE REQUEST, and that is the reflex law rather than an
	// omission: the calls that are told not to think are the ones that sort and
	// name in a few words, and this is one of them. WithoutStream because nobody
	// asked for this call and left on a stream it would type into the room.
	response, callErr := client.CompleteWithMessages(provider.WithoutStream(ctx),
		[]ai.Message{textMessage("system", taskNamePrompt), textMessage("user", subject)},
		ai.WithModel(call.Model), ai.WithTemperature(taskNameTemp), ai.WithMaxTokens(taskNameTokens))
	if callErr != nil || response == nil {
		return ""
	}
	// The person pays for it out of the same pocket the session's own title, the
	// guardian and the shaper come out of, and no turn asked for it.
	a.addAuxiliaryUsage(response, call.Model, 1)
	return cleanTaskName(response.Text())
}

// cleanTaskName reads the answer back through the same repair the session's own
// namer uses ([cleanTitle], title.go) — a model asked for a short lowercase name
// answers "Title: …", or quotes it, or welds it into a slug, at the same rates
// whichever prompt asked — and then holds it to the cap.
//
// AN ANSWER THAT IS STILL NOT A NAME IS NO ANSWER. The test is the same one that
// decided to make the call ([taskNameNeeded]): a namer that echoed a path has
// handed back exactly the thing the call was made to get rid of, and taking it
// would be paying to make the row no better.
func cleanTaskName(raw string) string {
	name := firstWordsOf(cleanTitle(raw), TaskNameWords)
	if name == "" || taskNameNeeded(name) {
		return ""
	}
	return name
}

// firstWordsOf cuts a phrase to its first n words and drops the punctuation the
// sentence it came out of ended on.
func firstWordsOf(text string, n int) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.TrimRight(strings.Join(fields, " "), ".,:;")
}

// rename writes a node's new name into its spec, keeps it, and tells the world.
//
// THE ORDER IS THE POINT. The write is under the graph's lock, because the spec
// is read under it everywhere else ([TaskNode.notice], [TaskNode.recordLocked]);
// the checkpoint and the update are outside it, because both take other locks
// and the graph is a package with one lock order.
//
// IT PUBLISHES THE UPDATE ITSELF rather than through [TaskGraph.announce], and
// the difference is what announce does with a SETTLED node: it writes the
// project's index row and hands the model a note saying the work has landed.
// This is a node being renamed, which is neither of those, and a node that
// finished before its name arrived would otherwise land twice.
func (g *TaskGraph) rename(node *TaskNode, name string) {
	g.mu.Lock()
	if node.spec.title == name {
		g.mu.Unlock()
		return
	}
	node.spec.title = name
	home := g.home
	g.mu.Unlock()

	g.checkpoint()
	if home != nil {
		home.emitTaskUpdate(node.notice())
	}
}

// ── an adaptive run's own row ───────────────────────────────────────────────
//
// A run is not a node in this package's graph (orchestrate.go says why), so it
// does not come through [TaskGraph.admit] and gets its name here instead. It is
// the same call, the same cap and the same silence on failure; what differs is
// where the answer is written, because a run's row is published by the family
// rather than read off a spec.

// nameRun gives the run's own row a name, if its goal is a sentence rather than
// one. The goal itself is what the namer reads: a run has no gloss and no brief
// of its own, and the goal is what every one of its nodes is cut out of.
func (f *orchestrateFamily) nameRun(goal string) {
	if f == nil || f.agent == nil {
		return
	}
	f.mu.Lock()
	current := f.title
	f.mu.Unlock()
	if !taskNameNeeded(current) {
		return
	}
	subject := clip(strings.TrimSpace(goal), taskNameBriefClip)
	if subject == "" {
		return
	}
	agent := f.agent
	go func() {
		if name := agent.taskName(context.Background(), subject); name != "" {
			f.rename(name)
		}
	}()
}

// rename writes the run's new name and republishes its row under it.
//
// A RUN THAT HAS ALREADY FINISHED IS NOT REPUBLISHED. Its last row was its final
// one and a surface reading a second "running" after it would draw a finished
// run as live again. The name is still written, because the family outlives the
// notice and anything that reads the title afterwards should read the good one.
func (f *orchestrateFamily) rename(name string) {
	f.mu.Lock()
	republish := f.title != name && !f.settled
	f.title = name
	root, run, model := f.root, f.run, f.model
	f.mu.Unlock()
	if !republish {
		return
	}
	f.agent.emitTaskUpdate(TaskNotice{
		ID: root, Run: run, Title: name, State: TaskRunning, Model: model,
	})
}

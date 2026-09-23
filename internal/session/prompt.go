package session

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filememo"
)

// systemPromptSource is omp's normal-chat system prompt, adapted (Decision 2).
// It is embedded rather than read at runtime so the binary carries its own
// prompt: a session must open the same way on a machine that has no source
// tree. [systemPrompt] is this with the shared discipline substituted in.
//
//go:embed prompts/system.md
var systemPromptSource string

// disciplinePrompt is the working discipline itself — how to spend the time —
// and it lives in a file of its own because TWO SURFACES ARE TAUGHT IT AND ONE
// WORDING IS ALL THERE MAY BE.
//
// THE SURFACE THAT PICKS THE APPROACH CARRIES THE DISCIPLINE FOR PICKING IT.
// Measured over twelve unattended runs of the same brief, the approach — and
// with it the whole outcome — was settled in the first couple of minutes of the
// CONVERSATION, before any task existed: the runs whose chat spent one step
// asking whether the thing already existed reached a real result three times out
// of three, and the runs whose chat set about making it by hand reached one none
// of five times in four hours. These three principles were on the worker's page
// alone, so the surface that was actually deciding never read them.
//
// It is SUBSTITUTED and not appended, at the point in each page where that page
// teaches working discipline, and it is substituted ONCE: prompts/system.md is
// read by every surface this package renders — the conversation and every worker,
// the floor of the tree included — so a second copy in prompts/worker.md would
// be a paragraph every worker paid for twice and the law stated in two places that
// can drift apart.
//
//go:embed prompts/discipline.md
var disciplinePrompt string

// systemPrompt is [systemPromptSource] with [disciplineToken] replaced by the
// one wording of [disciplinePrompt]. It is assembled at init rather than at
// render because neither half depends on the config, the clock or the
// workspace.
//
// IT IS NOT YET WHAT THE MODEL READS. It still carries [beltFactsToken], and
// what stands in for that is the one part of the page that DOES depend on the
// config: the session facts that name a tool, composed for this agent's belt
// (beltfacts.go). [promptWithBeltFacts] is the finished page, and it is what
// the fixed-prefix budget weighs (prefixbudget_test.go).
var systemPrompt = strings.Replace(systemPromptSource, disciplineToken,
	strings.TrimRight(disciplinePrompt, "\n"), 1)

// workerPrompt is what a TASK NODE is told on top of it: that nobody is there,
// that the outcome is its own, what a direction arriving mid-work is, and what
// its report and its copy of the material are for.
//
// It is appended and not substituted. A node is the same worker doing the same
// job somewhere quieter (task_run.go), so it reads the same house rules about
// deliverables, grounding and background work; what it needs extra is the part
// no conversation has, which is this.
//
// IT IS THE ROLE AND NOT THE ABILITIES, which is why it was split. Every
// worker is one of these — the floor of the tree included — and the pages below
// are the verbs only some of them carry. A worker that was told nothing about
// its own role because it could not fan out was left reading the CONVERSATION's
// page, which opens by telling it there is a person here to talk to.
//
//go:embed prompts/worker.md
var workerPrompt string

// revisePrompt is the one kind of direction that is not a fact or a question:
// the person moving what this work is judged by. It is its own page on the same
// law the two below are — `revise_assignment` is absent from a worker handed no
// graph (assignment_tool.go), and a page teaching a verb that is not on the belt
// is the prompt lying.
//
//go:embed prompts/revise.md
var revisePrompt string

// fanoutPrompt is how to decide whether a step of the brief is one this worker
// does or one it hands further out (task.go's fan-out law), and how to wait for
// what it handed out. It names `propose_task` and `tasks`, so it renders on the
// predicate that puts them on the belt and nowhere else.
//
//go:embed prompts/fanout.md
var fanoutPrompt string

// quickPrompt is what a QUICK task's worker is told about being one: that it
// works in the caller's own folder rather than a copy, that it ticks its list
// as it goes, that nothing is going to check it, and that its last message is
// the answer (task_quick.go).
//
// It is its own page on [revisePrompt]'s law rather than a paragraph inside
// prompts/worker.md: it names `items`, which is absent from every belt but a
// quick worker's, and a page teaching a verb that is not on the belt is the
// prompt lying.
//
// AND IT OPENS BY SAYING THE PAGE ABOVE IT IS NOT ABOUT IT, which is
// [handToolTail]'s shape and is here for [handToolTail]'s reason. A quick worker still
// reads [workerPrompt] — it is a task, it owns its outcome, directions still
// reach it, and there is nobody to ask — but three of that page's sentences are
// plainly false of it: the acceptance it was handed, the task folder, and the
// branch its work comes home on. A page that contradicted them silently would
// leave the model holding two accounts of where it is working, and it would act
// on whichever it read last.
//
// AND IT IS THE ONLY PLACE THESE LAWS ARE WRITTEN. The node's opening message
// carries the job and the record it came out of and no rules at all
// ([quickBrief] says why), so there is one page saying what a quick worker is
// and it cannot disagree with a second copy of itself.
//
//go:embed prompts/quick.md
var quickPrompt string

// landingAnswerPrompt is the short role page for the one answer an owed root landing opens.
//
//go:embed prompts/landing-answer.md
var landingAnswerPrompt string

// shapePrompt is what the BRIEF-SHAPER is told (task_shape.go): how to reason
// its way from the words a person typed after /task to the brief a worker with
// nobody to ask is actually given.
//
// It is embedded beside the other two, and for the same reason: a prompt read
// off disk is a prompt a shipped binary does not have. It is deliberately a
// META-prompt and names no domain — there is no list of rules for prose, for
// code, for research — because the requests it will be handed are every kind of
// work there is, and a list would be a list that is wrong for whatever the next
// person types.
//
//go:embed prompts/shape.md
var shapePrompt string

// dividePrompt is the extra page a worker gets when THIS piece of work was
// armed to discover that it is wide (task_divide.go). It is separate from
// the pages above rather than a paragraph inside one for the reason the belt is
// conditional: the verb it describes is absent from most workers, and a page
// telling a model about a tool it does not have is the prompt lying — the
// defect CLAUDE.md records `note`/`forget` having caused.
//
//go:embed prompts/divide.md
var dividePrompt string

// bashworkerPrompt is the branch belt's doctrine page (bashbelt.go): how to
// work when the six file tools have come off and the one hand is the shell.
//
// IT REPLACES THE FILE-TOOL GUIDANCE FOR ITS WORKERS rather than sitting beside
// it, and that is a law and not a preference: a page naming a tool the belt
// lacks is the prompt lying, and a second convention beside an existing one is
// prohibited — so the composition below swaps this page in WHERE the embedded
// page teaches the file tools, and rewrites the few sentences outside that
// section that name one of them. A bash worker reading its page reads one
// account of how to read, write, edit and search, not two.
//
//go:embed prompts/bashworker.md
var bashworkerPrompt string

// bashPolicyPrompt is the loop policy a bash-belt worker opens on: the FRAME →
// PLAN → DISPATCH → WAIT → INTEGRATE shape it works by, carried ahead of every
// other page so it is the thing the worker reads first and the thing its reading
// is measured against. It is a whole page rather than a paragraph inside one
// because the composition it leads REPLACES the chat colleague page for this
// belt (bashtask.md's own words are the authority; this is only where it rides).
//
// THE ONLY TRANSLATIONS ARE THE THREE VERBS THE LOOP NAMES, and they are named
// here rather than in the page so a drifted source is a red test and never a
// stale sentence: the act is the belt's own one bash call, the finish is the
// plan CLI's own `plandb done --agent`, and the wait is the plan CLI's own
// `plandb wait --agent` — the task parks, the claim is released, and the
// runtime runs it again when a dependency or a child moves.
//
//go:embed prompts/bashtask.md
var bashPolicyPrompt string

// fanLimitToken is the one thing the page above cannot spell for itself. THE
// NUMBER A MODEL REASONS WITH MUST BE THE NUMBER THE CODE ENFORCES, and a page
// that typed it would be the second place it lives (task.go's schema states the
// law and the drift it cost). So the page names the token and this substitutes
// the constant.
const fanLimitToken = "FAN_LIMIT"

// pieceDepthToken is the other thing the fan-out page cannot spell for itself:
// whether the pieces this worker hands out may split in turn. That depends on
// how deep the worker stands, so no one sentence is true on every page that
// carries it, and the clause is chosen where the depth is known.
const pieceDepthToken = "PIECE_DEPTH"

// fanoutPage is the fan-out page as one worker is handed it, with both of the
// numbers it reasons from read off the code that enforces them. A worker whose
// pieces will themselves be given the verb is told so, because a part it can
// hand out whole is a part it plans differently from one it must keep small
// enough for a single pair of hands; a worker whose pieces stand on the floor is
// told they do not have the tool. The page is composed once, at construction,
// from the worker's own depth, so it is as fixed as every other line of
// message[0].
func fanoutPage(config Config) string {
	pieces := "a piece you hand out cannot hand out more — it does not have the tool"
	if fansOutAt(config.taskDepth + 1) {
		pieces = "a piece you hand out may split its own share under the same cap"
	}
	return strings.NewReplacer(
		fanLimitToken, strconv.Itoa(taskFanLimit),
		pieceDepthToken, pieces,
	).Replace(strings.TrimRight(fanoutPrompt, "\n"))
}

// disciplineToken is where prompts/system.md says the working discipline goes.
// The page names the place and [disciplinePrompt] holds the words, for the same
// reason [fanLimitToken] exists: the second place a thing is written is the
// place it drifts.
const disciplineToken = "WORKING_DISCIPLINE"

// agentsFileLimit bounds how much of a project's AGENTS.md rides in the system
// prompt on a FULL prefix. 8KiB is a page of house rules; a file larger than
// that is documentation, and paying for it on every request of every turn is a
// cost the person never asked for.
//
// A lean prefix bounds it at [leanInstructionLimit] instead, and reads only the
// first instruction file it finds — [Config.instructionLimit] and
// [Config.onlyOneInstructionFile] are the one door into both numbers
// (promptprofile.go), so this constant is never read directly by the renderer.
const agentsFileLimit = 8 << 10

// agentsFileName is the project instruction file, discovered at the workspace
// root exactly as omp discovers it.
const agentsFileName = "AGENTS.md"

const claudeFileName = "CLAUDE.md"

// clockRefresh is how old the rendered prompt may get before a turn re-renders
// it to move the `Now` line forward ([Agent.refreshClockLocked]).
//
// THE THRESHOLD IS THE PROMPT CACHE'S OWN LIFETIME, WHICH IS WHY THE RE-RENDER
// IS FREE. A cached prefix is what a stable message[0] buys, and every provider
// this build talks to expires an untouched one in MINUTES — Anthropic's default
// cache entry lives five minutes from its last read, and the automatic caches
// the others run are of the same order. Ten minutes is comfortably past all of
// them: a conversation that has been quiet that long was going to pay for a
// cold prefix on its next turn whatever this line said, so moving the clock
// costs nothing that was not already spent. Inside the threshold the prompt is
// BYTE-IDENTICAL and the cache is hit exactly as before.
//
// It is not a live clock either way, and the prompt says so: the minute a turn
// opens with is the minute it reasons with, and anything that must be resolved
// against the real clock goes through `stand`'s own `when.in`
// (tools_standing.go).
const clockRefresh = 10 * time.Minute

// isWorker says whether this agent IS a task node — the thing prompts/worker.md
// is written to, at any depth of the tree.
//
// IT IS THE NODE AND NOT [Config.InTask], which is the posture rather than the
// role: a standing check's probe and a hand both run InTask because there is
// nobody there to ask and neither of them may hand work out (standing_run.go,
// fork.go), and neither has a brief, an acceptance or a branch that comes home.
// A page telling either of them to own an outcome and report on it would be the
// same lie the floor node was being told, pointed the other way. The task id is
// what only [Agent.newTaskAgent] sets, so it is the fact both halves read.
func (c Config) isWorker() bool { return c.InTask && c.taskID != 0 }

// mayRevise says whether `revise_assignment` belongs on this belt, and it is
// the SAME question [Agent.assignmentTools] answers, asked of a config before
// there is an agent: a worker handed no graph — an orchestrate run's node — has
// no assignment road to move, and the page that teaches the verb must come off
// with it.
func (c Config) mayRevise() bool { return c.InTask && c.tasker != nil && c.taskID != 0 }

// renderSystem builds the final system prompt as of right now.
func renderSystem(config Config) string { return renderSystemAt(config, time.Now()) }

// renderSystemAt builds the final system prompt: the embedded prompt plus the
// project footer — the facts that are true of this machine, this workspace and
// this minute, none of which can be embedded.
//
// The moment is a PARAMETER and not a call to the clock inside, because this is
// rendered more than once in a long conversation and a caller that can say when
// is a caller a test can hold still.
func renderSystemAt(config Config, now time.Time) string {
	// THE BASH BELT'S WORKER OPENS ON THE LOOP POLICY. A task on the experiment's
	// belt is handed a page of its own rather than the composed one: the policy
	// leads, the belt's doctrine follows (the shell idioms and the plan CLI),
	// then the few codeaf constraints that still bind a task worker, and the
	// project footer closes it. It is a whole page rather than a swap inside the
	// composed one because the page it replaces teaches a shape — the chat
	// colleague, the batch of calls, the visible plan before every step — that
	// the bash loop does not have, and a page that argues with its own head is
	// worse than a page that says less. Nothing here touches any other shape.
	//
	// A WORKER WITH ONE HAND READS THE PAGE ABOUT THAT HAND. This is the whole of
	// a belt worker's system prompt: the belt's two pages ([bashWorkerPage]) and
	// the project footer, and not one byte of prompts/system.md or
	// prompts/worker.md — the tools those pages name are the belt's own
	// ([renderBeltFacts]) or the shell, or the pages are the wrong shape's.
	if config.mayBashBelt() {
		var out strings.Builder
		out.WriteString(bashWorkerPage())
		out.WriteString(workerFooter(config, now))
		return out.String()
	}
	var out strings.Builder
	// THE PAGE, WITH ITS TOOL-NAMING FACTS COMPOSED FROM THIS BELT'S OWN
	// PREDICATES (beltfacts.go). Everything below conditions a whole page on
	// the shape; this conditions the sentences INSIDE one, which is where five
	// families of tools were being promised to workers that do not carry them.
	page := strings.TrimRight(promptWithBeltFacts(config), "\n")
	// AND THE PROFILE'S OWN CUT, WHICH IS THE ONE DOOR INTO IT. A lean prefix
	// drops the sections [leanPageSections] names, by their `# ` heading, and
	// gains the one line a shelved verb owes (promptprofile.go). A full prefix
	// passes through here byte for byte, which prefixbudget_test.go asserts.
	if config.promptProfile().lean() {
		page = leanPage(page)
		if pointer := config.leanShelfPointer(); pointer != "" {
			page += "\n" + pointer
		}
	}
	out.WriteString(page)

	// EVERY WORKER IS TOLD WHAT IT IS, floor of the tree included. The role page
	// names no conditional verb, so the one predicate under it is whether this
	// agent is a task node at all ([Config.isWorker]) — a standing check and a
	// hand are neither, and each opens on a page of its own.
	if config.isWorker() {
		out.WriteString("\n\n")
		out.WriteString(strings.TrimRight(workerPrompt, "\n"))
	}
	if text := renderBeltFacts(config, revisionFacts, "\n\n"); text != "" {
		out.WriteString("\n\n")
		out.WriteString(text)
	}
	// A node that may hand work out is told how to decide; a node standing on
	// the floor of the tree is not, because it has no propose_task to decide
	// with and a prompt promising one is a prompt that lies (the law is in
	// CLAUDE.md and the belt is built from the same predicate). ON THE
	// EXPERIMENT'S BELT the page is lies twice over — the verbs it teaches are
	// not on the belt, and the bashworker page above already carries the same
	// loop (plan, automatic dispatch, wait, integrate) in the plan's words —
	// so the fan-out page stays off this belt.
	if config.mayFanOut() && !config.mayBashBelt() {
		out.WriteString("\n\n")
		out.WriteString(fanoutPage(config))
	}
	// AND THE PAGE ABOUT DISCOVERING WIDTH, on exactly the predicate the belt
	// is built from, so the prompt and the toolbelt can never disagree about
	// whether this worker may divide (task_divide.go).
	if config.mayDivide() {
		out.WriteString("\n\n")
		out.WriteString(strings.TrimRight(dividePrompt, "\n"))
	}
	// AND THE PAGE ABOUT BEING A QUICK TASK, on exactly the predicate that puts
	// `items` on this belt, for the reason the two pages above are conditional:
	// it names a verb only a quick worker carries (task_quick.go).
	if config.mayTickItems() {
		out.WriteString("\n\n")
		out.WriteString(strings.TrimRight(quickPrompt, "\n"))
	}
	// AND THE SHELF THIS CONVERSATION ALREADY OWNS, when it has one. The skill
	// catalog is dynamic CONTENT rather than a fact about the shape, so it is
	// composed here from the store and not from beltfacts.go, and it renders
	// nothing at all on an empty shelf (skillcatalog.go).
	if catalog := renderSkillCatalog(config); catalog != "" {
		out.WriteString("\n\n")
		out.WriteString(catalog)
	}

	out.WriteString(workerFooter(config, now))
	return out.String()
}

// ── the bash worker's page ──────────────────────────────────────────────────

// bashWorkerPage composes the page a bash-belt worker opens message[0] on: the
// loop policy it works by and the belt's doctrine (the plan CLI and the shell
// idioms), and nothing else. [renderSystemAt] appends the project footer after
// it, so this is everything above the footer.
//
// A WORKER WITH ONE HAND READS THE PAGE ABOUT THAT HAND, AND NOTHING ELSE. The
// belt's own pages are the policy (prompts/bashtask.md) and the doctrine
// (prompts/bashworker.md); that is the whole of it. The chat's account of itself
// (prompts/system.md) describes a pane, slash commands and ten tools this belt
// does not carry, and prompts/worker.md is the TASK worker's page — the report it
// owes, the assignment road, the settings — whose verbs a shell-only worker also
// does not have. A page naming a hand the belt lacks is the prompt lying, and
// every step of every worker pays for the sentences again, so neither page rides
// this one.
//
// AND THE SHELF IS NOT ON THIS PAGE, for the same law read forward. The verb
// that lists and fetches a skill is gated on [Config.mayProposeTask] and on a
// store to read the shelf from (tools_skill.go), and this belt has neither: the
// predicate is false for every bash-belt worker by construction (beltfacts.go),
// and the seat a run builds carries no store at all, so the catalog section,
// the per-message block and the verb are all absent here. Nothing above the
// worker puts a skill in its brief either — the attachment road runs through
// the plan graph's own executor and not through this seat — so a paragraph
// telling this worker to reach the shelf would name a hand it has no way to
// use. A capability that cannot work is absent, not broken.
//
// THE ORDER IS THE POINT. A task on this belt is a planner first — it frames,
// plans, dispatches and integrates — and a page opening on the chat colleague or
// the batch of calls would teach a shape the envelope refuses. So the policy
// leads and the doctrine follows in the policy's own terms.
func bashWorkerPage() string {
	var out strings.Builder
	out.WriteString(strings.TrimRight(bashPolicyPrompt, "\n"))
	out.WriteString("\n\n")
	out.WriteString(strings.TrimRight(bashworkerPrompt, "\n"))
	return out.String()
}

// workerFooter is the closing footer every page ends on: the facts true of this
// machine, this workspace and this minute, and the project's own instruction
// files under this profile's bound. It is one function because two pages render
// it now — the composed page and the bash belt's own — and a footer written
// twice is a footer that will one day disagree with itself.
func workerFooter(config Config, now time.Time) string {
	workspace := config.Workspace
	var out strings.Builder
	out.WriteString("\n\n# Project\n")
	fmt.Fprintf(&out, "- Workstation: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&out, "- Working directory: %s\n", workspace)
	// AND WHETHER THERE IS A PROJECT HERE AT ALL. A conversation opened outside
	// one works in a space of its own, and so does a task cut from it — so a
	// worker that finds the directory holding nothing must be told that this is
	// the ordinary state of it and not a checkout that failed, or it spends its
	// steps hunting for a repository nobody named (task_run.go's
	// standingInOwnSpace decides it; session.go's ownSpace carries it).
	if config.inOwnSpace() {
		out.WriteString("- There is no project here: this is the conversation's own space, and it holds only what this conversation has put there.\n")
	}
	out.WriteString(nowLine(now))

	// THE PROJECT'S OWN RULES, UNDER THIS PROFILE'S BOUND. Both numbers here are
	// the profile's rather than this file's constants (promptprofile.go): how
	// much of one file rides, and whether the second one rides at all. A full
	// prefix reads [agentsFileLimit] and both files, exactly as it always has.
	limit := config.instructionLimit()
	for _, instructionFile := range []string{agentsFileName, claudeFileName} {
		instructions, truncated := readInstructionFileWithin(workspace, instructionFile, limit)
		if instructions == "" {
			continue
		}
		fmt.Fprintf(&out, "\n# %s\n\nThe project's own instructions, from %s at the workspace root. They rank above your defaults and below what the person says now.\n\n",
			instructionFile, instructionFile)
		fence := fenceFor(instructions)
		out.WriteString(fence + "markdown\n")
		out.WriteString(instructions)
		if !strings.HasSuffix(instructions, "\n") {
			out.WriteString("\n")
		}
		out.WriteString(fence + "\n")
		if truncated {
			fmt.Fprintf(&out, "\n(%s is longer than %dKiB; the rest is on disk — read it if you need it.)\n",
				instructionFile, limit>>10)
		}
		// AND ON A LEAN PREFIX THE FIRST FILE FOUND IS THE ONLY ONE. The two
		// names are two spellings of one set of house rules, and the second copy
		// is the first thing a small window gives up
		// ([Config.onlyOneInstructionFile]).
		if config.onlyOneInstructionFile() {
			break
		}
	}
	return out.String()
}

// nowLine is the one thing in the footer that a model used to have to SHELL OUT
// for. Without it the prompt carried a bare date, so every "remind me in two
// minutes" opened with a `bash date +%Y-%m-%dT%H:%M:%S%z` — a tool row the
// person saw and asked about, spending a call and a step to learn something the
// process already knew.
//
// It carries four facts because a reminder needs all four: the local time TO
// THE MINUTE, the numeric offset the model has to write back into an RFC3339
// stamp, the zone by name so "tomorrow 9am" lands in the person's morning, and
// the weekday so "Friday" needs no arithmetic.
//
// THE MINUTE COSTS NOTHING. [renderSystemAt]'s answer is [Agent.system] — the
// stable half of message[0] that a refresh re-renders around (memory.go's
// refreshSystemLocked) — and a finer stamp is not a finer cache key.
//
// IT IS THE TURN'S MINUTE AND STILL NOT A LIVE CLOCK. The prompt is rendered
// when the agent is made and again at the start of any turn that opens more
// than [clockRefresh] after the last render ([Agent.refreshClockLocked]), so a
// conversation left open over lunch does not go on telling the model it is
// still morning — the defect this line exists to prevent was a session whose
// `Now` was two hours stale proposing a reminder for a moment already gone.
// Inside the threshold nothing moves and the prompt is byte-identical.
//
// A stamp that is minutes old is still a stamp, so an ABSOLUTE moment is
// computed from it and a RELATIVE one — "in two minutes" — goes to `stand`'s
// own `when.in`, which resolves against the real clock at the moment of the
// call (tools_standing.go).
func nowLine(now time.Time) string {
	zone := now.Location().String()
	if zone == "" || zone == "Local" {
		// A machine with no zone database, or one whose TZ nobody set, still has
		// an abbreviation the clock itself reports. Naming that is honest; naming
		// "Local" would be telling the model the name of a Go variable.
		zone = now.Format("MST")
	}
	return fmt.Sprintf("- Now: %s (%s, %s)\n", now.Format("2006-01-02 15:04 -07:00"), zone, now.Format("Monday"))
}

// refreshClockLocked moves the prompt's `Now` line forward when it has gone
// stale, and does nothing at all when it has not.
//
// THE MODEL'S CLOCK MUST NOT GO STALE INSIDE ONE SESSION. A conversation opened
// at breakfast and spoken to at lunch used to carry breakfast's minute in its
// instructions, so "remind me in 1 minute" was worked out from a stamp two
// hours behind the wall clock and landed in the PAST. Re-rendering here is the
// fix at the source; tools_standing.go's refusal is the net under it.
//
// AND THE COMMON PATH IS BYTE-IDENTICAL. Inside [clockRefresh] this returns
// without touching a.system, so the cached prefix of a busy conversation is
// never disturbed; past it, the cache had expired anyway (clockRefresh states
// the reasoning). It re-renders the WHOLE footer rather than editing one line,
// because a prompt assembled in two different ways is a prompt that will one
// day disagree with itself.
//
// It is called with a.mu held, from [Agent.startTurnLocked].
func (a *Agent) refreshClockLocked(now time.Time) {
	if now.Sub(a.systemAt) < clockRefresh {
		return
	}
	a.rerenderSystemLocked(now)
}

// rerenderSystemLocked rebuilds this agent's instructions from its config AS IT
// NOW STANDS, and puts them back at the head of the transcript.
//
// IT IS ONE FUNCTION BECAUSE THE CONFIG MOVES UNDER RUNNING AGENTS, in three
// ways and no more: the clock going stale above, a conversation anchoring to a
// project (tools_anchor_workspace.go), and work being armed to divide beside its
// worker (task_divide.go's [Agent.armDivisionBeside]). Each of the three changes
// something [renderSystemAt] reads, and a prompt assembled in two different ways
// is a prompt that will one day disagree with itself — which is the same reason
// the clock re-renders the whole footer rather than editing one line.
//
// A PROMPT THIS AGENT DID NOT WRITE IS LEFT ALONE. [Agent.systemOwn] is false
// where a caller handed one in, and rendering ours over the top of it would be
// this file deciding what another door's worker is told.
//
// The caller holds a.mu.
func (a *Agent) rerenderSystemLocked(now time.Time) {
	if !a.systemOwn {
		return
	}
	a.system = renderSystemAt(a.liveModelConfigLocked(), now)
	a.systemAt = now
	a.refreshSystemLocked()
}

// liveModelConfigLocked is this agent's config with the model it is talking to
// NOW in place of the one it was launched on, which is the config the page is
// rendered from. The two differ after a `/model`: [Agent.setModel] moves
// [Agent.model] and leaves [Config.Model] where the launch put it, and a page
// rendered from the launch model names that model in the one line that is
// supposed to say which model wrote the work — `Assisted-by`
// (beltfacts.go's attribution fact).
//
// The caller holds a.mu.
func (a *Agent) liveModelConfigLocked() Config {
	config := a.config
	if model := strings.TrimSpace(a.model); model != "" {
		config.Model = model
	}
	return config
}

// followModelOnThePageLocked re-renders the page after the model changed, and
// touches nothing when the page does not say which model it is.
//
// THE `Assisted-by` LINE NAMES THE MODEL, AND IT WENT STALE AFTER `/model`. The
// page was rendered once from [Config.Model] and a switch never rendered it
// again, so every commit after one still credited the model the conversation
// was launched on.
//
// WHY THIS DOES NOT COST THE PREFIX CACHE ANYTHING IT WAS STILL GOING TO HAVE.
// A prompt cache belongs to one model: the first request on the model just
// picked is written cold whatever the page says, so re-rendering it on the
// switch buys the right name for nothing. Two things keep it that way:
//
//   - THE CLOCK IS NOT MOVED. The page is rendered at [Agent.systemAt], the
//     moment it was last rendered, so the only bytes that change are the ones
//     that follow the model. Switching back to the model before is then the
//     page that model already has cached, byte for byte, rather than a second
//     cold write for a newer minute.
//   - A PAGE THAT DOES NOT NAME THE MODEL IS LEFT ALONE. With the model's name off
//     the render comes back identical and message[0] is not touched at all.
//
// A prompt this agent did not write is never re-rendered ([Agent.systemOwn]).
//
// The caller holds a.mu.
func (a *Agent) followModelOnThePageLocked() {
	if !a.systemOwn {
		return
	}
	page := renderSystemAt(a.liveModelConfigLocked(), a.systemAt)
	if page == a.system {
		return
	}
	a.system = page
	a.refreshSystemLocked()
}

// readAgentsFile reads at most agentsFileLimit bytes of the workspace's
// AGENTS.md and reports whether it stopped early. A missing or unreadable file
// is not an error: most workspaces do not have one.
func readAgentsFile(workspace string) (content string, truncated bool) {
	return readInstructionFile(workspace, agentsFileName)
}

func readInstructionFile(workspace, name string) (content string, truncated bool) {
	return readInstructionFileWithin(workspace, name, agentsFileLimit)
}

// readInstructionFileWithin is the same read under a bound the caller names. It
// exists because an ATTACHED folder's rules ride under a tighter one than the
// workspace's own, and several of them can ride at once (placescontext.go) — and
// two spellings of "read this file, cut it on a rune boundary, say that it was
// cut" is one of them forgetting the boundary.
// instructionFileMemo is the project's own rules, READ AT MOST ONCE PER CHANGE.
//
// THE DEFECT IT CLOSES: [renderSystemAt] re-reads AGENTS.md and CLAUDE.md
// whenever the prompt's clock has gone stale ([clockRefresh]) — which is exactly
// the turn a person has just come back to their terminal and started, the one
// turn of a long conversation where there is nobody else's latency to hide
// behind. The files it re-reads have not changed since the session opened in the
// overwhelming majority of cases, and a project with several folders attached
// pays the same read once per folder (placescontext.go).
//
// It holds the BYTES rather than the trimmed string because two callers bound
// the same file differently — a lean prefix cuts it shorter than a full one —
// and a memo per (file, bound) would read one file twice to answer two questions
// about the same bytes. What it holds is bounded at [agentsFileLimit]+1, which
// is one byte more than the largest bound anybody asks for and is what makes
// "was it cut" answerable without the rest of the file.
var instructionFileMemo = filememo.New(func(_ string, data []byte, missing bool) ([]byte, error) {
	if missing {
		return nil, nil
	}
	if len(data) > agentsFileLimit+1 {
		data = data[:agentsFileLimit+1]
	}
	return data, nil
})

func readInstructionFileWithin(dir, name string, limit int) (content string, truncated bool) {
	buffer, err := instructionFileMemo.Read(filepath.Join(dir, name))
	if err != nil || len(buffer) == 0 {
		return "", false
	}
	// One byte past the limit tells truncation from an exactly-sized file, and a
	// bound above what the memo holds cannot be honoured — nothing asks for one
	// ([agentsFileLimit] is the ceiling), and if something ever does it reads the
	// file as truncated rather than as complete.
	if limit > agentsFileLimit {
		limit = agentsFileLimit
	}
	if len(buffer) > limit {
		// Back off to a rune boundary. A byte-exact cut can land inside a
		// multi-byte rune, and the U+FFFD that replaces the fragment is a
		// character the person never wrote arriving in the model's house rules.
		cut := limit
		for cut > 0 && !utf8RuneStart(buffer[cut]) {
			cut--
		}
		return strings.TrimRight(string(buffer[:cut]), "\n"), true
	}
	return strings.TrimRight(string(buffer), "\n"), false
}

// fenceFor returns a fence longer than the longest backtick run in the
// content, so a file that itself contains fenced code cannot close the block
// early and spill markdown into the prompt as instructions.
func fenceFor(content string) string {
	longest, run := 0, 0
	for i := 0; i < len(content); i++ {
		if content[i] == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	if longest < 3 {
		longest = 2
	}
	return strings.Repeat("`", longest+1)
}

package session

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
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

// fanLimitToken is the one thing the page above cannot spell for itself. THE
// NUMBER A MODEL REASONS WITH MUST BE THE NUMBER THE CODE ENFORCES, and a page
// that typed it would be the second place it lives (task.go's schema states the
// law and the drift it cost). So the page names the token and this substitutes
// the constant.
const fanLimitToken = "FAN_LIMIT"

// disciplineToken is where prompts/system.md says the working discipline goes.
// The page names the place and [disciplinePrompt] holds the words, for the same
// reason [fanLimitToken] exists: the second place a thing is written is the
// place it drifts.
const disciplineToken = "WORKING_DISCIPLINE"

// agentsFileLimit bounds how much of a project's AGENTS.md rides in the system
// prompt. 8KiB is a page of house rules; a file larger than that is
// documentation, and paying for it on every request of every turn is a cost
// the person never asked for.
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
	workspace := config.Workspace
	var out strings.Builder
	// THE PAGE, WITH ITS TOOL-NAMING FACTS COMPOSED FROM THIS BELT'S OWN
	// PREDICATES (beltfacts.go). Everything below conditions a whole page on
	// the shape; this conditions the sentences INSIDE one, which is where five
	// families of tools were being promised to workers that do not carry them.
	out.WriteString(strings.TrimRight(promptWithBeltFacts(config), "\n"))

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
	// CLAUDE.md and the belt is built from the same predicate).
	if config.mayFanOut() {
		out.WriteString("\n\n")
		out.WriteString(strings.ReplaceAll(strings.TrimRight(fanoutPrompt, "\n"), fanLimitToken, strconv.Itoa(taskFanLimit)))
	}
	// AND THE PAGE ABOUT DISCOVERING WIDTH, on exactly the predicate the belt
	// is built from, so the prompt and the toolbelt can never disagree about
	// whether this worker may divide (task_divide.go).
	if config.mayDivide() {
		out.WriteString("\n\n")
		out.WriteString(strings.TrimRight(dividePrompt, "\n"))
	}

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

	for _, instructionFile := range []string{agentsFileName, claudeFileName} {
		instructions, truncated := readInstructionFile(workspace, instructionFile)
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
				instructionFile, agentsFileLimit>>10)
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
	if !a.systemOwn || now.Sub(a.systemAt) < clockRefresh {
		return
	}
	a.system = renderSystemAt(a.config, now)
	a.systemAt = now
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
func readInstructionFileWithin(dir, name string, limit int) (content string, truncated bool) {
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return "", false
	}
	defer file.Close()
	// One byte past the limit tells truncation from an exactly-sized file.
	buffer, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", false
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

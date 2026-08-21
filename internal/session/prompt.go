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

// systemPrompt is omp's normal-chat system prompt, adapted (Decision 2). It is
// embedded rather than read at runtime so the binary carries its own prompt:
// a session must open the same way on a machine that has no source tree.
//
//go:embed prompts/system.md
var systemPrompt string

// taskPrompt is what a TASK NODE is told on top of it: that nobody is there,
// and how to decide whether a step of its brief is one it does or one it hands
// further out (task.go's fan-out law).
//
// It is appended and not substituted. A node is the same worker doing the same
// job somewhere quieter (task_run.go), so it reads the same house rules about
// deliverables, grounding and background work; what it needs extra is the part
// no conversation has, which is this.
//
//go:embed prompts/task.md
var taskPrompt string

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

// fanLimitToken is the one thing the page above cannot spell for itself. THE
// NUMBER A MODEL REASONS WITH MUST BE THE NUMBER THE CODE ENFORCES, and a page
// that typed it would be the second place it lives (task.go's schema states the
// law and the drift it cost). So the page names the token and this substitutes
// the constant.
const fanLimitToken = "FAN_LIMIT"

// agentsFileLimit bounds how much of a project's AGENTS.md rides in the system
// prompt. 8KiB is a page of house rules; a file larger than that is
// documentation, and paying for it on every request of every turn is a cost
// the person never asked for.
const agentsFileLimit = 8 << 10

// agentsFileName is the project instruction file, discovered at the workspace
// root exactly as omp discovers it.
const agentsFileName = "AGENTS.md"

// renderSystem builds the final system prompt: the embedded prompt plus the
// project footer — the facts that are true of this machine, this workspace and
// today, none of which can be embedded.
func renderSystem(config Config) string {
	workspace := config.Workspace
	var out strings.Builder
	out.WriteString(strings.TrimRight(systemPrompt, "\n"))

	// A node that may hand work out is told how to decide; a node standing on
	// the floor of the tree is not, because it has no propose_task to decide
	// with and a prompt promising one is a prompt that lies (the law is in
	// CLAUDE.md and the belt is built from the same predicate).
	if config.mayFanOut() {
		out.WriteString("\n\n")
		out.WriteString(strings.ReplaceAll(strings.TrimRight(taskPrompt, "\n"), fanLimitToken, strconv.Itoa(taskFanLimit)))
	}

	out.WriteString("\n\n# Project\n")
	fmt.Fprintf(&out, "- Workstation: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&out, "- Working directory: %s\n", workspace)
	out.WriteString(nowLine(time.Now()))

	if instructions, truncated := readAgentsFile(workspace); instructions != "" {
		fmt.Fprintf(&out, "\n# %s\n\nThe project's own instructions, from %s at the workspace root. They rank above your defaults and below what the person says now.\n\n",
			agentsFileName, agentsFileName)
		fence := fenceFor(instructions)
		out.WriteString(fence + "markdown\n")
		out.WriteString(instructions)
		if !strings.HasSuffix(instructions, "\n") {
			out.WriteString("\n")
		}
		out.WriteString(fence + "\n")
		if truncated {
			fmt.Fprintf(&out, "\n(%s is longer than %dKiB; the rest is on disk — read it if you need it.)\n",
				agentsFileName, agentsFileLimit>>10)
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
// THE MINUTE COSTS NOTHING. [renderSystem] is called ONCE, in [newAgent], and
// its answer is [Agent.system] — the stable half of message[0] that a refresh
// re-renders around (memory.go's refreshSystemLocked). A finer stamp is not a
// finer cache key; it is the same one key, minted once per conversation.
//
// WHICH IS ALSO WHY IT IS THE OPENING MINUTE AND NOT A LIVE CLOCK, and the
// prompt says so rather than implying a clock that ticks. Time passes inside a
// long session, so an ABSOLUTE moment is computed from this stamp and a RELATIVE
// one — "in two minutes" — goes to `stand`'s own `when.in`, which resolves
// against the real clock at the moment of the call (tools_standing.go).
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

// readAgentsFile reads at most agentsFileLimit bytes of the workspace's
// AGENTS.md and reports whether it stopped early. A missing or unreadable file
// is not an error: most workspaces do not have one.
func readAgentsFile(workspace string) (content string, truncated bool) {
	file, err := os.Open(filepath.Join(workspace, agentsFileName))
	if err != nil {
		return "", false
	}
	defer file.Close()
	// One byte past the limit tells truncation from an exactly-sized file.
	buffer, err := io.ReadAll(io.LimitReader(file, agentsFileLimit+1))
	if err != nil {
		return "", false
	}
	if len(buffer) > agentsFileLimit {
		// Back off to a rune boundary. A byte-exact cut can land inside a
		// multi-byte rune, and the U+FFFD that replaces the fragment is a
		// character the person never wrote arriving in the model's house rules.
		cut := agentsFileLimit
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

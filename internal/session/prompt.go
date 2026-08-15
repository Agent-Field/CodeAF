package session

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// systemPrompt is omp's normal-chat system prompt, adapted (Decision 2). It is
// embedded rather than read at runtime so the binary carries its own prompt:
// a session must open the same way on a machine that has no source tree.
//
//go:embed prompts/system.md
var systemPrompt string

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
func renderSystem(workspace string) string {
	var out strings.Builder
	out.WriteString(strings.TrimRight(systemPrompt, "\n"))

	out.WriteString("\n\n# Project\n")
	fmt.Fprintf(&out, "- Workstation: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&out, "- Working directory: %s\n", workspace)
	fmt.Fprintf(&out, "- Today: %s\n", time.Now().Format("2006-01-02"))

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

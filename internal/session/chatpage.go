package session

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// ── THE PAGE A MODEL WITH NO TOOLS READS ────────────────────────────────────
//
// A model the catalog says takes no tool calls is sent no tools
// (internal/provider's toolless.go). The working page was still sent with it:
// on 2026-09-28 microsoft/phi-4 carried 23,640 characters of system prompt —
// tool policy, workflow, delegation, the facts about files and jobs — about a
// third of its 16k window, every word of it about hands it did not have. A
// capability that cannot work is absent, and so is the page about it.
//
// So a conversation on such a model reads this page instead: who it is, what it
// cannot do and what to say about that, how to answer, the codeaf messages it
// must not answer as the person's, and the project facts that are true of this
// minute. The belt is empty ([Agent.belt]) and memory is off, as on a lean
// prefix. It lasts while the model does: the conversation's next request on a
// model that can use tools gets the working page back (promptprofile_live.go).

//go:embed prompts/chat.md
var chatPrompt string

// chatPage is the whole system prompt of a tool-less conversation.
//
// THE MESSAGES SECTION IS CUT FROM THE WORKING PAGE rather than written twice,
// so the list of codeaf's own user-role tags cannot drift between the two
// (messagesfromcodeaf_test.go pins it on system.md).
func chatPage(config Config, now time.Time) string {
	var out strings.Builder
	out.WriteString(strings.TrimRight(chatPrompt, "\n"))
	if messages := pageSection(systemPrompt, "Messages from codeaf"); messages != "" {
		out.WriteString("\n\n")
		out.WriteString(messages)
	}
	out.WriteString("\n\n# Project\n")
	fmt.Fprintf(&out, "- Workstation: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&out, "- Working directory: %s\n", config.Workspace)
	out.WriteString(nowLine(now))
	return out.String()
}

// pageSection is one `# ` section of a page, heading included, and "" when the
// page has no such heading.
func pageSection(page, heading string) string {
	var kept []string
	inside := false
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, "# ") {
			if inside {
				break
			}
			inside = strings.TrimSpace(strings.TrimPrefix(line, "# ")) == heading
		}
		if inside {
			kept = append(kept, line)
		}
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}

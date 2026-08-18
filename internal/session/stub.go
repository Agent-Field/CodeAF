package session

// Tool-output stubs: the old, heavy results in the LIVE context are replaced by
// a line saying how big they were and where the bytes are.
//
// The problem is specific and it is not compaction's. A session reads a 200KB
// file on its first turn and re-sends those 200KB on every request of every turn
// afterwards, for hours, to answer questions that have nothing to do with it.
// Compaction eventually summarizes the whole prefix away — lossily, and only
// once the window is nearly full. This is the cheaper move made much earlier:
// the result the model has already used is turned into a pointer to itself.
//
// ── STUB, DON'T DELETE ──
//
// The full bytes are written to a stubs/<hash>.txt of the session's own
// (landing.go) BEFORE the message is replaced, and the stub line names that
// path. A model
// told where the bytes live can read them back with the tool it already has, so
// a stub costs a call when the old output turns out to matter and costs nothing
// the rest of the time. Deleting the text instead would be the one version of
// this that loses work.
//
// ── THE JOURNAL IS NEVER STUBBED ──
//
// Only a.messages — the live context — is rewritten. The session file keeps the
// result it recorded, whole, because the journal is the RECORD: it is what a
// resume replays, what a person reads back tomorrow, and what a surface expands
// a tool row from. A record that quietly shrank the day after it was written
// would be a different kind of file than the one this session promises.
//
// ── AND THE ESTIMATE FOLLOWS FOR FREE ──
//
// messageBytes (loop.go) counts the text that is actually in the transcript, so
// a stubbed result weighs its stub line the moment it is replaced. The
// compaction estimate, the threshold check and the surface's context meter all
// read that same figure — nothing here has to tell them anything, and the
// pressure that would have fired a compaction pass is simply gone.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// stubKeepTurns is how many turns of results stay verbatim. Four is "what
	// the model is working on now": the file it just read, the build it just
	// ran, the error it is chasing, and one more for the thing before that.
	stubKeepTurns = 4

	// stubMinBytes is the floor. Below it a stub line is not much smaller than
	// the result it replaces, and replacing a short result would cost a read to
	// recover something the model could simply have kept.
	stubMinBytes = 1500

	// stubMarker opens every stub line and is how an already-stubbed message is
	// recognized, so a second pass never stubs a stub.
	stubMarker = "[tool"

	// stubOutcomeRunes bounds the one line of the result a stub quotes. Eighty
	// is a terminal's width: enough for a compiler's first error or a shell's
	// exit line, short enough that a stub of a 200KB read is still one line.
	stubOutcomeRunes = 80
)

// stubOldOutputs replaces the heavy tool results of older turns with pointers to
// their own bytes. It is called at the end of a COMPLETED turn, before the
// compaction check, so the check sees the transcript as it will actually be
// sent — AND AGAIN as the first pass of a compaction (loop.go), which is why the
// work itself lives in [Agent.stubOldOutputsLocked].
//
// A turn that was interrupted or that failed is left alone, and nothing is lost
// by that: the pass is idempotent and the next completed turn catches up. The
// reason is the moment rather than the mechanism — an interrupted turn is one
// the person is about to read, rewind or retry, and rewriting their context
// underneath them on the way out is work done at the one moment nobody asked for
// any.
//
// Everything it cannot do, it declines silently and completely: a session with
// no workspace, a directory it cannot write, a result it cannot file. A stub
// whose bytes did not reach disk is never written, because the one thing this
// must never do is turn a result into a path to nothing.
//
// The whole pass runs under a.mu, including the file writes. It is a lock held
// across I/O, which this package otherwise refuses to do — the exception is
// deliberate and bounded: the alternative is to release the lock between reading
// the messages and replacing them, and a compaction pass entering that window
// rebuilds the slice, so the indices would then name different messages. The
// writes are a few hundred kilobytes to a local file at the quietest moment of
// the turn, after the model has answered and before the next question exists.
func (a *Agent) stubOldOutputs() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stubOldOutputsLocked()
}

// stubOldOutputsLocked is the pass itself, and it reports how many results it
// replaced — the number the compaction row says out loud.
//
// THE POINTER IS THE STORE'S FIRST AND THE FILE'S SECOND. A result already
// posted to the store's thread (chatlog.go) needs no second copy on disk, and
// the store id is the better pointer besides: it survives a workspace being
// deleted, which a dropping does not. A session with no store spills to its own
// logs/ exactly as this always did, and a session that can do neither leaves the
// result verbatim — a stub pointing at nothing is the one failure this may not
// have.
func (a *Agent) stubOldOutputsLocked() int {
	workspace := strings.TrimSpace(a.config.Workspace)
	cut := stubCut(a.messages)
	stubbed := 0
	// index 0 is the system message and is not a tool result; starting at 1 says
	// so out loud rather than relying on the role check below.
	for index := 1; index < cut; index++ {
		message := a.messages[index]
		if message.Role != "tool" {
			continue
		}
		text := messageContentText(message)
		if len(text) <= stubMinBytes || strings.HasPrefix(strings.TrimSpace(text), stubMarker) {
			continue
		}
		pointer := a.chatlog.ref(message)
		if pointer == "" {
			if workspace == "" {
				continue
			}
			path, err := writeStub(a.config.Place, workspace, text)
			if err != nil {
				continue
			}
			pointer = path
		}
		// A NEW content slice, never a write into the old one: a request already
		// in flight holds a shallow copy of this message (see [Agent.snapshot]),
		// and mutating the parts underneath it would edit a request the provider
		// is reading.
		a.messages[index] = ai.Message{
			Role:       message.Role,
			ToolCallID: message.ToolCallID,
			Content: []ai.ContentPart{{Type: "text", Text: stubLine(
				toolNameFor(a.messages, index), text, pointer)}},
		}
		stubbed++
	}
	return stubbed
}

// toolNameFor answers which tool produced the result at index, by finding the
// call it answers in the assistant message above it. Empty is "tool": the name
// is a courtesy to a reader, and a result whose call was already folded away is
// still a result worth stubbing.
func toolNameFor(messages []ai.Message, index int) string {
	id := messages[index].ToolCallID
	if id == "" {
		return ""
	}
	for above := index - 1; above > 0; above-- {
		for _, call := range messages[above].ToolCalls {
			if call.ID == id {
				return call.Function.Name
			}
		}
	}
	return ""
}

// stubCut is the index the last [stubKeepTurns] turns start at: everything
// before it is old enough to stub, everything from it on is the recent work.
//
// A turn starts at a user message, so the boundary is the Nth user message from
// the end. Zero — do nothing — is the answer for a conversation that has not had
// that many turns yet.
//
// The count is of USER MESSAGES rather than of turns proper, which means a
// steering line or a nudge note (looped.go) counts as a turn boundary. That errs
// toward keeping MORE verbatim, which is the harmless direction: the worst case
// is a heavy result that stays in context a turn or two longer than it had to.
func stubCut(messages []ai.Message) int {
	turns := 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		turns++
		if turns == stubKeepTurns {
			return index
		}
	}
	return 0
}

// writeStub files one result's bytes and returns the path to name in the stub.
//
// The name is the content's own digest, which makes the write idempotent: the
// same result stubbed twice — a re-read of the same file, a resumed session
// stubbing again — is one file on disk, and a file that is already there is left
// exactly as it is rather than rewritten.
func writeStub(place Place, workspace, text string) (string, error) {
	digest := sha256.Sum256([]byte(text))
	directory := droppingsDir(place, workspace, droppingStubs)
	full := filepath.Join(directory, hex.EncodeToString(digest[:8])+".txt")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	if info, err := os.Stat(full); err == nil && info.Size() == int64(len(text)) {
		return stubPath(workspace, full), nil
	}
	if err := os.WriteFile(full, []byte(text), 0o600); err != nil {
		return "", err
	}
	return stubPath(workspace, full), nil
}

// stubPath is the path the stub line NAMES, and the rule is the one
// [displayMediaPath] follows: a file inside the workspace is named relative to
// it, because that is the string the model's own read tool takes and the string
// the person's shell takes; a file outside it is named absolutely, because a
// relative path out of the workspace is a path nobody can open.
//
// Both cases occur now. A session with no folder still stubs into the
// workspace's dot directory; a session with one stubs into its own logs/, which
// is somewhere else entirely (landing.go).
func stubPath(workspace, full string) string {
	relative, err := filepath.Rel(workspace, full)
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(full)
	}
	return filepath.ToSlash(relative)
}

// stubLine is what the model reads in place of the result: which tool ran, what
// it said in one line, and where the whole of it can be read back.
//
//	[tool: bash · go build ./... — 0 exit · 41208 bytes · full: store:412]
//
// All three parts are mechanical — a name lifted off the call, the result's own
// first line, its own byte count — because THE POINT OF THIS PASS IS THAT NO
// MODEL RUNS IN IT. The byte count is explicit for the reason [capOutput]'s is:
// a model deciding whether to fetch the rest needs to know whether it is missing
// a paragraph or a megabyte.
func stubLine(tool, text, pointer string) string {
	if strings.TrimSpace(tool) == "" {
		tool = "tool"
	}
	return fmt.Sprintf("%s: %s · %s · full: %s]", stubMarker, tool, stubOutcome(text), pointer)
}

// stubOutcome is the one line a result is reduced to: its first non-empty line,
// bounded, and its size.
func stubOutcome(text string) string {
	first := ""
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			first = line
			break
		}
	}
	if runes := []rune(first); len(runes) > stubOutcomeRunes {
		first = strings.TrimSpace(string(runes[:stubOutcomeRunes])) + "…"
	}
	if first == "" {
		return fmt.Sprintf("%d bytes", len(text))
	}
	return fmt.Sprintf("%s · %d bytes", first, len(text))
}

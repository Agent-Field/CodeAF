package session

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// The branch belt's bash result cut, and why it is not pi's.
//
// pi's bash truncates tail-only inside its own accumulator: the head of a big
// output is gone by the time the tool returns, and the model reads only the
// last 50KB of whatever it ran. The experiment's loop (docs/design/
// bash-task-loop/DESIGN.md, Decision 3) adopts miniplan's shape instead —
// head + tail + path — because the two halves of a build log answer different
// questions: the head says what the command started with, the tail says how it
// ended, and neither half alone says both. THE CUT IS THE SAME BOUND THE REST
// OF THE BELT APPLIES — [bare.CapsFor] over this agent's window, split half
// head and half tail by bytes — and nothing here is a second formula for it:
// the pair comes from the same window law every other result is cut with.

// bashTruncationMarker is the line a cut result carries, verbatim miniplan's,
// because its shape is part of what the experiment measures: a model that has
// read ten thousand of them reads this one the same way.
const bashTruncationMarker = "[output truncated; full output: %s]"

// bashSpillName is the file a cut command's whole output is written to, beside
// the node's own journal — never inside the working copy, so a worker's git
// status is never dirtied by its own telemetry.
const bashSpillName = "action-%06d.txt"

// cutBashResult applies the branch cut to one bash result and answers it
// unchanged when it fits. Any result over the byte cap is cut, whatever the
// command's exit said: a timeout's own status line is the tail of the text and
// survives the cut, which is exactly where it belongs.
func (a *Agent) cutBashResult(text string) string {
	caps := a.resultCaps()
	if len(text) <= caps.MaxBytes {
		return text
	}
	head := utf8SafeCut(text, caps.MaxBytes/2)
	tail := utf8SafeTail(text, caps.MaxBytes-caps.MaxBytes/2)
	// AND THE WHOLE OUTPUT IS NOT THROWN AWAY. Where it can be filed it is
	// filed beside this node's journal and the marker names the file; where
	// there is no journal to sit beside, the temp directory takes it, because
	// the one thing the cut must never do is hand the model a path into the
	// working copy it is standing in.
	path, err := a.spillBashOutput(text)
	if err != nil {
		return text[:head] + "\n" + fmt.Sprintf(bashTruncationMarker, "unavailable") + "\n" + tail
	}
	return text[:head] + "\n" + fmt.Sprintf(bashTruncationMarker, path) + "\n" + tail
}

// utf8SafeCut answers the longest prefix of s at or under max bytes that ends
// on a rune boundary, so a cut result never opens with half a character.
func utf8SafeCut(s string, max int) int {
	if max >= len(s) {
		return len(s)
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return max
}

// utf8SafeTail answers the longest suffix of s at or under max bytes that
// begins on a rune boundary.
func utf8SafeTail(s string, max int) string {
	if max >= len(s) {
		return s
	}
	start := len(s) - max
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// spillBashOutput writes the whole output beside this agent's own journal —
// the node is an agent, and its journal is the log its card already points at
// (task_run.go's [taskJournalPath]). The sequence number is minted from the
// directory rather than from a counter, because a worker's life is one
// directory and reading it is the one source of truth for what is already
// there; the exclusive create closes the race the scan leaves open.
func (a *Agent) spillBashOutput(text string) (string, error) {
	dir := a.logDirectory()
	if dir == "" {
		return "", fmt.Errorf("no directory to file the output in")
	}
	next := 1
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			var n int
			if _, err := fmt.Sscanf(entry.Name(), "action-%06d.txt", &n); err == nil && n >= next {
				next = n + 1
			}
		}
	}
	for attempt := 0; attempt < 1000; attempt++ {
		path := fmt.Sprintf(filepath.Join(dir, bashSpillName), next)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, writeErr := file.WriteString(text)
			closeErr := file.Close()
			if writeErr != nil {
				return "", writeErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			return path, nil
		}
		next++
	}
	return "", fmt.Errorf("no free action number in %s", dir)
}

// logDirectory is the directory this agent's own transcript is written to, and
// empty when this agent has none — a test's throwaway agent, a hand minted
// before its journal existed. The caller answers for the empty case.
func (a *Agent) logDirectory() string {
	if a.file == nil {
		return ""
	}
	path := a.file.journalName()
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

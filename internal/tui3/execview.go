package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// withExecRows appends software-derived launch state from the memo. It is a
// layout pass, not a store read: the beat already filled [app.execView]. An
// empty reading adds nothing, which is the emptiness law (never `0 runs`).
// A seam that is present but down, with no last-good works, names the
// absence rather than looking like empty work.
func (a *app) withExecRows(out []row, width int) []row {
	extra := a.execTranscriptRows(width)
	if len(extra) == 0 {
		return out
	}
	if len(out) > 0 {
		out = append(out, row{entry: -1})
	}
	return append(out, extra...)
}

func (a *app) execTranscriptRows(width int) []row {
	if a.exec == nil {
		return nil
	}
	id := a.conversationRef()
	var matched []ExecWork
	for _, work := range a.execView.works {
		if execWorkBelongs(work, id) {
			matched = append(matched, work)
		}
	}
	if len(matched) == 0 {
		if a.execView.down {
			return a.execDimRows(execUnavailableWord, width)
		}
		return nil
	}
	var out []row
	for _, work := range matched {
		line := a.execWorkLine(work)
		if line == "" {
			continue
		}
		out = append(out, a.execDimRows(line, width)...)
	}
	return out
}

func (a *app) execWorkLine(work ExecWork) string {
	state := execStateWord(work.State)
	title := strings.TrimSpace(work.Title)
	if title == "" {
		title = strings.TrimSpace(work.SourceRef)
	}
	mark := a.execStateGlyph(state)
	line := strings.TrimSpace(mark + " " + state)
	if title != "" {
		line += " " + execSourceWord + " " + title
	}
	if work.Joined {
		line += " · " + execJoinWord
	}
	if road := strings.TrimSpace(work.Road); road != "" {
		line += " · " + road
	}
	return strings.TrimSpace(line)
}

func (a *app) execStateGlyph(state string) string {
	switch state {
	case "paused":
		return a.icon(tokens.GPaused)
	case "stopped":
		return a.icon(tokens.GStopped)
	case "done":
		return a.icon(tokens.GSettled)
	case "incomplete":
		return a.icon(tokens.GFailed)
	case "pending":
		return a.icon(tokens.GActionCoordinate)
	default:
		return a.icon(tokens.GWorking)
	}
}

func (a *app) execDimRows(text string, width int) []row {
	var out []row
	for i, line := range wrap(text, max(1, width-2)) {
		lead := "· "
		if i > 0 {
			lead = "  "
		}
		out = append(out, row{text: a.pal.dim(lead + line), entry: -1})
	}
	return out
}

// folderExecNote is launch state on a folders-panel member. Empty work adds
// nothing. Joined work quotes launch-or-join. The panel never draws `0 runs`.
func folderExecNote(in *homeGridInput, refID string) string {
	var parts []string
	for _, work := range in.folders.works {
		if !execWorkBelongs(work, refID) {
			continue
		}
		if text := folderExecWorkText(work); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " · ")
}

func joinExecNote(left, right string) string {
	if right == "" || right == left {
		return left
	}
	if left == "" {
		return right
	}
	return left + " · " + right
}

// folderExecLines is launch state as its own dim rows, because a member note
// is a gloss the squeeze can fold. Empty work adds nothing. A present seam
// that cannot read names the absence rather than looking like an empty roll-up.
func folderExecLines(in *homeGridInput) []homeLine {
	if in.folders.execDown && len(in.folders.works) == 0 {
		return []homeLine{folderExecWhisper(execUnavailableWord)}
	}
	var lines []homeLine
	for _, work := range in.folders.works {
		if text := folderExecWorkText(work); text != "" {
			lines = append(lines, folderExecWhisper(text))
		}
	}
	return lines
}

func folderExecWorkText(work ExecWork) string {
	state := execStateWord(work.State)
	title := strings.TrimSpace(work.Title)
	if title == "" {
		title = strings.TrimSpace(work.SourceRef)
	}
	line := state
	if work.Joined {
		line = joinExecNote(execJoinWord, line)
	}
	if title != "" {
		line = joinExecNote(line, title)
	}
	if road := strings.TrimSpace(work.Road); road != "" {
		line = joinExecNote(line, road)
	}
	return line
}

func folderExecWhisper(text string) homeLine {
	return homeLine{
		kind: homeSwitchHead,
		cell: &homeCell{kind: cellWhisper, panel: panelFolders, title: text},
	}
}

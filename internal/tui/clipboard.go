package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// For a product whose deliverables are findings and file paths, there was no
// way to get either out: no clipboard, no opener, and the alt screen takes the
// terminal's own drag-select with it. Two keys and one command close it, and
// they act on the answer you are looking at.
//
// The copy goes through OSC 52, which is the terminal's own clipboard and the
// only route that survives ssh — the same reason paths are OSC 8 links rather
// than something only a local terminal understands. A few terminals refuse it,
// so the platform's clipboard tool is asked as well; whichever works, the same
// text lands.
const osc52Limit = 64 << 10

func osc52Clipboard(text string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\x07"
}

// clipboardSink and processOpener are the two places this file reaches out of
// the program. They are variables so a test can watch what would have been
// copied or opened without touching the machine it runs on.
var (
	clipboardSink = copyToClipboard
	processOpener = startOpener
)

// copyToClipboard is deliberately best-effort and silent about its route: the
// person is told what was copied, not which of two mechanisms carried it.
func copyToClipboard(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if len(text) <= osc52Limit {
		fmt.Fprint(os.Stdout, osc52Clipboard(text))
	}
	name, args := clipboardCommand()
	if name == "" {
		return
	}
	command := exec.Command(name, args...)
	command.Stdin = strings.NewReader(text)
	_ = command.Run()
}

func clipboardCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbcopy", nil
	case "linux":
		if path, err := exec.LookPath("wl-copy"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath("xclip"); err == nil {
			return path, []string{"-selection", "clipboard"}
		}
	}
	return "", nil
}

func openerCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "linux":
		return "xdg-open", nil
	}
	return "", nil
}

// focusedAnswer is the message the copy keys and /open act on: the one under
// the thread zone's focused line, or — when the thread is not the focused zone
// — the last answer that arrived, which is what a person means by "it".
func (m *Model) focusedAnswer() (store.Message, bool) {
	if m.focus == focusChat {
		targets := m.chatFocusLines()
		if len(targets) > 0 {
			line := targets[max(0, min(m.chatFocusIndex, len(targets)-1))]
			for _, row := range m.chatMessageRows {
				if line < row.start || line > row.end {
					continue
				}
				if message, ok := m.messageBySeq(row.seq); ok {
					return message, true
				}
			}
		}
	}
	for index := len(m.messages) - 1; index >= 0; index-- {
		message := m.messages[index]
		if message.Role == store.RoleUser || secondaryMessage(message) {
			continue
		}
		if strings.TrimSpace(message.Body) == "" {
			continue
		}
		return message, true
	}
	return store.Message{}, false
}

// deliverablePaths reads the files an answer named, by the same rule the
// renderer already uses to make them clickable: a line that is one absolute
// path, or a workspace-relative name the resolver can find on disk.
func (m *Model) deliverablePaths(message store.Message) []string {
	seen := make(map[string]bool)
	paths := make([]string, 0, 2)
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	for _, line := range strings.Split(ansi.Strip(message.Body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if filepath.IsAbs(trimmed) && !strings.ContainsAny(trimmed, "\t") {
			add(trimmed)
			continue
		}
		if resolved, ok := m.resolveWorkspacePath(message.NodeID, filepath.Clean(trimmed)); ok {
			add(resolved)
		}
	}
	return paths
}

// copyAnswer puts the answer's own text on the clipboard, stripped of the
// styling that made it readable here and useless anywhere else.
func (m *Model) copyAnswer() tea.Cmd {
	message, ok := m.focusedAnswer()
	if !ok {
		return m.showStatus("nothing to copy yet")
	}
	body := strings.TrimSpace(ansi.Strip(message.Body))
	if body == "" {
		return m.showStatus("that message has no text to copy")
	}
	clipboardSink(body)
	return m.showStatus("copied the answer")
}

// copyDeliverablePath puts the file the answer produced on the clipboard —
// the single most frequent thing anyone wants out of this product.
func (m *Model) copyDeliverablePath() tea.Cmd {
	message, ok := m.focusedAnswer()
	if !ok {
		return m.showStatus("nothing to copy yet")
	}
	paths := m.deliverablePaths(message)
	if len(paths) == 0 {
		return m.showStatus("that answer named no file — y copies its text")
	}
	clipboardSink(strings.Join(paths, "\n"))
	if len(paths) == 1 {
		return m.showStatus("copied " + paths[0])
	}
	return m.showStatus(fmt.Sprintf("copied %d paths", len(paths)))
}

// slashOpen hands the deliverable to the platform, which is where a document
// goes to be read by anything other than aforge.
func (m *Model) slashOpen(arguments []string) tea.Cmd {
	message, ok := m.focusedAnswer()
	if !ok {
		return m.showStatus("nothing to open yet")
	}
	paths := m.deliverablePaths(message)
	if len(arguments) > 0 {
		wanted := strings.TrimSpace(strings.Join(arguments, " "))
		if resolved, found := m.resolveWorkspacePath(message.NodeID, filepath.Clean(wanted)); found {
			paths = []string{resolved}
		} else if filepath.IsAbs(wanted) {
			paths = []string{wanted}
		}
	}
	if len(paths) == 0 {
		return m.showStatus("that answer named no file to open")
	}
	target := paths[0]
	if err := processOpener(target); err != nil {
		return m.showStatus(err.Error())
	}
	return m.showStatus("opened " + filepath.Base(target))
}

// startOpener hands the file to the platform and does not wait for whatever
// opens it: a text editor left open must not hold a goroutine here.
func startOpener(target string) error {
	name, args := openerCommand()
	if name == "" {
		return fmt.Errorf("no opener on this platform — Y copies the path")
	}
	command := exec.Command(name, append(append([]string(nil), args...), target)...)
	if err := command.Start(); err != nil {
		return fmt.Errorf("could not open %s", filepath.Base(target))
	}
	guard.Go("tui/open", func() { _ = command.Wait() })
	return nil
}

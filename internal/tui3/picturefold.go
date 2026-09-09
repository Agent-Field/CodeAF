package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// mediaItem preserves which machine owns a file independently of its display
// name. Every transcript image reaches the same controls through this value.
type mediaItem struct {
	path string
	here bool
}

// entryMedia discovers references only. Collapsed media never stats or decodes
// a file, so a long image-heavy conversation scrolls at the cost of text.
func (a *app) entryMedia(e *entry) []mediaItem {
	if e.kind == entryUser {
		out := make([]mediaItem, 0, len(e.pictures))
		for _, path := range e.pictures {
			out = append(out, mediaItem{path, e.picturesHere})
		}
		return out
	}
	if e.kind == entryTool && !e.status.live() {
		if path, ok := a.picturePath(e); ok {
			return []mediaItem{{path: path}}
		}
	}
	return nil
}

// mediaRows is the shared presentation for sent and tool-produced images in
// both transcript owners. The original-file action survives every colour and
// accessibility tier; the cell preview is optional and explicitly requested.
func (a *app) mediaRows(e *entry, entryIndex, width int, lead string) []row {
	items := a.entryMedia(e)
	out := make([]row, 0, len(items))
	for i, item := range items {
		open := e.pictureExpanded == i+1
		if e.kind == entryTool {
			open = e.open
		}
		action := "preview"
		if open {
			action = "collapse"
		}
		suffix := "  [open original]"
		prefix := lead + bandFoldMark(a.pal, !open) + " "
		name := drawableLine(filepath.Base(item.path))
		label := name + railSep + action
		room := width - ansi.StringWidth(prefix) - ansi.StringWidth(suffix)
		if room < 1 {
			suffix = "  open"
			room = width - ansi.StringWidth(prefix) - ansi.StringWidth(suffix)
		}
		if room < 1 {
			room = 1
			suffix = ""
		}
		tail := railSep + action
		if nameRoom := room - ansi.StringWidth(tail); nameRoom > 0 {
			label = fit(name, nameRoom) + tail
		} else {
			label = fit(action, room)
		}
		text := prefix + fit(label, room)
		start := ansi.StringWidth(text)
		text += suffix
		control := row{text: fit(a.pal.dim(prefix+fit(label, room))+a.pal.muted(suffix), width), entry: entryIndex, hit: hitPictures, pictureIndex: i}
		if suffix != "" {
			control.pictureOpen = hudSpan{from: start + 2, to: min(width, start+ansi.StringWidth(suffix))}
		}
		out = append(out, control)
		if !open || e.kind == entryTool {
			continue
		}
		cols := max(1, width-ansi.StringWidth(lead))
		preview, drawn := a.pictureRowsFor(item.path, item.here, cols, pictureRowsMax)
		if !drawn {
			out = append(out, row{text: lead + a.pal.dim(fit("Preview unavailable · open the original", cols)), entry: entryIndex})
			continue
		}
		out = append(out, row{text: lead + a.pal.muted(fit("Click image to open full size · alt+o", cols)), entry: entryIndex, hit: hitPictureOriginal, pictureIndex: i})
		for _, line := range preview {
			out = append(out, row{text: lead + line, entry: entryIndex, hit: hitPictureOriginal, pictureIndex: i})
		}
	}
	return out
}

// togglePictureAt changes only display state. At most one image per message
// expands, so opening a second attachment replaces the first preview.
func (a *app) togglePictureAt(index, picture int) {
	es := a.bodyDeck().entries
	if index < 0 || index >= len(es) {
		return
	}
	e := &es[index]
	if picture < 0 || picture >= len(a.entryMedia(e)) {
		return
	}
	if e.kind == entryTool {
		a.openTool(index)
		return
	}
	if e.pictureExpanded == picture+1 {
		e.pictureExpanded = 0
	} else {
		e.pictureExpanded = picture + 1
	}
	e.stale = true
	if a.room != nil {
		a.room.dirty = true
	}
	a.touch()
}

// visiblePicture chooses the last image intersecting
// the viewport, including its preview. An expansion taller than the viewport
// therefore remains keyboard-collapsible when its control scrolls offscreen.
func (a *app) visiblePicture() (int, int, bool) {
	es := a.bodyDeck().entries
	for y := a.bodyTop() + a.viewHeight() - 1; y >= a.bodyTop(); y-- {
		r, ok := a.rowAt(y)
		if !ok || r.entry < 0 || r.entry >= len(es) {
			continue
		}
		e := &es[r.entry]
		if len(a.entryMedia(e)) == 0 {
			continue
		}
		picture := max(0, e.pictureExpanded-1)
		if r.hit == hitPictures {
			picture = r.pictureIndex
		}
		return r.entry, picture, true
	}
	return 0, 0, false
}

func (a *app) toggleVisiblePictures() {
	if index, picture, ok := a.visiblePicture(); ok {
		a.togglePictureAt(index, picture)
	}
}

func (a *app) openVisiblePicture() tea.Cmd {
	if index, picture, ok := a.visiblePicture(); ok {
		return a.openPictureAt(index, picture)
	}
	return nil
}

func (a *app) openPictureAt(index, picture int) tea.Cmd {
	es := a.bodyDeck().entries
	if index < 0 || index >= len(es) {
		return nil
	}
	items := a.entryMedia(&es[index])
	if picture < 0 || picture >= len(items) {
		return nil
	}
	return a.openMediaOriginal(items[picture])
}

type pictureOpenedMsg struct {
	path string
	err  error
}

// openMediaOriginal shares the file shelf's platform handoff and the hosted
// session's fetch-and-mirror flow. It never starts a model turn or blocks paint.
func (a *app) openMediaOriginal(item mediaItem) tea.Cmd {
	// The viewer must run beside the person, not on the SSH server.
	if a.remote {
		a.note("This terminal is over SSH. Use aforge --host from your computer to open the original, or copy this file: " + drawableLine(item.path))
		return nil
	}
	if !item.here && a.rfiles != nil {
		return a.openRemotePath(item.path)
	}
	return func() tea.Msg { return pictureOpenedMsg{path: item.path, err: processOpener(item.path)} }
}

// pictureOriginalRow identifies the preview pixels and their explicit action
// caption. Tool details and the phone sheet use the same pointer contract.
func pictureOriginalRow(line string) bool {
	return strings.Contains(line, halfBlock) || strings.Contains(ansi.Strip(line), "Click image to open full size")
}

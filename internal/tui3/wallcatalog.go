package tui3

import (
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A grid refresh takes the same metadata reading as the switcher. Only visible
// saved cards read transcript tails, so a large library never replays its journals.
const wallCatalogEvery = 3 * time.Second

type wallSavedReadMsg struct {
	generation int
	reads      []wallReadMsg
	missing    []string
}

func (a *app) wallCatalogRead() tea.Cmd {
	if !a.wall.on || a.wall.catalogBusy {
		return nil
	}
	now := a.now()
	if !a.wall.catalogAt.IsZero() && now.Sub(a.wall.catalogAt) < wallCatalogEvery {
		return nil
	}
	gen := a.wall.catalogGen
	a.wall.catalogBusy = true
	a.wall.catalogAt = now
	return a.conversationCatalogRead(func(rows []conversationCandidate, known bool) tea.Cmd {
		if !a.wall.on || a.wall.catalogGen != gen {
			return nil
		}
		focused := a.wallFocusedKey(a.wallShown(a.now()))
		a.wall.catalogBusy = false
		a.wall.catalogKnown = known
		a.wall.catalog = rows
		a.wallFocusKey(focused)
		a.touch()
		return a.wallSavedReadCmd()
	})
}

// Saved previews are local reads only. Hosted catalogs never open matching
// paths on this machine; their metadata stays usable when no preview door exists.
func (a *app) wallSavedReadCmd() tea.Cmd {
	if !a.wall.on || a.wall.savedReading || a.hosted() {
		return nil
	}
	tiles := a.wallShown(a.now())
	cols, room := a.wallGeometry(len(tiles))
	width, _ := a.size()
	_, _, tileH := wallGrid(len(tiles), width, room, a.wall.cols)
	scroll := wallScrollFor(a.wall.focus, a.wall.scroll, len(tiles), width, room, a.wall.cols)
	first, last := scroll*cols, min((scroll+max(wallVisibleRows(room, tileH), 1))*cols, len(tiles))
	var tabs []chatTab
	for i := first; i < last; i++ {
		tab := tiles[i].tab
		if agent, _ := a.wallAgentFor(tab.key); agent != nil {
			continue
		}
		if at := a.wall.savedAt[tab.key]; !at.IsZero() && a.now().Sub(at) < wallCatalogEvery {
			continue
		}
		tabs = append(tabs, tab)
	}
	if len(tabs) == 0 {
		return nil
	}
	gen := a.wall.catalogGen
	a.wall.savedReading = true
	return func() tea.Msg {
		msg := wallSavedReadMsg{generation: gen}
		for _, tab := range tabs {
			entries, modified, err := wallReadSavedTail(tab.file)
			if os.IsNotExist(err) {
				msg.missing = append(msg.missing, tab.key)
				continue
			}
			msg.reads = append(msg.reads, wallReadMsg{key: tab.key, entries: entries, modified: modified, at: time.Now()})
		}
		return msg
	}
}

// The shared preview byte budget also bounds a saved grid card. Cutting the
// first partial JSON line keeps the last complete transcript entries intact.
func wallReadSavedTail(file string) ([]session.DisplayEntry, time.Time, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	start := max(info.Size()-teamsPreviewBytes, 0)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return nil, time.Time{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, teamsPreviewBytes))
	if err != nil {
		return nil, time.Time{}, err
	}
	if start > 0 {
		if newline := strings.IndexByte(string(data), '\n'); newline >= 0 {
			data = data[newline+1:]
		} else {
			return nil, info.ModTime(), nil
		}
	}
	return session.ReadTranscriptBytes(data).Entries, info.ModTime(), nil
}

func (a *app) wallTakeSavedRead(msg wallSavedReadMsg) {
	if !a.wall.on || msg.generation != a.wall.catalogGen {
		return
	}
	a.wall.savedReading = false
	if a.wall.savedAt == nil {
		a.wall.savedAt = map[string]time.Time{}
	}
	for _, read := range msg.reads {
		// A live reading always wins if this conversation was attached meanwhile.
		if agent, _ := a.wallAgentFor(read.key); agent != nil {
			continue
		}
		a.wallTakeRead(read)
		a.wall.savedAt[read.key] = a.now()
	}
	if len(msg.missing) > 0 {
		missing := map[string]bool{}
		for _, key := range msg.missing {
			missing[key] = true
			delete(a.wall.marked, key)
			delete(a.wall.tails, key)
		}
		kept := a.wall.catalog[:0]
		for _, row := range a.wall.catalog {
			if !missing[row.tab.key] || a.trafficHeld(row.tab.key) {
				kept = append(kept, row)
			}
		}
		a.wall.catalog = kept
	}
	a.touch()
}

// The cached catalog includes dismissed tabs. Current strip records refresh
// live identities, while saved metadata supplies state and cost for other cards.
func (a *app) wallCandidates() []conversationCandidate {
	return a.conversationCatalogMerge(a.wall.catalog)
}

package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── THE SPLIT ───────────────────────────────────────────────────────────────
//
// The divider between the floor's rows and the peek is the person's to move.
// `{` and `}` move it [factorySplitStep] columns left and right, `|` puts it
// back at [factoryRowsShare] percent, and the pointer drags it: a press on the
// divider's column starts a drag, motion moves it, and the release lets go.
//
// TWO LIMITS HOLD WHEREVER IT IS PUT. The rows never have fewer than
// [factoryRowsMin] columns, because under that a row loses the facts that make
// it a row; the peek never has fewer than [factoryPeekMin], because under that
// its sixty-cell prose is a column of fragments. A key or a drag past either
// stops at it.
//
// THE CHOICE IS REMEMBERED PER HOME, as the rows' share of the width rather
// than as a column, so a divider set on a wide terminal stands in the same
// proportion on a narrower one. It lives in `factory.json` beside the
// profile's `config.json` ([factoryPrefsPath]) — tui3 had no file for a
// surface's own layout, and a layout is not a setting the Settings page
// should carry — read once a launch off the loop with the floor's first read
// (factory_page.go's [app.factoryRead]) and written off the loop whenever the
// divider settles.
//
// AND A PRESS ON THE FLOOR IS READ HERE, before the place's own press, because
// the place's press knows the row and not the column: a press on a row puts
// the cursor there, two open it, a press on the item page's rail selects that
// row, and a press in the peek's column moves nothing.

// factoryPrefsName is the file the floor's own layout is remembered in.
const factoryPrefsName = "factory.json"

// factoryPrefs is that file's shape. Split is the rows' share of the width in
// percent; 0, or a file that is not there, is [factoryRowsShare].
type factoryPrefs struct {
	Split float64 `json:"split,omitempty"`
	// Handover is `h`: true draws the handover's four rows, false (and a file
	// written before it existed) its one line (factory_head.go).
	Handover bool `json:"handover,omitempty"`
}

// factoryPrefsPath is where the floor's layout is remembered for the profile
// at profileDir, which is codeaf's own home on an ordinary launch.
func factoryPrefsPath(profileDir string) string {
	return config.ProfilePath(profileDir, factoryPrefsName)
}

// readFactoryPrefs reads the file, and answers the zero prefs for every way
// that can fail: a divider that cannot be remembered starts where it always
// did, which is not worth a complaint.
func readFactoryPrefs(path string) factoryPrefs {
	raw, err := os.ReadFile(path)
	if err != nil {
		return factoryPrefs{}
	}
	var p factoryPrefs
	if json.Unmarshal(raw, &p) != nil || p.Split < 0 || p.Split > 100 {
		return factoryPrefs{}
	}
	return p
}

// writeFactoryPrefs replaces the file through a temporary, so a process that
// dies mid-write leaves the previous one readable.
func writeFactoryPrefs(path string, p factoryPrefs) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// factoryRowsColsAt is the rows' columns at width with the rows' share at
// share percent (0 for [factoryRowsShare]): the whole width under
// [factoryPaneFloor], and otherwise the share, held inside the two limits.
func factoryRowsColsAt(width int, share float64) int {
	if width < factoryPaneFloor {
		return width
	}
	cols := width * factoryRowsShare / 100
	if share > 0 {
		cols = int(float64(width)*share/100 + 0.5)
	}
	return factoryClampRows(width, cols)
}

// factoryClampRows holds a column count for the rows inside the two limits:
// never under [factoryRowsMin], and never leaving the peek, past the divider's
// one column, under [factoryPeekMin]. The rows' floor wins where the two meet.
func factoryClampRows(width, cols int) int {
	return max(min(cols, width-1-factoryPeekMin), factoryRowsMin)
}

// factoryRowsAt is the rows' columns at width with the divider where the
// person put it.
func (a *app) factoryRowsAt(width int) int { return factoryRowsColsAt(width, a.fp.split) }

// factorySplitWidth is the width the divider is moved within: the last body
// drawn, or the terminal before one has been.
func (a *app) factorySplitWidth() int {
	if a.fp.bodyW > 0 {
		return a.fp.bodyW
	}
	return a.width
}

// factorySetRows puts the divider so the rows have cols columns, held inside
// the limits, and answers whether it moved.
func (a *app) factorySetRows(cols int) bool {
	width := a.factorySplitWidth()
	if width < factoryPaneFloor {
		return false
	}
	was := a.factoryRowsAt(width)
	cols = factoryClampRows(width, cols)
	a.fp.splitRead = true
	if cols == was {
		return false
	}
	a.fp.split = float64(cols) * 100 / float64(width)
	a.touch()
	return true
}

// factorySplitKey is `{`, `}` and `|` on the floor, and answers false for
// every other key. A key that cannot move the divider — the item page is
// open, or the terminal is too narrow for a peek — is still the split's, and
// does nothing.
func (a *app) factorySplitKey(k string) (tea.Cmd, bool) {
	var moved bool
	switch k {
	case "{", "shift+[":
		moved = a.factorySplitShown() && a.factorySetRows(a.factoryRowsAt(a.factorySplitWidth())-factorySplitStep)
	case "}", "shift+]":
		moved = a.factorySplitShown() && a.factorySetRows(a.factoryRowsAt(a.factorySplitWidth())+factorySplitStep)
	case "|", "shift+\\":
		if a.factorySplitShown() {
			moved = a.fp.split != 0
			a.fp.split, a.fp.splitRead = 0, true
			a.touch()
		}
	default:
		return nil, false
	}
	if !moved {
		return nil, true
	}
	return a.factorySaveSplit(), true
}

// factorySplitShown says whether a divider is on the screen to move: the
// floor, not a page over it, at a width that draws the peek.
func (a *app) factorySplitShown() bool {
	return !a.fp.open && a.fp.pick == nil && a.fp.recipe == nil && a.factoryFloorHas() && a.factorySplitWidth() >= factoryPaneFloor
}

// factorySaveSplit writes the floor's layout off the loop: the divider's share
// and the handover's height, both, because the file is replaced whole.
func (a *app) factorySaveSplit() tea.Cmd {
	path, p := factoryPrefsPath(a.profileDir), factoryPrefs{Split: a.fp.split, Handover: a.fp.headFull}
	return func() tea.Msg {
		_ = writeFactoryPrefs(path, p)
		return nil
	}
}

// factoryPointer is a pointer event on the factory floor or its item page,
// answered before the place's own press and hover. It answers false for an
// event it leaves to the router: off the factory, under a layer, on the head
// or the foot, or over the floor's own settings pages.
func (a *app) factoryPointer(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	if a.fp.dragging {
		switch msg.(type) {
		case tea.MouseMotionMsg:
			a.factorySetRows(m.X)
			return nil, true
		case tea.MouseReleaseMsg:
			a.fp.dragging = false
			return a.factorySaveSplit(), true
		case tea.MouseClickMsg:
			// A second press with the first never let go: the first is over.
			a.fp.dragging = false
		}
	}
	if _, click := msg.(tea.MouseClickMsg); !click || m.Button != tea.MouseLeft {
		return nil, false
	}
	if !a.at(pageFactory) || a.composer.open || !a.factoryConnected() || a.fp.pick != nil || a.fp.recipe != nil {
		return nil, false
	}
	row := m.Y - placeHeadRows
	if row < 0 || row >= a.fp.bodyRows() {
		return nil, false
	}
	if a.fp.open {
		return a.factoryItemPress(m.X, m.Y, row), true
	}
	paneW := a.fp.bodyW - a.fp.rowsW - 1
	if paneW > 0 && row >= a.fp.headRows && abs(m.X-a.fp.rowsW) <= 1 {
		a.fp.dragging = true
		return nil, true
	}
	if paneW > 0 && m.X > a.fp.rowsW {
		// The peek is read, not pressed: a press there moves nothing.
		return nil, true
	}
	if !a.factoryPress(m.Y) {
		return nil, true
	}
	if a.countClick(m.X, m.Y) >= 2 {
		a.factoryOpenItem()
	}
	return nil, true
}

// bodyRows is how many rows of the screen the last body drew under the head:
// the handover and the rail's window on the floor, the whole room on the item
// page.
func (fp *factoryPage) bodyRows() int {
	if fp.open {
		return fp.pageRows
	}
	return fp.headRows + fp.shown
}

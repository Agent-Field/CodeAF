package settings

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// EntryID names the row internal/registry already carries for this surface —
// "open settings", alt+, , /settings. The wiring lane binds THAT entry to this
// pane rather than authoring a second one, which is the whole point of one
// registry (5.22): the palette row, the slash alias and the accelerator are
// already written down, in one place, and this package only has to agree with
// them. entry_test.go fails if the id ever stops resolving.
const EntryID = "slash.settings"

// DefaultDebounce is how long a value sits on screen before it is written.
//
// It is deliberately longer than the shell's resize window and shorter than a
// pause a person would read as "nothing happened": a bool being toggled four
// times while somebody makes up their mind is one write, not four, and the
// atomic read-modify-rename underneath is a whole file per write. 8.2.19 asks
// for debounced atomic writes; this is the debounce, and [config.Setting.Apply]
// is the atomic write.
const DefaultDebounce = 300 * time.Millisecond

// EscMsg is emitted when esc has run out of things to close and the surface
// itself should go away. It exists so a host can drive closing through its own
// Update — the same contract internal/tui2/composer already publishes for the
// same key — and it is only sent when [Options.OnClose] is nil.
type EscMsg struct{}

// ModelMsg asks the host to open the capability-filtered model picker for one
// role slot. Model rows are the one kind this surface does not edit itself:
// the models door is a different pane with its own catalog, and duplicating it
// inside a settings row would be a second place to set a role binding, which
// 8.2.16 forbids in as many words. Sent only when [Options.OnModel] is nil.
type ModelMsg struct{ Slot string }

// Options configures the surface. The zero value renders a calm, honest,
// empty sheet rather than panicking: every seam here is optional, and a
// missing one degrades a row to read-only instead of removing it.
type Options struct {
	// Registry is the settings registry to render. Nil is legal and says so on
	// screen — a surface that cannot reach the store must not pretend it has
	// no settings.
	Registry *config.Settings

	// Styler paints every cell. A nil Styler renders plain text, which is what
	// a NoColor terminal and most tests want.
	Styler *tokens.Styler

	// Linear is the accessible rendering (10.1.5). This surface is already one
	// column and already still — there is no motion here to reduce — so the
	// one thing it changes is the selection: the band is a background fill,
	// and linear mode drops the fill and keeps the accent-rail marker, which
	// carries the same fact in a printable cell.
	Linear bool

	// Invalidate tells the shell the frame is stale. Called only when a fact
	// moved that the shell could not observe — a debounced write landing, an
	// error arriving — never on a keystroke the shell already saw.
	Invalidate func()

	// OnClose runs when esc has nothing left to close. Nil emits [EscMsg].
	OnClose func() tea.Cmd

	// OnModel opens the models door for one slot. Nil emits [ModelMsg].
	OnModel func(slot string) tea.Cmd

	// Debounce overrides [DefaultDebounce]. A negative value writes through
	// immediately, which is what a test that does not want to think about time
	// asks for.
	Debounce time.Duration

	// Now is the clock the debounce reads. Nil takes time.Now; a test injects
	// one so the write path is exercised without a sleep anywhere in it.
	Now func() time.Time
}

// Model is the surface. It is used through a pointer and holds no goroutine,
// no timer and no lock: the debounce is a deadline compared against a clock on
// the paths that already run (a key, a frame, a close), so there is no second
// thread to race the renderer and nothing to leak if the host drops the pane.
type Model struct {
	registry *config.Settings
	base     *tokens.Styler
	styler   *tokens.Styler
	linear   bool

	invalidate func()
	onClose    func() tea.Cmd
	onModel    func(slot string) tea.Cmd

	debounce time.Duration
	now      func() time.Time

	// The projected registry: every row once, in registry order, plus the group
	// words built from the categories that actually have rows.
	rows   []row
	groups []string

	selected int
	query    string

	// visible is the row index list the gates and the query resolve to, and
	// hits is the parallel list of label highlight offsets. Both are
	// recomputed whenever the query or a gated value moves, never
	// inside Render.
	visible []int
	hits    [][]int

	// pending holds edits made but not yet written, keyed by registry key.
	// deadline is when the quiet period ends and the pending edits become one
	// write; epoch invalidates a tick armed for a deadline that has since
	// moved, exactly as the shell's resize debounce does.
	pending  map[string]edit
	deadline time.Time
	armed    bool
	epoch    uint64
	needTick bool

	// wrote remembers the rows this session has successfully written, so a row
	// whose store this surface cannot inspect still reports honest provenance
	// after we are the ones who saved it.
	wrote map[string]bool

	// persisted is the registry's answer to "which rows are written down",
	// re-read when the sheet opens and after every write rather than per frame.
	persisted map[string]bool

	// failed carries the plain-language refusal from the last write of a row.
	failed map[string]string

	// trail is the path this sheet was reached through — the palette row that
	// opened it — root-most first, and empty for a sheet opened on its own.
	// onBack leaves for one of those rungs, and steps is where the last render
	// put each ancestor word so a click can be turned back into one. See
	// trail.go.
	trail   []string
	onBack  func(depth int) tea.Cmd
	steps   []trailStep
	editing bool
	picking bool
	pick    int
	editor  field

	// rowAtLine maps a line of the last frame to a position in visible, so a
	// click lands on the row the pointer is over rather than on one this pane
	// guessed from a row height it assumed. It is a render artifact — nothing
	// the next frame reads — and it is the reason [Model.Mouse] never has to
	// know how tall a detail block is.
	rowAtLine []int

	focused bool
}

var (
	_ tui2.Pane      = (*Model)(nil)
	_ tui2.PaneKeys  = (*Model)(nil)
	_ tui2.PaneFocus = (*Model)(nil)
	_ tui2.PaneMouse = (*Model)(nil)
)

// New builds the surface and reads the registry once.
func New(opts Options) *Model {
	m := &Model{
		registry:   opts.Registry,
		base:       opts.Styler,
		styler:     opts.Styler,
		linear:     opts.Linear,
		invalidate: opts.Invalidate,
		onClose:    opts.OnClose,
		onModel:    opts.OnModel,
		debounce:   opts.Debounce,
		now:        opts.Now,
		pending:    map[string]edit{},
		wrote:      map[string]bool{},
		persisted:  map[string]bool{},
		failed:     map[string]string{},
		focused:    true,
	}
	if m.debounce == 0 {
		m.debounce = DefaultDebounce
	}
	if m.now == nil {
		m.now = time.Now
	}
	m.Refresh()
	return m
}

// Refresh re-reads the registry: the rows, the tabs, and which of them are
// written down. The host calls it when something outside this surface changed a
// setting (the models door, a /budget command); everything inside calls it
// after its own write lands.
func (m *Model) Refresh() {
	m.rows = m.rows[:0]
	m.groups = m.groups[:0]
	if m.registry != nil {
		for _, group := range m.registry.Groups() {
			m.groups = append(m.groups, group.Title)
			for _, setting := range group.Rows {
				m.rows = append(m.rows, row{setting: setting, group: group.Title})
			}
		}
		clear(m.persisted)
		for _, key := range m.registry.PersistedKeys() {
			m.persisted[key] = true
		}
	}
	m.reselect()
}

// Focus implements tui2.PaneFocus. Dimming is a property of the pane (8.3), so
// an unfocused sheet swaps one styler rather than re-resolving every row.
func (m *Model) Focus(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	if m.base == nil {
		return
	}
	if focused {
		m.styler = m.base.WithFocus(tokens.FocusNormal)
		return
	}
	m.styler = m.base.WithFocus(tokens.FocusDimmed)
}

// Close flushes anything still pending and returns the host's close command.
// It is the durability guarantee the debounce would otherwise cost: a value
// typed and then escaped out of is written before the surface goes away, so
// the last thing a user did is never the one thing that did not land.
func (m *Model) Close() tea.Cmd {
	m.Flush()
	if m.onClose != nil {
		return m.onClose()
	}
	return func() tea.Msg { return EscMsg{} }
}

// Query reports the live search string. It is exported for the wiring lane's
// breadcrumb, which shows the same filter in the scope header when this pane
// is mounted as the main surface rather than as an overlay.
func (m *Model) Query() string { return m.query }

// Group reports the group word the band is standing in, or the empty string
// when the sheet has no rows. It is exported for the wiring lane's breadcrumb;
// there is no selected group any more, only a selected row, and the group is
// the one that row belongs to.
func (m *Model) Group() string {
	r, ok := m.current()
	if !ok {
		return ""
	}
	return r.group
}

// Selected reports the registry key under the band, or "" when the sheet is
// empty. The wiring lane reads it to keep a footer's action strip in step.
func (m *Model) Selected() (string, bool) {
	r, ok := m.current()
	if !ok {
		return "", false
	}
	return r.setting.Key, true
}

func (m *Model) touch() {
	if m.invalidate != nil {
		m.invalidate()
	}
}

// row is one registry row projected into the sheet: the setting itself plus
// the group word it sits under, so a search result can name its group and the
// page can head each run of rows without walking the registry again.
type row struct {
	setting config.Setting
	group   string
}

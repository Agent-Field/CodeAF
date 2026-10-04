package atlas

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Model is the whole atlas state. It is a pointer used as a tea.Model, so
// Update hands the same model back and the tests can drive one directly
// without a program.
type Model struct {
	data *Map
	W, H int

	// pos is each box's centre as fractions of the map area; a drag edits it.
	pos map[string][2]float64

	selected string // keyboard selection (tab cycles it)
	detail   string // node whose detail pane is open
	flow     int    // index into Flows, or -1 for the plain overview
	step     int
	playing  bool
	help     bool

	drag  *drag
	rects map[string]Rect
	hits  []hit
}

// drag is a box being moved by the mouse: where it was grabbed and whether the
// mouse has actually moved — a release without movement is a click, which
// opens the detail pane instead of leaving the box where it was.
type drag struct {
	id         string
	offX, offY int
	moved      bool
}

// hit is a clickable rectangle recorded while the frame is drawn; a mouse
// click is matched against these in draw order, last drawn wins.
type hit struct {
	rect Rect
	kind hitKind
	arg  int
}

type hitKind byte

const (
	hitTab hitKind = iota
	hitPrev
	hitNext
)

// tickMsg is one beat of a playing flow.
type tickMsg struct{}

// New returns the model in its opening state: the plain overview, nothing
// selected, at whatever size the terminal is (80×24 until the first
// WindowSizeMsg says otherwise).
func New(mp *Map, width, height int) *Model {
	pos := make(map[string][2]float64, len(mp.Nodes))
	for _, n := range mp.Nodes {
		pos[n.ID] = [2]float64{n.X, n.Y}
	}
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	return &Model{
		data: mp,
		W:    width,
		H:    height,
		pos:  pos,
		flow: -1,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// View implements tea.Model. Cell-motion mouse is what dragging needs: press,
// move-while-pressed and release, and no motion events when a button is up.
// The alternate screen is what puts the terminal back the way it was found.
func (m *Model) View() tea.View {
	return tea.View{
		Content:   m.drawFrame(),
		AltScreen: true,

		MouseMode: tea.MouseModeCellMotion,
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m, m.updateKey(msg)
	case tea.MouseClickMsg:
		m.updateMouseClick(msg)
		return m, nil
	case tea.MouseMotionMsg:
		m.updateMouseMotion(msg)
		return m, nil
	case tea.MouseReleaseMsg:
		m.updateMouseRelease(msg)
		return m, nil
	case tickMsg, Beat:
		if m.playing && m.flow >= 0 {
			m.stepBy(1)
			return m, m.tick()
		}
		m.playing = false
		return m, nil
	}
	return m, nil
}

// updateKey answers the keys the map answers, in the order the help pane
// lists them: quit first, then help, then the panes, then the flow keys.
// Keys are matched by their keystroke spelling, the way the other surfaces
// match them, so shift+tab and ctrl+c need no constants of their own.
func (m *Model) updateKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+c", "q":
		return tea.Quit
	case "?":
		m.toggleHelp()
		return nil
	case "esc":
		switch {
		case m.help:
			m.help = false
		case m.detail != "":
			m.openDetail("")
		case m.flow >= 0:
			m.setFlow(-1)
		default:
			m.selected = ""
		}
		return nil
	case "tab":
		if !m.help {
			m.cycleSelection(1)
		}
		return nil
	case "shift+tab":
		if !m.help {
			m.cycleSelection(-1)
		}
		return nil
	case "enter":
		if !m.help && m.selected != "" {
			if m.detail == m.selected {
				m.openDetail("")
			} else {
				m.openDetail(m.selected)
			}
		}
		return nil
	case "left", "h":
		m.stopPlay()
		m.stepBy(-1)
		return nil
	case "right", "l":
		m.stopPlay()
		m.stepBy(1)
		return nil
	case "space", "p":
		m.togglePlay()
		return nil
	case "f":
		if m.flow < 0 {
			m.setFlow(0)
		} else {
			m.setFlow((m.flow + 1) % len(m.data.Flows))
		}
		return nil
	case "0", "o":
		m.setFlow(-1)
		return nil
	}
	if t := k.Text; len(t) == 1 && t[0] >= '1' && t[0] <= '9' {
		i := int(t[0] - '1')
		if i < len(m.data.Flows) {
			if m.flow == i {
				m.setFlow(-1)
			} else {
				m.setFlow(i)
			}
		}
	}
	return nil
}

func (m *Model) updateMouseClick(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	// Draw order is hit-test priority: the panels on top win over the map.
	for i := len(m.hits) - 1; i >= 0; i-- {
		h := m.hits[i]
		if !h.rect.Contains(msg.X, msg.Y) {
			continue
		}
		switch h.kind {
		case hitTab:
			if m.flow == h.arg {
				m.setFlow(-1)
			} else {
				m.setFlow(h.arg)
			}
		case hitPrev:
			m.stopPlay()
			m.stepBy(-1)
		case hitNext:
			m.stopPlay()
			m.stepBy(1)
		}
		return
	}
	for _, n := range m.data.Nodes {
		r := m.rects[n.ID]
		if !r.Contains(msg.X, msg.Y) {
			continue
		}
		m.selected = n.ID
		m.drag = &drag{id: n.ID, offX: msg.X - r.X, offY: msg.Y - r.Y}
		return
	}
}

func (m *Model) updateMouseMotion(msg tea.MouseMotionMsg) {
	if m.drag == nil {
		return
	}
	m.moveNodeTo(m.drag.id, msg.X-m.drag.offX, msg.Y-m.drag.offY)
	m.drag.moved = true
}

func (m *Model) updateMouseRelease(msg tea.MouseReleaseMsg) {
	if m.drag == nil {
		return
	}
	d := m.drag
	m.drag = nil
	if !d.moved {
		m.openDetail(d.id)
	}
}

// ── actions ──────────────────────────────────────────────────────────────────

// openDetail opens (or with an empty id, closes) the detail pane. Opening one
// also selects its box, so tab keeps cycling from where the click left off.
func (m *Model) openDetail(id string) {
	m.detail = id
	if id != "" {
		m.selected = id
	}
}

// setFlow opens a flow, or the plain overview with -1, and rewinds it.
func (m *Model) setFlow(index int) {
	m.playing = false
	if index >= 0 && index >= len(m.data.Flows) {
		index = -1
	}
	m.flow = index
	m.step = 0
}

// stepBy moves along the open flow, wrapping at both ends.
func (m *Model) stepBy(delta int) {
	if m.flow < 0 {
		return
	}
	n := len(m.data.Flows[m.flow].Steps)
	m.step = (m.step + delta + n) % n
}

// stopPlay pauses a playing flow.
func (m *Model) stopPlay() { m.playing = false }

// togglePlay plays or pauses the open flow, opening the first one when the
// overview is showing — the same one key both starts a story and steps it.
func (m *Model) togglePlay() {
	if m.flow < 0 {
		m.setFlow(0)
	}
	if m.playing {
		m.playing = false
		return
	}
	m.playing = true
}

// tick is the command a playing flow re-arms every beat.
func (m *Model) tick() tea.Cmd {
	return tea.Tick(time.Millisecond*playMillis, func(time.Time) tea.Msg { return tickMsg{} })
}

// Beat is the one beat of a playing flow, forwarded to the model by a host
// surface that keeps the frame for it. The chat's /atlas sheet is that host:
// its own loop runs the clock, so the beat has to cross packages as a
// message type both sides can spell.
type Beat struct{}

// PlayTick is the clock a playing flow runs on. A host that keeps the frame
// arms it when a beat arrives and drops the command when the flow is over;
// standalone [Run] re-arms through Update instead.
func (m *Model) PlayTick() tea.Cmd { return m.tick() }

// Frame is the map as one frame body, for a host surface that composes its
// own screen around it. It is the same drawing [View] returns, without the
// standalone program's own alt-screen and mouse ask — the host decides both.
func (m *Model) Frame() string { return m.drawFrame() }

// toggleHelp shows or hides the legend.
func (m *Model) toggleHelp() { m.help = !m.help }

// cycleSelection moves the keyboard selection around the nodes in data order;
// an open detail pane follows the selection.
func (m *Model) cycleSelection(delta int) {
	ids := make([]string, len(m.data.Nodes))
	for i, n := range m.data.Nodes {
		ids[i] = n.ID
	}
	i := -1
	for j, id := range ids {
		if id == m.selected {
			i = j
			break
		}
	}
	m.selected = ids[(i+delta+len(ids))%len(ids)]
	if m.detail != "" {
		m.detail = m.selected
	}
}

// moveNodeTo drags a box to a new top-left corner, keeping its centre inside
// the map so the arrows can always find it.
func (m *Model) moveNodeTo(id string, x, y int) {
	r, ok := m.rects[id]
	if !ok {
		return
	}
	g := m.geometry()
	nx := clamp(x, 0, max(0, g.mapW-r.W))
	ny := clamp(y, g.mapTop, max(g.mapTop, g.mapTop+g.mapH-r.H))
	if g.mapW > r.W {
		m.pos[id] = [2]float64{float64(nx+r.W/2) / float64(g.mapW), m.pos[id][1]}
	}
	if g.mapH > r.H {
		m.pos[id] = [2]float64{m.pos[id][0], float64(ny-g.mapTop+r.H/2) / float64(g.mapH)}
	}
}

// nodeRect answers where a box was last drawn, for the tests and the
// hit-testing both.
func (m *Model) nodeRect(id string) (Rect, bool) {
	r, ok := m.rects[id]
	return r, ok
}

// activeStep is the flow step the map is telling right now, if any.
func (m *Model) activeStep() (Step, bool) {
	if m.flow < 0 {
		return Step{}, false
	}
	steps := m.data.Flows[m.flow].Steps
	if m.step >= len(steps) {
		return Step{}, false
	}
	return steps[m.step], true
}

// edgeForStep is the overview edge a step travels along: the one the step
// names, or the one that joins the same two boxes.
func (m *Model) edgeForStep(s Step) (Edge, bool) {
	if s.Edge != "" {
		for _, e := range m.data.Edges {
			if e.ID == s.Edge {
				return e, true
			}
		}
	}
	for _, e := range m.data.Edges {
		if (e.From == s.From && e.To == s.To) || (e.From == s.To && e.To == s.From) {
			return e, true
		}
	}
	return Edge{}, false
}

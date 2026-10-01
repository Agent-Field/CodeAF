package tui3

// ── THE DEVICE LIST, AND ONE KEY TO REVOKE ──────────────────────────────────
//
// `/devices` lists the fleet: `● spark  Linux` for a device with a watch
// socket open now, `○ dumb  Mac  seen 3h ago` for one without. `r` revokes the
// device under the cursor, at once and without a second question: the row is
// the choice, a revoked device is refused by the relay from that moment, and
// it can only come back as a new request that this fleet approves again.
//
// THIS DEVICE CANNOT REVOKE ITSELF HERE. Its row has no `r`, so a stray key
// never locks a person out of their own fleet; the key says so. Rows sort this
// device, online, away, then by name and id; same-name rows carry a short id.

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

const (
	devicesKeys    = "↑↓ choose · r revoke · esc close"
	devicesLoading = "looking up your devices…"
	devicesNone    = "no other device is in your fleet yet - /pair adds one"
	devicesRevoked = "%s was revoked - it can no longer reach your chats."
	devicesSelf    = "this device"
	devicesOwnRow  = "this device cannot remove itself - choose another row."
	idTail         = 4
)

// deviceCard is the list's state. Online comes from the feed when the card opens.
type deviceCard struct {
	rowsOf []DeviceRow
	loaded bool
	cursor int
	line   string
}

type devicesMsg struct {
	card *deviceCard
	rows []DeviceRow
	err  error
}

type revokedMsg struct {
	card *deviceCard
	row  DeviceRow
	err  error
}

func (m devicesMsg) land(a *app) tea.Cmd {
	if a.pair.card != panelCard(m.card) {
		return nil
	}
	if m.err != nil {
		m.card.line = m.err.Error()
		return nil
	}
	m.card.rowsOf, m.card.loaded = orderDevices(m.rows), true
	return nil
}

func (m revokedMsg) land(a *app) tea.Cmd {
	if a.pair.card != panelCard(m.card) {
		return nil
	}
	if m.err != nil {
		m.card.line = m.err.Error()
		return nil
	}
	m.card.mark(m.row.ID)
	m.card.line = fmt.Sprintf(devicesRevoked, m.row.Name)
	return nil
}

// orderDevices puts this device first, then online ones, then the rest, each
// group by name then id, so the list is the same every time it opens.
func orderDevices(rows []DeviceRow) []DeviceRow {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].before(rows[j]) })
	return rows
}

// rank is the group a row sorts in: this device 0, online 1, away 2.
func (d DeviceRow) rank() int {
	switch {
	case d.Self:
		return 0
	case d.Online:
		return 1
	}
	return 2
}

func (d DeviceRow) before(o DeviceRow) bool {
	if a, b := d.rank(), o.rank(); a != b {
		return a < b
	}
	if d.Name != o.Name {
		return d.Name < o.Name
	}
	return d.ID < o.ID
}

// shownName is the name, with a short id tail when another row has the same name.
func (c *deviceCard) shownName(d DeviceRow) string {
	for _, o := range c.rowsOf {
		if o.ID != d.ID && o.Name == d.Name {
			return d.Name + " #" + d.ID[max(len(d.ID)-idTail, 0):]
		}
	}
	return d.Name
}

func (c *deviceCard) mark(id string) {
	for i := range c.rowsOf {
		if c.rowsOf[i].ID == id {
			c.rowsOf[i].Revoked = true
		}
	}
}

func (c *deviceCard) hint() string { return devicesKeys }

// pick is the row under the cursor, if the list has one.
func (c *deviceCard) pick() (DeviceRow, bool) {
	if c.cursor < 0 || c.cursor >= len(c.rowsOf) {
		return DeviceRow{}, false
	}
	return c.rowsOf[c.cursor], true
}

func (c *deviceCard) rows(width int, now time.Time, pal palette) []string {
	if !c.loaded {
		return dressed(wrap(devicesLoading, max(width, 4)), pal.ink)
	}
	var out []string
	for i, d := range c.rowsOf {
		out = append(out, c.row(i, d, width, now, pal))
	}
	if len(c.rowsOf) <= 1 {
		out = append(out, dressed(wrap(devicesNone, max(width, 4)), pal.dim)...)
	}
	if c.line != "" {
		out = append(out, dressed(wrap(c.line, max(width, 4)), pal.accent)...)
	}
	return out
}

// row is one device: its dot, name, system and, when it is off, when it was
// last seen. The cursor row is ink; the others are dim.
func (c *deviceCard) row(i int, d DeviceRow, width int, now time.Time, pal palette) string {
	text := fmt.Sprintf("%s %s  %s", d.dot(pal), c.shownName(d), platformOf(d.Platform).word)
	for _, tail := range d.tails(now) {
		text += "  " + tail
	}
	if i == c.cursor {
		return pal.ink(fit("› "+text, width))
	}
	return pal.dim(fit("  "+text, width))
}

// dot is `●` for a device online now, `○` otherwise.
func (d DeviceRow) dot(pal palette) string {
	if d.Online || d.Self {
		return "●"
	}
	return pal.glyph(tokens.GQueued)
}

func (d DeviceRow) tails(now time.Time) []string {
	switch {
	case d.Revoked:
		return []string{"revoked"}
	case d.Self:
		return []string{devicesSelf}
	case !d.Online && !d.LastSeen.IsZero():
		return []string{"seen " + sinceAt(d.LastSeen, now) + " ago"}
	}
	return nil
}

func (c *deviceCard) key(a *app, name string) (tea.Cmd, bool) {
	move, isMove := cursorMoves[name]
	switch {
	case name == "esc":
		return nil, true
	case isMove:
		c.cursor = min(max(c.cursor+move, 0), max(len(c.rowsOf)-1, 0))
	case name == "r":
		return a.revoke(c), false
	}
	return nil, false
}

var cursorMoves = map[string]int{"up": -1, "k": -1, "down": 1, "j": 1}

// revoke ends the picked device's membership, off the loop. Its own row, a
// revoked row and an empty list do nothing.
func (a *app) revoke(c *deviceCard) tea.Cmd {
	row, ok := c.pick()
	if ok && row.Self {
		c.line = devicesOwnRow
	}
	if !ok || row.Self || row.Revoked {
		return nil
	}
	door := a.approvals
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		return revokedMsg{card: c, row: row, err: door.Revoke(ctx, row.ID)}
	}
}

// openDevices is /devices.
func (a *app) openDevices() tea.Cmd {
	if a.approvals == nil {
		a.note(pairUnavailableWord)
		return nil
	}
	card := &deviceCard{}
	a.showCard(card)
	door, online := a.approvals, a.onlineNow()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		rows, err := door.Devices(ctx)
		for i := range rows {
			rows[i].Online = online[rows[i].ID]
		}
		return devicesMsg{card: card, rows: rows, err: err}
	}
}

// onlineNow is the set of devices holding a watch socket, from the feed when
// this screen follows one.
func (a *app) onlineNow() map[string]bool {
	set := map[string]bool{}
	if a.dirFeed == nil {
		return set
	}
	for _, id := range a.dirFeed.State().Online {
		set[id] = true
	}
	return set
}

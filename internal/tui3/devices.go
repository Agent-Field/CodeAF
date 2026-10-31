package tui3

// ── THE DEVICE LIST, AND ONE KEY TO REVOKE ──────────────────────────────────
//
// `/devices` lists the fleet: `● laptop  Linux` for a device with a watch
// socket open now, `○ desktop  Mac  seen 3h ago` for one without. `r` revokes the
// device under the cursor after an explicit confirmation. The question keeps
// that row selected, and every key except y cancels without another action.
// A removed device must pair again before it can return.
//
// `n` NAMES THIS DEVICE. The name starts as the host name and is filled in, so
// enter keeps it and any typing edits it; the other devices show the new name
// from then on. Only this device's own row can be named here, because a device
// writes nothing but its own record.
//
// THIS DEVICE CANNOT REVOKE ITSELF HERE. Its row has no `r`, so a stray key
// never locks a person out of their own fleet; the key says so. Rows sort this
// device, online, away, then by name and id; same-name rows carry a short id.

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/devname"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

const (
	devicesNameKey    = "n rename"
	devicesGoneKey    = "r remove"
	devicesMoveKey    = "↑↓ choose"
	devicesCloseKey   = "esc close"
	devicesNameKeys   = "enter save · ctrl+u clear · esc cancel"
	devicesNameAsk    = "name this device: "
	devicesNameOwn    = "a device renames itself — choose this device's row, or open /devices on the one you mean."
	devicesLoading    = "looking up your devices…"
	devicesRemoveKeys = "y removes  ·  any other key keeps it"
	devicesRemoveAsk  = "remove %s? it must pair again to come back  ·  " + devicesRemoveKeys
	devicesRevoked    = "%s was removed — it can no longer reach your chats."
	devicesSelf       = "this device"
	devicesOwnRow     = "this device cannot remove itself — choose another row."
)

// deviceCard is the list's state. Online comes from the feed when the card opens.
type deviceCard struct {
	// presence reads who is online and whether the feed can say, at each draw,
	// so a socket that comes up after the card opened still lights its dots.
	presence func() (map[string]bool, bool)
	service  string
	rowsOf   []DeviceRow
	loaded   bool
	cursor   int
	line     string
	// A list read cannot undo the outcome of a completed rename or removal.
	// Keep its failure separately so both facts survive delayed feed callbacks.
	readErr error
	// naming is the name being typed, and nil when no name is being typed.
	naming *string
	// removing freezes the chosen device across asynchronous roster refreshes.
	removing *DeviceRow
	// read is the newest directory version the rows were read at, so a change another computer
	// makes (a device revoked, renamed or added) is read again once and not on every signal.
	read uint64
}

type devicesMsg struct {
	card *deviceCard
	rows []DeviceRow
	err  error
}

type renamedMsg struct {
	card *deviceCard
	said string
	err  error
}

type revokedMsg struct {
	card *deviceCard
	row  DeviceRow
	err  error
}

// openDevicesCard is the device list currently on screen, whether opened as
// /devices or hosted on the /settings sheet.
func (a *app) openDevicesCard() *deviceCard {
	if a.pair.open {
		if card, ok := a.pair.card.(*deviceCard); ok {
			return card
		}
	}
	if a.at(pageSettings) && a.sheet.dev != nil {
		return a.sheet.dev
	}
	return nil
}

// ownsDeviceCard reports whether c is still held by /devices or by the open
// /settings sheet, so a late answer from a closed view is dropped.
func (a *app) ownsDeviceCard(c *deviceCard) bool {
	return c != nil && (a.pair.card == panelCard(c) || (a.at(pageSettings) && a.sheet.dev == c))
}

func (m devicesMsg) land(a *app) tea.Cmd {
	if !a.ownsDeviceCard(m.card) {
		return nil
	}
	wasAdd := m.card.loaded && m.card.cursor == len(m.card.rowsOf)
	if a.at(pageSettings) && a.sheet.dev == m.card {
		a.sheet.syncDeviceCursor()
	}
	if m.err != nil {
		if !m.card.loaded {
			m.card.line = m.err.Error()
		} else {
			m.card.readErr = m.err
		}
		a.syncSheetDeviceCard(m.card, wasAdd)
		return nil
	}
	m.card.readErr = nil
	online, _ := m.card.livePresence()
	for i := range m.rows {
		m.rows[i].Online = online[m.rows[i].ID] && !m.rows[i].Revoked
	}
	picked, had := m.card.pick()
	m.card.rowsOf, m.card.loaded = orderDevices(m.rows), true
	m.card.keepCursorOn(picked.ID, had)
	if wasAdd {
		m.card.cursor = len(m.card.rowsOf)
	}
	a.syncSheetDeviceCard(m.card, wasAdd)
	return nil
}

// keepCursorOn puts the cursor back on the row it was on after the list was read again, because a
// re-read can reorder rows and `r` must never land on a different computer than the one chosen.
func (c *deviceCard) keepCursorOn(id string, had bool) {
	if !had {
		return
	}
	if i := slices.IndexFunc(c.rowsOf, func(d DeviceRow) bool { return d.ID == id }); i >= 0 {
		c.cursor = i
	}
}

// livePresence is the card's presence, or none when it was given no source.
func (c *deviceCard) livePresence() (map[string]bool, bool) {
	if c.presence == nil {
		return nil, false
	}
	return c.presence()
}

func (m renamedMsg) land(a *app) tea.Cmd {
	if !a.ownsDeviceCard(m.card) {
		return nil
	}
	wasAdd := a.at(pageSettings) && a.sheet.dev == m.card && a.sheet.onAddDeviceRow()
	if m.err != nil {
		m.card.line = m.err.Error()
		a.syncSheetDeviceCard(m.card, wasAdd)
		return nil
	}
	m.card.naming, m.card.line = nil, m.said
	a.syncSheetDeviceCard(m.card, wasAdd)
	return a.reloadDevices(m.card)
}

func (m revokedMsg) land(a *app) tea.Cmd {
	if !a.ownsDeviceCard(m.card) {
		return nil
	}
	wasAdd := a.at(pageSettings) && a.sheet.dev == m.card && a.sheet.onAddDeviceRow()
	if m.err != nil {
		m.card.line = m.err.Error()
		a.syncSheetDeviceCard(m.card, wasAdd)
		return nil
	}
	m.card.mark(m.row.ID)
	m.card.line = fmt.Sprintf(devicesRevoked, m.row.Name)
	a.syncSheetDeviceCard(m.card, wasAdd)
	return a.forgetDevice(m.row.ID)
}

// forgetDevice drops a revoked device from the row of who is online and asks
// the fleet and the chats again, so the card that offers another machine, the
// sessions panel and the Continue prompt stop naming it.
func (a *app) forgetDevice(id string) tea.Cmd {
	a.devRow.devices = slices.DeleteFunc(slices.Clone(a.devRow.devices), func(d chatlist.Device) bool { return d.ID == id })
	a.addMachine.known = false
	a.touch()
	return a.askMachines()
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
			return devname.DistinctName(d.Name, d.ID)
		}
	}
	return d.Name
}

func (c *deviceCard) mark(id string) {
	for i := range c.rowsOf {
		if c.rowsOf[i].ID == id {
			c.rowsOf[i].Revoked, c.rowsOf[i].Online = true, false
		}
	}
}

func (c *deviceCard) hint() string {
	if c.removing != nil {
		return devicesRemoveKeys
	}
	if c.naming != nil {
		return devicesNameKeys
	}
	return c.keys()
}

// keys names only what the row under the cursor can do: `r` is absent on this
// device's own row and on one already removed, and the arrows when there is
// no other row to walk to.
func (c *deviceCard) keys() string {
	var out []string
	if c.loaded && c.cursor == len(c.rowsOf) {
		return strings.Join([]string{devicesMoveKey, devicesAddKey, devicesCloseKey}, " · ")
	}
	if len(c.rowsOf) > 0 {
		out = append(out, devicesMoveKey)
	}
	out = append(out, devicesNameKey)
	if row, ok := c.pick(); ok && !row.Self && !row.Revoked {
		out = append(out, devicesGoneKey)
	}
	return strings.Join(append(out, devicesCloseKey), " · ")
}

func (c *deviceCard) typing() bool { return c.naming != nil }

// typed is one key while a name is being typed: text goes in, backspace and
// ctrl+u take it out, enter saves it and esc gives up the edit and nothing else.
func (c *deviceCard) typed(a *app, msg tea.KeyPressMsg) tea.Cmd {
	switch name := msg.String(); name {
	case "esc":
		c.naming, c.line = nil, ""
	case "enter":
		return a.rename(c)
	case "backspace":
		runes := []rune(*c.naming)
		*c.naming = string(runes[:max(len(runes)-1, 0)])
	case "ctrl+u":
		*c.naming = ""
	case "ctrl+k":
		// The caret is always at the end of the name, so kill-to-the-end has
		// nothing to remove; the key is answered so it is not typed as text.
	default:
		*c.naming += msg.Key().Text
	}
	return nil
}

// startNaming opens the field on this device's own row with its current name in
// it, or says why the row under the cursor cannot be named.
func (c *deviceCard) startNaming() {
	row, ok := c.pick()
	if !ok || !row.Self {
		c.line = devicesNameOwn
		return
	}
	current := row.Name
	c.naming, c.line = &current, ""
}

// rename saves the typed name off the loop.
func (a *app) rename(c *deviceCard) tea.Cmd {
	door, typed := a.approvals, *c.naming
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		said, err := door.Rename(ctx, typed)
		return renamedMsg{card: c, said: said, err: err}
	}
}

// reloadDevices asks the door for the list again, so the row shows the name the
// directory now holds.
func (a *app) reloadDevices(c *deviceCard) tea.Cmd {
	door := a.approvals
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		rows, err := door.Devices(ctx)
		return devicesMsg{card: c, rows: rows, err: err}
	}
}

// refreshOnChange reads the list again when the feed has announced a directory version the open
// card has not read, so what another computer did to a device shows here without closing the card.
func (a *app) refreshOnChange() tea.Cmd {
	card := a.openDevicesCard()
	if card == nil || a.dirFeed == nil || a.approvals == nil {
		return nil
	}
	version := a.dirFeed.State().Version
	if version <= card.read {
		return nil
	}
	card.read = version
	return a.reloadDevices(card)
}

// pick is the row under the cursor, if the list has one.
func (c *deviceCard) pick() (DeviceRow, bool) {
	if c.cursor < 0 || c.cursor >= len(c.rowsOf) {
		return DeviceRow{}, false
	}
	return c.rowsOf[c.cursor], true
}

func (c *deviceCard) rows(width int, now time.Time, pal palette) []string {
	if !c.loaded {
		return dressed(wrap(c.waiting(), max(width, 4)), pal.ink)
	}
	var out []string
	if c.service != "" {
		out = append(out, dressed(wrap(syncwords.ServiceLine(c.service), max(width, 4)), pal.dim)...)
	}
	for i, d := range c.rowsOf {
		out = append(out, c.row(i, d, width, now, pal))
	}
	add := "  " + addDeviceWord
	if c.cursor == len(c.rowsOf) {
		add = pal.glyph(tokens.GPointer) + " " + addDeviceWord
	}
	out = append(out, pal.ink(fit(add, width)))
	if c.naming != nil {
		out = append(out, pal.ink(fit(devicesNameAsk+*c.naming+"▏", width)))
	}
	if c.line != "" {
		out = append(out, dressed(wrap(c.line, max(width, 4)), pal.accent)...)
	}
	if c.readErr != nil {
		out = append(out, dressed(wrap(c.readErr.Error(), max(width, 4)), pal.accent)...)
	}
	return out
}

// waiting is what the card says before it has a list: that it is looking, or,
// once the look has failed, why. A failure that landed on a card still saying
// "looking up" would never be read.
func (c *deviceCard) waiting() string {
	if c.readErr != nil {
		return c.readErr.Error()
	}
	if c.line != "" {
		return c.line
	}
	return devicesLoading
}

// rowText is one device's words without the card's own cursor gutter: its dot,
// name, system and, when it is off, when it was last seen. The card frames it
// with `› `, and the /settings Devices tab hands the same string to the sheet's
// row painter so neither list can drift from the other.
func (c *deviceCard) rowText(d DeviceRow, now time.Time, pal palette) string {
	online, _ := c.livePresence()
	d.Online = online[d.ID] && !d.Revoked
	nameW, wordW := c.columns()
	text := d.dot(pal) + " " + padTo(c.shownName(d), nameW) + "  " + padTo(platformOf(d.Platform).word, wordW)
	for _, tail := range d.tails(now) {
		text += "  " + tail
	}
	return text
}

// row is one device: its dot, name, system and, when it is off, when it was
// last seen. The cursor row is ink; the others are dim.
func (c *deviceCard) row(i int, d DeviceRow, width int, now time.Time, pal palette) string {
	text := c.rowText(d, now, pal)
	if i == c.cursor {
		return pal.ink(fit(pal.glyph(tokens.GPointer)+" "+text, width))
	}
	return pal.dim(fit("  "+text, width))
}

// columns are the widths of the name and system columns, so the marks, names,
// systems and tails of every row start in the same cells.
func (c *deviceCard) columns() (nameW, wordW int) {
	for _, d := range c.rowsOf {
		nameW = max(nameW, ansi.StringWidth(c.shownName(d)))
		wordW = max(wordW, ansi.StringWidth(platformOf(d.Platform).word))
	}
	return nameW, wordW
}

// dot is the filled circle for a device online now, and the resting circle
// otherwise, both resolved through the palette's glyph door so ASCII and Nerd
// Font terminals render their own repertoire.
func (d DeviceRow) dot(pal palette) string {
	if !d.Revoked && (d.Online || d.Self) {
		return pal.glyph(tokens.GStepDone)
	}
	return pal.glyph(tokens.GQueued)
}

func (d DeviceRow) tails(now time.Time) []string {
	switch {
	case d.Revoked:
		return []string{"removed"}
	case d.Self:
		return []string{devicesSelf}
	case d.Online:
		return nil
	case d.LastSeen.IsZero():
		return []string{"offline"}
	}
	return []string{seenTail(d.LastSeen, now)}
}

// seenTail is when an away device was last seen, in words that read whole:
// `seen just now`, `seen 3h ago`, `seen 2 Jan`.
func seenTail(at, now time.Time) string {
	ago := sinceAt(at, now)
	switch {
	case ago == "now":
		return "seen just now"
	case strings.HasSuffix(ago, "m"), strings.HasSuffix(ago, "h"), strings.HasSuffix(ago, "d"):
		return "seen " + ago + " ago"
	}
	return "seen " + ago
}

func (c *deviceCard) key(a *app, name string) (tea.Cmd, bool) {
	if c.removing != nil {
		row := *c.removing
		c.removing, c.line = nil, ""
		c.keepCursorOn(row.ID, true)
		if name == "y" {
			return a.removeDevice(c, row), false
		}
		return nil, false
	}
	move, isMove := cursorMoves[name]
	switch {
	case name == "esc":
		return nil, true
	case isMove:
		c.cursor = min(max(c.cursor+move, 0), len(c.rowsOf))
	case name == "enter" && c.loaded && c.cursor == len(c.rowsOf):
		a.pair.close()
		return a.runPair(""), false
	case name == "n":
		c.startNaming()
	case name == "r":
		return a.revoke(c), false
	}
	return nil, false
}

var cursorMoves = map[string]int{"up": -1, "k": -1, "down": 1, "j": 1}

// revoke asks about the picked device before any membership changes. Its own
// row, a revoked row and an empty list cannot open that question.
func (a *app) revoke(c *deviceCard) tea.Cmd {
	row, ok := c.pick()
	if ok && row.Self {
		c.line = devicesOwnRow
	}
	if !ok || row.Self || row.Revoked {
		return nil
	}
	c.removing = &row
	c.line = fmt.Sprintf(devicesRemoveAsk, c.shownName(row))
	return nil
}

// removeDevice writes only the row the person confirmed, even if a refresh moved it.
func (a *app) removeDevice(c *deviceCard, row DeviceRow) tea.Cmd {
	door := a.approvals
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		return revokedMsg{card: c, row: row, err: door.Revoke(ctx, row.ID)}
	}
}

// openDevices is /devices.
func (a *app) openDevices() tea.Cmd {
	if a.sayCellsUnavailable() {
		return nil
	}
	if a.approvals == nil {
		a.note(pairUnavailableWord)
		return nil
	}
	card := a.newDeviceCard()
	a.showCard(card)
	door := a.approvals
	return tea.Batch(a.joinWatchIfAbsent(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), approveTimeout)
		defer cancel()
		rows, err := door.Devices(ctx)
		return devicesMsg{card: card, rows: rows, err: err}
	})
}

// joinWatchIfAbsent gives the card the change feed on any screen, not only
// from home, so presence is the feed's wherever /devices was opened.
func (a *app) joinWatchIfAbsent() tea.Cmd {
	if a.dirFeed != nil || a.machines == nil {
		return nil
	}
	return a.joinWatch()
}

// newDeviceCard binds both device lists to the same service and presence source.
func (a *app) newDeviceCard() *deviceCard {
	address := a.syncService
	if door, ok := a.approvals.(interface{ SyncService() string }); ok {
		address = door.SyncService()
	}
	service := syncsetup.Relay{URL: address, Off: strings.EqualFold(address, "off")}
	if !service.On() {
		address = ""
	}
	return &deviceCard{presence: a.presence, service: address}
}

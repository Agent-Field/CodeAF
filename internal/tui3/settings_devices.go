package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
)

// ── THE DEVICES TAB ON THE SETTINGS SHEET ───────────────────────────────────
//
// `/devices` is the fleet asked in a hurry; this is the same list at rest on the
// settings sheet, right after Connections, plus one door that starts `/pair`.
//
// ONE SOURCE OF TRUTH. The tab holds a [deviceCard] (devices.go) rather than a
// second device slice or a second set of strings: the same [Approvals] door, the
// same [orderDevices] sort, the same row formatting ([deviceCard.rowText]), the
// same `n` rename editor ([deviceCard.startNaming], [deviceCard.typed],
// [app.rename]), the same `r` removal ([app.revoke]), the same directory-version
// refresh ([app.refreshOnChange]), and the same unavailability sentences
// ([app.openDevices]). A sentence spelled in two places drifts.

const (
	// tabDevices is the category title on the settings bar, placed between
	// Connections and Privacy.
	tabDevices = "Devices"
	// devicesAddKey is the key legend clause for the row that starts pairing
	// another computer.
	devicesAddKey = "enter add"
)

// deviceSheetRow hangs one Devices-tab entry off [sheetItem] so the sheet's own
// cursor walk, scroll window, hover and pointer hit-testing work unchanged.
type deviceSheetRow struct {
	index       int
	id          string
	add         bool
	unavailable bool
}

// onDevices reports whether the Devices category is the active tab.
func (s *sheet) onDevices() bool {
	return s.tab >= 0 && s.tab < len(settingTabs) && settingTabs[s.tab] == tabDevices
}

// devTyping reports whether the inline rename field on this device's row currently
// owns the keyboard.
func (s *sheet) devTyping() bool {
	return s.onDevices() && s.dev != nil && s.dev.typing()
}

// onAddDeviceRow reports whether the sheet cursor is resting on the add-device
// door row.
func (s *sheet) onAddDeviceRow() bool {
	item, ok := s.current()
	return ok && item.dev != nil && item.dev.add
}

// devicesUnavailable answers why devices cannot work in this session, in the
// exact order and wording a typed `/devices` uses ([app.openDevices]), or ""
// when the door is ready to ask.
func (s *sheet) devicesUnavailable() string {
	if s.cellsUnavailable != "" {
		return s.cellsUnavailable
	}
	if s.approvals == nil {
		return pairUnavailableWord
	}
	return ""
}

// devNow reads the surface clock handed to the sheet so relative `seen … ago`
// timestamps stay deterministic under test clocks.
func (s *sheet) devNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return timeNow()
}

// buildDevices populates the Devices tab when no search query is active. When
// devices cannot work here, or while the initial directory read is still in
// flight or failed, no rows are emitted — [sheet.listLines] draws the single
// explanatory sentence instead, never an empty list and never a working-looking
// add row.
func (s *sheet) buildDevices() {
	if s.devicesUnavailable() != "" || s.dev == nil || !s.dev.loaded {
		s.cursor = 0
		return
	}
	for i, d := range s.dev.rowsOf {
		s.items = append(s.items, sheetItem{
			dev: &deviceSheetRow{index: i, id: d.ID},
		})
	}
	s.items = append(s.items, sheetItem{
		dev: &deviceSheetRow{add: true},
	})
	s.cursor = s.clampCursor(s.cursor)
	s.syncDeviceCursor()
}

// devEmptyWord is the single sentence drawn when the Devices tab has no rows to
// offer: the unavailability reason, the loading line, or the card's own read
// failure ([deviceCard.waiting]).
func (s *sheet) devEmptyWord() string {
	if why := s.devicesUnavailable(); why != "" {
		return why
	}
	if s.dev == nil {
		return devicesLoading
	}
	return s.dev.waiting()
}

// syncDeviceCursor keeps the hosted [deviceCard]'s cursor aligned with the
// sheet's cursor so `r`, `n` and re-reads act on the row the person selected.
// Standing on the add row parks the card's cursor past its slice so
// [deviceCard.pick] reports no device row under it.
func (s *sheet) syncDeviceCursor() {
	if s.dev == nil {
		return
	}
	item, ok := s.current()
	if !ok || item.dev == nil || item.dev.add || item.dev.unavailable {
		s.dev.cursor = len(s.dev.rowsOf)
		return
	}
	s.dev.cursor = item.dev.index
}

// ensureSheetDevices starts the initial directory read and joins the change
// watch the first time a visit lands on the Devices tab. Other categories never
// pay a network ask they did not look at.
func (a *app) ensureSheetDevices() tea.Cmd {
	if !a.at(pageSettings) || !a.sheet.onDevices() {
		return nil
	}
	if a.sheet.devicesUnavailable() != "" || a.sheet.dev != nil {
		return nil
	}
	card := &deviceCard{presence: a.presence}
	a.sheet.dev = card
	if !a.sheet.searching() {
		a.sheet.build()
	}
	return tea.Batch(a.joinWatchIfAbsent(), a.reloadDevices(card))
}

// syncSheetDeviceCard rebuilds the sheet after an asynchronous device read,
// rename or removal lands on its hosted [deviceCard], keeping the cursor on the
// same computer (or on the add row) across re-sorts.
func (a *app) syncSheetDeviceCard(c *deviceCard, wasAdd bool) {
	if !a.at(pageSettings) || a.sheet.dev != c {
		return
	}
	s := &a.sheet
	if s.searching() {
		s.build()
		a.touch()
		return
	}
	picked, had := c.pick()
	s.build()
	switch {
	case wasAdd:
		for i, item := range s.items {
			if item.dev != nil && item.dev.add {
				s.cursor = i
				break
			}
		}
	case had:
		for i, item := range s.items {
			if item.dev != nil && !item.dev.add && item.dev.id == picked.ID {
				s.cursor = i
				break
			}
		}
	}
	s.syncDeviceCursor()
	a.touch()
}

// addDeviceSearchWords and unavailableDevicesSearchWords let global `/settings`
// search find the Devices tab by any of the words people ask with — `devices`,
// `remove`, `revoke`, `pair`, `add device` — even before the tab has been opened
// on this visit.
var addDeviceSearchWords = []string{
	addDeviceWord,
	tabDevices,
	"devices",
	"remove",
	"revoke",
	"pair",
	"rename",
	"computer",
}

var unavailableDevicesSearchWords = []string{
	tabDevices,
	"devices",
	"remove",
	"revoke",
	"pair",
	addDeviceWord,
	"rename",
	"computer",
}

// appendDevicesSearch scores the Devices tab against a standing search query and
// appends its heading and matching rows. When pairing cannot work here, a match
// surfaces the unavailability sentence rather than a working-looking add row.
func (s *sheet) appendDevicesSearch(terms []fuzzy.Term) int {
	if len(terms) == 0 {
		return 0
	}
	if s.devicesUnavailable() != "" {
		score, ok := fuzzy.ScoreFields(unavailableDevicesSearchWords, terms)
		if !ok {
			return 0
		}
		s.items = append(s.items,
			sheetItem{head: tabDevices},
			sheetItem{dev: &deviceSheetRow{unavailable: true}},
		)
		return score
	}
	type devHit struct {
		item  sheetItem
		score int
	}
	var matched []devHit
	best := 0
	if s.dev != nil && s.dev.loaded {
		for i, d := range s.dev.rowsOf {
			fields := []string{
				s.dev.shownName(d),
				platformOf(d.Platform).word,
				tabDevices,
				"devices",
				"remove",
				"revoke",
				"rename",
				"computer",
			}
			score, ok := fuzzy.ScoreFields(fields, terms)
			if !ok {
				continue
			}
			best = max(best, score)
			matched = append(matched, devHit{
				item:  sheetItem{dev: &deviceSheetRow{index: i, id: d.ID}},
				score: score,
			})
		}
	}
	if score, ok, hitAt, hitLen := s.matchHits(addDeviceSearchWords, terms); ok {
		best = max(best, score)
		matched = append(matched, devHit{
			item:  sheetItem{dev: &deviceSheetRow{add: true}, hitAt: hitAt, hitLen: hitLen},
			score: score,
		})
	}
	if len(matched) == 0 {
		return 0
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].score > matched[j].score })
	s.items = append(s.items, sheetItem{head: tabDevices})
	for _, one := range matched {
		s.items = append(s.items, one.item)
	}
	return best
}

// devRowLines renders one row of the Devices tab through the sheet's standard
// [overlayLines] grammar while delegating every word and mark of a device row
// to [deviceCard.rowText]. When this device is being renamed, the card's own
// prompt hangs directly beneath its row.
func (s *sheet) devRowLines(row *deviceSheetRow, hit []int, selected, hovered bool, width int, pal palette) []string {
	if row.unavailable {
		return overlayLines(s.devicesUnavailable(), "", selected, false, hovered, width, pal)
	}
	if row.add {
		return overlayLinesHit(addDeviceWord, "", hit, selected, false, hovered, width, pal)
	}
	if s.dev == nil || row.index < 0 || row.index >= len(s.dev.rowsOf) {
		return nil
	}
	d := s.dev.rowsOf[row.index]
	head := overlayLines(s.dev.rowText(d, s.devNow(), pal), "", selected, false, hovered, width, pal)
	if d.Self && s.devTyping() {
		line, _ := s.devNamingLine(width, pal)
		return append(head, line)
	}
	return head
}

// devNamingLine paints the inline rename prompt under this device's row and
// reports the terminal column immediately after the typed name so
// [placeSettings.caretRow] can park the hardware cursor there.
func (s *sheet) devNamingLine(width int, pal palette) (string, int) {
	if s.dev == nil || s.dev.naming == nil {
		return "", 0
	}
	room := max(1, width-overlayIndent)
	shown := fit(devicesNameAsk+*s.dev.naming, room)
	return strings.Repeat(" ", overlayIndent) + pal.ink(shown), overlayIndent + ansi.StringWidth(shown)
}

// devicesNote supplies the foot-rule caption for the Devices tab. The tab
// writes to the directory rather than `.codeaf/config.json`, so at rest it says
// nothing instead of claiming profile writes; after a rename, removal or refusal
// that fits the rule, it shows the card's own sentence on the foot line. A
// sentence wider than the rule wraps whole in [settingsDetails] above it rather
// than being cut in half or repeated twice on one frame.
func (s *sheet) devicesNote(pal palette, width int) ([]string, bool) {
	if !s.onDevices() || s.searching() {
		return nil, false
	}
	if s.devicesUnavailable() != "" || s.dev == nil || !s.dev.loaded {
		return nil, true
	}
	room := max(1, width-2)
	if s.dev.line != "" && ansi.StringWidth(s.dev.line) <= room {
		return []string{" " + pal.dim(noteFit(s.dev.line, room))}, true
	}
	if s.dev.line == "" && s.dev.readErr != nil && ansi.StringWidth(s.dev.readErr.Error()) <= room {
		return []string{" " + pal.bad(noteFit(s.dev.readErr.Error(), room))}, true
	}
	return nil, true
}

// devicesDetails returns any card message that did not fit on the single-line
// foot rule (or a concurrent refresh error standing alongside a completed
// removal, so neither fact erases the other).
func (s *sheet) devicesDetails(frameWidth, bodyWidth int) []string {
	if !s.onDevices() || s.searching() || s.devicesUnavailable() != "" || s.dev == nil || !s.dev.loaded {
		return nil
	}
	ruleRoom := max(1, frameWidth-placeNoteRuleFrame)
	wrapRoom := max(1, bodyWidth-4)
	var lines []string
	if s.dev.line != "" && ansi.StringWidth(s.dev.line) > ruleRoom {
		lines = append(lines, wrap(s.dev.line, wrapRoom)...)
	}
	if s.dev.readErr != nil && (s.dev.line != "" || ansi.StringWidth(s.dev.readErr.Error()) > ruleRoom) {
		lines = append(lines, wrap(s.dev.readErr.Error(), wrapRoom)...)
	}
	return lines
}

// devicesKeysLine builds the key legend for the Devices tab from [deviceCard]'s
// own key constants, shrinking dynamically to the gestures that work on the row
// under the cursor: `r remove` only on a removable computer, `↑↓ choose` only
// when more than one row can be walked, and `enter add` on the add row.
func (s *sheet) devicesKeysLine() string {
	if s.devTyping() {
		return s.dev.hint()
	}
	var out []string
	restful := 0
	for _, item := range s.items {
		if item.restful() {
			restful++
		}
	}
	if restful > 1 {
		out = append(out, devicesMoveKey)
	}
	if item, ok := s.current(); ok && item.dev != nil && !item.dev.unavailable {
		if item.dev.add {
			out = append(out, devicesAddKey)
		} else {
			out = append(out, devicesNameKey)
			if s.dev != nil && item.dev.index >= 0 && item.dev.index < len(s.dev.rowsOf) {
				if row := s.dev.rowsOf[item.dev.index]; !row.Self && !row.Revoked {
					out = append(out, devicesGoneKey)
				}
			}
		}
	}
	out = append(out, "←→ categories", devicesCloseKey)
	return strings.Join(out, " · ")
}

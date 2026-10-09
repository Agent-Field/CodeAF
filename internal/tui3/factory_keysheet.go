package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE `?` SHEET: EVERY KEY, WITH ITS WORD ─────────────────────────────────
//
// `?` on the floor or on the item page puts up the whole keyboard, grouped
// `do / set / also / move`, each key beside the word that says what it does
// (factory_words.go). It is the answer to the strips being short: the peek
// names at most five verbs and the bottom bar only the way around, so every
// other key needs one place a person can find it.
//
// IT IS THE CREW PANEL'S KEYS VIEW AT THE SCALE OF A PLACE (crewpanel.go's
// crewKeys): a view the page's body swaps in, standing over everything the way
// the repo picker and the recipe page already do ([app.factoryBody]), drawn as
// rows of an ink key and a dim word, and put away with `esc` (or `?` again).
// It is not a new overlay: the frame, the foot and the hint are the place's.
//
// THE SHEET NAMES ONLY KEYS THAT WORK where it was opened: the item's group
// is the item under the cursor's, for its state, on this seam's doors, the
// same predicates the keys themselves ask. A group with nothing in it is not
// drawn (the emptiness law).

// factorySheetRow is one key and its word.
type factorySheetRow struct{ key, word string }

// factorySheetGroup is one titled group of the sheet.
type factorySheetGroup struct {
	name string
	rows []factorySheetRow
}

// factorySheet is the sheet's groups for where the person stands.
func (a *app) factorySheet() []factorySheetGroup {
	seam := a.factory
	var do, set, also, move []factorySheetRow
	row := func(list *[]factorySheetRow, ok bool, key, word string) {
		if ok {
			*list = append(*list, factorySheetRow{key, word})
		}
	}
	it, ok := a.factoryCursorItem()
	// ON THE ITEM PAGE `space` IS THE TOP BAR'S CONTROL (factory_bar.go), and
	// the sheet names it first, by what it does now; select and pause are the
	// floor's.
	page := ok && a.fp.open
	if page {
		if c := a.factoryControlOf(it); c != factoryControlNone {
			_, word, _ := strings.Cut(ansi.Strip(a.factoryControlLabel(c)), " ")
			row(&do, true, keyControl, word)
		}
	}
	if ok {
		chat := seam.Has("talk")
		switch it.State {
		case factory.StateNew, factory.StateDismissed:
			row(&do, a.factoryCanRun(), keyRun, wordRun)
			row(&do, chat, keyChat, wordChat)
			row(&do, it.State == factory.StateNew && !page, keySelect, wordSelect)
			row(&do, seam.Has("launch"), keyRunSelected, wordRunSelected)
			row(&do, seam.Has("askauthor") && len(nonEmpty(it.Triage.Questions)) > 0, keyAskAuthor, wordAskAuthor)
			row(&set, seam.Has("setcap"), keyBudget, wordBudget)
			row(&set, seam.Has("seteffort"), keyThinking, wordThinking)
			row(&set, seam.Has("setstage") && len(factoryStages(a.fp.snap, it)) > 0, keyStages, wordStages)
			row(&set, seam.Has("addstage"), keyAddStage, wordAddStage)
			row(&set, seam.Has("steer") || seam.Has("setcap") || seam.Has("seteffort"), keyInWordsSet, wordInWordsSet)
			row(&set, seam.Has("bankstages"), keySaveRecipe, wordSaveRecipe)
			row(&also, seam.Has("sync") && it.Origin == factory.OriginTerminal, keySync, wordSync)
			row(&also, seam.Has("dismiss"), keyDismiss, wordDismiss)
		case factory.StateQueued, factory.StateRunning:
			row(&do, seam.Has("stop"), keyStop, wordStop)
			row(&do, chat, keyChat, wordChat)
			row(&do, seam.Has("pause") && it.State == factory.StateRunning && !page, keyPause, wordPause+" or "+wordResume)
			row(&do, a.factorySteerable(it), keySteer, wordSteer)
			row(&set, seam.Has("seteffort"), keyThinking, wordThinking)
		case factory.StateNeedsYou:
			yes, no := factoryYesNo(it)
			row(&do, seam.Has("answer"), keyYes, yes)
			row(&do, seam.Has("answer"), keyNo, no)
			row(&do, seam.Has("answer"), keyInWords, wordInWords)
			row(&do, chat, keyChat, wordChat)
			row(&do, a.factorySteerable(it), keySteer, wordSteer)
			row(&do, seam.Has("stop"), keyStop, wordStop)
		case factory.StateLanded:
			clean := factoryFirstFailed(it) == ""
			row(&do, clean && seam.Has("signoff"), keyApprove, wordApprove)
			row(&do, !clean && seam.Has("signoff"), keyApproveWithChanges, wordApproveWithChanges)
			row(&do, seam.Has("sendback"), keyRequestChanges, wordRequestChanges)
			row(&do, seam.Has("reverify"), keyRerunChecks, wordRerunChecks)
			row(&do, chat, keyChat, wordChat)
			row(&also, it.Diff != "", keyDiff, wordDiff)
		default:
			row(&do, chat, keyChat, wordChat)
		}
		row(&also, it.URL != "" && it.Origin != factory.OriginTerminal && seam.Has("open"), keyOpenGitHub, wordOpenGitHub)
		row(&also, seam.Has("refresh"), keyRefresh, wordRefresh)
		row(&also, a.factoryShapeable(it), keyShapeSteps, wordShapeSteps)
	}
	floor := !a.fp.open
	connected := a.factoryConnected()
	row(&also, floor && connected && seam.Has("new") && (!ok || it.State != factory.StateNeedsYou), keyNew, wordNew)
	row(&also, floor && seam.Has("foreman"), keyForeman, wordForeman)
	row(&also, floor && seam.Has("refreshall") && a.factoryFloorHas(), keyRefreshAll, wordRefreshAll)
	row(&also, floor && a.fp.columns, keyHandover, wordHandover)
	row(&also, floor && seam.Has("repos") && seam.Has("setrepos"), keyRepos, wordRepos)
	row(&also, floor && len(a.fp.snap.Repos) > 0, keyRecipe, wordRecipe)
	row(&also, floor && seam.Has("setrail"), keyRail, wordRail)
	row(&also, floor && a.factoryFloorHas(), keyBacklog, wordBacklog)
	row(&also, floor && a.factoryFloorHas(), keyDensity, wordDensity)
	row(&also, floor && a.factoryFloorHas(), keyOrder, wordOrder+rowSep+a.fp.order.word())
	row(&also, floor && len(a.fp.snap.Repos) > 1, keyRepoWalk, wordRepoWalk)
	row(&also, floor && ok, keySplit, wordSplit)
	row(&also, ok, keyScroll, wordScroll)
	// ON THE ITEM PAGE THE KNOBS ARE THE SETTINGS ROW'S (factory_item.go),
	// and the sheet says where they stand.
	row(&set, a.fp.open && ok, keyWalk, wordFacetSettings)
	if a.fp.open {
		row(&move, true, keyWalk, wordRows)
		row(&move, true, keyBack, wordFloorName)
	} else {
		row(&move, a.factoryFloorHas(), keyWalk, wordWalk)
		row(&move, ok, keyOpen, wordOpen)
		row(&move, a.factoryFloorHas(), keyFilter, wordFilter)
		row(&move, true, keyBack, wordBack)
	}
	row(&move, true, "tab", "next place")
	row(&move, true, keySheet, wordSheet)
	var out []factorySheetGroup
	for _, g := range []factorySheetGroup{{wordGroupDo, do}, {wordGroupSet, set}, {wordGroupAlso, also}, {wordGroupMove, move}} {
		if len(g.rows) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// factorySheetLines is the sheet as lines of at most measure cells: each
// group a muted name over its rows, the groups laid in as many columns as
// the width holds, left to right, and stacked under one another past that.
func (a *app) factorySheetLines(measure int) []string {
	pal := a.pal
	var blocks [][]string
	colW := min(factorySheetColW, max(measure, 1))
	for _, g := range a.factorySheet() {
		lines := []string{pal.muted(g.name)}
		for _, r := range g.rows {
			key := r.key + factorySpaces(max(1, factorySheetKeyW-ansi.StringWidth(r.key)))
			lines = append(lines, fit(pal.ink(key)+pal.dim(r.word), colW))
		}
		blocks = append(blocks, lines)
	}
	cols := max(1, min(len(blocks), (measure+factorySheetColGap)/(colW+factorySheetColGap)))
	var out []string
	for start := 0; start < len(blocks); start += cols {
		band := blocks[start:min(start+cols, len(blocks))]
		tall := 0
		for _, b := range band {
			tall = max(tall, len(b))
		}
		if start > 0 {
			out = append(out, "")
		}
		for i := 0; i < tall; i++ {
			var line strings.Builder
			for j, b := range band {
				cell := ""
				if i < len(b) {
					cell = b[i]
				}
				if j < len(band)-1 {
					cell += factorySpaces(max(0, colW+factorySheetColGap-ansi.StringWidth(cell)))
				}
				line.WriteString(cell)
			}
			out = append(out, strings.TrimRight(line.String(), " "))
		}
	}
	return out
}

// factorySheetBody is the sheet as the page's body: room rows of width
// cells, with the floor's margin, cut from its foot when the room is short.
func (a *app) factorySheetBody(width, room int) []placeRow {
	measure := max(width-factoryMargins, 0)
	lines := a.factorySheetLines(measure)
	rows := make([]placeRow, room)
	for i := range rows {
		text := ""
		if i < len(lines) && lines[i] != "" {
			text = factoryMarginPad() + lines[i]
		}
		rows[i] = placeRow{text: factoryPad(text, width), hit: -1}
	}
	return rows
}

// factorySheetKey is `?` and the sheet's own keys. `?` opens it wherever no
// box, typing row, question or settings page has the keyboard; while it
// stands, `esc`, `?` and `q` put it away, the router's walk between places
// and its alt chords put it away and go on, and every other key is held, so
// a letter meant to be read is not a verb pressed on the row underneath.
func (a *app) factorySheetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if !a.fp.keys {
		if k != keySheet || !a.factoryConnected() || a.fp.typing || a.fp.pick != nil || a.fp.recipe != nil ||
			a.fp.ghOffer != "" || a.fp.act.ask != nil || a.fp.act.refresh != nil || a.fp.act.launch != nil {
			return nil, false
		}
		a.fp.keys = true
		a.touch()
		return nil, true
	}
	switch {
	case k == keyBack || k == keySheet || k == "q":
		a.fp.keys = false
		a.touch()
		return nil, true
	case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
		a.fp.keys = false
		a.touch()
		return nil, false
	}
	return nil, true
}

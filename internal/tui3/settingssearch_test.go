package tui3

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// THE SETTINGS SEARCH, against the real sheet. The matcher every picker on
// this surface ranks with (internal/fuzzy) answers here over the five fields a
// row carries, and these tests hold the half of that promise a settings panel
// specifically makes: a row is findable by what it says about itself, by the
// value it holds, and by a phrase with a space in it — and a space typed
// mid-search is a word separator in the query and not a press on the row
// under the cursor.

// typeQuery types into the search box the way a person does, key by key.
func typeQuery(t *testing.T, a *app, query string) {
	t.Helper()
	for _, r := range query {
		drive(t, a, key(string(r)))
	}
}

// searchLabels is the sheet under a query: every row's label, headings aside.
// A standing search is cleared with ctrl+u rather than esc, because esc with
// the box already empty closes the whole panel.
func searchLabels(t *testing.T, a *app, query string) []string {
	t.Helper()
	drive(t, a, key("ctrl+u"))
	typeQuery(t, a, query)
	return sheetLabels(a)
}

// A ROW IS FOUND BY ITS ABOUT LINE. The one-line explanation is the field half
// this sheet's rows answer to and no label carries: "dangerous" is a word
// about what the approval gate does, and the row that holds it must come with
// it.
func TestTheSettingsSearchFindsARowByItsAboutLine(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	labels := searchLabels(t, a, "dangerous")
	if !screenHas(labels, "ask before running") {
		t.Fatalf("\"dangerous\" did not find the approval gate:\n%s", strings.Join(labels, "\n"))
	}
}

// searchItems is the sheet's built list under a query: headings and rows in
// the order the panel would draw them — above the window or below it, which a
// long result list puts the late tabs beneath.
func searchItems(t *testing.T, a *app, query string) []sheetItem {
	t.Helper()
	drive(t, a, key("ctrl+u"))
	typeQuery(t, a, query)
	return a.sheet.items
}

// itemAt is one registry key's place in a built list, or -1. Only a row the
// search kept counts, headings aside.
func itemAt(items []sheetItem, key string) int {
	for i, item := range items {
		if item.restful() && item.row.Key == key {
			return i
		}
	}
	return -1
}

// A ROW IS FOUND BY THE VALUE IT HOLDS, and by nothing else on it. The gate
// cycled to `allow` is findable as `allow` even though neither its label nor
// its key nor its about line carries the word — a person who knows what the
// panel does and not what it is called searches the way this test does.
func TestTheSettingsSearchFindsARowByItsCurrentValue(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	// This search fixture exercises an explicitly saved prompt posture.
	cursorTo(t, a, config.KeyToolApprovalMode)
	drive(t, a, key("enter")) // allow -> deny
	drive(t, a, key("enter")) // deny -> prompt
	if got := config.ToolApprovalModeAt(dir); got != "prompt" {
		t.Fatalf("the gate did not start at prompt: %q", got)
	}
	// `prompt` is the probe: the gate's own label, key and about line carry no
	// `p` at all, so the word cannot reach the row through any of them — not
	// even as a scattered subsequence, which is the reach a fuzzy matcher
	// always has — and only the value it holds can carry it.
	if at := itemAt(searchItems(t, a, "prompt"), config.KeyToolApprovalMode); at < 0 {
		t.Fatal("\"prompt\" did not find the gate holding it")
	}
	// Cycle the gate away from prompt, and the same word loses the row.
	cursorTo(t, a, config.KeyToolApprovalMode)
	drive(t, a, key("enter"))
	if got := config.ToolApprovalModeAt(dir); got != "allow" {
		t.Fatalf("the cycle wrote %q, want allow", got)
	}
	if at := itemAt(searchItems(t, a, "prompt"), config.KeyToolApprovalMode); at >= 0 {
		t.Fatal("\"prompt\" still found the gate after the gate stopped carrying it")
	}
}

// screenHas is one row's presence on the drawn sheet: the lines are the
// screen's own — a row's label and the value beside it on one line — so a
// presence is a containment and not an equality.
func screenHas(lines []string, want string) bool {
	return screenAt(lines, want) >= 0
}

// screenAt is the first line carrying the wanted text, or -1. First is the
// ranking: a row drawn above another is a row that answered better.
func screenAt(lines []string, want string) int {
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

// THE MATCHED LETTERS CARRY THE EMPHASIS, AND NOTHING ELSE ON THE ROW DOES.
// A search that answered should say WHICH letters answered: the bytes the
// query matched draw in bold over the row's own ink — weight is the one
// emphasis this surface already owns, the user/assistant distinction's —
// while the rest of the label keeps the ink it had. No new colour, no
// ground: it is a reading aid for the scan, which is what "subtle" asked
// for, and not a louder row.
func TestTheSearchCarriesTheMatchedLettersInBold(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	// This search fixture exercises an explicitly saved prompt posture.
	cursorTo(t, a, config.KeyToolApprovalMode)
	drive(t, a, key("enter")) // allow -> deny
	drive(t, a, key("enter")) // deny -> prompt
	typeQuery(t, a, "ask")
	// The approval row, kept by the search, and where the query landed on it:
	// the label's own head, because "ask" is the word the row's name begins
	// with — and not its middle, and not a whole word either.
	at := -1
	for i, item := range a.sheet.items {
		if item.restful() && item.row.Key == config.KeyToolApprovalMode {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("\"ask\" did not keep the approval row:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
	// Walk the cursor off the row: the cursor's row is bold whole already, and
	// this test is about the resting rows a person scans down.
	for guard := 0; guard < len(a.sheet.items) && at == a.sheet.cursor; guard++ {
		drive(t, a, key("down"))
	}
	if at == a.sheet.cursor {
		t.Fatal("the cursor never left the approval row")
	}
	hit := a.sheet.itemHit(a.sheet.items[at])
	if len(hit) != 3 || hit[0] != 0 || hit[1] != 1 || hit[2] != 2 {
		t.Fatalf("the approval row's label hit is %v; want its first three bytes, 0 1 2", hit)
	}
	lines, _ := a.sheet.listLines(a.width, a.height, a.pal, -1)
	// THE EMPHASIS IS EXACTLY THE MATCHED BYTES: bold over the row's own dim
	// for "ask", plain dim after it — the whole label spelled out, so a bold
	// run that bled past the match, or one that skipped a matched byte, does
	// not contain this string.
	want := a.pal.bold(a.pal.dim("ask")) + a.pal.dim(" before running")
	found := false
	for _, line := range lines {
		if strings.Contains(line, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the approval row did not draw its matched letters in bold; want %q among:\n%s",
			want, strings.Join(lines, "\n"))
	}
	// AND THE MATCH IS ON THE FIELD IT WON AND NOWHERE ELSE: "prompt" is
	// the approval row's own VALUE — the word the gate carries — and a query
	// of both words keeps the row with "ask" on its label and nothing else:
	// the span names exactly the bytes of the term that won the label, and a
	// term that won the value contributes no emphasis to a field it did not.
	typeQuery(t, a, " prompt")
	at = -1
	for i, item := range a.sheet.items {
		if item.restful() && item.row.Key == config.KeyToolApprovalMode {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("\"ask prompt\" did not keep the approval row:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
	if hit := a.sheet.itemHit(a.sheet.items[at]); !reflect.DeepEqual(hit, []int{0, 1, 2}) {
		t.Fatalf("\"ask prompt\" carried %v on the approval row's label; want exactly \"ask\"'s three bytes — the value's word stays on the value", hit)
	}
}

// A SPACE IS A WORD IN THE QUERY. "shell command" is one question with two
// words, and it finds the row that answers both — the shell command rules —
// which could not be asked at all while the space dropped out of the box.
func TestTheSettingsSearchTakesASpaceMidQuery(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	labels := searchLabels(t, a, "shell command")
	if !screenHas(labels, "shell command rules") {
		t.Fatalf("\"shell command\" did not find the shell rules row:\n%s", strings.Join(labels, "\n"))
	}
	if got := a.sheet.query.String(); got != "shell command" {
		t.Fatalf("the query held %q, want \"shell command\"", got)
	}
}

// A SPACE WHILE SEARCHING TYPES AND DOES NOT PRESS THE ROW. The search owns
// the panel while it holds anything, and space used to fall through it: the
// row under the cursor activated, a setting flipped on a word's separator,
// and a phrase could not be typed. Out of a search the key keeps activating
// rows exactly as it always has.
func TestSpaceWhileSearchingTypesAndDoesNotPressTheRow(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	// This search fixture exercises an explicitly saved prompt posture.
	cursorTo(t, a, config.KeyToolApprovalMode)
	drive(t, a, key("enter")) // allow -> deny
	drive(t, a, key("enter")) // deny -> prompt
	cursorTo(t, a, config.KeyToolApprovalMode)
	// The space with no search open is the panel's own gesture: it activates.
	drive(t, a, key(" "))
	if got := config.ToolApprovalModeAt(dir); got != "allow" {
		t.Fatalf("space out of a search did not activate the row: gate still %q", got)
	}
	// Put the gate back, start a search, and the same key is the box's.
	drive(t, a, key("enter")) // allow -> deny
	drive(t, a, key("enter")) // deny -> prompt
	if got := config.ToolApprovalModeAt(dir); got != "prompt" {
		t.Fatalf("the gate did not come back around to prompt: %q", got)
	}
	cursorTo(t, a, config.KeyToolApprovalMode)
	typeQuery(t, a, "sh")
	drive(t, a, key(" "))
	if got := a.sheet.query.String(); got != "sh " {
		t.Fatalf("the space typed %q into the query, want \"sh \"", got)
	}
	if got := config.ToolApprovalModeAt(dir); got != "prompt" {
		t.Fatalf("the space flipped the gate mid-search: %q", got)
	}
	// And enter is still the row's key while a search is on.
	if a.sheet.searching() != true {
		t.Fatal("the search was not on")
	}
}

// THE ROWS A TAB KEEPS ARE RANKED BEST-FIRST. A word that prefixes a row's
// label — "approval" at the head of "approval countdown" — outranks the same
// word found inside another row's registry key, because the matcher pays the
// first character's boundary doubled when the word begins the row's own name
// and only a single delimiter boundary when it begins mid-key: 218 against
// 200. A word landing merely word-initial is worth the same as a label
// prefix, which is by design and not a defect — both are the same reading of
// "this word starts here".
func TestTheSettingsSearchRanksItsBestAnswerFirst(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	items := searchItems(t, a, "approval")
	countdown, gate := itemAt(items, config.KeyConsentTimeout), itemAt(items, config.KeyToolApprovalMode)
	if countdown < 0 || gate < 0 {
		t.Fatalf("\"approval\" found only %d of its two rows", countdown+gate+2)
	}
	if countdown > gate {
		t.Fatalf("the key hit (row %d) ranked above the label prefix (row %d)", gate, countdown)
	}
}

// TIES KEEP THE REGISTRY'S OWN ORDER. Two rows whose keys carry the same
// prefix with the same shape — `tools.approval` and `tools.approvalMode`
// against "tools.a" — score exactly alike, and the search leaves them in the
// order the unfiltered sheet draws them, which is the registry's and not the
// matcher's opinion.
func TestTheSettingsSearchKeepsRegistryOrderOnTies(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	filtered := searchItems(t, a, "tools.a")
	filteredApproval, filteredMode := itemAt(filtered, config.KeyToolApprovals), itemAt(filtered, config.KeyToolApprovalMode)
	if filteredApproval < 0 || filteredMode < 0 {
		t.Fatal("\"tools.a\" did not find both rows")
	}
	// The registry's own order is the sheet's row order, which a tie may not
	// disturb.
	plainApproval, plainMode := 0, 0
	for i, row := range a.sheet.rows {
		switch row.Key {
		case config.KeyToolApprovals:
			plainApproval = i
		case config.KeyToolApprovalMode:
			plainMode = i
		}
	}
	if (plainApproval < plainMode) != (filteredApproval < filteredMode) {
		t.Fatalf("a tie changed the registry's order: %d/%d became %d/%d",
			plainApproval, plainMode, filteredApproval, filteredMode)
	}
}

package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// railLines is the rail's column on the next frame, plain, one string a row,
// with the body row each landed on.
func railLines(t *testing.T, a *app) []string {
	t.Helper()
	frame, _, _ := a.frame()
	cols := a.trafficWidth()
	if cols <= trafficGripCols {
		t.Fatalf("the rail is not a column: %d", cols)
	}
	var out []string
	for _, r := range strings.Split(ansi.Strip(frame), "\n") {
		out = append(out, strings.TrimRight(plainCells(r, a.width-cols, a.width), " "))
	}
	return out
}

// railRowOf is the first frame row whose rail cells hold want, -1 for none.
func railRowOf(rows []string, want string) int {
	for y, r := range rows {
		if strings.Contains(r, want) {
			return y
		}
	}
	return -1
}

// threadScenario is the owner's measured afternoon: an older note, then one
// question to three members as ONE entry, each member woken on it, three
// replies, two finishings and the manager woken by them, all linked the way
// the session writes them. It hands back the question's id.
func threadScenario(t *testing.T, a *app, harbor, price, rail string) string {
	t.Helper()
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: price, Text: "an older aside"})
	q, err := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager,
		To: teamstore.ToSeveral, Handles: []string{price, rail, "review"}, Text: "Please provide a brief status update on your part"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{price, rail, "review"} {
		trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindEvent, From: teamstore.FromManager, To: h, State: teamstore.StateRunning, Text: "woke @" + h, Answers: q})
	}
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: "Status update: prices are scraped and cached", Answers: q},
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished", Answers: q},
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateRunning, Text: "woke ◆ (with @review)"},
		teamstore.Entry{Kind: teamstore.KindNote, From: "review", To: teamstore.ToManager, Text: "Status update: two findings, both minor", Answers: q},
		teamstore.Entry{Kind: teamstore.KindEvent, From: "review", To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished", Answers: q},
	)
	trafficReadNow(t, a)
	return q
}

// ONE QUESTION TO THREE MEMBERS IS ONE THREAD, at the top. Its header names
// all three, its words are on their own line, each answer is one row of a
// tree with its finishing folded in as ✓, a member woken with nothing said yet
// reads working…, no wake is a row, and the older thread is under it.
func TestTrafficThreadOneQuestionIsOneThreadAtTheTop(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	threadScenario(t, a, harbor, price, rail)
	rows := railLines(t, a)
	head := railRowOf(rows, trafficWord)
	top := railRowOf(rows, teamManagerGlyph+" manager → @"+price+" @"+rail+" @review  do")
	if head < 0 || top != head+1 {
		t.Fatalf("the question's thread is not straight under the header (%d, %d):\n%s", head, top, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[top+1], "Please provide a brief status") {
		t.Fatalf("the question's words are not on their own line:\n%s", strings.Join(rows, "\n"))
	}
	want := []string{
		"├ @" + price + "  ✓ Status update: pri",
		"├ @" + rail + "  working…",
		"└ @review  ✓ Status update: two",
	}
	// The tree is in the order things happened: each member's line stands
	// where it was first woken, and its answer takes that line.
	for i, w := range want {
		if !strings.Contains(rows[top+2+i], w) {
			t.Fatalf("row %d of the tree reads %q, want %q:\n%s", i, rows[top+2+i], w, strings.Join(rows, "\n"))
		}
	}
	joined := strings.Join(rows, "\n")
	for _, never := range []string{"woke", "finished"} {
		if strings.Contains(joined, never) {
			t.Errorf("%q is drawn as a row:\n%s", never, joined)
		}
	}
	older := railRowOf(rows, teamManagerGlyph+" manager → @"+price+"  fyi")
	if older <= top+4 || !strings.Contains(rows[older+1], "an older aside") {
		t.Fatalf("the older thread is not under the newer one (%d):\n%s", older, joined)
	}
}

// A MEMBER ASKING IS THE ONE AMBER LINE; A FAILURE IS ✗; AN OLD ENTRY THAT
// ANSWERS NOTHING IS ONE LINE OF ITS OWN.
func TestTrafficThreadEventsFoldAndOldEntriesStand(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindEvent, From: rail, To: teamstore.ToManager, State: teamstore.StateFinished, Text: "finished"})
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "run the migration"})
	trafficAppend(t, a, harbor,
		teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateAsking, Text: "asks: may I run it on prod?", Answers: q})
	trafficReadNow(t, a)
	rows := railLines(t, a)
	joined := strings.Join(rows, "\n")
	ask := railRowOf(rows, "└ @"+price+"  asking: may I run")
	if ask < 0 {
		t.Fatalf("the asking member is not its thread's line:\n%s", joined)
	}
	old := railRowOf(rows, "@"+rail+"  ✓ finished")
	if old < 0 || old < ask {
		t.Fatalf("the old unlinked finishing is not a line of its own under the newer thread:\n%s", joined)
	}
	// The amber is the asking line's words and nothing else's.
	a.traffic.cache = trafficCache{}
	body, _, _, _ := a.trafficBody(mustTeam(t, a, harbor), 20, 60)
	probe := a.pal.ask("x")
	warm := probe[:strings.Index(probe, "x")]
	amber := 0
	for _, r := range body {
		if warm != "" && strings.Contains(r, warm) {
			amber++
		}
	}
	if warm != "" && amber != 1 {
		t.Fatalf("%d rows carry the needs-you amber", amber)
	}
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindEvent, From: price, To: teamstore.ToManager, State: teamstore.StateFailed, Text: "the migration failed: lock timeout", Answers: q})
	trafficReadNow(t, a)
	if rows := railLines(t, a); railRowOf(rows, "└ @"+price+"  "+tokens.GlyphFailed+" the migration failed") < 0 {
		t.Fatalf("the failure did not fold into the member's line:\n%s", strings.Join(rows, "\n"))
	}
}

// A MESSAGE'S WORDS LAY OUT IN FULL ON A PRESS AND FOLD ON THE NEXT, and the
// hint line says them whole before either.
func TestTrafficThreadWordsExpandAndCollapse(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, _ := trafficHandle(t, a, harbor, "openrouter")
	long := "Status update: " + strings.Repeat("the scrape is cached and every price is checked twice ", 3) + "END"
	q, _ := teamstore.AppendTrafficID(a.profileDir, harbor, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: price, Text: "status?"})
	trafficAppend(t, a, harbor, teamstore.Entry{Kind: teamstore.KindNote, From: price, To: teamstore.ToManager, Text: long, Answers: q})
	trafficReadNow(t, a)
	rows := railLines(t, a)
	reply := railRowOf(rows, "└ @"+price)
	if reply < 0 || strings.Contains(strings.Join(rows, "\n"), "END") {
		t.Fatalf("the reply is not one cut row:\n%s", strings.Join(rows, "\n"))
	}
	cols := a.trafficWidth()
	x := a.width - cols + 2 + len("└ @"+price+"  ✓ ") + 2
	at, ok := a.trafficHoverAt(x, reply)
	if !ok || at.kind != hoverTraffic {
		t.Fatalf("the words do not answer the pointer: %+v", at)
	}
	a.hot = at
	if words := a.dockHoverWords(); !strings.Contains(words, "END") {
		t.Fatalf("the hint line does not say the words whole: %q", words)
	}
	a.hot = hoverAt{}
	front := a.frontTabKey()
	if _, took := a.trafficPress(x, reply); !took {
		t.Fatal("a press on the words was not taken")
	}
	if a.frontTabKey() != front {
		t.Fatal("laying the words out moved the focus")
	}
	open := railLines(t, a)
	if !strings.Contains(strings.Join(open, "\n"), "END") || railRowOf(open, "END") <= reply {
		t.Fatalf("the press did not lay the words out under the row:\n%s", strings.Join(open, "\n"))
	}
	if _, took := a.trafficPress(x, reply); !took {
		t.Fatal("the second press was not taken")
	}
	if folded := railLines(t, a); strings.Contains(strings.Join(folded, "\n"), "END") {
		t.Fatalf("the second press did not fold the words:\n%s", strings.Join(folded, "\n"))
	}
}

// A HANDLE ON THE RAIL IS A LINK, inked as the chat inks one, and a press on it
// opens its member; an address that is nobody's is plain words.
func TestTrafficThreadHandlesAreLinks(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 180, 40
	price, priceKey := trafficHandle(t, a, harbor, "openrouter")
	rail, _ := trafficHandle(t, a, harbor, "Refactor")
	threadScenario(t, a, harbor, price, rail)
	_ = railLines(t, a)
	d := a.traffic.drawn
	var links, strangers int
	for _, row := range d.doors {
		for _, door := range row {
			if door.link {
				links++
				if door.member == "" {
					t.Fatalf("a link opens nobody: %+v", door)
				}
			}
		}
	}
	for _, row := range a.traffic.cache.out {
		if strings.Contains(row, teamLinkInk(a.pal, "@review")) {
			strangers++
		}
	}
	if links < 4 || strangers != 0 {
		t.Fatalf("%d links drawn, %d inked strangers", links, strangers)
	}
	if !strings.Contains(strings.Join(a.traffic.cache.out, "\n"), teamLinkInk(a.pal, "@"+price)) {
		t.Fatal("a handle is not inked as a chat link is")
	}
	rows := railLines(t, a)
	y := railRowOf(rows, "├ @"+price)
	x := a.width - a.trafficWidth() + 2 + 3
	at, _ := a.trafficHoverAt(x, y)
	a.hot = at
	if words := a.dockHoverWords(); !strings.Contains(words, "@"+price) || !strings.Contains(words, "click") {
		t.Fatalf("the handle's hint says %q", words)
	}
	a.hot = hoverAt{}
	if _, took := a.trafficPress(x, y); !took || a.frontTabKey() != priceKey {
		t.Fatalf("the handle went to %q, want %q", a.frontTabKey(), priceKey)
	}
}

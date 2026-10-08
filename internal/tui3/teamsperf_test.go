package tui3

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func teamsPerfApp(tb testing.TB, members int) *app {
	tb.Helper()
	a := newApp(nil, Options{Agent: &fakeAgent{model: "bench/model"}, Workspace: tb.TempDir(), ProfileDir: tb.TempDir()})
	a.width, a.height = 160, 55
	a.pal = newPalette(tokens.TrueColor, false)
	a.wall.loaded = true
	a.page = pageTeams
	a.tp.sel = "perf"
	a.tp.previews = map[string]teamsPreview{}
	t := team{ID: "perf", Name: "Performance", Manager: "member-0"}
	for i := 0; i < members; i++ {
		key := fmt.Sprintf("member-%d", i)
		t.Members = append(t.Members, teamMember{Key: key, Handle: key, Word: "Check the rendering", File: key})
		a.tp.previews[key] = teamsPreview{count: 2, messages: [2]teamsPreviewMessage{
			{role: "user", text: "Please review this change"},
			{role: "assistant", text: strings.Repeat("## Review\n\nThe **rendering** preserves `layout` and [links](https://example.com).\n\n", 600)},
		}}
	}
	a.wall.teams = []team{t}
	return a
}

func TestTeamsHoverReusesPreviewAndKeepsGroundFresh(t *testing.T) {
	a := teamsPerfApp(t, 120)
	room := a.height - placeHeadRows - placeFootRowsFor(pageTeams, a.height)
	a.teamsBody(a.width, room)
	before := map[teamsPreviewShape]*teamsRenderedPreview{}
	for shape, cached := range a.tp.previewRows {
		before[shape] = cached
	}
	if len(before) == 0 || len(before) >= 20 {
		t.Fatalf("rendered offscreen previews: %d", len(before))
	}
	var target teamsTarget
	for _, hit := range a.tp.targets {
		if hit.act == teamsActMember && !hit.hidden && hit.arg != "member-0" {
			target = hit
			break
		}
	}
	if target.arg == "" {
		t.Fatal("no visible member to hover")
	}
	a.teamsHover(target.x0, target.y)
	if a.tp.hot != target.ref() || !a.dirty {
		t.Fatal("hover did not repaint the member ground")
	}
	a.teamsBody(a.width, room)
	for shape, cached := range before {
		if a.tp.previewRows[shape] != cached {
			t.Fatal("hover reparsed an unchanged preview")
		}
	}
	// A member below the screen remains a keyboard stop, and navigating to it
	// paints its excerpt immediately rather than publishing placeholder rows.
	a.tp.cur = teamsRef{act: teamsActMember, id: "perf", arg: "member-119"}
	a.tp.paneWheel = false
	rows := a.teamsBody(a.width, room)
	var words []string
	for _, row := range rows {
		words = append(words, row.text)
	}
	if a.tp.paneOffset == 0 || !strings.Contains(plain(strings.Join(words, "\n")), "@member-119") {
		t.Fatal("keyboard reveal published a blank offscreen card")
	}
	for _, hit := range a.tp.targets {
		if hit.arg == "member-119" && !hit.hidden {
			if got, ok := a.teamsTargetAt(hit.x0, hit.y); !ok || got.ref() != hit.ref() {
				t.Fatal("revealed member's click target drifted")
			}
			return
		}
	}
	t.Fatal("revealed member has no pointer target")
}

func TestTeamsPreviewMemoTracksContentGeometryInkAndPaths(t *testing.T) {
	a := teamsPerfApp(t, 1)
	key := "member-0"
	width, budget := 70, 6
	a.tp.previews[key] = teamsPreview{count: 2, messages: [2]teamsPreviewMessage{
		{role: "user", text: "Check this please"}, {role: "assistant", text: "The **answer** uses `file.go`."},
	}}
	a.pathLinks = true
	a.pathSeen = map[string]string{"file.go": ""}
	check := func() {
		t.Helper()
		got := a.teamsConversationPreview(key, width, budget)
		want := a.teamsRenderPreview(a.teamsManagerMessages(key), width, budget, a.plainCodePath)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("memo differs from fresh render:\n%q\n%q", got, want)
		}
	}
	check()
	shape := teamsPreviewShape{key: key, width: width, budget: budget}
	first := a.tp.previewRows[shape]
	check()
	if a.tp.previewRows[shape] != first {
		t.Fatal("unchanged preview was rendered twice")
	}
	p := a.tp.previews[key]
	p.messages[1].text = "The **update** uses `file.go`." // same byte length
	a.tp.previews[key] = p
	check()
	if a.tp.previewRows[shape] == first {
		t.Fatal("same-length correction retained stale words")
	}
	first = a.tp.previewRows[shape]
	a.pathSeen["file.go"] = a.workspace + "/file.go"
	check()
	if a.tp.previewRows[shape] == first {
		t.Fatal("new path fact retained stale code-span styling")
	}
	a.pathLinks = false
	check()
	a.pal = newPalette(tokens.ANSI16, true)
	check()
	width, budget = 25, 3
	check()
	p = a.tp.previews[key]
	p.messages[1].interrupted, p.messages[1].clipped = true, true
	a.tp.previews[key] = p
	check()
	a.tp.previews[key] = teamsPreview{missing: true}
	if words := plain(strings.Join(a.teamsConversationPreview(key, width, budget), "\n")); words != "Conversation unavailable" {
		t.Fatal("deleted conversation retained its preview:", words)
	}
}

func TestTeamsPreviewMemoBoundsShapesAndDropsOnClose(t *testing.T) {
	a := teamsPerfApp(t, 1)
	for width := 20; width < 20+teamsRenderedPreviewMax+10; width++ {
		a.teamsConversationPreview("member-0", width, 3)
	}
	if len(a.tp.previewRows) != teamsRenderedPreviewMax {
		t.Fatalf("unbounded resized previews: %d", len(a.tp.previewRows))
	}
	placeTeams{}.close(a)
	if len(a.tp.previewRows) != 0 {
		t.Fatal("page visit retained rendered previews after close")
	}
}

func TestTeamsViewportMatchesFullCardsAcrossScrollAndResize(t *testing.T) {
	a := teamsPerfApp(t, 18)
	for _, width := range []int{55, 90, 160} {
		a.width = width
		for _, off := range []int{0, 25, 60, 9999} {
			a.tp.paneOffset, a.tp.paneWheel = off, true
			room := 22
			got := a.teamsBody(width, room)
			paneW := width - teamsRailCols(width) - 1
			d := &teamsDraw{a: a}
			pane := a.teamsTop(d, paneW)
			pane = append(pane, a.teamsPaneRest(d, paneW, len(pane))...)
			for row := a.tp.paneTop; row < room; row++ {
				i := a.tp.paneOffset + row - a.tp.paneTop
				want := ""
				if i < len(pane) {
					want = strings.TrimRight(plain(pane[i]), " ")
				}
				text := got[row].text
				if rail := teamsRailCols(width); rail > 0 {
					text = ansi.Cut(text, rail, width)
				}
				if strings.TrimRight(plain(text), " ") != want {
					t.Fatalf("viewport differs at width=%d offset=%d row=%d", width, off, row)
				}
			}
		}
	}
}

func TestTeamsPointerStormStillFoldsBeforeKeyboard(t *testing.T) {
	a := teamsPerfApp(t, 120)
	now := time.Now()
	a.clock = func() time.Time { return now }
	a.View()
	for i := 0; i < 600; i++ {
		a.Update(tea.MouseMotionMsg{X: 35 + i%80, Y: 12})
		a.View()
	}
	if a.ptr.answered > 2 {
		t.Fatalf("pointer storm escaped coalescing: %d answers", a.ptr.answered)
	}
	a.Update(key("down"))
	a.View()
	if !a.tp.focus || a.tp.cur == (teamsRef{}) {
		t.Fatal("keyboard navigation did not run after the pointer burst")
	}
}

func TestTeamsPreviewMemoFollowsTheFrontStream(t *testing.T) {
	a := teamsPerfApp(t, 1)
	key := a.frontTabKey()
	a.entries = []entry{{kind: entryUser, text: "Review the stream"}, {kind: entryAssistant, text: "The first answer"}}
	shape := teamsPreviewShape{key: key, width: 60, budget: 6}
	first := a.teamsConversationPreview(key, 60, 6)
	a.entries[1].text = "The fresh answer"
	second := a.teamsConversationPreview(key, 60, 6)
	if reflect.DeepEqual(first, second) || !strings.Contains(plain(strings.Join(second, "\n")), "fresh answer") {
		t.Fatal("front stream retained the previous answer")
	}
	a.entries = append(a.entries, entry{kind: entryUser, text: "Now a new question"}, entry{kind: entryAssistant, text: "A new response"})
	a.teamsConversationPreview(key, 60, 6)
	if messages := a.tp.previewRows[shape].messages; messages[0].text != "Now a new question" {
		t.Fatal("new human exchange retained the old prompt")
	}
}

func BenchmarkTeamsHover(b *testing.B) {
	for _, members := range []int{12, 120, 1000} {
		b.Run(fmt.Sprintf("members=%d", members), func(b *testing.B) {
			a := teamsPerfApp(b, members)
			a.teamsBody(a.width, a.height-placeHeadRows-placeFootRowsFor(pageTeams, a.height))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.tp.hot = teamsRef{act: teamsActMember, id: "perf", arg: fmt.Sprintf("member-%d", i%3)}
				a.teamsBody(a.width, a.height-placeHeadRows-placeFootRowsFor(pageTeams, a.height))
			}
		})
	}
}

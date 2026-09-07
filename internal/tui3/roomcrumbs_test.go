package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestBreadcrumbsNameTheChatAndWalkAllAncestors(t *testing.T) {
	a, _, _ := taskApp(t)
	a.title = "Shipping the parser"
	for id := uint64(1); id <= 12; id++ {
		a.taskUpdate(update(id, fmt.Sprintf("Step %d", id), session.TaskRunning, session.TaskNotice{}))
		if id > 1 {
			a.tasks[id].parent = fmt.Sprint(id - 1)
		}
	}
	a.room = a.newRoom(12, "Step 12")
	if got := a.roomAncestors(); len(got) != 11 || got[0].id != 1 || got[10].id != 11 {
		t.Fatalf("lost ancestry: %+v", got)
	}
	for _, width := range []int{40, 60, 80, 160} {
		line, hits, _ := a.roomCrumbLine(width)
		if ansi.StringWidth(line) > width || !strings.Contains(line, "Step 12") || !strings.Contains(line, crumbFoldWord) {
			t.Fatalf("bad %d-cell trail: %q", width, line)
		}
		for _, hit := range hits {
			if hit.crumb.kind == crumbFold && hit.crumb.node == nil {
				t.Fatal("fold lost its ancestor door")
			}
		}
	}
	a.tasks[1].parent = "12"
	if got := len(a.roomAncestors()); got != 11 {
		t.Fatalf("cycle changed chain: %d", got)
	}
}

func TestBreadcrumbRootReturnsToTheNamedChat(t *testing.T) {
	a := roomNamed(t, 3, "Fix parsing")
	a.title = "Shipping the parser"
	a.width = 100
	_ = a.roomHead(a.width)
	var root crumbHit
	for _, hit := range a.crumbs {
		if hit.crumb.kind == crumbRoot {
			root = hit
		}
	}
	if !root.crumb.door() || !a.crumbPress(root.span.from, a.roomHeadRow()) || a.room != nil {
		t.Fatal("named root did not return to its conversation")
	}
	// AND THE CONVERSATION IS NAMED BY THE TAB ABOVE, which is where its name
	// lives now that the trail row it used to wear is gone (chattabs.go).
	if !strings.Contains(plain(a.tabsRow(a.width)), a.title) {
		t.Fatal("the tab strip lost its conversation name")
	}
}

func TestTheStripsOwnControlOpensThePickerWithoutSwitching(t *testing.T) {
	a, _, _ := taskApp(t)
	a.title = "Shipping the parser"
	keepThree(t, a)
	a.width, a.height = 100, 40
	before := a.title
	_ = a.tabsRow(a.width)
	var more tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabMore {
			more = hit
		}
	}
	if more.span.to == 0 {
		t.Fatalf("the strip drew no way to the picker: %q\n%+v", plain(a.tabsRow(a.width)), a.chatTabHits)
	}
	if cmd, took := a.tabPress(more.span.from, 0); !took || cmd != nil {
		t.Fatalf("the strip's control did not take the press: took=%v", took)
	}
	if !a.hop.open || a.title != before {
		t.Fatal("the strip's control must open a preview, without switching")
	}
}

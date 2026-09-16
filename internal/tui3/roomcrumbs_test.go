package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
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
	_ = strings.Join(a.roomHeadRows(a.width), "\n")
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

// THE STRIP HAS NO CONTROL OF ITS OWN ANY MORE. `Chats ▾` opened the switcher
// from the row's right end and is deleted; the card is reached by its key, which
// the legend under the box names wherever it would open. What this asserts is
// that the row did not keep a silent door where the label used to be — a press
// on the right end of the strip must fall through to the page under it rather
// than opening something nothing on screen says is there.
func TestTheStripHasNoSilentDoorWhereItsControlWas(t *testing.T) {
	a, _, _ := taskApp(t)
	a.title = "Shipping the parser"
	keepThree(t, a)
	a.width, a.height = 100, 40
	strip := plain(a.tabsRow(a.width))
	for _, hit := range a.chatTabHits {
		if hit.kind == tabFold && hit.door(a) {
			t.Fatalf("the count at the row's end is still a door:\n%q", strip)
		}
	}
	if strings.Contains(strip, "Chats") {
		t.Fatalf("the strip still draws its own control:\n%q", strip)
	}
	// AND THE KEY IS STILL THE WAY IN, which is the half that has to keep working.
	drive(t, a, key(hopOpenKey))
	if !a.hop.open {
		t.Fatalf("%s no longer opens the card from a task room", hopOpenKey)
	}
}

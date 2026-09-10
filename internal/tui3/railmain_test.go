package tui3

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestRailMainReturnsDirectlyFromDeepTask(t *testing.T) {
	for _, height := range []int{16, 40} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			a, _, _ := roomApp(t)
			a.width, a.height = 120, height
			for id := uint64(8); id <= 19; id++ {
				a.taskUpdate(update(id, fmt.Sprintf("Nested task %d", id), session.TaskRunning, session.TaskNotice{}))
				a.tasks[id].parent = fmt.Sprint(id - 1)
			}
			a.openRailRoom(a.tasks[19])
			a.railTop = 100
			rows := a.railRows(a.viewHeight())
			if len(rows) == 0 || !strings.Contains(plain(rows[0]), railMainWord) {
				t.Fatalf("return door is not pinned: %v", rows)
			}
			x, y := a.bodyWidth()+ansi.StringWidth(railSeam)+6, a.bodyTop()
			if got := a.roomPanelActionAt(x, y); got != railMainAction {
				t.Fatalf("return row has no hover target: %q", got)
			}
			a.hot = hoverAt{kind: hoverRoomControl, key: railMainAction}
			if hovered := a.railRows(a.viewHeight())[0]; hovered == rows[0] {
				t.Fatal("return row has no hover feedback")
			}
			drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
			if a.roomOpen() || a.railHold {
				t.Fatal("one click did not return directly to main")
			}
			if strings.Contains(plain(strings.Join(a.railRows(a.viewHeight()), "\n")), railMainWord) {
				t.Fatal("return door remains on main")
			}
		})
	}
}

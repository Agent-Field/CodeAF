package tui3

import (
	"fmt"
	"strings"
	"testing"
)

func TestHomeCloseRemovesTheRowAndTabUntilSearchedAgain(t *testing.T) {
	for _, width := range []int{50, 80, 120, 180} {
		for _, filtered := range []bool{false, true} {
			t.Run(fmt.Sprintf("width_%d_filtered_%t", width, filtered), func(t *testing.T) {
				a, files := homeTabsFixture(t)
				a.width = width
				a.input.setText("keep my draft")
				if filtered {
					a.home.box.setText("Conversation 2")
				}
				a.home.build()
				a.home.point(files[1])
				if filtered {
					drive(t, a, key("ctrl+e"))
				} else {
					drive(t, a, key("right"), key("x"))
				}
				assertGone := func() {
					t.Helper()
					if !a.at(pageHome) {
						t.Fatal("close left Home")
					}
					for _, line := range a.home.lines {
						if line.kind == homeSession && line.row.Transcript == files[1] {
							t.Fatal("closed conversation stayed on Home")
						}
					}
					for _, tab := range a.tabList() {
						if tab.file == files[1] {
							t.Fatal("closed conversation kept its tab")
						}
					}
					if a.input.String() != "keep my draft" {
						t.Fatal("close lost the active conversation's draft")
					}
				}
				assertGone()
				a.refreshHome()
				assertGone()
				a.home.box.setText("")
				a.home.build()
				a.home.box.setText("Conversation 2")
				a.home.build()
				a.home.point(files[1])
				line, ok := a.home.focusedLine()
				if !ok || line.row.Transcript != files[1] {
					t.Fatal("a fresh search cannot find the closed conversation")
				}
				drive(t, a, key("enter"))
				// Compact Home first opens its detail sheet, whose Enter opens the conversation.
				if a.at(pageHome) && a.home.phone {
					drive(t, a, key("enter"))
				}
				if a.at(pageHome) || a.file != files[1] || a.tabShut[a.convKey(files[1])] {
					t.Fatal("search did not reopen the conversation and its tab")
				}
				if strings.Contains(plain(frame(a)), "could not") {
					t.Fatal("reopen reported a failure")
				}
			})
		}
	}
}

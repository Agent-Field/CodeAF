package tui3

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
	"time"
)

func TestCommandMenuIsAlphabeticalWhileFiltering(t *testing.T) {
	for _, query := range []string{"", "a", "o", "res", "conf"} {
		m := menu{}
		e := editor{}
		e.setText("/" + query)
		m.sync(&e)
		last := ""
		for _, at := range m.hits {
			c := commands[at]
			if c.name < last {
				t.Fatalf("%q: /%s followed /%s", query, c.name, last)
			}
			if _, ok := c.matchAt(query); !ok {
				t.Fatalf("%q offered %s", query, c.name)
			}
			last = c.name
		}
		if query == "" && len(m.hits) != len(commands) {
			t.Fatal("bare slash omitted commands")
		}
	}
}

func TestHomeCommandModeContainsOnlyCommandsAndCanReachEveryRow(t *testing.T) {
	for _, width := range []int{45, 100, 180} {
		lab := newHomeLab(t)
		mine := lab.session("-alpha", "aaaa000000000001", "/ model approvals", "/tmp/alpha", time.Now())
		a := lab.app(mine)
		a.width, a.height = width, 25
		a.openHome()
		typeHome(a, "/")
		if len(a.home.lines) != len(commands) {
			t.Fatalf("%d columns: %d rows, want %d", width, len(a.home.lines), len(commands))
		}
		last := ""
		for i := range a.home.lines {
			line, ok := a.home.focusedLine()
			if !ok || line.kind != homeCommand || line.cmd.name < last {
				t.Fatalf("row %d: missing or unsorted command", i)
			}
			if !strings.Contains(homeText(a), "/"+line.cmd.name) {
				t.Fatalf("%d columns: chosen %s is off screen", width, line.cmd.typed())
			}
			last = line.cmd.name
			drive(t, a, key("down"))
		}
		if a.home.cursor != len(a.home.lines)-1 {
			t.Fatal("down escaped the command menu")
		}
		a.home.box.setText("/zzzz")
		a.home.build()
		if !a.home.cmd.open || len(a.home.lines) != 0 || !strings.Contains(homeText(a), commandNoMatchWord) {
			t.Fatal("no-match command filter showed other results")
		}
		a.home.box.setText("model")
		a.home.build()
		if a.home.cmd.open || len(a.home.lines) == 0 {
			t.Fatal("ordinary text did not restore search")
		}
	}
}

func TestCommandModeTracksTheWholeTokenAndCaret(t *testing.T) {
	for _, tc := range []struct {
		text  string
		caret int
		open  bool
	}{
		{"/", 1, true}, {"/?", 2, true}, {"/mod", 4, true}, {"/zzzz", 5, true},
		{"explain /mod", 12, true}, {"/model arg", 10, false},
		{"/tmp/project", 4, false}, {"/tmp/project", 12, false},
		{"/image.png", 10, false}, {"https://example.org", 19, false},
		{"path/to/file", 12, false}, {"prose", 5, false},
		{"/model arg", 4, true}, {"/model arg", 0, false},
	} {
		e := editor{}
		e.setText(tc.text)
		e.cursor = tc.caret
		m := menu{}
		m.sync(&e)
		if m.open != tc.open {
			t.Fatalf("%q at %d: open=%v", tc.text, tc.caret, m.open)
		}
	}
	a := newTestApp(&fakeAgent{model: "m"})
	typeInto(t, a, "/model text")
	drive(t, a, key("home"))
	for range 4 {
		drive(t, a, key("right"))
	}
	if !a.menu.open {
		t.Fatal("moving the caret into a command did not open the list")
	}
	drive(t, a, key("end"))
	if a.menu.open {
		t.Fatal("moving into the argument kept the list open")
	}
}

func TestConversationCommandsAreAboveTheSeamAndKeepPointerTargets(t *testing.T) {
	for _, width := range []int{45, 100, 180} {
		for _, height := range []int{18, 40} {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width, a.height = width, height
			typeInto(t, a, "/")
			rows, marks, _, caret := a.chrome(width)
			seam, lastMenu := -1, -1
			for i, mark := range marks {
				if mark.kind == chromeLegend {
					seam = i
				}
				if mark.kind == chromeOverlay {
					lastMenu = i
				}
			}
			if seam < 0 || lastMenu < 0 || lastMenu >= seam || caret <= seam {
				t.Fatalf("%dx%d: menu %d, seam %d, caret %d", width, height, lastMenu, seam, caret)
			}
			if len(rows)+a.topHeight() > height {
				t.Fatalf("command list overflows %dx%d", width, height)
			}
			frame, _, _ := a.frame()
			first, _ := a.menu.choice()
			if !strings.Contains(plain(frame), first.typed()) {
				t.Fatal("first command clipped from frame")
			}
			seen := false
			for y := 0; y < height; y++ {
				if mark, ok := a.chromeAt(y); ok && mark.kind == chromeOverlay {
					seen = true
					break
				}
			}
			if !seen {
				t.Fatal("moved command menu lost pointer targets")
			}
			for range len(commands) {
				drive(t, a, key("down"))
			}
			last, _ := a.menu.choice()
			frame, _, _ = a.frame()
			if !strings.Contains(plain(frame), last.typed()) {
				t.Fatal("last command is unreachable")
			}
		}
	}
}

func TestCommandListsScrollByWheelAndPageKeys(t *testing.T) {
	for _, home := range []bool{false, true} {
		lab := newHomeLab(t)
		mine := lab.session("-alpha", "aaaa000000000001", "alpha", "/tmp/alpha", time.Now())
		a := lab.app(mine)
		a.width, a.height = 100, 25
		if home {
			a.openHome()
			typeHome(a, "/")
		} else {
			typeInto(t, a, "/")
		}
		cursor := func() int {
			if home {
				return a.home.cursor
			}
			return a.menu.cursor
		}
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: 10, Button: tea.MouseWheelDown})
		if cursor() <= 0 {
			t.Fatalf("home=%v: wheel did not advance command", home)
		}
		before := cursor()
		drive(t, a, key("pgdown"))
		if cursor() <= before {
			t.Fatalf("home=%v: page down did not advance", home)
		}
		drive(t, a, key("pgup"))
		if cursor() >= before+1 {
			t.Fatalf("home=%v: page up did not return", home)
		}
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: 10, Button: tea.MouseWheelUp})
		if cursor() != 0 {
			t.Fatalf("home=%v: wheel did not return to first command: %d", home, cursor())
		}
	}
}

func TestHomeCommandModeFollowsCaretAndOwnsMultilineArrows(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-alpha", "aaaa000000000001", "alpha", "/tmp/alpha", time.Now())
	a := lab.app(mine)
	a.openHome()
	a.home.box.setText("first line\n/model argument")
	a.home.box.cursor = len([]rune("first line\n/mod"))
	a.home.build()
	if !a.home.cmd.open {
		t.Fatal("inline command did not open")
	}
	before := a.home.box.cursor
	drive(t, a, key("down"))
	if a.home.cursor != 1 || a.home.box.cursor != before {
		t.Fatal("command navigation moved the multiline draft caret")
	}
	for range len("el argument") {
		drive(t, a, key("right"))
	}
	if a.home.cmd.open {
		t.Fatal("argument kept home in command mode")
	}
	for range len(" argument") {
		drive(t, a, key("left"))
	}
	if !a.home.cmd.open {
		t.Fatal("returning to the command token did not reopen it")
	}
}

func TestStartPageCommandListKeepsItsDraftCaret(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)
	typeInto(t, a, "/")
	for _, height := range []int{18, 40} {
		a.width, a.height = 100, height
		text, _, caretY := a.frame()
		lines := strings.Split(plain(text), "\n")
		if caretY < 0 || caretY >= len(lines) || !strings.Contains(lines[caretY], "/") {
			t.Fatalf("height %d: caret row %d does not contain the draft", height, caretY)
		}
		chosen, _ := a.menu.choice()
		if !strings.Contains(plain(text), chosen.typed()) {
			t.Fatalf("height %d: start page clipped %s at menu top %d, chrome %d:\n%s", height, chosen.typed(), a.menu.top, a.chromeHeight(), plain(text))
		}
	}
}

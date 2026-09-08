package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A remembered tab can belong to a shared connection or a locally released
// conversation. Its navigation identity outlives the live keeper entry.
func rememberUnheldTab(a *app, file, workspace, title string) chatTab {
	tab := chatTab{key: a.convKey(file), file: file, where: workspace, word: title}
	a.chatTabs = append(a.chatTabs, tab)
	a.rememberOpen(tab.key)
	return tab
}

func TestClosingSharedCurrentTabSelectsThePreviousRemoteTab(t *testing.T) {
	a, engine, handle := sharedSurface(t)
	a.width, a.height = 160, 40
	a.draftFile = filepath.Join(t.TempDir(), "draft.txt")
	a.input.setText("draft belonging to A")
	_ = a.tabsRow(a.width)
	cmd, refusal := a.openBeside("/srv/app", "/srv/app/b.jsonl")
	if refusal != "" {
		t.Fatal(refusal)
	}
	drain(t, a, cmd)
	a.input.setText("draft belonging to B")
	_ = a.tabsRow(a.width)
	closed := a.frontChatTab()
	lanes := engine.lanes
	drain(t, a, a.tabDismiss(closed))
	if a.at(pageHome) || a.file != "/srv/app/a.jsonl" || engine.at != a.file {
		t.Fatalf("closing shared B did not select remote A: page=%v surface=%q engine=%q", a.page, a.file, engine.at)
	}
	if a.input.String() != "draft belonging to A" {
		t.Fatalf("selection restored the wrong draft: %q", a.input.String())
	}
	if !a.tabShut[closed.key] || len(a.behind) != 0 {
		t.Fatal("shared close retained its tab or duplicated the shared handle")
	}
	if len(engine.ended) != 2 || engine.ended[1] != closed.file {
		t.Fatalf("close did not use normal remote swap semantics: %v", engine.ended)
	}
	if len(engine.shut) != 0 || len(engine.stopped) != 0 || handle.closes != 0 || engine.lanes != lanes {
		t.Fatal("surface stopped the selected remote session or duplicated its lanes")
	}
	cmd, refusal = a.openBeside("/srv/app", closed.file)
	if refusal != "" {
		t.Fatal(refusal)
	}
	drain(t, a, cmd)
	if a.input.String() != "draft belonging to B" {
		t.Fatalf("closing lost the outgoing draft: %q", a.input.String())
	}
}

func TestClosingCurrentTabKeepsSelectionAndDraftWhenResumeRefuses(t *testing.T) {
	for _, refusal := range []error{os.ErrNotExist, errors.New("resume permission denied")} {
		t.Run(refusal.Error(), func(t *testing.T) {
			a, engine, handle := sharedSurface(t)
			a.width, a.height = 120, 40
			_ = a.tabsRow(a.width)
			rememberUnheldTab(a, "/srv/only-on-the-other-machine/b.jsonl", "/srv/only-on-the-other-machine", "Remote previous chat")
			a.tabShut = map[string]bool{"already-dismissed": true}
			a.input.setText("keep this exact draft")
			a.input.cursor = 9
			_ = a.tabsRow(a.width)
			before, order := a.file, append([]string(nil), a.prev...)
			closed := a.frontChatTab()
			called := 0
			a.resume = func(file string) (Agent, error) {
				called++
				if file != "/srv/only-on-the-other-machine/b.jsonl" {
					t.Fatalf("resume asked for %q", file)
				}
				return nil, refusal
			}
			drain(t, a, a.tabDismiss(closed))
			if called != 1 {
				t.Fatal("remote tab was rejected by a local-folder check before the resume door")
			}
			if a.file != before || a.at(pageHome) || engine.at != before || a.input.String() != "keep this exact draft" || a.input.cursor != 9 {
				t.Fatal("failed resume changed the selected chat or its draft")
			}
			if !reflect.DeepEqual(a.prev, order) || len(a.tabShut) != 1 || !a.tabShut["already-dismissed"] {
				t.Fatal("failed resume changed tab history or recorded a successful dismissal")
			}
			if !strings.Contains(plain(frame(a)), refusal.Error()) {
				t.Fatal("failed selection did not explain the refusal")
			}
			if handle.closes != 0 || len(engine.ended) != 0 || len(engine.stopped) != 0 {
				t.Fatal("failed selection ended a conversation")
			}
		})
	}
}

func TestClosingCurrentTabCanReopenAnUnheldLocalTab(t *testing.T) {
	old, next := &fakeAgent{model: "m"}, &fakeAgent{model: "m"}
	a := newTestApp(old)
	dir := t.TempDir()
	a.file, a.workspace, a.title = filepath.Join(dir, "a.jsonl"), dir, "Current chat"
	a.width, a.height = 120, 40
	_ = a.tabsRow(a.width)
	target := rememberUnheldTab(a, filepath.Join(dir, "b.jsonl"), dir, "Remembered local chat")
	a.input.setText("unsent local draft")
	called := 0
	a.resume = func(file string) (Agent, error) {
		called++
		if file != target.file {
			t.Fatalf("reopening wrong remembered tab: %q", file)
		}
		return next, nil
	}
	closed := a.frontChatTab()
	drain(t, a, a.tabDismiss(closed))
	if called != 1 || a.file != target.file || a.at(pageHome) || !a.tabShut[closed.key] {
		t.Fatal("unheld local tab was skipped in favor of Home")
	}
	if old.closes != 0 || old.stops != 0 || next.closes != 0 || next.stops != 0 {
		t.Fatal("closing local tab ended work")
	}
	if held := a.behind[closed.key]; held == nil || held.side.draft != "unsent local draft" {
		t.Fatal("local outgoing draft was not parked under its owner")
	}
}

func TestClosingCurrentTabSkipsTheMostRecentDismissedTab(t *testing.T) {
	a, _, _ := tabApp(t)
	older := a.convKey("/tmp/lab/price-scrape.jsonl")
	newer := a.convKey("/tmp/lab/rail-scope.jsonl")
	a.tabShutKey(newer)
	// The dismissed tab stays most recent in the remembered history; it must
	// not become the close gesture's fallback just because its agent is alive.
	a.forget(newer)
	a.prev = append(a.prev, newer)
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	if a.file != older || a.at(pageHome) || !a.tabShut[newer] {
		t.Fatalf("closing selected a dismissed tab: %q", a.file)
	}
}

func TestClosingAnUnnamedCurrentTabSelectsAnotherOpenTab(t *testing.T) {
	a, older, newer := tabApp(t)
	a.file, a.title = "", ""
	a.chatTabWho = tabIdentity{}
	a.chatTabs = nil
	a.chatTabBar = tabBar{}
	_ = a.tabsRow(a.width)
	if a.frontTabKey() != "" {
		t.Fatal("fixture unexpectedly has a persisted current identity")
	}
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	if a.at(pageHome) || a.file != "/tmp/lab/rail-scope.jsonl" {
		t.Fatalf("unnamed current tab went Home despite an open tab: %q page=%v", a.file, a.page)
	}
	if older.closes+newer.closes+older.stops+newer.stops != 0 {
		t.Fatal("unnamed tab navigation ended held work")
	}
}

package tui3

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestRestartRestoresEveryMainPlace(t *testing.T) {
	for _, room := range placeRegistry {
		if room.id() == pageChats {
			continue
		}
		t.Run(room.word(), func(t *testing.T) {
			profile, launch := t.TempDir(), t.TempDir()
			opts := Options{Agent: &fakeAgent{model: "m"}, ProfileDir: profile,
				Workspace: launch, SessionFile: filepath.Join(launch, "chosen.jsonl"),
				SaveView: func(view config.ViewState) error { return config.WriteViewState(profile, launch, view) }}
			first := newApp(context.Background(), opts)
			first.showPage(room.id())
			first.quit()
			saved, ok := config.ViewStateAt(profile, launch)
			if !ok || saved.Place != room.word() {
				t.Fatalf("shutdown saved %+v, found %v", saved, ok)
			}
			opts.RestorePlace, opts.SessionFile = saved.Place, saved.Session
			second := newApp(context.Background(), opts)
			if !second.at(room.id()) || second.file != saved.Session || second.welcome.open {
				t.Fatalf("restart: page %v, file %s, welcome %v", second.page, second.file, second.welcome.open)
			}
			if second.at(pageHome) && second.restoreViewCmd != nil {
				t.Fatal("home restoration armed a second set of launch clocks")
			}
			second.quit()
		})
	}
}

func TestSavedChatSkipsGreetingAndNavigationWritesOnlyOnChange(t *testing.T) {
	profile, workspace := t.TempDir(), t.TempDir()
	var saved []config.ViewState
	a := newApp(context.Background(), Options{Agent: &fakeAgent{model: "m"}, ProfileDir: profile,
		Workspace: workspace, SessionFile: filepath.Join(workspace, "chosen.jsonl"), Landing: true,
		RestorePlace: "chat", SaveView: func(view config.ViewState) error { saved = append(saved, view); return nil }})
	if a.showing() != nil || a.welcome.open {
		t.Fatal("saved chat was covered by a greeting")
	}
	a.Update(key("x"))
	a.Update(key("y"))
	if len(saved) != 1 || saved[0].Place != "chat" {
		t.Fatalf("typing rewrote navigation: %+v", saved)
	}
	a.showPage(pageSettings)
	a.Update(key("left"))
	if len(saved) != 2 || saved[1].Place != "settings" {
		t.Fatalf("navigation did not save settings: %+v", saved)
	}
	a.quit()
	if len(saved) != 3 || saved[2].Place != "settings" {
		t.Fatalf("shutdown did not take precedence: %+v", saved)
	}
}

func TestRestorePlaceLeavesExplicitAndRemoteViewsAlone(t *testing.T) {
	for _, setup := range []func(*app){func(a *app) { a.pickSession = true }, func(a *app) { a.host = "devbox" }, func(a *app) { a.takeOverAt = "/held.jsonl" }} {
		a := newTestApp(&fakeAgent{model: "m"})
		setup(a)
		if a.restorePlace("settings") {
			t.Fatal("restore covered an explicit, remote or takeover view")
		}
		a.quit()
	}
	a := newTestApp(&fakeAgent{model: "m"})
	if a.restorePlace("a-future-place") {
		t.Fatal("unknown saved place was accepted")
	}
	a.quit()
}

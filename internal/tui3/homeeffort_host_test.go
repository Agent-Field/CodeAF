package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A local window uses the host connection too. Auto is an empty wire value,
// so the real welcome must keep both message boxes' effort controls visible.
func TestHomeEffortSurvivesTheEngineConnectionAtAuto(t *testing.T) {
	for _, installed := range []effort.Rung{effort.None, effort.High} {
		word := installed.String()
		if word == "" {
			word = effortAutoWord
		}
		t.Run(word, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "transcript.jsonl")
			engine, err := session.New(session.Config{
				Workspace: dir, SessionFile: file, Model: "test/model", APIKey: "fixture",
				BaseURL: "http://127.0.0.1:1/v1", System: "Test only", DefaultEffort: installed,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{Agent: engine, SessionFile: file, Workspace: dir}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loop.Close() })
			_, a := drafting(t)
			a.agent = loop.Client.Agent()
			a.model = engine.Model()
			a.showPage(pageHome)
			firstRowsReadHome(t, a)
			before := loop.Client.CallsMade()
			frame := placeFrameText(a)
			if !strings.Contains(frame, ":"+word) || !strings.Contains(a.homeHint(), targetEffortKeyWord) {
				t.Fatalf("home lost effort %q: hint=%q, frame=%s", word, a.homeHint(), frame)
			}
			if !a.targetEffortSpan.pressable() {
				t.Fatal("home effort has no click target")
			}
			drive(t, a, key(effortKey))
			next := effortNextClearing(installed)
			if got, ok := a.targetEffort(); !ok || got != next.String() {
				t.Fatalf("home effort key landed on %q, supported=%v", got, ok)
			}
			placeFrameText(a)
			if _, took := a.placeTargetPress(a.targetEffortSpan.from, a.targetRow); !took {
				t.Fatal("home effort click was not taken")
			}
			if got, _ := a.targetEffort(); got != effortNextClearing(next).String() {
				t.Fatalf("home effort click landed on %q", got)
			}
			a.leavePlace()
			if chip := a.effortChipText(); chip != word || !strings.Contains(a.idleHint(), targetEffortKeyWord) {
				t.Fatalf("conversation lost effort: chip=%q hint=%q", chip, a.idleHint())
			}
			if got := loop.Client.CallsMade() - before; got != 0 {
				t.Fatalf("drawing and pinning home effort made %d remote calls", got)
			}
		})
	}
}

// Connection methods exist even when the engine has no effort capability.
// A remembered default must not make that unavailable control actionable.
type draftWithoutEffort struct{ *draftAgent }

func (*draftWithoutEffort) EffortSupported() bool { return false }

func TestHomeOmitsEffortWhenTheEngineHasNoDial(t *testing.T) {
	agent, a := drafting(t)
	a.agent = &draftWithoutEffort{agent}
	a.showPage(pageHome)
	frame := placeFrameText(a)
	if strings.Contains(frame, ":high") || strings.Contains(a.homeHint(), targetEffortKeyWord) || a.targetEffortSpan.pressable() {
		t.Fatal("home offered effort without an engine capability")
	}
	drive(t, a, key(effortKey))
	if a.target.effort != "" {
		t.Fatal("effort key pinned a rung on an unsupported engine")
	}
}

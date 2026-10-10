package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

func updateNotes(a *app) string {
	var lines []string
	for _, entry := range a.entries {
		if entry.kind == entryNote {
			lines = append(lines, entry.text)
		}
	}
	return strings.Join(lines, "\n")
}

// TestC1LaunchAvailabilityBecomesOneTranscriptNote proves C1.
func TestC1LaunchAvailabilityBecomesOneTranscriptNote(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "test/model"})
	available := codeupdate.Available{Latest: "v0.2.0", Running: "v0.1.1"}
	drive(t, a, updateCheckMsg{available: available, show: true})
	line := "codeaf v0.2.0 is out · you have v0.1.1 · /update installs it for the next launch · or: " + codeupdate.CurlCommand
	if got := strings.Count(updateNotes(a), line); got != 1 {
		t.Fatalf("launch note count = %d, want 1:\n%s", got, updateNotes(a))
	}
}

// TestASecondUpdateIsRefusedWhileWorkContinues proves one install at a time is
// enforced, and that an install in flight blocks NOTHING else: a turn started
// from home while the download runs is exactly what a background update must
// allow. (The old name of this test was "OneUpdateOwnsTheSurface": it does not.)
func TestASecondUpdateIsRefusedWhileWorkContinues(t *testing.T) {
	agent := &fakeAgent{model: "test/model"}
	a := newApp(context.Background(), Options{
		Agent: agent, Workspace: "/tmp/lab", UpdateRunning: "v0.1.1", Restart: &codeupdate.Plan{},
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{}, nil
		},
	})
	first := a.slash("/update")
	if first == nil || !a.updateInFlight {
		t.Fatalf("first command = %v, in flight = %t", first, a.updateInFlight)
	}
	if second := a.slash("/upgrade"); second != nil {
		t.Fatal("the overlapping update returned a command")
	}
	if strings.Count(updateNotes(a), "an update is already running") != 1 {
		t.Fatalf("notes do not contain the refusal once:\n%s", updateNotes(a))
	}
	started := 0
	a.start = func(string) (Conversation, error) {
		started++
		return Conversation{Agent: &fakeAgent{model: "test/model"}, SessionFile: "/tmp/next/transcript.jsonl"}, nil
	}
	a.openHome()
	a.home.box.setText("start another turn")
	a.home.build()
	drive(t, a, key("enter"))
	if started != 1 {
		t.Fatalf("home enter started %d conversations while an install was in flight", started)
	}
}

// TestABackgroundInstallLeavesTheAskHereRoadOpen proves a running download is
// not a lock on the surface: the home chord still starts an errand, which the
// old blocking design refused.
func TestABackgroundInstallLeavesTheAskHereRoadOpen(t *testing.T) {
	called := 0
	a := newTestApp(&fakeAgent{model: "test/model"})
	a.updateInFlight = true
	a.errandsRoot = t.TempDir()
	a.errand = func(ErrandOrders) (Agent, error) {
		called++
		return &errandAgent{fakeAgent: fakeAgent{model: "test/model"}}, nil
	}
	a.openHome()
	a.home.box.setText("ask from home")
	a.home.build()
	drive(t, a, key("alt+enter"))
	if !a.composerShowing() {
		t.Fatal("the first ask-here chord did not open its composer")
	}
	drive(t, a, key("alt+enter"))
	if called == 0 {
		t.Fatal("the install in flight stopped home's ask-here road")
	}
}

// TestUpdateCheckCommandDefersTheReleaseDoor proves the command itself does no
// work until Bubble Tea runs it. The shared door test proves the first frame.
func TestUpdateCheckCommandDefersTheReleaseDoor(t *testing.T) {
	called := false
	a := newTestApp(&fakeAgent{model: "test/model"})
	a.updateCheck = func(context.Context) (codeupdate.Available, bool) {
		called = true
		return codeupdate.Available{}, false
	}
	command := a.checkForUpdate()
	if called || command == nil {
		t.Fatalf("building the first frame called = %t, command nil = %t", called, command == nil)
	}
	_ = command()
	if !called {
		t.Fatal("the command did not make the deferred check")
	}
}

// TestC6UpdateOnTheNewestBuildDoesNotInstallOrQuit proves C6.
func TestC6UpdateOnTheNewestBuildDoesNotInstallOrQuit(t *testing.T) {
	installed := false
	restart := &codeupdate.Plan{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab",
		UpdateRunning: "v0.2.0", Restart: restart,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			installed = true
			return codeupdate.InstallResult{}, nil
		},
	})
	command := a.slash("/update")
	drive(t, a, command())
	if installed || a.updateInFlight || restart.Path != "" || !strings.Contains(updateNotes(a), "you are on the newest codeaf, v0.2.0") {
		t.Fatalf("installed = %t in flight = %t restart = %+v notes:\n%s", installed, a.updateInFlight, restart, updateNotes(a))
	}
}

// TestC15ChatUpdateRefusesAnImplicitDowngradeAndAnExactTagInstalls proves C15.
func TestC15ChatUpdateRefusesAnImplicitDowngradeAndAnExactTagInstalls(t *testing.T) {
	t.Run("implicit stable", func(t *testing.T) {
		installed := false
		restart := &codeupdate.Plan{}
		a := newApp(context.Background(), Options{
			Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab",
			UpdateRunning: "v0.3.0", Restart: restart,
			ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
				return codeupdate.Release{Tag: "v0.2.0"}, nil
			},
			InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
				installed = true
				return codeupdate.InstallResult{}, nil
			},
		})
		command := a.slash("/update")
		drive(t, a, command())
		want := "this codeaf is v0.3.0, ahead of the newest stable v0.2.0 — /update v0.2.0 installs it anyway"
		if installed || a.updateInFlight || restart.Path != "" || !strings.Contains(updateNotes(a), want) {
			t.Fatalf("installed = %t in flight = %t restart = %+v notes:\n%s", installed, a.updateInFlight, restart, updateNotes(a))
		}
	})

	t.Run("exact tag", func(t *testing.T) {
		installed := false
		restart := &codeupdate.Plan{}
		a := newApp(context.Background(), Options{
			Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", SessionFile: "/tmp/this.jsonl",
			UpdateRunning: "v0.3.0", Restart: restart,
			ResolveUpdate: func(_ context.Context, choice codeupdate.Choice) (codeupdate.Release, error) {
				if choice.Version != "v0.2.0" {
					t.Fatalf("choice = %+v", choice)
				}
				return codeupdate.Release{Tag: "v0.2.0"}, nil
			},
			InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
				installed = true
				return codeupdate.InstallResult{Release: codeupdate.Release{Tag: "v0.2.0"}, Path: "/tmp/codeaf"}, nil
			},
		})
		command := a.slash("/update v0.2.0")
		drive(t, a, command())
		// A HAND-RUN INSTALL IS A BACKGROUND INSTALL: the file is replaced and
		// the session keeps running the build it started on, so no restart plan
		// is armed at all.
		if !installed || restart.Path != "" || !strings.Contains(updateNotes(a), "codeaf v0.2.0 installed") {
			t.Fatalf("installed = %t restart = %+v notes:\n%s", installed, restart, updateNotes(a))
		}
	})
}

// TestUpdateCommandListNamesItsOptionalArgument proves the list agrees with the manual.
func TestUpdateCommandListNamesItsOptionalArgument(t *testing.T) {
	bare, withArgument := false, false
	for _, row := range commands {
		if row.name != "update" {
			continue
		}
		bare = bare || row.args == ""
		withArgument = withArgument || row.args == "<channel or tag>"
	}
	if !bare || !withArgument {
		t.Fatalf("update rows: bare = %t argument = %t", bare, withArgument)
	}
}

// TestC7UpdateInstallReplacesTheFileAndKeepsThisSession proves C7 under the
// new law: the file is replaced in the background, the session keeps the build
// it started on, and NO restart is armed.
func TestC7UpdateInstallReplacesTheFileAndKeepsThisSession(t *testing.T) {
	restart := &codeupdate.Plan{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", SessionFile: "/tmp/this.jsonl",
		UpdateRunning: "v0.1.1", UpdateArgs: []string{"chat", "--model", "x"}, Restart: restart,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{Release: codeupdate.Release{Tag: "v0.2.0"}, Path: "/tmp/codeaf"}, nil
		},
	})
	command := a.slash("/update")
	drive(t, a, command())
	if restart.Path != "" {
		t.Fatalf("a background install armed a restart: %+v", restart)
	}
	for _, want := range []string{"updating codeaf in the background", "checksum matched \u00b7 installed at /tmp/codeaf", "codeaf v0.2.0 installed"} {
		if !strings.Contains(updateNotes(a), want) {
			t.Fatalf("notes do not contain %q:\n%s", want, updateNotes(a))
		}
	}
}

// TestC7UpdateFailureNamesTheCauseAndLeavesNoRestart proves C7.
func TestC7UpdateFailureNamesTheCauseAndLeavesNoRestart(t *testing.T) {
	restart := &codeupdate.Plan{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", UpdateRunning: "v0.1.1", Restart: restart,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.2.0"}, nil
		},
		InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{}, errors.New("the checksum did not match")
		},
	})
	drive(t, a, a.slash("/update")())
	if a.updateInFlight || restart.Path != "" || !strings.Contains(updateNotes(a), "the checksum did not match") || !strings.Contains(updateNotes(a), codeupdate.CurlCommand) {
		t.Fatalf("in flight = %t restart = %+v notes:\n%s", a.updateInFlight, restart, updateNotes(a))
	}
}

// TestC8UpdateRefusesWhileATurnOrTaskIsRunning proves C8.
// TestC8AnUpdateInstallsWhileATurnOrTaskIsRunning proves C8 under the new law:
// an install never interrupts work and never refuses because work is happening.
// The file is replaced in the background and the turn or task is untouched.
func TestC8AnUpdateInstallsWhileATurnOrTaskIsRunning(t *testing.T) {
	for _, kind := range []string{"turn", "task"} {
		t.Run(kind, func(t *testing.T) {
			installed := 0
			a := newTestApp(&fakeAgent{model: "test/model"})
			a.updateRunning = "v0.1.1"
			a.restart = &codeupdate.Plan{}
			a.resolveUpdate = func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
				return codeupdate.Release{Tag: "v0.2.0"}, nil
			}
			a.installUpdate = func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
				installed++
				return codeupdate.InstallResult{Release: codeupdate.Release{Tag: "v0.2.0"}, Path: "/tmp/codeaf"}, nil
			}
			if kind == "turn" {
				a.state = stateWorking
			} else {
				a.tasks = map[uint64]*taskNode{1: {id: 1, state: session.TaskRunning}}
				a.taskOrder = []uint64{1}
			}
			command := a.slash("/update")
			if command == nil {
				t.Fatal("an update was refused because work was running")
			}
			drive(t, a, command())
			if installed != 1 || a.updateInFlight || a.restart.Path != "" {
				t.Fatalf("installed = %d in flight = %t restart = %+v notes:\n%s", installed, a.updateInFlight, a.restart, updateNotes(a))
			}
			if strings.Contains(updateNotes(a), "finish the running turn or task") {
				t.Fatalf("the old refusal is still spoken:\n%s", updateNotes(a))
			}
		})
	}
}

// V6: Bare /update follows dev and staging builds, stable and rc builds follow
// stable, and an explicit channel or tag overrides that default.
func TestV6UpdateChoiceFollowsTheRunningBuildUnlessOverridden(t *testing.T) {
	for _, row := range []struct {
		name, running, argument string
		want                    codeupdate.Choice
	}{
		{"dev default", "dev-20260921-aaaaaaaaaaaa", "", codeupdate.Choice{Channel: "dev", Running: "dev-20260921-aaaaaaaaaaaa"}},
		{"staging default", "staging-20260921-aaaaaaaaaaaa", "", codeupdate.Choice{Channel: "staging", Running: "staging-20260921-aaaaaaaaaaaa"}},
		{"stable default", "v0.3.0", "", codeupdate.Choice{Channel: "stable", Running: "v0.3.0"}},
		{"rc default", "v0.3.0-rc.1", "", codeupdate.Choice{Channel: "stable", Running: "v0.3.0-rc.1"}},
		{"explicit staging", "dev-20260921-aaaaaaaaaaaa", "staging", codeupdate.Choice{Channel: "staging", Running: "dev-20260921-aaaaaaaaaaaa"}},
		{"explicit stable", "dev-20260921-aaaaaaaaaaaa", "stable", codeupdate.Choice{Channel: "stable", Running: "dev-20260921-aaaaaaaaaaaa"}},
		{"explicit tag", "dev-20260921-aaaaaaaaaaaa", "dev-20260918-bbbbbbbbbbbb", codeupdate.Choice{Version: "dev-20260918-bbbbbbbbbbbb", Running: "dev-20260921-aaaaaaaaaaaa"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var got codeupdate.Choice
			a := newApp(context.Background(), Options{
				Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab",
				UpdateRunning: row.running, Restart: &codeupdate.Plan{},
				ResolveUpdate: func(_ context.Context, choice codeupdate.Choice) (codeupdate.Release, error) {
					got = choice
					return codeupdate.Release{Tag: row.running}, nil
				},
				InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
					return codeupdate.InstallResult{}, nil
				},
			})
			command := a.runUpdateCommand(row.argument)
			if command == nil {
				t.Fatal("update returned no command")
			}
			_ = command()
			if got != row.want {
				t.Fatalf("choice = %+v, want %+v", got, row.want)
			}
		})
	}
}

// V7: A dev build ahead of the API-selected dev release refuses a bare
// downgrade, while naming the tag remains the deliberate install road.
func TestV7ChatUpdateRefusesAnAheadChannelBuildUnlessTheTagIsNamed(t *testing.T) {
	const running = "dev-20260921-bbbbbbbbbbbb"
	const newest = "dev-20260918-aaaaaaaaaaaa"
	installed := 0
	newAppFor := func() *app {
		return newApp(context.Background(), Options{
			Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", SessionFile: "/tmp/this.jsonl",
			UpdateRunning: running, Restart: &codeupdate.Plan{},
			ResolveUpdate: func(_ context.Context, choice codeupdate.Choice) (codeupdate.Release, error) {
				if choice.Version != "" {
					return codeupdate.Release{Tag: choice.Version}, nil
				}
				return codeupdate.Release{Tag: newest}, nil
			},
			InstallUpdate: func(_ context.Context, options codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
				installed++
				return codeupdate.InstallResult{Release: options.Release, Path: "/tmp/devaf"}, nil
			},
		})
	}
	ahead := newAppFor()
	drive(t, ahead, ahead.slash("/update")())
	want := "this codeaf is " + running + ", ahead of the newest dev " + newest + " — /update " + newest + " installs it anyway"
	if installed != 0 || !strings.Contains(updateNotes(ahead), want) {
		t.Fatalf("installed = %d notes:\n%s", installed, updateNotes(ahead))
	}
	named := newAppFor()
	drive(t, named, named.slash("/update "+newest)())
	if installed != 1 {
		t.Fatalf("named tag installed %d times", installed)
	}
}

// V8: /update failures use the running file's curl road, while a source-build
// refusal keeps the stable codeaf CurlCommand.
func TestV8UpdateNotesUseTheRightCurlLine(t *testing.T) {
	const devafCurl = "curl -fsSL https://agentfield.ai/get/devaf | bash"
	failing := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab",
		UpdateRunning: "dev-20260918-aaaaaaaaaaaa", UpdateCurl: devafCurl, Restart: &codeupdate.Plan{},
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{}, errors.New("release service is away")
		},
		InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{}, nil
		},
	})
	drive(t, failing, failing.slash("/update")())
	if !strings.Contains(updateNotes(failing), "install a release with: "+devafCurl) {
		t.Fatalf("failure notes:\n%s", updateNotes(failing))
	}

	source := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab",
		UpdateRunning: "deadbeef", UpdateCurl: devafCurl, Restart: &codeupdate.Plan{},
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) { return codeupdate.Release{}, nil },
		InstallUpdate: func(context.Context, codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			return codeupdate.InstallResult{}, nil
		},
	})
	if command := source.slash("/update"); command != nil {
		t.Fatal("source refusal returned a command")
	}
	if !strings.Contains(updateNotes(source), codeupdate.CurlCommand) || strings.Contains(updateNotes(source), devafCurl) {
		t.Fatalf("source notes:\n%s", updateNotes(source))
	}
}

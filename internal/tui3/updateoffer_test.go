package tui3

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

func offerAuto(state *codeupdate.AutoState) *UpdateCoordinator {
	return &UpdateCoordinator{
		Enabled: true,
		State:   func() codeupdate.AutoState { return *state },
		Dismiss: func(tag string) error { state.DismissedRelease = tag; return nil },
		Failure: func(tag string, err error) { state.FailedRelease = tag; state.Failures++ },
		Success: func() { state.FailedRelease = ""; state.Failures = 0 },
		Disable: func() error { return nil },
	}
}

type offerLab struct {
	app       *app
	installed int
	result    codeupdate.InstallResult
	failWith  error
	allow     *bool
}

func newOfferLab(t *testing.T, coordinator *UpdateCoordinator) *offerLab {
	t.Helper()
	lab := &offerLab{allow: new(bool)}
	lab.app = newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", UpdateRunning: "v0.9.2",
		Restart: &codeupdate.Plan{}, UpdateAuto: coordinator,
		ResolveUpdate: func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.9.3"}, nil
		},
		InstallUpdate: func(_ context.Context, options codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			*lab.allow = options.AllowDowngrade
			if lab.failWith != nil {
				return codeupdate.InstallResult{}, lab.failWith
			}
			lab.installed++
			if lab.result.Release.Tag != "" || lab.result.Already {
				return lab.result, nil
			}
			return codeupdate.InstallResult{Release: options.Release, Path: "/tmp/codeaf"}, nil
		},
	})
	return lab
}

func offerCheck(tag string) updateCheckMsg {
	return updateCheckMsg{available: codeupdate.Available{Latest: tag, Running: "v0.9.2"}, show: true}
}

// TestLaunchOfferInstallsInBackgroundAfterTheGrace proves the default road.
func TestLaunchOfferInstallsInBackgroundAfterTheGrace(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	a := lab.app
	drive(t, a, offerCheck("v0.9.3"))
	if !a.offer.offering() || lab.installed != 0 {
		t.Fatalf("offer = %+v installed = %d", a.offer, lab.installed)
	}
	drive(t, a, updateGraceMsg{})
	if lab.installed != 1 || a.updateInFlight || a.restart.Path != "" {
		t.Fatalf("installed = %d in flight = %t restart = %+v", lab.installed, a.updateInFlight, a.restart)
	}
	if !strings.Contains(updateNotes(a), "codeaf v0.9.3 installed") {
		t.Fatalf("the completion was not said:\n%s", updateNotes(a))
	}
	if !a.offer.active() || a.offer.phase != updateReady {
		t.Fatalf("the offer did not settle ready: %+v", a.offer)
	}
	// M2: the ready line folds away; the durable note remains.
	drive(t, a, offerSettleMsg{})
	if a.offer.active() {
		t.Fatalf("the ready line never folded: %+v", a.offer)
	}
	if !strings.Contains(updateNotes(a), "codeaf v0.9.3 installed") {
		t.Fatalf("the note did not survive the fold:\n%s", updateNotes(a))
	}
}

// TestSkipOnlyAnswersTheOfferingRelease proves H2 and the wording.
func TestSkipOnlyAnswersTheOfferingRelease(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	a := lab.app
	drive(t, a, offerCheck("v0.9.3"))
	drive(t, a, key("alt+n"))
	if lab.installed != 0 || a.offer.active() || state.DismissedRelease != "v0.9.3" {
		t.Fatalf("installed = %d offer = %+v dismissed = %q", lab.installed, a.offer, state.DismissedRelease)
	}
	if !strings.Contains(updateNotes(a), "skipped codeaf v0.9.3") ||
		!strings.Contains(updateNotes(a), "newer releases will still be offered") {
		t.Fatalf("the skip note is unclear:\n%s", updateNotes(a))
	}
}

// TestSkipCannotCancelARunningInstall proves the honest answer while a download
// is already in flight.
func TestSkipCannotCancelARunningInstall(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	a := lab.app
	drive(t, a, offerCheck("v0.9.3"))
	// The grace expiring starts the install; the seam answers on the next drive.
	drive(t, a, updateGraceMsg{})
	if lab.installed == 0 {
		t.Fatal("the background install did not run")
	}
	// A person types skip afterwards: it must not claim to have cancelled.
	command := a.slash("/update skip")
	_ = command
	if !strings.Contains(updateNotes(a), "already running") {
		t.Fatalf("skip lied about a running install:\n%s", updateNotes(a))
	}
}

// TestAutoOffShowsOneQuietNoticeAndNeverInstalls proves the quiet indicator.
func TestAutoOffShowsOneQuietNoticeAndNeverInstalls(t *testing.T) {
	coordinator := offerAuto(&codeupdate.AutoState{})
	coordinator.Enabled = false
	lab := newOfferLab(t, coordinator)
	drive(t, lab.app, offerCheck("v0.9.3"))
	if lab.app.offer.active() || lab.installed != 0 {
		t.Fatalf("offer = %+v installed = %d with auto off", lab.app.offer, lab.installed)
	}
	if !strings.Contains(updateNotes(lab.app), "codeaf v0.9.3 is out") || !strings.Contains(updateNotes(lab.app), "/update installs it for the next launch") {
		t.Fatalf("the quiet notice is missing:\n%s", updateNotes(lab.app))
	}
}

// TestGraceRechecksTheLiveSettingAndMemory proves H1: a setting turned off, or a
// dismissal written by another window, inside the window must stop the install.
func TestGraceRechecksTheLiveSettingAndMemory(t *testing.T) {
	t.Run("setting turned off", func(t *testing.T) {
		coordinator := offerAuto(&codeupdate.AutoState{})
		lab := newOfferLab(t, coordinator)
		drive(t, lab.app, offerCheck("v0.9.3"))
		coordinator.Enabled = false
		drive(t, lab.app, updateGraceMsg{})
		if lab.installed != 0 || lab.app.offer.active() {
			t.Fatalf("installed %d after the setting went off", lab.installed)
		}
		if !strings.Contains(updateNotes(lab.app), "auto update is off") {
			t.Fatalf("the refusal was silent:\n%s", updateNotes(lab.app))
		}
	})
	t.Run("dismissed elsewhere", func(t *testing.T) {
		state := codeupdate.AutoState{}
		lab := newOfferLab(t, offerAuto(&state))
		drive(t, lab.app, offerCheck("v0.9.3"))
		// Another window writes the dismissal while our grace runs.
		state.DismissedRelease = "v0.9.3"
		drive(t, lab.app, updateGraceMsg{})
		if lab.installed != 0 || lab.app.offer.active() {
			t.Fatalf("installed %d after another window dismissed it", lab.installed)
		}
		if !strings.Contains(updateNotes(lab.app), "will not be installed on its own") {
			t.Fatalf("the refusal was silent:\n%s", updateNotes(lab.app))
		}
	})
}

// TestOfferHintCountsDownAndKeepsItsActionsAtEightyColumns proves M3/M4 on the
// footer the person actually reads.
func TestOfferHintCountsDownAndKeepsItsActionsAtEightyColumns(t *testing.T) {
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	a := lab.app
	fixed := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return fixed }
	a.offer.raise("v0.9.3", "v0.9.2", fixed)
	line := a.updateOfferHint(80)
	for _, want := range []string{a.chords.say(updateSkipKey), "/update", "10s"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the 80-column offer dropped %q:\n%s", want, line)
		}
	}
	// The count is live, not frozen: three seconds later it reads 7s.
	a.clock = func() time.Time { return fixed.Add(3 * time.Second) }
	if later := a.updateOfferHint(80); !strings.Contains(later, "7s") {
		t.Fatalf("the countdown did not move:\n%s", later)
	}
	// The opening note names the grace once, for the record.
	if note := a.updateOfferNote(); !strings.Contains(note, "installs on its own shortly") {
		t.Fatalf("the note lost the grace:\n%s", note)
	}
}

// TestTheWorkingOfferKeepsItsSkipWhileTheCountdownRuns proves M3/M4 over a
// running turn: at a narrow width the OFFER's clause survives the shortening and
// what is given up first is the turn's leftmost clause. A countdown that ran
// with its only control shortened away would be a ten-second timer nobody could
// answer.
func TestTheWorkingOfferKeepsItsSkipWhileTheCountdownRuns(t *testing.T) {
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	a := lab.app
	a.state = stateWorking
	a.turn = 1
	a.input.setText("keep going")
	a.offer.raise("v0.9.3", "v0.9.2", a.now())
	full := a.runHint()
	if full == "" || !strings.Contains(full, "esc interrupt") {
		t.Fatalf("the working hint is not the turn's own: %q", full)
	}
	composed := a.withUpdateOffer(full)
	if !strings.Contains(composed, updateOfferDeferWords) {
		t.Fatalf("the defer clause is not composed:\n%s", composed)
	}
	shortened := a.hintShorter(composed)
	if !strings.Contains(shortened, updateOfferDeferWords) {
		t.Fatalf("the offer's skip was dropped while the countdown ran:\n%s", shortened)
	}
	if len(shortened) >= len(composed) {
		t.Fatalf("the line did not shorten: %q", shortened)
	}
	if !strings.Contains(shortened, "esc interrupt") {
		t.Fatalf("the key that stops the turn went before the offer's clause: %q", shortened)
	}
	// SHORTENING ALWAYS MAKES PROGRESS, so a narrow frame cannot spin here, and
	// the offer's clause is the last thing to go before the turn's own key.
	line := shortened
	for steps := 0; steps < 12; steps++ {
		next := a.hintShorter(line)
		if next == line {
			t.Fatalf("shortening stopped making progress at %q", line)
		}
		line = next
		if line == "" {
			return
		}
	}
	t.Fatalf("shortening never emptied the row: %q", line)
}

// TestPlainLettersNeverAnswerTheOffer proves the composer keeps its typing.
func TestPlainLettersNeverAnswerTheOffer(t *testing.T) {
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	a := lab.app
	drive(t, a, offerCheck("v0.9.3"))
	drive(t, a, key("y"))
	drive(t, a, key("n"))
	if got := a.input.String(); got != "yn" {
		t.Fatalf("the offer ate the typing: %q", got)
	}
	if !a.offer.offering() || lab.installed != 0 {
		t.Fatalf("a letter answered the offer: %+v installed = %d", a.offer, lab.installed)
	}
}

// TestARefusalIsQuietAndNeverCounted proves the typed refusal path.
func TestARefusalIsQuietAndNeverCounted(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	lab.failWith = &codeupdate.RefusalError{Reason: "codeaf v0.9.4 is already the build on disk, newer than v0.9.3"}
	drive(t, lab.app, offerCheck("v0.9.3"))
	drive(t, lab.app, updateGraceMsg{})
	if state.Failures != 0 || state.FailedRelease != "" {
		t.Fatalf("a refusal was counted as a failure: %+v", state)
	}
	if !strings.Contains(updateNotes(lab.app), "newer than v0.9.3") {
		t.Fatalf("the refusal was not said:\n%s", updateNotes(lab.app))
	}
}

// TestAnAlreadyInstalledReleaseIsQuietSuccess proves the no-op wording.
func TestAnAlreadyInstalledReleaseIsQuietSuccess(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	lab.result = codeupdate.InstallResult{Release: codeupdate.Release{Tag: "v0.9.3"}, Path: "/tmp/codeaf", Already: true}
	drive(t, lab.app, offerCheck("v0.9.3"))
	drive(t, lab.app, updateGraceMsg{})
	if !strings.Contains(updateNotes(lab.app), "already the build on disk") {
		t.Fatalf("the no-op was not said:\n%s", updateNotes(lab.app))
	}
	if lab.app.offer.active() {
		t.Fatalf("a no-op raised an offer: %+v", lab.app.offer)
	}
}

// TestOnlyANamedTagAllowsADowngrade proves the automatic road never rolls back.
func TestOnlyANamedTagAllowsADowngrade(t *testing.T) {
	// The automatic road: no downgrade permission.
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	drive(t, lab.app, offerCheck("v0.9.3"))
	drive(t, lab.app, updateGraceMsg{})
	if *lab.allow {
		t.Fatal("the automatic road asked to be allowed to downgrade")
	}
	// A typed exact tag: the person asked for that release by name.
	named := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	named.app.resolveUpdate = func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
		return codeupdate.Release{Tag: "v0.9.0"}, nil
	}
	drive(t, named.app, named.app.slash("/update v0.9.0")())
	if !*named.allow || named.installed != 1 {
		t.Fatalf("an explicit tag did not allow its own rollback: allow = %t installed = %d", *named.allow, named.installed)
	}
}

// TestThePackagedInstallIsRefusedBeforeAnyOffer proves the ownership exclusion.
func TestThePackagedInstallIsRefusedBeforeAnyOffer(t *testing.T) {
	coordinator := offerAuto(&codeupdate.AutoState{})
	coordinator.Refusal = func() string { return "this codeaf is managed by Homebrew" }
	lab := newOfferLab(t, coordinator)
	drive(t, lab.app, offerCheck("v0.9.3"))
	if lab.app.offer.active() || lab.installed != 0 {
		t.Fatalf("offer = %+v installed = %d for a managed file", lab.app.offer, lab.installed)
	}
	if command := lab.app.slash("/update"); command != nil || lab.installed != 0 {
		t.Fatalf("the typed road installed a managed file: %v", command)
	}
	if !strings.Contains(updateNotes(lab.app), "managed by Homebrew") {
		t.Fatalf("the refusal was not said:\n%s", updateNotes(lab.app))
	}
}

// TestUpdateDemoFixturesDrawEveryState is the deterministic render entrypoint.
func TestUpdateDemoFixturesDrawEveryState(t *testing.T) {
	for _, name := range []string{"offer", "downloading", "ready", "ready-working", "manual", "off", "failure"} {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "test/model"})
			a.openDemoUpdate(func(key string) string {
				if key == updateDemoEnv {
					return name
				}
				return ""
			})
			if a.updateCheck != nil || a.resolveUpdate != nil || a.installUpdate != nil || a.updateAuto != nil {
				t.Fatal("a fixture left an updater hook armed")
			}
			if !strings.Contains(updateNotes(a), "fixture · codeaf update states") ||
				!strings.Contains(updateNotes(a), "CODEAF_UPDATE_DEMO="+name) {
				t.Fatalf("the fixture is not labelled:\n%s", updateNotes(a))
			}
			switch name {
			case "offer":
				if !a.offer.offering() || !strings.Contains(a.updateOfferHint(120), "is out") {
					t.Fatalf("offer fixture = %+v", a.offer)
				}
				home := plain(a.homeFootLine(80, a.pal))
				for _, want := range []string{a.chords.say(updateSkipKey), "/update"} {
					if !strings.Contains(home, want) {
						t.Fatalf("home's 80-column keys row dropped %q: %q", want, home)
					}
				}
			case "downloading", "manual":
				// BOTH ROADS DRAW THE SAME LINE; the case name is what tells
				// the frame which one it raised (the label is written last).
				if a.offer.phase != updateDownloading {
					t.Fatalf("download fixture = %+v", a.offer)
				}
			case "ready":
				if a.offer.phase != updateReady || !strings.Contains(updateNotes(a), "installed") {
					t.Fatalf("ready fixture = %+v", a.offer)
				}
			case "ready-working":
				if a.offer.phase != updateReady || a.state != stateWorking || a.input.String() == "" {
					t.Fatalf("working fixture state = %v offer = %+v draft = %q", a.state, a.offer, a.input.String())
				}
			case "off":
				if a.offer.active() || !strings.Contains(updateNotes(a), "is out") {
					t.Fatalf("off fixture = %+v", a.offer)
				}
			case "failure":
				if a.offer.phase != updateFailed {
					t.Fatalf("failure fixture = %+v", a.offer)
				}
			}
		})
	}
}

// TestEveryUpdateCommandWeShowIsParsed guards the copy: a hint or note may only
// print commands the parser accepts as themselves rather than as a tag.
func TestEveryUpdateCommandWeShowIsParsed(t *testing.T) {
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	a := lab.app
	a.offer.raise("v0.9.3", "v0.9.2", a.now())
	text := strings.Join([]string{
		a.updateOfferHint(120),
		a.updateOfferNote(),
		a.withUpdateOffer("esc interrupt"),
		quietUpdateNotice(codeupdate.Available{Latest: "v0.9.3", Running: "v0.9.2"}),
	}, "\n")
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, "·,;")
		if !strings.HasPrefix(field, "/update") {
			continue
		}
		argument := strings.TrimSpace(strings.TrimPrefix(field, "/update"))
		switch argument {
		case "", "skip", "never":
			// The three forms the parser answers as themselves.
		default:
			t.Fatalf("the surface prints %q, which the parser would read as a tag", field)
		}
	}
	if !strings.Contains(text, "/update") {
		t.Fatal("no /update road is shown at all")
	}
}

// productionAuto wires the automatic updater the way cmd/codeaf does: every
// answer comes out of the PROFILE, so two windows in one test share one row,
// one remembered dismissal and one install lock. A mirror of these callbacks
// would prove nothing about the two-window case.
func productionAuto(profile string) *UpdateCoordinator {
	return &UpdateCoordinator{
		Enabled:     config.UpdateAutoAt(profile),
		EnabledLive: func() bool { return config.UpdateAutoAt(profile) },
		State:       func() codeupdate.AutoState { return codeupdate.LoadAutoState(profile) },
		Dismiss:     func(tag string) error { return codeupdate.DismissRelease(profile, tag) },
		Failure:     func(tag string, failure error) { codeupdate.AutoFailure(profile, tag, failure) },
		Success:     func() { codeupdate.AutoSuccess(profile) },
		Disable:     func() error { return config.SaveUpdateAuto(profile, false) },
	}
}

// TestTwoWindowsSharingAProfileAnswerTheAutomaticRoad is the shared-profile
// regression: two windows on ONE profile, wired the way production wires them,
// so a row turned off \u2014 or a release skipped \u2014 in the other window stops THIS
// window's countdown before it downloads anything.
func TestTwoWindowsSharingAProfileAnswerTheAutomaticRoad(t *testing.T) {
	t.Run("the other window turns the row off", func(t *testing.T) {
		profile := t.TempDir()
		lab := newOfferLab(t, productionAuto(profile))
		drive(t, lab.app, offerCheck("v0.9.3"))
		if !lab.app.offer.offering() {
			t.Fatal("the launch check raised no offer")
		}
		// The other window's /settings, or `/update never` there.
		if err := config.SaveUpdateAuto(profile, false); err != nil {
			t.Fatal(err)
		}
		drive(t, lab.app, updateGraceMsg{})
		if lab.installed != 0 || lab.app.offer.active() {
			t.Fatalf("the other window's row was ignored: installed = %d offer = %+v", lab.installed, lab.app.offer)
		}
		if !strings.Contains(updateNotes(lab.app), "auto update is off") {
			t.Fatalf("the withdrawal was silent:\n%s", updateNotes(lab.app))
		}
	})
	t.Run("the other window skips this release", func(t *testing.T) {
		profile := t.TempDir()
		lab := newOfferLab(t, productionAuto(profile))
		drive(t, lab.app, offerCheck("v0.9.3"))
		if !lab.app.offer.offering() {
			t.Fatal("the launch check raised no offer")
		}
		if err := codeupdate.DismissRelease(profile, "v0.9.3"); err != nil {
			t.Fatal(err)
		}
		// The countdown's own beat, which asks the same live question.
		drive(t, lab.app, updateOfferTickMsg{})
		if lab.installed != 0 || lab.app.offer.active() {
			t.Fatalf("the other window's dismissal was ignored: installed = %d offer = %+v", lab.installed, lab.app.offer)
		}
		if !strings.Contains(updateNotes(lab.app), "will not be installed on its own") {
			t.Fatalf("the withdrawal was silent:\n%s", updateNotes(lab.app))
		}
	})
	t.Run("the row goes off while the resolver is out", func(t *testing.T) {
		profile := t.TempDir()
		lab := newOfferLab(t, productionAuto(profile))
		a := lab.app
		drive(t, a, offerCheck("v0.9.3"))
		command := a.tookUpdateGrace()
		if command == nil || !a.updateInFlight {
			t.Fatalf("the deadline did not start the resolver: cmd = %v in flight = %t", command, a.updateInFlight)
		}
		// The resolver is off-frame; the other window answers in the gap.
		if err := config.SaveUpdateAuto(profile, false); err != nil {
			t.Fatal(err)
		}
		if cmd := a.tookUpdateResolve(updateResolveMsg{
			release: codeupdate.Release{Tag: "v0.9.3"}, auto: true,
		}); cmd != nil {
			t.Fatalf("a withdrawn automatic resolve returned work: %v", cmd)
		}
		if lab.installed != 0 || a.updateInFlight || a.offer.active() {
			t.Fatalf("installed = %d in flight = %t offer = %+v", lab.installed, a.updateInFlight, a.offer)
		}
		if !strings.Contains(updateNotes(a), "auto update is off") {
			t.Fatalf("the withdrawal was silent:\n%s", updateNotes(a))
		}
	})
	t.Run("this window's own row writes the file the reader reads", func(t *testing.T) {
		profile := t.TempDir()
		lab := newOfferLab(t, productionAuto(profile))
		if !lab.app.updateAutoEnabledNow() {
			t.Fatal("the default row did not read as on")
		}
		if cmd := lab.app.runUpdateCommand("never"); cmd != nil {
			t.Fatalf("/update never returned work: %v", cmd)
		}
		if config.UpdateAutoAt(profile) || lab.app.updateAutoEnabledNow() {
			t.Fatalf("the offer's /update never did not reach the row: file = %t live = %t",
				config.UpdateAutoAt(profile), lab.app.updateAutoEnabledNow())
		}
	})
}

// TestTheGraceWaitsWhereTheOfferCannotBeAnswered proves the pause's scope: a
// page or layer that owns the keyboard must not consume the ten seconds
// invisibly, while the conversation, home and a RUNNING TURN must keep counting
// (the running turn is the whole reason the countdown exists).
func TestTheGraceWaitsWhereTheOfferCannotBeAnswered(t *testing.T) {
	hidden := []struct {
		name string
		seat func(*app)
	}{
		{"the settings page", func(a *app) { a.raisePlace(pageSettings) }},
		{"the memory page", func(a *app) { a.raisePlace(pageMemory) }},
		{"the standing page", func(a *app) { a.raisePlace(pageStanding) }},
		{"the first-run sheet", func(a *app) { a.setup.open = true }},
		{"a team's card", func(a *app) { a.tsheet.on = true }},
		{"the move picker", func(a *app) { a.tmove.on = true }},
		{"the wall", func(a *app) { a.wall.on = true }},
		{"the team menu", func(a *app) { a.teamMenu.on = true }},
		{"the provider panel", func(a *app) { a.addPanel.open = true }},
		{"a question page", func(a *app) { a.qroom = &questionRoom{} }},
	}
	for _, row := range hidden {
		t.Run("waits under "+row.name, func(t *testing.T) {
			lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
			a := lab.app
			a.offer.raise("v0.9.3", "v0.9.2", a.now())
			row.seat(a)
			a.offer.deadline = a.now().Add(-time.Second)
			if cmd := a.tookOfferTick(); cmd == nil || !a.offer.offering() {
				t.Fatalf("the grace was spent under %s: cmd = %v offer = %+v", row.name, cmd, a.offer)
			}
			if !a.offer.deadline.After(a.now()) {
				t.Fatalf("the deadline was not moved on under %s: %s", row.name, a.offer.deadline)
			}
			if lab.installed != 0 {
				t.Fatalf("%s installed a release", row.name)
			}
		})
	}
	counting := []struct {
		name string
		seat func(*app)
	}{
		{"the conversation", func(a *app) {}},
		{"home", func(a *app) { a.raisePlace(pageHome) }},
		{"a running turn", func(a *app) { a.state = stateWorking; a.turn = 1; a.input.setText("work") }},
	}
	for _, row := range counting {
		t.Run("counts over "+row.name, func(t *testing.T) {
			lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
			a := lab.app
			a.offer.raise("v0.9.3", "v0.9.2", a.now())
			row.seat(a)
			a.offer.deadline = a.now().Add(-time.Second)
			if cmd := a.tookOfferTick(); cmd == nil || !a.updateInFlight {
				t.Fatalf("the deadline did not fire over %s: cmd = %v in flight = %t", row.name, cmd, a.updateInFlight)
			}
		})
	}
}

// TestBareUpdateDuringTheOfferDoesNotRollBackAFileAnotherWindowAdvanced is the
// /update resolution boundary, exercised through the REAL installer: the offer
// is about v0.9.3 while the file on disk already holds v0.9.4, because another
// window installed it. A bare `/update` \u2014 the offer's own "now", which pinned
// the offered tag itself \u2014 must not step the disk back; `/update v0.9.3` typed
// by hand may, because that is a person naming the release.
func TestBareUpdateDuringTheOfferDoesNotRollBackAFileAnotherWindowAdvanced(t *testing.T) {
	older, newer := []byte("v0.9.3 bytes"), []byte("v0.9.4 bytes")
	olderSum, newerSum := sha256.Sum256(older), sha256.Sum256(newer)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		asset, sum := newer, newerSum
		if strings.Contains(request.URL.Path, "v0.9.3") {
			asset, sum = older, olderSum
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", sum, runtime.GOOS, runtime.GOARCH)
		case strings.HasSuffix(request.URL.Path, "/codeaf-"+runtime.GOOS+"-"+runtime.GOARCH):
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}

	// labFor gives a window whose installer is the SHARED one, writing to a
	// temporary executable that another window has already advanced to v0.9.4.
	labFor := func(t *testing.T, running string) (*offerLab, string) {
		t.Helper()
		target := filepath.Join(t.TempDir(), "codeaf")
		if err := os.WriteFile(target, []byte("something older"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := codeupdate.Install(context.Background(), codeupdate.InstallOptions{
			Client: client, Release: codeupdate.Release{Tag: "v0.9.4"}, Target: target,
		}); err != nil {
			t.Fatalf("the other window's install: %v", err)
		}
		lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
		lab.app.updateRunning = running
		lab.app.resolveUpdate = func(_ context.Context, choice codeupdate.Choice) (codeupdate.Release, error) {
			tag := "v0.9.3"
			if strings.TrimSpace(choice.Version) != "" {
				tag = strings.TrimSpace(choice.Version)
			}
			return codeupdate.Release{Tag: tag}, nil
		}
		lab.app.installUpdate = func(_ context.Context, options codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
			options.Client = client
			options.Target = target
			return codeupdate.Install(context.Background(), options)
		}
		return lab, target
	}

	t.Run("bare /update answered from the offer", func(t *testing.T) {
		lab, target := labFor(t, "v0.9.2")
		drive(t, lab.app, offerCheck("v0.9.3"))
		if !lab.app.offer.offering() {
			t.Fatal("the launch check raised no offer")
		}
		drive(t, lab.app, lab.app.slash("/update")())
		if onDisk, err := os.ReadFile(target); err != nil || string(onDisk) != string(newer) {
			t.Fatalf("bare /update rolled the file back: %q, %v", onDisk, err)
		}
		if !strings.Contains(updateNotes(lab.app), "newer than v0.9.3") {
			t.Fatalf("the refusal was not said:\n%s", updateNotes(lab.app))
		}
	})

	t.Run("the same release named by hand", func(t *testing.T) {
		lab, target := labFor(t, "v0.9.2")
		if cmd := lab.app.slash("/update v0.9.3"); cmd == nil {
			t.Fatal("/update v0.9.3 built no command")
		} else {
			drive(t, lab.app, cmd())
		}
		if onDisk, err := os.ReadFile(target); err != nil || string(onDisk) != string(older) {
			t.Fatalf("a named tag did not reach the installer: %q, %v", onDisk, err)
		}
		if !strings.Contains(updateNotes(lab.app), "checksum matched") {
			t.Fatalf("the hand-run completion was not said:\n%s", updateNotes(lab.app))
		}
	})

	t.Run("a named tag equal to the running stamp", func(t *testing.T) {
		// THIS WINDOW RUNS v0.9.3 AND THE FILE HOLDS v0.9.4: the stamp is not
		// the disk, so the request must reach the installer instead of the
		// quiet "newest" no-op.
		lab, target := labFor(t, "v0.9.3")
		if cmd := lab.app.slash("/update v0.9.3"); cmd == nil {
			t.Fatal("/update v0.9.3 built no command")
		} else {
			drive(t, lab.app, cmd())
		}
		if onDisk, err := os.ReadFile(target); err != nil || string(onDisk) != string(older) {
			t.Fatalf("a named tag equal to the running stamp never reached the installer: %q, %v", onDisk, err)
		}
		if strings.Contains(updateNotes(lab.app), "you are on the newest codeaf") {
			t.Fatalf("the process stamp was mistaken for the file on disk:\n%s", updateNotes(lab.app))
		}
	})
}

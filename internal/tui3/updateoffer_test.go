package tui3

import (
	"context"
	"crypto/sha256"
	"errors"
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
	// A PROFILE OF ITS OWN, so this lab never sees a marker another test wrote
	// (testprofilelaw_test.go). The two-window tests share their profile through
	// the Coordinator's own callbacks ([productionAuto]) and write the file the
	// other window writes, so no second surface needs the same directory here.
	lab.app = newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", UpdateRunning: "v0.9.2",
		Restart: &codeupdate.Plan{}, UpdateAuto: coordinator, ProfileDir: t.TempDir(),
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
		// THE SHEETS THE FOOT REPLACES THE OFFER'S SENTENCE WITH, or that take
		// the frame whole (the phone tier's status sheet and tool detail).
		{"the model picker", func(a *app) { a.pick.open = true }},
		{"the thinking ladder", func(a *app) { a.effPick.open = true }},
		{"the session picker", func(a *app) { a.roster.open = true }},
		{"the folder chooser", func(a *app) { a.folder.open = true }},
		{"the status sheet", func(a *app) { a.width = phoneWidth; a.deck.open = true }},
		{"the tool detail", func(a *app) { a.width = phoneWidth; a.expand.open = true }},
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
			// THE GRACE IS GIVEN BACK WHOLE, not merely kept alive: the clock
			// restarts at the full window from the moment the offer is readable
			// again, so a person who leaves the sheet gets the whole countdown.
			if left := a.offer.deadline.Sub(a.now()); left <= 0 || left > codeupdate.AutoGrace || left < codeupdate.AutoGrace-time.Second {
				t.Fatalf("the grace did not restart under %s: %s left, want about %s", row.name, left, codeupdate.AutoGrace)
			}
			if lab.installed != 0 {
				t.Fatalf("%s installed a release", row.name)
			}
		})
	}
	counting := []struct {
		name string
		seat func(*app)
		keys func(*app, int) string
	}{
		{"the conversation", func(a *app) {}, func(a *app, width int) string { return a.footHint(width) }},
		{"home", func(a *app) { a.raisePlace(pageHome) }, func(a *app, width int) string { return a.homeFootLine(width, a.pal) }},
		{"a running turn", func(a *app) { a.state = stateWorking; a.turn = 1; a.input.setText("work") }, func(a *app, width int) string { return a.footHint(width) }},
	}
	for _, row := range counting {
		t.Run("counts over "+row.name, func(t *testing.T) {
			lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
			a := lab.app
			a.offer.raise("v0.9.3", "v0.9.2", a.now())
			row.seat(a)
			// THE CONTROL STAYS ON THE ROW WHERE THE CLOCK KEEPS RUNNING: a
			// countdown that cannot be skipped is a countdown a person cannot
			// answer, which is the whole reason the pause list exists.
			if hint := row.keys(a, 200); !strings.Contains(hint, "skip") {
				t.Fatalf("the offer's control left the keys row over %s: %q", row.name, hint)
			}
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

// TestAFailedAutomaticInstallKeepsItsTagAndStopsAfterThree is P2-1 through the
// REAL installer: every error it answers carries an empty
// [codeupdate.InstallResult], so the tag has to survive in the surface or the
// automatic road records no failure against the release — no count, no backoff,
// and a line naming nobody. The release host here answers nothing, which is
// exactly where the empty result comes from.
//
// THE WALL CLOCK IS COMPRESSED, NOT THE COUNTER. Between attempts the test
// clears LastAttemptAt in the profile so the backoff window has passed, the way
// three launches on three days would; the failure count, the release it belongs
// to and the block at the end are the production ones.
func TestAFailedAutomaticInstallKeepsItsTagAndStopsAfterThree(t *testing.T) {
	profile := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.NotFound(w, request)
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}
	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("the running build"), 0o755); err != nil {
		t.Fatal(err)
	}
	const tag = "v0.9.3"
	lab := newOfferLab(t, productionAuto(profile))
	a := lab.app
	var asked []string
	a.resolveUpdate = client.Select
	a.installUpdate = func(install context.Context, options codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
		options.Client = client
		options.Target = target
		asked = append(asked, strings.TrimSpace(options.Release.Tag))
		result, err := codeupdate.Install(install, options)
		if err == nil {
			return result, errors.New("the release host answered an install it should not have")
		}
		// THE INSTALLER'S OWN CONTRACT, ASSERTED HERE: a failure names no
		// release, so a surface reading the result alone loses the tag.
		if result.Release.Tag != "" {
			return result, fmt.Errorf("the installer returned a tagged result on an error: %+v", result)
		}
		return result, err
	}
	// THE FIRST ATTEMPT IS THE PRODUCTION ONE: the launch check raises the offer
	// and the grace runs out.
	drive(t, a, offerCheck(tag))
	if !a.offer.offering() {
		t.Fatal("the launch check raised no offer")
	}
	a.offer.deadline = a.now().Add(-time.Second)
	drive(t, a, updateOfferTickMsg{})
	state := codeupdate.LoadAutoState(profile)
	if state.FailedRelease != tag || state.Failures != 1 || state.LastError == "" {
		t.Fatalf("the failed release was not remembered: %+v", state)
	}
	if !strings.Contains(updateNotes(a), "codeaf "+tag+" could not be installed") {
		t.Fatalf("the failure line did not name the release:\n%s", updateNotes(a))
	}
	// THE BACKOFF IS REAL: the same check on the next launch does not offer it.
	if err := codeupdate.SaveAutoState(profile, state); err != nil {
		t.Fatal(err)
	}
	drive(t, a, offerCheck(tag))
	if a.offer.offering() {
		t.Fatal("a release inside its failure backoff was offered again")
	}
	// TWO MORE FAILURES, the way later launches reach the same release, exhaust
	// the count.
	for attempt := 2; attempt <= 3; attempt++ {
		state := codeupdate.LoadAutoState(profile)
		state.LastAttemptAt = time.Time{} // the backoff window has passed
		if err := codeupdate.SaveAutoState(profile, state); err != nil {
			t.Fatal(err)
		}
		drive(t, a, offerCheck(tag))
		if !a.offer.offering() {
			t.Fatalf("attempt %d was not offered after its backoff", attempt)
		}
		a.offer.deadline = a.now().Add(-time.Second)
		drive(t, a, updateOfferTickMsg{})
		if got := codeupdate.LoadAutoState(profile).Failures; got != attempt {
			t.Fatalf("attempt %d recorded %d failures", attempt, got)
		}
	}
	final := codeupdate.LoadAutoState(profile)
	if !final.Blocked(tag) || codeupdate.ShouldOffer(final, tag, a.now()) {
		t.Fatalf("the exhausted release is still offered: %+v", final)
	}
	drive(t, a, offerCheck(tag))
	if a.offer.offering() {
		t.Fatal("a release that failed out was offered again")
	}
	if len(asked) != 3 {
		t.Fatalf("the installer was asked for %d releases, want 3", len(asked))
	}
	for _, requested := range asked {
		if requested != tag {
			t.Fatalf("an attempt carried %q, want %q", requested, tag)
		}
	}
}

// TestASameDayDevWindowDoesNotStepBackTheBuildAnotherWindowInstalled is P2-3 end
// to end through the REAL resolver and the REAL installer, on the automatic
// road: window A installs dev-b at 11:00 (the record keeps that moment), and
// window B's stale check — cached before b was published — offers dev-a at
// 10:00. AllowDowngrade is false there, so the published ordering the record
// now carries refuses the rollback instead of writing the older file.
func TestASameDayDevWindowDoesNotStepBackTheBuildAnotherWindowInstalled(t *testing.T) {
	const aTag, bTag = "dev-20261006-aaaaaaaaaaaa", "dev-20261006-bbbbbbbbbbbb"
	publishedA := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	publishedB := publishedA.Add(time.Hour)
	aBytes, bBytes := []byte("dev-a bytes"), []byte("dev-b bytes")
	aSum, bSum := sha256.Sum256(aBytes), sha256.Sum256(bBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/releases?") || strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprintf(w, `[{"tag_name":%q,"published_at":%q},{"tag_name":%q,"published_at":%q}]`,
				aTag, publishedA.Format(time.RFC3339), bTag, publishedB.Format(time.RFC3339))
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			sum := bSum
			if strings.Contains(request.URL.Path, aTag) {
				sum = aSum
			}
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", sum, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(request.URL.Path, "/releases/download/"):
			asset := bBytes
			if strings.Contains(request.URL.Path, aTag) {
				asset = aBytes
			}
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL}

	target := filepath.Join(t.TempDir(), "codeaf")
	if err := os.WriteFile(target, []byte("a before-this-morning build"), 0o755); err != nil {
		t.Fatal(err)
	}
	// WINDOW A: the ordinary channel road (what `codeaf update --dev` and a bare
	// /update resolve), so the install record is written with the API's own
	// publish moment for dev-b.
	releaseB, err := client.Select(context.Background(), codeupdate.Choice{Channel: "dev", Running: aTag})
	if err != nil || releaseB.Tag != bTag || !releaseB.PublishedAt.Equal(publishedB) {
		t.Fatalf("the channel resolve = %+v, %v", releaseB, err)
	}
	if _, err := codeupdate.Install(context.Background(), codeupdate.InstallOptions{
		Client: client, Release: releaseB, Target: target,
	}); err != nil {
		t.Fatalf("window A's install: %v", err)
	}

	// WINDOW B: a surface holding the STALE check, with production's own seams
	// pointed at the one shared executable.
	profile := t.TempDir()
	lab := newOfferLab(t, productionAuto(profile))
	a := lab.app
	var resolved codeupdate.Release
	a.resolveUpdate = client.Select
	a.installUpdate = func(install context.Context, options codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
		options.Client = client
		options.Target = target
		resolved = options.Release
		return codeupdate.Install(install, options)
	}
	drive(t, a, updateCheckMsg{available: codeupdate.Available{
		Latest: aTag, Running: bTag, LatestPublished: publishedA, RunningPublished: publishedB,
	}, show: true})
	if !a.offer.offering() {
		t.Fatal("the stale check raised no offer")
	}
	a.offer.deadline = a.now().Add(-time.Second)
	drive(t, a, updateOfferTickMsg{})

	if resolved.Tag != aTag {
		t.Fatalf("the resolver was asked for %q", resolved.Tag)
	}
	// THE MOMENT RODE WITH THE PINNED TAG. Without it the record and the
	// candidate could not be ordered under the lock and the older build would
	// have been written over the newer one.
	if !resolved.PublishedAt.Equal(publishedA) {
		t.Fatalf("the pinned resolve carried published_at %s, want %s", resolved.PublishedAt, publishedA)
	}
	if onDisk, err := os.ReadFile(target); err != nil || string(onDisk) != string(bBytes) {
		t.Fatalf("the older same-day build was written: %q, %v", onDisk, err)
	}
	if !strings.Contains(updateNotes(a), "newer than "+aTag) {
		t.Fatalf("the refusal was not said:\n%s", updateNotes(a))
	}
	if state := codeupdate.LoadAutoState(profile); state.Failures != 0 {
		t.Fatalf("a refusal was counted as a failure: %+v", state)
	}
}

// TestATypedNamedTagRecordsTheLookedUpPublishMoment proves the MANUAL named road
// end to end through the surface's own wiring: `/update <dev-tag>` resolves a
// PINNED tag without the API, the shared installer looks the tag's publish
// moment up once (best-effort) and writes it into the record \u2014 which is the fact
// a later same-day channel update needs to move forward safely.
func TestATypedNamedTagRecordsTheLookedUpPublishMoment(t *testing.T) {
	const running = "dev-20261007-aaaaaaaaaaaa"
	publishedAt := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	asset := []byte("dev early bytes")
	digest := sha256.Sum256(asset)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/releases/tags/"):
			fmt.Fprintf(w, `{"tag_name":%q,"published_at":%q}`, running, publishedAt.Format(time.RFC3339))
		case strings.HasSuffix(request.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%x  codeaf-%s-%s\n", digest, runtime.GOOS, runtime.GOARCH)
		case strings.Contains(request.URL.Path, "/releases/download/"):
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &codeupdate.Client{HTTP: server.Client(), APIBase: server.URL, DownloadBase: server.URL, Revision: running}
	target := filepath.Join(t.TempDir(), "codeaf")

	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	a := lab.app
	a.updateRunning = running
	a.resolveUpdate = client.Select
	a.installUpdate = func(install context.Context, options codeupdate.InstallOptions) (codeupdate.InstallResult, error) {
		options.Client = client
		options.Target = target
		return codeupdate.Install(install, options)
	}
	command := a.runUpdateCommand(running)
	if command == nil {
		t.Fatal("the typed named tag was refused")
	}
	drive(t, a, command())
	if a.updateInFlight {
		t.Fatal("the named install never finished")
	}
	if !strings.Contains(updateNotes(a), "checksum matched") {
		t.Fatalf("the manual completion was not said:\n%s", updateNotes(a))
	}
	lock, err := codeupdate.TryTargetLock(target)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := lock.Record()
	lock.Release()
	if !ok || record.Tag != running || !record.PublishedAt.Equal(publishedAt) {
		t.Fatalf("the typed install recorded %+v, ok = %t; want the looked-up moment %s", record, ok, publishedAt)
	}
}

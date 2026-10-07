package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(updateNotes(a), "will not be offered again") {
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

// TestTheDeferClauseIsDroppedBeforeTheTurnsOwnKeys proves M3.
func TestTheDeferClauseIsDroppedBeforeTheTurnsOwnKeys(t *testing.T) {
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	a := lab.app
	a.offer.raise("v0.9.3", "v0.9.2", a.now())
	composed := a.withUpdateOffer("esc interrupt · ctrl+g backgrounds")
	if !strings.Contains(composed, "/update skip") {
		t.Fatalf("the defer clause is not composed:\n%s", composed)
	}
	shortened := a.hintShorter(composed)
	if shortened != a.runHint() {
		t.Fatalf("shortening dropped more than the offer clause: %q", shortened)
	}
	if strings.Contains(shortened, "/update skip") {
		t.Fatalf("the offer clause was not the first to go: %q", shortened)
	}
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
				if a.offer.phase != updateDownloading || (name == "manual") != a.offer.manual {
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

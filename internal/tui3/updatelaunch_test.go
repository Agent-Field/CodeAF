package tui3

import (
	"context"
	"strings"
	"testing"

	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// TestABuildTheAutomaticRoadIsNotForGetsTheQuietLine proves the countdown is
// only for the builds the rule names: a devaf, a dev build, a release
// candidate or any other file hears the one quiet line and nothing counts down.
func TestABuildTheAutomaticRoadIsNotForGetsTheQuietLine(t *testing.T) {
	state := codeupdate.AutoState{}
	coordinator := offerAuto(&state)
	asked := codeupdate.Available{}
	coordinator.Automatic = func(available codeupdate.Available) bool {
		asked = available
		return false
	}
	lab := newOfferLab(t, coordinator)
	drive(t, lab.app, offerCheck("v0.9.3"))
	if lab.app.offer.offering() || lab.installed != 0 {
		t.Fatalf("a build the road is not for was counted down: %+v, installed %d", lab.app.offer, lab.installed)
	}
	if asked.Latest != "v0.9.3" || asked.Running != "v0.9.2" {
		t.Fatalf("the rule was asked about %+v", asked)
	}
	if !strings.Contains(updateNotes(lab.app), "codeaf v0.9.3 is out · you have v0.9.2 · /update installs it for the next launch") {
		t.Fatalf("the quiet line was not said:\n%s", updateNotes(lab.app))
	}
}

// TestAPinnedFileIsToldAndNeverCountedDown proves a person's pin outranks the
// countdown, and the line says the road that lets it go.
func TestAPinnedFileIsToldAndNeverCountedDown(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	check := offerCheck("v0.9.3")
	check.available.Pinned = "v0.9.2"
	drive(t, lab.app, check)
	if lab.app.offer.offering() || lab.installed != 0 {
		t.Fatalf("a pinned file was counted down: %+v", lab.app.offer)
	}
	if !strings.Contains(updateNotes(lab.app), "codeaf v0.9.3 is out · you have v0.9.2, pinned · /update installs it and unpins") {
		t.Fatalf("the pinned line was not said:\n%s", updateNotes(lab.app))
	}
}

// TestAReleaseStoppedAtLaunchIsNotCountedDownAgain proves ctrl+c before the
// chat opened is the person's answer for THAT release only.
func TestAReleaseStoppedAtLaunchIsNotCountedDownAgain(t *testing.T) {
	state := codeupdate.AutoState{}
	coordinator := offerAuto(&state)
	coordinator.Deferred = "v0.9.3"
	lab := newOfferLab(t, coordinator)
	drive(t, lab.app, offerCheck("v0.9.3"))
	if lab.app.offer.offering() {
		t.Fatal("the release the person stopped was counted down again")
	}
	if !strings.Contains(updateNotes(lab.app), "/update installs it for the next launch") {
		t.Fatalf("the stopped release was not named:\n%s", updateNotes(lab.app))
	}
	later := newOfferLab(t, coordinator)
	drive(t, later.app, offerCheck("v0.9.4"))
	if !later.app.offer.offering() {
		t.Fatal("a later release was treated as the one the person stopped")
	}
}

// TestOnlyTheCountdownInstallsAsNobody proves the installer hears who asked:
// the countdown's own install is automatic, and `/update` typed by a person is
// not — which is what lets a person's install pin and unpin while the
// countdown never can.
func TestOnlyTheCountdownInstallsAsNobody(t *testing.T) {
	state := codeupdate.AutoState{}
	lab := newOfferLab(t, offerAuto(&state))
	drive(t, lab.app, offerCheck("v0.9.3"))
	drive(t, lab.app, updateGraceMsg{})
	if lab.installed != 1 || !lab.automatic {
		t.Fatalf("the countdown installed %d times, automatic %t", lab.installed, lab.automatic)
	}
	typed := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	command := typed.app.runUpdateCommand("")
	if command == nil {
		t.Fatal("a typed /update was refused")
	}
	drive(t, typed.app, command())
	if typed.installed != 1 || typed.automatic {
		t.Fatalf("a typed /update installed %d times, automatic %t", typed.installed, typed.automatic)
	}
}

// TestANamedInstallSaysItPinned proves the pin is said the moment it is set,
// whether the release had to be downloaded or was already on disk.
func TestANamedInstallSaysItPinned(t *testing.T) {
	for _, already := range []bool{false, true} {
		lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
		lab.result = codeupdate.InstallResult{Release: codeupdate.Release{Tag: "v0.9.0"}, Path: "/tmp/codeaf", Pinned: true, Already: already}
		lab.app.resolveUpdate = func(context.Context, codeupdate.Choice) (codeupdate.Release, error) {
			return codeupdate.Release{Tag: "v0.9.0"}, nil
		}
		command := lab.app.runUpdateCommand("v0.9.0")
		if command == nil {
			t.Fatal("a named /update was refused")
		}
		drive(t, lab.app, command())
		if !strings.Contains(updateNotes(lab.app), "codeaf v0.9.0 is pinned · it is not updated on its own until /update runs without a version") {
			t.Fatalf("already=%t: the pin was not said:\n%s", already, updateNotes(lab.app))
		}
	}
}

// TestAFinishedInstallSaysOneShortLine proves the completion is the release and
// the one thing to do about it — in the transcript and in the keys line alike —
// and that it fits an eighty-column window.
func TestAFinishedInstallSaysOneShortLine(t *testing.T) {
	lab := newOfferLab(t, offerAuto(&codeupdate.AutoState{}))
	drive(t, lab.app, offerCheck("v0.9.3"))
	drive(t, lab.app, updateGraceMsg{})
	want := "codeaf v0.9.3 installed · restart to use the new version"
	if !strings.Contains(updateNotes(lab.app), want) {
		t.Fatalf("the transcript did not say %q:\n%s", want, updateNotes(lab.app))
	}
	if strings.Contains(updateNotes(lab.app), "new windows open the updated app") {
		t.Fatalf("the long completion came back:\n%s", updateNotes(lab.app))
	}
	if got := lab.app.updateOfferHint(80); got != want {
		t.Fatalf("the keys line says %q, want %q", got, want)
	}
}

// TestAWindowALaunchUpdatedSaysSo proves the line the old build printed is not
// the only record: the chat's own first lines name both builds and the switch.
func TestAWindowALaunchUpdatedSaysSo(t *testing.T) {
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", ProfileDir: t.TempDir(),
		UpdateRunning: "v0.9.3", UpdatedFrom: "v0.9.2",
	})
	if !strings.Contains(updateNotes(a), "codeaf updated from v0.9.2 to v0.9.3 as it opened · automatic updates in /settings turns this off") {
		t.Fatalf("the window did not say it was updated:\n%s", updateNotes(a))
	}
	quiet := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "test/model"}, Workspace: "/tmp/lab", ProfileDir: t.TempDir(), UpdateRunning: "v0.9.3",
	})
	if strings.Contains(updateNotes(quiet), "codeaf updated from") {
		t.Fatalf("an ordinary launch claimed an update:\n%s", updateNotes(quiet))
	}
}

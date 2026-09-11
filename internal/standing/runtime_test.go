package standing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

func TestStandingOwnerRefusesStaleDocument(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item, err := store.Create(reminder("remind me", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	paused, err := store.SetStatus(item.ID, StatusPaused, "")
	if err != nil {
		t.Fatal(err)
	}
	item.Grant = "a stale grant"
	if err := store.Save(item); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	got, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPaused || got.Grant != paused.Grant || got.Revision != paused.Revision {
		t.Fatalf("stale document changed the current item: %+v", got)
	}
}

func TestStandingOwnerLateExpiryPreservesEditedDeadline(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item, err := store.Create(reminder("remind me", now.Add(-25*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	changed := item
	changed.When.At = now.Add(time.Hour)
	if err := store.Save(changed); err != nil {
		t.Fatal(err)
	}
	expired := item
	expired.Status, expired.RetiredWhy = StatusRetired, "expired"
	expired.LastChecked = now
	if err := store.recordRuntime(item, expired); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusActive || !got.When.At.Equal(changed.When.At) {
		t.Fatalf("late expiry replaced edit: %+v", got)
	}
}

func TestStandingOwnerPauseResumeInvalidatesEarlierJudgment(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := reminder("tell me if there is news", time.Time{})
	item.When = When{Kind: WhenProbe, Probe: Probe{Command: "true"}, Hint: "if there is news"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	ticker := newTicker(store, runner, now)
	ticker.Sentinel = func(context.Context, Judgment) (bool, string, float64, error) {
		if _, err := store.SetStatus(made.ID, StatusPaused, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetStatus(made.ID, StatusActive, ""); err != nil {
			t.Fatal(err)
		}
		return true, "yes", .01, nil
	}
	mustTick(t, ticker)
	if len(runner.said) != 0 {
		t.Fatal("an obsolete judgment fired after pause and resume")
	}
	got, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpentUSD != .01 {
		t.Fatalf("lost actual judgment cost: %g", got.SpentUSD)
	}
}

func TestStandingOwnerEffortAndReceiptPreserveEachOther(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item, err := store.Create(reminder("remind me", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetStandingEffort(item.ID, effort.None); err != nil {
		t.Fatal(err)
	}
	after := item
	after.Runs, after.CleanRuns, after.LastOutcome = 1, 1, "said"
	after.LastFired, after.LastChecked = now, now
	if err := store.recordRuntime(item, after); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStandingEffort(item.ID, effort.None); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Runs != 1 || got.LastOutcome != "said" {
		t.Fatalf("effort edit lost progress: %+v", got)
	}
}

func TestStandingOwnerUnrelatedEditConsumesCompletedOneShot(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	before, err := store.Create(reminder("remind me", now.Add(-time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetStandingEffort(before.ID, effort.High); err != nil {
		t.Fatal(err)
	}
	after := before
	after.Runs, after.CleanRuns = 1, 1
	after.LastFired, after.LastChecked = now, now
	after.Status, after.RetiredWhy = StatusRetired, "fired"
	if err := store.recordRuntime(before, after); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRetired || got.Does.Effort != effort.High.String() || got.Runs != 1 {
		t.Fatalf("unrelated edit replayed completion or was lost: %+v", got)
	}
}

func TestStandingOwnerExtendedExpirySurvivesLateReceipt(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := reminder("remind me", now.Add(time.Hour))
	item.When = When{Kind: WhenEvery, Every: "1h"}
	item.Rails.Expires = now.Add(-time.Minute)
	before, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	changed := before
	changed.Rails.Expires = now.Add(time.Hour)
	if err := store.Save(changed); err != nil {
		t.Fatal(err)
	}
	after := before
	after.Status, after.RetiredWhy, after.LastChecked = StatusRetired, "expired", now
	if err := store.recordRuntime(before, after); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusActive || !got.Rails.Expires.Equal(changed.Rails.Expires) {
		t.Fatalf("late expiry: %+v", got)
	}
}

func TestStandingOwnerReadsLegacyAndUpgradesOnMutation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item, err := store.Create(reminder("remind me", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	item.Schema, item.Revision = 1, 0
	if err := store.write(item); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Revision != 0 {
		t.Fatal("legacy revision was invented")
	}
	changed, err := store.SetStatus(item.ID, StatusPaused, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Schema != Schema || changed.Revision != 1 {
		t.Fatalf("legacy upgrade: %+v", changed)
	}
}

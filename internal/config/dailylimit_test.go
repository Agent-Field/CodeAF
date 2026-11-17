package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dailyProfile(t *testing.T, body string) string {
	t.Helper()
	t.Setenv(dailyBudgetEnv, "")
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(BudgetConfigPath(dir), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dailyNow() time.Time { return time.Date(2026, 10, 16, 15, 0, 0, 0, time.Local) }

func figure(v float64) *float64 { return &v }

// THE TIGHTER POSITIVE LIMIT SURVIVES THE HAND-OVER, absence is not zero, and
// an explicit zero is a limit nobody set: it only survives when nothing
// positive applies.
func TestAdoptingALegacyDailyLimitKeepsTheTighterPositiveOne(t *testing.T) {
	for _, tc := range []struct {
		name      string
		persisted string
		legacy    LegacyDailyLimit
		want      float64
	}{
		{"implicit default beats the engine default", ``, LegacyDailyLimit{Implicit: 20}, 20},
		{"persisted tighter than the implicit default", `{"daily_budget_usd":12}`, LegacyDailyLimit{Implicit: 20}, 12},
		{"chosen tighter than persisted", `{"daily_budget_usd":12}`, LegacyDailyLimit{Chosen: figure(5), Implicit: 20}, 5},
		{"chosen looser than persisted keeps persisted", `{"daily_budget_usd":12}`, LegacyDailyLimit{Chosen: figure(50), Implicit: 20}, 12},
		{"chosen none, nothing persisted, is none", ``, LegacyDailyLimit{Chosen: figure(0), Implicit: 20}, 0},
		{"chosen none keeps a persisted positive limit", `{"daily_budget_usd":300}`, LegacyDailyLimit{Chosen: figure(0), Implicit: 20}, 300},
		{"persisted none does not loosen the implicit default", `{"daily_budget_usd":0}`, LegacyDailyLimit{Implicit: 20}, 20},
		{"both none is none", `{"daily_budget_usd":0}`, LegacyDailyLimit{Chosen: figure(0), Implicit: 20}, 0},
		{"nothing said anywhere keeps the built-in default", ``, LegacyDailyLimit{}, DefaultDailyBudgetUSD},
		{"persisted none and nothing legacy stays none", `{"daily_budget_usd":0}`, LegacyDailyLimit{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := dailyProfile(t, tc.persisted)
			if err := AdoptLegacyDailyLimit(dir, tc.legacy, dailyNow()); err != nil {
				t.Fatal(err)
			}
			got, err := DailyBudgetUSDAtTime(dir, dailyNow())
			if err != nil || got != tc.want {
				t.Fatalf("limit = %v, %v; want %v", got, err, tc.want)
			}
			if ok, err := DailyBudgetAuthorityEstablished(dir); err != nil || !ok {
				t.Fatalf("authority = %v, %v", ok, err)
			}
		})
	}
}

// A STALE LEGACY VALUE NEVER UNDOES A LATER EXPLICIT WRITE, however it is
// handed over again.
func TestALegacyLimitCannotUndoAnExplicitWrite(t *testing.T) {
	dir := dailyProfile(t, ``)
	if err := AdoptLegacyDailyLimit(dir, LegacyDailyLimit{Implicit: 20}, dailyNow()); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyBudgetUSD(dir, 90); err != nil {
		t.Fatal(err)
	}
	if err := AdoptLegacyDailyLimit(dir, LegacyDailyLimit{Chosen: figure(3), Implicit: 20}, dailyNow()); err != nil {
		t.Fatal(err)
	}
	if got, _ := DailyBudgetUSDAtTime(dir, dailyNow()); got != 90 {
		t.Fatalf("a stale legacy limit moved the limit to %v", got)
	}
	// And an explicit write before any hand-over settles it for good.
	other := dailyProfile(t, ``)
	if err := WriteDailyBudgetUSD(other, 0); err != nil {
		t.Fatal(err)
	}
	if err := AdoptLegacyDailyLimit(other, LegacyDailyLimit{Chosen: figure(3), Implicit: 20}, dailyNow()); err != nil {
		t.Fatal(err)
	}
	if got, _ := DailyBudgetUSDAtTime(other, dailyNow()); got != 0 {
		t.Fatalf("an explicit none was undone: %v", got)
	}
}

// A ROW THAT IS NOT A NUMBER IS NEITHER THE DEFAULT NOR NO LIMIT: every reader
// and the hand-over refuse it, and the file is left as it was.
func TestAMalformedDailyLimitFailsClosed(t *testing.T) {
	for _, body := range []string{
		`{"daily_budget_usd":null}`, `{"daily_budget_usd":true}`, `{"daily_budget_usd":"5"}`,
		`{"daily_budget_usd":{}}`, `{"daily_budget_usd":[1]}`, `{"daily_budget_usd":-1}`,
		`{"daily_budget_usd":5,"daily_budget_authority":2}`, `{"daily_budget_usd":5,"daily_budget_authority":"1"}`,
		`{"daily_budget_usd":5,"daily_budget_authority":true}`,
		`{"daily_budget_usd":5,"daily_budget_today":null}`,
		`{"daily_budget_usd":5,"daily_budget_today":{"usd":9}}`,
		`{"daily_budget_usd":5,"daily_budget_today":{"usd":9,"until":"2026-10-17T00:00:00-04:00","extra":1}}`,
		`{"daily_budget_usd":5,"daily_budget_today":{"usd":9,"until":"2026-10-17T03:00:00-04:00"}}`,
		`not json`,
	} {
		dir := dailyProfile(t, body)
		if got, err := DailyBudgetUSDAtTime(dir, dailyNow()); err == nil {
			t.Errorf("%s read as %v", body, got)
		}
		if err := AdoptLegacyDailyLimit(dir, LegacyDailyLimit{Implicit: 20}, dailyNow()); err == nil {
			t.Errorf("%s was adopted over", body)
		}
		if after, _ := os.ReadFile(BudgetConfigPath(dir)); string(after) != body {
			t.Errorf("%s was rewritten to %s", body, after)
		}
		if err := RaiseDailyBudgetToday(dir, 99, dailyNow()); err == nil {
			t.Errorf("%s accepted a raise", body)
		}
	}
}

// THE RAISE FOR TODAY IS CONFIG'S: it is read by the same getter, ends at the
// local midnight, survives a restart (a fresh read of the file), is cleared by
// any explicit write, and a raise that raises nothing is refused.
func TestARaiseForTodayExpiresAtLocalMidnightAndIsClearedByAWrite(t *testing.T) {
	dir := dailyProfile(t, ``)
	now := dailyNow()
	if err := RaiseDailyBudgetToday(dir, 60, now); err == nil {
		t.Fatal("a raise was taken before the limit was settled")
	}
	if err := WriteDailyBudgetUSD(dir, 40); err != nil {
		t.Fatal(err)
	}
	for _, usd := range []float64{40, 39, 0, -1} {
		if err := RaiseDailyBudgetToday(dir, usd, now); err == nil {
			t.Fatalf("a raise to %v was taken", usd)
		}
	}
	if err := RaiseDailyBudgetToday(dir, 60, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := DailyBudgetUSDAtTime(dir, now); got != 60 {
		t.Fatalf("in force = %v", got)
	}
	if base, _ := DailyBudgetBaseUSDAt(dir); base != 40 {
		t.Fatalf("a raise moved the base to %v", base)
	}
	// A restart is nothing but a fresh read of the same file.
	if got, _ := DailyBudgetUSDAtTime(dir, now.Add(8*time.Hour)); got != 60 {
		t.Fatalf("after a restart in force = %v", got)
	}
	midnight := time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)
	if got, _ := DailyBudgetUSDAtTime(dir, midnight.Add(-time.Second)); got != 60 {
		t.Fatalf("just before midnight = %v", got)
	}
	if got, err := DailyBudgetUSDAtTime(dir, midnight); err != nil || got != 40 {
		t.Fatalf("at midnight = %v, %v", got, err)
	}
	if got, err := DailyBudgetUSDAtTime(dir, midnight.AddDate(0, 0, 5)); err != nil || got != 40 {
		t.Fatalf("days later = %v, %v", got, err)
	}
	// A backward clock correction ignores a far-future raise and uses the tighter base.
	if got, err := DailyBudgetUSDAtTime(dir, now.AddDate(0, 0, -3)); err != nil || got != 40 {
		t.Fatalf("a far-future raise did not fall to base: %v, %v", got, err)
	}
	if err := WriteDailyBudgetUSD(dir, 45); err != nil {
		t.Fatal(err)
	}
	if got, _ := DailyBudgetUSDAtTime(dir, now); got != 45 {
		t.Fatalf("an explicit write kept the raise: %v", got)
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(raw), dailyTodayKey) {
		t.Fatalf("the raise is still on disk: %s", raw)
	}
	if err := WriteDailyBudgetUSD(dir, 0); err != nil {
		t.Fatal(err)
	}
	if err := RaiseDailyBudgetToday(dir, 60, now); err == nil {
		t.Fatal("no limit was raised")
	}
}

// THE ENVIRONMENT STILL WINS, over the row and over a raise, and nothing may
// be reported as taking effect under it.
func TestTheEnvironmentPinsTheDailyLimit(t *testing.T) {
	dir := dailyProfile(t, ``)
	if err := WriteDailyBudgetUSD(dir, 40); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dailyBudgetEnv, "7")
	if name, pinned := DailyBudgetPinnedBy(); !pinned || name != dailyBudgetEnv {
		t.Fatalf("pinned = %q, %v", name, pinned)
	}
	if got, err := DailyBudgetUSDAtTime(dir, dailyNow()); err != nil || got != 7 {
		t.Fatalf("limit under the pin = %v, %v", got, err)
	}
	if err := RaiseDailyBudgetToday(dir, 60, dailyNow()); err == nil {
		t.Fatal("a raise was taken under the pin")
	}
	t.Setenv(dailyBudgetEnv, "")
	if _, pinned := DailyBudgetPinnedBy(); pinned {
		t.Fatal("an empty value pins the limit")
	}
	if got, _ := DailyBudgetUSDAtTime(dir, dailyNow()); got != 40 {
		t.Fatalf("limit without the pin = %v", got)
	}
}

// A WRITE THAT FAILS CHANGES NOTHING AND SAYS SO.
func TestAFailedDailyLimitWriteIsAnError(t *testing.T) {
	t.Setenv(dailyBudgetEnv, "")
	blocker := filepath.Join(t.TempDir(), "profile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteDailyBudgetUSD(blocker, 5); err == nil {
		t.Fatal("a write under a file succeeded")
	}
	if err := AdoptLegacyDailyLimit(blocker, LegacyDailyLimit{Implicit: 20}, dailyNow()); err == nil {
		t.Fatal("a hand-over under a file succeeded")
	}
	if ok, _ := DailyBudgetAuthorityEstablished(blocker); ok {
		t.Fatal("a failed hand-over established the authority")
	}
}

// A still-current legacy raise is carried, at most to tonight's midnight, and
// a spent one is dropped.
func TestAdoptingALegacyRaiseIsBoundedByTonight(t *testing.T) {
	now := dailyNow()
	dir := dailyProfile(t, ``)
	far := now.AddDate(0, 0, 9)
	if err := AdoptLegacyDailyLimit(dir, LegacyDailyLimit{Chosen: figure(40), RaiseUSD: 60, RaiseUntil: far}, now); err != nil {
		t.Fatal(err)
	}
	if got, err := DailyBudgetUSDAtTime(dir, now); err != nil || got != 60 {
		t.Fatalf("carried raise = %v, %v", got, err)
	}
	if got, err := DailyBudgetUSDAtTime(dir, time.Date(2026, 10, 17, 0, 0, 0, 0, time.Local)); err != nil || got != 40 {
		t.Fatalf("the carried raise outlived tonight: %v, %v", got, err)
	}
	spent := dailyProfile(t, ``)
	if err := AdoptLegacyDailyLimit(spent, LegacyDailyLimit{Chosen: figure(40), RaiseUSD: 60, RaiseUntil: now.Add(-time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(BudgetConfigPath(spent))
	if strings.Contains(string(raw), dailyTodayKey) {
		t.Fatalf("a spent raise was carried: %s", raw)
	}
}

// THE SETTINGS ROW IS THE SAME EXPLICIT SETTER.
func TestTheDailyBudgetSettingsRowEstablishesTheAuthority(t *testing.T) {
	dir := dailyProfile(t, ``)
	if err := WriteDailyBudgetUSD(dir, 40); err != nil {
		t.Fatal(err)
	}
	if err := RaiseDailyBudgetToday(dir, 60, time.Now()); err != nil {
		t.Fatal(err)
	}
	row, ok := NewSettings(SettingsOptions{ProfileDir: dir}).Row(KeyDailyBudget)
	if !ok {
		t.Fatal("no daily budget row")
	}
	if err := row.Apply("3"); err != nil {
		t.Fatal(err)
	}
	if got, err := DailyBudgetUSDAt(dir); err != nil || got != 3 {
		t.Fatalf("limit after the row = %v, %v", got, err)
	}
	if err := row.Apply("none"); err != nil {
		t.Fatal(err)
	}
	if got, err := DailyBudgetUSDAt(dir); err != nil || got != 0 {
		t.Fatalf("limit after none = %v, %v", got, err)
	}
}

func TestLegacyRaiseDoesNotLiftAnIndependentTighterDailyLimit(t *testing.T) {
	now := dailyNow()
	for _, tc := range []struct {
		raw  string
		want float64
	}{{`{"daily_budget_usd":12}`, 12}, {`{"daily_budget_usd":500}`, 153}, {``, 153}} {
		dir := dailyProfile(t, tc.raw)
		err := AdoptLegacyDailyLimit(dir, LegacyDailyLimit{Chosen: figure(20), RaiseUSD: 153, RaiseUntil: dailyMidnightAfter(now)}, now)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DailyBudgetUSDAtTime(dir, now); err != nil || got != tc.want {
			t.Fatalf("%s: %v,%v want %v", tc.raw, got, err, tc.want)
		}
	}
}

func TestTodayRaiseRejectsQuotedNumbersAndCannotLowerAnActiveRaise(t *testing.T) {
	dir := dailyProfile(t, `{"daily_budget_usd":20,"daily_budget_authority":1,"daily_budget_today":{"usd":"153","until":"2026-10-17T00:00:00Z"}}`)
	if _, err := DailyBudgetUSDAtTime(dir, dailyNow()); err == nil {
		t.Fatal("quoted raise was accepted")
	}
	if err := WriteDailyBudgetUSD(dir, 20); err != nil {
		t.Fatal(err)
	}
	if err := RaiseDailyBudgetToday(dir, 153, dailyNow()); err != nil {
		t.Fatal(err)
	}
	if err := RaiseDailyBudgetToday(dir, 100, dailyNow()); err == nil {
		t.Fatal("lowered active limit as a raise")
	}
	if got, err := DailyBudgetUSDAtTime(dir, dailyNow()); err != nil || got != 153 {
		t.Fatalf("%v,%v", got, err)
	}
}

func TestDailyRaiseAfterAClockCorrectionUsesTheTighterBase(t *testing.T) {
	now := dailyNow()
	dir := dailyProfile(t, ``)
	if err := WriteDailyBudgetUSD(dir, 20); err != nil {
		t.Fatal(err)
	}
	if err := RaiseDailyBudgetToday(dir, 153, now); err != nil {
		t.Fatal(err)
	}
	got, err := DailyBudgetUSDAtTime(dir, dailyMidnightAfter(now).Add(-30*time.Hour))
	if err != nil || got != 20 {
		t.Fatalf("clock correction: %v,%v", got, err)
	}
	corrected := dailyMidnightAfter(now).Add(-30 * time.Hour)
	if err := RaiseDailyBudgetToday(dir, 100, corrected); err != nil {
		t.Fatalf("stale raise blocked replacement: %v", err)
	}
	if got, err := DailyBudgetUSDAtTime(dir, corrected); err != nil || got != 100 {
		t.Fatalf("replacement raise: %v,%v", got, err)
	}
}

func TestNullConfigDocumentIsNotAnUnlimitedDailyBudget(t *testing.T) {
	dir := dailyProfile(t, `null`)
	if _, err := DailyBudgetUSDAtTime(dir, dailyNow()); err == nil {
		t.Fatal("null config granted default")
	}
	if err := AdoptLegacyDailyLimit(dir, LegacyDailyLimit{Implicit: 20}, dailyNow()); err == nil {
		t.Fatal("null config silently replaced")
	}
}

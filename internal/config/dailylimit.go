package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// ── THE DAILY LIMIT HAS ONE AUTHORITY ───────────────────────────────────────
//
// What codeaf may spend in a day is ONE persisted row of the profile's
// config.json, [KeyDailyBudget], read by every door through
// [DailyBudgetUSDAt]. A surface that wants a limit of its own writes this row
// or it has none: two limits with two defaults (a surface's $20 beside the
// engine's $500) meant the one the person chose was not the one that stopped
// a call.
//
// THREE ROWS, ONE TRANSACTION. The amount, an authority marker beside it, and
// an optional raise that ends tonight are always written together, through
// [writeProfileValues]'s one read-copy-rename, so a limit and the fact that it
// is the limit can never be seen apart.
//
//   - ABSENCE IS NOT ZERO. A profile that never said keeps the built-in default
//     ([DefaultDailyBudgetUSD]); an explicit 0 is the person's "no limit".
//   - A ROW THAT IS NOT A NUMBER FAILS CLOSED. null, a boolean, a string, an
//     object, a negative or a non-finite amount is an error here and in every
//     caller; none of them reads as the default or as no limit.
//   - THE MARKER SAYS LEGACY VALUES ARE OVER. Until it is written a surface may
//     hand [AdoptLegacyDailyLimit] a limit it used to keep elsewhere; the
//     adoption takes the tighter positive limit once, in the same lock as the
//     write, and sets the marker. After it, no stale legacy value can be
//     adopted again, so it can never undo a later explicit write.
//   - A RAISE FOR TODAY IS CONFIG'S, BOUNDED AND EXPIRING. It names a local
//     midnight at most a day away; it is read by the same getter, only ever
//     lifts a positive limit, and is deleted by any explicit write.
//   - THE ENVIRONMENT STILL WINS. CODEAF_DAILY_BUDGET overrides the row and
//     the raise; a surface must ask [DailyBudgetPinnedBy] before it claims a
//     write took effect.

const (
	// dailyAuthorityKey holds the authority version, dailyAuthorityVersion
	// today. Any other value is a file from a build this one cannot read, and
	// fails closed.
	dailyAuthorityKey     = "daily_budget_authority"
	dailyAuthorityVersion = 1
	// dailyTodayKey holds the raise for today: {"usd":…,"until":…}.
	dailyTodayKey = "daily_budget_today"
	// dailyRaiseReach is the furthest ahead a raise may name its end: a day
	// and an hour of slack for a daylight-saving day.
	dailyRaiseReach = 26 * time.Hour
	dailyBudgetEnv  = "CODEAF_DAILY_BUDGET"
)

// dailyRaise is the persisted raise for today.
type dailyRaise struct {
	USD   float64   `json:"usd"`
	Until time.Time `json:"until"`
}

// DailyBudgetPinnedBy is the environment variable that pins the daily limit,
// when one does: a write to the row cannot take effect while it is set.
func DailyBudgetPinnedBy() (string, bool) {
	if strings.TrimSpace(env.Get(dailyBudgetEnv)) == "" {
		return "", false
	}
	return dailyBudgetEnv, true
}

// persistedNumber reads a row that must be a JSON number.
func persistedNumber(raw json.RawMessage, key string) (float64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, fmt.Errorf("read budget config: %s: %w", key, err)
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("read budget config: %s: want a non-negative dollar amount", key)
	}
	amount, err := number.Float64()
	if err != nil {
		return 0, fmt.Errorf("read budget config: %s: %w", key, err)
	}
	return validateDailyBudget(amount, key)
}

// dailyAuthorityOf says whether the marker is present, and refuses one that is
// not the version this build writes.
func dailyAuthorityOf(held map[string]json.RawMessage) (bool, error) {
	raw, present := held[dailyAuthorityKey]
	if !present {
		return false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	number, ok := json.Number(""), false
	if err := decoder.Decode(&value); err == nil {
		number, ok = value.(json.Number)
	}
	if version, err := number.Int64(); !ok || err != nil || version != dailyAuthorityVersion {
		return false, fmt.Errorf("read budget config: %s: unrecognised version", dailyAuthorityKey)
	}
	return true, nil
}

// dailyBaseOf is the persisted limit and whether the row is there.
func dailyBaseOf(held map[string]json.RawMessage) (float64, bool, error) {
	raw, present := held[KeyDailyBudget]
	if !present {
		return DefaultDailyBudgetUSD, false, nil
	}
	amount, err := persistedNumber(raw, KeyDailyBudget)
	return amount, true, err
}

// dailyRaiseOf is the persisted raise, nil when there is none. A raise that is
// there and malformed is an error even when it would have expired.
func dailyRaiseOf(held map[string]json.RawMessage) (*dailyRaise, error) {
	raw, present := held[dailyTodayKey]
	if !present {
		return nil, nil
	}
	bad := func(why string) error { return fmt.Errorf("read budget config: %s: %s", dailyTodayKey, why) }
	var stored struct {
		USD   json.RawMessage `json:"usd"`
		Until *string         `json:"until"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil || len(stored.USD) == 0 || stored.Until == nil {
		return nil, bad("want {\"usd\":…,\"until\":…}")
	}
	usd, err := persistedNumber(stored.USD, dailyTodayKey)
	if err != nil || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return nil, bad("want a positive dollar amount")
	}
	until, err := time.Parse(time.RFC3339Nano, *stored.Until)
	if err != nil {
		return nil, bad("until is not a time")
	}
	if until.Hour() != 0 || until.Minute() != 0 || until.Second() != 0 || until.Nanosecond() != 0 {
		return nil, bad("until is not a local midnight")
	}
	return &dailyRaise{USD: usd, Until: until}, nil
}

// DailyBudgetBaseUSDAt is the daily limit without any raise for today:
// the environment, else the persisted row, else the built-in default. Zero is
// no limit. A persisted row that cannot be read is an error.
func DailyBudgetBaseUSDAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(env.Get(dailyBudgetEnv)); raw != "" {
		return parseDailyBudget(raw, dailyBudgetEnv)
	}
	held, err := readProfileConfig(profileDir)
	if err != nil {
		return 0, err
	}
	if _, err := dailyAuthorityOf(held); err != nil {
		return 0, err
	}
	base, _, err := dailyBaseOf(held)
	return base, err
}

// DailyBudgetUSDAt is the daily limit in force now: the base with any raise
// for today laid over it. Zero is no limit.
func DailyBudgetUSDAt(profileDir string) (float64, error) {
	return DailyBudgetUSDAtTime(profileDir, time.Now())
}

// DailyBudgetUSDAtTime is [DailyBudgetUSDAt] at an instant a caller owns.
func DailyBudgetUSDAtTime(profileDir string, now time.Time) (float64, error) {
	if raw := strings.TrimSpace(env.Get(dailyBudgetEnv)); raw != "" {
		return parseDailyBudget(raw, dailyBudgetEnv)
	}
	held, err := readProfileConfig(profileDir)
	if err != nil {
		return 0, err
	}
	if _, err := dailyAuthorityOf(held); err != nil {
		return 0, err
	}
	base, _, err := dailyBaseOf(held)
	if err != nil {
		return 0, err
	}
	raise, err := dailyRaiseOf(held)
	if err != nil {
		return 0, err
	}
	if raise == nil || base <= 0 || !now.Before(raise.Until) {
		return base, nil
	}
	if raise.Until.Sub(now) > dailyRaiseReach {
		// A clock correction cannot turn a temporary raise into a broken
		// profile. Ignore the raise conservatively; the base is tighter.
		return base, nil
	}
	return max(base, raise.USD), nil
}

// DailyBudgetAuthorityEstablished says whether the limit's authority marker is
// written, so legacy values are over.
func DailyBudgetAuthorityEstablished(profileDir string) (bool, error) {
	held, err := readProfileConfig(profileDir)
	if err != nil {
		return false, err
	}
	return dailyAuthorityOf(held)
}

// WriteDailyBudgetUSD is the explicit setter: it writes the limit, establishes
// the authority and clears any raise for today in one atomic write. Zero is no
// limit. It does not look at the environment; a caller that reports the write
// as effective asks [DailyBudgetPinnedBy] first.
func WriteDailyBudgetUSD(profileDir string, amount float64) error {
	amount, err := validateDailyBudget(amount, KeyDailyBudget)
	if err != nil {
		return err
	}
	return writeProfileValues(profileDir, map[string]any{
		KeyDailyBudget:    amount,
		dailyAuthorityKey: dailyAuthorityVersion,
		dailyTodayKey:     removeProfileKey,
	})
}

// writeDailyBudget is the settings row's writer: the same explicit set.
func writeDailyBudget(profileDir, raw string) error {
	amount, err := parseDollars(raw)
	if err != nil {
		return err
	}
	return WriteDailyBudgetUSD(profileDir, amount)
}

// dailyMidnightAfter is the first local midnight after now.
func dailyMidnightAfter(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

// dailyWriteTries bounds the read-decide-write loops below when another
// writer moves a row between the read and the write.
const dailyWriteTries = 4

// rawOf is a held row's bytes for a write's expectation, nil when absent.
func rawOf(held map[string]json.RawMessage, key string) json.RawMessage {
	if raw, ok := held[key]; ok {
		return raw
	}
	return nil
}

// RaiseDailyBudgetToday lifts the limit to usd until the next local midnight.
// It refuses while the environment pins the limit, before the authority is
// established, when there is no limit to raise, and for a figure that is not
// above the base: a raise that would change nothing is not reported as one.
func RaiseDailyBudgetToday(profileDir string, usd float64, now time.Time) error {
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd <= 0 {
		return errors.New("a raise is a positive dollar amount")
	}
	if name, pinned := DailyBudgetPinnedBy(); pinned {
		return fmt.Errorf("the daily limit is set by %s", name)
	}
	for range dailyWriteTries {
		held, err := readProfileConfig(profileDir)
		if err != nil {
			return err
		}
		established, err := dailyAuthorityOf(held)
		if err != nil {
			return err
		}
		if !established {
			return errors.New("the daily limit has not been settled yet")
		}
		base, _, err := dailyBaseOf(held)
		if err != nil {
			return err
		}
		active, err := dailyRaiseOf(held)
		if err != nil {
			return err
		}
		effective := base
		if active != nil && now.Before(active.Until) && active.Until.Sub(now) <= dailyRaiseReach {
			effective = max(base, active.USD)
		}
		if base <= 0 {
			return errors.New("there is no daily limit to raise")
		}
		if usd <= effective {
			return fmt.Errorf("a raise must be above the current daily limit of $%.2f", effective)
		}
		err = writeProfileValuesExpecting(profileDir,
			map[string]json.RawMessage{KeyDailyBudget: rawOf(held, KeyDailyBudget), dailyAuthorityKey: rawOf(held, dailyAuthorityKey), dailyTodayKey: rawOf(held, dailyTodayKey)},
			map[string]any{dailyTodayKey: dailyRaise{USD: usd, Until: dailyMidnightAfter(now)}})
		if !errors.Is(err, errProfileMoved) {
			return err
		}
	}
	return errProfileMoved
}

// LegacyDailyLimit is a daily limit a surface used to keep somewhere of its
// own, handed over once to become the profile's.
type LegacyDailyLimit struct {
	// Chosen is the limit the person set there, nil when they never did. Zero
	// is their "no limit".
	Chosen *float64
	// Implicit is the limit that applied there while nothing was chosen; zero
	// when none did.
	Implicit float64
	// RaiseUSD and RaiseUntil are a raise for today that was still current.
	RaiseUSD   float64
	RaiseUntil time.Time
}

// AdoptLegacyDailyLimit settles the limit's authority once. Nothing happens if
// the marker is already written, so a legacy value can never undo a later
// write. Otherwise the limit becomes the tighter of the legacy limit and the
// persisted positive limit; a "no limit" survives only when nothing positive
// applies; a profile that never said anything and a surface that never limited
// keep the built-in default. A still-current raise is carried to tonight's
// midnight at most. Any row that cannot be read stops the adoption and writes
// nothing.
func AdoptLegacyDailyLimit(profileDir string, legacy LegacyDailyLimit, now time.Time) error {
	for _, figure := range []float64{legacy.Implicit, legacy.RaiseUSD} {
		if math.IsNaN(figure) || math.IsInf(figure, 0) || figure < 0 {
			return errors.New("the earlier daily limit is not a dollar amount")
		}
	}
	if legacy.Chosen != nil {
		if _, err := validateDailyBudget(*legacy.Chosen, "earlier daily limit"); err != nil {
			return err
		}
	}
	for range dailyWriteTries {
		held, err := readProfileConfig(profileDir)
		if err != nil {
			return err
		}
		established, err := dailyAuthorityOf(held)
		if err != nil {
			return err
		}
		if established {
			return nil
		}
		persisted, present, err := dailyBaseOf(held)
		if err != nil {
			return err
		}
		if _, err := dailyRaiseOf(held); err != nil {
			return err
		}
		earlier := legacy.Implicit
		if legacy.Chosen != nil {
			earlier = *legacy.Chosen
		}
		var base float64
		write := true
		switch {
		case earlier > 0 && present && persisted > 0:
			base = min(earlier, persisted)
		case earlier > 0:
			base = earlier
		case present && persisted > 0:
			base = persisted
		case (legacy.Chosen != nil && earlier == 0) || (present && persisted == 0):
			base = 0
		default:
			write = false
		}
		updates := map[string]any{dailyAuthorityKey: dailyAuthorityVersion, dailyTodayKey: removeProfileKey}
		if write {
			updates[KeyDailyBudget] = base
		}
		if until := legacy.RaiseUntil; base > 0 && legacy.RaiseUSD > base && now.Before(until) && !(present && persisted > 0 && (earlier <= 0 || persisted < earlier)) {
			if midnight := dailyMidnightAfter(now); until.After(midnight) {
				until = midnight
			}
			updates[dailyTodayKey] = dailyRaise{USD: legacy.RaiseUSD, Until: until}
		}
		err = writeProfileValuesExpecting(profileDir,
			map[string]json.RawMessage{KeyDailyBudget: rawOf(held, KeyDailyBudget), dailyAuthorityKey: nil, dailyTodayKey: rawOf(held, dailyTodayKey)},
			updates)
		if !errors.Is(err, errProfileMoved) {
			return err
		}
	}
	return errProfileMoved
}

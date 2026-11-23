package main

// ── codeaf app: THE DAY LIMIT IS THE PROFILE'S ──────────────────────────────
//
// THE APP KEEPS NO DAY CAP OF ITS OWN. Settings, Setup, Spend, a raise for
// today and the turn gate all read and write the profile's daily limit
// (internal/config's dailylimit.go), the one the session's rail and every
// crew guard hold. The app's file once kept a $20 cap beside the engine's
// $500; the number the person chose was not the number that stopped a call.
//
// ITS OLD FIELD IS READ ONCE, to carry what the person had chosen across, and
// never again: the first read of the limit hands it to
// [config.AdoptLegacyDailyLimit], which takes the tighter positive limit and
// writes the authority marker, after which this file's value cannot undo any
// later write. A file that cannot be read is an error, not a default.

import (
	"fmt"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

// appDayAuthority remembers which profile the authority was last settled on,
// so the migration check is a read of one row after the first ask.
type appDayAuthority struct {
	mu      sync.Mutex
	profile string
	settled bool
}

// settleDayLimit makes the profile's limit authoritative, adopting the app's
// old cap the first time.
func (b *appBackend) settleDayLimit() error {
	profile := config.ProfileDir()
	b.dayAuth.mu.Lock()
	defer b.dayAuth.mu.Unlock()
	if b.dayAuth.settled && b.dayAuth.profile == profile {
		return nil
	}
	established, err := config.DailyBudgetAuthorityEstablished(profile)
	if err != nil {
		return err
	}
	if !established {
		legacy := config.LegacyDailyLimit{Implicit: appDayCapDefault}
		if err := b.state.read(func(f *appStateFile) {
			if f.DayCapUSD != nil {
				chosen := *f.DayCapUSD
				legacy.Chosen = &chosen
			}
			if raise, ok := f.Raises[appMidnightRaiseScope]; ok {
				legacy.RaiseUSD, legacy.RaiseUntil = raise.USD, raise.Until
			}
		}); err != nil {
			return fmt.Errorf("the app's earlier day cap could not be read: %w", err)
		}
		if err := config.AdoptLegacyDailyLimit(profile, legacy, b.now()); err != nil {
			return err
		}
	}
	b.dayAuth.profile, b.dayAuth.settled = profile, true
	return nil
}

// dayBase is the daily limit without a raise for today, and zero for none.
func (b *appBackend) dayBase() (float64, error) {
	if err := b.settleDayLimit(); err != nil {
		return 0, err
	}
	return config.DailyBudgetBaseUSDAt(config.ProfileDir())
}

// dayCap is the daily limit in force at now, and zero for none.
func (b *appBackend) dayCap(now time.Time) (float64, error) {
	if err := b.settleDayLimit(); err != nil {
		return 0, err
	}
	return config.DailyBudgetUSDAtTime(config.ProfileDir(), now)
}

// dayLimitFree is nil when a write to the daily limit would take effect, and
// otherwise the reason it would not: the environment pins it.
func dayLimitFree() error {
	if name, pinned := config.DailyBudgetPinnedBy(); pinned {
		return appwire.Errorf(appwire.CodeBadParams, "the daily limit is set by %s", name)
	}
	return nil
}

// setDayBase writes the daily limit as the one authority, and answers the
// refusal when the environment would keep overriding it.
func (b *appBackend) setDayBase(usd float64) error {
	if err := dayLimitFree(); err != nil {
		return err
	}
	if err := config.WriteDailyBudgetUSD(config.ProfileDir(), usd); err != nil {
		return appEngineError(err)
	}
	return nil
}

// raiseDayToday lifts the limit until the next local midnight.
func (b *appBackend) raiseDayToday(usd float64, now time.Time) error {
	if err := dayLimitFree(); err != nil {
		return err
	}
	if err := b.settleDayLimit(); err != nil {
		return appEngineError(err)
	}
	if err := config.RaiseDailyBudgetToday(config.ProfileDir(), usd, now); err != nil {
		return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	return nil
}

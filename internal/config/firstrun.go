package config

import (
	"os"
	"strings"
	"time"
)

// THE FIRST-RUN SETUP'S THREE FACTS, AND ITS ONE MARKER.
//
// The v3 chat opens a three-step setup the first time it meets a profile with
// nothing in it — a key, a crew, a daily ceiling (internal/tui3's firstrun.go).
// Which steps it asks is decided HERE, from the same readers every other surface
// resolves those rows through, so the setup cannot ask for a key Load would
// have found in the environment or a crew /crew would already report.
//
// "Configured" is deliberately narrower than "resolves to something". Every row
// resolves — the crew reads `balanced` and the ceiling reads $20 on a profile
// nobody has touched, because those are the defaults — and a setup that took a
// default for an answer would never ask anybody anything. A row is configured
// when a PERSON answered it: the environment pins it, or the profile file holds
// it.

// KeySetupSeen is the profile field that says the setup has been shown. It is a
// timestamp rather than a bool for the one thing a bool cannot answer later —
// which features arrived after this person's first day — and it lives in
// config.json beside [KeyAPIKey] rather than in a stamp file because it is a
// fact about the profile, and the profile has one file.
//
// It is NOT a settings row: nothing about it is a preference, and a row a person
// could edit would be a way to be greeted twice.
const KeySetupSeen = "setup_seen_at"

// SetupSeenAt is when the setup was last shown, or the zero time. Skipping
// with esc counts as shown — the marker records that the person met it, not
// that they answered it.
func SetupSeenAt(profileDir string) time.Time {
	value, ok := persistedString(profileDir, KeySetupSeen)
	if !ok {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return at
}

// MarkSetupSeen records that the setup was shown now, through the same atomic
// writer every setting uses.
func MarkSetupSeen(profileDir string, at time.Time) error {
	return writeProfileValue(profileDir, KeySetupSeen, at.UTC().Format(time.RFC3339Nano))
}

// APIKeyConfigured is whether a session opened on this profile has a key to
// talk with, from anywhere Load would look.
func APIKeyConfigured(profileDir string) bool {
	return APIKeyAt(profileDir) != ""
}

// CrewConfigured is whether a person has ever answered the crew: any of the
// four tier rows is in the profile file. It is any rather than all because
// [ApplyCrew] writes the four together, and a person who pinned one tier by hand
// has an opinion the setup must not paper over with a preset.
func CrewConfigured(profileDir string) bool {
	for _, tier := range ModelTiers {
		if _, ok := persistedString(profileDir, tierKeyFor(tier)); ok {
			return true
		}
	}
	return false
}

// DailyBudgetConfigured is whether a person has answered the daily ceiling: the
// environment pins it, or the profile file holds it.
func DailyBudgetConfigured(profileDir string) bool {
	if strings.TrimSpace(os.Getenv("AFORGE_DAILY_BUDGET")) != "" {
		return true
	}
	_, ok := persistedValue(profileDir, KeyDailyBudget)
	return ok
}

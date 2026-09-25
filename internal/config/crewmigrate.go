package config

import (
	"sort"
	"strings"
)

// A PROFILE WRITTEN BEFORE THE CREW WAS ROUTED, READ THE WAY ITS OWNER MEANT IT.
//
// Until 2026-09-24 a crew was a preset word — frugal, balanced, max — that
// wrote all five tier rows at once, a family row (`open` or `all`) saying
// which shelf the word drew from, a pick row (`table`, `catalog`, `learn`)
// saying where unwritten seats were computed from, and a tier row could say
// `auto` to be computed. None of those words means anything now: every seat
// nobody pinned is routed per task. So a profile carrying them is read like
// this, and migrated once, with one line saying so:
//
//   - the preset word, the pick word and an `auto` row are read as AUTO;
//   - the family row's `open` becomes the allowed-models rule `open`, because
//     it said which models the person would accept, which is exactly what that
//     rule says now; `all` is the default and needs no row;
//   - a seat row holding ANY model id is A PIN — even one a retired preset
//     once wrote, and even three rows holding the same id. There is no telling
//     from a row which hand wrote it, and a pick never overrides a model id
//     that may have been a person's: the rule the old picker kept, kept.
//
// The reading is applied on every read until the migration writes it down
// ([MigrateCrew]), so a build that has not yet migrated a profile still runs
// the crew its owner meant.

// The retired rows, spelled here and nowhere else.
const (
	legacyKeyCrew       = "models.crew"
	legacyKeyCrewSource = "models.crew.source"
	legacyKeyCrewPick   = "models.crew.pick"
)

// legacyWords are the retired preset and pick words a seat row may hold.
var legacyWords = map[string]bool{
	"frugal": true, "balanced": true, "max": true, "table": true, "catalog": true, "learn": true, CrewAuto: true,
}

// crewSeatTiers are the three tier rows that hold crew pins.
var crewSeatTiers = []string{ModelTierWorker, ModelTierHigh, ModelTierMastermind}

// legacyCrewClearing is the write that migrates a profile: every retired row
// removed, the family's `open` carried onto the allowed rule, and every seat
// row that is not a person's pin removed. It is empty for a profile with
// nothing retired on it, and any crew write folds it into its own one write.
func legacyCrewClearing(profileDir string) map[string]any {
	values := map[string]any{}
	for _, key := range []string{legacyKeyCrew, legacyKeyCrewPick} {
		if _, held := persistedValue(profileDir, key); held {
			values[key] = removeProfileKey
		}
	}
	if source, held := persistedString(profileDir, legacyKeyCrewSource); held {
		values[legacyKeyCrewSource] = removeProfileKey
		if strings.EqualFold(strings.TrimSpace(source), "open") {
			if _, answered := persistedValue(profileDir, KeyCrewAllowed); !answered {
				values[KeyCrewAllowed] = "open"
			}
		}
	}
	for _, tier := range crewSeatTiers {
		value, held := persistedString(profileDir, tierKeyFor(tier))
		if !held {
			continue
		}
		word := strings.ToLower(strings.TrimSpace(value))
		if word == "" || legacyWords[word] {
			values[tierKeyFor(tier)] = removeProfileKey
		}
	}
	return values
}

// MigrateCrew writes a retired crew's reading down, once, and answers the one
// line that says so — empty for a profile with nothing to migrate, which is
// every profile after the first run of this build.
//
// THE LINE IS SAID ONCE BECAUSE THE MIGRATION IS DONE ONCE: after it, the
// retired rows are gone and nothing is left to say it about. It names what
// stayed pinned, because a person whose crew changed shape under them is owed
// the half that did not.
func MigrateCrew(profileDir string) (string, error) {
	values := legacyCrewClearing(profileDir)
	if len(values) == 0 {
		return "", nil
	}
	if err := writeProfileValues(profileDir, values); err != nil {
		return "", err
	}
	var kept []string
	for seat, pin := range CrewPinsAt(profileDir) {
		kept = append(kept, string(seat)+" "+pin.String())
	}
	sort.Strings(kept)
	line := "your crew is auto now · codeaf picks the worker, planner and checker for each task"
	if len(kept) > 0 {
		// A PERSON WHOSE SEATS STAYED PINNED IS TOLD THAT FIRST: nothing they
		// wrote was changed, and only the seats they left open are picked.
		line = "kept your pins: " + strings.Join(kept, ", ")
		if len(kept) < len(crewSeatTiers) {
			line += " · the other seats are picked for each task"
		}
	}
	if rule, held := values[KeyCrewAllowed]; held {
		line += " · allowed models: " + rule.(string)
	}
	return line + " · " + CrewCommand + " to see it", nil
}

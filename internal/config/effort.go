package config

import "github.com/Agent-Field/aforge-v2/internal/effort"

// The install's own rung on the effort ladder, written down.
//
// It is the LAST scope the resolver consults and the one every other scope
// falls back to (internal/effort's Resolve), so it is the number that decides
// how hard this machine thinks about anything nobody has said anything specific
// about — a person's turn, and the work they hand out.
//
// It lands here, in the profile's config.json, for the reason the conversation
// model lands here (chatmodel.go): a choice a person makes once and expects to
// find again is a choice that has to survive the process. There is deliberately
// no environment variable for it. AFORGE_REASONING already exists and means
// something else — the v1 planning economy (config.go's Reasoning) — and a
// second variable spelled almost the same would be answered wrong by everyone
// who met it.

// KeyEffort is where the install's rung is written in the profile's config.json.
//
// It is spelled bare rather than under a `model.` or `task.` prefix because it
// is not about one slot or one kind of work: it is the default for all of them,
// which is exactly what the resolver's last rung means.
const KeyEffort = "effort"

// EffortChoices offers the provider default followed by the five explicit
// levels. Auto means no reasoning override, not disabled reasoning.
var EffortChoices = func() []string {
	choices := make([]string, 0, len(effort.Rungs)+1)
	choices = append(choices, "auto")
	for _, rung := range effort.Rungs {
		choices = append(choices, rung.String())
	}
	return choices
}()

// DefaultEffortAt is the rung this profile last settled on, or [effort.Ship]
// when nobody has chosen one.
//
// Missing settings use the shipped default; saved choices remain authoritative.
// Legacy "off" files still mean absence and are displayed as auto.
func DefaultEffortAt(profileDir string) effort.Rung {
	value, ok := persistedString(profileDir, KeyEffort)
	if !ok {
		return effort.Ship
	}
	// Unknown settings fall back to the shipped default.
	rung, valid := effort.Parse(value)
	if !valid {
		return effort.Ship
	}
	return rung
}

// EffortWord names absence as auto in settings and persisted configuration.
func EffortWord(rung effort.Rung) string {
	if rung == effort.None {
		return "auto"
	}
	return rung.String()
}

// WriteDefaultEffort records the choice.
//
// It goes through [writeChoice] — the same validating writer every other choice
// row uses — so an unknown rung is refused in the same words as every other bad
// choice, rather than being written and then read back as the shipped default
// forever, which looks exactly like the write having been ignored.
func WriteDefaultEffort(profileDir string, rung effort.Rung) error {
	return writeChoice(profileDir, KeyEffort, EffortWord(rung), EffortChoices)
}

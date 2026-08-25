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

// EffortChoices is what the settings row offers, cheapest first, with the
// surface's word for absence in front.
//
// "off" and not "" because a choice row is a list somebody reads: an empty
// first option is a row that looks broken. It parses back to [effort.None]
// through [effort.Parse], which is the one door that knows the two are the same
// thing.
var EffortChoices = func() []string {
	choices := make([]string, 0, len(effort.Rungs)+1)
	choices = append(choices, "off")
	for _, rung := range effort.Rungs {
		choices = append(choices, rung.String())
	}
	return choices
}()

// DefaultEffortAt is the rung this profile last settled on, or [effort.Ship]
// when nobody has chosen one.
//
// AN UNCONFIGURED INSTALL IS NOT ABSENCE HERE. Every other row in this file
// answers an unset key with emptiness and lets the caller pick its own default;
// this one answers with the shipped rung, because the shipped rung IS the
// answer to "and otherwise?" and there is nowhere further to fall through to.
// A person who wants no reasoning asked for at all writes "off", which is a
// choice and reads back as one.
func DefaultEffortAt(profileDir string) effort.Rung {
	value, ok := persistedString(profileDir, KeyEffort)
	if !ok {
		return effort.Ship
	}
	// A rung this build does not know — an older file, a hand-edited line — is
	// the shipped rung and not silence. Silence would quietly stop asking a
	// model to think because somebody mistyped a word in a config file.
	rung, valid := effort.Parse(value)
	if !valid {
		return effort.Ship
	}
	return rung
}

// EffortWord is a rung as the settings row and the config file spell it, with
// "off" standing in for absence. It is the inverse of [effort.Parse]'s one
// leniency and the only place the substitution is made.
func EffortWord(rung effort.Rung) string {
	if rung == effort.None {
		return "off"
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

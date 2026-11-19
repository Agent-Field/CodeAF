package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/lane"
)

// THIS FILE IS THE WHOLE OF THE `response.patience` ROW, kept apart from the
// settings registry so that the registry's own file changes by two lines (the
// row's entry and the install in [InstallLaneRows]). What the number means and
// why a machine of your own is given ten times the hosted wait is said once, in
// internal/lane's localpatience.go.

const (
	// KeyResponsePatience is how many times the hosted wait bounds a model call
	// is held to before it is called dead: `auto`, or a number from 1 to 100.
	// It is the one knob for the silence and stall bounds of the transport,
	// where `response.attempts` above is the one knob for the turn's give-up,
	// and the two are different questions (see [ResponsePatienceAt]).
	KeyResponsePatience = "response.patience"
	// EnvResponsePatience pins [KeyResponsePatience] for one launch.
	EnvResponsePatience = "CODEAF_RESPONSE_PATIENCE"
)

// ── response.patience: how long a slow model may stay quiet ─────────────────

// ResponsePatienceAuto is the word for "decide by where the request goes", and
// the value a row nobody has written reads as.
const ResponsePatienceAuto = "auto"

// ParseResponsePatience reads what a person wrote for `response.patience`: the
// word `auto`, or a multiplier from [lane.MinResponsePatience] to
// [lane.MaxResponsePatience], with an optional trailing `x` (`10` and `10x` are
// the same number) and a decimal point allowed (`2.5`). It returns ZERO FOR
// `auto`, which is a number no multiplier can be.
//
// IT REFUSES RATHER THAN CLAMPS, for every value outside the range and for
// anything that is not a number. A `0.5` would be a request to give up sooner
// than the measurement says a call takes and a `500` a bound nobody is waiting
// through, and writing either quietly as the nearest legal number is a person
// told one thing about a wait and given another. The message is a plain
// sentence naming the accepted shapes; the caller prefixes the row or variable
// it came from, because the same words are read in two places.
func ParseResponsePatience(raw string) (float64, error) {
	word := strings.ToLower(strings.TrimSpace(raw))
	if word == "" || word == ResponsePatienceAuto {
		return 0, nil
	}
	number := strings.TrimSuffix(word, "x")
	value, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
	if err != nil || !(value >= lane.MinResponsePatience && value <= lane.MaxResponsePatience) {
		return 0, fmt.Errorf("want %s or a number from %s to %s (%s or %sx), got %q",
			ResponsePatienceAuto, formatNumber(lane.MinResponsePatience), formatNumber(lane.MaxResponsePatience),
			formatNumber(lane.LocalPatience), formatNumber(lane.LocalPatience), strings.TrimSpace(raw))
	}
	return value, nil
}

// FormatResponsePatience is the one spelling of a stored `response.patience`:
// `auto`, or the number and an `x` (`3x`, `2.5x`). The row reads it back, the
// writer persists it, and the manual's examples are written in it.
func FormatResponsePatience(factor float64) string {
	if factor <= 0 {
		return ResponsePatienceAuto
	}
	return formatNumber(factor) + "x"
}

// ResponsePatienceAt resolves `response.patience` for a profile, as the
// multiplier on every wait bound or ZERO FOR `auto`: environment pin, then the
// persisted row, then `auto`, the order every other number in this file
// resolves in.
//
// A MALFORMED ENVIRONMENT PIN IS AN ERROR AND A MALFORMED PERSISTED ROW IS NOT,
// the policy docs/LIMITS.md states for every pin. The variable was typed for
// this launch by somebody who can read the refusal, and a launch that
// silently ignored `CODEAF_RESPONSE_PATIENCE=fast` would wait the wrong time and
// call it the right one; a row in config.json may be old, hand-edited or
// written by a build that read it differently, and refusing to start over it
// would lock a person out of the settings page that could fix it, so it reads
// as `auto`.
//
// The persisted row may be a string (`"3x"`, the spelling this build writes) or
// a bare JSON number (`3`, the spelling a person editing the file writes).
func ResponsePatienceAt(profileDir string) (float64, error) {
	if raw := strings.TrimSpace(env.Get(EnvResponsePatience)); raw != "" {
		value, err := ParseResponsePatience(raw)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", EnvResponsePatience, err)
		}
		return value, nil
	}
	if encoded, ok := persistedValue(profileDir, KeyResponsePatience); ok {
		var text string
		if json.Unmarshal(encoded, &text) == nil {
			if value, err := ParseResponsePatience(text); err == nil {
				return value, nil
			}
			return 0, nil
		}
		var number float64
		if json.Unmarshal(encoded, &number) == nil {
			if value, err := ParseResponsePatience(strconv.FormatFloat(number, 'f', -1, 64)); err == nil {
				return value, nil
			}
		}
	}
	return 0, nil
}

// InstallResponsePatience hands the profile's `response.patience` to the
// process-wide figure the transport reads it from ([lane.UseResponsePatience]).
// It is called where the profile is loaded and where the row is written, for
// the reason [InstallLaneRows] is: the code that builds a wait bound cannot read
// a profile, and a row that reached some of the bounds and not the others would
// be a person told two different things about the same wait.
//
// A pin that will not parse installs `auto` here and is REFUSED where the
// profile is loaded (the launch errors), so the only way to reach this with one
// is a caller that has already been told.
func InstallResponsePatience(profileDir string) {
	value, _ := ResponsePatienceAt(profileDir)
	lane.UseResponsePatience(value)
}

// responsePatienceWord is what the row reads: the word `auto` or the number and
// its `x`. A pin that will not parse reads as the persisted row, because the
// launch has already refused it and the sheet is where it can be corrected.
func responsePatienceWord(profileDir string) string {
	value, err := ResponsePatienceAt(profileDir)
	if err != nil {
		value = 0
	}
	return FormatResponsePatience(value)
}

// writeResponsePatience is the row's writer: it parses, refuses with the row's
// own name in the message, persists the one canonical spelling, and installs the
// number so the very next request goes out under it.
func writeResponsePatience(profileDir, raw string) error {
	value, err := ParseResponsePatience(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", KeyResponsePatience, err)
	}
	if err := writeProfileValue(profileDir, KeyResponsePatience, FormatResponsePatience(value)); err != nil {
		return err
	}
	InstallResponsePatience(profileDir)
	return nil
}

// responsePatienceSetting is the settings row for [KeyResponsePatience]. It
// reads what the product will use, refuses a bad value naming the row, and — by
// way of [writeResponsePatience] — takes effect on the very next request.
func responsePatienceSetting(dir string) Setting {
	return Setting{
		Key: KeyResponsePatience, Category: CategoryModels, Kind: SettingText,
		Label: "patience", Env: EnvResponsePatience, EmptyLabel: ResponsePatienceAuto,
		Hint: "how long codeaf waits on a quiet model before it calls the request dead, as a multiple of the " +
			"wait it gives a hosted model. auto waits 1x for a hosted model and " + formatNumber(lane.LocalPatience) +
			"x for one on your own machine (Ollama, LM Studio, a box on your network), which can spend minutes " +
			"reading a long prompt. A number from " + formatNumber(lane.MinResponsePatience) + " to " +
			formatNumber(lane.MaxResponsePatience) + ", like 3 or 3x, applies to every model. " +
			"A change lands on the next call.",
		read:  func() string { return responsePatienceWord(dir) },
		write: func(raw string) error { return writeResponsePatience(dir, raw) },
	}
}

// ResponsePatienceHelp is the `codeaf help env` entry for the variable, with
// the figures read from [lane.LocalPatience] and the range so the table cannot
// drift from what the transport does.
func ResponsePatienceHelp() string {
	return "  " + EnvResponsePatience + "\n" +
		"                       how long a quiet model is waited on, as a multiple of the\n" +
		"                       hosted wait: auto (default) is 1 for a hosted model and\n" +
		"                       " + formatNumber(lane.LocalPatience) + " for one on your own machine, or a number from\n" +
		"                       " + formatNumber(lane.MinResponsePatience) + " to " + formatNumber(lane.MaxResponsePatience) + " (3 or 3x) for every model\n"
}

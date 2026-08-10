package specidentifiers

import "testing"

// Validation contract for the backtick `code` fallback, derived from the
// commander-2342 benchmark run, where a correct 34-line patch was failed
// because the gate demanded the agent paste `🚀🚀🚀🚀` and the string
// `mycommand one --help` into lib/command.js:
//
//   - a backticked token is enforceable only when it could be a name the fix
//     must contain verbatim;
//   - sample output quoted in prose (command invocations, letterless runs,
//     inline statements) is evidence, not a requirement;
//   - the gate's original purpose survives: real export/option names stated in
//     the spec are still enforced, so paraphrasing them is still caught.
func TestEnforceableCodeNames(t *testing.T) {
	enforcedValues := func(text string) []string {
		out := []string{}
		for _, id := range EnforceableIdentifiers(ExtractSpecIdentifiers(text)) {
			out = append(out, id.Value)
		}
		return out
	}
	has := func(values []string, want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}

	t.Run("rejects", func(t *testing.T) {
		for _, tc := range []struct {
			name, text, token string
		}{
			{"command invocation in prose", "When you do `mycommand one --help` it uses 200.", "mycommand one --help"},
			{"letterless run", "It prints `🚀🚀🚀🚀` at the bottom, which is expected.", "🚀🚀🚀🚀"},
			{"inline statement", "Today it does `this._out = source._out` instead.", "this._out = source._out"},
			{"bare operator", "The check uses `===` here.", "==="},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := enforcedValues(tc.text); has(got, tc.token) {
					t.Errorf("enforced %q from prose; enforced set = %v", tc.token, got)
				}
			})
		}
	})

	t.Run("still enforces real names", func(t *testing.T) {
		for _, tc := range []struct {
			name, text, token string
		}{
			{"export name", "Add `copyInheritedSettings` to the prototype.", "copyInheritedSettings"},
			{"method name", "It must call `configureOutput` first.", "configureOutput"},
			{"flag", "Add the `--no-color` flag to `renderHelp`.", "--no-color"},
			{"path", "Modify `src/rules/auto-toc.ts` only.", "src/rules/auto-toc.ts"},
			{"marker keeps its whitespace", "Emit `<!-- toc -->` at the top.", "<!-- toc -->"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := enforcedValues(tc.text); !has(got, tc.token) {
					t.Errorf("dropped %q, which the spec states verbatim; enforced set = %v", tc.token, got)
				}
			})
		}
	})

	t.Run("quoted multiword display names are unaffected", func(t *testing.T) {
		// These come from the separate quoted-phrase branch, not the backtick
		// scanner, so the single-token rule must not reach them.
		got := enforcedValues(`The heading must read "Auto Table of Contents" exactly.`)
		if !has(got, "Auto Table of Contents") {
			t.Errorf("dropped quoted display name; enforced set = %v", got)
		}
	})
}

// A regex quoted in an issue is notation, not a name the fix must contain.
// werkzeug-3146's reporter offered two fixes and upstream took the one that
// does NOT touch the regex, so enforcing the pattern steered the agent wrong.
func TestRegexNotationIsNotEnforced(t *testing.T) {
	enforced := func(text string) []string {
		out := []string{}
		for _, id := range EnforceableIdentifiers(ExtractSpecIdentifiers(text)) {
			out = append(out, id.Value)
		}
		return out
	}
	contains := func(values []string, want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}

	t.Run("rejects patterns", func(t *testing.T) {
		for _, token := range []string{`\d+\.\d+(e[+-]/d+)?`, `\d+\.\d+`, `^\w+\s*=\s*\d+$`} {
			text := "Adjust the regex to `" + token + "` instead."
			if got := enforced(text); contains(got, token) {
				t.Errorf("enforced regex %q; enforced set = %v", token, got)
			}
		}
	})

	t.Run("keeps names and non-regex backslashes", func(t *testing.T) {
		for _, tc := range []struct{ text, token string }{
			{"Call `num_convert` in `to_url`.", "num_convert"},
			{"The installer writes `C:\\Users\\app`.", `C:\Users\app`},
		} {
			if got := enforced(tc.text); !contains(got, tc.token) {
				t.Errorf("dropped %q; enforced set = %v", tc.token, got)
			}
		}
	})

	t.Run("a numeric output value is not a source identifier", func(t *testing.T) {
		// An expected runtime value is not a string the source must contain.
		// werkzeug-3146's issue shows `0.000010` as desired output, while
		// upstream's fix is f"{...:f}".rstrip("0") — which never contains it.
		if got := enforced("It must emit `0.000010` exactly."); contains(got, "0.000010") {
			t.Errorf("enforced a runtime output value: %v", got)
		}
	})
}

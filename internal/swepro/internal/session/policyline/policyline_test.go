package policyline

import (
	"strings"
	"testing"
)

// policy-line.ts ships no .test.ts of its own, so these subtests encode the
// validation contract directly: caller-observable behaviour, phrased the way
// the module's doc comment phrases it.

func ptr(s string) *string { return &s }

func TestPolicyLine(t *testing.T) {
	t.Run("returns undefined for an undefined description", func(t *testing.T) {
		if got := PolicyLine(nil, "file_scope"); got != nil {
			t.Fatalf("got %q, want nil", *got)
		}
	})

	t.Run("extracts the value of a key: value line", func(t *testing.T) {
		got := PolicyLine(ptr("file_scope: src/**"), "file_scope")
		if got == nil || *got != "src/**" {
			t.Fatalf("got %v, want src/**", got)
		}
	})

	t.Run("is case-insensitive on the key", func(t *testing.T) {
		got := PolicyLine(ptr("FILE_SCOPE: a"), "file_scope")
		if got == nil || *got != "a" {
			t.Fatalf("got %v, want a", got)
		}
	})

	t.Run("is anchored per line", func(t *testing.T) {
		if got := PolicyLine(ptr("x file_scope: a"), "file_scope"); got != nil {
			t.Fatalf("got %q, want nil", *got)
		}
		got := PolicyLine(ptr("intro\nfile_scope: a"), "file_scope")
		if got == nil || *got != "a" {
			t.Fatalf("got %v, want a", got)
		}
	})

	t.Run("anchors after every JS line terminator", func(t *testing.T) {
		for _, sep := range []string{"\n", "\r", "\u2028", "\u2029"} {
			got := PolicyLine(ptr("intro"+sep+"file_scope: a"), "file_scope")
			if got == nil || *got != "a" {
				t.Fatalf("sep %q: got %v, want a", sep, got)
			}
		}
	})

	t.Run("the value stops at every JS line terminator", func(t *testing.T) {
		for _, sep := range []string{"\n", "\r", "\u2028", "\u2029"} {
			got := PolicyLine(ptr("file_scope: a"+sep+"b"), "file_scope")
			if got == nil || *got != "a" {
				t.Fatalf("sep %q: got %v, want a", sep, got)
			}
		}
	})

	t.Run("first match wins", func(t *testing.T) {
		got := PolicyLine(ptr("file_scope: a\nfile_scope: b"), "file_scope")
		if got == nil || *got != "a" {
			t.Fatalf("got %v, want a", got)
		}
	})

	t.Run("the whitespace run after the colon may cross a newline", func(t *testing.T) {
		got := PolicyLine(ptr("file_scope:\n  a\nb"), "file_scope")
		if got == nil || *got != "a" {
			t.Fatalf("got %v, want a", got)
		}
	})

	t.Run("accepts the full JS whitespace class after the colon", func(t *testing.T) {
		for _, ws := range []string{"\t", "\v", "\f", " ", "\u00a0", "\u1680", "\u2000", "\u200a", "\u202f", "\u205f", "\u3000", "\ufeff"} {
			got := PolicyLine(ptr("file_scope:"+ws+"a"), "file_scope")
			if got == nil || *got != "a" {
				t.Fatalf("ws %q: got %v, want a", ws, got)
			}
		}
	})

	t.Run("returns undefined when the line has no value", func(t *testing.T) {
		if got := PolicyLine(ptr("file_scope:"), "file_scope"); got != nil {
			t.Fatalf("got %q, want nil", *got)
		}
	})

	t.Run("returns empty string when the value is only whitespace", func(t *testing.T) {
		got := PolicyLine(ptr("file_scope:   "), "file_scope")
		if got == nil || *got != "" {
			t.Fatalf("got %v, want empty string", got)
		}
	})

	// Bug-for-bug: the key is interpolated into the RegExp source unescaped.
	t.Run("a capture group in the key hijacks the returned group", func(t *testing.T) {
		got := PolicyLine(ptr("a: 1"), "(a|b)")
		if got == nil || *got != "a" {
			t.Fatalf("got %v, want a (the key's own group, not the value)", got)
		}
	})

	t.Run("a bare alternation in the key can match with no group at all", func(t *testing.T) {
		if got := PolicyLine(ptr("a: 1\nb: 2"), "a|b"); got != nil {
			t.Fatalf("got %q, want nil", *got)
		}
	})

	t.Run("JS case folding does not reach U+017F, U+212A or U+1E9E", func(t *testing.T) {
		if got := PolicyLine(ptr("file_\u017fcope: a"), "file_scope"); got != nil {
			t.Fatalf("U+017F: got %q, want nil", *got)
		}
		if got := PolicyLine(ptr("\u212aey: a"), "key"); got != nil {
			t.Fatalf("U+212A: got %q, want nil", *got)
		}
		if got := PolicyLine(ptr("große: a"), "gro\u1e9ee"); got != nil {
			t.Fatalf("U+1E9E: got %q, want nil", *got)
		}
	})

	t.Run("an uncompilable key panics like new RegExp throws", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected a panic")
			}
			if !strings.Contains(r.(string), "policyline: cannot compile key") {
				t.Fatalf("unexpected panic %v", r)
			}
		}()
		PolicyLine(ptr("x: 1"), "(?=x)")
	})
}

func TestParseFileScope(t *testing.T) {
	eq := func(t *testing.T, got []string, want []string) {
		t.Helper()
		if (got == nil) != (want == nil) {
			t.Fatalf("nil-ness mismatch: got %#v, want %#v", got, want)
		}
		if len(got) != len(want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		}
	}

	t.Run("returns undefined for undefined", func(t *testing.T) {
		eq(t, ParseFileScope(nil), nil)
	})

	t.Run("returns undefined for empty and blank input", func(t *testing.T) {
		eq(t, ParseFileScope(ptr("")), nil)
		eq(t, ParseFileScope(ptr("   ")), nil)
		eq(t, ParseFileScope(ptr("\ufeff")), nil)
	})

	t.Run("returns undefined for the unknown until probe sentinel", func(t *testing.T) {
		eq(t, ParseFileScope(ptr("unknown until probe")), nil)
		eq(t, ParseFileScope(ptr("  unknown until probe  ")), nil)
	})

	t.Run("the sentinel check is exact and case-sensitive", func(t *testing.T) {
		eq(t, ParseFileScope(ptr("Unknown Until Probe")), []string{"Unknown Until Probe"})
		eq(t, ParseFileScope(ptr("unknown until probe, x")), []string{"unknown until probe", "x"})
	})

	t.Run("splits on commas and newlines", func(t *testing.T) {
		eq(t, ParseFileScope(ptr("a, b, c")), []string{"a", "b", "c"})
		eq(t, ParseFileScope(ptr("a\nb")), []string{"a", "b"})
		eq(t, ParseFileScope(ptr("a,\nb")), []string{"a", "b"})
	})

	t.Run("does not split on a carriage return, but trims it", func(t *testing.T) {
		eq(t, ParseFileScope(ptr("a\r\nb")), []string{"a", "b"})
		eq(t, ParseFileScope(ptr("a\rb")), []string{"a\rb"})
	})

	t.Run("drops empty parts and can return an empty list", func(t *testing.T) {
		eq(t, ParseFileScope(ptr("a,,b")), []string{"a", "b"})
		eq(t, ParseFileScope(ptr(",,,")), []string{})
	})

	t.Run("strips at most one quote from each end", func(t *testing.T) {
		eq(t, ParseFileScope(ptr(`"a"`)), []string{"a"})
		eq(t, ParseFileScope(ptr(`'a'`)), []string{"a"})
		eq(t, ParseFileScope(ptr(`"a`)), []string{"a"})
		eq(t, ParseFileScope(ptr(`a"`)), []string{"a"})
		eq(t, ParseFileScope(ptr(`'a"`)), []string{"a"})
		eq(t, ParseFileScope(ptr(`"'a'"`)), []string{"'a'"})
	})

	t.Run("a lone quote collapses to nothing", func(t *testing.T) {
		eq(t, ParseFileScope(ptr(`"`)), []string{})
		eq(t, ParseFileScope(ptr(`'`)), []string{})
		eq(t, ParseFileScope(ptr(`""`)), []string{})
	})

	t.Run("splitting happens before quote stripping", func(t *testing.T) {
		eq(t, ParseFileScope(ptr(`"a,b"`)), []string{"a", "b"})
	})
}

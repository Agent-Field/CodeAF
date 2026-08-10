package hygiene

// Translation of src/session/hygiene.test.ts — verbatim describe/test names,
// same assertion semantics. The TS helpers `reasons` and `scratchFiles` are
// reproduced below; `.sort()` is JS's default sort (stable, UTF-16 code-unit
// order), so jsSortStrings stands in for sort.Strings.

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"
)

// ── TS test helpers ─────────────────────────────────────────────────────────

func classifyWithAllow(files []string, allow []string) HygieneVerdict {
	in := ClassifyInput{ChangedFiles: files}
	if allow != nil {
		in.Allow = allow
	}
	return ClassifyLeftovers(in)
}

func reasons(files []string, allow []string) []string {
	v := classifyWithAllow(files, allow)
	out := []string{}
	for _, f := range v.Scratch {
		out = append(out, f.Reason)
	}
	return out
}

func scratchFiles(files []string, allow []string) []string {
	v := classifyWithAllow(files, allow)
	out := []string{}
	for _, f := range v.Scratch {
		out = append(out, f.File)
	}
	return out
}

// jsSortStrings is Array.prototype.sort() with the default comparator: stable,
// comparing UTF-16 code-unit sequences (NOT Go's UTF-8 byte order — the two
// disagree for astral-plane characters).
func jsSortStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return utf16Less(out[i], out[j]) })
	return out
}

func utf16Less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func eqStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func mustMatch(t *testing.T, s, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		t.Errorf("%q does not match /%s/", s, pattern)
	}
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("%q does not contain %q", s, sub)
	}
}

// ── classifyLeftovers — (a) backup / editor artifacts ───────────────────────

func TestClassifyLeftoversBackupEditorArtifacts(t *testing.T) {
	t.Run("plain .bak is scratch", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/util.py.bak"}})
		if len(v.Scratch) != 1 {
			t.Fatalf("scratch length = %d, want 1", len(v.Scratch))
		}
		mustMatch(t, v.Scratch[0].Reason, `(?i)backup/editor artifact`)
	})

	t.Run("compound <changed-file>.bak reports a shadow reason", func(t *testing.T) {
		files := []string{"src/click/core.py", "src/click/core.py.bak"}
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: files})
		eqStrings(t, scratchFiles(files, nil), []string{"src/click/core.py.bak"})
		var finding *HygieneFinding
		for i := range v.Scratch {
			if v.Scratch[i].File == "src/click/core.py.bak" {
				finding = &v.Scratch[i]
				break
			}
		}
		if finding == nil {
			t.Fatal("no finding for src/click/core.py.bak")
		}
		mustMatch(t, finding.Reason, `(?i)shadow`)
		mustContain(t, finding.Reason, "src/click/core.py")
	})

	t.Run(".orig / .rej / .tmp / .swp / .old / .save and trailing ~ all flagged", func(t *testing.T) {
		files := []string{"a.orig", "b.rej", "c.tmp", ".d.swp", "e.old", "f.save", "g.ts~"}
		eqStrings(t, jsSortStrings(scratchFiles(files, nil)), jsSortStrings(files))
	})

	t.Run("NEGATIVE: a real source file with a normal extension is clean", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/util.py"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
		eqStrings(t, v.Clean, []string{"src/util.py"})
	})
}

// ── classifyLeftovers — (b) scaffolding names ───────────────────────────────

func TestClassifyLeftoversScaffoldingNames(t *testing.T) {
	t.Run("probe_multiple_ellipsis.py under tests/ is scratch", func(t *testing.T) {
		r := reasons([]string{"tests/probe_multiple_ellipsis.py"}, nil)
		if len(r) == 0 {
			t.Fatal("no scratch findings")
		}
		mustMatch(t, r[0], `probe_`)
	})

	t.Run("scratch_ / debug_ / temp_ / tmp_ / __grader prefixes flagged anywhere", func(t *testing.T) {
		files := []string{
			"scratch_x.ts",
			"pkg/debug_run.go",
			"temp_out.json",
			"a/b/tmp_notes.txt",
			"__grader.py",
		}
		eqStrings(t, jsSortStrings(scratchFiles(files, nil)), jsSortStrings(files))
	})

	t.Run("verify_*_done_criteria one-off grader is scratch", func(t *testing.T) {
		r := reasons([]string{"tests/verify_ellipsis_done_criteria.py"}, nil)
		if len(r) == 0 {
			t.Fatal("no scratch findings")
		}
		mustMatch(t, r[0], `(?i)one-off scaffolding`)
	})

	t.Run("NEGATIVE: probe.ts (no underscore stem) is clean", func(t *testing.T) {
		// `probe_` is the prefix — a substantive file merely containing "probe" is fine.
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/probe.ts"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})

	t.Run("NEGATIVE: verify_email.py (not the done-criteria grader) is clean", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/verify_email.py"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})
}

// ── classifyLeftovers — (b) location-scoped stems ───────────────────────────

func TestClassifyLeftoversLocationScopedStems(t *testing.T) {
	t.Run("notes.md under src/ is scratch", func(t *testing.T) {
		r := reasons([]string{"src/session/notes.md"}, nil)
		if len(r) == 0 {
			t.Fatal("no scratch findings")
		}
		mustMatch(t, r[0], `(?i)scratch stem`)
	})

	t.Run("scratch.py under tests/ is scratch", func(t *testing.T) {
		r := reasons([]string{"tests/scratch.py"}, nil)
		if len(r) == 0 {
			t.Fatal("no scratch findings")
		}
		mustMatch(t, r[0], `(?i)scratch stem`)
	})

	t.Run("NEGATIVE: root-level NOTES.md in a docs change is clean", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"NOTES.md", "docs/guide.md"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
		eqStrings(t, jsSortStrings(v.Clean), jsSortStrings([]string{"NOTES.md", "docs/guide.md"}))
	})

	t.Run("NEGATIVE: notes.md under docs/ (not a source tree) is clean", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"docs/notes.md"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})
}

// ── classifyLeftovers — (c) well-known junk ─────────────────────────────────

func TestClassifyLeftoversWellKnownJunk(t *testing.T) {
	t.Run(".DS_Store / Thumbs.db / *.pyc / __pycache__ / node_modules flagged", func(t *testing.T) {
		files := []string{
			".DS_Store",
			"sub/Thumbs.db",
			"pkg/mod.pyc",
			"pkg/__pycache__/mod.cpython-311.pyc",
			"node_modules/left-pad/index.js",
		}
		// every one is scratch (some by suffix, some by segment)
		eqStrings(t, jsSortStrings(scratchFiles(files, nil)), jsSortStrings(files))
	})

	t.Run("NEGATIVE: a file named 'store.ts' is not .DS_Store", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/store.ts"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})
}

// ── classifyLeftovers — (d) allowlist ───────────────────────────────────────

func TestClassifyLeftoversAllowlist(t *testing.T) {
	t.Run(".codeaf/contract.json is clean via built-in allowlist", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{".codeaf/contract.json"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
		eqStrings(t, v.Clean, []string{".codeaf/contract.json"})
	})

	t.Run("built-in allowlist wins even for a would-be scratch name", func(t *testing.T) {
		// .codeaf/scratch_state.json shape-matches scaffolding but is allowed state.
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{".codeaf/scratch_state.json"}})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})

	t.Run("caller allow: exact path match wins", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{
			ChangedFiles: []string{"tests/probe_keep.py"},
			Allow:        []string{"tests/probe_keep.py"},
		})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})

	t.Run("caller allow: prefix/ match wins", func(t *testing.T) {
		files := []string{"fixtures/probe_a.py", "fixtures/scratch_b.py", "src/scratch_c.py"}
		allow := []string{"fixtures/"}
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: files, Allow: allow})
		eqStrings(t, scratchFiles(files, allow), []string{"src/scratch_c.py"})
		found := false
		for _, c := range v.Clean {
			if c == "fixtures/probe_a.py" {
				found = true
			}
		}
		if !found {
			t.Errorf("clean %#v does not contain fixtures/probe_a.py", v.Clean)
		}
	})

	t.Run("caller allow: trailing ** is accepted as a prefix", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{
			ChangedFiles: []string{"gen/temp_out.json"},
			Allow:        []string{"gen/**"},
		})
		if len(v.Scratch) != 0 {
			t.Errorf("scratch = %#v, want []", v.Scratch)
		}
	})
}

// ── classifyLeftovers — normalization & edge cases ──────────────────────────

func TestClassifyLeftoversNormalizationAndEdgeCases(t *testing.T) {
	t.Run("empty input yields empty verdict", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{}})
		if !reflect.DeepEqual(v, HygieneVerdict{Scratch: []HygieneFinding{}, Clean: []string{}}) {
			t.Errorf("got %#v, want empty verdict", v)
		}
	})

	t.Run("Windows-style separators are normalized", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{
			ChangedFiles: []string{`src\click\core.py`, `src\click\core.py.bak`},
		})
		var finding *HygieneFinding
		for i := range v.Scratch {
			if v.Scratch[i].File == "src/click/core.py.bak" {
				finding = &v.Scratch[i]
				break
			}
		}
		if finding == nil {
			t.Fatal("finding is undefined")
		}
		// shadow reason after normalization
		mustContain(t, finding.Reason, "src/click/core.py")
	})

	t.Run("duplicate paths are de-duplicated", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"a.bak", "a.bak"}})
		if len(v.Scratch) != 1 {
			t.Errorf("scratch length = %d, want 1", len(v.Scratch))
		}
	})
}

// ── hygienePromptBlock ──────────────────────────────────────────────────────

func TestHygienePromptBlock(t *testing.T) {
	t.Run("returns null when nothing is scratch", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/util.py"}})
		if got := HygienePromptBlock(v); got != nil {
			t.Errorf("got %q, want nil", *got)
		}
	})

	t.Run("numbered block lists exactly the scratch files with reasons", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{
			ChangedFiles: []string{"src/click/core.py", "src/click/core.py.bak", "tests/probe_x.py"},
		})
		blockPtr := HygienePromptBlock(v)
		if blockPtr == nil {
			t.Fatal("block is null")
		}
		block := *blockPtr
		mustContain(t, block, "1. src/click/core.py.bak")
		mustContain(t, block, "2. tests/probe_x.py")
		mustMatch(t, block, `(?i)NO other changes`)
		if strings.Contains(block, "src/click/core.py —") { // the real file is never listed
			t.Errorf("block lists the real file:\n%s", block)
		}
	})
}

// ── isTreeClean ─────────────────────────────────────────────────────────────

func TestIsTreeClean(t *testing.T) {
	t.Run("true for a clean tree", func(t *testing.T) {
		if !IsTreeClean([]string{"src/util.py", "NOTES.md"}, nil) {
			t.Error("want true")
		}
	})

	t.Run("false when scratch remains", func(t *testing.T) {
		if IsTreeClean([]string{"src/util.py", "src/util.py.bak"}, nil) {
			t.Error("want false")
		}
	})

	t.Run("allowlist is honored", func(t *testing.T) {
		if !IsTreeClean([]string{"tests/probe_x.py"}, []string{"tests/probe_x.py"}) {
			t.Error("want true")
		}
	})
}

// ── Port-specific coverage (no TS counterpart) ──────────────────────────────

// TestLineTerminatorsInRegexDot pins the one place where RE2's `.` and JS's `.`
// disagree. Expectations were read off the real TS module; they are kept out of
// the JSON fixtures because Go's encoding/json escapes U+2028/U+2029 where
// JSON.stringify emits them literally, which would break the byte-for-byte gate
// for a reason unrelated to this module.
func TestLineTerminatorsInRegexDot(t *testing.T) {
	const ls = "\u2028"  // LINE SEPARATOR
	const ps = "\u2029"  // PARAGRAPH SEPARATOR
	const nel = "\u0085" // NEXT LINE (not a JS LineTerminator)

	cases := []struct {
		name    string
		file    string
		scratch bool
	}{
		{"U+2028 LINE SEPARATOR blocks the dot", "verify_a" + ls + "b_done_criteria.py", false},
		{"U+2029 PARAGRAPH SEPARATOR blocks the dot", "verify_a" + ps + "b_done_criteria.py", false},
		{"CR blocks the dot", "verify_a\rb_done_criteria.py", false},
		{"LF blocks the dot", "verify_a\nb_done_criteria.py", false},
		{"U+0085 NEL is not a JS line terminator", "verify_a" + nel + "b_done_criteria.py", true},
		{"TAB is matched by the dot", "verify_a\tb_done_criteria.py", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{tc.file}})
			if got := len(v.Scratch) == 1; got != tc.scratch {
				t.Errorf("scratch=%v, want %v (verdict %#v)", got, tc.scratch, v)
			}
		})
	}

	t.Run("U+2028 is JS whitespace and is trimmed away", func(t *testing.T) {
		v := ClassifyLeftovers(ClassifyInput{ChangedFiles: []string{"src/scratch." + ls}})
		want := HygieneVerdict{
			Scratch: []HygieneFinding{{
				File:   "src/scratch.",
				Reason: "scratch stem (scratch.) under a source/test tree",
			}},
			Clean: []string{},
		}
		if !reflect.DeepEqual(v, want) {
			t.Errorf("got %#v, want %#v", v, want)
		}
	})
}

// TestJSLowerCaseFullMapping pins the two SpecialCasing rules that
// strings.ToLower alone does not implement.
func TestJSLowerCaseFullMapping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ABC", "abc"},
		{"İ", "i̇"},   // LATIN CAPITAL LETTER I WITH DOT ABOVE
		{"İX", "i̇x"}, //
		{"Σ", "σ"},    // lone sigma: no preceding cased letter
		{"ΑΣ", "ας"},  // word-final sigma
		{"ΣΑ", "σα"},  // followed by a cased letter
		{"ΑΣ.", "ας."},
		{"ΑΣ́", "ας́"}, // combining mark is case-ignorable
		{"a/Σ", "a/σ"}, // "/" is neither cased nor case-ignorable
		{"K", "k"},     // KELVIN SIGN
		{"ſ", "ſ"},     // LONG S lowercases to itself
	}
	for _, tc := range cases {
		if got := jsLowerCase(tc.in); got != tc.want {
			t.Errorf("jsLowerCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

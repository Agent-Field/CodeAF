package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// buildTerrain lays out a workspace on disk. A path ending in a slash is a
// directory; everything else is a file with the given contents. Directories are
// created for the files that need them, so a case only has to name what it is
// actually testing.
func buildTerrain(t *testing.T, layout map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range layout {
		full := filepath.Join(root, filepath.FromSlash(path))
		if strings.HasSuffix(path, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestRenderTerrainSaysNothingWithoutMaterial covers the compatibility promise
// from the other side. Every one of these is a workspace the render has nothing
// true to say about, and in every one of them the answer has to be exactly the
// empty string — because that is what leaves the planning prompts byte for byte
// the prompts a run without a workspace has always sent.
func TestRenderTerrainSaysNothingWithoutMaterial(t *testing.T) {
	empty := t.TempDir()
	onlyPlumbing := buildTerrain(t, map[string]string{
		".hidden":                    "invisible",
		".config/settings.ini":       "x=1",
		"node_modules/left/index.js": "module.exports = {}",
		"__pycache__/cached.pyc":     "\x00\x00",
	})
	for _, testcase := range []struct {
		name string
		dir  string
	}{
		{"no directory named at all", ""},
		{"only whitespace for a directory", "   "},
		{"a directory that is not there", filepath.Join(empty, "absent")},
		{"a directory with nothing in it", empty},
		{"a directory holding only plumbing and bulk", onlyPlumbing},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			if got := RenderTerrain(testcase.dir, "write the quarterly summary"); got != "" {
				t.Errorf("terrain = %q, want the empty string so the prompt is unchanged", got)
			}
		})
	}
}

// TestRenderTerrainIsByteStable is the invariant the shared prefix rests on. The
// same workspace and the same goal have to draw the same bytes every time, or
// every cache hit behind the preamble is lost and two passes can plan from two
// different pictures.
func TestRenderTerrainIsByteStable(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"responses/north.csv":     "a,b\n1,2\n",
		"responses/south.csv":     "a,b\n3,4\n",
		"responses/raw/dump.json": "{}",
		"sources/one.pdf":         "%PDF",
		"sources/two.pdf":         "%PDF",
		"drafts/summary.md":       "# draft\n",
		"README.md":               "The 2024 constituency responses.\n",
		"notes.txt":               "loose ends\n",
	})
	const goal = "summarise the survey responses by region"

	first := RenderTerrain(root, goal)
	if first == "" {
		t.Fatal("a workspace with material rendered nothing")
	}
	for pass := 0; pass < 4; pass++ {
		if again := RenderTerrain(root, goal); again != first {
			t.Fatalf("render %d differs from the first:\n%s\n---\n%s", pass, first, again)
		}
	}
	if !utf8.ValidString(first) {
		t.Error("the render is not valid UTF-8")
	}
}

// TestRenderTerrainStaysUnderTheCap holds the price of the block down. It is
// paid by every planning call in the run, so a workspace with very long names or
// a great many directories may not buy itself a larger share of the prompt.
func TestRenderTerrainStaysUnderTheCap(t *testing.T) {
	layout := map[string]string{}
	long := strings.Repeat("観測", 90) // 3-byte runes, so a naive cut lands mid-character
	for _, suffix := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n"} {
		layout[long+suffix+"/entry.csv"] = "1,2\n"
	}
	root := buildTerrain(t, layout)

	got := RenderTerrain(root, "read everything")
	// The marker is charged against the cap, not added on top of it: the cap is
	// the promise made to every planning call in the run.
	if len(got) > terrainBytes {
		t.Fatalf("terrain is %d bytes, over the %d-byte cap", len(got), terrainBytes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("an over-length render should say it was cut:\n%s", got)
	}
	if !utf8.ValidString(got) {
		t.Error("the cap cut a character in half; every planning call would carry the mangled rune")
	}
}

// TestRenderTerrainClipsTheOpeningLineOnARuneBoundary is the same guarantee one
// level down. Whoever wrote the README is free to put a paragraph on its first
// line, in any script.
func TestRenderTerrainClipsTheOpeningLineOnARuneBoundary(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"README.md": "# " + strings.Repeat("観測記録", 200) + "\nsecond line\n",
		"data.csv":  "a,b\n",
	})
	got := RenderTerrain(root, "read everything")
	opening := strings.SplitN(got, "\n", 2)[0]

	if !strings.HasPrefix(opening, "README.md: ") {
		t.Fatalf("the README line is missing:\n%s", got)
	}
	if len(opening) > len("README.md: ")+terrainReadmeBytes {
		t.Errorf("the opening line was not clipped: %d bytes", len(opening))
	}
	if !utf8.ValidString(got) {
		t.Error("the opening line was cut in the middle of a character")
	}
	if strings.Contains(got, "second line") {
		t.Error("more than the first line of the README was taken")
	}
}

// TestRenderTerrainOpensOnlyTheDirectoriesTheGoalNames pins the whole of the
// relevance judgment: deterministic string matching, equality with one plural
// either way, and nothing looser. The substring cases are the ones that matter —
// a rule that opened every directory whose name contains a goal word would spend
// the entire budget on the level that was not asked about.
func TestRenderTerrainOpensOnlyTheDirectoriesTheGoalNames(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"responses/north.csv":  "a\n",
		"metadata/schema.json": "{}",
		"archive/old.zip":      "PK",
	})
	for _, testcase := range []struct {
		name   string
		goal   string
		opened []string
		closed []string
	}{
		{
			name:   "the exact name",
			goal:   "check the metadata",
			opened: []string{"schema.json"},
			closed: []string{"north.csv", "old.zip"},
		},
		{
			name:   "the goal says the plural, the directory is singular",
			goal:   "read the archives end to end",
			opened: []string{"old.zip"},
			closed: []string{"north.csv", "schema.json"},
		},
		{
			name:   "the goal says the singular, the directory is plural",
			goal:   "summarise every response",
			opened: []string{"north.csv"},
			closed: []string{"schema.json", "old.zip"},
		},
		{
			name:   "a goal word inside a longer name opens nothing",
			goal:   "tabulate the data",
			closed: []string{"schema.json", "north.csv", "old.zip"},
		},
		{
			name:   "a goal that names none of them opens none of them",
			goal:   "write a short poem",
			closed: []string{"schema.json", "north.csv", "old.zip"},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			got := RenderTerrain(root, testcase.goal)
			for _, want := range testcase.opened {
				if !strings.Contains(got, "  "+want) {
					t.Errorf("the goal named this directory but it was not opened; %q missing:\n%s", want, got)
				}
			}
			for _, unwanted := range testcase.closed {
				if strings.Contains(got, unwanted) {
					t.Errorf("a directory the goal never named was opened; %q present:\n%s", unwanted, got)
				}
			}
			// The first level is unconditional whichever way the cues fell.
			for _, always := range []string{"responses/", "metadata/", "archive/"} {
				if !strings.Contains(got, always) {
					t.Errorf("the depth-1 listing lost %q:\n%s", always, got)
				}
			}
		})
	}
}

// TestRenderTerrainKeepsTheNamedDirectoryWhenThereAreTooManyToShow is the hole
// the cue mechanism used to fall into. Selection truncated alphabetically and
// only then asked which names the goal had mentioned, so in a workspace with
// more directories than fit — which is every workspace the cue was written for —
// the one the goal was about could be dropped before it was ever considered.
func TestRenderTerrainKeepsTheNamedDirectoryWhenThereAreTooManyToShow(t *testing.T) {
	layout := map[string]string{"zebra-responses/north.csv": "a\n"}
	for index := 0; index < terrainDirs+6; index++ {
		layout[string(rune('a'+index))+"-filler/thing.txt"] = "x\n"
	}
	root := buildTerrain(t, layout)

	got := RenderTerrain(root, "summarise the responses")
	if !strings.Contains(got, "zebra-responses/") {
		t.Fatalf("the directory the goal named was truncated away:\n%s", got)
	}
	if !strings.Contains(got, "  north.csv") {
		t.Errorf("the named directory was listed but never opened:\n%s", got)
	}
	// Alphabetical order survives the cue-first selection.
	if index := strings.Index(got, "zebra-responses/"); index >= 0 && strings.Index(got, "a-filler/") > index {
		t.Errorf("the rows are no longer in name order:\n%s", got)
	}
}

// TestRenderTerrainMatchesCuesAcrossSeparatorsAndForms covers the tokenizer on
// both sides of the match. Only the goal used to be split, so a separator in a
// directory name defeated the cue outright, and two spellings of the same
// character were compared as bytes and called strangers.
func TestRenderTerrainMatchesCuesAcrossSeparatorsAndForms(t *testing.T) {
	for _, testcase := range []struct {
		name      string
		directory string
		goal      string
		opens     bool
	}{
		{"a hyphen in the name", "survey-responses", "summarise the survey", true},
		{"an underscore in the name", "survey_responses", "summarise the survey", true},
		{"a dot in the name", "survey.responses", "summarise the responses", true},
		{"the goal writes it hyphenated", "surveys", "check the survey-responses", true},
		{"composed against decomposed", "re\u0301sume\u0301s", "read the r\u00e9sum\u00e9s", true},
		{"different case", "RESPONSES", "summarise the responses", true},
		// Lowercasing is not folding, and the difference is not pedantic: a
		// directory named in one of these forms could never answer to a goal
		// that named it in the other.
		{"the sharp s against a double s", "Stra\u00dfe", "photograph the strasse", true},
		{"a Greek final sigma against a medial one", "\u03a3\u0399\u03a3\u03a5\u03a6\u039f\u03a3", "read the \u03c3\u03b9\u03c3\u03c5\u03c6\u03bf\u03c2 notes", true},
		// Folding decomposes this one \u2014 \u0390 becomes iota plus two combining marks \u2014
		// and a combining mark is neither letter nor digit, so without recomposing
		// afterwards the splitter tears "\u03ba\u03b1\u0390\u03ba\u03b9" into "\u03ba\u03b1" and "\u03ba\u03b9" and rejoins
		// nothing. The name survives as itself only if it is put back together.
		{"a character folding into combining marks", "\u03ba\u03b1\u0390\u03ba\u03b9", "photograph the \u03ba\u03b1\u0390\u03ba\u03b9", true},
		// The same shredding read the other way, which is the damaging half: the
		// fragments of a torn name can spell a different and much commoner word \u2014
		// here "\u03ba\u03b1\u03b9", Greek for "and" \u2014 and every goal containing that word would
		// open a directory that has nothing to do with it.
		{"a torn name must not spell a commoner word", "\u03ba\u03b1\u0390\u03ba\u03b9", "read the notes \u03ba\u03b1\u03b9 the rest", false},
		{"a real plural", "archive", "read the archives end to end", true},
		{"a short stem is not a plural", "news", "write the report", false},
		{"nothing in common", "ledgers", "write a short poem", false},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			root := buildTerrain(t, map[string]string{
				testcase.directory + "/inside.csv": "a\n",
			})
			got := RenderTerrain(root, testcase.goal)
			opened := strings.Contains(got, "  inside.csv")
			if opened != testcase.opens {
				t.Errorf("opened = %v, want %v:\n%s", opened, testcase.opens, got)
			}
		})
	}
}

// TestTerrainRollupSaysWhatItActuallyCounted is the label bug. The line used to
// turn "the walk stopped early" into "over N files", an inference the walk never
// made: a tree of thousands of directories holding one file rendered as "over 1
// file", and a stop before any file was seen rendered as "over 0 empty". Every
// case here reports the numbers that were actually taken, and says separately
// that something was not reached.
func TestTerrainRollupSaysWhatItActuallyCounted(t *testing.T) {
	for _, testcase := range []struct {
		name   string
		rollup terrainRollup
		want   string
	}{
		{"nothing in it", terrainRollup{}, "empty"},
		{"one file", terrainRollup{files: 1}, "1 file"},
		{"several files", terrainRollup{files: 41}, "41 files"},
		{"directories but no files", terrainRollup{directories: 7}, "no files, 7 directories"},
		{"kinds are named", terrainRollup{files: 3, extensions: []string{".csv", ".md"}}, "3 files (.csv, .md)"},
		{
			name:   "one file and a great many directories",
			rollup: terrainRollup{files: 1, directories: 3999, unreadUnknown: true},
			want:   "1 file, more unread",
		},
		{
			name:   "stopped before any file was seen",
			rollup: terrainRollup{directories: 4000, unreadUnknown: true},
			want:   "no files, 4000 directories, more unread",
		},
		{
			name:   "the shortfall has a number",
			rollup: terrainRollup{files: 12, unreadEntries: 250000},
			want:   "12 files, 250000 entries unread",
		},
		{
			// The conflation: a number and a flag are different facts, and
			// printing only the number reads as though it were the whole of what
			// is missing when the flag says it is not.
			name:   "a number and a shortfall with no number",
			rollup: terrainRollup{files: 12, unreadEntries: 250000, unreadUnknown: true},
			want:   "12 files, 250000 entries unread, more unread",
		},
		{
			name:   "nothing counted, but a number is known",
			rollup: terrainRollup{unreadEntries: 250000},
			want:   "not read, 250000 entries unread",
		},
		{"stopped before anything at all", terrainRollup{unreadUnknown: true}, "not read"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			if got := testcase.rollup.label(); got != testcase.want {
				t.Errorf("label = %q, want %q", got, testcase.want)
			}
		})
	}
}

// TestTerrainReadsADirectoryWholeBeforeDroppingAnything is the determinism fix.
// A bounded prefix read kept whichever entries the filesystem returned first, so
// a large directory could render different bytes on two consecutive calls and a
// goal-named directory could be discarded before the cue match ever saw it. The
// listing is now read whole and sorted, and only then cut.
func TestTerrainReadsADirectoryWholeBeforeDroppingAnything(t *testing.T) {
	root := t.TempDir()
	const entries = 1000
	for index := 0; index < entries; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%05d.csv", index)), []byte("a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scan := &terrainScan{cues: map[string]bool{}, nameCap: terrainNameCap, budget: terrainBudget}
	listing := scan.read(root)
	if listing.capped {
		t.Fatal("a thousand entries is far under the cap and should have been held whole")
	}
	if listing.count != entries || len(listing.entries) != entries {
		t.Fatalf("read %d of %d entries", len(listing.entries), entries)
	}
	if !sort.SliceIsSorted(listing.entries, func(i, j int) bool {
		return listing.entries[i].Name() < listing.entries[j].Name()
	}) {
		t.Error("the listing was not sorted, so any cut through it would be filesystem order")
	}

	first := RenderTerrain(root, "read it")
	for pass := 0; pass < 4; pass++ {
		if again := RenderTerrain(root, "read it"); again != first {
			t.Fatalf("render %d of a large directory differs:\n%s\n---\n%s", pass, first, again)
		}
	}
}

// TestRenderTerrainKeepsACuedDirectoryAmongAThousand is the same fix seen from
// the cue's side: the directory the goal named sorts last of a thousand, and
// must still be the one that gets opened.
func TestRenderTerrainKeepsACuedDirectoryAmongAThousand(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 1000; index++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("%05d-filler", index)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	named := filepath.Join(root, "zzzz-responses")
	if err := os.MkdirAll(named, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(named, "north.csv"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := RenderTerrain(root, "summarise the responses")
	if !strings.Contains(got, "zzzz-responses/") {
		t.Fatalf("the directory the goal named lost its row among a thousand others:\n%s", got)
	}
	if !strings.Contains(got, "  north.csv") {
		t.Errorf("it was listed but never opened:\n%s", got)
	}
}

// TestTerrainCountsWhatItRefusesToList covers the one directory this will not
// describe. Past the cap a listing would be a sample of an arbitrary order, so
// there is no listing — only the count, which is the same number whatever order
// the entries arrived in, and therefore the only reproducible thing left to say.
func TestTerrainCountsWhatItRefusesToList(t *testing.T) {
	root := t.TempDir()
	const entries = 40
	for index := 0; index < entries; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.csv", index)), []byte("a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scan := &terrainScan{cues: map[string]bool{}, nameCap: 10, budget: terrainBudget}
	listing := scan.read(root)
	if !listing.capped || listing.entries != nil {
		t.Fatal("past the cap the listing must be dropped, not trimmed")
	}
	if listing.count != entries {
		t.Fatalf("count = %d, want the exact %d — the count is what stays reproducible", listing.count, entries)
	}

	got := renderTerrain(root, "read it", 10, terrainBudget)
	want := fmt.Sprintf("%d entries at the top level, too many to list", entries)
	if !strings.Contains(got, want) {
		t.Fatalf("render = %q, want it to carry %q", got, want)
	}
	if strings.Contains(got, ".csv") {
		t.Errorf("nothing from an unlistable directory may be named:\n%s", got)
	}
	if again := renderTerrain(root, "read it", 10, terrainBudget); again != got {
		t.Error("even the refusal has to be byte-stable")
	}
}

// TestTerrainStopsScanningWhenTheRenderRunsOutOfBudget is the fix for the only
// unbounded thing left in here. The per-directory cap bounds one directory, and
// a workspace with two thousand enormous children pays it two thousand times —
// each of those directories read to its last entry to be counted, none of them
// costing anything against a total. The meter spans the whole render, and what
// it does not reach is reported as unread rather than as absent.
func TestTerrainStopsScanningWhenTheRenderRunsOutOfBudget(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"alpha/one.csv": "a\n",
		"beta/two.csv":  "a\n",
		"top.txt":       "x\n",
	})
	// Enough to read the top level to its end and no further.
	const budget = 4
	got := renderTerrain(root, "read it", terrainNameCap, budget)

	for _, want := range []string{"alpha/", "beta/", "top.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("the top level should still be described; %q missing:\n%s", want, got)
		}
	}
	if strings.Count(got, "not read") != 2 {
		t.Errorf("both rollups ran past the meter and should say so:\n%s", got)
	}
	if strings.Contains(got, "one.csv") || strings.Contains(got, "empty") {
		t.Errorf("what the meter stopped short of is unread, never empty and never named:\n%s", got)
	}
	if again := renderTerrain(root, "read it", terrainNameCap, budget); again != got {
		t.Error("the meter is spent in a fixed order, so where it runs out must be stable")
	}
}

// TestTerrainRefusesToDescribeWhatItCouldNotRead is the error-path fix. A
// directory that cannot be opened, or that stops being readable partway, used to
// arrive at the caller looking exactly like a directory with nothing in it —
// and "empty" is a claim about the workspace that a failed read never earned.
func TestTerrainRefusesToDescribeWhatItCouldNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 000 directory regardless, so the case cannot be built here")
	}
	root := buildTerrain(t, map[string]string{
		"closed/secret.csv": "a\n",
		"open/plain.csv":    "a\n",
	})
	closed := filepath.Join(root, "closed")
	if err := os.Chmod(closed, 0o000); err != nil {
		t.Skip("this filesystem does not enforce directory permissions")
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	scan := &terrainScan{cues: map[string]bool{}, nameCap: terrainNameCap, budget: terrainBudget}
	if listing := scan.read(closed); !listing.failed || listing.count != 0 {
		t.Skip("this environment let the unreadable directory be read after all")
	}

	got := RenderTerrain(root, "read it")
	if !strings.Contains(got, "not read") {
		t.Errorf("an unreadable directory must say so:\n%s", got)
	}
	if strings.Contains(got, "empty") {
		t.Errorf("a directory nobody could read is not a directory with nothing in it:\n%s", got)
	}
	if !strings.Contains(got, "1 file (.csv)") {
		t.Errorf("the readable directory beside it should be unaffected:\n%s", got)
	}
}

// TestRenderTerrainKeepsTheOpeningLineValidAcrossTheReadBoundary is the other
// half of the UTF-8 promise. The head of a README is read by byte count, so a
// character can straddle the boundary; the fragment must never reach a prompt.
func TestRenderTerrainKeepsTheOpeningLineValidAcrossTheReadBoundary(t *testing.T) {
	// One 3-byte rune repeated so that the last one crosses terrainReadmeHead,
	// on a single line with no newline before the boundary.
	body := strings.Repeat("観", terrainReadmeHead/3+8)
	root := buildTerrain(t, map[string]string{"README.md": body})

	got := RenderTerrain(root, "read it")
	if !utf8.ValidString(got) {
		t.Fatal("a character split by the 4KB read reached the render")
	}
	if !strings.HasPrefix(got, "README.md: 観") {
		t.Errorf("the opening line was lost:\n%s", got)
	}
}

// TestRenderTerrainSkipsPlumbingAndBulk states the exclusion rule as an
// observable fact rather than a helper's behaviour: installed dependencies and
// hidden plumbing are not this run's material, at either level.
func TestRenderTerrainSkipsPlumbingAndBulk(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"corpus/filing.pdf":              "%PDF",
		"corpus/node_modules/dep/pkg.js": "module.exports={}",
		"corpus/.cache/warm":             "x",
		"node_modules/left/index.js":     "module.exports={}",
		"vendor/copied/lib.go":           "package copied",
		"__pycache__/stale.pyc":          "\x00",
		".git/HEAD":                      "ref: refs/heads/main\n",
		".env":                           "SECRET=1",
		"index.md":                       "the corpus\n",
	})
	got := RenderTerrain(root, "read the corpus")
	for _, unwanted := range []string{"node_modules", "vendor", "__pycache__", ".git", ".env", ".cache", "pkg.js"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q is not this run's material and should not be in the render:\n%s", unwanted, got)
		}
	}
	for _, want := range []string{"corpus/", "filing.pdf", "index.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("the render lost real material %q:\n%s", want, got)
		}
	}
}

// TestRenderTerrainNamesFewFilesAndRollsUpMany covers the two ways the top level
// can be said. A handful of files is best said by name, because the names are
// the information; a pile is best said by shape.
func TestRenderTerrainNamesFewFilesAndRollsUpMany(t *testing.T) {
	few := buildTerrain(t, map[string]string{
		"summary.md": strings.Repeat("x", 2048),
		"raw.csv":    "a,b\n",
	})
	got := RenderTerrain(few, "read it")
	for _, want := range []string{"summary.md", "2.0 KB", "raw.csv", "4 B"} {
		if !strings.Contains(got, want) {
			t.Errorf("a small top level should name its files and their sizes; %q missing:\n%s", want, got)
		}
	}

	layout := map[string]string{}
	for index := 0; index < terrainNamedFiles+5; index++ {
		layout[string(rune('a'+index))+".csv"] = "a,b\n"
	}
	layout["stray.md"] = "note\n"
	many := buildTerrain(t, layout)
	got = RenderTerrain(many, "read it")
	if !strings.Contains(got, "16 files at the top level (.csv, .md)") {
		t.Errorf("a large top level should be rolled up by shape:\n%s", got)
	}
	if strings.Contains(got, "a.csv") {
		t.Errorf("a rolled-up top level should not also name its files:\n%s", got)
	}
}

// TestRenderTerrainOpensWithTheReadme keeps the highest-value line in the block.
// Forty PDFs say "forty PDFs"; the sentence somebody wrote above them says what
// they are.
func TestRenderTerrainOpensWithTheReadme(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"README.md":       "\n\n#  The 2024 constituency filings, as received.\n\nmore\n",
		"filings/one.pdf": "%PDF",
	})
	got := RenderTerrain(root, "read the filings")
	if !strings.HasPrefix(got, "README.md: The 2024 constituency filings, as received.") {
		t.Errorf("the README's opening sentence should lead the render:\n%s", got)
	}
}

// TestTerrainRollupCountsWholeAndNamesItsKinds checks the two facts a rollup
// line carries: the count is of everything underneath, not of what happens to
// sit at the top, and the kinds are ordered by how many there are rather than by
// map iteration.
func TestTerrainRollupCountsWholeAndNamesItsKinds(t *testing.T) {
	root := buildTerrain(t, map[string]string{
		"corpus/one.csv":         "a\n",
		"corpus/two.csv":         "a\n",
		"corpus/three.csv":       "a\n",
		"corpus/deep/four.json":  "{}",
		"corpus/deep/notes.md":   "x\n",
		"corpus/deep/more/x.csv": "a\n",
	})
	got := RenderTerrain(root, "read it")
	if !strings.Contains(got, "6 files (.csv, .json, .md)") {
		t.Errorf("the rollup should count the whole tree and name its kinds commonest first:\n%s", got)
	}
}

// TestGraphContextWithoutTerrainIsByteIdentical is the compatibility test that
// matters most. The preamble is the frozen prefix behind every planning call, so
// a run that carries no terrain has to produce the exact bytes it produced
// before terrain existed — not merely equivalent prose.
func TestGraphContextWithoutTerrainIsByteIdentical(t *testing.T) {
	settled := []string{"The three cities are Berlin, Lisbon and Warsaw."}
	open := []string{"Which of the three suits the workload best."}
	const evidence = "Read published documentation and cite it; build nothing."

	for _, testcase := range []struct {
		name  string
		graph *Graph
		want  string
	}{
		{
			name:  "a bare goal",
			graph: &Graph{Goal: "write the summary"},
			want:  "Goal:\nwrite the summary",
		},
		{
			name:  "the full preamble",
			graph: &Graph{Goal: "write the summary", Settled: settled, Open: open, Evidence: evidence},
			want: "Goal:\nwrite the summary" +
				"\n\nSettled for this goal. Use these exactly as written. Never substitute\nyour own choice for one of these, and never leave one of them vague:\n" +
				"  - The three cities are Berlin, Lisbon and Warsaw.\n" +
				"\nDecided by the work itself, not known yet. Anything that needs one of these\nmust wait for whatever produces it — it cannot assume or invent an answer:\n" +
				"  - Which of the three suits the workload best.\n" +
				"\nThe evidence this goal warrants. It is the ceiling as well as the floor: no\npart of the work may buy stronger evidence than this, and none may settle for\nweaker:\n" +
				"  - " + evidence + "\n",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			if got := testcase.graph.context(); got != testcase.want {
				t.Errorf("an empty terrain changed the shared preamble\n got: %q\nwant: %q", got, testcase.want)
			}
		})
	}
}

// TestGraphContextCarriesTheTerrainBetweenGoalAndSettled pins where the block
// goes and what it looks like when it is there: after the ask, before the
// decisions taken over it, indented and otherwise verbatim.
func TestGraphContextCarriesTheTerrainBetweenGoalAndSettled(t *testing.T) {
	graph := &Graph{
		Goal:    "summarise the responses",
		Terrain: "responses/               41 files (.csv)\nREADME.md                2 B",
		Settled: []string{"The regions are north and south."},
	}
	got := graph.context()
	const want = "Goal:\nsummarise the responses" +
		"\n\nThe workspace this run stands on (rendered from the material itself; it may\nbe incomplete, and it is what was there when planning began):\n" +
		"  responses/               41 files (.csv)\n" +
		"  README.md                2 B" +
		"\n\nSettled for this goal. Use these exactly as written. Never substitute\nyour own choice for one of these, and never leave one of them vague:\n" +
		"  - The regions are north and south.\n"
	if got != want {
		t.Errorf("terrain block\n got: %q\nwant: %q", got, want)
	}
}

// TestGraphRoundTripsTheTerrain keeps the picture with the plan. A graph is
// written to disk and revised later against the premises it was built from, and
// a terrain that did not survive the file would leave the reviser judging what
// happened against a workspace it can no longer see.
func TestGraphRoundTripsTheTerrain(t *testing.T) {
	const terrain = "filings/                 41 files (.pdf)"
	graph := &Graph{Goal: "read the filings", Terrain: terrain, NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Read", Summary: "Read the filings"})

	encoded, err := graph.JSON()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Terrain != terrain {
		t.Fatalf("terrain did not survive the round trip: %q", loaded.Terrain)
	}
	if !strings.Contains(loaded.context(), terrain) {
		t.Error("the reloaded graph's preamble no longer carries the workspace it was planned against")
	}
}

// planningReply answers any of the three pre-graph passes, chosen by the system
// prompt that identifies them.
func planningReply(system, _ string) string {
	switch {
	case strings.Contains(system, "You settle what a goal leaves unsaid"):
		return `{"settled":[],"open":[],"evidence":"Read what is there and cite it."}`
	case strings.Contains(system, "You break a goal into its ordered stages"):
		return `{"stages":[{"title":"Read","summary":"Read the responses"}]}`
	default:
		return `{"mode":"decompose","reason":"several bodies of material"}`
	}
}

// TestThePassesBeforeTheGraphSeeTheTerrain is the fix for the hole that made the
// whole feature nearly ornamental. Grounding, the spine and the ensemble
// judgment all run before there is a graph to read a preamble from, and each
// assembled its own goal-only message — so the pass that settles a goal's free
// variables by fiat was still doing it blind, which is the exact failure terrain
// exists to prevent.
//
// The empty half is the compatibility promise, and it is checked as bytes: the
// message has to be the string these passes have always sent, not a rearranged
// equivalent of it.
func TestThePassesBeforeTheGraphSeeTheTerrain(t *testing.T) {
	const goal = "summarise the responses"
	const terrain = "responses/               41 files (.csv)"

	for _, testcase := range []struct {
		name string
		call func(t *testing.T, client Completer, terrain string)
	}{
		{
			name: "grounding",
			call: func(t *testing.T, client Completer, terrain string) {
				if _, _, err := GroundWith(t.Context(), client, goal, terrain, nil, nil); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "the spine",
			call: func(t *testing.T, client Completer, terrain string) {
				if _, _, err := Spine(t.Context(), client, goal, terrain, nil, 1); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "the ensemble judgment",
			call: func(t *testing.T, client Completer, terrain string) {
				if _, _, err := DecidePanel(t.Context(), client, goal, terrain, nil, nil, ""); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			bare := &stubClient{reply: planningReply}
			testcase.call(t, bare, "")
			if got, want := bare.prompts[0], "Goal:\n"+goal; got != want {
				t.Fatalf("an empty terrain changed the prompt\n got: %q\nwant: %q", got, want)
			}

			sighted := &stubClient{reply: planningReply}
			testcase.call(t, sighted, terrain)
			prompt := sighted.prompts[0]
			for _, want := range []string{"The workspace this run stands on", "  " + terrain} {
				if !strings.Contains(prompt, want) {
					t.Errorf("the prompt does not carry %q:\n%s", want, prompt)
				}
			}
			// One wording, shared with the preamble every later pass reads.
			if !strings.HasPrefix(prompt, (&Graph{Goal: goal, Terrain: terrain}).context()) {
				t.Errorf("this pass words the workspace differently from the shared preamble:\n%s", prompt)
			}
		})
	}
}

// TestBuildHandsTheTerrainToTheOpeners is the same guarantee through Build,
// where the two openers run concurrently and take the snapshot from the options
// rather than from the half-built graph.
func TestBuildHandsTheTerrainToTheOpeners(t *testing.T) {
	const terrain = "responses/               41 files (.csv)"
	client := &stubClient{reply: planningReply}

	if _, err := Build(t.Context(), client, "summarise the responses", Options{
		Terrain:   terrain,
		Undivided: true,
		Ensemble:  EnsembleNever,
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	seen := 0
	for _, prompt := range client.prompts {
		if strings.Contains(prompt, terrain) {
			seen++
		}
	}
	// Grounding and the spine, both of them, before anything is decided.
	if seen < 2 {
		t.Fatalf("only %d of the opening calls saw the workspace: %#v", seen, client.prompts)
	}
}

// TestBuildFreezesTheTerrainForEveryPass checks the wiring end to end: the
// option lands on the graph before the openers launch, so the grounding call and
// every pass after it work from one picture rather than several.
func TestBuildFreezesTheTerrainForEveryPass(t *testing.T) {
	const terrain = "responses/               41 files (.csv)"
	client := &stubClient{reply: func(system, _ string) string {
		switch {
		case strings.Contains(system, "You settle what a goal leaves unsaid"):
			return `{"settled":[],"open":[],"evidence":"Read what is there and cite it."}`
		default:
			return `{"stages":[{"title":"Summarise","summary":"Summarise the responses"}]}`
		}
	}}

	graph, err := Build(t.Context(), client, "summarise the responses", Options{
		Terrain:   terrain,
		Undivided: true,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if graph.Terrain != terrain {
		t.Fatalf("graph terrain = %q, want the option verbatim", graph.Terrain)
	}
	if !strings.Contains(graph.context(), terrain) {
		t.Error("the built graph's shared preamble does not carry the terrain")
	}
}

package plan

import (
	"os"
	"path/filepath"
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
	if len(got) > terrainBytes+len("…") {
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
	if len(opening) > len("README.md: ")+terrainReadmeBytes+len("…") {
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

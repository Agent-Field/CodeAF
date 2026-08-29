package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// The document is the source of truth, and this is what makes that sentence
// mean something.
//
// The two long strings are prompts: the only thing that decides whether a
// coding job reaches the coding worker, and the only ruler that decides how
// much of one fits in a leaf. They were written in docs/SUBHARNESSES.md, beside
// the reasoning that produced them, and a prompt edited in the source without
// the paragraph that explains it is how a system ends up with a rule nobody can
// account for. So the code carries them verbatim and this reads the document
// back.
func TestSWERegistrationCarriesTheDocumentsPromptsVerbatim(t *testing.T) {
	raw, err := os.ReadFile("../../docs/SUBHARNESSES.md")
	if err != nil {
		t.Fatal(err)
	}
	quotes := blockQuotesAfter(string(raw), "## Canonical choice prompts for `swe`")
	if len(quotes) < 2 {
		t.Fatalf("the canonical prompts section no longer holds two block quotes (found %d)", len(quotes))
	}
	if quotes[0] != sweMenuEntry {
		t.Fatalf("the menu entry has drifted from the document.\ndocument:\n%s\n\ncode:\n%s",
			quotes[0], sweMenuEntry)
	}
	if quotes[1] != swePriorAnchors {
		t.Fatalf("the capacity ruler has drifted from the document.\ndocument:\n%s\n\ncode:\n%s",
			quotes[1], swePriorAnchors)
	}
}

// blockQuotesAfter returns the markdown block quotes following a heading, with
// their quote markers removed and nothing else touched. A ">" on its own is a
// blank line inside one quote rather than the end of it — which is exactly what
// separates the ruler's three anchors from each other.
func blockQuotesAfter(document, heading string) []string {
	_, rest, found := strings.Cut(document, heading)
	if !found {
		return nil
	}
	var quotes []string
	var current []string
	for _, line := range strings.Split(rest, "\n") {
		switch {
		case strings.HasPrefix(line, "> "):
			current = append(current, strings.TrimPrefix(line, "> "))
		case line == ">":
			current = append(current, "")
		default:
			if len(current) > 0 {
				quotes = append(quotes, strings.Join(current, "\n"))
				current = nil
			}
		}
	}
	if len(current) > 0 {
		quotes = append(quotes, strings.Join(current, "\n"))
	}
	return quotes
}

// The menu is what a model actually reads. MenuText supplies the worker's name
// itself, so a Purpose that repeated it would render "- swe — swe — an
// end-to-end…" into every compile.
func TestSWEMenuEntryIsNotSaidTwice(t *testing.T) {
	defer exec.ForgetSubharnesses()
	// A profile nobody has written, so the worker roster is its default —
	// every worker this build has. Without this the test would be reading the
	// roster of whoever's machine it happens to run on.
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	installSubharnesses()
	menu := exec.MenuText()
	if strings.Contains(menu, "swe — swe —") {
		t.Fatalf("the worker's name is rendered twice:\n%s", menu)
	}
	for _, want := range []string{
		"- swe — an end-to-end software-engineering pipeline",
		"when the work must be discovered rather than merely made",
		"When in doubt, or for mixed or non-matching work, leave it unset",
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("the menu is missing %q:\n%s", want, menu)
		}
	}
}

// Registration is the only door: it has to reach the compiler's menu, the
// sizing pass's rulers, and the budget shape a leaf is granted, all three, or a
// worker is half-installed in a way nothing would notice until a run.
func TestInstallingSWEReachesEveryReaderOfARegistration(t *testing.T) {
	defer exec.ForgetSubharnesses()
	// A profile nobody has written, so the worker roster is its default —
	// every worker this build has. Without this the test would be reading the
	// roster of whoever's machine it happens to run on.
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	installSubharnesses()

	if !exec.KnownSubharness("swe") {
		t.Fatal("the worker is not registered")
	}
	anchors := plan.AnchorsFor("swe")
	if !strings.Contains(anchors, "TOO SMALL") || !strings.Contains(anchors, "TOO BIG") {
		t.Fatalf("the sizing pass got no ruler for swe: %q", anchors)
	}
	if anchors == plan.AnchorsFor(exec.LinearSubharness) {
		t.Fatal("the worker is being sized against the generalist's ruler")
	}

	shape := exec.SubharnessFor("swe")
	if floor := shape.Deadline(0); floor != 60*time.Minute {
		t.Fatalf("deadline floor = %s, want 60m", floor)
	}
	// The ordinary leaf grant has to buy the run the anchors promise: one
	// coding issue taken whole, "even if that run takes an hour". The floor
	// is measured, not guessed — a 30-minute floor watched two real issues
	// die at the watchdog with the suite already growing.
	typical := shape.Deadline(150_000)
	if typical < 60*time.Minute || typical > 90*time.Minute {
		t.Fatalf("a typical leaf gets %s — the anchors promise the hour, with room", typical)
	}
	if typical <= exec.SubharnessFor(exec.LinearSubharness).Deadline(150_000) {
		t.Fatal("the coding pipeline is on the generalist's clock")
	}
}

// The two-surface covenant, in the one function that could break it. A worker
// this build can construct must be reachable from the resident's per-leaf
// construction and from the headless registry both, on its own budget shape.
func TestSWEIsReachableFromBothDispatchPaths(t *testing.T) {
	defer exec.ForgetSubharnesses()
	// A profile nobody has written, so the worker roster is its default —
	// every worker this build has. Without this the test would be reading the
	// roster of whoever's machine it happens to run on.
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	installSubharnesses()

	directory := t.TempDir()
	workspace, err := exec.NewWorkspace(directory)
	if err != nil {
		t.Fatal(err)
	}
	build := leafBuild{
		settings:  config.Config{ProfileDir: directory, APIKey: "sk", BaseURL: "https://example/api/v1"},
		workspace: workspace, model: "vendor/model",
		maxTurns: 1, maxTokens: 150_000, deadline: 15 * time.Minute,
	}

	if worker := executorFor("swe", build); worker.Subharness() != "swe" {
		t.Fatalf("the resident path built %q", worker.Subharness())
	}
	registry := exec.NewRegistry(executorFor("", build))
	registerLeafExecutors(registry, build)
	if worker := registry.For("swe"); worker.Subharness() != "swe" {
		t.Fatalf("the headless path built %q", worker.Subharness())
	}
	// And the degradation the whole seam rests on is untouched: a name this
	// build does not have is served by the generalist rather than refused.
	if worker := registry.For("reviewer"); worker.Subharness() != exec.LinearSubharness {
		t.Fatalf("an unknown worker resolved to %q", worker.Subharness())
	}
}

// A ceiling nobody can read is a ceiling nobody can raise, and a build that
// died because somebody typed a word into it would be worse than one that
// quietly used the default.
func TestSWEMaxCostReadsTheOperatorsCeilingOrKeepsTheDefault(t *testing.T) {
	for _, testCase := range []struct {
		set  string
		want float64
	}{
		{"", exec.DefaultSWEMaxCost},
		{"  ", exec.DefaultSWEMaxCost},
		{"ten", exec.DefaultSWEMaxCost},
		{"0", exec.DefaultSWEMaxCost},
		{"-4", exec.DefaultSWEMaxCost},
		{" 2.5 ", 2.5},
		{"40", 40},
	} {
		environ := func(name string) string {
			if name != sweMaxCostEnv {
				t.Fatalf("read %q instead of the ceiling", name)
			}
			return testCase.set
		}
		if got := sweMaxCost(environ); got != testCase.want {
			t.Fatalf("sweMaxCost(%q) = %v, want %v", testCase.set, got, testCase.want)
		}
	}
	if !slicesContain(config.OperatorEnvPins, sweMaxCostEnv) {
		t.Fatalf("%s is not pinned as operator plumbing", sweMaxCostEnv)
	}
}

func slicesContain(list []string, want string) bool {
	for _, value := range list {
		if value == want {
			return true
		}
	}
	return false
}

// A capability the product has and does not mention is one a person meets by
// accident. Both headless surfaces name it.
func TestTheSubharnessFlagIsDocumented(t *testing.T) {
	if !strings.Contains(usageText, "--subharness") || !strings.Contains(usageText, "swe") {
		t.Fatal("aforge --help never mentions the flag or the worker")
	}
	if !strings.Contains(usageText, sweMaxCostEnv) {
		t.Fatalf("aforge --help never mentions %s", sweMaxCostEnv)
	}
	raw, err := os.ReadFile("../../docs/HEADLESS.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--subharness", "`swe`", sweMaxCostEnv} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("docs/HEADLESS.md never mentions %q", want)
		}
	}
}

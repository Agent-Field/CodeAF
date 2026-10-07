package manual

import (
	"strings"
	"testing"
)

// Each answer is retrieved alone, so a qualification on another page cannot
// correct a promise in the section the model actually reads.
func chatSection(t *testing.T, page, title string) string {
	t.Helper()
	for _, section := range Chat().Sections() {
		if section.Page == page && section.Title == title {
			return flatten(section.Body)
		}
	}
	t.Fatalf("missing manual section %s: %s", page, title)
	return ""
}

func TestHomeManualQualifiesTheFreshProjectConversationTab(t *testing.T) {
	answer := chatSection(t, "home", "Open another project from home")
	for _, fact := range []string{"untouched conversation has no tab", "draft or first sent message"} {
		if !strings.Contains(answer, fact) {
			t.Errorf("opening another project does not explain %q:\n%s", fact, answer)
		}
	}
}

func TestModelManualListsOnlyUsableProviderConnections(t *testing.T) {
	answer := chatSection(t, "commands", "/model — pick a model")
	for _, fact := range []string{"only OpenRouter connected", "absent without its key", "only Ollama connected", "installed models", "connected providers", "selects it and closes the list immediately"} {
		if !strings.Contains(answer, fact) {
			t.Errorf("model picker does not explain %q:\n%s", fact, answer)
		}
	}
	for _, stale := range []string{"even without its key", "Enter chooses and the list stays up"} {
		if strings.Contains(answer, stale) {
			t.Errorf("model picker still promises retired behavior: %q", stale)
		}
	}
}

func TestRateManualExplainsAbsentAndRoundedThroughput(t *testing.T) {
	answer := chatSection(t, "models-and-cost", "Why the via name keeps changing on the model list")
	for _, fact := range []string{"unknown", "below 1 token per second", "`0 tok/s`", "work pulse", "model rows", "rounded `t/s`", "rounds to zero"} {
		if !strings.Contains(answer, fact) {
			t.Errorf("throughput answer does not explain %q:\n%s", fact, answer)
		}
	}
}

// A qualification written on one page does not correct a copy of the old claim
// on another: each section is retrieved alone. These are the stale sentences the
// 2026-09-30 pass found repeated after their first copy had been fixed.
func TestNoSectionRepeatsAStaleDisplayClaim(t *testing.T) {
	stale := []string{
		"| `auditor` | checker |",
		"draws one tab per open conversation, and",
		"names every open conversation, and",
	}
	for _, section := range Chat().Sections() {
		body := flatten(section.Body)
		for _, claim := range stale {
			if strings.Contains(body, claim) {
				t.Errorf("%s: %s still says %q", section.Page, section.Title, claim)
			}
		}
	}
}

// A correct answer elsewhere cannot repair a stale claim in a section that
// is retrieved on its own. Check headings as well as bodies across the corpus.
func TestNoSectionRepeatsStaleHelpSettingsGreetingOrManagerClaims(t *testing.T) {
	stale := []string{
		"run `codeaf --help` for every command and the environment table.",
		"`codeaf --help` prints every command, what each is for, and the environment table under them.",
		"The nine settings tabs",
		"The nine tabs need",
		"walk the nine one at a time",
		"Tasks · Providers · Connections",
		"a plain launch opens straight on home",
		"deterministic `@manager-...` fallback",
	}
	for _, section := range Chat().Sections() {
		text := flatten(section.Title + " " + section.Body)
		for _, claim := range stale {
			if strings.Contains(text, claim) {
				t.Errorf("%s: %s still says %q", section.Page, section.Title, claim)
			}
		}
	}
}

func TestManualStatesHelpSettingsGreetingAndManagerDisplayTruth(t *testing.T) {
	for _, test := range []struct {
		page, title string
		facts       []string
	}{
		{"commands", "codeaf <command> --help — asking one command what it takes, which is not a failure", []string{"run `codeaf --help` for every command, `codeaf help env` for the variables."}},
		{"commands", "codeaf --help, and --help on any command — what does this command take, what are its flags, how do I see the usage", []string{"`codeaf help env`", "every variable and its default"}},
		{"commands", "The ten settings tabs", []string{"Session · Context · Workspace · Display · Spending · Safety · Tasks · Teams · Providers · Connections"}},
		{"models-and-cost", "Where are the spending limits — the Spending tab, and every door onto it", []string{"Tasks · Teams · Providers"}},
		{"getting-started", "Getting started — first time setup, what happens the first time I run codeaf", []string{"nothing elsewhere", "What would you like to work on?", "home", "`--no-host`"}},
		{"team-manager", "Making a manager", []string{"team's name", "`@second`", "`@lead`", "All teams"}},
	} {
		t.Run(test.page+"/"+test.title, func(t *testing.T) {
			answer := chatSection(t, test.page, test.title)
			for _, fact := range test.facts {
				if !strings.Contains(answer, fact) {
					t.Errorf("the section does not state %q:\n%s", fact, answer)
				}
			}
		})
	}
}

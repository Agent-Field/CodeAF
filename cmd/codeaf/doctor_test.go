package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/store"
)

// H8: help env lists the current names and says exactly once that legacy
// spellings remain a one-release fallback; doctor names the resolved root and
// never recommends the former HOME variable.
func TestH8EnvironmentHelpAndDoctorUseCurrentNames(t *testing.T) {
	if !strings.Contains(environmentText, "CODEAF_HOME") {
		t.Fatal("help env does not list CODEAF_HOME")
	}
	if got := strings.Count(environmentText, legacyEnvironmentHelp); got != 1 {
		t.Fatalf("legacy fallback sentence appears %d times, want once", got)
	}
	if got := strings.Count(environmentText, "AFORGE_*"); got != 1 { // legacy-name
		t.Fatalf("legacy wildcard appears %d times, want once", got)
	}
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	var output bytes.Buffer
	if err := runDoctorWith(nil, &output, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), root) {
		t.Fatalf("doctor does not name resolved root %q:\n%s", root, output.String())
	}
	if strings.Contains(output.String(), env.Legacy(home.EnvVar)) {
		t.Fatalf("doctor recommends former HOME spelling:\n%s", output.String())
	}
}

func TestDoctorShowsSharedCalmStatusRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "doctor", Text: "Which release?", Urgency: store.QuestionWhenever,
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 3.4}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	// The lock is keyed to the store it guards, so the report reads the one this
	// brain would actually be held by rather than a directory-wide file.
	if err := os.WriteFile(residentLockFor(path), []byte(`{"surface":"desktop","pid":4321}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 20); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		// `store` was `brain`: nobody looking for where their data lives
		// searches for a brain.
		"store", path, "desktop · pid 4321", "$3.40 today · rail $20.00",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
	// The two rows that read the v1 scheduler are gone with it: the timer it
	// installed (`background timer`, once `standing watch`) and the count of
	// its charters (`standing`). A row about machinery that is not there could
	// only ever have said so.
	for _, gone := range []string{"background timer", "last wake", "next check", "standing", "charter"} {
		if strings.Contains(text, gone) {
			t.Fatalf("doctor still reports the removed scheduler (%q):\n%s", gone, text)
		}
	}
	for _, forbidden := range []string{"daemon", "launchd", "systemd", "service", "brain", "standing watch"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("doctor output contains %q:\n%s", forbidden, text)
		}
	}
}

func TestDoctorDoesNotCreateMissingBrainAndDegradesResidentCalmly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "graph.db")
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("doctor created missing brain: %v", err)
	}
	text := output.String()
	for _, want := range []string{
		path + " · not created", "this terminal while open",
		// A machine that has spent nothing and holds nothing standing says so
		// by leaving those figures out: the rail is the only claim here that
		// anybody made (the emptiness law, emptiness_test.go).
		"rail unlimited",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
}

// ── C14: DOCTOR SAID NOTHING ABOUT THE ONE THING THAT STOPS EVERYTHING ───────
//
// `codeaf doctor` is what somebody runs when nothing works. On a machine with
// no provider key it reported six healthy-looking rows and left with 0, and the
// single most common reason nothing works was the one thing it did not check.
func TestDoctorSaysThereIsNoKeyAndWhatToTypeAboutIt(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv(fallbackKeyEnv, "")
	t.Setenv(config.ProfileDirEnv, t.TempDir())

	path := filepath.Join(t.TempDir(), "graph.db")
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	line := rowSaying(text, "key")
	if line == "" {
		t.Fatalf("doctor has no `key` row at all, so a machine that cannot call a model reads as healthy:\n%s", text)
	}
	if !strings.Contains(line, "none") {
		t.Fatalf("the key row does not say there is no key.\n  row:  %q\n  want: it to say `none`", line)
	}
	// AND WHAT TO DO ABOUT IT, in the door's own words.
	for _, want := range []string{"export " + config.APIKeyEnv} {
		if !strings.Contains(line, want) {
			t.Fatalf("the key row says the cause and never what to type.\n  row:  %q\n  want it to name %q",
				line, want)
		}
	}
}

// AND WHERE IT CAME FROM WHEN THERE IS ONE. Three rungs answer, and knowing
// WHICH is the difference between "my shell has it" and "this machine has it" —
// the question behind every report of a timer-driven run that could not
// authenticate while the terminal beside it could.
// C9: Doctor names the default-endpoint rung and never a key.
func TestDoctorNamesWhereTheKeyCameFrom(t *testing.T) {
	profile := t.TempDir()
	if err := config.WriteAPIKey(profile, "sk-or-v1-persisted-000000000000"); err != nil {
		t.Fatal(err)
	}
	secret := "sk-or-v1-this-must-never-be-printed"

	for _, probe := range []struct {
		name       string
		openRouter string
		openAI     string
		want       string
	}{
		{name: "the OpenRouter variable", openRouter: secret, want: config.APIKeyEnv},
		{name: "profile over the OpenAI variable", openAI: secret, want: config.BudgetConfigPath(profile)},
		{name: "the profile file", want: config.BudgetConfigPath(profile)},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Setenv(config.APIKeyEnv, probe.openRouter)
			t.Setenv(fallbackKeyEnv, probe.openAI)
			t.Setenv(config.ProfileDirEnv, profile)

			var output bytes.Buffer
			if err := runDoctorWith([]string{"--db", filepath.Join(t.TempDir(), "graph.db")},
				&output, 0); err != nil {
				t.Fatal(err)
			}
			line := rowSaying(output.String(), "key")
			if !strings.Contains(line, "set") || !strings.Contains(line, probe.want) {
				t.Fatalf("the key row does not name the rung that answered.\n  row:  %q\n  want: `set` and %q",
					line, probe.want)
			}
			// A KEY IS A SECRET. The row names where it came from and never
			// what it is, so the page can be pasted into a defect report.
			if strings.Contains(output.String(), secret) || strings.Contains(output.String(), "persisted") {
				t.Fatalf("doctor printed the key itself:\n%s", output.String())
			}
		})
	}
}

// The row and the door must not be able to disagree. [readKeyReport] climbs the
// rungs one at a time so it can say WHICH answered; [config.APIKeyAt] folds them
// into one string. This is the seam that keeps them one reading, so a ladder
// that grows a step fails here rather than leaving doctor telling a working
// machine it has no key.
// C7, C8 and C9: Doctor agrees with the door at every new rung and endpoint.
func TestDoctorAgreesWithTheDoorAboutWhetherThereIsAKey(t *testing.T) {
	withKey := t.TempDir()
	if err := config.WriteAPIKey(withKey, "sk-or-v1-persisted-000000000000"); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		name       string
		openRouter string
		openAI     string
		profile    string
		base       string
	}{
		{name: "nothing anywhere", profile: t.TempDir()},
		{name: "the OpenRouter variable", openRouter: "sk-or-v1-aaaaaaaaaaaaaaaaaaaa", profile: t.TempDir()},
		{name: "an unusable OpenAI variable", openAI: "sk-proj-test-aaaaaaaa", profile: t.TempDir()},
		{name: "a usable OpenAI variable", openAI: "sk-or-v1-test-aaaaaaaa", profile: t.TempDir()},
		{name: "profile over usable fallback", openAI: "sk-or-v1-test-aaaaaaaa", profile: withKey},
		{name: "OpenAI on custom endpoint", openAI: "sk-proj-test-aaaaaaaa", profile: withKey, base: "https://custom.invalid/v1"},
		{name: "profile on custom endpoint", profile: withKey, base: "https://custom.invalid/v1"},
		{name: "OpenRouter on custom endpoint", openRouter: "sk-or-v1-test-router", openAI: "sk-proj-test-aaaaaaaa", profile: withKey, base: "https://custom.invalid/v1"},
		{name: "only the profile file", profile: withKey},
		{name: "a variable over a profile file", openRouter: "sk-or-v1-bbbbbbbbbbbbbbbbbbbb", profile: withKey},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Setenv(config.APIKeyEnv, probe.openRouter)
			t.Setenv(fallbackKeyEnv, probe.openAI)
			t.Setenv("CODEAF_BASE_URL", probe.base)
			doorFound := config.APIKeyAt(probe.profile) != ""
			doctorFound := readKeyReport(probe.profile).Where != ""
			if doorFound != doctorFound {
				t.Fatalf("doctor and the door disagree about whether this machine has a key.\n"+
					"  config.APIKeyAt found a key: %v\n  doctor's row found one:      %v",
					doorFound, doctorFound)
			}
		})
	}
}

// C12: Doctor and the missing-key door offer the variable that works with the
// default provider. A compatibility variable must not be offered as a remedy.
func TestDoctorNamesTheSameKeyVariablesTheDoorDoes(t *testing.T) {
	refusal := config.ErrNoAPIKey.Error()
	remedy := formatKey(keyReport{})
	for _, sentence := range []string{refusal, remedy} {
		if !strings.Contains(sentence, config.APIKeyEnv) || strings.Contains(sentence, fallbackKeyEnv) {
			t.Fatalf("missing-key advice must name the OpenRouter variable: %q", sentence)
		}
	}
}

// rowSaying is one labelled line out of doctor's block, found by its label.
func rowSaying(text, label string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, label+" ") {
			return line
		}
	}
	return ""
}

// TestTheEnvironmentPageDoesNotCallTheKeyVariableRequired is line 7 of the
// first table on the front door, and it was wrong in the one direction that
// costs somebody an afternoon.
//
// A developer's first run started with no OPENROUTER_API_KEY at all, because
// the key they had pasted into the chat lives in the profile's own config.json.
// `codeaf help env` called the variable **required**, so the page said the run
// they had just watched succeed was impossible, and sent them hunting for a key
// they already had.
//
// The truth is the ladder [config.APIKeyAt] climbs and `codeaf doctor` reports:
// the OpenRouter variable, the profile, then a usable OpenAI variable. This asserts the
// page against that ladder rather than against a sentence, so a rung added
// tomorrow is a red test here and not a front door that has quietly gone stale.
func TestTheEnvironmentPageDoesNotCallTheKeyVariableRequired(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv(fallbackKeyEnv, "")
	if err := config.WriteAPIKey(profile, "sk-or-v1-a-key-kept-in-the-profile"); err != nil {
		t.Fatal(err)
	}
	// The state the page has to describe: no variable anywhere, and a machine
	// that talks to a model perfectly well.
	if config.APIKeyAt(profile) == "" {
		t.Fatalf("the ladder found no key on a profile that holds one, so this test is not about what it says it is")
	}

	row := environmentRow(t, config.APIKeyEnv)
	if strings.Contains(strings.ToLower(row), "required") {
		t.Errorf("`codeaf help env` calls %s required, and a key in the profile answers without it — "+
			"a required thing that is not required sends somebody to find a key they already have:\n%s",
			config.APIKeyEnv, row)
	}
	// And it says where else a key comes from, because "not required" on its
	// own leaves a person with no idea what IS.
	for _, rung := range []string{fallbackKeyEnv, "profile"} {
		if !strings.Contains(row, rung) {
			t.Errorf("the %s row never mentions %q, so the page names one rung of a three-rung ladder:\n%s",
				config.APIKeyEnv, rung, row)
		}
	}
}

// environmentRow lifts one variable's whole entry out of [environmentText] —
// the name line and every continuation line indented under its text column.
func environmentRow(t *testing.T, variable string) string {
	t.Helper()
	var row []string
	for _, line := range strings.Split(environmentText, "\n") {
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), variable+" "):
			row = append(row, line)
		case len(row) > 0 && strings.HasPrefix(line, "      ") && strings.TrimSpace(line) != "":
			row = append(row, line)
		case len(row) > 0:
			return strings.Join(row, "\n")
		}
	}
	if len(row) == 0 {
		t.Fatalf("`codeaf help env` has no row for %s at all", variable)
	}
	return strings.Join(row, "\n")
}

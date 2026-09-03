package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/watchdog"
)

type fakeDoctorWatch struct {
	status watchdog.Status
	err    error
}

func (watch fakeDoctorWatch) Status() (watchdog.Status, error) { return watch.status, watch.err }

func TestDoctorShowsSharedCalmStatusRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := store.NewCharter("doctor-charter", "Keep releases documented.", store.WatchSpec{
		Kind: store.WatchPoll, Poll: &store.PollWatch{Condition: "look for releases", Cadence: time.Hour},
	}, "Did a release land?", store.CharterAction{Template: "Update release notes"},
		store.CharterRails{PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3}, store.CharterActive,
		store.Ratification{Origin: store.OriginUser, SessionID: "doctor", Evidence: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
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

	now := time.Now()
	watch := fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-2 * time.Minute), NextDue: now.Add(3 * time.Minute),
	}}
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 20, watch); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		// `store` and `background timer` were `brain` and `standing watch`.
		// Nobody looking for where their data lives searches for a brain, and
		// `standing watch` is the RESIDENT's vocabulary — a word the chat's own
		// manual is forbidden to use, so the manual could not quote this row
		// and stay legal. What the row measures, in a developer's words, is
		// what is running, since when, and whether it still answers.
		"store", path, "desktop · pid 4321", "background timer", "installed",
		"last wake 2m ago", "next check in 3m", "$3.40 today · rail $20.00",
		"1 active charter · 1 pending question",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"daemon", "launchd", "systemd", "service", "brain", "standing watch"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("doctor output contains %q:\n%s", forbidden, text)
		}
	}
}

func TestDoctorSaysWhenAnArrangedWatchStoppedWaking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-4 * time.Hour), NextDue: now.Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "checks look stalled") {
		t.Fatalf("stalled watch read as healthy:\n%s", output.String())
	}

	output.Reset()
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{status: watchdog.Status{
		Installed: true, LastWake: now.Add(-2 * time.Minute), NextDue: now.Add(3 * time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "look stalled") {
		t.Fatalf("healthy watch reported as stalled:\n%s", output.String())
	}
}

func TestDoctorDoesNotCreateMissingBrainAndDegradesResidentCalmly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "graph.db")
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("doctor created missing brain: %v", err)
	}
	text := output.String()
	for _, want := range []string{
		path + " · not created", "this terminal while open", "not installed",
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
// `aforge doctor` is what somebody runs when nothing works. On a machine with
// no provider key it reported six healthy-looking rows and left with 0, and the
// single most common reason nothing works was the one thing it did not check.
func TestDoctorSaysThereIsNoKeyAndWhatToTypeAboutIt(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv(fallbackKeyEnv, "")
	t.Setenv(config.ProfileDirEnv, t.TempDir())

	path := filepath.Join(t.TempDir(), "graph.db")
	var output bytes.Buffer
	if err := runDoctorWith([]string{"--db", path}, &output, 0, fakeDoctorWatch{}); err != nil {
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
	for _, want := range []string{"export " + config.APIKeyEnv, fallbackKeyEnv} {
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
		{name: "the OpenAI variable", openAI: secret, want: fallbackKeyEnv},
		{name: "the profile file", want: config.BudgetConfigPath(profile)},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Setenv(config.APIKeyEnv, probe.openRouter)
			t.Setenv(fallbackKeyEnv, probe.openAI)
			t.Setenv(config.ProfileDirEnv, profile)

			var output bytes.Buffer
			if err := runDoctorWith([]string{"--db", filepath.Join(t.TempDir(), "graph.db")},
				&output, 0, fakeDoctorWatch{}); err != nil {
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
	}{
		{name: "nothing anywhere", profile: t.TempDir()},
		{name: "the OpenRouter variable", openRouter: "sk-or-v1-aaaaaaaaaaaaaaaaaaaa", profile: t.TempDir()},
		{name: "the OpenAI variable", openAI: "sk-aaaaaaaaaaaaaaaaaaaaaaaa", profile: t.TempDir()},
		{name: "only the profile file", profile: withKey},
		{name: "a variable over a profile file", openRouter: "sk-or-v1-bbbbbbbbbbbbbbbbbbbb", profile: withKey},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Setenv(config.APIKeyEnv, probe.openRouter)
			t.Setenv(fallbackKeyEnv, probe.openAI)
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

// And doctor names the same two variables the refusal at the door names. There
// is no exported constant for the OpenAI-shaped one, so this is the pin that
// keeps the two spellings one fact.
func TestDoctorNamesTheSameKeyVariablesTheDoorDoes(t *testing.T) {
	refusal := config.ErrNoAPIKey.Error()
	for _, variable := range []string{config.APIKeyEnv, fallbackKeyEnv} {
		if !strings.Contains(refusal, variable) {
			t.Fatalf("doctor points at %q and the door's refusal does not name it: %q", variable, refusal)
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

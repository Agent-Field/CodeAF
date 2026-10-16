package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// KEYS.CHECK IS ONE REQUEST. A rejected key is answered with the provider's
// own status and words, a good one with the models it reaches, a redirect is
// never followed with the key on it, and nothing is kept.
func TestAppKeyCheckIsOneRequestInTheProvidersWords(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer sk-good-key-0001":
			_, _ = w.Write([]byte(`{"data":[{"id":"deepseek/deepseek-v4-flash","name":"DeepSeek V4 Flash"},{"id":"qwen/qwen3-coder"}]}`))
		case "Bearer sk-moved-key-0002":
			http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"User not found.","code":401}}`))
		}
	}))
	defer server.Close()
	backend := newFakeBackend(newAppRecorder(), nil)
	defer backend.Close()
	backend.keyAddress = func(string) string { return server.URL + "/models/user" }

	bad, err := backend.CheckKey("openrouter", "sk-bad-key-0000")
	if err != nil || bad.OK || bad.Status != http.StatusUnauthorized || bad.Message != "User not found." {
		t.Fatalf("bad key = %+v, %v", bad, err)
	}
	good, err := backend.CheckKey("openrouter", "sk-good-key-0001")
	if err != nil || !good.OK || len(good.Models) != 2 || good.Models[0].Name != "DeepSeek V4 Flash" || good.Models[1].Name != "qwen/qwen3-coder" || good.Models[0].Provider != "openrouter" {
		t.Fatalf("good key = %+v, %v", good, err)
	}
	moved, _ := backend.CheckKey("openrouter", "sk-moved-key-0002")
	if moved.OK || moved.Status != http.StatusFound {
		t.Fatalf("a redirect = %+v", moved)
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("three checks made %d requests", n)
	}
	if _, ok := backend.keys.Key("openrouter"); ok {
		t.Fatal("a checked key was kept")
	}
	if _, err := backend.CheckKey("nowhere", "sk-good-key-0001"); err == nil {
		t.Fatal("a provider codeaf cannot connect was checked")
	}
	// A provider that never answers is said as the host it could not reach.
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer stall.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	timeout := appCheckKey(ctx, nil, stall.URL+"/models", "deepseek", "sk-good-key-0001")
	if timeout.OK || timeout.Status != "timeout" || timeout.Message != "No answer · couldn't reach 127.0.0.1 in 10s" {
		t.Fatalf("timeout = %+v", timeout)
	}
}

// KEYS LIVE IN MEMORY. A key the app sets is the one every reader finds, it
// is never written to the profile, a removed key is not replaced by the
// profile's, and the providers list says which are connected by four
// characters only.
func TestAppKeysAreHeldInMemoryAndOutrankTheProfile(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := os.WriteFile(config.BudgetConfigPath(profile), []byte(`{"api_key":"sk-or-profile-key-1111"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	creds := config.NewAppCredentials()
	restore := config.UseAppCredentials(creds)
	defer restore()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	defer backend.Close()
	if backend.keys != creds {
		t.Fatal("the backend holds other keys than the process's")
	}

	if err := backend.SetKey("openrouter", "sk-or-app-key-a91f"); err != nil {
		t.Fatal(err)
	}
	if got := config.APIKeyAt(profile); got != "sk-or-app-key-a91f" {
		t.Fatalf("the key readers found %q", redact.Secrets(got))
	}
	if err := backend.SetKey("deepseek", "sk-deepseek-app-key-7c2e"); err != nil {
		t.Fatal(err)
	}
	providers, err := backend.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]appwire.Provider{}
	for _, p := range providers {
		byID[p.ID] = p
	}
	if p := byID["openrouter"]; !p.SignedIn || p.KeyHint != "…a91f" || !p.SignIn || providers[0].ID != "openrouter" {
		t.Fatalf("openrouter = %+v", p)
	}
	if p := byID["deepseek"]; !p.SignedIn || p.KeyHint != "…7c2e" || p.SignIn {
		t.Fatalf("deepseek = %+v", p)
	}
	// The ChatGPT sign-in is listed as the browser sign-in it is, an account
	// rather than a pasted key, and with nothing signed in it says so and draws
	// no hint from any key.
	if p := byID["codex"]; p.Kind != appwire.ProviderBrowser || p.AuthKind != "account" || !p.SignIn || p.SignedIn || p.KeyHint != "" {
		t.Fatalf("Codex must be a browser sign-in to an account, not a pasted key: %+v", p)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.ProviderChanged)
		return ok && changed.Provider.ID == "deepseek" && changed.Provider.SignedIn && changed.Key == ""
	})
	raw, _ := os.ReadFile(config.BudgetConfigPath(profile))
	if strings.Contains(string(raw), "app-key") {
		t.Fatalf("a key reached the profile: %s", redact.Secrets(string(raw)))
	}

	if err := backend.RemoveKey("openrouter"); err != nil {
		t.Fatal(err)
	}
	if got := config.APIKeyAt(profile); got != "" {
		t.Fatal("a removed key was replaced by the profile's")
	}
	if err := backend.SetKey("nowhere", "sk-x-0000000000"); err == nil {
		t.Fatal("a key for no provider was kept")
	}
}

// SETUP RESUMES. A setup never started is at step one offering the $20.00 day
// cap; a step the app saved is where the next launch finds it, and the day cap
// it chose is the app's.
func TestAppSetupIsKeptAcrossLaunches(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	path := filepath.Join(t.TempDir(), appStateName)
	first := newFakeBackend(newAppRecorder(), nil)
	first.state = &appState{path: path}
	setup, err := first.Setup()
	if err != nil || setup.Step != (appwire.SetupStep{N: 1}) || setup.DayCapUSD == nil || *setup.DayCapUSD != 20 {
		t.Fatalf("a new setup = %+v, %v", setup, err)
	}
	cap := 35.0
	if _, err := first.SetSetup(appwire.Setup{Step: appwire.SetupStep{N: 3}, Provider: "OpenRouter", DayCapUSD: &cap}); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := newFakeBackend(newAppRecorder(), nil)
	defer second.Close()
	second.state = &appState{path: path}
	setup, err = second.Setup()
	if err != nil || setup.Step != (appwire.SetupStep{N: 3}) || setup.Provider != "openrouter" || *setup.DayCapUSD != 35 {
		t.Fatalf("the resumed setup = %+v, %v", setup, err)
	}
	if settings, _ := second.Settings(); settings.DayCapUSD == nil || *settings.DayCapUSD != 35 {
		t.Fatalf("the setup's day cap is not the app's: %+v", settings.DayCapUSD)
	}
}

// A SETUP KEPT BY A FOUR-STEP BUILD LANDS ON ITS STEP IN THREE. Setup was four
// steps until the welcome and the connecting step became one; a person who
// quit halfway and then updated resumes where they were — the welcome or the
// connecting step on the first, the defaults on the second, how to start on
// the third — and a kept 4, which is no step now, does not cost them the whole
// file. A step this build keeps is read back as it was kept.
func TestAppSetupKeptByAFourStepBuildLandsOnItsStep(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	for kept, want := range map[string]appwire.SetupStep{
		`1`:      {N: 1},
		`2`:      {N: 1},
		`3`:      {N: 2},
		`4`:      {N: 3},
		`"done"`: {Done: true},
	} {
		path := filepath.Join(t.TempDir(), appStateName)
		old := `{"setup":{"step":` + kept + `,"provider":"openrouter","day_cap_usd":30},"day_cap_usd":30,"work_folder":"/w"}`
		if err := os.WriteFile(path, []byte(old), appStateFileMode); err != nil {
			t.Fatal(err)
		}
		backend := newFakeBackend(newAppRecorder(), nil)
		backend.state = &appState{path: path}
		setup, err := backend.Setup()
		if err != nil || setup.Step != want || setup.Provider != "openrouter" || setup.DayCapUSD == nil || *setup.DayCapUSD != 30 {
			backend.Close()
			t.Fatalf("a four-step setup kept at %s = %+v, %v; want step %+v", kept, setup, err, want)
		}
		if settings, err := backend.Settings(); err != nil || settings.WorkFolder != "/w" {
			backend.Close()
			t.Fatalf("the rest of a four-step file was not read: %+v, %v", settings, err)
		}
		backend.Close()
	}

	// What this build keeps is counted in three and read back unmoved.
	path := filepath.Join(t.TempDir(), appStateName)
	backend := newFakeBackend(newAppRecorder(), nil)
	defer backend.Close()
	backend.state = &appState{path: path}
	for _, step := range []appwire.SetupStep{{N: 1}, {N: 2}, {N: 3}, {Done: true}} {
		if _, err := backend.SetSetup(appwire.Setup{Step: step}); err != nil {
			t.Fatal(err)
		}
		again := newFakeBackend(newAppRecorder(), nil)
		again.state = &appState{path: path}
		setup, err := again.Setup()
		again.Close()
		if err != nil || setup.Step != step {
			t.Fatalf("a setup kept at %+v read back as %+v, %v", step, setup, err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"setup_steps": 3`) {
		t.Fatalf("the kept file does not say how many steps it counts in:\n%s (%v)", raw, err)
	}
}

// SETTINGS.SET changes only what it names, says settings.changed, writes the
// shared rows to the profile and the app's own to its file, and a new agent
// with no workspace works in the chosen folder.
func TestAppSettingsSetChangesWhatItNames(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv(home.EnvVar, t.TempDir())
	work := resolvedTempDir(t)
	conv := &fakeConversation{}
	var mu sync.Mutex
	var opens, releases int
	var opened []string
	open := fakeOpener(conv, &opens, &releases, &mu)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		opened = append(opened, ask.Workspace)
		mu.Unlock()
		return open(ask)
	})
	defer backend.Close()
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}

	clock, idle, folder, model := 0, 3, work, "qwen/qwen3-coder"
	cap, task := 40.0, 2.5
	settings, err := backend.SetSettings(appwire.SettingsPatch{
		DefaultModel: &model, DayCapUSD: &appwire.Figure{USD: &cap}, TaskCapUSD: &appwire.Figure{USD: &task},
		ProposedTaskClockS: &clock, IdleCloseDays: &idle, WorkFolder: &folder, BudgetUSD: &appwire.Figure{USD: &task},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.DefaultModel != model || *settings.DayCapUSD != 40 || *settings.TaskCapUSD != 2.5 || *settings.BudgetUSD != 2.5 || settings.ProposedTaskClockS != 0 || settings.IdleCloseDays != 3 || settings.WorkFolder != work {
		t.Fatalf("settings = %+v", settings)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.SettingsChanged)
		return ok && changed.Settings.IdleCloseDays == 3
	})
	if config.CrewTaskCapAt(profile) != 2.5 || config.SpendRailUSDAt(profile) != 2.5 {
		t.Fatal("the shared rows did not reach the profile")
	}
	settings, _ = backend.SetSettings(appwire.SettingsPatch{BudgetUSD: &appwire.Figure{}, DayCapUSD: &appwire.Figure{}})
	if settings.BudgetUSD != nil || settings.DayCapUSD != nil || settings.IdleCloseDays != 3 {
		t.Fatalf("cleared figures = %+v", settings)
	}
	if _, err := backend.SetSettings(appwire.SettingsPatch{TaskCapUSD: &appwire.Figure{}}); err == nil {
		t.Fatal("a task cap of none was taken")
	}
	nowhere := filepath.Join(work, "missing")
	if _, err := backend.SetSettings(appwire.SettingsPatch{WorkFolder: &nowhere}); err == nil {
		t.Fatal("a folder that is not there was taken")
	}

	if _, err := mustCreate(t, backend, appwire.CreateAsk{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || opened[0] != work {
		t.Fatalf("a new agent opened in %v", opened)
	}
}

// AN AGENT CLOSE TO ITS BUDGET ASKS, ONCE. The ask is an approval of subkind
// budget with raise, stop and tell; raising it binds the new figure to the
// conversation, keeps it in the app's file and closes the ask with a receipt.
func TestAppBudgetAskRaisesTheAgentsBudget(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	conv := &fakeConversation{script: nil, cost: 4.2}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.today = func(time.Time) float64 { return 1 }

	budget := 5.0
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), BudgetUSD: &budget})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "keep going", ""); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.Subkind == appwire.SubkindBudget
	})
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Kind != appwire.AskApproval || ask.Title != "It's close to its $5.00 budget" || !strings.HasPrefix(ask.Detail, "Spent $4.20 so far") || len(ask.Options) != 3 ||
		ask.Options[0].ID != appwire.OptionRaise || ask.Options[1].ID != appwire.OptionStop || ask.Options[2].ID != appwire.OptionTell {
		t.Fatalf("the budget ask = %+v", ask)
	}
	if asks, _ := backend.ListAsks(agent.ID); len(asks) != 1 {
		t.Fatalf("open asks = %+v", asks)
	}
	if err := backend.AnswerAsk(agent.ID, ask.ID, appwire.OptionRaise, "not money"); err == nil {
		t.Fatal("a raise with no figure was taken")
	}
	if err := backend.AnswerAsk(agent.ID, ask.ID, appwire.OptionRaise, "$8.00"); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		closed, ok := data.(appwire.AskClosed)
		return ok && closed.ID == ask.ID && closed.By == appwire.ClosedByApp && closed.Option == appwire.OptionRaise
	})
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntryReceipt && strings.HasPrefix(added.Entry.Text, "Budget raised to $8.00 · you · ")
	})
	conv.mu.Lock()
	rail := conv.rail
	conv.mu.Unlock()
	if rail != 8 {
		t.Fatalf("the conversation's limit is %v", rail)
	}
	if usd, set := backend.agentBudget(agent.ID, time.Now()); !set || usd != 8 {
		t.Fatalf("the app keeps %v (%v)", usd, set)
	}
	if asks, _ := backend.ListAsks(agent.ID); len(asks) != 0 {
		t.Fatalf("asks still open: %+v", asks)
	}
}

// THE DAY CAP REFUSES A TURN BEFORE THE ENGINE HEARS OF IT, and asks. The
// refused turn ends as a budget failure, nothing is submitted, and raising the
// cap for today runs it again with the same words; a raise for today ends at
// midnight.
func TestAppDayCapRefusesAndARaiseRunsTheTurnAgain(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	conv := &fakeConversation{}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer backend.Close()
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.today = func(time.Time) float64 { return 40 }
	cap := 40.0
	if _, err := backend.SetSettings(appwire.SettingsPatch{DayCapUSD: &appwire.Figure{USD: &cap}}); err != nil {
		t.Fatal(err)
	}

	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := backend.Send(agent.ID, "ship it", "")
	if err != nil || turn == 0 {
		t.Fatalf("send = %d, %v", turn, err)
	}
	done := rec.waitFor(t, time.Second, appTurnDone)
	rec.mu.Lock()
	refused := rec.data[done].(appwire.TurnDone)
	rec.mu.Unlock()
	if refused.OK || refused.ErrorKind != appwire.ErrorBudget || refused.Error != "Day cap reached · $40.00 of $40.00" {
		t.Fatalf("the refused turn = %+v", refused)
	}
	i := rec.waitFor(t, time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.Subkind == appwire.SubkindBudget
	})
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Title != "Day cap reached · $40.00 of $40.00" || len(ask.Options) != 2 || ask.Options[0].ID != appwire.OptionRaiseToday || ask.Options[1].ID != appwire.OptionWait || !strings.Contains(ask.Detail, "waiting to send: ship it") {
		t.Fatalf("the day ask = %+v", ask)
	}
	conv.mu.Lock()
	submits := conv.submits
	conv.mu.Unlock()
	if submits != 0 {
		t.Fatal("a refused turn reached the engine")
	}

	if err := backend.AnswerAsk(agent.ID, ask.ID, appwire.OptionRaiseToday, "60"); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntryReceipt && strings.HasPrefix(added.Entry.Text, "Day cap raised to $60.00 for today · you · ")
	})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		started, ok := data.(appwire.TurnStarted)
		return ok && started.Turn > turn
	})
	conv.mu.Lock()
	texts := append([]string(nil), conv.texts...)
	conv.mu.Unlock()
	if len(texts) != 1 || texts[0] != "ship it" {
		t.Fatalf("the turn run again sent %q", texts)
	}
	spend, _ := backend.SpendToday()
	if spend.DayCapUSD == nil || *spend.DayCapUSD != 60 {
		t.Fatalf("today's cap = %v", spend.DayCapUSD)
	}
	if settings, _ := backend.Settings(); *settings.DayCapUSD != 40 {
		t.Fatalf("a raise for today moved the setting to %v", *settings.DayCapUSD)
	}
	tomorrow, err := config.DailyBudgetUSDAtTime(config.ProfileDir(), appMidnight(time.Now()).Add(time.Minute))
	if err != nil || tomorrow != 40 {
		t.Fatalf("tomorrow's cap = %v", tomorrow)
	}
}

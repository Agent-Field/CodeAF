package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func TestStartedConversationResolvesCurrentCreditDefault(t *testing.T) {
	t.Setenv(config.ModelEnv, "")
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "key"); err != nil {
		t.Fatal(err)
	}
	settings := config.Config{ProfileDir: dir, Model: config.DefaultModel}
	if got := v3TalkModel("", settings); got != config.DefaultModel {
		t.Fatalf("healthy default = %q", got)
	}
	if err := config.WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if got := v3TalkModel("", settings); got != config.FreeChatModel {
		t.Fatalf("new low-credit conversation = %q", got)
	}
	if got := v3TalkModel("openai/gpt-4", settings); got != "openai/gpt-4" {
		t.Fatalf("flag model = %q", got)
	}
	if err := config.WriteChatModel(dir, "custom/model"); err != nil {
		t.Fatal(err)
	}
	if got := v3TalkModel("", settings); got != "custom/model" {
		t.Fatalf("saved model = %q", got)
	}
	if err := config.WriteChatModel(dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteCreditsReading(dir, "key", credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	if got := v3TalkModel("", settings); got != config.DefaultModel {
		t.Fatalf("new topped-up conversation = %q", got)
	}
}

func TestChangedKeySupersedesACompletedCreditRead(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/credits" {
			close(started)
			<-release
			fmt.Fprint(w, `{"data":{"total_credits":0.02,"total_usage":0}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"limit_remaining":null}}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "key-A"); err != nil {
		t.Fatal(err)
	}
	proc := &v3Process{ProfileDir: dir, Settings: config.Config{APIKey: "key-A", BaseURL: server.URL}}
	finished := make(chan error, 1)
	go func() { _, err := v3CreditReader(proc)(context.Background()); finished <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("read did not reach the server")
	}
	if err := config.WriteAPIKey(dir, "key-B"); err != nil {
		t.Fatal(err)
	}
	if err := proc.setAPIKey("key-B"); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-finished:
		if err != errCreditsSuperseded {
			t.Fatalf("old read ended with %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
	if _, err := os.Stat(config.ProfilePath(dir, "credits.json")); !os.IsNotExist(err) {
		t.Fatalf("superseded read wrote a record: %v", err)
	}
}

func TestProcessWatcherRecordsOnlyDefaultService402OncePerQuietPeriod(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key":
			fmt.Fprint(w, `{"data":{"limit_remaining":null}}`)
		case "/credits":
			reads.Add(1)
			fmt.Fprint(w, `{"data":{"total_credits":0.02,"total_usage":0}}`)
		default:
			w.WriteHeader(http.StatusPaymentRequired)
			fmt.Fprint(w, `{"error":{"message":"Payment Required","code":402}}`)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "key"); err != nil {
		t.Fatal(err)
	}
	proc := &v3Process{ProfileDir: dir, Settings: config.Config{APIKey: "key", BaseURL: server.URL}, processCtx: context.Background()}
	watcher := newV3CreditWatcher(proc)
	proc.creditWatcher = watcher
	defer watcher.close()
	called := make(chan struct{}, 2)
	stop := v3PaymentRefusals(proc)(func() { called <- struct{}{} })
	defer stop()
	client, err := provider.NewClient(provider.Config{APIKey: "key", BaseURL: server.URL, Model: "sim/model", Direct: true, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	request := func(c *provider.Client) {
		_, _ = c.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}})
	}
	request(client)
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not finish the balance read")
	}
	if reads.Load() != 1 || !config.CreditsLowAt(dir) {
		t.Fatal("402 did not write exactly one low record")
	}
	request(client)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusPaymentRequired) }))
	defer other.Close()
	otherClient, err := provider.NewClient(provider.Config{APIKey: "key", BaseURL: other.URL, Model: "sim/model", Direct: true, HTTPClient: other.Client()})
	if err != nil {
		t.Fatal(err)
	}
	request(otherClient)
	// Exercise the same hook synchronously so a slow guarded dispatch cannot
	// make this negative assertion pass before the quiet check ran.
	watcher.refused(server.URL)
	watcher.refused(other.URL)
	watcher.close()
	select {
	case <-called:
		t.Fatal("debounced or foreign 402 rang the listener")
	default:
	}
	if reads.Load() != 1 {
		t.Fatalf("debounced 402 made %d reads", reads.Load())
	}
}

func TestProcessWatcherDropsAReadSupersededByAChangedKey(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key":
			fmt.Fprint(w, `{"data":{"limit_remaining":null}}`)
		case "/credits":
			close(started)
			<-release
			fmt.Fprint(w, `{"data":{"total_credits":0.01,"total_usage":0}}`)
		default:
			w.WriteHeader(http.StatusPaymentRequired)
			fmt.Fprint(w, `{"error":{"message":"Payment Required","code":402}}`)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "key-A"); err != nil {
		t.Fatal(err)
	}
	proc := &v3Process{ProfileDir: dir, Settings: config.Config{APIKey: "key-A", BaseURL: server.URL}, processCtx: context.Background()}
	watcher := newV3CreditWatcher(proc)
	defer watcher.close()
	client, err := provider.NewClient(provider.Config{APIKey: "key-A", BaseURL: server.URL, Model: "sim/model", Direct: true, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not read")
	}
	if err := config.WriteAPIKey(dir, "key-B"); err != nil {
		t.Fatal(err)
	}
	if err := proc.setAPIKey("key-B"); err != nil {
		t.Fatal(err)
	}
	close(release)
	watcher.close()
	if _, err := os.Stat(config.ProfilePath(dir, "credits.json")); !os.IsNotExist(err) {
		t.Fatalf("watcher wrote a superseded record: %v", err)
	}
}

func TestEngineStartUsesTheCurrentDefaultAndItsWindow(t *testing.T) {
	proc := v3TestProcess(t)
	opts := v3Options{Workspace: t.TempDir(), Interactive: true}
	boot, err := openV3Launch(proc, opts)
	if err != nil {
		t.Fatal(err)
	}
	boot.Config.ContextWindowFor = func(model string) int {
		if model == config.FreeChatModel {
			return 262144
		}
		return 1310720
	}
	boot.Config.ContextWindow = 1310720
	seam := &v3Seam{proc: proc, boot: boot, seed: opts}
	if err := config.WriteCreditsReading(proc.ProfileDir, "test-key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	low, err := seam.start("")
	if err != nil {
		t.Fatal(err)
	}
	if low.Agent.Model() != config.FreeChatModel || low.ContextWindow != 262144 {
		t.Fatalf("low engine hello opened %q with window %d", low.Agent.Model(), low.ContextWindow)
	}
	if err := low.Agent.(*session.Agent).Close(); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteCreditsReading(proc.ProfileDir, "test-key", credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	healthy, err := seam.start("")
	if err != nil {
		t.Fatal(err)
	}
	if healthy.Agent.Model() != config.DefaultModel || healthy.ContextWindow != 1310720 {
		t.Fatalf("healthy engine hello opened %q with window %d", healthy.Agent.Model(), healthy.ContextWindow)
	}
	if err := healthy.Agent.(*session.Agent).Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalCreditDoorReadsOnlyDefaultServiceAndStoresNoBalance(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer private-key" || r.ContentLength > 0 {
			t.Errorf("credit read sent a prompt or lost the key: %s %s", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/v1/key":
			fmt.Fprint(w, `{"data":{"limit_remaining":null}}`)
		case "/api/v1/credits":
			fmt.Fprint(w, `{"data":{"total_credits":0.05,"total_usage":0.03}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "private-key"); err != nil {
		t.Fatal(err)
	}
	proc := &v3Process{ProfileDir: dir, Settings: config.Config{APIKey: "private-key", BaseURL: server.URL + "/api/v1"}}
	reading, err := v3CreditReader(proc)(context.Background())
	if err != nil || !reading.Low || !config.CreditsLowAt(proc.ProfileDir) || len(calls) != 2 {
		t.Fatalf("local credit door = %+v, %v; calls %v", reading, err, calls)
	}
	if !v3DefaultCreditBase(proc, server.URL+"/api/v1") || v3DefaultCreditBase(proc, "https://other.example/v1") {
		t.Fatal("402 from another service was treated as the default account")
	}
	options := tui3.Options{}
	settings := config.Config{ProfileDir: dir, APIKey: "private-key", BaseURL: server.URL + "/api/v1"}
	localDoors(&options, remote.Welcome{ProfileDir: dir}, settings)
	if options.ReadCredits == nil {
		t.Fatal("ordinary engine road has no surface reader")
	}
	reading, err = options.ReadCredits(context.Background())
	if err != nil || !reading.Low || len(calls) != 4 {
		t.Fatalf("ordinary engine road credit door = %+v, %v; calls %v", reading, err, calls)
	}
}

// C6 and C15: a conversation started after the balance moved opens on the
// default the check answers NOW, carries that model's window rather than the
// boot model's, and a saved choice or a flag is never moved.
func TestAFreshConversationOpensOnTheDefaultTheBalanceAnswersNow(t *testing.T) {
	t.Setenv(config.ModelEnv, "")
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "key"); err != nil {
		t.Fatal(err)
	}
	windows := map[string]int{config.DefaultModel: 1310720, config.FreeChatModel: 262144}
	boot := &v3Launch{Settings: config.Config{ProfileDir: dir}, Model: config.DefaultModel}
	boot.Config.Model = config.DefaultModel
	boot.Config.ContextWindow = windows[config.DefaultModel]
	boot.Config.ContextWindowFor = func(model string) int { return windows[model] }

	if got := v3FreshDefault(boot, ""); got != boot {
		t.Fatalf("a healthy profile moved the boot launch to %q", got.Model)
	}
	if err := config.WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	low := v3FreshDefault(boot, "")
	if low.Model != config.FreeChatModel || low.Config.Model != config.FreeChatModel {
		t.Fatalf("a low profile's new conversation opened on %q / %q", low.Model, low.Config.Model)
	}
	if low.Config.ContextWindow != windows[config.FreeChatModel] {
		t.Fatalf("the free default kept the boot window %d", low.Config.ContextWindow)
	}
	if boot.Model != config.DefaultModel {
		t.Fatal("the shared boot launch was changed in place")
	}
	if got := v3FreshDefault(boot, "openai/gpt-4"); got != boot {
		t.Fatal("a --model flag was overridden by the balance")
	}
	if err := config.WriteChatModel(dir, "openai/gpt-4"); err != nil {
		t.Fatal(err)
	}
	if got := v3FreshDefault(boot, ""); got != boot {
		t.Fatal("a saved talk model was overridden by the balance")
	}
	if err := config.WriteChatModel(dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteCreditsReading(dir, "key", credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	onFree := *boot
	onFree.Model, onFree.Config.Model = config.FreeChatModel, config.FreeChatModel
	if got := v3FreshDefault(&onFree, ""); got.Model != config.DefaultModel || got.Config.ContextWindow != windows[config.DefaultModel] {
		t.Fatalf("a topped-up profile's new conversation opened on %q with window %d", got.Model, got.Config.ContextWindow)
	}
}

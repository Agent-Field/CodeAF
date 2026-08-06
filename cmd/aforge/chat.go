package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// runChat is the resident surface: one durable graph, a head that always
// replies, a reconciler that applies mutations, a runner that executes ready
// nodes — all clients over the same SQLite file, all shut down when the TUI
// exits. The one-shot plan/run path shares none of this and stays untouched.
func runChat(args []string) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	sessionID := flags.String("session", newSessionID(), "thread session id")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true, "session": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge [chat] [--db path] [--session id]")
	}

	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	if strings.TrimSpace(*sessionID) == "" {
		return fmt.Errorf("chat session cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create chat database directory: %w", err)
	}

	settings, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aforge chat needs a model to talk with.")
		fmt.Fprintln(os.Stderr, "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.")
		return err
	}
	prefs := loadChatPrefs(filepath.Dir(path))

	chatClient, err := newLiveClient(settings, firstNonEmptyString(prefs.ChatModel, settings.Model))
	if err != nil {
		return err
	}
	taskClient, err := newLiveClient(settings, firstNonEmptyString(prefs.TaskModel, settings.Model))
	if err != nil {
		return err
	}

	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()

	workspaceRoot := filepath.Join(filepath.Dir(path), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		return fmt.Errorf("create chat workspace: %w", err)
	}

	// plans retains each planned job's graph so execution can be the headless
	// mechanism exactly: call shapes for the router, escalation verdicts, and
	// the profile records that calibrate the planner's ruler all read from the
	// same plan nodes the scheduler would have read.
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	compiler := head.NewCompiler(chatClient)
	reconciler := resident.New(graph,
		func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
			augmented := graphContext
			if sk := selfKnowledge(settings); sk != "" {
				augmented += "\n\nMeasured execution costs (this system's own measured history):\n" + sk
			}
			brief, err := compiler.Compile(settings.Context(ctx, instruction), instruction, augmented)
			if err != nil {
				return resident.Compiled{}, err
			}
			return resident.Compiled{
				Goal:        brief.Goal,
				Assumptions: brief.Assumptions,
				Scale:       brief.Scale,
				BuildsOn:    brief.BuildsOn,
				Question:    brief.Question,
			}, nil
		},
		planSubtree(settings, taskClient, plans, graph),
	).WithNarrator(narrateProgress(settings, chatClient)).
		WithDistiller(distillFacts(settings, chatClient, graph)).
		WithConsolidator(consolidateFacts(settings, chatClient, graph)).
		WithTitler(titleGoal(settings, chatClient)).
		WithReflector(reflectAcrossJobs(settings, chatClient))

	web := exec.NewWeb()
	runner := resident.NewRunner(graph, func(ctx context.Context, node store.Node) (resident.ExecResult, error) {
		// Each top-level job works in its own directory: one thread hosts
		// many unrelated jobs, and continuity between them travels through
		// the graph as digests and absolute paths, never through a shared
		// folder they could trample.
		jobDir := filepath.Join(workspaceRoot, jobIDOf(graph, node))
		jobSpace, err := exec.NewWorkspace(jobDir)
		if err != nil {
			return resident.ExecResult{}, err
		}
		// The exact leaf configuration the headless scheduler uses: the same
		// turn backstop, the same binding token budget, and a deadline that
		// scales with that budget.
		deadline := leafDeadline(chatLeafTokens)
		linear := exec.NewLinear(taskClient, jobSpace, web, chatLeafTurns, chatLeafTokens, deadline).WithStore(graph)
		planGraph, planNode := plans.lookup(node.ID)
		shape := "atomic"
		if planNode != nil {
			shape = chatLeafShape(planNode)
			// Frozen means frozen everywhere: the sentinel may not edit a
			// node whose transcript is already being written.
			plans.markRunning(planNode)
		}

		inputs := make([]exec.Input, 0)
		if digest := resident.NotebookDigest(graph, node.Brief, node.Provenance.Intent, 8); digest != "" {
			inputs = append(inputs, exec.Input{Result: digest})
		}
		digests, err := graph.DependencyDigests(node.ID, store.MaxDigestBytes)
		if err == nil {
			for _, digest := range digests {
				inputs = append(inputs, exec.Input{Result: digest})
			}
		}
		// The steering mailbox: user messages anchored to this node land in
		// the worker's transcript before its next turn. The cursor starts at
		// zero so guidance sent while the node was still queued applies too.
		var steerCursor int64
		steer := func() []string {
			messages, err := graph.NodeMessages(node.ID, steerCursor, 20)
			if err != nil {
				return nil
			}
			var lines []string
			for _, message := range messages {
				steerCursor = message.Seq
				if message.Role == store.RoleUser {
					lines = append(lines, message.Body)
				}
			}
			return lines
		}

		task := exec.Task{
			NodeID: int(node.CreatedSeq),
			Title:  firstLine(node.Brief),
			Goal:   node.Provenance.Intent,
			Brief:  node.Brief,
			Inputs: inputs,
			Steer:  steer,
		}
		// The scheduler's quality loop, inline: each attempt is one routable
		// unit carrying its call shape, a watchdog sits above the leaf's own
		// deadline so a wedged executor becomes a recorded failure rather
		// than a silent hang, and a leaf whose verdict says a stronger model
		// might fix it gets exactly one escalation when a panel offers one.
		attempts := 1
		if taskClient.escalatable() {
			attempts = 2
		}
		// One job is one cache lineage, exactly as one headless run is: the
		// affinity key rides every leaf of the job so a prefix cache warmed
		// by one worker serves its siblings.
		ctx = provider.WithCacheKey(ctx, provider.RunCacheKey(node.Provenance.Intent, taskClient.Model()))
		var outcome *exec.Outcome
		var spent exec.Usage
		for attempt := 0; attempt < attempts; attempt++ {
			runCtx := provider.WithCallShape(settings.ExecContext(ctx), provider.ClassExecLeaf, attempt, shape)
			outcome, err = runLeafWithWatchdog(runCtx, linear, task, deadline+2*time.Minute)
			if outcome != nil {
				spent.PromptTokens += outcome.Usage.PromptTokens
				spent.CompletionTokens += outcome.Usage.CompletionTokens
				spent.Cost += outcome.Usage.Cost
			}
			if err == nil && outcome != nil && !outcome.Verdict.Escalates() {
				break
			}
		}
		if planNode != nil {
			plans.recordOutcome(planNode, outcome, err)
		}
		// Result-driven revision: each landed leaf is shown to the sentinel,
		// which edits the job's unstarted remainder only when this result
		// contradicts a specific assumption in a specific node. Its default
		// is no change; the store refuses everything else.
		if planGraph != nil && outcome != nil {
			plans.reviseAfter(ctx, settings, taskClient, graph, node, planGraph, outcome.Text, err != nil)
		}
		// A graph's sink landing means its run is over: the measured leaves
		// become profile evidence, exactly as recordAndCalibrate does after a
		// headless run. Detached, because the ruler is telemetry and the
		// user's result must not wait on it.
		if landed := plans.takeIfRoot(node.ID); landed != nil {
			go recordAndCalibrate(settings.Context(context.Background(), landed.Goal), taskClient, settings, landed)
		} else if planGraph == nil && node.Parent == store.RootID && outcome != nil {
			go recordSingleLeaf(settings, node, outcome)
		}
		if err != nil {
			return resident.ExecResult{}, err
		}
		text := outcome.Text
		// Artifact paths come back workspace-relative; the user's next act is
		// opening the file, so the summary carries where it actually lives.
		absolute := make([]string, 0, len(outcome.Artifacts))
		for _, artifact := range outcome.Artifacts {
			absolute = append(absolute, filepath.Join(jobDir, artifact))
		}
		if len(absolute) > 0 {
			text += "\n\nFiles:\n" + strings.Join(absolute, "\n")
		}
		// The quality gate: before a deliverable lands in the thread, one
		// judge call asks the only question that matters — would the person
		// who asked accept this as done? A named gap earns exactly one
		// revision pass with the critique as input; then the result ships
		// either way, because a gate that can loop is a gate that can stall.
		if node.Parent == store.RootID && outcome.Stop != exec.StopBudget && outcome.Stop != exec.StopTurnCap {
			if gaps := judgeDeliverable(ctx, settings, taskClient, node, text); gaps != "" {
				revision := task
				revision.Inputs = append(append([]exec.Input{}, inputs...), exec.Input{
					Result: "A reviewer compared the previous attempt against the original request and found gaps that must be closed:\n" + gaps +
						"\n\nThe previous attempt (build on it, fix the gaps, do not start over):\n" + text,
				})
				retryCtx := provider.WithCallShape(settings.ExecContext(ctx), provider.ClassExecLeaf, 1, shape)
				polished, polishErr := runLeafWithWatchdog(retryCtx, linear, revision, deadline+2*time.Minute)
				if polishErr == nil && polished != nil && strings.TrimSpace(polished.Text) != "" {
					spent.PromptTokens += polished.Usage.PromptTokens
					spent.CompletionTokens += polished.Usage.CompletionTokens
					spent.Cost += polished.Usage.Cost
					text = polished.Text
					for _, artifact := range polished.Artifacts {
						text += "\n" + filepath.Join(jobDir, artifact)
					}
					_, _ = graph.PostMessage(store.Message{
						SessionID: node.Provenance.SessionID,
						Role:      store.RoleSystem,
						Body:      "a review found gaps in the first draft — revised before delivering: " + firstLine(gaps),
					})
				}
			}
		}
		// Just-in-time re-decomposition: a budget or turn-cap stop is the
		// planner's clearest "one node held too much". The partial result
		// lands as this node's summary; the remainder — planned from what
		// the partial actually contains — is spliced in as deeper structure
		// that consumes it and feeds everything that was waiting.
		if outcome.Stop == exec.StopBudget || outcome.Stop == exec.StopTurnCap {
			spliced, sink, replanErr := resident.ReplanOverrun(ctx, graph, node, outcome.Text, absolute,
				replanRemainder(settings, taskClient, plans, graph))
			if replanErr == nil && spliced > 0 {
				text += fmt.Sprintf("\n\n[partial: ran out of %s — the remainder was re-planned into %d follow-up nodes; the finished result lands with %s]",
					outcome.Stop, spliced, sink)
				_, _ = graph.PostMessage(store.Message{
					SessionID: node.Provenance.SessionID,
					Role:      store.RoleSystem,
					Body: fmt.Sprintf("%s ran out of room mid-work — kept its partial progress and split the remainder into %d queued pieces",
						firstLine(node.Brief), spliced),
				})
			}
		}
		return resident.ExecResult{
			Summary:          text,
			PromptTokens:     spent.PromptTokens,
			CompletionTokens: spent.CompletionTokens,
			Cost:             spent.Cost,
		}, nil
	}, "chat-runner", 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var background sync.WaitGroup
	background.Add(3)
	// Routing is a structuring call, and it was the one loop served with a bare
	// context: without the configured effort knob, a reasoning model spends the
	// head's whole token cap deliberating and returns empty text — measured as
	// 600/600 completion tokens of thought and zero answer on the default model.
	go func() { defer background.Done(); _ = head.New(chatClient, graph).Serve(settings.Context(ctx, "head")) }()
	go func() { defer background.Done(); _ = reconciler.Serve(ctx) }()
	go func() { defer background.Done(); _ = runner.Serve(ctx) }()

	commander := &chatCommander{
		settings:      settings,
		database:      path,
		prefsDir:      filepath.Dir(path),
		workspaceRoot: workspaceRoot,
		chatClient:    chatClient,
		taskClient:    taskClient,
		store:         graph,
		prefs:         prefs,
		sessionID:     *sessionID,
	}
	err = tui.RunWithCommander(graph, *sessionID, commander)
	cancel()
	waitWithGrace(&background, 5*time.Second)
	return err
}

// chatPrefs persists the surface's model choices across launches. It lives
// beside the graph database so the whole resident state moves as one
// directory.
type chatPrefs struct {
	ChatModel string `json:"chat_model,omitempty"`
	TaskModel string `json:"task_model,omitempty"`

	// SplitPct is the chat pane's share of the terminal width in percent,
	// set by dragging the divider (or [ and ]) in the TUI.
	SplitPct int `json:"split_pct,omitempty"`
}

var fallbackChatModels = []string{
	"~deepseek/deepseek-v4-flash-latest",
	"moonshotai/kimi-k2.6",
	"qwen/qwen3-30b-a3b",
	"google/gemma-3-12b-it",
}

const (
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	modelCatalogTTL     = 24 * time.Hour
	maxCatalogBytes     = 16 << 20
)

var modelCatalogHTTPClient = &http.Client{Timeout: 15 * time.Second}

// chatCommander bridges surface commands to the two hot-swappable clients
// and the durable command journal. Session state lives here so /new and a
// subsequent /cancel always agree about which thread owns the request.
type chatCommander struct {
	settings      config.Config
	database      string
	prefsDir      string
	workspaceRoot string

	chatClient *liveClient
	taskClient *liveClient
	store      *store.Store

	mu        sync.Mutex
	prefs     chatPrefs
	sessionID string

	catalogOnce sync.Once
	catalog     []tui.ModelChoice
}

func (c *chatCommander) Models() []string {
	candidates := make([]string, 0, len(c.settings.Panel.Models)+7)
	for _, spec := range c.settings.Panel.Models {
		candidates = append(candidates, spec.Slug)
	}
	candidates = append(candidates, c.settings.Model, c.chatClient.Model(), c.taskClient.Model())
	models := dedupeModels(candidates)
	if len(models) < 4 {
		models = dedupeModels(append(models, fallbackChatModels...))
	}
	return models
}

func (c *chatCommander) Catalog() []tui.ModelChoice {
	c.catalogOnce.Do(func() {
		cached, cachedOK := loadModelCatalog(c.prefsDir)
		if cachedOK && time.Now().Before(cached.FetchedAt.Add(modelCatalogTTL)) {
			c.catalog = cached.Models
			return
		}

		models, err := fetchModelCatalog()
		if err == nil && len(models) > 0 {
			c.catalog = models
			_ = saveModelCatalog(c.prefsDir, modelCatalogCache{
				FetchedAt: time.Now(),
				Models:    models,
			})
			return
		}
		if cachedOK {
			c.catalog = cached.Models
			return
		}
		c.catalog = make([]tui.ModelChoice, 0, len(c.Models()))
		for _, model := range c.Models() {
			c.catalog = append(c.catalog, tui.ModelChoice{Slug: model})
		}
	})
	return append([]tui.ModelChoice(nil), c.catalog...)
}

func (c *chatCommander) CurrentModel(role string) string {
	if role == "work" {
		return c.taskClient.Model()
	}
	return c.chatClient.Model()
}

func (c *chatCommander) SetModel(role, slug string) error {
	switch role {
	case "talk":
		if err := c.chatClient.SetModel(slug); err != nil {
			return err
		}
	case "work":
		if err := c.taskClient.SetModel(slug); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown model role %q", role)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if role == "talk" {
		c.prefs.ChatModel = c.chatClient.Model()
	} else {
		c.prefs.TaskModel = c.taskClient.Model()
	}
	if err := saveChatPrefs(c.prefsDir, c.prefs); err != nil {
		return fmt.Errorf("save chat model preference: %w", err)
	}
	return nil
}

// SplitPct and SaveSplitPct persist the TUI's chat/graph divider position in
// the same prefs file as the model choices.
func (c *chatCommander) SplitPct() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefs.SplitPct
}

func (c *chatCommander) SaveSplitPct(pct int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefs.SplitPct = pct
	_ = saveChatPrefs(c.prefsDir, c.prefs)
}

func (c *chatCommander) NewSession() (string, error) {
	sessionID := newSessionID()
	c.mu.Lock()
	c.sessionID = sessionID
	c.mu.Unlock()
	return sessionID, nil
}

func (c *chatCommander) Cancel(nodeID string) error {
	c.mu.Lock()
	sessionID := c.sessionID
	c.mu.Unlock()
	_, err := c.store.RequestCommand(store.Command{
		SessionID:   sessionID,
		Kind:        store.CommandCancel,
		Target:      nodeID,
		Instruction: "cancelled from the TUI",
	})
	return err
}

func (c *chatCommander) NodeTrace(nodeID string, maxBytes int) string {
	if c == nil || c.store == nil || c.workspaceRoot == "" || nodeID == "" || maxBytes <= 0 {
		return ""
	}
	node, ok, err := c.store.Node(nodeID)
	if err != nil || !ok {
		return ""
	}
	jobDir := filepath.Join(c.workspaceRoot, jobIDOf(c.store, node))
	file, err := os.Open(filepath.Join(jobDir, ".obs", fmt.Sprintf("%d.trace.log", node.CreatedSeq)))
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - int64(maxBytes)
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)))
	if err != nil {
		return ""
	}
	return string(data)
}

func (c *chatCommander) Notebook(limit int) []store.Fact {
	if c == nil || c.store == nil {
		return nil
	}
	facts, err := c.store.ActiveFacts("", 100)
	if err != nil {
		return nil
	}
	if limit > 0 && len(facts) > limit {
		facts = facts[:limit]
	}
	return facts
}

func (c *chatCommander) DatabasePath() string { return c.database }

type modelCatalogCache struct {
	FetchedAt time.Time         `json:"fetched_at"`
	Models    []tui.ModelChoice `json:"models"`
}

type openRouterCatalogResponse struct {
	Data []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

func fetchModelCatalog() ([]tui.ModelChoice, error) {
	request, err := http.NewRequest(http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := modelCatalogHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("OpenRouter model catalog: %s", response.Status)
	}

	var payload openRouterCatalogResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxCatalogBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]tui.ModelChoice, 0, len(payload.Data))
	seen := make(map[string]bool, len(payload.Data))
	for _, item := range payload.Data {
		slug := strings.TrimSpace(item.ID)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		models = append(models, tui.ModelChoice{
			Slug:  slug,
			Name:  strings.TrimSpace(item.Name),
			Price: formatModelPrice(item.Pricing.Prompt, item.Pricing.Completion),
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("OpenRouter model catalog is empty")
	}
	return models, nil
}

func formatModelPrice(prompt, completion string) string {
	promptPrice, promptErr := strconv.ParseFloat(strings.TrimSpace(prompt), 64)
	completionPrice, completionErr := strconv.ParseFloat(strings.TrimSpace(completion), 64)
	if promptErr != nil || completionErr != nil || promptPrice < 0 || completionPrice < 0 ||
		math.IsNaN(promptPrice) || math.IsNaN(completionPrice) ||
		math.IsInf(promptPrice, 0) || math.IsInf(completionPrice, 0) {
		return ""
	}
	return fmt.Sprintf("$%s/M in · $%s/M out",
		formatMillionPrice(promptPrice*1_000_000),
		formatMillionPrice(completionPrice*1_000_000),
	)
}

func formatMillionPrice(price float64) string {
	formatted := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(price, 'f', 6, 64), "0"), ".")
	if formatted == "" {
		return "0"
	}
	return formatted
}

func modelCatalogPath(dir string) string { return filepath.Join(dir, "models-catalog.json") }

func loadModelCatalog(dir string) (modelCatalogCache, bool) {
	var cached modelCatalogCache
	raw, err := os.ReadFile(modelCatalogPath(dir))
	if err != nil || json.Unmarshal(raw, &cached) != nil || cached.FetchedAt.IsZero() || len(cached.Models) == 0 {
		return modelCatalogCache{}, false
	}
	models := cached.Models[:0]
	seen := make(map[string]bool, len(cached.Models))
	for _, model := range cached.Models {
		model.Slug = strings.TrimSpace(model.Slug)
		if model.Slug == "" || seen[model.Slug] {
			continue
		}
		seen[model.Slug] = true
		models = append(models, model)
	}
	cached.Models = models
	return cached, len(cached.Models) > 0
}

func saveModelCatalog(dir string, cached modelCatalogCache) error {
	raw, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(modelCatalogPath(dir), raw, 0o600)
}

func dedupeModels(candidates []string) []string {
	seen := make(map[string]bool, len(candidates))
	models := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		models = append(models, candidate)
	}
	return models
}

func prefsPath(dir string) string { return filepath.Join(dir, "settings.json") }

func loadChatPrefs(dir string) chatPrefs {
	var prefs chatPrefs
	raw, err := os.ReadFile(prefsPath(dir))
	if err != nil {
		return prefs
	}
	_ = json.Unmarshal(raw, &prefs)
	return prefs
}

func saveChatPrefs(dir string, prefs chatPrefs) error {
	raw, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(prefsPath(dir), raw, 0o600)
}

// liveClient is a model-switchable completion client. Every consumer (head,
// compiler, executor) holds this one handle; swapping the model behind it
// takes effect on the next call without restarting any loop.
type liveClient struct {
	settings config.Config
	mu       sync.RWMutex
	model    string
	client   router.Client
}

func newLiveClient(settings config.Config, model string) (*liveClient, error) {
	client, err := settings.ClientFor(model)
	if err != nil {
		return nil, err
	}
	return &liveClient{settings: settings, model: model, client: client}, nil
}

func (l *liveClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	l.mu.RLock()
	client := l.client
	l.mu.RUnlock()
	return client.CompleteWithMessages(ctx, messages, options...)
}

func (l *liveClient) Model() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.model
}

// escalatable reports whether a failed leaf has somewhere stronger to go —
// the same condition the headless runner uses to grant one escalation.
func (l *liveClient) escalatable() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if panel, ok := l.client.(*router.Router); ok {
		return panel.Rungs() > 1
	}
	return false
}

func (l *liveClient) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	client, err := l.settings.ClientFor(model)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.model = model
	l.client = client
	l.mu.Unlock()
	return nil
}

func waitWithGrace(group *sync.WaitGroup, grace time.Duration) {
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(grace):
	}
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultChatDB() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".aforge", "graph.db")
	}
	return filepath.Join(home, ".aforge", "graph.db")
}

func expandHome(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("chat database path cannot be empty")
	}
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func newSessionID() string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%08x", time.Now().UnixNano())
}

// planSubtree decides how much structure a compiled request deserves. A
// lookup or single task is one node — the head already replied, so the only
// latency that matters is the work itself. A project runs the full planning
// pipeline and splices the resulting graph, which is where parallel workers
// pay for the planning pass.
// chatLeafTurns and chatLeafTokens mirror the headless run defaults exactly:
// the same runaway backstop and the same binding per-leaf token budget, so a
// worker in the chat surface is the same worker the benchmarks measured.
const (
	chatLeafTurns  = 200
	chatLeafTokens = 150_000
)

// leafDeadline scales the hang backstop with the granted budget, as the
// headless runner does: 15 minutes floor, one minute per 50k tokens above it.
func leafDeadline(budget int) time.Duration {
	deadline := 15 * time.Minute
	if scaled := time.Duration(budget/50_000) * time.Minute; scaled > deadline {
		deadline = scaled
	}
	return deadline
}

// jobPlans retains each planned job's graph for the lifetime of its run, so
// per-leaf execution reads the same plan facts the headless scheduler reads:
// kind and size for the call shape, and the measured outcome fields that
// become profile records. Best-effort by design — a restart forgets in-flight
// graphs and costs only telemetry, never work.
type jobPlans struct {
	mu     sync.Mutex
	graphs map[string]plannedJob
}

// plannedJob pairs a retained graph with the store id of its sink node — the
// landing that means "this graph's run is over, record it".
type plannedJob struct {
	graph *plan.Graph
	root  string
}

func (j *jobPlans) put(prefix string, graph *plan.Graph, root string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.graphs[prefix] = plannedJob{graph: graph, root: root}
}

// lookup resolves a store node id ("<prefix>-n<planID>") back to its plan
// node. Single-task jobs have no plan graph and resolve to nil.
func (j *jobPlans) lookup(nodeID string) (*plan.Graph, *plan.Node) {
	cut := strings.LastIndex(nodeID, "-n")
	if cut < 0 {
		return nil, nil
	}
	planID, err := strconv.Atoi(nodeID[cut+2:])
	if err != nil {
		return nil, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok := j.graphs[nodeID[:cut]]
	if !ok {
		return nil, nil
	}
	for index := range entry.graph.Nodes {
		if entry.graph.Nodes[index].ID == planID {
			return entry.graph, &entry.graph.Nodes[index]
		}
	}
	return entry.graph, nil
}

// recordOutcome writes a leaf's measured ending onto its plan node — the same
// fields, in the same shape, that the headless scheduler records.
func (j *jobPlans) recordOutcome(node *plan.Node, outcome *exec.Outcome, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if outcome != nil {
		node.Turns = outcome.Turns
		node.Tokens = outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
		node.Cost = outcome.Usage.Cost
		node.Stop = string(outcome.Stop)
		node.Verdict = outcome.Verdict
		node.Artifacts = outcome.Artifacts
		node.Result = outcome.Text
	}
	if err != nil || outcome == nil || strings.TrimSpace(node.Result) == "" {
		node.State = plan.StateFailed
	} else {
		node.State = plan.StateDone
	}
}

// takeIfRoot removes and returns a job's graph when the landed node is that
// graph's sink — the moment its leaves become profile evidence. Any other
// node returns nil and the graph stays for the leaves still to land.
func (j *jobPlans) takeIfRoot(nodeID string) *plan.Graph {
	cut := strings.LastIndex(nodeID, "-n")
	if cut < 0 {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok := j.graphs[nodeID[:cut]]
	if !ok || entry.root != nodeID {
		return nil
	}
	delete(j.graphs, nodeID[:cut])
	return entry.graph
}

func (j *jobPlans) markRunning(node *plan.Node) {
	j.mu.Lock()
	defer j.mu.Unlock()
	node.State = plan.StateRunning
}

// reviseAfter runs the sentinel over a job's remaining plan in light of one
// landed result, and mirrors whatever it legally edits onto the store. The
// whole pass holds the registry lock — the sentinel must see a consistent
// graph, and its call is a short structuring call — and it skips entirely
// when the job has no unstarted work left to edit.
func (j *jobPlans) reviseAfter(ctx context.Context, settings config.Config, client *liveClient, graph *store.Store, node store.Node, planGraph *plan.Graph, summary string, failed bool) {
	cut := strings.LastIndex(node.ID, "-n")
	if cut < 0 {
		return
	}
	prefix := node.ID[:cut]
	j.mu.Lock()
	entry, ok := j.graphs[prefix]
	j.mu.Unlock()
	if !ok || entry.root == node.ID {
		return
	}
	active, err := graph.ActiveNodes()
	if err != nil {
		return
	}
	pending := 0
	for _, sibling := range active {
		if sibling.Status == store.Pending && strings.HasPrefix(sibling.ID, prefix) && sibling.ID != entry.root {
			pending++
		}
	}
	if pending == 0 {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	operations, _, err := plan.Revise(settings.Context(ctx, planGraph.Goal), client, planGraph,
		resident.RevisionEvent(node, summary, failed))
	if err != nil || len(operations) == 0 {
		return
	}
	applied, _ := resident.ApplyRevision(graph, planGraph, prefix, entry.root, operations)
	if applied == 0 {
		return
	}
	reasons := make([]string, 0, len(operations))
	for _, operation := range operations {
		if operation.Applied && strings.TrimSpace(operation.Reason) != "" {
			reasons = append(reasons, operation.Op+": "+firstLine(operation.Reason))
		}
	}
	body := fmt.Sprintf("revised the remaining plan after %q — %d change(s)", firstLine(nodeDisplay(node)), applied)
	if len(reasons) > 0 {
		body += "\n" + strings.Join(reasons, "\n")
	}
	_, _ = graph.PostMessage(store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		Body:      body,
	})
}

func nodeDisplay(node store.Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	return firstLine(node.Brief)
}

// judgeDeliverablePrompt is a gate, not a critic: its default is pass, and a
// fail must name the specific element of the request that is absent. The
// failure mode being prevented is the gate that always finds something —
// polish loops that spend the user's money on taste.
const judgeDeliverablePrompt = `You are the final gate before a finished piece of work is handed to the person who asked for it. You receive their verbatim request, the compiled goal, and the deliverable as produced.

Judge exactly one question: would the person who asked accept this as done? Default to PASS. The gate exists for real gaps, not polish — wording, style, and things they never asked for are not gaps.

FAIL only when you can name a specific element of the request that is absent, unanswered, or unsupported by evidence the goal promised. Quote or name the missing element concretely enough that a worker could close the gap from your words alone.

Return exactly one JSON object, nothing else: {"pass": true} or {"pass": false, "gaps": "<the named gaps>"}`

// judgeDeliverable returns the named gaps, or "" for pass. Every failure of
// the gate itself — provider error, unparseable reply — is a pass: the gate
// must never be the reason a finished job cannot land.
func judgeDeliverable(ctx context.Context, settings config.Config, client *liveClient, node store.Node, deliverable string) string {
	ask := node.Provenance.Intent
	body := "Verbatim request:\n" + ask + "\n\nCompiled goal:\n" + node.Brief + "\n\nDeliverable as produced:\n" + deliverable
	response, err := client.CompleteWithMessages(settings.Context(ctx, "gate"), []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: judgeDeliverablePrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body}}},
	}, ai.WithMaxTokens(400))
	if err != nil || response == nil {
		return ""
	}
	text := strings.TrimSpace(response.Text())
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ""
	}
	var verdict struct {
		Pass bool   `json:"pass"`
		Gaps string `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &verdict); err != nil {
		return ""
	}
	if verdict.Pass {
		return ""
	}
	return strings.TrimSpace(verdict.Gaps)
}

// chatLeafShape is the scheduler's leaf population key, verbatim: synthesis
// apart from work, and the leaves one agent may not fit apart from the rest.
func chatLeafShape(node *plan.Node) string {
	if node.Kind == plan.KindSynthesis {
		return "synthesis"
	}
	switch node.Size {
	case plan.SizeOversized, plan.SizeBorderline:
		return "oversized"
	default:
		return "atomic"
	}
}

// runLeafWithWatchdog is the scheduler's node watchdog, inline: the executor
// has its own deadline, so this only fires when a worker is wedged past every
// limit it was given — turning a silent forever-hang into a recorded failure.
func runLeafWithWatchdog(ctx context.Context, linear *exec.Linear, task exec.Task, timeout time.Duration) (*exec.Outcome, error) {
	type landing struct {
		outcome *exec.Outcome
		err     error
	}
	done := make(chan landing, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- landing{nil, fmt.Errorf("executor panicked: %v", recovered)}
			}
		}()
		outcome, err := linear.Run(ctx, task)
		done <- landing{outcome, err}
	}()
	select {
	case result := <-done:
		return result.outcome, result.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("executor did not return within %s; abandoned", timeout.Round(time.Second))
	}
}

// recordSingleLeaf keeps single-task jobs contributing to the same ruler the
// planner sizes with: one honest record, no recalibration call.
func recordSingleLeaf(settings config.Config, node store.Node, outcome *exec.Outcome) {
	measured, err := profile.Load(settings.ProfileDir, settings.Model, "linear")
	if err != nil {
		return
	}
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = firstLine(node.Brief)
	}
	measured.Add(profile.Record{
		Title:   title,
		Summary: firstLine(node.Brief),
		Size:    string(plan.SizeAtomic),
		Turns:   outcome.Turns,
		Tokens:  outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens,
		Stop:    string(outcome.Stop),
		Verdict: outcome.Verdict,
	})
	_ = measured.Save()
}

func planSubtree(settings config.Config, client *liveClient, plans *jobPlans, history *store.Store) resident.PlanFunc {
	return func(ctx context.Context, compiled resident.Compiled) (store.Subtree, error) {
		prefix, err := subtreePrefix()
		if err != nil {
			return store.Subtree{}, err
		}
		if compiled.Scale != head.ScaleProject {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID:    prefix,
				Brief: compiled.Goal,
				Stage: 1,
			}}}, nil
		}
		graph, err := plan.Build(settings.Context(ctx, compiled.Goal), client, compiled.Goal, plan.Options{
			Recall:       recallHits(history, compiled.Goal, groundRecallLimit),
			SpineSamples: settings.SpineSamples,
			// One level deeper than the one-shot default: chat projects are
			// where visible fan-out is the product, and the compiler now
			// names the parts for the planner to expand.
			MaxDepth:   settings.MaxDepth + 1,
			NodeBudget: settings.NodeBudget,
			Briefs:     true,
		})
		if err != nil {
			return store.Subtree{}, err
		}
		// Per-leaf working contracts, exactly as a headless run writes them
		// before dispatch. A contract failure costs specificity, not the job.
		if _, err := plan.Contracts(ctx, client, graph); err != nil {
			fmt.Fprintf(os.Stderr, "note: could not write contracts: %v\n", err)
		}
		subtree, err := resident.SubtreeFromPlan(graph, prefix)
		if err != nil {
			return store.Subtree{}, err
		}
		plans.put(prefix, graph, subtreeSink(subtree))
		return subtree, nil
	}
}

// subtreeSink is the one spec with no parent — the node whose landing means
// the whole subtree has run.
func subtreeSink(subtree store.Subtree) string {
	for _, spec := range subtree.Nodes {
		if spec.Parent == "" {
			return spec.ID
		}
	}
	return ""
}

// replanRemainder plans an exhausted leaf's remaining work: the same full
// planning pass a fresh project gets — briefs, contracts, the retained graph
// for call shapes and profile records — scoped to what the partial left
// undone. Falls back to one continuation node rather than failing: a leaf
// out of budget deserves at least one fresh worker on the remainder.
func replanRemainder(settings config.Config, client *liveClient, plans *jobPlans, history *store.Store) resident.OverrunPlanFunc {
	return func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		graph, err := plan.Build(settings.Context(ctx, goal), client, goal, plan.Options{
			Recall:       recallHits(history, goal, groundRecallLimit),
			SpineSamples: settings.SpineSamples,
			MaxDepth:     settings.MaxDepth,
			NodeBudget:   settings.NodeBudget,
			Briefs:       true,
		})
		if err != nil {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID:    prefix,
				Brief: goal,
				Title: "Finish the remainder",
				Stage: 1,
			}}}, nil
		}
		if _, err := plan.Contracts(ctx, client, graph); err != nil {
			fmt.Fprintf(os.Stderr, "note: could not write repair contracts: %v\n", err)
		}
		subtree, err := resident.SubtreeFromPlan(graph, prefix)
		if err != nil {
			return store.Subtree{}, err
		}
		plans.put(prefix, graph, subtreeSink(subtree))
		return subtree, nil
	}
}

func subtreePrefix() (string, error) {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate subtree id: %w", err)
	}
	return "t" + hex.EncodeToString(random[:]), nil
}

// narratorSystemPrompt keeps progress updates in the agent's own casual
// voice. The reconciler decides when to speak; this decides only how.
const narratorSystemPrompt = `You are aforge, giving the user one casual progress update on work happening in the background. One sentence, two at most. Plain speech in first person, no markdown, no lists, no internal jargon. Name the concrete things that just finished and what is in motion now; mention a duration only when it is notable. Do not repeat anything from your earlier updates, provided below. Never imply the whole job is finished — it is not.`

// narrateProgress wires the reconciler's narration context to the talk model.
func narrateProgress(settings config.Config, client *liveClient) resident.NarrateFunc {
	return func(ctx context.Context, narration resident.Narration) (string, error) {
		var input strings.Builder
		fmt.Fprintf(&input, "The job: %s\n", narration.Goal)
		if len(narration.Finished) > 0 {
			input.WriteString("\nJust finished:\n")
			for _, item := range narration.Finished {
				input.WriteString("- " + item + "\n")
			}
		}
		if len(narration.Running) > 0 {
			input.WriteString("\nIn motion now:\n")
			for _, item := range narration.Running {
				input.WriteString("- " + item + "\n")
			}
		}
		if narration.Queued > 0 {
			fmt.Fprintf(&input, "\nQueued behind them: %d parts\n", narration.Queued)
		}
		if len(narration.Previous) > 0 {
			input.WriteString("\nYour earlier updates (do not repeat):\n")
			for _, line := range narration.Previous {
				input.WriteString("- " + line + "\n")
			}
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "narrate"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: narratorSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input.String()}}},
		}, ai.WithMaxTokens(150))
		if err != nil || response == nil {
			return "", err
		}
		return strings.TrimSpace(response.Text()), nil
	}
}

// jobIDOf resolves the top-level job a node belongs to, which names its
// workspace directory. A resolution failure falls back to the node itself:
// an isolated directory is always safe, a shared one is not.
func jobIDOf(graph *store.Store, node store.Node) string {
	current := node
	for current.Parent != "" && current.Parent != store.RootID {
		parent, ok, err := graph.Node(current.Parent)
		if err != nil || !ok {
			return node.ID
		}
		current = parent
	}
	return current.ID
}

// distillerSystemPrompt writes the notebook. The bar is durability: a memory
// must still matter after this job is forgotten.
const distillerSystemPrompt = `You judge whether a finished job taught an assistant anything worth keeping in its scoped notebook. You receive the goal, the outcome, and whether the job FAILED. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|quirk|lesson|fact","body":"..."}]}.

Judgment framework:
- A memory qualifies only if it will matter after this job is forgotten.
- Scope every memory to the narrowest thing it is about: file:<absolute path> for a file's quirk, repo:<dir> for a codebase-wide one, tool:<name> for a tool's behaviour, domain:<topic> for subject knowledge, user for preferences, or env for machine facts.
- When the job FAILED, the single most valuable memory is the cause and its fix or workaround. Classify it as a quirk or lesson.
- Judge like an after-action review: what was expected, what actually happened, and what explains the gap. The explanation is the memory; the events themselves are not.
- When the direct route failed and a substitute route worked — a different source, tool, or method reached the same end — record the working route as a lesson in the narrowest scope it applies to. A proven detour is the most transferable thing a job can teach.
- Beliefs must stay true as the world moves. When this job's evidence updates, contradicts, or outdates one of the standing numbered entries shown to you, write the corrected memory in full and set "replaces" to that entry's number — the old belief retires when the new one lands. Accumulating a contradiction beside the belief it contradicts is worse than either alone.
- When the job compared approaches — deliberately, or by failing over from one route to another — the comparison's outcome is the memory: record the winner as the standing approach with what decided it, and point "replaces" at any entry that backed the loser. A settled experiment is worth more than either belief that preceded it.
- Each fact object may carry "replaces": <number of the standing entry it supersedes>; omit it otherwise.
- Job status and transient results never qualify.
- An empty list is the common correct answer.
- Return at most five memories.`

const consolidatorSystemPrompt = `You rewrite one scope's accumulated notebook lines into a smaller, sharper notebook. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|quirk|lesson|fact","body":"..."}]}.

Merge duplicates and near-duplicates. Resolve contradictions in favour of the newest line. Keep every load-bearing specific, including paths, values, and names. Each output must stand alone, use exactly the target scope, and preserve the best fitting kind. Return at most eight lines.

Each line carries its age and how often retrieval has used it. Judge staleness by what the claim is about, not by the age alone: a preference or a filesystem quirk ages slowly, while a ranking, a price, a version, or a "current state" claim rots fast. Rewrite fast-rotting claims to name their time ("as of <when>, …") or drop them when their moment has passed; a never-used old line about a moving target is the first candidate to go.

A line may also carry the evidence it was distilled from: the job that taught it, in the words it was asked and what it actually delivered. Weigh lines by that evidence. A claim its own evidence does not support — broader than the one job it came from, or contradicted by what that job delivered — is the first to drop, ahead of anything merely old. When two lines compete and their evidence cannot settle which is right, do not pick: keep both, rewritten as one explicitly competing pair ("X worked for A; Y worked for B — unsettled"), so a future job settles it on evidence instead of a coin flip here.`

// reflectorSystemPrompt is the retrospective an effective employee runs on
// their own work: not what any single job taught — the distiller owns that —
// but what only the series reveals.
const reflectorSystemPrompt = `You are an assistant's periodic retrospective over its recent jobs. You receive the jobs newest first: what was asked in the user's own words, what was delivered, and how long ago. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|lesson|fact","body":"...","replaces":0}]}.

Look only for what the SERIES shows and no single job could:
- A need that keeps recurring — the user comes back for the same kind of thing. Record who the user is and what they regularly want, so future work anticipates it.
- A correction that repeats — successive asks that rework the same aspect of earlier deliveries reveal a standard the user holds and the work keeps missing. Record the standard.
- An approach that consistently worked, or consistently cost too much, across several jobs of the same shape. Record the pattern with what made it work or fail.

The bar for a pattern is at least two independent occurrences; one job is an anecdote and the distiller already handled it. Scope user for who the user is and what they recurrently want; domain:<topic> for proven approaches. One sharp sentence each, at most four, and an empty list is the common correct answer.`

func reflectAcrossJobs(settings config.Config, client *liveClient) resident.ReflectFunc {
	return func(ctx context.Context, jobs []resident.JobSketch) ([]resident.Learned, error) {
		var input strings.Builder
		input.WriteString("Recent jobs, newest first:\n")
		for index, job := range jobs {
			title := job.Title
			if title == "" {
				title = firstLine(job.Ask)
			}
			fmt.Fprintf(&input, "\n%d. %s (%s)\nasked: %s\ndelivered: %s\n", index+1, title, job.Age, job.Ask, job.Outcome)
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "reflect"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: reflectorSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input.String()}}},
		}, ai.WithMaxTokens(500))
		if err != nil || response == nil {
			return nil, err
		}
		return parseLearnedFacts(response.Text(), 4), nil
	}
}

// distillFacts wires the reconciler's notebook to the talk model.
// titleGoalPrompt earns its own call by what it is not: not a summary, not a
// restatement, a NAME. The rail has ~30 characters per node; a name that
// needs the brief to be understood has failed.
const titleGoalPrompt = `You name jobs for a narrow task list. Given a goal, answer with ONLY a name of 3 to 5 words — no quotes, no punctuation at the end, no explanation.

Judge a good name by one test: someone who asked for this work yesterday must recognise it at a glance among unrelated jobs. Prefer the distinctive noun over the generic verb — "Mahabharata nighttime podcast" beats "Create audio content", "Org-wide star count" beats "Gather repository data". Never use the words task, job, request, or goal.`

// titleGoal compresses a job's goal to a rail-sized display name with one
// tiny model call at splice time — chat-surface only, and only for the one
// root node per job that would otherwise show a paragraph.
func titleGoal(settings config.Config, client *liveClient) resident.TitleFunc {
	return func(ctx context.Context, goal string) (string, error) {
		response, err := client.CompleteWithMessages(settings.Context(ctx, "title"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: titleGoalPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: firstLine(goal)}}},
		}, ai.WithMaxTokens(30))
		if err != nil || response == nil {
			return "", err
		}
		return strings.Trim(strings.TrimSpace(response.Text()), `"'`), nil
	}
}

func distillFacts(settings config.Config, client *liveClient, graph *store.Store) resident.DistillFunc {
	return func(ctx context.Context, goal, outcome string, failed bool) ([]resident.Learned, error) {
		input := fmt.Sprintf("Goal:\n%s\n\nFAILED: %t\n\nOutcome:\n%s", goal, failed, outcome)
		// Reconsolidation: the distiller sees the standing beliefs its new
		// evidence might touch, numbered, so a memory that updates one can
		// retire it instead of accumulating beside it.
		if related, err := graph.SearchFacts(store.FactQuery{
			Cues:  resident.ExtractCues(goal + "\n" + outcome),
			Terms: goal,
			Limit: 10,
		}); err == nil && len(related) > 0 {
			var standing strings.Builder
			now := time.Now()
			for _, fact := range related {
				fmt.Fprintf(&standing, "#%d [%s · %s · %s] %s\n", fact.Seq, fact.Scope, fact.Kind, store.AgeLabel(fact.Time, now), fact.Body)
			}
			input += "\n\nStanding notebook entries this job's evidence may touch:\n" + standing.String()
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "distill"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: distillerSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input}}},
		}, ai.WithMaxTokens(500))
		if err != nil || response == nil {
			return nil, err
		}
		return parseLearnedFacts(response.Text(), 5), nil
	}
}

// consolidateFacts sharpens a crowded scope without losing the specifics
// that made its entries worth retaining.
func consolidateFacts(settings config.Config, client *liveClient, graph *store.Store) resident.ConsolidateFunc {
	return func(ctx context.Context, scope string, facts []store.Fact) ([]resident.Learned, error) {
		ordered := append([]store.Fact(nil), facts...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return ordered[i].Seq > ordered[j].Seq
		})

		var input strings.Builder
		now := time.Now()
		fmt.Fprintf(&input, "Target scope: %s\n\nNotebook lines, newest first:\n", scope)
		for index, fact := range ordered {
			fmt.Fprintf(&input, "%d. [%s · %s · used %d×] %s\n", index+1, fact.Kind, store.AgeLabel(fact.Time, now), fact.Uses, fact.Body)
			// Belief audit: every fact names the job that taught it and that
			// job is still in the graph, so a line can be weighed against the
			// evidence it was distilled from rather than against its own
			// confident wording. The root node is the thread itself and
			// carries no single ask worth citing.
			if fact.NodeID == "" || fact.NodeID == store.RootID {
				continue
			}
			source, found, err := graph.Node(fact.NodeID)
			if err != nil || !found {
				continue
			}
			var evidence []string
			if ask := clip(firstLine(strings.TrimSpace(source.Provenance.Intent)), 120); ask != "" {
				evidence = append(evidence, "asked: "+ask)
			}
			// FoldDigest stands in for folded jobs, whose Summary is gone.
			delivery := firstNonEmptyString(source.Summary, source.FoldDigest)
			if delivered := clip(firstLine(strings.TrimSpace(delivery)), 160); delivered != "" {
				evidence = append(evidence, "delivered: "+delivered)
			}
			if len(evidence) == 0 {
				continue
			}
			fmt.Fprintf(&input, "   evidence — %s\n", strings.Join(evidence, " · "))
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "consolidate"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: consolidatorSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input.String()}}},
		}, ai.WithMaxTokens(700))
		if err != nil || response == nil {
			return nil, err
		}
		return parseLearnedFacts(response.Text(), 8), nil
	}
}

func parseLearnedFacts(raw string, limit int) []resident.Learned {
	if limit <= 0 {
		return nil
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return nil
	}
	var parsed struct {
		Facts []struct {
			Scope    string         `json:"scope"`
			Kind     store.FactKind `json:"kind"`
			Body     string         `json:"body"`
			Replaces int64          `json:"replaces"`
		} `json:"facts"`
	}
	if err := json.NewDecoder(strings.NewReader(raw[start:])).Decode(&parsed); err != nil {
		return nil
	}
	learned := make([]resident.Learned, 0, min(limit, len(parsed.Facts)))
	for _, fact := range parsed.Facts {
		fact.Scope = strings.TrimSpace(fact.Scope)
		fact.Body = strings.TrimSpace(fact.Body)
		if fact.Scope == "" || fact.Body == "" || !validLearnedKind(fact.Kind) {
			continue
		}
		learned = append(learned, resident.Learned{Scope: fact.Scope, Kind: fact.Kind, Body: fact.Body, Replaces: fact.Replaces})
		if len(learned) == limit {
			break
		}
	}
	return learned
}

func validLearnedKind(kind store.FactKind) bool {
	switch kind {
	case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain:
		return true
	default:
		return false
	}
}

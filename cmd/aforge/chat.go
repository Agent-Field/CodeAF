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

	compiler := head.NewCompiler(chatClient)
	reconciler := resident.New(graph,
		func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
			brief, err := compiler.Compile(settings.Context(ctx, instruction), instruction, graphContext)
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
		planSubtree(settings, taskClient),
	).WithNarrator(narrateProgress(settings, chatClient)).
		WithDistiller(distillFacts(settings, chatClient)).
		WithConsolidator(consolidateFacts(settings, chatClient))

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
		linear := exec.NewLinear(taskClient, jobSpace, web, 0, 0, 0)

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
		outcome, err := linear.Run(settings.ExecContext(ctx), exec.Task{
			NodeID: int(node.CreatedSeq),
			Title:  firstLine(node.Brief),
			Goal:   node.Provenance.Intent,
			Brief:  node.Brief,
			Inputs: inputs,
		})
		if err != nil {
			return resident.ExecResult{}, err
		}
		text := outcome.Text
		// Artifact paths come back workspace-relative; the user's next act is
		// opening the file, so the summary carries where it actually lives.
		if len(outcome.Artifacts) > 0 {
			text += "\n\nFiles:"
			for _, artifact := range outcome.Artifacts {
				text += "\n" + filepath.Join(jobDir, artifact)
			}
		}
		return resident.ExecResult{
			Summary:          text,
			PromptTokens:     outcome.Usage.PromptTokens,
			CompletionTokens: outcome.Usage.CompletionTokens,
			Cost:             outcome.Usage.Cost,
		}, nil
	}, "chat-runner", 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var background sync.WaitGroup
	background.Add(3)
	go func() { defer background.Done(); _ = head.New(chatClient, graph).Serve(ctx) }()
	go func() { defer background.Done(); _ = reconciler.Serve(ctx) }()
	go func() { defer background.Done(); _ = runner.Serve(ctx) }()

	commander := &chatCommander{
		settings:   settings,
		database:   path,
		prefsDir:   filepath.Dir(path),
		chatClient: chatClient,
		taskClient: taskClient,
		store:      graph,
		prefs:      prefs,
		sessionID:  *sessionID,
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
	settings config.Config
	database string
	prefsDir string

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
func planSubtree(settings config.Config, client *liveClient) resident.PlanFunc {
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
		return resident.SubtreeFromPlan(graph, prefix)
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
- Job status and transient results never qualify.
- An empty list is the common correct answer.
- Return at most five memories.`

const consolidatorSystemPrompt = `You rewrite one scope's accumulated notebook lines into a smaller, sharper notebook. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|quirk|lesson|fact","body":"..."}]}.

Merge duplicates and near-duplicates. Drop stale lines. Resolve contradictions in favour of the newest line. Keep every load-bearing specific, including paths, values, and names. Each output must stand alone, use exactly the target scope, and preserve the best fitting kind. Return at most eight lines.`

// distillFacts wires the reconciler's notebook to the talk model.
func distillFacts(settings config.Config, client *liveClient) resident.DistillFunc {
	return func(ctx context.Context, goal, outcome string, failed bool) ([]resident.Learned, error) {
		input := fmt.Sprintf("Goal:\n%s\n\nFAILED: %t\n\nOutcome:\n%s", goal, failed, outcome)
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
func consolidateFacts(settings config.Config, client *liveClient) resident.ConsolidateFunc {
	return func(ctx context.Context, scope string, facts []store.Fact) ([]resident.Learned, error) {
		ordered := append([]store.Fact(nil), facts...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return ordered[i].Seq > ordered[j].Seq
		})

		var input strings.Builder
		fmt.Fprintf(&input, "Target scope: %s\n\nNotebook lines, newest first:\n", scope)
		for index, fact := range ordered {
			fmt.Fprintf(&input, "%d. [%s] %s\n", index+1, fact.Kind, fact.Body)
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
			Scope string         `json:"scope"`
			Kind  store.FactKind `json:"kind"`
			Body  string         `json:"body"`
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
		learned = append(learned, resident.Learned{Scope: fact.Scope, Kind: fact.Kind, Body: fact.Body})
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

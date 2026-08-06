package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/head"
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
	workspace, err := exec.NewWorkspace(workspaceRoot)
	if err != nil {
		return err
	}

	compiler := head.NewCompiler(chatClient)
	reconciler := resident.New(graph,
		func(ctx context.Context, instruction, graphContext string) (string, []string, error) {
			brief, err := compiler.Compile(settings.Context(ctx, instruction), instruction, graphContext)
			if err != nil {
				return "", nil, err
			}
			return brief.Goal, brief.Assumptions, nil
		},
		nil, // default single-node plan; the full planner adapter is next
	)

	linear := exec.NewLinear(taskClient, workspace, exec.NewWeb(), 0, 0, 0)
	runner := resident.NewRunner(graph, func(ctx context.Context, node store.Node) (string, error) {
		inputs := make([]exec.Input, 0)
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
			return "", err
		}
		return outcome.Text, nil
	}, "chat-runner", 2)

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

func (c *chatCommander) DatabasePath() string { return c.database }

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

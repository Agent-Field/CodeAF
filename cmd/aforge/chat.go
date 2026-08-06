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
	"unicode/utf8"

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
	// Planning is done by the working model, so its measured ruler must be in
	// force before either the initial subtree planner or an overrun replan runs.
	measured, _ := profile.Load(settings.ProfileDir, taskClient.Model(), "linear")
	plan.UseAnchors(measured.Anchors)

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
			if sk := selfKnowledge(settings, taskClient.Model()); sk != "" {
				augmented += "\n\nMeasured execution costs (this system's own measured history):\n" + sk
			}
			brief, err := compiler.Compile(settings.Context(ctx, instruction), instruction, augmented)
			if err != nil {
				return resident.Compiled{}, err
			}
			return resident.Compiled{
				Goal:            brief.Goal,
				Assumptions:     brief.Assumptions,
				Scale:           brief.Scale,
				TrialOf:         brief.TrialOf,
				BuildsOn:        brief.BuildsOn,
				Question:        brief.Question,
				QuestionOptions: brief.QuestionOptions,
				Charter:         brief.Charter,
			}, nil
		},
		planSubtree(settings, taskClient, plans, graph),
	).WithNarrator(narrateProgress(settings, chatClient, graph)).
		WithDistiller(distillFacts(settings, chatClient, graph)).
		WithConsolidator(consolidateFacts(settings, chatClient, graph)).
		WithTitler(titleGoal(settings, chatClient)).
		WithReflector(reflectAcrossJobs(settings, chatClient, graph)).
		WithTerritoryDigester(digestTerritory(settings, chatClient)).
		WithOverrunPlanner(settings.DailyBudgetUSD, replanRemainder(settings, taskClient, plans, graph))

	web := exec.NewWeb()
	runner := resident.NewRunner(graph, func(ctx context.Context, node store.Node) (resident.ExecResult, error) {
		isReflex := node.Group == resident.ReflexGroup
		// Each top-level job works in its own directory: one thread hosts
		// many unrelated jobs, and continuity between them travels through
		// the graph as digests and absolute paths, never through a shared
		// folder they could trample.
		jobDir := filepath.Join(workspaceRoot, jobIDOf(graph, node))
		jobSpace, err := exec.NewWorkspace(jobDir)
		if err != nil {
			return resident.ExecResult{}, err
		}
		// A planned job keeps one executor for every leaf so its profile key names
		// the model that actually produced all measured turns. A picker change
		// applies to the next job rather than relabeling work already in flight.
		planGraph, planNode, workingModel, workingClient := plans.lookup(node.ID)
		if workingClient == nil {
			workingModel, workingClient = taskClient.Snapshot()
		}
		// Ordinary leaves retain the byte-identical headless envelope. Reflexes use
		// the deliberately tiny rung budget and a seconds-scale watchdog.
		turns, tokens := chatLeafTurns, chatLeafTokens
		deadline := leafDeadline(tokens)
		watchdog := deadline + 2*time.Minute
		if isReflex {
			turns, tokens = reflexTurns, reflexTokens
			deadline = reflexDeadline
			watchdog = deadline + 15*time.Second
		}
		linear := exec.NewLinear(workingClient, jobSpace, web, turns, tokens, deadline).WithStore(graph)
		shape := "atomic"
		if isReflex {
			shape = "reflex"
		}
		if planNode != nil {
			shape = exec.LeafShape(planNode)
			// Frozen means frozen everywhere: the sentinel may not edit a
			// node whose transcript is already being written.
			plans.markRunning(planNode)
		}

		inputs := make([]exec.Input, 0)
		if digest := resident.NotebookDigest(graph, node.ID, node.Brief, node.Provenance.Intent, 8); digest != "" {
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
			Reflex: isReflex,
			NodeID: int(node.CreatedSeq),
			Title:  firstLine(node.Brief),
			Goal:   node.Provenance.Intent,
			Brief:  residentDeliveryBrief(graph, node),
			Inputs: inputs,
			Steer:  steer,
		}
		// The scheduler's quality loop, inline: each attempt is one routable
		// unit carrying its call shape, a watchdog sits above the leaf's own
		// deadline so a wedged executor becomes a recorded failure rather
		// than a silent hang, and a leaf whose verdict says a stronger model
		// might fix it gets exactly one escalation when a panel offers one.
		attempts := 1
		if !isReflex && taskClient.escalatable() {
			attempts = 2
		}
		// One job is one cache lineage, exactly as one headless run is: the
		// affinity key rides every leaf of the job so a prefix cache warmed
		// by one worker serves its siblings.
		ctx = provider.WithCacheKey(ctx, provider.RunCacheKey(node.Provenance.Intent, workingModel))
		var outcome *exec.Outcome
		var spent exec.Usage
		spentTurns := 0
		workerModel := taskClient.Model()
		for attempt := 0; attempt < attempts; attempt++ {
			runCtx := provider.WithCallShape(settings.ExecContext(ctx), provider.ClassExecLeaf, attempt, shape)
			outcome, err = runLeafWithWatchdog(runCtx, linear, task, watchdog)
			if model := provider.CallFrom(runCtx).Model(); model != "" {
				workerModel = model
			}
			if outcome != nil {
				spent.PromptTokens += outcome.Usage.PromptTokens
				spent.CompletionTokens += outcome.Usage.CompletionTokens
				spent.Cost += outcome.Usage.Cost
				spentTurns += outcome.Turns
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
		if !isReflex && planGraph != nil && outcome != nil {
			plans.reviseAfter(ctx, settings, taskClient, graph, node, planGraph, outcome.Text, err != nil, workerModel)
		}
		if err != nil {
			// Preserve failed-attempt evidence even though no delivery reaches the
			// gate. This is the pre-existing profile path, kept on the early return.
			if landed := plans.takeIfRoot(node.ID); landed != nil {
				prefix := node.ID[:strings.LastIndex(node.ID, "-n")]
				go func() {
					_, records := recordAndCalibrateDetailed(settings.Context(context.Background(), landed.Goal), workingClient, settings, workingModel, landed)
					recordPlanSurprises(graph, prefix, records)
				}()
			} else if planGraph == nil && node.Parent == store.RootID && outcome != nil {
				if isReflex {
					go func() {
						record, ok := recordReflex(settings, workerModel, node, outcome, false)
						if ok {
							recordProfileSurprise(graph, node.ID, record)
						}
					}()
				} else {
					go func() {
						record, ok := recordSingleLeaf(settings, workerModel, node, outcome)
						if ok {
							recordProfileSurprise(graph, node.ID, record)
						}
					}()
				}
			}
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
		continuing := false
		// Resource exhaustion is invisible: it grows the graph and the final
		// assembled deliverable reaches the gate. Semantic failure stays honest
		// and still lands with the evidence from the failing leaf.
		if !isReflex && (outcome.Stop == exec.StopBudget || outcome.Stop == exec.StopTurnCap) {
			spliced, _, replanErr := resident.ReplanOverrun(ctx, graph, node, outcome.Text, absolute,
				settings.DailyBudgetUSD, replanRemainder(settings, taskClient, plans, graph))
			if replanErr == nil && spliced > 0 {
				continuing = true
				text += "\n\n[" + continuationMessage(spliced) + "]"
				_, _ = graph.PostMessage(store.Message{
					SessionID: node.Provenance.SessionID,
					Role:      store.RoleSystem,
					NodeID:    node.ID,
					Body:      continuationMessage(spliced),
				})
			} else if replanErr == nil {
				// A zero splice at the rail is a pause, not a final partial. The
				// question and deferred remainder are journaled; the reconciler
				// resumes the split after the head records consent.
				if rail, err := graph.DailyRailToday(settings.DailyBudgetUSD); err == nil {
					continuing = rail.Reached
				}
			}
		}
		promoted := shouldPromoteReflex(node, outcome)
		// The quality gate: before a deliverable lands in the thread, one
		// judge call asks the only question that matters — would the person
		// who asked accept this as done? A named gap earns exactly one
		// revision pass with the critique as input; then the result ships
		// either way, because a gate that can loop is a gate that can stall.
		if shouldGate(node, outcome, continuing) {
			gate := judgeDeliverable(ctx, settings, taskClient, graph, node, text, workerModel)
			if gate.Checked {
				evidence := store.DeliveryGate{Pass: gate.Pass, Gap: gate.Gaps}
				if gate.Pass {
					outcome.Verdict = provider.VerdictVerifiedSuccess
				}
				if !gate.Pass {
					revision := task
					revision.Inputs = append(append([]exec.Input{}, inputs...), exec.Input{
						Result: "A reviewer compared the previous attempt against the original request and found gaps that must be closed:\n" + gate.Gaps +
							"\n\nThe previous attempt (build on it, fix the gaps, do not start over):\n" + text,
					})
					retryCtx := provider.WithCallShape(settings.ExecContext(ctx), provider.ClassExecLeaf, 1, shape)
					polished, polishErr := runLeafWithWatchdog(retryCtx, linear, revision, deadline+2*time.Minute)
					polishModel := workerModel
					if model := provider.CallFrom(retryCtx).Model(); model != "" {
						polishModel = model
					}
					if polishErr == nil && polished != nil && strings.TrimSpace(polished.Text) != "" {
						spent.PromptTokens += polished.Usage.PromptTokens
						spent.CompletionTokens += polished.Usage.CompletionTokens
						spent.Cost += polished.Usage.Cost
						spentTurns += polished.Turns
						outcome = polished
						workerModel = polishModel
						text = polished.Text
						absolute = absolute[:0]
						for _, artifact := range polished.Artifacts {
							absolute = append(absolute, filepath.Join(jobDir, artifact))
						}
						if len(absolute) > 0 {
							text += "\n\nFiles:\n" + strings.Join(absolute, "\n")
						}
						closed := judgeDeliverable(ctx, settings, taskClient, graph, node, text, polishModel)
						evidence.PolishClosed = closed.Checked && closed.Pass
						outcome.Verdict = provider.VerdictSemanticFailure
						if evidence.PolishClosed {
							outcome.Verdict = provider.VerdictVerifiedSuccess
						}
						message := "a review found gaps in the first draft — revised before delivering: " + firstLine(gate.Gaps)
						if !evidence.PolishClosed {
							message += " (the follow-up gate did not confirm the gap was closed)"
						}
						_, _ = graph.PostMessage(store.Message{
							SessionID: node.Provenance.SessionID,
							Role:      store.RoleSystem,
							NodeID:    node.ID,
							Body:      message,
						})
					} else {
						outcome.Verdict = provider.VerdictSemanticFailure
					}
				}
				_ = graph.RecordDeliveryGate(node.ID, evidence)
			}
		}
		outcome.Text = text
		outcome.Usage = spent
		outcome.Turns = spentTurns
		if planNode != nil {
			plans.recordOutcome(planNode, outcome, nil)
		}
		if landed := plans.takeIfRoot(node.ID); landed != nil {
			// The recalibration report reaches the thread, not a stdout the TUI
			// owns; detached, because the ruler is telemetry and the user's
			// result must not wait on it.
			sessionID := node.Provenance.SessionID
			prefix := node.ID[:strings.LastIndex(node.ID, "-n")]
			go func() {
				report, records := recordAndCalibrateDetailed(settings.Context(context.Background(), landed.Goal), workingClient, settings, workingModel, landed)
				recordPlanSurprises(graph, prefix, records)
				if strings.TrimSpace(report) == "" {
					return
				}
				_, _ = graph.PostMessage(store.Message{
					SessionID: sessionID,
					Role:      store.RoleSystem,
					NodeID:    node.ID,
					Body:      report,
				})
			}()
		} else if planGraph == nil && node.Parent == store.RootID {
			if isReflex {
				go func() {
					record, ok := recordReflex(settings, workerModel, node, outcome, promoted)
					if ok {
						recordProfileSurprise(graph, node.ID, record)
					}
				}()
			} else {
				go func() {
					record, ok := recordSingleLeaf(settings, workerModel, node, outcome)
					if ok {
						recordProfileSurprise(graph, node.ID, record)
					}
				}()
			}
		}
		return resident.ExecResult{
			Summary:          text,
			PromptTokens:     spent.PromptTokens,
			CompletionTokens: spent.CompletionTokens,
			Cost:             spent.Cost,
			Promote:          promoted,
		}, nil
	}, "chat-runner", 4).WithDailyBudgetUSD(settings.DailyBudgetUSD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var background sync.WaitGroup
	streamEvents := make(chan tui.StreamEvent, 256)
	background.Add(3)
	// Routing is a structuring call, and it was the one loop served with a bare
	// context: without the configured effort knob, a reasoning model spends the
	// head's whole token cap deliberating and returns empty text — measured as
	// 600/600 completion tokens of thought and zero answer on the default model.
	go func() {
		defer background.Done()
		defer close(streamEvents)
		headContext := provider.WithStreamObserver(settings.Context(ctx, "head"), func(event provider.StreamEvent) {
			translated := tui.StreamEvent{Delta: event.Delta}
			switch event.Kind {
			case provider.StreamStarted:
				translated.Kind = tui.StreamStarted
			case provider.StreamDelta:
				translated.Kind = tui.StreamDelta
			case provider.StreamFinished:
				translated.Kind = tui.StreamFinished
			case provider.StreamFailed:
				translated.Kind = tui.StreamFailed
			}
			select {
			case streamEvents <- translated:
			case <-ctx.Done():
			}
		})
		_ = head.New(chatClient, graph).
			WithSelfKnowledge(func() string { return selfKnowledge(settings, taskClient.Model()) }).
			WithDailyBudgetUSD(settings.DailyBudgetUSD).
			Serve(headContext)
	}()
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
		streamEvents:  streamEvents,
	}
	err = tui.RunWithCommander(graph, *sessionID, commander)
	cancel()
	waitWithGrace(&background, 5*time.Second)
	return err
}

// residentDeliveryBrief gives only the top-level deliverable owner the voice
// contract. Planned synthesis and direct jobs share this path; child results
// remain worker-to-worker material. A gate repair copies this same task, so its
// one polish pass cannot drift to a different voice.
func residentDeliveryBrief(graph *store.Store, node store.Node) string {
	if node.Parent != store.RootID {
		return node.Brief
	}
	return resident.VoicePrompt(graph, node.Brief, node.Provenance.Intent, node.Brief)
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

	mu           sync.Mutex
	prefs        chatPrefs
	sessionID    string
	streamEvents <-chan tui.StreamEvent

	catalogOnce sync.Once
	catalog     []tui.ModelChoice
}

func (c *chatCommander) StreamEvents() <-chan tui.StreamEvent { return c.streamEvents }

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
		// A model switch changes the capability being sized; install that model's
		// own ruler before the next planning call can observe the new client.
		measured, _ := profile.Load(c.settings.ProfileDir, c.taskClient.Model(), "linear")
		plan.UseAnchors(measured.Anchors)
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

// liveClient is a model-switchable completion client. Long-running leaves take
// a snapshot so one measurement has one model; structuring consumers hold this
// handle directly, so a swap takes effect on their next call.
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

// Snapshot returns a model and client from the same instant, which keeps the
// profile key and the executor it describes inseparable.
func (l *liveClient) Snapshot() (string, router.Client) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.model, l.client
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

func (l *liveClient) routed() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.client.(*router.Router)
	return ok
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

func clipUTF8Bytes(value string, limit int) string {
	if limit <= 3 || len(value) <= limit {
		return value
	}
	cut := limit - 3
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
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

	// A reflex gets four exchanges and one eighth of a normal chat leaf's
	// token allowance: enough to use a tool and report its result, but small
	// enough that ambiguous work promotes before impersonating a full job.
	reflexTurns    = 4
	reflexTokens   = chatLeafTokens / 8
	reflexDeadline = 90 * time.Second
)

func continuationMessage(pieces int) string {
	return resident.OverrunContinuationMessage(pieces)
}

// shouldGate keeps the delivery ceremony off the reflex rung. A promoted
// partial is evidence for the compiled job, not a deliverable to review.
func shouldGate(node store.Node, outcome *exec.Outcome, continuing bool) bool {
	return !continuing && node.Group != resident.ReflexGroup &&
		node.Parent == store.RootID && outcome != nil
}

func shouldPromoteReflex(node store.Node, outcome *exec.Outcome) bool {
	return node.Group == resident.ReflexGroup && outcome != nil &&
		(outcome.Promote || outcome.Stop == exec.StopBudget || outcome.Stop == exec.StopTurnCap)
}

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
	graph  *plan.Graph
	root   string
	model  string
	client router.Client
}

func (j *jobPlans) put(prefix string, graph *plan.Graph, root, model string, client router.Client) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.graphs[prefix] = plannedJob{graph: graph, root: root, model: model, client: client}
}

// lookup resolves a store node id back to its plan node. The job root uses the
// bare prefix so planning messages can name it before admission; other nodes
// retain "<prefix>-n<planID>".
func (j *jobPlans) lookup(nodeID string) (*plan.Graph, *plan.Node, string, router.Client) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if entry, ok := j.graphs[nodeID]; ok {
		if len(entry.graph.Nodes) == 0 {
			return entry.graph, nil, entry.model, entry.client
		}
		return entry.graph, &entry.graph.Nodes[len(entry.graph.Nodes)-1], entry.model, entry.client
	}

	cut := strings.LastIndex(nodeID, "-n")
	if cut < 0 {
		return nil, nil, "", nil
	}
	planID, err := strconv.Atoi(nodeID[cut+2:])
	if err != nil {
		return nil, nil, "", nil
	}
	entry, ok := j.graphs[nodeID[:cut]]
	if !ok {
		return nil, nil, "", nil
	}
	for index := range entry.graph.Nodes {
		if entry.graph.Nodes[index].ID == planID {
			return entry.graph, &entry.graph.Nodes[index], entry.model, entry.client
		}
	}
	return entry.graph, nil, entry.model, entry.client
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
	j.mu.Lock()
	defer j.mu.Unlock()
	if entry, ok := j.graphs[nodeID]; ok && entry.root == nodeID {
		delete(j.graphs, nodeID)
		return entry.graph
	}

	cut := strings.LastIndex(nodeID, "-n")
	if cut < 0 {
		return nil
	}
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
func (j *jobPlans) reviseAfter(ctx context.Context, settings config.Config, client *liveClient, graph *store.Store, node store.Node, planGraph *plan.Graph, summary string, failed bool, workerModel string) {
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
	judgeCtx := router.WithAvoidModel(ctx, workerModel)
	operations, _, err := plan.Revise(settings.Context(judgeCtx, planGraph.Goal), client, planGraph,
		resident.RevisionEvent(node, summary, failed))
	if err != nil || len(operations) == 0 {
		return
	}
	applied, notes := resident.ApplyRevision(graph, planGraph, prefix, entry.root, operations)
	if len(notes) > 0 {
		_, _ = graph.PostMessage(store.Message{
			SessionID: node.Provenance.SessionID,
			Role:      store.RoleSystem,
			NodeID:    node.ID,
			Body:      "revision sentinel refusals after " + fmt.Sprintf("%q", firstLine(nodeDisplay(node))) + ":\n" + strings.Join(notes, "\n"),
		})
	}
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
		NodeID:    node.ID,
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

var judgeDeliverableSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pass": {"type": "boolean"},
    "gaps": {"type": "string"}
  },
  "required": ["pass"],
  "additionalProperties": false
}`)

type deliverableJudgment struct {
	Pass    bool
	Gaps    string
	Checked bool
}

const gateNotebookBytes = 1 << 10

// judgeDeliverable returns a checked pass or named gap. Every failure of the
// gate itself remains fail-open: Checked is false, so it neither blocks delivery
// nor manufactures verified evidence for the profile.
func judgeDeliverable(ctx context.Context, settings config.Config, client *liveClient, graph *store.Store, node store.Node, deliverable, workerModel string) deliverableJudgment {
	ask := node.Provenance.Intent
	body := "Verbatim request:\n" + ask + "\n\nCompiled goal:\n" + node.Brief + "\n\nDeliverable as produced:\n" + deliverable
	if digest := resident.NotebookDigest(graph, node.ID, node.Brief, ask, 8); digest != "" {
		body += "\n\nStanding preferences and relevant lessons:\n" + clipUTF8Bytes(digest, gateNotebookBytes)
	}
	judgeCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "gate")
	judgeCtx = provider.WithCall(judgeCtx, provider.ClassPlanAudit)
	options := []ai.Option{ai.WithMaxTokens(400)}
	// Structured output is the cascade's free verifier. Keep the no-panel
	// adapter's request options unchanged; there is no second rung to unlock.
	if client.routed() {
		options = append(options, ai.WithSchema(judgeDeliverableSchema))
	}
	response, err := client.CompleteWithMessages(judgeCtx, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: judgeDeliverablePrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body}}},
	}, options...)
	if err != nil || response == nil {
		provider.Report(judgeCtx, provider.VerdictProviderFailure)
		return deliverableJudgment{Pass: true}
	}
	text := strings.TrimSpace(response.Text())
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		provider.Report(judgeCtx, provider.VerdictFormatFailure)
		return deliverableJudgment{Pass: true}
	}
	var verdict struct {
		Pass bool   `json:"pass"`
		Gaps string `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &verdict); err != nil {
		provider.Report(judgeCtx, provider.VerdictFormatFailure)
		return deliverableJudgment{Pass: true}
	}
	if verdict.Pass {
		provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
		return deliverableJudgment{Pass: true, Checked: true}
	}
	gaps := strings.TrimSpace(verdict.Gaps)
	if gaps == "" {
		provider.Report(judgeCtx, provider.VerdictSemanticFailure)
		return deliverableJudgment{Pass: true}
	}
	provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
	return deliverableJudgment{Gaps: gaps, Checked: true}
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

// recordSingleLeaf keeps direct-job costs available to compiler self-knowledge
// without pretending an unplanned task was atomic ruler evidence.
func recordSingleLeaf(settings config.Config, model string, node store.Node, outcome *exec.Outcome) (profile.Record, bool) {
	if strings.TrimSpace(model) == "" {
		model = settings.Model
	}
	measured, err := profile.Load(settings.ProfileDir, model, "linear")
	if err != nil {
		return profile.Record{}, false
	}
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = firstLine(node.Brief)
	}
	added := measured.Add(profile.Record{
		Title:   title,
		Summary: firstLine(node.Brief),
		Size:    profile.BucketDirect,
		Turns:   outcome.Turns,
		Tokens:  outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens,
		Stop:    string(outcome.Stop),
		Verdict: outcome.Verdict,
	})
	if len(added) == 0 || measured.Save() != nil {
		return profile.Record{}, false
	}
	return added[0], true
}

// recordReflex learns the boundary independently from the planner's ruler.
// Promotions remain observations, including the cost of the useful partial.
func recordReflex(settings config.Config, model string, node store.Node, outcome *exec.Outcome, promoted bool) (profile.Record, bool) {
	if strings.TrimSpace(model) == "" {
		model = settings.Model
	}
	measured, err := profile.Load(settings.ProfileDir, model, "linear")
	if err != nil {
		return profile.Record{}, false
	}
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = firstLine(node.Brief)
	}
	added := measured.Add(profile.Record{
		Title:    title,
		Summary:  firstLine(node.Brief),
		Size:     profile.BucketReflex,
		Turns:    outcome.Turns,
		Tokens:   outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens,
		Stop:     string(outcome.Stop),
		Cost:     outcome.Usage.Cost,
		Promoted: promoted,
		Verdict:  outcome.Verdict,
	})
	if len(added) == 0 || measured.Save() != nil {
		return profile.Record{}, false
	}
	return added[0], true
}

func recordPlanSurprises(graph *store.Store, prefix string, records []landedProfileRecord) {
	for _, landed := range records {
		recordProfileSurprise(graph, fmt.Sprintf("%s-n%d", prefix, landed.planID), landed.record)
	}
}

func recordProfileSurprise(graph *store.Store, nodeID string, record profile.Record) {
	if record.Surprise == nil || record.ExpectedTokens == nil {
		return
	}
	_ = graph.RecordSurprise(store.NodeSurprise{
		NodeID:         nodeID,
		ActualTokens:   record.Tokens,
		ExpectedTokens: *record.ExpectedTokens,
		Surprise:       *record.Surprise,
	})
}

func planSubtree(settings config.Config, client *liveClient, plans *jobPlans, history *store.Store) resident.PlanFunc {
	return func(ctx context.Context, compiled resident.Compiled) (store.Subtree, error) {
		anchor, anchored := resident.PlanAnchorFromContext(ctx)
		prefix := anchor.NodeID
		if !anchored {
			var err error
			prefix, err = subtreePrefix()
			if err != nil {
				return store.Subtree{}, err
			}
		}
		if compiled.Scale != head.ScaleProject {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID:    prefix,
				Brief: compiled.Goal,
				Stage: 1,
			}}}, nil
		}
		workingModel, workingClient := client.Snapshot()
		progress := chatPlanProgress(history, anchor)
		graph, err := plan.Build(settings.Context(ctx, compiled.Goal), workingClient, compiled.Goal, plan.Options{
			Recall:       recallHits(history, compiled.Goal, groundRecallLimit),
			SpineSamples: settings.SpineSamples,
			// One level deeper than the one-shot default: chat projects are
			// where visible fan-out is the product, and the compiler now
			// names the parts for the planner to expand.
			MaxDepth:   settings.MaxDepth + 1,
			NodeBudget: settings.NodeBudget,
			Briefs:     true,
			Progress:   progress,
		})
		if err != nil {
			return store.Subtree{}, err
		}
		// Per-leaf working contracts, exactly as a headless run writes them
		// before dispatch. A contract failure costs specificity, not the job.
		if _, err := plan.Contracts(ctx, workingClient, graph, resident.ContractPlaybook(history), progress); err != nil {
			fmt.Fprintf(os.Stderr, "note: could not write contracts: %v\n", err)
		}
		subtree, err := resident.SubtreeFromPlan(graph, prefix)
		if err != nil {
			return store.Subtree{}, err
		}
		plans.put(prefix, graph, subtreeSink(subtree), workingModel, workingClient)
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
		workingModel, workingClient := client.Snapshot()
		anchor, _ := resident.PlanAnchorFromContext(ctx)
		progress := chatPlanProgress(history, anchor)
		graph, err := plan.Build(settings.Context(ctx, goal), workingClient, goal, plan.Options{
			Recall:       recallHits(history, goal, groundRecallLimit),
			SpineSamples: settings.SpineSamples,
			MaxDepth:     settings.MaxDepth,
			NodeBudget:   settings.NodeBudget,
			Briefs:       true,
			Progress:     progress,
		})
		if err != nil {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID:    prefix,
				Brief: goal,
				Title: "Finish the remainder",
				Stage: 1,
			}}}, nil
		}
		if _, err := plan.Contracts(ctx, workingClient, graph, resident.ContractPlaybook(history), progress); err != nil {
			fmt.Fprintf(os.Stderr, "note: could not write repair contracts: %v\n", err)
		}
		subtree, err := resident.SubtreeFromPlan(graph, prefix)
		if err != nil {
			return store.Subtree{}, err
		}
		plans.put(prefix, graph, subtreeSink(subtree), workingModel, workingClient)
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
func narrateProgress(settings config.Config, client *liveClient, graph *store.Store) resident.NarrateFunc {
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
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: resident.VoicePrompt(graph, narratorSystemPrompt, narration.Goal)}}},
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
const distillerSystemPrompt = `You judge whether a finished job taught an assistant anything worth keeping in its scoped notebook. You receive the goal, the outcome, and whether the job FAILED. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|quirk|lesson|fact|unsettled|skill|playbook","body":"...","unsettled":{"approaches":[{"approach":"...","scope":"...","evidence":[123]},{"approach":"...","scope":"...","evidence":[456]}]},"replaces":0,"skill":{"artifact":"/absolute/path/to/artifact-directory"}}]}.

Judgment framework:
- A memory qualifies only if it will matter after this job is forgotten.
- Scope every memory to the narrowest thing it is about: file:<absolute path> for a file's quirk, repo:<dir> for a codebase-wide one, tool:<name> for a tool's behaviour, domain:<topic> for subject knowledge, user for preferences, or env for machine facts.
- When the job FAILED, the single most valuable memory is the cause and its fix or workaround. Classify it as a quirk or lesson.
- A correction about HOW something was communicated — its length, format, tone, or language — is a voice preference. Emit scope "user", kind "preference", and phrase the body as a direct instruction such as "keep answers short; no preamble", not as a report of this episode.
- Judge like an after-action review: what was expected, what actually happened, and what explains the gap. The explanation is the memory; the events themselves are not.
- When the direct route failed and a substitute route worked — a different source, tool, or method reached the same end — record the working route as a lesson in the narrowest scope it applies to. A proven detour is the most transferable thing a job can teach.
- Emit kind "playbook" when the experience yields one method rule actionable while writing a future job's working contract: what to do, avoid, verify, or try instead. Its body is one self-contained strategy bullet and its scope must be repo:<dir>, tool:<name>, or domain:<topic>. Emit at most one playbook delta per job: add that bullet or supersede one numbered bullet, never rewrite a scope's playbook. A fact about the world that does not change how the work should be done is not a playbook bullet.
- Beliefs must stay true as the world moves. When this job's evidence updates, contradicts, or outdates one of the standing numbered entries shown to you, write the corrected memory in full and set "replaces" to that entry's number — the old belief retires when the new one lands. Accumulating a contradiction beside the belief it contradicts is worse than either alone.
- Make one additional judgment: when the job leaves a reusable procedure it actually used — a script written in the workspace, a repeated command sequence captured as an artifact, or a proven detour — emit one skill memory whose body is a one-line command doc and whose skill.artifact is the absolute path to its command-named directory; that directory must contain executable check.sh plus run.sh or another executable. This only proposes a candidate. If a FAILED job shows a standing skill broke, emit the failure lesson with replaces set to that skill's number so it retires.
- When the job compared approaches — deliberately, or by failing over from one route to another — the comparison's outcome is the memory: record the winner as the standing approach with what decided it, and point "replaces" at any entry that backed the loser. Use kind "playbook" when the winner is a method future work should follow. A settled experiment is worth more than either belief that preceded it.
- When the outcome contains TRIAL VERDICT REQUIRED for unsettled fact #N, consume that pair explicitly. If the evidence settles it, emit the winning lesson, fact, or actionable playbook method with "replaces":N. If it does not, emit kind "unsettled" with "replaces":N and the same two structured approaches and evidence sequences. Never omit the replacement merely because the result was inconclusive.
- Include "unsettled" only for kind "unsettled"; omit it for every ordinary fact.
- Each fact object may carry "replaces": <number of the standing entry it supersedes>; omit it otherwise.
- Job status and transient results never qualify.
- An empty list is the common correct answer.
- Return at most five memories.`

const consolidatorSystemPrompt = `You rewrite one scope's accumulated notebook lines into a smaller, sharper notebook. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|quirk|lesson|fact|unsettled|skill|playbook","body":"...","unsettled":{"approaches":[{"approach":"...","scope":"...","evidence":[123]},{"approach":"...","scope":"...","evidence":[456]}]},"sources":[123,456],"replaces":0},{"quarantines":[456]}],"scope_alias":{"merge":true,"canonical":"domain:example"}}.

Merge duplicates and near-duplicates. Resolve contradictions in favour of the newest line. Keep every load-bearing specific, including paths, values, and names. Each output must stand alone, use exactly the target scope, and preserve the best fitting kind. Return at most eight lines.
An input skill was admitted by execution. Preserve kind skill only when an output derives from a skill input; never turn an ordinary fact into a skill.
An input playbook line is earned contract doctrine. Merge near-duplicate playbook bullets only through the numbered sources/replaces delta operations below, preserve kind playbook, and keep the result actionable at contract-writing time. Never rewrite a playbook wholesale or turn an ordinary fact into one.

Every input line is numbered with its durable notebook number. Each output must name in "sources" every input it derives from, strongest evidence first, and every input must be assigned exactly once: either to one output's "sources" or to one "quarantines" list. The sources are the evidence and supersession map, not citations to invent. When an output corrects a standing line, also set "replaces" to that input's number; omit it or use 0 otherwise. A quarantine-only object needs no scope, kind, or body.

Each line carries its age and how often retrieval has used it. Judge staleness by what the claim is about, not by the age alone: a preference or a filesystem quirk ages slowly, while a ranking, a price, a version, or a "current state" claim rots fast. Rewrite fast-rotting claims to name their time ("as of <when>, …"); a never-used old line about a moving target should survive only as compact, explicitly dated evidence when another input still makes it useful.

A line that rode jobs may carry how many of those jobs ended badly — execution failed, a delivery gate failed, or the work overran. Repeated bad co-occurrence is evidence against the line: quarantine it when the pattern makes preserving it more dangerous than withholding it. Co-occurrence is not causation, so one bad job is never enough; require a repeated pattern across at least two jobs, and keep or cautiously rewrite the line when another explanation remains plausible.

A line may also carry the evidence it was distilled from: the job that taught it, in the words it was asked and what it actually delivered. Weigh lines by that evidence. Strip a claim its own evidence does not support back to only what the evidence establishes; when nothing else survives, merge that dated evidence into the closest output without preserving the unsupported claim. When two lines compete and their evidence cannot settle which is right, do not pick. Emit one kind "unsettled" line with exactly two structured approaches. Each approach names the method, the scope where it worked, and the numbered input fact seqs supporting that side in evidence. For a newly formed pair those evidence seqs are numbered inputs and also appear in sources; when preserving an existing pair, carry its earlier evidence seqs forward. The body is a concise readable projection of the same pair. This structure, not an "— unsettled" prose suffix, is what makes a future job test it.

When one scope-gardening candidate is shown, make exactly one additional judgment in "scope_alias". Merge only when both names mean the same shelf, not merely related subjects: use {"merge":true,"canonical":"<one of the two shown scopes>"} and choose the clearer durable name. Otherwise use {"merge":false,"canonical":""}. Similar spelling earned the comparison, not the merge. Omit "scope_alias" when no candidate is shown.`

// reflectorSystemPrompt is the retrospective an effective employee runs on
// their own work: not what any single job taught — the distiller owns that —
// but what only the series reveals.
const reflectorSystemPrompt = `You are an assistant's periodic retrospective over its recent jobs. You receive the jobs newest first: what was asked in the user's own words, what was delivered, and how long ago. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|lesson|fact","body":"...","replaces":0}]}.

Look only for what the SERIES shows and no single job could:
- A need that keeps recurring — the user comes back for the same kind of thing. Record who the user is and what they regularly want, so future work anticipates it.
- A correction that repeats — successive asks that rework the same aspect of earlier deliveries reveal a standard the user holds and the work keeps missing. Record the standard.
- An approach that consistently worked, or consistently cost too much, across several jobs of the same shape. Record the pattern with what made it work or fail.

Consider the most mispredicted jobs first: where the self-model is most wrong is where the series has the most to teach.

The bar for a pattern is at least two independent occurrences; one job is an anecdote and the distiller already handled it. Scope user for who the user is and what they recurrently want; domain:<topic> for proven approaches. Do not restate a numbered standing notebook entry. When the series corrects or sharpens one, write the replacement in full and set "replaces" to its number. One sharp sentence each, at most four, and an empty list is the common correct answer.`

func reflectAcrossJobs(settings config.Config, client *liveClient, graph *store.Store) resident.ReflectFunc {
	return func(ctx context.Context, jobs []resident.JobSketch) ([]resident.Learned, error) {
		var input strings.Builder
		input.WriteString("Recent jobs, newest first:\n")
		var queryText strings.Builder
		for index, job := range jobs {
			title := job.Title
			if title == "" {
				title = firstLine(job.Ask)
			}
			fmt.Fprintf(&input, "\n%d. %s (%s · %s)\nasked: %s\ndelivered: %s\n", index+1, title, job.Age, job.CostSummary(), job.Ask, job.Outcome)
			queryText.WriteString(job.Ask)
			queryText.WriteByte('\n')
			queryText.WriteString(job.Outcome)
			queryText.WriteByte('\n')
		}
		if related, err := graph.SearchFactsUncounted(store.FactQuery{
			Cues: resident.ExtractCues(queryText.String()), Terms: queryText.String(), Limit: 10,
		}); err == nil && len(related) > 0 {
			input.WriteString("\nStanding notebook entries these patterns may update:\n")
			now := time.Now()
			for _, fact := range related {
				fmt.Fprintf(&input, "#%d [%s · %s · %s] %s\n", fact.Seq, fact.Scope, fact.Kind, store.AgeLabel(fact.Time, now), fact.Body)
			}
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

const territoryDigestSystemPrompt = `You write one compact map of a territory made from several completed jobs. Return plain text, no JSON and no heading.

Begin with the exact job count. State what the series learned, then where its durable assets live. Preserve member IDs in square brackets beside the claims they support so a reader can route back to a job fold. Mention only paths supplied in the input. Be specific and bounded: at most 180 words.`

// digestTerritory is the territory mechanism's only model call. Membership
// and the display noun have already been chosen deterministically.
func digestTerritory(settings config.Config, client *liveClient) resident.TerritoryDigestFunc {
	return func(ctx context.Context, title string, jobs []resident.TerritoryDigestJob) (string, error) {
		var input strings.Builder
		fmt.Fprintf(&input, "Territory: %s\nJobs: %d\n", title, len(jobs))
		for _, job := range jobs {
			fmt.Fprintf(&input, "\n[%s] %s\nasked: %s\nlearned: %s\n",
				job.ID, job.Title, job.Ask, job.Outcome)
			if len(job.Pointers) > 0 {
				fmt.Fprintf(&input, "assets: %s\n", strings.Join(job.Pointers, ", "))
			}
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "reflect"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: territoryDigestSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input.String()}}},
		}, ai.WithMaxTokens(300))
		if err != nil || response == nil {
			return "", err
		}
		return strings.TrimSpace(response.Text()), nil
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
		if related, err := graph.SearchFactsUncounted(store.FactQuery{
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
	return func(ctx context.Context, scope string, facts []store.Fact, candidate *resident.ScopePair) (resident.Consolidation, error) {
		ordered := append([]store.Fact(nil), facts...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return ordered[i].Seq > ordered[j].Seq
		})

		var input strings.Builder
		outcomes, outcomesErr := graph.FactOutcomes()
		if outcomesErr != nil {
			outcomes = nil
		}
		now := time.Now()
		if scope == "" {
			input.WriteString("No scope needs notebook-line rewriting this tick.\n")
		} else {
			fmt.Fprintf(&input, "Target scope: %s\n\nNotebook lines, newest first:\n", scope)
		}
		for _, fact := range ordered {
			outcome := outcomes[fact.Seq]
			badRides := ""
			if outcome.Bad > 0 {
				badRides = fmt.Sprintf(" · rode %d jobs, %d ended badly", outcome.Rides, outcome.Bad)
			}
			kind := string(fact.Kind)
			if fact.Kind == store.FactPlaybook {
				kind = fact.Scope + " · " + kind
			}
			fmt.Fprintf(&input, "#%d [%s · %s · used %d×%s] %s\n", fact.Seq,
				kind, store.AgeLabel(fact.Time, now), fact.Uses, badRides, fact.Body)
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
		if candidate != nil {
			fmt.Fprintf(&input, "\nScope-gardening candidate:\n- %s\n- %s\n", candidate.First, candidate.Second)
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "consolidate"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: consolidatorSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input.String()}}},
		}, ai.WithMaxTokens(700))
		if err != nil || response == nil {
			return resident.Consolidation{}, err
		}
		return parseConsolidation(response.Text(), 8), nil
	}
}

func parseConsolidation(raw string, limit int) resident.Consolidation {
	result := resident.Consolidation{Facts: parseLearnedFacts(raw, limit)}
	object := jsonResponseObject(raw)
	if object == "" {
		return result
	}
	var parsed struct {
		ScopeAlias *resident.ScopeAliasJudgment `json:"scope_alias"`
	}
	if err := json.NewDecoder(strings.NewReader(object)).Decode(&parsed); err == nil {
		result.ScopeAlias = parsed.ScopeAlias
	}
	return result
}

func parseLearnedFacts(raw string, limit int) []resident.Learned {
	if limit <= 0 {
		return nil
	}
	raw = jsonResponseObject(raw)
	if raw == "" {
		return nil
	}
	var parsed struct {
		Facts []struct {
			Scope       string               `json:"scope"`
			Kind        store.FactKind       `json:"kind"`
			Body        string               `json:"body"`
			Unsettled   *store.UnsettledPair `json:"unsettled"`
			Replaces    int64                `json:"replaces"`
			Sources     []int64              `json:"sources"`
			Quarantines []int64              `json:"quarantines"`
			Skill       *struct {
				Artifact string `json:"artifact"`
			} `json:"skill"`
		} `json:"facts"`
	}
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&parsed); err != nil {
		return nil
	}
	learned := make([]resident.Learned, 0, min(limit, len(parsed.Facts)))
	for _, fact := range parsed.Facts {
		fact.Scope = strings.TrimSpace(fact.Scope)
		fact.Body = strings.TrimSpace(fact.Body)
		if fact.Kind == store.FactUnsettled {
			if fact.Unsettled == nil || fact.Unsettled.Validate() != nil {
				continue
			}
			fact.Body = store.FormatUnsettledPair(*fact.Unsettled)
		} else if fact.Unsettled != nil {
			continue
		}
		hasLine := fact.Scope != "" || fact.Body != "" || fact.Kind != ""
		if hasLine && (fact.Scope == "" || fact.Body == "" || !validLearnedKind(fact.Kind)) {
			continue
		}
		if !hasLine && len(fact.Quarantines) == 0 {
			continue
		}
		var skill *resident.SkillCandidate
		if fact.Skill != nil {
			artifact := strings.TrimSpace(fact.Skill.Artifact)
			if artifact == "" || fact.Kind != store.FactSkill {
				continue
			}
			skill = &resident.SkillCandidate{Artifact: artifact}
		}
		learned = append(learned, resident.Learned{
			Scope: fact.Scope, Kind: fact.Kind, Body: fact.Body, Unsettled: fact.Unsettled,
			Replaces: fact.Replaces, Sources: fact.Sources, Skill: skill,
			Quarantines: fact.Quarantines,
		})
		if len(learned) == limit {
			break
		}
	}
	return learned
}

func jsonResponseObject(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	return raw[start:]
}

func validLearnedKind(kind store.FactKind) bool {
	switch kind {
	case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain, store.FactUnsettled, store.FactSkill, store.FactPlaybook:
		return true
	default:
		return false
	}
}

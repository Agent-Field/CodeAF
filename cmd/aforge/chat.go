package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/aforge-v2/internal/voice"
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
	releaseResident, heldBy, err := lease.AcquireResident(filepath.Dir(path), "chat")
	if err != nil {
		return err
	}
	if releaseResident == nil {
		host := strings.TrimSpace(heldBy.Host)
		if host == "" {
			host = "unknown host"
		}
		fmt.Fprintf(os.Stderr, "another aforge is resident (pid %d on %s); attaching as a visitor\n",
			heldBy.PID, host)
		return runChatVisitor(path, *sessionID)
	}
	defer releaseResident()

	settings, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aforge chat needs a model to talk with.")
		fmt.Fprintln(os.Stderr, "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.")
		return err
	}
	prefs := loadChatPrefs(filepath.Dir(path))
	if strings.TrimSpace(prefs.VoiceModel) == "" {
		prefs.VoiceModel = settings.VoiceModel
	}
	// Discovery starts here and is waited for nowhere on this path. On a cold
	// cache it is a network round-trip, and every question it answers belongs
	// to a tool the user has not been able to reach yet — the surface has not
	// been drawn. Asking it in front of the first frame buys nothing and can
	// cost fifteen seconds of dead terminal.
	modelCatalog := catalog.LoadLazy(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})
	mediaClient, err := settings.MediaClient()
	if err != nil {
		return err
	}
	visionClient, err := settings.VisionClient()
	if err != nil {
		return err
	}
	documentClient, err := settings.DocumentClient()
	if err != nil {
		return err
	}
	talkModel := firstNonEmptyString(prefs.ChatModel, settings.Model)
	workModel := firstNonEmptyString(prefs.TaskModel, settings.Model)
	baseMedia := exec.MediaTools{
		Provider: mediaClient, Catalog: modelCatalog, VisionClient: visionClient,
		DocumentClient: documentClient, DocumentEngine: settings.DocumentEngine,
		ImageModel: prefs.ImageModel, SpeechModel: prefs.SpeechModel,
		MusicModel: prefs.MusicModel, VideoModel: prefs.VideoModel,
		// "best" and a named model are resolved at call time, so the strongest
		// advertised model is whatever the catalog says now, not at startup.
		ResolveModel: func(modality, word string) (string, error) {
			return config.ResolveMediaModel(modelCatalog, modality, word)
		},
	}
	// The slot defaults the catalog owns are filled the first time a leaf asks
	// for them, which is the first time they can possibly matter. Everything
	// here is the same resolution in the same order as before; only the moment
	// moves, from in front of the first frame to behind it.
	mediaModels := &chatMediaModels{tools: baseMedia}
	mediaModels.fill = func(tools *exec.MediaTools) {
		tools.ImageModel = firstNonEmptyString(tools.ImageModel, settings.ResolveImageModel(modelCatalog))
		tools.SpeechModel = firstNonEmptyString(tools.SpeechModel, settings.ResolveSpeechModel(modelCatalog))
		tools.MusicModel = firstNonEmptyString(tools.MusicModel, settings.ResolveMusicModel(modelCatalog))
		tools.VideoModel = firstNonEmptyString(tools.VideoModel, settings.ResolveVideoModel(modelCatalog))
		tools.VisionModel = settings.ResolveVisionModel(modelCatalog, talkModel, workModel)
		if video, ok := modelCatalog.Model(tools.VideoModel); ok {
			tools.VideoPrice = video.RequestPrice
		}
	}

	chatClient, err := newLiveClient(settings, talkModel)
	if err != nil {
		return err
	}
	defer chatClient.Close()
	taskClient, err := newLiveClient(settings, workModel)
	if err != nil {
		return err
	}
	defer taskClient.Close()
	// The plan slot structures work — the task graph, replans, contracts, the
	// delivery gate. Empty follows the work model live, so by default this is
	// the same model behind a second hot-swappable handle; a picked plan model
	// or AFORGE_PLAN_MODEL splits structuring from execution, and a /model
	// change lands on the very next planning call.
	planModel := firstNonEmptyString(prefs.PlanModel, settings.PlanModel, workModel)
	planClient, err := newLiveClient(settings, planModel)
	if err != nil {
		return err
	}
	defer planClient.Close()
	boostClients := &messageClientPool{settings: settings, clients: make(map[string]*liveClient)}
	defer boostClients.Close()
	// The ruler stays keyed to the work model even when a different model
	// plans: the anchors measure how the executor spends turns, and the plan
	// model only reads them to size work for that executor.
	measured, _ := profile.Load(settings.ProfileDir, taskClient.Model(), "linear")
	plan.UseAnchors(measured.Anchors)

	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()
	standingWatch, err := newStandingWatchManager(graph)
	if err != nil {
		return err
	}

	workspaceRoot := filepath.Join(filepath.Dir(path), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		return fmt.Errorf("create chat workspace: %w", err)
	}

	// The lease proves that no live resident can still own a claim in this DB.
	// A closed terminal mid-run can otherwise leave leaves stranded as
	// "running" forever. Release them back to pending before the
	// reconciler starts, and say so once: recovered work resumes rather than
	// haunting the rail.
	if released, err := graph.ReleaseOrphans(); err == nil && len(released) > 0 {
		_, _ = graph.PostMessage(store.Message{
			SessionID: *sessionID,
			Role:      store.RoleSystem,
			Body: fmt.Sprintf("recovered %d interrupted task(s) from the last session — resuming where they left off",
				len(released)),
		})
	}
	if err := resident.ReAdoptServices(graph, *sessionID, nil); err != nil {
		return fmt.Errorf("re-adopt services: %w", err)
	}

	// plans retains each planned job's graph so execution can be the headless
	// mechanism exactly: call shapes for the router, escalation verdicts, and
	// the profile records that calibrate the planner's ruler all read from the
	// same plan nodes the scheduler would have read.
	// They also outlive this process: a job that was planned in the session you
	// closed last night is still running this morning, and its remaining plan
	// is still editable.
	plans := newJobPlans(graph)
	// The craft repository lives beside the graph it serves. Without git or
	// with a broken repo, craft is dormant — never a startup failure.
	var craftRunner *resident.CraftRunner
	var craftShelf resident.CraftShelf
	craftDir := ""
	if craftRepo, craftErr := craft.Open(filepath.Join(filepath.Dir(*database), "craft")); craftErr == nil {
		craftRunner = resident.NewCraftRunner(graph, craftRepo, craftRepo.Dir())
		craftShelf, craftDir = craftRepo, craftRepo.Dir()
	} else {
		log.Printf("note: craft repository unavailable: %v", craftErr)
	}
	// The commander owns the live boost slot, and it is built further down;
	// the resolver reads it through this handle so a later /model change is
	// what the next job's model words resolve against.
	var commander *chatCommander
	reconciler := newResidentReconciler(settings, graph, chatClient, taskClient, planClient, plans,
		func(words head.ModelWords) head.WorkModelChoice {
			return resolveWorkModelWords(words, modelCatalog, func() string {
				if commander != nil {
					return commander.CurrentModel(head.ModelSlotBoost)
				}
				return taskClient.Model()
			})
		}).
		WithNarrator(narrateProgress(settings, chatClient, graph)).
		WithBriefComposer(composeMorningBrief(settings, chatClient, graph)).
		// Redirection is a chat-surface concern — a wake pass has no user
		// whose words could revise a running job.
		WithRedirector(func(ctx context.Context, job store.Node, message string,
			flavor resident.RevisionFlavor) (resident.Redirection, error) {
			return plans.reviseForUser(ctx, settings, planClient, graph, job, message, flavor)
		}).
		WithStandingWatch(standingWatch).
		WithStandingWatchKeyPersist(func() (bool, string, error) {
			return config.EnsurePersistedAPIKey(settings.ProfileDir)
		})
	if craftRunner != nil {
		reconciler = reconciler.WithCraftRunner(craftRunner)
	}
	// Recognition and forging ride the resident's own talk client, like every
	// other small verdict it makes about itself.
	if craftShelf != nil {
		reconciler = reconciler.WithCraftMind(resident.NewCraftMind(craftShelf, craftDir,
			fillCraftParams(settings, chatClient), repairCraft(settings, chatClient)))
	}
	if err := reconciler.AttachSession(*sessionID); err != nil {
		return err
	}
	// The attach edge is journalled now, because its ordering is what fixes the
	// brief's window. Composing that brief is a model round-trip over a journal
	// gather, and it used to run here, in front of the first frame — the user
	// waited on the network to be told what happened while they were away. The
	// thread is the delivery channel, so the composition rides behind the
	// surface and the brief lands in the same place a moment later.
	deliverBrief, briefErr := reconciler.SessionOpening(*sessionID, "tui", settings.BriefAfter)
	if briefErr != nil {
		log.Printf("note: could not prepare the arrival brief: %v", briefErr)
	}

	web := exec.NewWeb()
	// chatWorkerCeiling is not a concurrency policy — the provider's adaptive
	// rate limiter is the real throttle. This is a local sanity bound on
	// goroutines/file handles, far above any realistic graph width.
	const chatWorkerCeiling = 32
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
		documentPaths, err := stageDocumentAttachments(jobSpace, node.Provenance.Attachments)
		if err != nil {
			return resident.ExecResult{}, err
		}
		// A planned job keeps one executor for every leaf so its profile key names
		// the model that actually produced all measured turns. A picker change
		// applies to the next job rather than relabeling work already in flight.
		planPrefix, planGraph, planNode, workingModel, workingClient := plans.lookup(node.ID)
		if workingClient == nil {
			// A job rehydrated from the journal after a restart carries its
			// structure but no live client, and its leaves genuinely do run on
			// the current work model — so the profile key must name that one
			// rather than the model that planned the job in a process that is
			// no longer here.
			workingModel, workingClient = taskClient.Snapshot()
		}
		// A model the user named for this job outranks both, and only for this
		// job: the request rides the node's provenance, so every leaf under it
		// is served by that model however the work slot moves afterwards.
		escalatable := taskClient.escalatable()
		if pinned, ok := pinnedWorkClient(boostClients, node); ok {
			workingModel, workingClient = pinned.Snapshot()
			escalatable = pinned.escalatable()
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
		leafMedia := mediaModels.Snapshot()
		leafMedia.WorkingModel = workingModel
		leafMedia.VisionModel = settings.ResolveVisionModel(modelCatalog, chatClient.Model(), workingModel)
		leafMedia.BeforeSpend = func(_ context.Context, additional float64) error {
			if settings.DailyBudgetUSD <= 0 {
				return nil
			}
			rail, _, gateErr := graph.PauseDailyRailWithAdditionalSpend(settings.DailyBudgetUSD, node.Provenance.SessionID, additional)
			if gateErr != nil {
				return gateErr
			}
			if rail.Reached {
				return fmt.Errorf("daily budget reached")
			}
			return nil
		}
		linear := exec.NewLinear(workingClient, jobSpace, web, turns, tokens, deadline).
			WithStore(graph).WithMedia(&leafMedia).
			WithAttribution(config.AttributionAt(settings.ProfileDir))
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

		// Every input is named. Untitled, they render as `=== from "" ===`
		// under a header that says the results are prior work the leaf already
		// has and must not gather again — so a standing lesson reading "check X
		// before Y" arrived as a claim that X had been checked. The notebook is
		// not a prior result and says so; a dependency says whose it is.
		inputs := make([]exec.Input, 0)
		if digest := resident.NotebookDigest(graph, node.ID, node.Brief, node.Provenance.Intent, 8); digest != "" {
			inputs = append(inputs, exec.Input{Title: notebookInputTitle, Result: digest})
		}
		dependencies, err := graph.DependencyInputs(node.ID, store.MaxDigestBytes)
		if err == nil {
			for _, dependency := range dependencies {
				// The files are what make the 300-word cap survivable: a
				// producer keeps its answer in the message and its working in a
				// file, and its consumer can only honour that split if it is
				// told where the file is. Without this the pointer was written
				// and never delivered.
				inputs = append(inputs, exec.Input{
					Title:     dependency.NodeID,
					Result:    dependency.Digest,
					Artifacts: dependency.Artifacts,
				})
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

		// The contract is the working method and belongs in the system message
		// beside the harness's own invariants, where a headless run already
		// puts it: together they are what a specialised harness for this domain
		// would have been hand-written to say, and they are the frozen prefix
		// every turn of the leaf is billed against. Folded into the brief it
		// still arrived, but as user text below the churn.
		leafTitle := firstLine(node.Brief)
		if title := strings.TrimSpace(node.Title); title != "" {
			leafTitle = title
		}
		task := exec.Task{
			Reflex:      isReflex,
			NodeID:      int(node.CreatedSeq),
			StoreNodeID: node.ID,
			Title:       firstLine(node.Brief),
			Goal:        node.Provenance.Intent,
			Brief:       withDocumentAttachmentBrief(residentDeliveryBrief(graph, node), documentPaths),
			Contract:    planNodeContract(planNode),
			OutputHint:  exec.SuggestPath(int(node.CreatedSeq), leafTitle),
			Inputs:      inputs,
			Steer:       steer,
			Control: func() exec.ControlAction {
				control, err := graph.Control(node.ID)
				if err != nil {
					return exec.ControlNone
				}
				if control.CancelRequested {
					return exec.ControlCancel
				}
				if control.Held {
					return exec.ControlPause
				}
				return exec.ControlNone
			},
			ImagePaths: append([]string(nil), node.Provenance.Attachments...),
		}
		// The scheduler's quality loop, inline: each attempt is one routable
		// unit carrying its call shape, a watchdog sits above the leaf's own
		// deadline so a wedged executor becomes a recorded failure rather
		// than a silent hang, and a leaf whose verdict says a stronger model
		// might fix it gets exactly one escalation when a panel offers one.
		attempts := 1
		if !isReflex && escalatable {
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
			// An escalation that repeats the task verbatim buys a stronger model
			// and then pays it to rediscover everything the first attempt found —
			// including files sitting in the shared workspace it is about to
			// write again. The gate's revision pass has always been sighted this
			// way; the escalation was the one retry that was not.
			if attempt > 0 && outcome != nil {
				attempted := task
				attempted.Inputs = append(append([]exec.Input{}, inputs...),
					previousAttemptInput(outcome, jobDir))
				task = attempted
			}
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
		if err == nil && outcome != nil && (outcome.Stop == exec.StopPaused || outcome.Stop == exec.StopCancelled) {
			return resident.ExecResult{
				Summary: outcome.Text, PromptTokens: spent.PromptTokens,
				CompletionTokens: spent.CompletionTokens, Cost: spent.Cost,
				ServiceRequests: outcome.ServiceRequests,
			}, nil
		}
		// The workspace-relative paths become absolute once, here, because three
		// readers need the same list: the summary the user opens files from, the
		// revision sentinel, and the overrun replan. It used to be built below
		// the sentinel's call, which is why the sentinel was the one reader told
		// nothing about the file a leaf had just written.
		absolute := make([]string, 0)
		if outcome != nil {
			for _, artifact := range outcome.Artifacts {
				absolute = append(absolute, filepath.Join(jobDir, artifact))
			}
		}
		// Result-driven revision: each landed leaf is shown to the sentinel,
		// which edits the job's unstarted remainder only when this result
		// contradicts a specific assumption in a specific node. Its default
		// is no change; the store refuses everything else. A failure goes in as
		// its own words: "it failed" says the next steps have nothing to
		// consume, while the reason says which assumption died.
		if !isReflex && planGraph != nil && outcome != nil {
			failure := ""
			if err != nil {
				failure = err.Error()
			}
			plans.reviseAfter(ctx, settings, planClient, graph, node, planPrefix, planGraph,
				outcome.Text, absolute, failure, workerModel)
		}
		if err != nil {
			// Preserve failed-attempt evidence even though no delivery reaches the
			// gate. This is the pre-existing profile path, kept on the early return.
			if landed, prefix := plans.takeIfRoot(node.ID); landed != nil {
				guard.Go("chat/recalibrate", func() {
					_, records := recordAndCalibrateDetailed(settings.Context(context.Background(), landed.Goal), planClient, settings, workingModel, landed)
					recordPlanSurprises(graph, prefix, records)
				})
			} else if node.Parent == store.RootID && outcome != nil && isSingleLeafJob(graph, node) {
				if isReflex {
					guard.Go("chat/record-reflex", func() {
						record, ok := recordReflex(settings, workerModel, node, outcome, false)
						if ok {
							recordProfileSurprise(graph, node.ID, record)
						}
					})
				} else {
					guard.Go("chat/record-single-leaf", func() {
						record, ok := recordSingleLeaf(settings, workerModel, node, outcome)
						if ok {
							recordProfileSurprise(graph, node.ID, record)
						}
					})
				}
			}
			// The error says the leaf produced nothing; it says nothing about
			// what producing nothing cost. Escalation has usually run the whole
			// task twice by the time we arrive here, so this is the most
			// expensive kind of result there is — and returning a bare zero
			// value is what made real spend journal as $0.00 on the daily rail.
			return resident.ExecResult{
				PromptTokens:     spent.PromptTokens,
				CompletionTokens: spent.CompletionTokens,
				Cost:             spent.Cost,
			}, err
		}
		// The user's next act is opening the file, so the summary carries where
		// it actually lives; the absolute paths were resolved above.
		text := outcome.Text
		if len(absolute) > 0 {
			text += "\n\nFiles:\n" + strings.Join(absolute, "\n")
		}
		continuing := false
		// Resource exhaustion is invisible: it grows the graph and the final
		// assembled deliverable reaches the gate. Semantic failure stays honest
		// and still lands with the evidence from the failing leaf.
		if !isReflex && outcome.Overran() {
			spliced, _, replanErr := resident.ReplanOverrun(ctx, graph, node, outcome.Text, absolute,
				settings.DailyBudgetUSD, replanRemainder(settings, planClient, taskClient, plans, graph))
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
		if len(outcome.ServiceRequests) == 0 && shouldGate(node, outcome, continuing) {
			gate := judgeDeliverable(ctx, settings, planClient, graph, node, text, workerModel)
			if gate.Checked {
				evidence := store.DeliveryGate{Pass: gate.Pass, Gap: gate.Gaps}
				if gate.Pass {
					outcome.Verdict = provider.VerdictVerifiedSuccess
				}
				if !gate.Pass {
					revision := task
					revision.Inputs = append(append([]exec.Input{}, inputs...), exec.Input{
						Title:     "a review of your own first draft",
						Artifacts: append([]string(nil), absolute...),
						Result: "A reviewer compared the previous attempt against the original request and found gaps that must be closed:\n" + gate.Gaps +
							"\n\nThe previous attempt (build on it, fix the gaps, do not start over):\n" + text +
							"\n\n" + gateRevisionContract,
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
						closed := judgeDeliverable(ctx, settings, planClient, graph, node, text, polishModel)
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
			// The gate judges the request. Taste is the other half and is never
			// allowed to be a gate: an unproven rule rides one quiet question
			// with the delivery, which lands either way.
			_, _, _ = resident.AnnotateDelivery(graph, node, text)
		}
		outcome.Text = text
		outcome.Usage = spent
		outcome.Turns = spentTurns
		if planNode != nil {
			plans.recordOutcome(planNode, outcome, nil)
		}
		if landed, prefix := plans.takeIfRoot(node.ID); landed != nil {
			// The recalibration report reaches the thread, not a stdout the TUI
			// owns; detached, because the ruler is telemetry and the user's
			// result must not wait on it. The namespace comes from the registry
			// rather than from slicing the id: this root's own id IS the prefix,
			// so the old "-n" search never matched and recalibration for a
			// planned chat job simply never ran.
			sessionID := node.Provenance.SessionID
			guard.Go("chat/recalibrate", func() {
				report, records := recordAndCalibrateDetailed(settings.Context(context.Background(), landed.Goal), planClient, settings, workingModel, landed)
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
			})
		} else if node.Parent == store.RootID && isSingleLeafJob(graph, node) {
			if isReflex {
				guard.Go("chat/record-reflex", func() {
					record, ok := recordReflex(settings, workerModel, node, outcome, promoted)
					if ok {
						recordProfileSurprise(graph, node.ID, record)
					}
				})
			} else {
				guard.Go("chat/record-single-leaf", func() {
					record, ok := recordSingleLeaf(settings, workerModel, node, outcome)
					if ok {
						recordProfileSurprise(graph, node.ID, record)
					}
				})
			}
		}
		return resident.ExecResult{
			Summary:          text,
			PromptTokens:     spent.PromptTokens,
			CompletionTokens: spent.CompletionTokens,
			Cost:             spent.Cost,
			Promote:          promoted,
			ServiceRequests:  outcome.ServiceRequests,
		}, nil
	}, "chat-runner", chatWorkerCeiling).WithDailyBudgetUSD(settings.DailyBudgetUSD)
	if craftRunner != nil {
		runner = runner.WithCraftRunner(craftRunner)
	}

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
		// Registered first so it absorbs last: the channel close and the wait
		// group both settle on the unwind before the fault is recorded.
		defer guard.Recover("chat/head")
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
			WithMessageClient(boostClients.ForMessage).
			WithSelfKnowledge(func() string { return selfKnowledge(settings, taskClient.Model()) }).
			WithImageInput(modelCatalog, settings.Model).
			WithCompetenceMap(func() string {
				return competenceGrounding(graph, settings.ProfileDir, taskClient.Model())
			}).
			WithStandingWatch(func() string {
				return watchGrounding(path, graph, standingWatch, settings.DailyBudgetUSD)
			}).
			WithDailyBudgetUSD(settings.DailyBudgetUSD).
			Serve(headContext)
	}()
	go func() { defer guard.Recover("chat/reconciler"); defer background.Done(); _ = reconciler.Serve(ctx) }()
	go func() { defer guard.Recover("chat/runner"); defer background.Done(); _ = runner.Serve(ctx) }()

	commander = &chatCommander{
		settings:      settings,
		database:      path,
		prefsDir:      filepath.Dir(path),
		workspaceRoot: workspaceRoot,
		chatClient:    chatClient,
		taskClient:    taskClient,
		planClient:    planClient,
		store:         graph,
		prefs:         prefs,
		sessionID:     *sessionID,
		streamEvents:  streamEvents,
		voiceRecorder: voice.NewSystemRecorder(),
		models:        modelCatalog,
		mediaModels:   mediaModels,
		attachSession: reconciler.AttachSession,
	}
	commander.voiceTranscriber, err = voice.NewClient(voice.ClientConfig{
		APIKey: settings.APIKey, BaseURL: settings.BaseURL, Timeout: settings.Timeout,
		SiteURL: settings.SiteURL, SiteName: settings.SiteName,
	})
	if err != nil {
		return err
	}
	// While bubbletea owns the terminal, anything written to stderr or the
	// standard logger tears straight through the alt screen as a raw row (a
	// contract failure once printed itself across both panes). Everything the
	// runtime logs goes to a file for the TUI's lifetime instead.
	if logFile, logErr := os.OpenFile(filepath.Join(filepath.Dir(defaultChatDB()), "chat.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); logErr == nil {
		log.SetOutput(logFile)
		defer func() {
			log.SetOutput(os.Stderr)
			_ = logFile.Close()
		}()
	}
	if deliverBrief != nil {
		background.Add(1)
		go func() {
			defer guard.Recover("chat/arrival-brief")
			defer background.Done()
			if err := deliverBrief(ctx); err != nil {
				log.Printf("note: could not deliver the arrival brief: %v", err)
			}
		}()
	}
	err = tui.RunWithCommander(graph, *sessionID, commander)
	seenErr := reconciler.SessionClosed(*sessionID, "tui")
	cancel()
	waitWithGrace(&background, 5*time.Second)
	return errors.Join(err, seenErr)
}

// runChatVisitor is a pure surface over the durable store. The elected
// resident's head will route its user messages and the command journal will be
// reconciled there; this process tails the thread and never starts a head,
// reconciler, or worker runner of its own.
func runChatVisitor(path, sessionID string) error {
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()

	surface := resident.New(graph, nil, nil)
	if err := surface.AttachSession(sessionID); err != nil {
		return err
	}
	if err := surface.SessionOpened(context.Background(), sessionID, "tui", 0); err != nil {
		return err
	}
	commander := newVisitorCommander(path, sessionID, graph, surface.AttachSession)
	err = tui.RunWithCommander(graph, sessionID, commander)
	seenErr := surface.SessionClosed(sessionID, "tui")
	return errors.Join(err, seenErr)
}

// newVisitorCommander builds the commander a visitor drives. It deliberately
// carries no model clients and no live catalog: a visitor owns no head, so
// every capability that would speak to a provider must degrade to the recorded
// preference rather than reach for a client that does not exist here.
func newVisitorCommander(path, sessionID string, graph *store.Store,
	attachSession func(string) error) *chatCommander {
	return &chatCommander{
		database:      path,
		prefsDir:      filepath.Dir(path),
		workspaceRoot: filepath.Join(filepath.Dir(path), "workspace"),
		store:         graph,
		prefs:         loadChatPrefs(filepath.Dir(path)),
		sessionID:     sessionID,
		attachSession: attachSession,
	}
}

// residentDeliveryBrief gives only the top-level deliverable owner the voice
// contract. Planned synthesis and direct jobs share this path; child results
// remain worker-to-worker material. A gate repair copies this same task, so its
// one polish pass cannot drift to a different voice.
// notebookInputTitle names the digest for what it is. The leaf's input header
// calls everything below it prior work it already has; the notebook is standing
// memory instead, and the only thing that separates the two in the rendering is
// this name.
const notebookInputTitle = "your notebook — standing preferences and lessons, not results"

// planNodeContract reads the working method off the plan node when this leaf
// belongs to a planned job. A single-leaf job, a splice and a reflex have no
// contract, and inventing a generic one would only dilute the system message
// they do have.
func planNodeContract(node *plan.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Contract)
}

// previousAttemptInput hands the escalated attempt what the first one actually
// produced. It is deliberately shaped like the gate's revision input: the text,
// then the paths, then the instruction not to start over — because the failure
// mode is not that the strong model works badly, it is that it works from
// scratch.
func previousAttemptInput(outcome *exec.Outcome, jobDir string) exec.Input {
	body := strings.TrimSpace(outcome.Text)
	if body == "" {
		body = "It produced no usable text before it stopped."
	}
	absolute := make([]string, 0, len(outcome.Artifacts))
	for _, artifact := range outcome.Artifacts {
		absolute = append(absolute, filepath.Join(jobDir, artifact))
	}
	return exec.Input{
		Title:     "your own earlier attempt at this same task",
		Result:    "An earlier attempt on a weaker model ended as " + string(outcome.Verdict) + ". What it had when it stopped:\n" + body,
		Artifacts: absolute,
	}
}

func residentDeliveryBrief(graph *store.Store, node store.Node) string {
	if node.Parent != store.RootID {
		return node.Brief
	}
	return resident.VoicePrompt(graph, node.Brief, node.Provenance.Intent, node.Brief)
}

const chatDocumentAttachmentLimit = 25 << 20

// stageDocumentAttachments turns durable drag-and-drop paths into immutable
// workspace inputs. A content suffix avoids basename collisions and lets every
// leaf in one job race safely toward the same already-complete file.
func stageDocumentAttachments(space *exec.Workspace, attachments []string) ([]string, error) {
	if space == nil {
		return nil, fmt.Errorf("stage document attachments: nil workspace")
	}
	seen := make(map[string]bool)
	var staged []string
	for _, source := range attachments {
		extension := strings.ToLower(filepath.Ext(source))
		if extension != ".pdf" && extension != ".docx" && extension != ".pptx" {
			continue
		}
		info, err := os.Stat(source)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("stage attached document %s: file is unavailable", filepath.Base(source))
		}
		if info.Size() > chatDocumentAttachmentLimit {
			return nil, fmt.Errorf("stage attached document %s: over the 25 MB document limit", filepath.Base(source))
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("stage attached document %s: %w", filepath.Base(source), err)
		}
		hash := sha256.Sum256(data)
		stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
		relative := filepath.Join("attachments", stem+"-"+hex.EncodeToString(hash[:4])+extension)
		if seen[relative] {
			continue
		}
		seen[relative] = true
		target, err := space.Resolve(relative)
		if err != nil {
			return nil, fmt.Errorf("stage attached document %s: %w", filepath.Base(source), err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("stage attached document %s: %w", filepath.Base(source), err)
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			if _, err = file.Write(data); err == nil {
				err = file.Close()
			} else {
				_ = file.Close()
			}
		}
		if err != nil && !os.IsExist(err) {
			return nil, fmt.Errorf("stage attached document %s: %w", filepath.Base(source), err)
		}
		staged = append(staged, filepath.ToSlash(relative))
	}
	return staged, nil
}

func withDocumentAttachmentBrief(brief string, paths []string) string {
	if len(paths) == 0 {
		return brief
	}
	var addition strings.Builder
	addition.WriteString("\n\nAttached documents are workspace inputs. Read them with read_document:\n")
	for _, path := range paths {
		addition.WriteString("- ")
		addition.WriteString(filepath.ToSlash(path))
		addition.WriteByte('\n')
	}
	return strings.TrimSpace(brief) + strings.TrimRight(addition.String(), "\n")
}

// chatPrefs persists the surface's model choices across launches. It lives
// beside the graph database so the whole resident state moves as one
// directory.
type chatPrefs struct {
	ChatModel string `json:"chat_model,omitempty"`
	TaskModel string `json:"task_model,omitempty"`
	// PlanModel empty means the plan slot follows the work model live —
	// the same contract as an empty boost slot.
	PlanModel   string `json:"plan_model,omitempty"`
	BoostModel  string `json:"boost_model,omitempty"`
	VoiceModel  string `json:"voice_model,omitempty"`
	ImageModel  string `json:"image_model,omitempty"`
	SpeechModel string `json:"speech_model,omitempty"`
	MusicModel  string `json:"music_model,omitempty"`
	VideoModel  string `json:"video_model,omitempty"`

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

// chatMediaModels is the resolved media configuration seen by new leaves.
// Each leaf takes one value snapshot, preserving the same "next job/leaf"
// boundary used by the hot-swappable work model.
type chatMediaModels struct {
	// fill supplies the slot defaults that have to be asked of the model
	// catalog. It runs at most once, on the first read, so a cold catalog is
	// paid for by the first leaf that needs a media model rather than by the
	// user waiting for the surface to appear.
	once  sync.Once
	fill  func(*exec.MediaTools)
	mu    sync.RWMutex
	tools exec.MediaTools
}

func (m *chatMediaModels) Snapshot() exec.MediaTools {
	if m == nil {
		return exec.MediaTools{}
	}
	m.resolve()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools
}

func (m *chatMediaModels) resolve() {
	m.once.Do(func() {
		if m.fill == nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.fill(&m.tools)
	})
}

func (m *chatMediaModels) Set(role, slug string, models *catalog.Catalog) {
	if m == nil {
		return
	}
	// A user's choice must land on top of the discovered defaults, never
	// underneath a fill that has not run yet.
	m.resolve()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch role {
	case "image":
		m.tools.ImageModel = slug
	case "speech":
		m.tools.SpeechModel = slug
	case "music":
		m.tools.MusicModel = slug
	case "video":
		m.tools.VideoModel = slug
		m.tools.VideoPrice = 0
		if model, ok := models.Model(slug); ok {
			m.tools.VideoPrice = model.RequestPrice
		}
	}
}

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
	planClient *liveClient
	store      *store.Store

	mu               sync.Mutex
	prefs            chatPrefs
	sessionID        string
	streamEvents     <-chan tui.StreamEvent
	voiceRecorder    voice.Recorder
	voiceTranscriber voice.Transcriber
	attachSession    func(string) error

	catalogOnce    sync.Once
	catalogChoices []tui.ModelChoice
	slotCatalogMu  sync.Mutex
	slotCatalog    map[string][]tui.ModelChoice
	models         *catalog.Catalog
	mediaModels    *chatMediaModels
}

func (c *chatCommander) StreamEvents() <-chan tui.StreamEvent { return c.streamEvents }

func (c *chatCommander) VoiceRecorder() voice.Recorder       { return c.voiceRecorder }
func (c *chatCommander) VoiceTranscriber() voice.Transcriber { return c.voiceTranscriber }

func (c *chatCommander) RecordVoiceUsage(cost float64) {
	if c == nil || c.store == nil || cost <= 0 {
		return
	}
	_ = c.store.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: cost})
}

func (c *chatCommander) Models() []string {
	candidates := make([]string, 0, len(c.settings.Panel.Models)+7)
	for _, spec := range c.settings.Panel.Models {
		candidates = append(candidates, spec.Slug)
	}
	candidates = append(candidates, c.settings.Model)
	if c.chatClient != nil {
		candidates = append(candidates, c.chatClient.Model())
	}
	if c.taskClient != nil {
		candidates = append(candidates, c.taskClient.Model())
	}
	if c.planClient != nil {
		candidates = append(candidates, c.planClient.Model())
	}
	if boost := strings.TrimSpace(c.CurrentModel("boost")); boost != "" {
		candidates = append(candidates, boost)
	}
	models := dedupeModels(candidates)
	if len(models) < 4 {
		models = dedupeModels(append(models, fallbackChatModels...))
	}
	return models
}

func (c *chatCommander) Catalog() []tui.ModelChoice {
	c.catalogOnce.Do(func() {
		if c.models != nil {
			for _, model := range config.ModelCandidates(c.models, "talk") {
				c.catalogChoices = append(c.catalogChoices, tui.ModelChoice{
					Slug: model.ID, Name: model.Name,
					Price: formatModelPrice(model.PromptPrice, model.CompletionPrice),
				})
			}
		}
		if len(c.catalogChoices) < 4 {
			seen := make(map[string]bool, len(c.catalogChoices))
			for _, choice := range c.catalogChoices {
				seen[choice.Slug] = true
			}
			for _, model := range c.Models() {
				if seen[model] {
					continue
				}
				c.catalogChoices = append(c.catalogChoices, tui.ModelChoice{Slug: model})
				seen[model] = true
			}
		}
	})
	return append([]tui.ModelChoice(nil), c.catalogChoices...)
}

// CatalogFor keys the shared searchable picker by palette slot. Capability
// filtering lives in config.ModelCandidates so music discovery uses the exact
// same recognizable-TTS exclusion as runtime resolution.
func (c *chatCommander) CatalogFor(role string) []tui.ModelChoice {
	if role == "talk" || role == "work" || role == "plan" || role == "boost" {
		return c.Catalog()
	}
	if cached, ok := c.cachedSlotCatalog(role); ok {
		return cached
	}

	choices := make([]tui.ModelChoice, 0)
	for _, model := range config.ModelCandidates(c.models, role) {
		choices = append(choices, tui.ModelChoice{
			Slug: model.ID, Name: model.Name,
			Price: formatModelPrice(model.PromptPrice, model.CompletionPrice),
		})
	}
	if len(choices) == 0 {
		if current := strings.TrimSpace(c.CurrentModel(role)); current != "" {
			choices = []tui.ModelChoice{{Slug: current}}
		}
	}
	c.cacheSlotCatalog(role, choices)
	return choices
}

// cachedSlotCatalog hands back a copy, never the stored slice: the picker is
// free to sort what it is given.
func (c *chatCommander) cachedSlotCatalog(role string) ([]tui.ModelChoice, bool) {
	c.slotCatalogMu.Lock()
	defer c.slotCatalogMu.Unlock()
	choices, ok := c.slotCatalog[role]
	if !ok {
		return nil, false
	}
	return append([]tui.ModelChoice(nil), choices...), true
}

func (c *chatCommander) cacheSlotCatalog(role string, choices []tui.ModelChoice) {
	c.slotCatalogMu.Lock()
	defer c.slotCatalogMu.Unlock()
	if c.slotCatalog == nil {
		c.slotCatalog = make(map[string][]tui.ModelChoice)
	}
	c.slotCatalog[role] = append([]tui.ModelChoice(nil), choices...)
}

func (c *chatCommander) CurrentModel(role string) string {
	switch role {
	case "work":
		if c.taskClient != nil {
			return c.taskClient.Model()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.prefs.TaskModel
	case "plan":
		if c.planClient != nil {
			return c.planClient.Model()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.prefs.PlanModel
	case "boost":
		if boost := c.boostPreference(); boost != "" {
			return boost
		}
		if c.taskClient != nil {
			return c.taskClient.Model()
		}
		return ""
	case "voice":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.VoiceModel, c.settings.VoiceModel)
	case "image":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.ImageModel, c.settings.ResolveImageModel(c.models))
	case "speech":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.SpeechModel, c.settings.ResolveSpeechModel(c.models))
	case "music":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.MusicModel, c.settings.ResolveMusicModel(c.models))
	case "video":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.VideoModel, c.settings.ResolveVideoModel(c.models))
	}
	if c.chatClient != nil {
		return c.chatClient.Model()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefs.ChatModel
}

// boostPreference is the boost slot's own critical section: empty means the
// user never chose one and the work model stands in.
func (c *chatCommander) boostPreference() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.prefs.BoostModel)
}

func (c *chatCommander) ImageInputSupport() (string, bool) {
	return c.ImageInputSupportFor("talk")
}

func (c *chatCommander) ImageInputSupportFor(role string) (string, bool) {
	model := c.CurrentModel(role)
	return model, c.models != nil && c.models.Supports(model, "input", "image")
}

func (c *chatCommander) ModelFollows(role string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch role {
	case "boost":
		return strings.TrimSpace(c.prefs.BoostModel) == ""
	case "plan":
		// An environment pin is an explicit choice too: the slot only follows
		// the work model when neither the prefs nor AFORGE_PLAN_MODEL name one.
		return strings.TrimSpace(c.prefs.PlanModel) == "" && strings.TrimSpace(c.settings.PlanModel) == ""
	}
	return false
}

func (c *chatCommander) SetModel(role, slug string) error {
	switch role {
	case "talk":
		if c.chatClient == nil {
			return fmt.Errorf("talk model switching is unavailable in visitor mode")
		}
		if err := c.chatClient.SetModel(slug); err != nil {
			return err
		}
	case "work":
		if c.taskClient == nil {
			return fmt.Errorf("work model switching is unavailable in visitor mode")
		}
		if err := c.taskClient.SetModel(slug); err != nil {
			return err
		}
		// A model switch changes the capability being sized; install that model's
		// own ruler before the next planning call can observe the new client.
		measured, _ := profile.Load(c.settings.ProfileDir, c.taskClient.Model(), "linear")
		plan.UseAnchors(measured.Anchors)
		// A following plan slot moves with the work model, live — the next
		// planning call sees the new model without the user touching the slot.
		if c.planClient != nil && c.ModelFollows("plan") {
			if err := c.planClient.SetModel(c.taskClient.Model()); err != nil {
				return err
			}
		}
	case "plan":
		if c.planClient == nil {
			return fmt.Errorf("plan model switching is unavailable in visitor mode")
		}
		// Empty is meaningful for plan, like boost: it resumes following the
		// work model live.
		target := strings.TrimSpace(slug)
		if target == "" && c.taskClient != nil {
			target = c.taskClient.Model()
		}
		if err := c.planClient.SetModel(target); err != nil {
			return err
		}
	case "boost":
		// Empty is meaningful for boost: it restores live inheritance from work.
	case "voice", "image", "speech", "music", "video":
		if strings.TrimSpace(slug) == "" {
			return fmt.Errorf("%s model cannot be empty", role)
		}
	default:
		return fmt.Errorf("unknown model role %q", role)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if role == "talk" {
		c.prefs.ChatModel = c.chatClient.Model()
	} else if role == "work" {
		c.prefs.TaskModel = c.taskClient.Model()
	} else if role == "plan" {
		c.prefs.PlanModel = strings.TrimSpace(slug)
		// The user cleared the slot by hand: their choice to follow the work
		// model outranks the environment seed for the rest of this session.
		if c.prefs.PlanModel == "" {
			c.settings.PlanModel = ""
		}
	} else if role == "boost" {
		c.prefs.BoostModel = strings.TrimSpace(slug)
	} else if role == "voice" {
		c.prefs.VoiceModel = strings.TrimSpace(slug)
	} else if role == "image" {
		c.prefs.ImageModel = strings.TrimSpace(slug)
	} else if role == "speech" {
		c.prefs.SpeechModel = strings.TrimSpace(slug)
	} else if role == "music" {
		c.prefs.MusicModel = strings.TrimSpace(slug)
	} else if role == "video" {
		c.prefs.VideoModel = strings.TrimSpace(slug)
	}
	if err := saveChatPrefs(c.prefsDir, c.prefs); err != nil {
		return fmt.Errorf("save chat model preference: %w", err)
	}
	if role == "image" || role == "speech" || role == "music" || role == "video" {
		c.mediaModels.Set(role, strings.TrimSpace(slug), c.models)
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

// session and setSession are the whole of the session id's critical section.
// Everything downstream — attaching, posting, cancelling — happens outside the
// lock, because those are store calls and a store call must never hold it.
func (c *chatCommander) session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *chatCommander) setSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = sessionID
}

func (c *chatCommander) NewSession() (string, error) {
	sessionID := newSessionID()
	c.setSession(sessionID)
	if c.attachSession != nil {
		if err := c.attachSession(sessionID); err != nil {
			return "", err
		}
	}
	if c.store != nil {
		if _, err := c.store.TouchSeen("tui", sessionID, store.SeenAttached); err != nil {
			return "", err
		}
	}
	return sessionID, nil
}

func (c *chatCommander) Cancel(nodeID string) error {
	_, err := c.store.RequestCommand(store.Command{
		SessionID:   c.session(),
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

func (c *chatCommander) ResolveMediaPath(nodeID, relative string) (string, bool) {
	if filepath.IsAbs(relative) {
		if info, err := os.Stat(relative); err == nil && !info.IsDir() {
			return relative, true
		}
		return "", false
	}
	return c.ResolveWorkspacePath(nodeID, relative)
}

// ResolveWorkspacePath is the shared safety boundary for every deliverable
// link. A node resolves to its top-level job directory; relative traversal may
// not escape that directory, and only existing files become links.
func (c *chatCommander) ResolveWorkspacePath(nodeID, relative string) (string, bool) {
	if c == nil || c.store == nil || c.workspaceRoot == "" || nodeID == "" || strings.TrimSpace(relative) == "" {
		return "", false
	}
	if filepath.IsAbs(relative) {
		return "", false
	}
	node, ok, err := c.store.Node(nodeID)
	if err != nil || !ok {
		return "", false
	}
	root := filepath.Join(c.workspaceRoot, jobIDOf(c.store, node))
	target := filepath.Join(root, filepath.Clean(relative))
	inside, err := filepath.Rel(root, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", false
	}
	info, err := os.Stat(target)
	return target, err == nil && !info.IsDir()
}

// WorkspacePath resolves the directory itself for the settled-card and
// history affordances. The directory must already exist; rendering never
// creates workspaces or guesses at a missing job.
func (c *chatCommander) WorkspacePath(nodeID string) (string, bool) {
	if c == nil || c.store == nil || c.workspaceRoot == "" || nodeID == "" {
		return "", false
	}
	node, ok, err := c.store.Node(nodeID)
	if err != nil || !ok {
		return "", false
	}
	target := filepath.Join(c.workspaceRoot, jobIDOf(c.store, node))
	info, err := os.Stat(target)
	return target, err == nil && info.IsDir()
}

func (c *chatCommander) Notebook(limit int) []store.Fact {
	if c == nil || c.store == nil {
		return nil
	}
	facts, err := c.store.Facts(limit)
	if err != nil {
		return nil
	}
	if limit > 0 && len(facts) > limit {
		facts = facts[:limit]
	}
	return facts
}

func (c *chatCommander) SearchNotebook(terms string, limit int) []store.Fact {
	if c == nil || c.store == nil {
		return nil
	}
	facts, err := c.store.SearchFactsUncounted(store.FactQuery{
		Terms: strings.TrimSpace(terms), Limit: limit,
	})
	if err != nil {
		return nil
	}
	return facts
}

func (c *chatCommander) NotebookEvidence(seq int64) []string {
	if c == nil || c.store == nil || seq <= 0 {
		return nil
	}
	target, found, err := c.store.FactBySeq(seq)
	if err != nil || !found {
		return nil
	}
	all, err := c.store.Facts(0)
	if err != nil {
		return nil
	}
	bySeq := make(map[int64]store.Fact, len(all))
	for _, fact := range all {
		bySeq[fact.Seq] = fact
	}
	evidence := make([]store.Fact, 0)
	if target.NodeID != "" {
		evidence = append(evidence, target)
	}
	for _, fact := range all {
		if fact.EvidenceSeq == target.Seq {
			evidence = append(evidence, fact)
		}
	}
	if target.Unsettled != nil {
		for _, approach := range target.Unsettled.Approaches {
			for _, evidenceSeq := range approach.Evidence {
				if fact, ok := bySeq[evidenceSeq]; ok {
					evidence = append(evidence, fact)
				}
			}
		}
	}
	seen := make(map[string]bool)
	refs := make([]string, 0, len(evidence))
	for _, fact := range evidence {
		ref := strings.TrimSpace(fact.NodeID)
		if ref == "" || ref == store.RootID {
			ref = "#" + strconv.FormatInt(fact.Seq, 10)
		}
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

func (c *chatCommander) RetractNotebook(seq int64) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("notebook store unavailable")
	}
	fact, found, err := c.store.FactBySeq(seq)
	if err != nil {
		return err
	}
	if !found || fact.Status != store.FactActive {
		return fmt.Errorf("belief #%d is not active", seq)
	}
	if err := c.store.QuarantineFact(seq, 0, store.FactOriginUser); err != nil {
		return err
	}
	_, err = c.store.PostMessage(store.Message{
		SessionID: c.session(),
		Role:      store.RoleSystem,
		Body:      "· let go — " + firstLine(fact.Body),
	})
	return err
}

func (c *chatCommander) DatabasePath() string { return c.database }

func formatModelPrice(promptPrice, completionPrice float64) string {
	if promptPrice < 0 || completionPrice < 0 || promptPrice == 0 && completionPrice == 0 {
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

// messageClientPool keeps per-message chat overrides pinned to the exact model
// recorded on the durable user message. A later work-model change therefore
// cannot race an armed submission that the head has not tailed yet.
type messageClientPool struct {
	settings config.Config
	mu       sync.Mutex
	clients  map[string]*liveClient
}

func (p *messageClientPool) ForMessage(message store.Message) (head.Client, error) {
	model := strings.TrimSpace(message.Model)
	if model == "" {
		return nil, errors.New("boost message has no model")
	}
	return p.ForModel(model)
}

// ForModel is the same pinning seam seen from the graph side: one client per
// exact model slug, shared by every leaf that asked for it.
func (p *messageClientPool) ForModel(model string) (*liveClient, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("no model named")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if client := p.clients[model]; client != nil {
		return client, nil
	}
	client, err := newLiveClient(p.settings, model)
	if err != nil {
		return nil, err
	}
	if p.clients == nil {
		p.clients = make(map[string]*liveClient)
	}
	p.clients[model] = client
	return client, nil
}

// pinnedWorkClient honors the model a job's own words asked for. An
// unreachable slug degrades silently to the ordinary work client: the job
// still runs, which is the whole point of a preference.
func pinnedWorkClient(pool *messageClientPool, node store.Node) (*liveClient, bool) {
	model := strings.TrimSpace(node.Provenance.WorkModel)
	if pool == nil || model == "" {
		return nil, false
	}
	client, err := pool.ForModel(model)
	if err != nil || client == nil {
		return nil, false
	}
	return client, true
}

// resolveWorkModelWords is the surface's answer to the model words the
// compiler recognized: the boost slot for a slot word, catalog resolution
// inside the chat-capable candidacy filter for a name.
func resolveWorkModelWords(words head.ModelWords, models *catalog.Catalog, boost func() string) head.WorkModelChoice {
	if words.Boost {
		model := ""
		if boost != nil {
			model = strings.TrimSpace(boost())
		}
		return head.WorkModelChoice{Model: model, Requested: "the boost model"}
	}
	for _, name := range words.Names {
		matches := config.ModelMatches(models, head.ModelSlotWork, name, head.MaxModelCandidates)
		if len(matches) == 1 {
			return head.WorkModelChoice{Model: matches[0], Requested: name}
		}
		if len(matches) > 1 {
			return head.WorkModelChoice{Candidates: matches, Requested: name}
		}
	}
	requested := ""
	if len(words.Names) > 0 {
		requested = words.Names[0]
	}
	return head.WorkModelChoice{Requested: requested}
}

func newLiveClient(settings config.Config, model string) (*liveClient, error) {
	client, err := settings.ClientFor(model)
	if err != nil {
		return nil, err
	}
	return &liveClient{settings: settings, model: model, client: client}, nil
}

func (l *liveClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	_, client := l.Snapshot()
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
	closeReplaced(l.swap(model, client))
	return nil
}

// swap installs the new pair and returns the client it displaced, which the
// caller closes outside the lock — a client's Close flushes a ledger, and no
// model read should wait behind that.
func (l *liveClient) swap(model string, client router.Client) router.Client {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := l.client
	l.model, l.client = model, client
	return previous
}

// Close releases the underlying router client so its ledger flushes and its
// events handle is returned before the process exits.
func (l *liveClient) Close() {
	_, client := l.Snapshot()
	closeReplaced(client)
}

// Close releases every pinned per-model client the pool has handed out.
func (p *messageClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, client := range p.clients {
		client.Close()
	}
}

// closeReplaced releases a client that has just been swapped out.
//
// A router is not a value: it owns the append handle on router-events.jsonl and
// a queue of graded observations the run has already paid for. Every model
// switch used to drop one on the floor, which leaks the handle for the life of
// the process and loses whatever had not reached the ledger file yet. Closing
// is best-effort and idempotent; a plain adapter has nothing to close and is
// left alone.
func closeReplaced(client router.Client) {
	if closer, ok := client.(io.Closer); ok {
		_ = closer.Close()
	}
}

func waitWithGrace(group *sync.WaitGroup, grace time.Duration) {
	done := make(chan struct{})
	guard.Go("chat/background-wait", func() { group.Wait(); close(done) })
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
		(outcome.Promote || outcome.Overran())
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
// become profile records.
//
// This map was once defended as best-effort — "a restart forgets in-flight
// graphs and costs only telemetry, never work." That stopped being true when
// result-driven revision started reading the same map: editing a job's
// unstarted remainder is work, and a redirect that finds no graph used to
// answer with a receipt claiming the plan had been examined. So the graph is
// journaled on the job root at plan time and rehydrated on a lookup miss.
//
// The rehydrated copy is deliberately half-derived. Structure comes from the
// journal because a plan is a document that was written once; state comes from
// the durable graph because that is where what has happened is actually
// recorded, and a rehydrated plan that thought every node was still pending
// would hand the sentinel a licence to edit work already running.
type jobPlans struct {
	mu     sync.Mutex
	graphs map[string]plannedJob
	// journal persists a job's structure, and hydrate reads it back. Both are
	// nil on surfaces with no store to write to — `aforge wake` builds a
	// registry for one bounded pass and never outlives it — and a nil pair
	// leaves the registry exactly the memory-only map it used to be.
	journal func(prefix string, entry plannedJob)
	hydrate func(prefix string) (plannedJob, bool)
}

// newJobPlans builds the registry over a durable store, so a plan survives the
// process that made it. A registry built with the bare literal instead — the
// one bounded `wake` pass does — behaves exactly as it always has.
func newJobPlans(graph *store.Store) *jobPlans {
	plans := &jobPlans{graphs: map[string]plannedJob{}}
	if graph == nil {
		return plans
	}
	plans.journal = func(prefix string, entry plannedJob) {
		if entry.graph == nil {
			return
		}
		encoded, err := entry.graph.JSON()
		if err != nil {
			return
		}
		// Best-effort in the honest sense: losing this costs the ability to
		// revise the job's remainder after a restart, which is exactly what it
		// cost before the journal existed. It must never cost the plan itself.
		if err := graph.RecordPlanGraph(prefix, store.PlanGraph{
			Root: entry.root, Model: entry.model, Graph: encoded,
		}); err != nil {
			log.Printf("note: could not journal the plan for %s: %v", prefix, err)
		}
	}
	plans.hydrate = func(prefix string) (plannedJob, bool) {
		journaled, found, err := graph.PlanGraphFor(prefix)
		if err != nil || !found {
			return plannedJob{}, false
		}
		restored, err := plan.Load(journaled.Graph)
		if err != nil {
			log.Printf("note: journaled plan for %s could not be read: %v", prefix, err)
			return plannedJob{}, false
		}
		// The journal holds the plan as a document; the durable graph holds
		// what has since happened to it. Taking state from the store is not
		// belt-and-braces — a rehydrated plan that believed every node was
		// still pending would hand the sentinel a licence to rewrite work
		// already running, and the frozen rule is the one rule the whole
		// revision subsystem rests on.
		syncPlanState(graph, prefix, journaled.Root, restored)
		// No client: the leaves of a rehydrated job run on whatever work model
		// is current, and the caller's own fallback is what names it.
		return plannedJob{graph: restored, root: journaled.Root, model: journaled.Model}, true
	}
	return plans
}

// syncPlanState re-derives each plan node's state from the durable graph. It is
// deliberately one read of the job's subtree rather than one read per node: the
// whole point of rehydrating lazily is that it happens on a leaf's critical
// path, and a twenty-node job would otherwise pay twenty round-trips for it.
func syncPlanState(graph *store.Store, prefix, root string, restored *plan.Graph) {
	nodes, err := graph.SubtreeNodes(root)
	if err != nil {
		return
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	for index := range restored.Nodes {
		node := &restored.Nodes[index]
		id := fmt.Sprintf("%s-n%d", prefix, node.ID)
		stored, ok := byID[id]
		if !ok {
			// The sink is minted under the bare prefix, and a container node
			// the adapter dropped has no store node at all. Neither is evidence
			// that anything has happened to it.
			if stored, ok = byID[prefix]; !ok || stored.ID != root {
				continue
			}
		}
		node.State = planStateOf(stored.Status)
		if summary := strings.TrimSpace(stored.Summary); summary != "" {
			node.Result = summary
		}
		if failure := strings.TrimSpace(stored.Error); failure != "" {
			node.Failure = failure
		}
	}
}

// planStateOf maps a durable status onto the plan's own vocabulary. Cancelled
// lands on failed because the plan has no word for "deliberately dropped" and
// the property that matters downstream is the same either way: it produced
// nothing, and it is not editable.
func planStateOf(status store.Status) plan.State {
	switch status {
	case store.Done:
		return plan.StateDone
	case store.Failed, store.Cancelled:
		return plan.StateFailed
	case store.Claimed, store.Running:
		return plan.StateRunning
	default:
		return plan.StatePending
	}
}

// entry reads one retained job, rehydrating from the journal on a miss and
// caching what it finds. Callers hold the registry lock.
func (j *jobPlans) entry(prefix string) (plannedJob, bool) {
	if found, ok := j.graphs[prefix]; ok {
		return found, true
	}
	if j.hydrate == nil || prefix == "" {
		return plannedJob{}, false
	}
	restored, ok := j.hydrate(prefix)
	if !ok {
		return plannedJob{}, false
	}
	j.graphs[prefix] = restored
	return restored, true
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
	entry := plannedJob{graph: graph, root: root, model: model, client: client}
	j.graphs[prefix] = entry
	if j.journal != nil {
		j.journal(prefix, entry)
	}
}

// get reads one retained job without holding the registry across whatever the
// caller decides to do with it.
func (j *jobPlans) get(prefix string) (plannedJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.entry(prefix)
}

// lookup resolves a store node id back to its plan node. The job root uses the
// bare prefix so planning messages can name it before admission; other nodes
// retain "<prefix>-n<planID>".
//
// The prefix comes back with the graph because it is the graph's own key, and
// every caller that re-derived it from the id instead got it wrong somewhere:
// a planned root has no "-n" in it at all, and an overrun repair root has one
// that belongs to the job it repairs rather than to itself. Returning the key
// makes the namespace a fact the registry states rather than a string every
// caller re-parses.
func (j *jobPlans) lookup(nodeID string) (string, *plan.Graph, *plan.Node, string, router.Client) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if entry, ok := j.entry(nodeID); ok {
		if len(entry.graph.Nodes) == 0 {
			return nodeID, entry.graph, nil, entry.model, entry.client
		}
		return nodeID, entry.graph, &entry.graph.Nodes[len(entry.graph.Nodes)-1], entry.model, entry.client
	}

	cut := strings.LastIndex(nodeID, "-n")
	if cut < 0 {
		return "", nil, nil, "", nil
	}
	planID, err := strconv.Atoi(nodeID[cut+2:])
	if err != nil {
		return "", nil, nil, "", nil
	}
	prefix := nodeID[:cut]
	entry, ok := j.entry(prefix)
	if !ok {
		return "", nil, nil, "", nil
	}
	for index := range entry.graph.Nodes {
		if entry.graph.Nodes[index].ID == planID {
			return prefix, entry.graph, &entry.graph.Nodes[index], entry.model, entry.client
		}
	}
	return prefix, entry.graph, nil, entry.model, entry.client
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
//
// It returns the registry key alongside the graph. The key is the id namespace
// every node of that job was minted under, and it is the one thing the caller
// must not guess: a recalibration that slices the root id looking for "-n"
// finds nothing on a planned root, so it silently recorded no surprises at
// all — and on an overrun repair root it found the wrong one and wrote this
// job's surprises onto a sibling leaf of another.
func (j *jobPlans) takeIfRoot(nodeID string) (*plan.Graph, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if entry, ok := j.entry(nodeID); ok && entry.root == nodeID {
		delete(j.graphs, nodeID)
		return entry.graph, nodeID
	}

	cut := strings.LastIndex(nodeID, "-n")
	if cut < 0 {
		return nil, ""
	}
	prefix := nodeID[:cut]
	entry, ok := j.entry(prefix)
	if !ok || entry.root != nodeID {
		return nil, ""
	}
	delete(j.graphs, prefix)
	return entry.graph, prefix
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
func (j *jobPlans) reviseAfter(ctx context.Context, settings config.Config, client *liveClient, graph *store.Store, node store.Node, prefix string, planGraph *plan.Graph, summary string, artifacts []string, failure string, workerModel string) {
	if prefix == "" {
		return
	}
	entry, ok := j.get(prefix)
	if !ok || entry.root == node.ID {
		return
	}
	active, err := graph.ActiveNodes()
	if err != nil {
		return
	}
	pending := 0
	for _, sibling := range active {
		// The separator is not cosmetic: without it "task-14" claims every
		// pending node of "task-142", and the job pays for a full sentinel pass
		// — held under the registry lock — over another job's remainder.
		if sibling.Status == store.Pending && isJobNode(sibling.ID, prefix) && sibling.ID != entry.root {
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
		resident.RevisionEvent(node, summary, artifacts, failure))
	if err != nil || len(operations) == 0 {
		return
	}
	applied, notes := resident.ApplyRevision(graph, planGraph, prefix, entry.root, operations)
	if applied > 0 && j.journal != nil {
		// The journaled structure is now behind the graph in memory. Re-writing
		// it here rather than on every landing is the whole economy of the
		// arrangement: a revision is rare and changes the document, a landing is
		// constant and changes only what the store already records.
		j.journal(prefix, entry)
	}
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

// reviseForUser is reviseAfter's twin for the other event source. It holds the
// same registry lock for the same reason, and differs in exactly two places:
// the event is the user speaking with authority, and it does not return early
// when nothing is pending — the leaves already running still have to be told,
// and that broadcast is the reconciler's next move.
func (j *jobPlans) reviseForUser(ctx context.Context, settings config.Config, client *liveClient,
	graph *store.Store, job store.Node, message string,
	flavor resident.RevisionFlavor) (resident.Redirection, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok := j.entry(job.ID)
	if !ok {
		// Silence here was a lie with a receipt attached. An empty Redirection
		// and a nil error are indistinguishable from "the sentinel read the
		// plan and found nothing to change", so the user was told exactly that
		// while every pending leaf went on building the version they had just
		// asked to replace. The caller already owns an honest branch for a
		// revision that could not happen; this is how it reaches it.
		return resident.Redirection{}, errNoRetainedPlan
	}
	operations, _, err := plan.Revise(settings.Context(ctx, entry.graph.Goal), client, entry.graph,
		resident.UserRevisionEvent(message, flavor))
	if err != nil {
		return resident.Redirection{}, err
	}

	var revision resident.Redirection
	editable := make([]plan.Operation, 0, len(operations))
	for _, operation := range operations {
		id := fmt.Sprintf("%s-n%d", job.ID, operation.Node)
		if operation.Op == "remove" {
			if node, found, err := graph.Node(id); err == nil && found &&
				(node.Status == store.Running || node.Status == store.Claimed) {
				revision.RunningRemovals = append(revision.RunningRemovals, id)
				continue
			}
		}
		editable = append(editable, operation)
	}
	applied, notes := resident.ApplyRevision(graph, entry.graph, job.ID, entry.root, editable)
	revision.Notes = notes
	for _, operation := range editable {
		if !operation.Applied {
			continue
		}
		switch operation.Op {
		case "add":
			revision.Added++
		case "remove":
			revision.Dropped++
		case "rewire", "retitle":
			revision.Amended++
		}
	}
	if applied > 0 && j.journal != nil {
		j.journal(job.ID, entry)
	}
	return revision, nil
}

// errNoRetainedPlan says that this job's plan is not in hand — not that it
// needed no changes. The distinction is the whole of the redirect receipt's
// honesty, so it is a sentinel value rather than a formatted string.
var errNoRetainedPlan = errors.New("its plan is not in hand, so the remaining steps could not be re-read")

// isJobNode reports whether a store id belongs to the job minted under prefix.
// The separator is the entire content of the test: ids are minted as
// "<prefix>-n<planID>", so "task-14" is not a prefix of "task-142-n1" in any
// sense the graph means, however much it looks like one to strings.HasPrefix.
func isJobNode(id, prefix string) bool {
	return id == prefix || strings.HasPrefix(id, prefix+"-n")
}

// isSingleLeafJob asks the durable graph whether this job really was one leaf.
//
// The question used to be answered by "no plan graph is in hand", which is a
// fact about this process rather than about the job: after a restart a landed
// twelve-node project answered yes, and went into the durable profile as one
// direct leaf — biasing the planner's own ruler towards never decomposing
// anything. The store knows the shape whoever is asking; an error fails closed,
// because a measurement we cannot justify is worse than one we skip.
func isSingleLeafJob(graph *store.Store, node store.Node) bool {
	nodes, err := graph.SubtreeNodes(node.ID)
	if err != nil {
		return false
	}
	return len(nodes) <= 1
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
//
// The middle paragraph was added after a live failure the gate waved through. A
// worker asked to judge an architecture plan wrote its judgement into a file and
// ended with "the deliverable is written and verified against the actual repo
// source" — true, complete, and containing no verdict. That text became the
// node's summary, and the summary is the single source every later surface
// reads, so the answer existed nowhere the user or the head could reach it. The
// paragraph is stated as a value rather than a list of giveaway phrases,
// because the next way to describe work instead of doing it is always a phrasing
// nobody wrote down: the question is whether the substance is present, not
// whether some sentence pattern is.
const judgeDeliverablePrompt = `You are the final gate before a finished piece of work is handed to the person who asked for it. You receive their verbatim request, the compiled goal, and the deliverable as produced.

Judge exactly one question: would the person who asked accept this as done? Default to PASS. The gate exists for real gaps, not polish — wording, style, and things they never asked for are not gaps.

FAIL only when you can name a specific element of the request that is absent, unanswered, or unsupported by evidence the goal promised. Quote or name the missing element concretely enough that a worker could close the gap from your words alone.

Working decisions declared in the goal are part of what was promised. A commitment about method or evidence — what would be run, checked or reviewed before the work was handed over — is a gap when nothing in the deliverable shows it happened.

One absence counts exactly like every other and is the one most easily waved through: the substance itself. What you are handed IS the deliverable — it is the whole of what the person will read, and nothing beside it will be opened for them. So text that reports on the work rather than carrying it — that the work is finished, that a file now holds the answer, that the analysis was checked and is consistent — has described the deliverable in place of being it, and the element of the request that is absent is the answer: the verdict that was asked for, the findings, the numbers, the recommendation. Name that as the gap. A pointer to where the answer lives is not the answer however true the pointer is; naming the file is right beside the substance and never instead of it. This is still one absence and not a second style test: text that gives the answer in its own plain words passes whatever shape it takes.

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

// gateRevisionContract closes every revision, not only the ones whose named gap
// was a missing answer. The revision's own final message replaces the first
// attempt as the node's summary, and a second pass that closes a real gap inside
// a file and then reports that it did so has moved the original failure one
// round along rather than fixing it. The worker was told this once already in
// its own contract; a revision is the moment it demonstrably was not heard.
const gateRevisionContract = "Your final message is the deliverable and the only thing the person will read. " +
	"Put the substance in it — the verdict, the findings, the numbers they asked for — " +
	"and name the files beside that substance, never in place of it."

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
	// The standing half of the gate comes first and the job in front of it last,
	// which is both the reading order and the billing order. Settled taste is
	// the same text for every job in a session, so leading with it makes it the
	// one block the endpoint can hand back warm; the digest is retrieved per
	// node but identical across a node's repair passes, so it extends that warm
	// stretch through a revision. The request, the goal and the deliverable move
	// with every call and can invalidate nothing but themselves down here.
	//
	// Settled taste still leads the notebook material for the older reason: a
	// rule the user corrected their way to three times is not one lesson among
	// eight — it is the shape of an acceptable answer, and cannot be crowded out
	// by the digest's byte budget.
	var body string
	if taste := resident.TasteBlock(graph); taste != "" {
		body += "Settled taste — hold to these:\n" + taste + "\n\n"
	}
	if digest := resident.NotebookDigest(graph, node.ID, node.Brief, ask, 8); digest != "" {
		body += "Standing preferences and relevant lessons:\n" + clipUTF8Bytes(digest, gateNotebookBytes) + "\n\n"
	}
	body += "Verbatim request:\n" + ask + "\n\nCompiled goal:\n" + node.Brief + "\n\nDeliverable as produced:\n" + deliverable
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
				done <- landing{nil, guard.Note("chat/leaf executor", recovered)}
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

func planSubtree(settings config.Config, planClient, workClient *liveClient, plans *jobPlans, history *store.Store) resident.PlanFunc {
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
		// Structuring runs on the plan slot; the retained snapshot is the work
		// slot, because that is who the leaves run on and whose model the
		// profile key must name.
		workingModel, workingClient := workClient.Snapshot()
		_, structuring := planClient.Snapshot()
		progress := chatPlanProgress(history, anchor)
		graph, err := plan.Build(settings.Context(ctx, compiled.Goal), structuring, compiled.Goal, plan.Options{
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
		// The same run context the spine and briefs were built with: the contract
		// pass is the widest fan-out in the lineage, and without the run's cache
		// key its N concurrent calls scatter across providers and each writes the
		// shared prefix cold. It also carries the operator's reasoning setting.
		if _, err := plan.Contracts(settings.Context(ctx, compiled.Goal), structuring, graph, resident.ContractPlaybook(history), progress); err != nil {
			log.Printf("note: could not write contracts: %v", err)
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
func replanRemainder(settings config.Config, planClient, workClient *liveClient, plans *jobPlans, history *store.Store) resident.OverrunPlanFunc {
	return func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		workingModel, workingClient := workClient.Snapshot()
		_, structuring := planClient.Snapshot()
		anchor, _ := resident.PlanAnchorFromContext(ctx)
		progress := chatPlanProgress(history, anchor)
		graph, err := plan.Build(settings.Context(ctx, goal), structuring, goal, plan.Options{
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
		if _, err := plan.Contracts(settings.Context(ctx, goal), structuring, graph, resident.ContractPlaybook(history), progress); err != nil {
			log.Printf("note: could not write repair contracts: %v", err)
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
		// The job and the updates already spoken are the only append-only parts
		// of a narration: across the heartbeats of one job the goal never moves
		// and each new update is added to the end of a list whose earlier lines
		// are fixed. They lead, so successive narrations of the same job share
		// everything up to the new update. What is running and what just
		// finished are rewritten every time by definition, and sit below.
		fmt.Fprintf(&input, "The job: %s\n", narration.Goal)
		if len(narration.Previous) > 0 {
			input.WriteString("\nYour earlier updates (do not repeat):\n")
			for _, line := range narration.Previous {
				input.WriteString("- " + line + "\n")
			}
		}
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
		// A settled job keeps naming its original workspace after the store
		// reparents that fold beneath an organizational territory. Descendants
		// of the fold stop here on their next pass as well.
		if current.FoldRoot && !store.IsOrganizationalGroup(current.Group) {
			return current.ID
		}
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
const distillerSystemPrompt = `You judge whether a finished job taught an assistant anything worth keeping in its scoped notebook. You receive the goal, the outcome, and whether the job FAILED. Return exactly one JSON object: {"facts":[{"scope":"...","kind":"preference|quirk|lesson|fact|unsettled|skill|playbook|question","body":"...","unsettled":{"approaches":[{"approach":"...","scope":"...","evidence":[123]},{"approach":"...","scope":"...","evidence":[456]}]},"replaces":0,"skill":{"artifact":"/absolute/path/to/artifact-directory"}}]}.

Judgment framework:
- A memory qualifies only if it will matter after this job is forgotten.
- Scope every memory to the narrowest thing it is about: file:<absolute path> for a file's quirk, repo:<dir> for a codebase-wide one, tool:<name> for a tool's behaviour, domain:<topic> for subject knowledge, user for preferences, or env for machine facts.
- When the job FAILED, the single most valuable memory is the cause and its fix or workaround. Classify it as a quirk or lesson.
- When a FAILED job or a correction/revision of earlier delivered work exposes something the assistant did not know, emit one kind "question" knowledge gap in the narrowest scope. Phrase it as "I didn't know X" and name what execution could verify. Do not emit a question for ordinary successful work, communication preferences, or a gap the outcome already resolved.
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
- Return at most five memories.
- Make one last judgment about the job's SHAPE rather than about anything it taught: when the way this work was carried out could recur — several steps that fed each other, a deliverable at the end, the kind of request that comes back with different particulars — also emit one "craft": a reusable workflow file whose params are exactly the particulars that would change next time. Judge the shape alone. A single-step answer, a one-off investigation, and work whose steps were improvised for this situation only are not crafts, and most jobs have none. When the input says this job already ran a craft AND that craft's own shape is what went wrong, emit the corrected file under the SAME name; otherwise leave the craft out entirely.

The craft's yaml is a whole workflow file, and these are all of its fields:
name: lowercase-slug, matching the craft's name
description: one line saying what it is for, in the words a person would ask for it in
params: a list of {name, description, required: true} or {name, description, default: value}; a brief writes a param as {{name}}
steps: a list of {id: lowercase-slug, brief: what this one worker does, needs: [earlier-step-id], model: talk|work|boost, skill: executable-name, for_each: {source: earlier-step-id, fan: 4}, verify: {script: verifiers/name.sh, until_pass: {revise: [step-id], max_rounds: 2}}}
limits: {cost_usd: 2.5, minutes: 30}
A step has a brief or a verify, never both; a for_each step's brief writes {{item}} for the one item it handles. Everything but name and steps may be left out.

Example: {"facts":[],"craft":{"name":"release-notes","yaml":"name: release-notes\ndescription: turn a range of commits into user-facing release notes\nparams:\n  - name: since\n    description: the tag or date to start from\n    required: true\nsteps:\n  - id: collect\n    brief: Collect the commits since {{since}} and group them by area.\n  - id: write\n    brief: Write the release notes from the grouped commits.\n    needs: [collect]\n"}}`

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
	return func(ctx context.Context, title string, jobs []resident.TerritoryDigestJob, voice string) (string, error) {
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
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: territoryDigestSystemPrompt + voice}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input.String()}}},
		}, ai.WithMaxTokens(300))
		if err != nil || response == nil {
			return "", err
		}
		return strings.TrimSpace(response.Text()), nil
	}
}

const sentinelSystemPrompt = `You are a cheap standing-watch sentinel. Decide only whether the supplied condition occurred or the invariant is threatened now. Answer exactly "yes — <one line>" or "no — <one line>". No markdown, no qualifications, no suggested work.`

const morningBriefSystemPrompt = `You are the resident assistant writing one calm arrival fold after the person has been away. Return exactly one JSON object: {"headline":"While you were away: ...","items":[{"seq":123,"body":"..."}]}.

The input is journal truth. Write one short, human sentence for headline: begin exactly "While you were away:" and summarize the shape of what changed, including a waiting question or failure before routine progress. No greeting, dashboard language, hype, or "nothing to report".

Write exactly one slim item for every supplied event, in the same order, preserving its seq. Do not combine, omit, or invent events. Keep concrete names, results, questions, learned facts, and dollar amounts. Each item is one sentence fragment, at most 22 words. No markdown.`

var morningBriefSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "headline": {"type": "string"},
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "seq": {"type": "integer"},
          "body": {"type": "string"}
        },
        "required": ["seq", "body"],
        "additionalProperties": false
      }
    }
  },
  "required": ["headline", "items"],
  "additionalProperties": false
}`)

// composeMorningBrief is one small, routable resident verdict. Session
// surfaces never see this client; they only attach and render the message the
// resident journals. The panel may verify the JSON schema exactly as it does
// for other bounded planning verdicts.
func composeMorningBrief(settings config.Config, client *liveClient, graph *store.Store) resident.BriefComposeFunc {
	return func(ctx context.Context, activity resident.BriefActivity) (resident.BriefDraft, error) {
		input, err := json.Marshal(activity)
		if err != nil {
			return resident.BriefDraft{}, err
		}
		briefCtx := provider.WithCall(settings.Context(ctx, "morning-brief"), provider.ClassPlanBrief)
		options := []ai.Option{ai.WithMaxTokens(500)}
		if client.routed() {
			options = append(options, ai.WithSchema(morningBriefSchema))
		}
		// The first thing a person reads after being away is not the place to
		// disobey what they taught the assistant yesterday. "Stop opening with a
		// preamble" was learned, obeyed in ordinary replies, and then broken by
		// the one message they were guaranteed to read.
		response, err := client.CompleteWithMessages(briefCtx, []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text",
				Text: resident.VoicePrompt(graph, morningBriefSystemPrompt)}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: string(input)}}},
		}, options...)
		if err != nil || response == nil {
			provider.Report(briefCtx, provider.VerdictProviderFailure)
			if err == nil {
				err = fmt.Errorf("brief composer returned no response")
			}
			return resident.BriefDraft{}, err
		}
		object := jsonResponseObject(response.Text())
		var draft resident.BriefDraft
		if object == "" || json.Unmarshal([]byte(object), &draft) != nil || strings.TrimSpace(draft.Headline) == "" {
			provider.Report(briefCtx, provider.VerdictFormatFailure)
			return resident.BriefDraft{}, fmt.Errorf("brief composer returned malformed JSON")
		}
		provider.Report(briefCtx, provider.VerdictVerifiedSuccess)
		return draft, nil
	}
}

// checkSentinel reuses the resident talk client just like consolidation. The
// store, not this parser, decides whether a yes may spend or fire.
func checkSentinel(settings config.Config, client *liveClient) resident.SentinelFunc {
	return func(ctx context.Context, prompt resident.SentinelPrompt) (resident.SentinelVerdict, error) {
		input := fmt.Sprintf("Invariant (verbatim):\n%s\n\nSentinel hint:\n%s\n\nWake evidence:\n%s",
			prompt.Invariant, prompt.SentinelHint, prompt.Evidence)
		// The charter's own history, last: it is the only part of this prompt
		// that moves between wakes, and for a poll charter it is the only part
		// that moves at all. Without it the same judgment was made against the
		// same bytes an hour later, however the last one turned out.
		if len(prompt.Previous) > 0 {
			input += "\n\nYour last judgments on this same charter, newest first, and how each turned out:\n"
			for _, line := range prompt.Previous {
				input += "- " + line + "\n"
			}
		}
		system := sentinelSystemPrompt + prompt.Voice
		response, err := client.CompleteWithMessages(settings.Context(ctx, "sentinel"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: system}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input}}},
		}, ai.WithMaxTokens(60))
		if err != nil {
			return resident.SentinelVerdict{}, err
		}
		if response == nil {
			return resident.SentinelVerdict{}, fmt.Errorf("sentinel returned no response")
		}
		answer := strings.TrimSpace(response.Text())
		lower := strings.ToLower(answer)
		yes := strings.HasPrefix(lower, "yes")
		if !yes && !strings.HasPrefix(lower, "no") {
			return resident.SentinelVerdict{Line: "sentinel returned no clear yes"}, nil
		}
		line := answer
		if fields := strings.Fields(answer); len(fields) > 1 {
			line = strings.TrimSpace(strings.Join(fields[1:], " "))
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "—:- "))
		return resident.SentinelVerdict{Yes: yes, Line: line}, nil
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
		// The same call may now carry a whole workflow file, which is worth
		// several times what five one-line memories are: the ceiling is what
		// keeps a craft from being truncated into an invalid file.
		response, err := client.CompleteWithMessages(settings.Context(ctx, "distill"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: distillerSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input}}},
		}, ai.WithMaxTokens(1500))
		if err != nil || response == nil {
			return nil, err
		}
		facts := parseLearnedFacts(response.Text(), 5)
		if draft := parseCraftDraft(response.Text()); draft != nil {
			facts = append(facts, resident.Learned{Craft: draft})
		}
		return facts, nil
	}
}

// parseCraftDraft reads the one workflow file a distillation may carry beside
// its memories. Nothing is validated here: whether the file is a workflow is
// the craft parser's judgment, and its errors are the repair round's input.
func parseCraftDraft(raw string) *resident.CraftCandidate {
	object := jsonResponseObject(raw)
	if object == "" {
		return nil
	}
	var parsed struct {
		Craft *struct {
			Name string `json:"name"`
			YAML string `json:"yaml"`
		} `json:"craft"`
	}
	if err := json.NewDecoder(strings.NewReader(object)).Decode(&parsed); err != nil || parsed.Craft == nil {
		return nil
	}
	body := strings.TrimSpace(parsed.Craft.YAML)
	if body == "" {
		return nil
	}
	return &resident.CraftCandidate{Name: strings.TrimSpace(parsed.Craft.Name), YAML: body}
}

const craftRepairSystemPrompt = `You wrote a workflow file for an assistant's craft repository and the parser refused it. You receive the file exactly as you wrote it and the parser's own words. Return ONLY the corrected YAML file: no JSON, no prose, no code fence, nothing before or after it.

Fix exactly what the errors name and change nothing else. Each error names the step or field it is about and often what was meant instead. A field the errors call unknown is not a field this file has — remove it or use the one the error suggests. If an error says a step has neither a brief nor a verify, give it the one it was meant to have.`

// repairCraft is the one retry a refused candidate gets. The craft parser's
// errors were written to be read together and fixed in one edit; this hands
// them back to the only reader that can.
func repairCraft(settings config.Config, client *liveClient) resident.CraftRepairFunc {
	return func(ctx context.Context, candidate resident.CraftCandidate, problem string) (resident.CraftCandidate, error) {
		input := fmt.Sprintf("The file:\n%s\n\nWhat the parser said:\n%s", candidate.YAML, problem)
		response, err := client.CompleteWithMessages(settings.Context(ctx, "craft-repair"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: craftRepairSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input}}},
		}, ai.WithMaxTokens(1500))
		if err != nil || response == nil {
			if err == nil {
				err = fmt.Errorf("craft repair returned no response")
			}
			return resident.CraftCandidate{}, err
		}
		return resident.CraftCandidate{Name: candidate.Name, YAML: yamlResponseBody(response.Text())}, nil
	}
}

// yamlResponseBody strips the fence a model puts around a file even when it
// was told not to. Everything else is left exactly as written: leading
// whitespace is structure in YAML.
func yamlResponseBody(raw string) string {
	body := strings.TrimSpace(raw)
	if !strings.HasPrefix(body, "```") {
		return body
	}
	if _, rest, found := strings.Cut(body, "\n"); found {
		body = rest
	}
	if cut := strings.LastIndex(body, "```"); cut >= 0 {
		body = body[:cut]
	}
	return strings.TrimSpace(body)
}

const craftParamsSystemPrompt = `You read the values one stored workflow needs out of the request that matched it. You receive the request in the user's own words and the values the workflow declares, each with what it is for.

Return exactly one JSON object mapping each value you can actually read from the request to its value, phrased as the user phrased it: {"topic":"the Q3 numbers"}. Leave out anything the request does not say. A guessed value is worse than a missing one: a missing one makes the assistant plan the work from scratch, and a guessed one makes it do the wrong work confidently.`

// fillCraftParams reads a matched workflow's declared holes out of the
// request. It is the second half of a decisive match — a craft whose required
// values are not in the words the user used is not what they asked for.
func fillCraftParams(settings config.Config, client *liveClient) resident.CraftParamFiller {
	return func(ctx context.Context, instruction string, workflow *craft.Workflow) (map[string]string, error) {
		var wanted strings.Builder
		properties := make(map[string]any, len(workflow.Params))
		for _, param := range workflow.Params {
			fmt.Fprintf(&wanted, "- %s", param.Name)
			if description := strings.TrimSpace(param.Description); description != "" {
				fmt.Fprintf(&wanted, ": %s", description)
			}
			if param.Required {
				wanted.WriteString(" (required)")
			}
			wanted.WriteString("\n")
			properties[param.Name] = map[string]any{"type": "string"}
		}
		input := fmt.Sprintf("Request:\n%s\n\nWorkflow: %s — %s\n\nValues it needs:\n%s",
			instruction, workflow.Name, workflow.Description, wanted.String())
		options := []ai.Option{ai.WithMaxTokens(300)}
		// The schema is built per workflow because the values are: a fixed one
		// would either name nothing or name another craft's holes.
		if client.routed() {
			if schema, err := json.Marshal(map[string]any{
				"type": "object", "properties": properties, "additionalProperties": false,
			}); err == nil {
				options = append(options, ai.WithSchema(json.RawMessage(schema)))
			}
		}
		response, err := client.CompleteWithMessages(settings.Context(ctx, "craft-params"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: craftParamsSystemPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input}}},
		}, options...)
		if err != nil || response == nil {
			if err == nil {
				err = fmt.Errorf("craft params returned no response")
			}
			return nil, err
		}
		return parseCraftParams(response.Text()), nil
	}
}

func parseCraftParams(raw string) map[string]string {
	object := jsonResponseObject(raw)
	if object == "" {
		return nil
	}
	var parsed map[string]any
	if err := json.NewDecoder(strings.NewReader(object)).Decode(&parsed); err != nil {
		return nil
	}
	values := make(map[string]string, len(parsed))
	for key, value := range parsed {
		switch typed := value.(type) {
		case string:
			values[key] = strings.TrimSpace(typed)
		case float64, bool:
			values[key] = fmt.Sprint(typed)
		}
	}
	return values
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
	case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain, store.FactUnsettled, store.FactSkill, store.FactPlaybook, store.FactQuestion:
		return true
	default:
		return false
	}
}

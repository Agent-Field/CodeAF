package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/consent"
	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/rtk"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/aforge-v2/internal/voice"
	"github.com/Agent-Field/aforge-v2/internal/watchdog"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// runChat opens one window on a durable graph. Which half of aforge that window
// runs is not its decision: the first process on a store takes the resident
// role and runs the brain — a head that always replies, a reconciler that
// applies mutations, a runner that executes ready nodes — and every later
// window is a surface over the same journal until the role comes free. The
// one-shot plan/run path shares none of this and stays untouched.
func runChat(args []string) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	sessionID := flags.String("session", "", "thread session id; empty resumes the last one, \"new\" starts a fresh one")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true, "session": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge [chat] [--db path] [--session id|new]")
	}
	requestedSession := strings.TrimSpace(*sessionID)

	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create chat database directory: %w", err)
	}
	window, err := openChatWindow(path, strings.TrimSpace(*database), requestedSession)
	if err != nil {
		return err
	}
	defer window.close()

	role := newChatResidency(window)
	defer role.stop()
	if err := role.claim(); err != nil {
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

	err = tui.RunWithCommander(window.graph, window.session, role.commander())
	seenErr := role.sessionClosed()
	role.stop()
	return errors.Join(err, seenErr)
}

// brainOptions is what separates the two ways this machine is driven. There is
// one brain and one construction of it; these say which parts of it a
// particular driver has any use for.
//
// A chat window takes the zero value, which is the whole thing — head, voice,
// arrival brief, standing watch, the commander the surface reaches everything
// through. A headless one-shot takes headless, which removes exactly the
// conversational half and nothing else: the compiler, the reconciler, the
// runner, the contracts, the gate, the extensions and the replans are the same
// objects wired the same way, because a headless run that planned once and
// froze the plan would not be this system at all.
type brainOptions struct {
	// hand is the handover seam. Only a window that can stand down has one.
	hand resident.HandoverFunc
	// headless removes the head, the TUI commander, voice, the arrival brief,
	// the narrator, and the standing-watch installer. Nothing else.
	headless bool
	// ephemeral says the store evaporates when this process exits, so the
	// loops whose whole product is a durable record — craft, self-practice —
	// would be paying for something nobody can ever read.
	ephemeral bool
	// workspaceRoot overrides where jobs work. Empty is the store's own
	// workspace directory, which is what a window has always used.
	workspaceRoot string
	// sharedWorkspace makes every node work directly in workspaceRoot instead
	// of in a per-job subdirectory beneath it, and moves the harness's own
	// scratch out of it entirely.
	//
	// It is what an errand selects. A window hosts many unrelated jobs and
	// gives each one its own directory so they cannot trample each other; an
	// errand IS one job, aimed at a directory a person named, and the only
	// correct reading of "fix the bug in intervals.py -w ~/project" is that the
	// agent works in ~/project — opening the file that is there, editing it in
	// place, the way every other CLI agent does. Under the per-job layout it
	// started in an empty ~/project/task-2 instead, could not see the file it
	// was sent to fix, and invented one.
	sharedWorkspace bool
	// model and planModel are the per-run slot overrides. They outrank both the
	// environment and the persisted picker, because a flag is the most recent
	// thing the person said.
	model, planModel string
	// subharness forces every job this brain admits onto one worker. It is a
	// benchmarking instrument: a run comparing two workers on the same corpus
	// cannot let the choice be the variable it is measuring. Empty is the
	// ordinary path, where the compiler chooses and usually chooses nothing.
	subharness string
	// consent answers the price question for a desk with nobody at it. Nil is
	// the ordinary desk: it asks, and the job waits.
	consent func(store.Node, planEstimate) bool
	// newClient builds provider clients. Nil is the real one; a test scripts a
	// provider through it and drives the same brain every other caller drives.
	newClient func(config.Config, string) (*liveClient, error)
}

// buildChatBrain assembles the resident half of a window. It is the whole brain
// with a head on it, which is what a chat window is.
func buildChatBrain(w *chatWindow, session string, hand resident.HandoverFunc) (*chatBrain, error) {
	return buildBrain(w, session, brainOptions{hand: hand})
}

// buildBrain assembles the brain: every provider client, the reconciler, the
// runner, the consent desk, and — for a window — the commander that lets the
// surface reach all of it. It is a function rather than the body of runChat
// because a window may need it twice over — once at launch if it is the first
// on the store, and again if it starts as a visitor and later takes the role
// over — and because `aforge do` needs the same brain with the conversation
// taken off. Nothing here starts a goroutine or holds the terminal; start does
// that, once, on a brain that has already been built.
func buildBrain(w *chatWindow, session string, opts brainOptions) (*chatBrain, error) {
	brain := &chatBrain{window: w, session: session}
	path, database, graph := w.path, w.database, w.graph
	newClient := opts.newClient
	if newClient == nil {
		newClient = newLiveClient
	}
	settings, err := config.Load()
	if err != nil {
		if opts.headless {
			fmt.Fprintln(os.Stderr, "aforge do needs a model to work with.")
		} else {
			fmt.Fprintln(os.Stderr, "aforge chat needs a model to talk with.")
		}
		fmt.Fprintln(os.Stderr, "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.")
		return nil, err
	}
	applyModelFlags(&settings, opts.model, opts.planModel)
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
	// Every client built below shapes its requests against these rows: which
	// knobs a model accepts is the catalog's answer, not a guess.
	settings.Models = modelCatalog
	mediaClient, err := settings.MediaClient()
	if err != nil {
		return nil, err
	}
	visionClient, err := settings.VisionClient()
	if err != nil {
		return nil, err
	}
	documentClient, err := settings.DocumentClient()
	if err != nil {
		return nil, err
	}
	// A flag names the model for this run and outranks the picker, which is a
	// standing preference; the picker outranks the environment, which is a
	// default. With no flag — every chat window — this is the picker exactly as
	// before.
	talkModel := firstNonEmptyString(opts.model, prefs.ChatModel, settings.Model)
	workModel := firstNonEmptyString(opts.model, prefs.TaskModel, settings.Model)
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
	mediaModels := command.NewMediaModels(baseMedia, func(tools *exec.MediaTools) {
		tools.ImageModel = firstNonEmptyString(tools.ImageModel, settings.ResolveImageModel(modelCatalog))
		tools.SpeechModel = firstNonEmptyString(tools.SpeechModel, settings.ResolveSpeechModel(modelCatalog))
		tools.MusicModel = firstNonEmptyString(tools.MusicModel, settings.ResolveMusicModel(modelCatalog))
		tools.VideoModel = firstNonEmptyString(tools.VideoModel, settings.ResolveVideoModel(modelCatalog))
		tools.VisionModel = settings.ResolveVisionModel(modelCatalog, talkModel, workModel)
		if video, ok := modelCatalog.Model(tools.VideoModel); ok {
			tools.VideoPrice = video.RequestPrice
		}
	})

	chatClient, err := newClient(settings, talkModel)
	if err != nil {
		return nil, err
	}
	brain.closing(chatClient.Close)
	taskClient, err := newClient(settings, workModel)
	if err != nil {
		return nil, brain.abandon(err)
	}
	brain.closing(taskClient.Close)
	// The plan slot structures work — the task graph, replans, contracts, the
	// delivery gate. Empty follows the work model live, so by default this is
	// the same model behind a second hot-swappable handle; a picked plan model
	// or AFORGE_PLAN_MODEL splits structuring from execution, and a /model
	// change lands on the very next planning call.
	planModel := firstNonEmptyString(opts.planModel, prefs.PlanModel, settings.PlanModel, workModel)
	// How much the structuring model can hold, read once for the session. Every
	// budget on the planning side — what a reviser is shown of what happened,
	// what a claim-time division is shown of what landed, what the completion
	// gate is shown of the table it judges — is a share of this. Zero is the
	// catalog saying it cannot place the model, and each of those falls back to
	// the literal it carried before this number existed.
	planContextTokens := modelCatalog.ContextLength(planModel)
	planClient, err := newClient(settings, planModel)
	if err != nil {
		return nil, brain.abandon(err)
	}
	brain.closing(planClient.Close)
	installRoleLadder(graph, talkModel, planModel, workModel, settings.PlanModel, opts.planModel)
	boostClients := newMessageClientPool(settings)
	brain.closing(boostClients.Close)
	// The ruler stays keyed to the work model even when a different model
	// plans: the anchors measure how the executor spends turns, and the plan
	// model only reads them to size work for that executor.
	measured := installMeasuredRulers(settings.ProfileDir, taskClient.Model())
	// What each worker has actually cost, under its own name on the menu. The
	// hook is read at render time rather than captured, so a specialist that
	// crosses its evidence gate mid-session is grounded in that session.
	exec.UseSubharnessKnowledge(func(subharness string) string {
		return subharnessKnowledge(settings, taskClient.Model(), subharness)
	})

	// Every structuring call this surface makes now bills the same rail its
	// leaves bill. A journal write that fails is not a reason to fail the call
	// it is describing, so this is best-effort by construction — but it fails
	// loudly enough into the log to be findable.
	journalSpend := func(usage store.NodeUsage) {
		if err := graph.RecordUsage(usage); err != nil {
			log.Printf("note: could not journal structuring spend for %s: %v", usage.NodeID, err)
		}
	}
	chatClient.WithUsageJournal(journalSpend)
	taskClient.WithUsageJournal(journalSpend)
	planClient.WithUsageJournal(journalSpend)
	boostClients.WithUsageJournal(journalSpend)
	// The two structuring slots get a wall on a single completion; the work slot
	// deliberately does not. Everything the talk and plan slots do is one
	// round-trip that either answers or has stopped answering — compiling an ask,
	// titling it, writing briefs, setting contracts, judging a deliverable — and
	// none of it has an honest duration measured in minutes. A leaf is the other
	// kind of thing: an agent loop with the executor's own deadline over it, where
	// a long silence is often just a long tool call.
	chatClient.WithCallWall(pool.DefaultCallWall)
	planClient.WithCallWall(pool.DefaultCallWall)
	// The positive stopping condition, installed once for every path that can
	// grow a running job. It is asked last, after rounds, nodes and the daily
	// rail have all passed, so on the common path it is never asked at all; the
	// paths that ask it reach it through a package seam because they are
	// reached through signatures that carry a plan function and a budget and
	// have no client to give it. AFORGE_GROWTH_GATE=0 turns it off.
	resident.SetGrowthSatisfier(resident.SatisfierFor(planClient, planContextTokens))
	// The standing watch is a host timer: installing it shells out to launchctl
	// or systemctl and leaves something behind that outlives the process. A
	// one-shot command may not do that to a machine, so headless never builds
	// the manager and the reconciler's repair pass finds nothing to reconcile.
	var standingWatch *watchdog.Manager
	if !opts.headless {
		standingWatch, err = newStandingWatchManager(graph)
		if err != nil {
			return nil, brain.abandon(err)
		}
	}

	workspaceRoot := strings.TrimSpace(opts.workspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = home.StoreDir(path, "workspace")
	}
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		return nil, brain.abandon(fmt.Errorf("create chat workspace: %w", err))
	}
	// A shared workspace belongs to the person rather than to the run, so the
	// machinery that would otherwise pile up beside their files — spilled
	// observations, turn traces, background job logs — is sent to the store's
	// own directory, which for a one-shot evaporates with it.
	scratchRoot := ""
	if opts.sharedWorkspace {
		scratchRoot = home.StoreDir(path, "scratch")
	}
	// What the planner is allowed to see of the world before it plans. Only a
	// shared workspace holds the person's own material; in the per-job layout the
	// directory a job will work in does not exist yet, and the root above it holds
	// nothing but the other jobs' folders — which says nothing about this goal and
	// would spend prompt budget saying it. Empty renders nothing, so every
	// planning prompt in an ordinary chat session stays byte for byte what it is.
	terrainRoot := ""
	if opts.sharedWorkspace {
		terrainRoot = workspaceRoot
	}
	// Where this surface's jobs work, so a leaf promised a worker this build does
	// not have can say so once in its own flight recorder rather than degrading
	// in silence.
	seatLeafWorkerNotes(workspaceRoot, scratchRoot, graph)

	// The lease proves that no live resident can still own a claim in this DB.
	// A closed terminal mid-run can otherwise leave leaves stranded as
	// "running" forever. Release them back to pending before the
	// reconciler starts, and say so once: recovered work resumes rather than
	// haunting the rail.
	//
	// What it does NOT do is continue a transcript. The executor is rebuilt from
	// nothing and the worker starts at turn zero; only the workspace directory
	// survives, because the job id that names it is stable. The old sentence
	// here promised the opposite — the surface's own bootstrap breaking the
	// house rule against describing a behaviour nobody recorded — so it now says
	// what actually happens and what actually survives.
	if released, err := graph.ReleaseOrphans(); err == nil && len(released) > 0 {
		_, _ = thread.Post(graph, store.Message{
			SessionID: session,
			Role:      store.RoleSystem,
			Body: fmt.Sprintf("picked up %d piece(s) of work that were interrupted — each starts again from the beginning, with the files it had already written still where it left them",
				len(released)),
		})
	}
	if err := resident.ReAdoptServices(graph, session, nil); err != nil {
		return nil, brain.abandon(fmt.Errorf("re-adopt services: %w", err))
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
	// An ephemeral store has no craft: the repository would be born in a
	// directory that is deleted a minute later, so recognising a learned
	// workflow could never find one and forging a new one would throw it away.
	var craftRunner *resident.CraftRunner
	var craftShelf resident.CraftShelf
	craftDir := ""
	if !opts.ephemeral {
		if craftRepo, craftErr := craft.Open(filepath.Join(filepath.Dir(database), "craft")); craftErr == nil {
			craftRunner = resident.NewCraftRunner(graph, craftRepo, craftRepo.Dir())
			craftShelf, craftDir = craftRepo, craftRepo.Dir()
		} else {
			log.Printf("note: craft repository unavailable: %v", craftErr)
		}
	}
	// The commander owns the live boost slot, and it is built further down;
	// the resolver reads it through this handle so a later /model change is
	// what the next job's model words resolve against.
	var commander *chatCommander
	reconciler := newResidentReconciler(settings, graph, chatClient, taskClient, planClient, plans, terrainRoot,
		func(words head.ModelWords) head.WorkModelChoice {
			return resolveWorkModelWords(words, modelCatalog, func() string {
				if commander != nil {
					return commander.CurrentModel(head.ModelSlotBoost)
				}
				return taskClient.Model()
			})
		}, opts.headless)
	// Narration, the arrival brief, redirection and the host timer are all one
	// thing: a conversation. Each of them speaks into a thread, or waits for
	// somebody to speak into it, or leaves something on the machine that
	// outlives the process. A one-shot has none of those, so it buys none of
	// the calls they cost.
	if !opts.headless {
		reconciler = reconciler.
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
	}
	// Self-practice is curiosity spent on a notebook. An ephemeral store's
	// notebook is deleted with it, so the practice would be paid for and
	// unreadable — and it would be competing with the one errand this process
	// was started to run.
	if opts.ephemeral {
		reconciler = reconciler.WithPracticeLoop(0, 0)
	}
	if craftRunner != nil {
		reconciler = reconciler.WithCraftRunner(craftRunner)
	}
	// A restart that names a model resolves it through the same catalog the
	// compiler's model words use; without this seam every "rerun that on the
	// better model" honestly reports falling back to the default.
	reconciler = reconciler.WithModelResolver(func(names []string, boost bool) (string, bool) {
		choice := resolveWorkModelWords(head.ModelWords{Names: names, Boost: boost}, modelCatalog, func() string {
			if commander != nil {
				return commander.CurrentModel(head.ModelSlotBoost)
			}
			return taskClient.Model()
		})
		return choice.Model, choice.Model != ""
	})
	// The lease's flock proves the process exists; the heartbeat proves it is
	// doing the work. Without this stamp a wedged TUI holds the resident role
	// while every wake pass defers to it, and the standing watches go blind.
	reconciler = reconciler.WithHeartbeat(func(at time.Time) {
		_ = lease.NoteResidentTick(path, at)
	})
	// The handover seam, and the moment it is measured against. A request
	// journaled before this process took the role belongs to whoever was
	// serving then; answering it would make a window that has just promoted
	// stand straight back down, and the role would circle the open windows
	// forever.
	reconciler = reconciler.WithHandover(opts.hand).WithResidentSince(time.Now())
	// A forced worker takes the choice away from the compiler for this whole
	// process. It is what a measurement run asks for and nothing else asks for.
	if forced := resolveSubharnessFlag(opts.subharness, os.Stderr); forced != "" {
		reconciler = reconciler.WithSubharness(forced)
	}
	// Recognition and forging ride the resident's own talk client, like every
	// other small verdict it makes about itself.
	if craftShelf != nil {
		reconciler = reconciler.WithCraftMind(resident.NewCraftMind(craftShelf, craftDir,
			fillCraftParams(settings, chatClient), repairCraft(settings, chatClient)))
	}
	if err := reconciler.AttachSession(session); err != nil {
		return nil, brain.abandon(err)
	}
	// The attach edge is journalled now, because its ordering is what fixes the
	// brief's window. Composing that brief is a model round-trip over a journal
	// gather, and it used to run here, in front of the first frame — the user
	// waited on the network to be told what happened while they were away. The
	// thread is the delivery channel, so the composition rides behind the
	// surface and the brief lands in the same place a moment later.
	// A one-shot has not been away and has nobody to greet, so there is no
	// arrival and no brief to compose for it.
	var deliverBrief func(context.Context) error
	if !opts.headless {
		brief, briefErr := reconciler.SessionOpening(session, "tui", settings.BriefAfter)
		if briefErr != nil {
			log.Printf("note: could not prepare the arrival brief: %v", briefErr)
		}
		deliverBrief = brief
	}

	web := exec.NewWeb()
	// chatWorkerCeiling is not a concurrency policy — the provider's adaptive
	// rate limiter is the real throttle. This is a local sanity bound on
	// goroutines/file handles, far above any realistic graph width.
	const chatWorkerCeiling = 32
	// The price before the purchase. It is built here so the runner can consult
	// it on the very first tick of a claimed leaf, which is the last moment at
	// which not starting the work is still free.
	desk := newConsentDesk(graph).WithHeadless(opts.consent)
	desk.Rehydrate()
	runner := resident.NewRunner(graph, func(ctx context.Context, node store.Node) (resident.ExecResult, error) {
		isReflex := node.Group == resident.ReflexGroup
		// Nothing above this line spends anything, which is the whole point of
		// its position: a job whose estimate crosses the threshold is held and
		// asked about before its first worker has said a word. The runner's
		// landing path reads the hold and releases the claim, so this returns
		// nothing and the node goes back to the queue rather than lying about
		// having finished or failed.
		if desk.Gate(settings, measured, node) {
			return resident.ExecResult{}, nil
		}
		// Each top-level job works in its own directory: one thread hosts
		// many unrelated jobs, and continuity between them travels through
		// the graph as digests and absolute paths, never through a shared
		// folder they could trample.
		//
		// An errand is the one case with nothing to trample. It is a single job
		// pointed at a directory somebody named, every node of it — leaves,
		// gate-bought extensions, overrun continuations — shares that directory,
		// and the files already in it are the work.
		jobDir := workspaceRoot
		if !opts.sharedWorkspace {
			jobDir = filepath.Join(workspaceRoot, jobIDOf(graph, node))
		}
		jobSpace, err := exec.NewWorkspace(jobDir)
		if err != nil {
			return resident.ExecResult{}, err
		}
		if scratchRoot != "" {
			jobSpace = jobSpace.WithScratch(scratchRoot)
		}
		staged, err := exec.StageAttachments(jobSpace, attachmentStoreRoot(path), node.Provenance.Attachments)
		if err != nil {
			return resident.ExecResult{}, err
		}
		documentPaths := staged.Documents
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
		escalatable := taskClient.Escalatable()
		if pinned, ok := pinnedWorkClient(boostClients, node); ok {
			workingModel, workingClient = pinned.Snapshot()
			escalatable = pinned.Escalatable()
		}
		// What runs this leaf was settled when the job was admitted, and the
		// node's row carries the answer. Reading it here rather than deciding
		// it here is the whole point of journaling it: a leaf claimed an hour
		// after its splice runs on what it was promised.
		subharness := leafSubharness(node)
		// Ordinary leaves retain the byte-identical headless envelope. Reflexes use
		// the deliberately tiny rung budget and a seconds-scale watchdog.
		turns, tokens := chatLeafTurns, chatLeafTokens
		deadline := exec.SubharnessFor(subharness).Deadline(tokens)
		watchdog := deadline + 2*time.Minute
		if isReflex {
			turns, tokens = reflexTurns, reflexTokens
			deadline = reflexDeadline
			watchdog = deadline + 15*time.Second
		}
		leafMedia := mediaModels.Snapshot()
		leafMedia.WorkingModel = workingModel
		leafMedia.VisionModel = settings.ResolveVisionModel(modelCatalog, chatClient.Model(), workingModel)
		// A media purchase is the one spend in the system that is quoted before
		// it is made, so it is the one place a rail can refuse rather than
		// discover. Both rails are asked, in the order they bind: the day's
		// ceiling governs the machine, and a task ceiling governs one subtree —
		// a leaf inside a governed job must not spend the job's last dollars on
		// a video while the rest of the job is being held for the same reason.
		// An ungoverned graph pays nothing for the second question: the store's
		// first act is to ask whether any ceiling exists at all.
		leafMedia.BeforeSpend = func(_ context.Context, additional float64) error {
			if settings.DailyBudgetUSD > 0 {
				rail, _, gateErr := graph.PauseDailyRailWithAdditionalSpend(settings.DailyBudgetUSD, node.Provenance.SessionID, additional)
				if gateErr != nil {
					return gateErr
				}
				if rail.Reached {
					return fmt.Errorf("daily budget reached")
				}
			}
			task, _, gateErr := graph.PauseTaskRail(node.ID, node.Provenance.SessionID, additional)
			if gateErr != nil {
				return gateErr
			}
			if task.Reached {
				return fmt.Errorf("this task's budget is reached")
			}
			return nil
		}
		// The wiring one worker is built from, kept rather than spent, because a
		// second attempt may be given to a different kind of worker: everything
		// here is a fact about the leaf and none of it is a fact about who runs
		// it, so the same build serves whichever one does.
		build := leafBuild{
			settings: settings, client: workingClient, workspace: jobSpace, web: web,
			graph: graph, media: &leafMedia, model: workingModel, models: modelCatalog,
			maxTurns: turns, maxTokens: tokens, deadline: deadline,
		}
		worker := executorFor(subharness, build)
		shape := "atomic"
		if isReflex {
			shape = "reflex"
		}
		if planNode != nil {
			shape = exec.LeafShape(planNode)
			// Frozen means frozen everywhere: the sentinel may not edit a
			// node whose transcript is already being written.
			plans.markRunning(planGraph, planNode)
		}
		// One ledger bucket per worker and no finer. What a router learns about
		// a specialist says nothing about a generalist leaf, and a key any
		// finer than this never accumulates enough graded outcomes to mean
		// anything — see exec.LeafShape, which splits on the same principle.
		if exec.KnownSubharness(subharness) {
			shape = subharness
		}

		// Every input is named. Untitled, they render as `=== from "" ===`
		// under a header that says the results are prior work the leaf already
		// has and must not gather again — so a standing lesson reading "check X
		// before Y" arrived as a claim that X had been checked. The notebook is
		// not a prior result and says so; a dependency says whose it is.
		inputs := leafNotebookInputs(graph, node)
		dependencies, err := graph.DependencyInputs(node.ID, build.dependencyPot())
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
		outputHint, intermediate := leafOutputHint(node, leafTitle, jobSpace)
		// The job blackboard: what one worker learns mid-flight that changes how
		// the others must act — a discovery about the material, a pitfall, a
		// decision to match. A note is an agent message anchored to the job's
		// root, and every leaf of the job (the sink included) reads the root at
		// the same between-turn boundary that already reads user steering. A
		// leaf that starts late reads the whole board on its first poll because
		// its cursor starts at zero; a single-leaf job carries neither the tool
		// nor the poll, so the atomic path pays nothing.
		jobRoot := jobIDOf(graph, node)
		// Which board rows this leaf wrote. A leaf must not read its own notes
		// back, and it used to know which were its own by finding its own TITLE
		// glued to the front of them — so every note carried a clipped node title
		// colon-joined to a sentence, and the room showed lines like
		// "⚑ Assemble the three country sections into a: Brief saved to …": a
		// name cut mid-phrase, wearing a colon it never earned, in front of the
		// one part of the line that says anything. The row's own sequence answers
		// the same question exactly, so the note is just the note.
		mine := map[int64]bool{}
		var share func(string) error
		if node.Parent != store.RootID {
			share = func(line string) error {
				// The board is workers talking to workers. It is read off the job
				// root by node, never by session, so it belongs to the record and
				// nothing is lost by keeping the conversation out of it — a person
				// watching a job saw one ⚑ line per note per leaf, which is the
				// machinery's internal correspondence delivered to their inbox.
				posted, postErr := thread.Record(graph, store.Message{
					Role:   store.RoleAgent,
					NodeID: jobRoot,
					Body:   jobNoteBody(line),
				})
				if postErr == nil {
					mine[posted.Seq] = true
				}
				return postErr
			}
		}
		// Reading the board costs one indexed query per turn boundary and zero
		// tokens when it is empty, so every leaf reads it — including the sink,
		// whose merge is exactly where a sibling's warning matters most.
		var boardCursor int64
		board := func() []string {
			messages, boardErr := graph.NodeMessages(jobRoot, boardCursor, 20)
			if boardErr != nil {
				return nil
			}
			var lines []string
			for _, message := range messages {
				boardCursor = message.Seq
				note, isNote := jobNoteLine(message)
				if !isNote || mine[message.Seq] {
					continue
				}
				lines = append(lines, note)
			}
			return lines
		}
		task := exec.Task{
			Reflex:     isReflex,
			Subharness: subharness,
			NodeID:     int(node.CreatedSeq),
			// The identity everything this leaf writes is filed under, and it is
			// the node's own id for the same reason its output path is (see
			// leafOutputHint): the creation sequence belongs to the whole splice.
			// Under it, four siblings of one job shared one artifact bucket, one
			// flight recorder and one set of .obs spill names — so each was told
			// the others' files were its own, their recorders interleaved into a
			// single document, and a spilled observation was overwritten by a
			// sibling's while the stub in its context still pointed at the file.
			NodeKey:     node.ID,
			StoreNodeID: node.ID,
			Title:       firstLine(node.Brief),
			Goal:        node.Provenance.Intent,
			Brief:       withDocumentAttachmentBrief(residentDeliveryBrief(graph, node), documentPaths),
			Contract:    leafContract(plans, planNode, node),
			// The whole object, beside the two halves of it the executor
			// already reads. Nothing in the generic loop renders it today; it
			// is here so a worker that speaks a spec is handed one rather than
			// having it reassembled from prose at the boundary (W3).
			Spec:         leafSpec(plans, planNode, node),
			OutputHint:   outputHint,
			Intermediate: intermediate,
			Inputs:       inputs,
			Steer:        steer,
			Share:        share,
			Board:        board,
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
			ImagePaths:    append([]string(nil), staged.ImageFiles...),
			DocumentPaths: append([]string(nil), documentPaths...),
			// Within-node progress, through the one channel the thread already
			// has for "this is still happening": the same replaceable rows a
			// compile posts, anchored to the job rather than typed into it.
			// The generalist passes nothing here and pays nothing for it; a
			// worker whose leaf runs for the better part of an hour would
			// otherwise be a spinner, and the two alternatives — splicing its
			// insides into the graph, or narrating them as messages — are the
			// two things the subharness law forbids by name.
			Progress: leafProgress(graph, resident.PlanAnchor{
				NodeID: jobRoot, SessionID: node.Provenance.SessionID,
			}),
		}
		// The scheduler's quality loop, inline: each attempt is one routable
		// unit carrying its call shape, a watchdog sits above the leaf's own
		// deadline so a wedged executor becomes a recorded failure rather
		// than a silent hang, and a leaf whose verdict says a stronger model
		// might fix it gets exactly one escalation when a panel offers one.
		//
		// A stronger model is not the only second rung there is. A leaf whose
		// work is, in its essence, the thing a registered specialist exists for
		// has somewhere else to go — a different KIND of worker rather than a
		// bigger version of the same one — and that is the rung the boundary
		// between the two rulers is actually made of. It is offered here, on the
		// existing loop, and the menu it is chosen from excludes whoever just
		// failed, so no worker is ever handed back its own failure.
		//
		// In a build with no specialist the menu is empty, the condition below
		// reads exactly as it always did, and not one extra call is made.
		specialists := exec.MenuTextExcept(subharness)
		attempts := 1
		if !isReflex && (escalatable || specialists != "") {
			attempts = 2
		}
		// escalatedFrom remembers that this leaf reached its worker through a
		// failure rather than through a choice. It is the one piece of evidence
		// that says a boundary sits too high — "the generalist could not, this
		// one could" — and it is worth nothing unless it survives into the
		// profile record, which is where recalibration reads it.
		escalatedFrom := ""
		// One job is one cache lineage, exactly as one headless run is: the
		// affinity key rides every leaf of the job so a prefix cache warmed
		// by one worker serves its siblings.
		ctx = provider.WithCacheKey(ctx, provider.RunCacheKey(node.Provenance.Intent, workingModel))
		var outcome *exec.Outcome
		var spent exec.Usage
		spentTurns := 0
		workerModel := taskClient.Model()
		// A bundle sink with nothing to reconcile is assembly, not judgment:
		// the parts are self-contained deliveries and the board is silent, so
		// joining them in the asked order is geometry — measured, a model sink
		// re-typing a 1,863-word part spent 121 of a 212-second job saying
		// what the parts had already said. When the board carries notes or a
		// part came back dirty, the model sink runs exactly as before, and if
		// the gate finds a contradiction in a mechanical join, its revision
		// buys the model pass with the critique in hand — reconciliation on
		// demand instead of re-emission by default.
		mechanical := false
		if node.Group == resident.BundleGroup {
			if joined, ok := assembledBundle(graph, node); ok {
				outcome = &exec.Outcome{Text: joined, Verdict: provider.VerdictUnverifiedSuccess}
				mechanical = true
			}
		}
		for attempt := 0; !mechanical && attempt < attempts; attempt++ {
			// An escalation that repeats the task verbatim buys a stronger model
			// and then pays it to rediscover everything the first attempt found —
			// including files sitting in the shared workspace it is about to
			// write again. The gate's revision pass has always been sighted this
			// way; the escalation was the one retry that was not.
			if attempt > 0 && outcome != nil {
				attempted := task
				attempted.Inputs = append(append([]exec.Input{}, inputs...),
					previousAttemptInput(outcome, jobDir))
				// Who takes the retry, asked once, of the same judge machinery
				// that already reads failures. An empty menu never reaches here.
				if chosen := revision.JudgeRetryWorker(ctx, settings, planClient, node,
					attempted, outcome, err, specialists, workerModel); chosen != "" {
					escalatedFrom = subharness
					if escalatedFrom == "" {
						escalatedFrom = exec.LinearSubharness
					}
					subharness = chosen
					attempted.Subharness = chosen
					// The row this function goes on to read for the profile
					// key. Without it the specialist's leaf would be measured
					// into the generalist's file — the one the generalist's
					// ruler is rewritten from — and one coding pipeline's forty
					// minutes would teach the planner that ordinary leaves are
					// enormous.
					node.Subharness = chosen
					// Durable, because the promise has to outlive this process:
					// a leaf whose retry is interrupted and claimed again must
					// be claimed by the worker it was moved to, not by the one
					// that already failed at it.
					if _, changeErr := graph.SetNodeSubharness(node.ID, chosen,
						"escalated from "+escalatedFrom+" after a failed attempt"); changeErr != nil {
						log.Printf("note: could not journal the worker change for %s: %v", node.ID, changeErr)
					}
					if planNode != nil {
						plans.markWorker(planGraph, planNode, chosen)
					}
					// The budget shape belongs to the worker, not to the leaf:
					// the generalist's fifteen-minute backstop applied to a
					// coding pipeline is a guillotine at its first merge.
					build.deadline = exec.SubharnessFor(chosen).Deadline(tokens)
					watchdog = build.deadline + 2*time.Minute
					worker = executorFor(chosen, build)
					shape = chosen
				}
				task = attempted
			}
			runCtx := provider.WithCallShape(settings.ExecContext(ctx), provider.ClassExecLeaf, attempt, shape)
			outcome, err = runLeafWithWatchdog(runCtx, worker, task, watchdog)
			if model := provider.CallFrom(runCtx).Model(); model != "" {
				workerModel = model
			}
			if outcome != nil {
				spent.PromptTokens += outcome.Usage.PromptTokens
				spent.CompletionTokens += outcome.Usage.CompletionTokens
				// The share of those prompt tokens the provider billed at the
				// cached rate. It rides with them or the journal reads every
				// leaf as a cold run: the executor counts it correctly and the
				// runner writes it, and for as long as this line was missing
				// the only rows carrying a cache count were the small
				// structuring calls — so the table anybody audits said 91%
				// cache hits were 0%.
				spent.CachedTokens += outcome.Usage.CachedTokens
				spent.Cost += outcome.Usage.Cost
				spentTurns += outcome.Turns
			}
			if err == nil && outcome != nil && !outcome.Verdict.Escalates() {
				break
			}
		}
		if planNode != nil && !mechanical {
			// A join that cost nothing is not a measurement of any model.
			plans.recordOutcome(planGraph, planNode, outcome, err, escalatedFrom)
		}
		if err == nil && outcome != nil && (outcome.Stop == exec.StopPaused || outcome.Stop == exec.StopCancelled) {
			// A cancel is news for the plan above this leaf, and it is the one
			// ending that never told it anything. A pause is not: the work is
			// coming back, and nothing about the remainder has changed. The
			// sentinel is asked before the early return because this is the last
			// moment the partial is in hand — after this the claim goes back and
			// the node settles cancelled somewhere else entirely.
			if outcome.Stop == exec.StopCancelled && !isReflex && planGraph != nil {
				plans.reviseAfterCancel(ctx, settings, planClient, graph, node, planPrefix, planGraph,
					outcome.Text, store.UserCancelReason, workerModel)
			}
			result := leafSpend(spent, workerModel)
			result.Summary = outcome.Text
			result.ServiceRequests = outcome.ServiceRequests
			return result, nil
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
						record, ok := recordSingleLeaf(settings, workerModel, node, outcome, escalatedFrom)
						if ok {
							recordProfileSurprise(graph, node.ID, record)
						}
					})
				}
			}
			// A failed leaf usually leaves something behind. The files are on
			// disk in the job's workspace and, until now, no surface in the
			// product knew they existed: the error path threw the outcome away,
			// the summary stayed empty, and the fold pointers had no absolute
			// path to find. Naming them in the error text is enough — the error
			// is what a failed node records, and a downstream step now reads
			// paths out of it the same way it reads them out of a summary.
			failure := humanFailure(node, err, absolute)
			// A sibling's failure is the board note nobody should have to
			// remember to write: the workers still running are about to lean on
			// a result that is not coming, and the reason it died is the fact
			// they need. Structural — the reason already exists; no model call.
			if share != nil {
				// The board is workers talking to workers, but it is written onto
				// the job root's thread, which is a room a person is watching. So
				// it gets the same cause clause: a sibling needs to know what
				// died and why, and neither of them needs the attempt count or
				// the JSON body to know it. The writer's identity is the row's,
				// not the sentence's, exactly as jobNoteBody already says.
				_ = share("did not finish — " + firstLine(failure.Error()))
			}
			if len(absolute) > 0 {
				// One part of a job stopping is not the job's failure, and the
				// files it left are the record's business: the whole-task failure
				// names what died (failedPartsNote), and whoever wants the
				// half-written files opens the part that wrote them.
				recordOnNode(graph, node.ID,
					"it stopped before finishing, but it had already written these — they are yours to keep or hand to a retry:\n"+
						strings.Join(absolute, "\n"), store.RoleSystem)
			}
			// The error says the leaf produced nothing; it says nothing about
			// what producing nothing cost. Escalation has usually run the whole
			// task twice by the time we arrive here, so this is the most
			// expensive kind of result there is — and returning a bare zero
			// value is what made real spend journal as $0.00 on the daily rail.
			return leafSpend(spent, workerModel), failure
		}
		// The user's next act is opening the file, so the summary carries where
		// it actually lives; the absolute paths were resolved above.
		text := outcome.Text
		if len(absolute) > 0 {
			text += "\n\nFiles:\n" + strings.Join(absolute, "\n")
		}
		// notes are what the system owes the person ABOUT the work, kept apart
		// from the work itself for the whole of this function and joined only at
		// the end. text is always the final state of the deliverable and nothing
		// else — a revision replaces it whole — so no round of a dispute can
		// leave its prose in front of the answer. See composeDelivery.
		var notes []string
		continuing := false
		// Resource exhaustion is invisible: it grows the graph and the final
		// assembled deliverable reaches the gate. Semantic failure stays honest
		// and still lands with the evidence from the failing leaf.
		//
		// But exhaustion alone is not evidence of unfinished work. A leaf that
		// is told to land within its reserve often lands complete — one real
		// run split a finished, self-described "complete and verified"
		// inventory into 27 rounds of invented verification, because the
		// replan was asked to find a remainder rather than whether one exists.
		// So the judge runs first: a checked "done" ships the result as-is,
		// and a named gap becomes the replan's target instead of a guess.
		if !isReflex && outcome.Overran() {
			remainder := revision.JudgeRemainder(ctx, settings, planClient, graph, node, text,
				exec.MenuTextExcept(promisedWorker(node)), workerModel)
			if remainder.Checked && remainder.Done {
				outcome.Verdict = provider.VerdictVerifiedSuccess
			} else {
				spliced, _, replanErr := resident.ReplanOverrunOn(ctx, graph, node, outcome.Text, remainder.Remaining, absolute,
					settings.DailyBudgetUSD, remainder.Worker,
					replanRemainder(settings, planClient, taskClient, plans, graph, terrainRoot))
				if replanErr == nil && spliced > 0 {
					continuing = true
					notes = append(notes, "["+continuationMessage(spliced)+"]")
					// How work was divided is the machinery's own arithmetic. The
					// person asked for a result, not for a count of pieces, and the
					// receipt on the summary above already tells every reader
					// downstream that this node's last word is not its last word.
					recordOnNode(graph, node.ID, continuationMessage(spliced), store.RoleSystem)
				} else if replanErr == nil {
					// A zero splice at the rail is a pause, not a final partial. The
					// question and deferred remainder are journaled; the reconciler
					// resumes the split after the head records consent. A zero
					// splice from a governor cap is final: the rail check below
					// stays false and the partial delivers as the result.
					if rail, err := graph.DailyRailToday(settings.DailyBudgetUSD); err == nil {
						continuing = rail.Reached
					}
					if continuing {
						// Continuing suppresses the announcement, which is right for
						// a job that will speak again in a minute and wrong for one
						// waiting on a human. Without this the user got the budget
						// question and no result line at all, while a real partial
						// sat finished in the graph. It is posted here rather than
						// left to the announcer because the announcer is the thing
						// being suppressed.
						_, _ = thread.Post(graph, store.Message{
							SessionID: node.Provenance.SessionID,
							Role:      store.RoleSystem,
							NodeID:    node.ID,
							Body: boundedDelivery(text) +
								"\n\nThat is as far as today's budget goes. The rest is planned and waiting on the rail — raise it and I'll carry on.",
						})
					}
				}
			}
		}
		promoted := shouldPromoteReflex(node, outcome)
		extended := false
		// The quality gate: before a deliverable lands in the thread, one
		// judge call asks the only question that matters — would the person
		// who asked accept this as done? A named gap earns exactly one
		// revision pass with the critique as input.
		//
		// A gap that survives that pass used to be the end of the road: the
		// draft shipped with a reservation on it, because a gate that can loop
		// is a gate that can stall. It can now buy one thing instead — the work
		// that closes it, through the same path an exhausted leaf takes, on the
		// one condition that the gap quotes the ask. That condition is what makes
		// looping impossible rather than merely capped, so the old sentence is
		// still true of a gate that loops on its own judgement, and this is not
		// one: it loops on the user's words, which are finite and do not move.
		if len(outcome.ServiceRequests) == 0 && shouldGate(node, outcome, continuing) {
			// A mechanical join was never a watched worker run: its parts were
			// the watched runs, so the gate reads the joined text on its own
			// merits rather than convicting an assembly for calling no tools.
			records := gateEvidence(node, task.Spec, outcome, absolute, !mechanical)
			gate := revision.JudgeDeliverable(ctx, settings, planClient, graph, node, text, task.Contract,
				records, workerModel)
			if gate.Checked {
				evidence := store.DeliveryGate{Pass: gate.Pass, Gap: gate.Gaps, Quote: gate.Quote}
				if gate.Pass {
					outcome.Verdict = revision.GateVerdict(gate)
				}
				// The grounding check the extension has always had, applied one
				// layer earlier: to the revision round. A gap the review cannot
				// quote from the ask or from the working method is a standard
				// this system set for itself after seeing its own output, and a
				// round bought against a moving standard cannot converge — it
				// produced the one measured round that made a deliverable worse.
				// So it downgrades to a note: journaled, said, carried on the
				// delivery, and free.
				ungrounded, closed := "", ""
				if !gate.Pass {
					ungrounded = revision.AdmitGapRevision(node.Provenance.Intent, task.Contract, gate.Quote)
					// The same refusal, for the gap the record has already
					// closed rather than the one the request never set. A span
					// of the ask naming a file the run produced is not a thing
					// the person asked for and did not get, and the round it
					// used to buy was spent retyping a correct file into a
					// message while the file itself sat in the workspace.
					if ungrounded == "" {
						closed = revision.AdmitGapArtifact(gate.Quote, records)
					}
					// And the same refusal for the gap the deliverable itself
					// has already closed. The artifact half asks the disk; this
					// half asks the text the person is about to read, which is
					// the half that was missing when a gate looked at twelve
					// verbatim profiles and reported that the twelve were not
					// there. A repair round bought on that verdict replaced a
					// correct answer with a broken one.
					if ungrounded == "" && closed == "" {
						closed = revision.AdmitGapPresent(gate.Quote, text)
					}
				}
				switch {
				case ungrounded != "":
					evidence.Refused = ungrounded
					// It rides the delivery as its own short note after the work
					// and is not also posted on its own. Posted separately it
					// arrived BEFORE the announcement — the review's words
					// standing in front of an answer the review was wrong about,
					// which is the shape the panel scored 2/5.
					notes = append(notes, revision.GapNote(gate.Gaps, ungrounded))
					// The verdict is deliberately left where the worker put it.
					// Nothing about the work was shown to be wrong here; a judge
					// invented a requirement, and charging the model's rating for
					// that would teach the profile the reviewer's mistake.
				case closed != "":
					evidence.Refused = closed
					notes = append(notes, revision.GapClosedNote(gate.Gaps, closed))
					// Same reasoning, one step stronger: the work produced what
					// the gap says is missing, so the worker's own verdict is
					// the accurate one and the review's is not.
				}
				if !gate.Pass && ungrounded == "" && closed == "" {
					// unmet is the judgement that still stands against whatever is
					// about to be delivered: the second gate's when a revision ran
					// and was re-judged, the first gate's when nothing came back to
					// re-judge. It is what any further work would be aimed at, so it
					// is also what the honest handover has to name.
					unmet := gate
					revised := false
					// repair is the one revision round a failed gate buys: the
					// same task, plus the critique and the draft it is aimed at.
					repair := task
					repair.Inputs = append(append([]exec.Input{}, inputs...), exec.Input{
						Title:     "a review of your own first draft",
						Artifacts: append([]string(nil), absolute...),
						Result: "A reviewer compared the previous attempt against the original request and found gaps that must be closed:\n" + gate.Gaps +
							"\n\nThe previous attempt (build on it, fix the gaps, do not start over):\n" + text +
							"\n\n" + revision.GateRevisionContract,
					})
					retryCtx := provider.WithCallShape(settings.ExecContext(ctx), provider.ClassExecLeaf, 1, shape)
					polished, polishErr := runLeafWithWatchdog(retryCtx, worker, repair, deadline+2*time.Minute)
					polishModel := workerModel
					if model := provider.CallFrom(retryCtx).Model(); model != "" {
						polishModel = model
					}
					if polishErr == nil && polished != nil && strings.TrimSpace(polished.Text) != "" {
						spent.PromptTokens += polished.Usage.PromptTokens
						spent.CompletionTokens += polished.Usage.CompletionTokens
						spent.CachedTokens += polished.Usage.CachedTokens
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
						closed := revision.JudgeDeliverable(ctx, settings, planClient, graph, node, text, task.Contract,
							gateEvidence(node, task.Spec, outcome, absolute, true), polishModel)
						evidence.PolishClosed = closed.Checked && closed.Pass
						outcome.Verdict = provider.VerdictSemanticFailure
						revised = true
						if evidence.PolishClosed {
							// The same distinction the first gate makes; drawing it
							// only there would launder the verdict one round later.
							outcome.Verdict = revision.GateVerdict(closed)
						} else if closed.Checked && strings.TrimSpace(closed.Gaps) != "" {
							// The second reading is the current one: it was taken
							// against the revised text, so it is what any further
							// work is aimed at and what the citation is checked on.
							unmet = closed
							evidence.Gap, evidence.Quote = closed.Gaps, closed.Quote
						}
					} else {
						outcome.Verdict = provider.VerdictSemanticFailure
					}
					switch {
					case evidence.PolishClosed:
						// A settled dispute leaves no trace in the conversation.
						// The revision closed the gap, so the finished work is
						// the whole of what happened as far as the person is
						// concerned; the round, its critique and its verdict are
						// in the gate ledger, where the belt tools read them. The
						// line that used to be posted here — "a review found gaps
						// in the first draft" — landed just before the answer and
						// made a repaired deliverable read as a doubted one.
					default:
						// The gap survived the one revision, which is exactly where
						// the old path gave up. The job may grow the work that closes
						// it — once per span of the ask, inside the same round caps,
						// rails and consent an exhausted leaf lives under.
						extension := revision.ExtendForGap(ctx, graph, node, outcome.Text, unmet, absolute,
							settings.DailyBudgetUSD, replanRemainder(settings, planClient, taskClient, plans, graph, terrainRoot))
						evidence.Quote, evidence.Round = extension.Quote, extension.Round
						evidence.Extended, evidence.Refused = extension.Spliced > 0, extension.Refused
						if extension.Spliced > 0 {
							extended = true
							// The receipt on the summary is what tells every reader
							// downstream that this node's last word is not its last
							// word; the line on the record says why rather than only
							// what, for whoever opens the part it happened in.
							notes = append(notes, "["+continuationMessage(extension.Spliced)+"]")
							recordOnNode(graph, node.ID,
								revision.GapContinuationNotice(unmet.Gaps), store.RoleSystem)
							break
						}
						// Nothing more will run, and the rejected draft ships anyway
						// because a job that cannot finish still owes the person what
						// it has. What must not also happen is that it ships in
						// silence: for a while the verdict was recorded, no message
						// was posted, and the user read a draft the system had already
						// judged incomplete as though it were the answer. The
						// reservation rides the delivery so it cannot be missed and
						// cannot be separated from it, and it names the gap and why
						// nothing more was started — which is what makes the user's
						// next sentence land in the correction path. It is a note
						// AFTER the work and only that: said once, last, and never
						// standing in front of what was actually produced.
						notes = append(notes, revision.GapHandover(unmet.Gaps, revised, extension.Refused))
					}
				}
				_ = graph.RecordDeliveryGate(node.ID, evidence)
			}
			// The gate judges the request. Taste is the other half and is never
			// allowed to be a gate: an unproven rule rides one quiet question
			// with the delivery, which lands either way. A job that is about to
			// carry on asks nothing yet — the question belongs to the delivery
			// that actually lands.
			if !extended {
				_, _, _ = resident.AnnotateDelivery(graph, node, text)
			}
		}
		// A settled failure downstream of a synthesis node is terminal by
		// design, so the parent goes Ready over it, receives `<id> (failed):
		// <error>` as one input among many, and writes the confident summary any
		// synthesis writes. The user then reads an interruption line and, right
		// under it, an answer that behaves as though nothing was missing. What
		// the parent knows and does not say is exactly what has to be said.
		if node.Parent == store.RootID {
			notes = append(notes, failedPartsNote(graph, node))
		}
		text = composeDelivery(text, notes)
		outcome.Text = text
		outcome.Usage = spent
		outcome.Turns = spentTurns
		if planNode != nil {
			plans.recordOutcome(planGraph, planNode, outcome, nil, escalatedFrom)
		}
		if landed, prefix := plans.takeIfRoot(node.ID); landed != nil {
			// The recalibration report reaches the job's RECORD, not a stdout the
			// TUI owns and not the conversation; detached, because the ruler is
			// telemetry and the user's result must not wait on it. The namespace
			// comes from the registry rather than from slicing the id: this
			// root's own id IS the prefix, so the old "-n" search never matched
			// and recalibration for a planned chat job simply never ran.
			//
			// "ruler: median 19 turns, 4 of 18 overran — the ruler holds" is the
			// sentence this writes, and it was going into the conversation. It is
			// a measurement the system takes of itself, in the system's own
			// vocabulary, about work the person has already been handed — the
			// clearest possible case of the record's business.
			nodeID := node.ID
			guard.Go("chat/recalibrate", func() {
				report, records := recordAndCalibrateDetailed(settings.Context(context.Background(), landed.Goal), planClient, settings, workingModel, landed)
				recordPlanSurprises(graph, prefix, records)
				recordOnNode(graph, nodeID, strings.TrimSpace(report), store.RoleSystem)
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
					record, ok := recordSingleLeaf(settings, workerModel, node, outcome, escalatedFrom)
					if ok {
						recordProfileSurprise(graph, node.ID, record)
					}
				})
			}
		}
		result := leafSpend(spent, workerModel)
		result.Summary = text
		result.Promote = promoted
		result.ServiceRequests = outcome.ServiceRequests
		return result, nil
	}, "chat-runner", chatWorkerCeiling).
		WithDailyBudgetUSD(settings.DailyBudgetUSD).
		// The depth loop, asked at the claim instead of at the build. See
		// cmd/aforge/jit.go; a nil hook here is the whole rollback.
		WithExpand(jitExpander(graph, plans, settings, planClient, planContextTokens).Expand)
	if craftRunner != nil {
		runner = runner.WithCraftRunner(craftRunner)
	}

	brain.settings = settings
	brain.reconciler = reconciler
	brain.runner = runner
	brain.consent = desk
	brain.workspaceRoot = workspaceRoot
	// Everything below this line is the conversation: the commander the surface
	// reaches capabilities through, the microphone, the stream the reply is
	// typed into, and the head that does the replying. A headless run has no
	// surface to reach anything, nothing to listen to, nobody to stream at, and
	// no routing to do — the task IS the work order. The brain above is
	// complete and identical either way.
	if opts.headless {
		return brain, nil
	}
	// A window's shutdown is somebody closing a terminal, which says nothing at
	// all about whether the work should stop. A one-shot's is the wall it was
	// given, which says exactly that — so the grace belongs to this side of the
	// line and `aforge do` keeps cancelling on the instant.
	brain.leafGrace = windowLeafGrace

	streamEvents := make(chan tui.StreamEvent, 256)
	transcriber, err := voice.NewClient(voice.ClientConfig{
		APIKey: settings.APIKey, BaseURL: settings.BaseURL, Timeout: settings.Timeout,
		SiteURL: settings.SiteURL, SiteName: settings.SiteName,
	})
	if err != nil {
		return nil, brain.abandon(err)
	}
	commander = command.New(command.Options{
		Settings:         settings,
		Database:         path,
		PrefsDir:         filepath.Dir(path),
		WorkspaceRoot:    workspaceRoot,
		ChatClient:       chatClient,
		TaskClient:       taskClient,
		PlanClient:       planClient,
		Store:            graph,
		Prefs:            prefs,
		SessionID:        session,
		StreamEvents:     streamEvents,
		VoiceRecorder:    voice.NewSystemRecorder(),
		VoiceTranscriber: transcriber,
		Models:           modelCatalog,
		MediaModels:      mediaModels,
		AttachSession:    reconciler.AttachSession,
		// The two seams the commander borrows from this process: where a job's
		// files live, and what a work-model switch means to the measured ruler.
		JobID: func(node store.Node) string { return jobIDOf(graph, node) },
		InstallRuler: func(model string) {
			installMeasuredRulers(settings.ProfileDir, model)
		},
	})

	// The head is built here rather than inside the loop below because the
	// surface has to be able to reach it: stopping the turn being answered right
	// now is a handle on this process, and the commander is where the surface
	// keeps its handles.
	conversationalHead := head.New(chatClient, graph).
		// Where the artifact door writes. Unset, the head writes into the
		// process's working directory, which is the right default for a person
		// typing in a terminal and the wrong one for a chat serving a job
		// workspace: a diagram born beside the binary is a diagram nobody finds
		// beside the work it belongs to. The commander already resolved this
		// root for every other file the job touches, so the head uses the same.
		WithWorkspace(workspaceRoot).
		// How much the talk model holds, which is what every block of the head's
		// prompt is sized from. The catalog lives here, on the surface, and the
		// head is handed the fact — the same doctrine the linear leaf's own
		// window takes (subharness.go). A model the catalog cannot size answers
		// zero, and zero leaves the head on the literals it shipped with rather
		// than on a guess about a window nobody knows.
		WithContextLength(modelCatalog.ContextLength(talkModel)).
		// The pool answers with its own concrete client; the head asks for its
		// own interface. The lift is written out rather than passed as a method
		// value so a failed pin returns a nil interface rather than a non-nil
		// one wrapping a nil pointer.
		WithMessageClient(func(message store.Message) (head.Client, error) {
			pinned, pinErr := boostClients.ForMessage(message)
			if pinErr != nil {
				return nil, pinErr
			}
			return pinned, nil
		}).
		WithSelfKnowledge(func() string { return selfKnowledge(settings, taskClient.Model()) }).
		WithImageInput(modelCatalog, settings.Model).
		WithCompetenceMap(func() string {
			return competenceGrounding(graph, settings.ProfileDir, taskClient.Model())
		}).
		WithStandingWatch(func() string {
			return watchGrounding(path, graph, standingWatch, settings.DailyBudgetUSD)
		}).
		// This window draws a list of rooms, so its rooms get names: after the
		// first exchange in a room, the head's clerk titles it on the scribe rung
		// of the ladder. Everything past this line is the conversation, and a
		// headless brain never reaches it — which is exactly the window that has
		// no rail to name anything for.
		WithRoomNaming(true).
		WithDailyBudgetUSD(settings.DailyBudgetUSD)
	commander.SetHead(conversationalHead)

	brain.commander = commander
	brain.streamEvents = streamEvents
	brain.deliverBrief = deliverBrief
	// Routing is a structuring call, and it was the one loop served with a bare
	// context: without the configured effort knob, a reasoning model spends the
	// head's whole token cap deliberating and returns empty text — measured as
	// 600/600 completion tokens of thought and zero answer on the default model.
	brain.serveHead = func(ctx context.Context) {
		headContext := provider.WithStreamObserver(settings.Context(ctx, "head"), func(event provider.StreamEvent) {
			kind, known := headStreamKind(event.Kind)
			if !known {
				// A boundary this build has never heard of is DROPPED rather than
				// mapped to whatever the zero value happens to be. The zero value
				// is StreamStarted, which resets the live region — so the old
				// unguarded switch turned every future provider boundary into a
				// wiped reply. See headStreamKind.
				return
			}
			translated := tui.StreamEvent{Kind: kind, Delta: event.Delta, Session: event.Session}
			select {
			case streamEvents <- translated:
			case <-ctx.Done():
			}
		})
		// The rooms that existed before the naming clerk did get their one chance
		// here, off this goroutine and bounded — see head.BackfillRoomNames. It
		// belongs beside the room grooming that already runs at launch
		// (groomChatRooms, store.ReapEmptySessions) and not inside it, because
		// grooming is a query against the journal and this one spends a model
		// call: it needs the head, and the head only exists once the brain does.
		conversationalHead.BackfillRoomNames(headContext)
		_ = conversationalHead.Serve(headContext)
	}
	return brain, nil
}

// headStreamKind maps the provider's stream vocabulary onto the window's, and
// says when it could not.
//
// IT REPORTS FAILURE BECAUSE THE ZERO VALUE IS A REAL BOUNDARY. tui.StreamStarted
// is ordinal zero and means "wipe the live region and start again"; a switch
// that silently left an unrecognised kind at the zero value therefore did the
// most destructive possible thing with the least information. Two vocabularies
// only stay in step if the seam between them can say "I do not know this one".
//
// The tool-activity boundaries ride here with the rest (internal/head/
// activity.go emits them): they cross into the window on the same channel the
// tokens do, because they are the same turn happening.
func headStreamKind(kind provider.StreamEventKind) (tui.StreamEventKind, bool) {
	switch kind {
	case provider.StreamStarted:
		return tui.StreamStarted, true
	case provider.StreamDelta:
		return tui.StreamDelta, true
	case provider.StreamThinking:
		return tui.StreamThinking, true
	case provider.StreamFinished:
		return tui.StreamFinished, true
	case provider.StreamFailed:
		return tui.StreamFailed, true
	case provider.StreamToolBegin:
		return tui.StreamToolBegin, true
	case provider.StreamToolEnd:
		return tui.StreamToolEnd, true
	case provider.StreamToolFailed:
		return tui.StreamToolFailed, true
	}
	return 0, false
}

// chatBrain is the resident half of a window: the head that replies, the
// reconciler that applies the command journal, the runner that executes ready
// leaves, and the consent desk that prices work before it is bought. A window
// has one for as long as it holds the resident role and none the rest of the
// time.
//
// It keeps two contexts rather than one, and that is the whole reason it is a
// type. An ordinary shutdown ends both, because the terminal is going away. A
// handover ends only the first: the head must stop answering the instant this
// process is no longer the brain, while the leaves already running belong to
// claims in the store and must be allowed to land.
type chatBrain struct {
	window       *chatWindow
	session      string
	settings     config.Config
	commander    *chatCommander
	reconciler   *resident.Reconciler
	runner       *resident.Runner
	consent      *consentDesk
	streamEvents chan tui.StreamEvent
	deliverBrief func(context.Context) error
	// serveHead is nil in a headless brain, which is the one structural
	// difference between the two: no head means nothing routes, and the command
	// journal is written by the caller instead.
	serveHead func(context.Context)
	// workspaceRoot is where this brain's jobs work, so a caller that gave one
	// can find what was written without guessing at the id law.
	workspaceRoot string
	// leafGrace is how long an ordinary shutdown lets the leaves already
	// running finish before it takes their context away. Zero — every headless
	// caller, and every test that does not ask otherwise — cancels at once,
	// which is what a one-shot's wall means.
	leafGrace time.Duration

	closers    []func()
	background sync.WaitGroup
	cancel     context.CancelFunc
	runCancel  context.CancelFunc
	runDone    chan struct{}
	stopOnce   sync.Once
	closeOnce  sync.Once
}

// closing registers something built here that must be given back. They are
// collected rather than deferred because construction returns the brain to a
// caller that will run it, so the unwind of the constructor is exactly the
// wrong moment to close a client.
func (b *chatBrain) closing(close func()) {
	b.closers = append(b.closers, close)
}

// abandon gives back everything built so far and returns the error that ended
// construction, so a half-built brain never leaks the clients it opened.
func (b *chatBrain) abandon(err error) error {
	b.closeAll()
	return err
}

func (b *chatBrain) closeAll() {
	b.closeOnce.Do(func() {
		for i := len(b.closers) - 1; i >= 0; i-- {
			b.closers[i]()
		}
	})
}

// start runs the brain. Every loop it owns lives on this side of the call, so
// a window that has just promoted starts exactly what a window that launched
// as the resident starts, in the same order.
func (b *chatBrain) start() {
	ctx, cancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	b.cancel, b.runCancel = cancel, runCancel
	b.runDone = make(chan struct{})
	graph, session := b.window.graph, b.session

	// The leaves' shell compressor fetches itself here if it is missing, off
	// the startup path and outside the wait group: nothing waits on it, nothing
	// fails without it, and until it lands every command runs plain.
	guard.Go("chat/rtk", func() { rtk.Bootstrap(ctx) })

	if b.serveHead != nil {
		b.background.Add(1)
		go func() {
			// Registered first so it absorbs last: the channel close and the wait
			// group both settle on the unwind before the fault is recorded.
			defer guard.Recover("chat/head")
			defer b.background.Done()
			defer close(b.streamEvents)
			b.serveHead(ctx)
		}()
	}
	b.background.Add(2)
	guard.Go("chat/reconciler", func() {
		defer b.background.Done()
		superviseResident(ctx, b.reconciler.Serve, residentRestartBackoff, residentHealthyRun, func(body string) {
			_, _ = thread.Post(graph, store.Message{
				SessionID: session, Role: store.RoleSystem, Body: body,
			})
		})
	})
	go func() {
		defer guard.Recover("chat/runner")
		defer b.background.Done()
		defer close(b.runDone)
		// A dispatch loop that gives up is the quietest failure this process
		// has: the reconciler keeps ticking, the board keeps rendering, and
		// nothing is ever claimed again. It now takes ten consecutive failed
		// passes to get here, so arriving with anything but a cancellation is
		// news worth writing down.
		if err := b.runner.Serve(runCtx); err != nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("note: the work dispatcher stopped claiming: %v", err)
		}
	}()
	b.background.Add(1)
	guard.Go("chat/consent", func() { defer b.background.Done(); b.consent.Serve(ctx) })

	if b.deliverBrief != nil {
		b.background.Add(1)
		go func() {
			defer guard.Recover("chat/arrival-brief")
			defer b.background.Done()
			if err := b.deliverBrief(ctx); err != nil {
				log.Printf("note: could not deliver the arrival brief: %v", err)
			}
		}()
	}
}

// standDown is the handover shutdown. The head, the reconciler and the consent
// desk end at once, because from this moment another process is answering for
// this store and two of anything would be a race. The runner is only told to
// stop claiming: its running leaves keep the context they started with until
// they land, and the clients they are still talking through are given back
// after — never before.
func (b *chatBrain) standDown() {
	if b.cancel != nil {
		b.cancel()
	}
	b.runner.Drain()
	guard.Go("chat/stand-down", func() {
		<-b.runDone
		if b.runCancel != nil {
			b.runCancel()
		}
		b.closeAll()
	})
}

// stop is the ordinary shutdown: the terminal is going away, so the half of
// this brain that was talking to it goes with it.
//
// The work does not, and that asymmetry is the whole point. This used to cancel
// both contexts on the same line, which meant closing a chat window killed
// every leaf mid-POST: three nodes of one job died on the millisecond the
// surface detached, five milliseconds after the seen edge was journaled, and
// the resident then read `context canceled` as a flaky provider and learned to
// add retries. A product whose premise is background work may not make the
// person's terminal the lifetime of the work.
//
// So it takes the two steps standDown has always taken — end the conversation,
// drain the dispatcher, let the running leaves land on the context they started
// with — and only then takes that context away. The wait is bounded, because a
// window being closed must eventually close; what makes the bound safe rather
// than merely polite is that a leaf whose context ends this way is now released
// back to pending instead of failed (see the runner's landing path), so
// whatever the grace does not cover is picked up next time rather than lost.
func (b *chatBrain) stop() {
	b.stopOnce.Do(func() {
		if b.cancel != nil {
			b.cancel()
		}
		b.runner.Drain()
		b.awaitLanding()
		if b.runCancel != nil {
			b.runCancel()
		}
		waitWithGrace(&b.background, 5*time.Second)
		b.closeAll()
	})
}

// leafSettleNotice is how long a shutdown waits in silence before it admits it
// is waiting. Below this, the leaves land in the time it takes the terminal to
// repaint and saying anything would be noise.
const leafSettleNotice = 250 * time.Millisecond

// awaitLanding gives the leaves already in flight their bounded chance to
// finish. It says so on the way past, because a terminal that does not come
// back for a minute with nothing on it is indistinguishable from a hang — and
// the sentence has to name the way out, since the way out is safe.
func (b *chatBrain) awaitLanding() {
	if b.runDone == nil || b.leafGrace <= 0 {
		return
	}
	select {
	case <-b.runDone:
		return
	case <-time.After(leafSettleNotice):
	}
	fmt.Fprintf(os.Stderr,
		"finishing the work already running before closing (up to %s) — ctrl+C leaves it to be picked up next time\n",
		b.leafGrace)
	timer := time.NewTimer(b.leafGrace)
	defer timer.Stop()
	select {
	case <-b.runDone:
	case <-timer.C:
		fmt.Fprintln(os.Stderr, "still running when the grace ran out — it goes back on the queue and resumes next time")
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

// leafNotebookInputs is the whole of how what aforge has learned reaches the
// work: one retrieval against this leaf's own brief and goal, rendered into the
// first input the worker reads.
//
// It is a named seam rather than four lines inside the runner because it is the
// last link in the chain the harness measures — a lesson taught in the thread
// has to survive capture, retrieval, injection and rendering to change the next
// job's behaviour, and a chain is only as testable as its narrowest seam.
func leafNotebookInputs(graph *store.Store, node store.Node) []exec.Input {
	inputs := make([]exec.Input, 0, 4)
	if digest := resident.NotebookDigest(graph, node.ID, node.Brief, node.Provenance.Intent, 8); digest != "" {
		inputs = append(inputs, exec.Input{Title: notebookInputTitle, Result: digest})
	}
	return inputs
}

// planNodeContract reads the working method off the plan node when this leaf
// belongs to a planned job. A splice and a reflex have no contract, and
// inventing a generic one would only dilute the system message they do have.
func planNodeContract(node *plan.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Contract)
}

// leafContract is the working method in force for this leaf.
//
// A planned job carries it on the plan node. A task-scale job has no plan node
// at all — one leaf, spliced whole — and for as long as that was true, the
// single most valuable half of the executor never ran for the shape of work the
// product handles most: what done means in the user's terms, how it is
// verified, and where to stop. It is written at splice time and handed over
// here, once, to the leaf it was written for.
func leafContract(plans *jobPlans, planNode *plan.Node, node store.Node) string {
	if contract := planNodeContract(planNode); contract != "" {
		return contract
	}
	// The task object, which is where the method lives once a job has one. It
	// is read before the registry because it is durable and the registry is
	// not: a restart between the splice and the leaf used to lose the method
	// outright, and a leaf whose spec survived the restart no longer notices.
	if method := strings.TrimSpace(leafSpec(plans, planNode, node).Method); method != "" {
		return method
	}
	if plans == nil {
		return ""
	}
	return plans.takeContract(node.ID)
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

// tasteBriefBytes bounds settled taste inside a leaf's brief. Taste is a short
// list of rules by construction — a user has to correct their way to one twice
// before it is even a candidate — so this is a guard against pathology rather
// than a working budget.
const tasteBriefBytes = 1 << 10

func residentDeliveryBrief(graph *store.Store, node store.Node) string {
	brief := node.Brief
	if node.Parent == store.RootID {
		brief = resident.VoicePrompt(graph, node.Brief, node.Provenance.Intent, node.Brief)
	}
	return withTasteBrief(graph, brief)
}

// leafOutputHint decides where, if anywhere, a leaf is invited to write a file,
// and whether it is working for the person or for the work that comes after it.
//
// The invitation was the defect. Every leaf was handed a numbered path in the
// workspace, so a single job left 07-pr-482-code-review.md, 52-write-complete-
// review.md, 70-read-diff.md, 144-low-findings.md, 144-synthesis.md and
// 216-assemble-review.md in the person's own directory: an offered address
// reads as an expectation, and most of those nodes were producing a handoff
// nobody would ever open.
//
// Who the deliverable belongs to is not a guess. It is the law the delivery
// gate and the announcement already run on — a job root's result is what the
// person reads, everything under it is a handoff — so the workspace path is
// offered to the root alone. An intermediate leaf is pointed at the run's own
// scratch instead, which for an errand working in someone's project is not
// their directory at all.
//
// The address is keyed on the node's own id, which is the only identity here
// that is unique per node. It used to be keyed on the creation sequence and the
// title, and neither is: one splice stamps its whole subtree with one sequence
// (the siblings are told apart by CreatedOrder, not by it), and titles are
// clipped to 48 characters for display, so five parts of one ask that open with
// the same words arrive as one identical string. A live run fanned five briefs
// on five topics onto a single filename and four of them were overwritten by
// the last writer, with nothing but the run's own reflection noticing.
// leafSpend is what one leaf's run cost, in the shape the runner journals.
//
// It is a function rather than three literals for the reason the drop it fixes
// was invisible for so long: the three endings of a leaf — settled, refused,
// failed — differ in what they say about the work and not at all in what it
// cost, and every field a literal forgets is a column of the usage table that
// silently reads zero. Cached tokens were the forgotten one: counted by the
// executor, written by the runner, and never once carried across this seam, so
// the leaf rows — the ones holding almost all the tokens — journalled a warm
// prefix as a cold run. Anything added to the ledger belongs here, once.
func leafSpend(spent exec.Usage, model string) resident.ExecResult {
	return resident.ExecResult{
		PromptTokens:     spent.PromptTokens,
		CompletionTokens: spent.CompletionTokens,
		CachedTokens:     spent.CachedTokens,
		Cost:             spent.Cost,
		Model:            model,
	}
}

// gateEvidence is the record the delivery gate is held to, assembled from the
// three things the caller holds and the judge cannot see: what the run left
// behind, what it ran, and what the person actually asked for.
//
// The two additions past the files and the run tail are what made the gate stop
// judging prose. The request's own named files, settled against the artifact
// registry — which is complete now that subprocess-produced files are recorded —
// answer both halves of the same question: a file the ask named and the run
// produced closes a gap about producing it, and a file the ask named and nothing
// produced convicts a claim that it was written. The done-criterion is the
// standard the plan set before the work started; it travels verbatim through
// retries, so it is the only standard here the run itself cannot have moved.
func gateEvidence(node store.Node, spec plan.Spec, outcome *exec.Outcome, artifacts []string, observed bool) revision.Evidence {
	evidence := revision.Evidence{
		Artifacts: artifacts,
		Named:     revision.NamedFiles(node.Provenance.Intent),
		Done:      spec.Done,
		Observed:  observed,
	}
	if outcome != nil {
		evidence.Ran = outcome.Ran
		// The baseline delta rides with the rest. A coding leaf that found the
		// repository already red says so here, in the worker's own words, so
		// the gate reads "pre-existing failure, unrelated" instead of inferring
		// "`make all` exited 2, therefore this change is broken" from a run tail
		// it cannot rerun.
		evidence.Baseline = outcome.Baseline
	}
	return evidence
}

// A hint is an invitation to write a file at a name, so a directory already
// standing at that name makes it unfulfillable and it is withdrawn rather than
// quietly re-aimed. A leaf handed such a hint has one move left — write inside
// the directory — and the run that did it reported "the file is written and
// verified", settled done, and left the asked-for file nowhere on disk. No
// second address is offered in its place: a redirect the worker was never told
// about is the same silence one layer along, and a leaf with no hint writes
// where the ask told it to, which is the address that was actually wanted.
// Nothing here creates the path either; the invitation must not be the thing
// that occupies it.
func leafOutputHint(node store.Node, title string, space *exec.Workspace) (hint string, intermediate bool) {
	suggested := exec.SuggestPathFor(node.ID, title)
	if node.Parent == store.RootID {
		if space != nil && space.DirectoryAt(suggested) {
			return "", false
		}
		return suggested, false
	}
	// The shown spelling, not the one on disk: it is absolute exactly when
	// scratch has been moved out of the workspace, which is the only case where
	// a relative path would name nothing the worker could open.
	_, shown, err := space.ScratchPath(suggested)
	if err != nil {
		return "", true
	}
	if space.DirectoryAt(shown) {
		return "", true
	}
	return shown, true
}

// withTasteBrief puts settled taste in front of every worker, not only the one
// that owns the deliverable.
//
// Taste had exactly one consumer in the tree — the gate — and the gate is told
// in the same breath that wording and style are not gaps. So a rule the user
// corrected their way to three times reached the one reader instructed to
// ignore it and reached none of the readers it was for. A worker cannot honour
// a standard it was never shown; a reviewer cannot repair one that was never
// applied. It rides the brief rather than the inputs because the input header
// calls everything below it prior results this leaf already has, and a standing
// rule is not a result.
func withTasteBrief(graph *store.Store, brief string) string {
	taste := strings.TrimSpace(resident.TasteBlock(graph))
	if taste == "" {
		return brief
	}
	return strings.TrimSpace(brief) + "\n\nSettled taste — how this person likes work done. Hold to these:\n" +
		clipUTF8Bytes(taste, tasteBriefBytes)
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

// The commander is internal/command's now: every capability the surface
// reaches through, in a package a process without a terminal can also reach.
// These names stay because they are what this package's own prose calls them.
type (
	chatCommander   = command.Commander
	chatPrefs       = command.Prefs
	chatMediaModels = command.MediaModels
)

var (
	newVisitorCommander = command.NewVisitor
	loadChatPrefs       = command.LoadPrefs
	saveChatPrefs       = command.SavePrefs
	attachmentStoreRoot = command.AttachmentStoreRoot
	newSessionID        = command.NewSessionID
)

// liveClient and messageClientPool are internal/provider/pool's Client and
// Pool. The provider seam moved out of this file in the Wave 1 dissolution;
// these names stay because they are what this package's own prose has always
// called them, and an alias is the whole of the difference.
type (
	liveClient        = pool.Client
	messageClientPool = pool.Pool
)

// newLiveClient and withSpendNode are the moved constructor and the moved
// attribution seam, kept under their old spellings for the same reason.
var (
	newLiveClient        = pool.New
	newMessageClientPool = pool.NewPool
	withSpendNode        = pool.WithSpendNode
)

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

// leafSubharness reads what was promised, not what could be chosen now. The
// node's own row holds the settled answer — its own choice where the sizing
// pass made one, the splice's otherwise — and the provenance behind it is read
// only for a node written before the row carried it.
func leafSubharness(node store.Node) string {
	settled := promisedWorker(node)
	// Reading the promise is also the moment this build discovers it cannot keep
	// it. The leaf still runs, on the generalist, exactly as the registry
	// promises — and the node's own recorder now carries the one line that tells
	// a reader afterwards which of the two actually happened.
	noteDegradedLeafWorker(node, settled)
	return settled
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
			// An interrogation has to be earned. "use the kimi model" said
			// "model" and may ask which kimi; a bare preposition-noun read out
			// of ordinary prose — "with the command below" — may not stop the
			// job to offer a Cohere menu. Ambiguity in an unearned reading
			// resolves to the default, silently, like any other non-match.
			if !words.Explicit {
				continue
			}
			return head.WorkModelChoice{Candidates: matches, Requested: name}
		}
	}
	requested := ""
	if len(words.Names) > 0 {
		requested = words.Names[0]
	}
	return head.WorkModelChoice{Requested: requested}
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
	return home.Join("graph.db")
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

// sessionNewWord is the one spelling that means "do not resume". It is a flag
// value rather than an absence because absence is now the common case.
const sessionNewWord = "new"

// resolveChatSession decides which conversation this launch belongs to.
//
// A resident whose thread empties every morning is not a resident: the dock,
// the head's memory of what was said, and every question a job filed against
// the session that created it all live behind this one id, and minting a fresh
// one at every launch quietly threw all four away — including the deliverable
// an overnight job posted into the session it was born in. So the default is
// continuity: come back to the conversation the journal last saw someone in.
// Starting over is still available and is now the explicit act it always
// should have been — `--session new`, or `/new` once the surface is up.
//
// The journal is the only honest source for "the last one", and it holds two
// answers that agree except in the case rooms created. The seen edge is written
// on every attach and detach of every lens, so it names the room this window
// OPENED — but a reader who switched rooms and spent the evening in another one
// never journaled a second attach, and the room they were actually in is the
// one they last spoke in. store.LatestSession answers that; the seen edge stays
// underneath it for a journal with rooms and no words in any of them.
//
// `--session new` reuses an empty unnamed room when one is standing, because an
// empty room is exactly as new as a minted one and two of them are
// indistinguishable to a reader — the accumulation A2 filed is nothing but this
// question asked and answered wrongly, five times.
func resolveChatSession(graph *store.Store, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(requested, sessionNewWord) {
		return freshChatSession(graph), nil
	}
	if requested != "" {
		return requested, nil
	}
	if graph == nil {
		return newSessionID(), nil
	}
	latest, found, err := graph.LatestSession()
	if err != nil {
		return "", fmt.Errorf("resolve chat session: %w", err)
	}
	if found && strings.TrimSpace(latest.ID) != "" {
		return latest.ID, nil
	}
	seen, found, err := graph.LastSeen()
	if err != nil {
		return "", fmt.Errorf("resolve chat session: %w", err)
	}
	if found && strings.TrimSpace(seen.SessionID) != "" {
		return seen.SessionID, nil
	}
	return newSessionID(), nil
}

// freshChatSession is what "start over" resolves to: the empty unnamed room
// already standing, or a new id when there is none. A read that fails is not a
// reason to refuse the launch — the fresh id is always a correct answer, only a
// less tidy one.
func freshChatSession(graph *store.Store) string {
	if graph == nil {
		return newSessionID()
	}
	empty, err := graph.EmptySessions()
	if err == nil && len(empty) > 0 {
		return empty[0].ID
	}
	return newSessionID()
}

// groomChatRooms takes back the empty unnamed rooms that piled up before rooms
// were reused rather than minted. It runs at launch, once, and never touches the
// room this window just resolved or the newest empty one — see
// store.ReapEmptySessions. A failure is a note in the log and nothing more: a
// window may not fail to open because the tidying did.
func groomChatRooms(graph *store.Store, keep string) {
	if graph == nil {
		return
	}
	if discarded, err := graph.ReapEmptySessions(keep); err != nil {
		log.Printf("note: could not reap the empty rooms: %v", err)
	} else if len(discarded) > 0 {
		log.Printf("note: reaped %d empty unnamed room(s)", len(discarded))
	}
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

	// windowLeafGrace is how long closing a chat window waits for the leaves it
	// was already running to land.
	//
	// It is a wait rather than a kill because the leaf is a paid-for turn
	// against a provider that is answering: the receipts for the incident this
	// exists for show ~995k prompt tokens bought and thrown away on the
	// millisecond a terminal closed. It is two minutes rather than unbounded
	// because a window being closed must eventually close, and it is safe to be
	// short because the runner now releases a leaf its context outlived instead
	// of failing it — the remainder resumes on the next window rather than
	// becoming a fault the machine has to explain to itself.
	windowLeafGrace = 2 * time.Minute
)

func continuationMessage(pieces int) string {
	return resident.OverrunContinuationMessage(pieces)
}

// The consent desk is internal/consent's now: the price before the purchase,
// reachable from anywhere work is admitted rather than only from the window
// holding the terminal (chat-rebuild Part 9.10). These names stay because they
// are what this package's own prose calls them.
type (
	consentDesk  = consent.Desk
	planEstimate = consent.Estimate
)

var (
	newConsentDesk     = consent.NewDesk
	estimateJob        = consent.EstimateJob
	medianProfileCost  = consent.MedianLeafCost
	jobRootOf          = consent.JobRootOf
	planConsentApprove = consent.Approve
)

// deliveryPartialBytes bounds the partial a rail-deferred job posts. A partial
// is still what the person reads, so it is bounded by what a message can carry
// and not by what a digest may route — the room is left for the sentence this
// body is posted with. Held at 4 KiB it was the same guillotine the node
// summary used to be: long work reached the rail and its answer stopped
// mid-word.
const deliveryPartialBytes = store.MaxMessageBytes - 1<<10

func boundedDelivery(text string) string {
	return clipUTF8Bytes(strings.TrimSpace(text), deliveryPartialBytes)
}

// composeDelivery is the whole of one rule: what is delivered is the FINAL
// state of the work, and everything the system has to say about that work comes
// after it.
//
// The body is the deliverable and only ever the deliverable — a revision
// replaces it outright rather than adding to it, so no round of a dispute can
// leave its prose in front of the answer. The notes are what is owed about it:
// a review the system declined to act on, a reservation on a gap nothing could
// close, parts of the job that failed. They are short, they are separate, and
// they are last.
//
// Measured: on a cell that objectively passed, the visible reply opened "no bug
// to find" — a reservation from a dispute the system had already settled,
// standing where the answer should have been. Judges scored it 2/5. The residue
// was structural, so the separation is too: the dispute's own history lives in
// the journal and the delivery-gate ledger, which the belt tools read on
// request. It is never the opening of a deliverable.
func composeDelivery(body string, notes []string) string {
	delivered := strings.TrimSpace(body)
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" || strings.Contains(delivered, note) {
			continue
		}
		if delivered == "" {
			delivered = note
			continue
		}
		delivered += "\n\n" + note
	}
	return delivered
}

// failedPartsNamed bounds how many failed steps a delivery names one by one.
// Past a handful the list stops being information and becomes a wall; the count
// still tells the truth.
const failedPartsNamed = 3

// failedPartsNote is the sentence a parent owes its reader when part of the
// work under it did not land. It counts the whole subtree rather than the
// direct children because a fan-out's failures are usually two levels down, and
// it names the first few with their own reasons, because "3 of 4 parts landed"
// without saying which one is missing is only marginally better than silence.
func failedPartsNote(graph *store.Store, node store.Node) string {
	nodes, err := graph.SubtreeNodes(node.ID)
	if err != nil || len(nodes) <= 1 {
		return ""
	}
	total, lost := 0, make([]store.Node, 0)
	for _, part := range nodes {
		if part.ID == node.ID {
			continue
		}
		total++
		if part.Status == store.Failed || part.Status == store.Cancelled {
			lost = append(lost, part)
		}
	}
	if len(lost) == 0 || total == 0 {
		return ""
	}
	note := fmt.Sprintf("Not all of this landed: %d of %d parts finished.", total-len(lost), total)
	named := lost
	if len(named) > failedPartsNamed {
		named = named[:failedPartsNamed]
	}
	for _, part := range named {
		reason := firstLine(strings.TrimSpace(part.Error))
		if reason == "" {
			reason = "no reason was recorded"
		}
		note += "\n- " + clipUTF8Bytes(firstLine(nodeDisplay(part)), 70) + " — " + clipUTF8Bytes(reason, 160)
	}
	if len(lost) > len(named) {
		note += fmt.Sprintf("\n- and %d more that did not finish", len(lost)-len(named))
	}
	return note + "\nRead what is above knowing that much of it is missing."
}

// leafErrorPrefix is how the executor stamps its own errors: `node 7: ...`,
// where 7 is the node's CreatedSeq. It is a real handle inside the executor and
// meaningless outside it — the number appears on no surface the user has ever
// seen — so it is stripped here, at the seam where an executor error becomes
// something a person reads.
var leafErrorPrefix = regexp.MustCompile(`^node \d+: `)

// leafCause is the one clause a person can act on, taken from the parts of the
// failure that are typed rather than from the sentence it printed.
//
// A provider refusal now travels as a value with its own message field, so the
// clause is READ rather than recovered: no unwrapping of colon chains, no
// guessing which half of a wrapped string is the diagnosis. Only when nothing
// typed is in the chain does this fall back to the text, and then it hands the
// string to the same envelope decoder every other reader of a failure uses, so
// the two paths cannot drift into disagreeing about what a failure said.
//
// Nothing is invented. A failure that says nothing readable comes back empty and
// the composition says so in its own words rather than making one up.
func leafCause(node store.Node, err error) string {
	var refused *provider.APIError
	if errors.As(err, &refused) {
		if message := strings.TrimSpace(refused.Message); message != "" {
			// Through the same reducer the string path uses, so a provider that
			// answers a refusal with a diagnosis and three lines of advice about
			// which options to change is quoted once and identically wherever
			// the failure is read. The advice is still whole, one line down.
			return resident.FailureCause(message)
		}
	}
	stamped := strings.TrimSpace(leafErrorPrefix.ReplaceAllString(strings.TrimSpace(err.Error()), ""))
	return resident.FailureCause(resident.StripNodeStamp(node.ID, stamped))
}

// humanFailure turns an executor error into what a node records about why it
// stopped.
//
// THE FIRST LINE IS THE CAUSE AND ONLY THE CAUSE. Every reader of this string
// puts it beside the work it belongs to — the room's failure row leads with the
// person's own ask (see resident.Failed), the parent's missing-parts note leads
// with the part's name, the record page draws it in the node's own row — so a
// label glued onto the front here is the same name printed twice, and the
// second printing is the one that pushed the actual reason off the end of the
// line. It used to read `Launch the analysis fan did not finish — node
// craft-3799~launch: after 3 node call attempts: API error (404): {"error":…`,
// which is a title, an id, an attempt count and a JSON body in front of a
// sentence the reader needed.
//
// The provider's own words are kept verbatim, because they are the only
// diagnosis anyone has and paraphrasing them would be inventing one. What is
// dropped from the line is the envelope, not the message — and dropped is the
// wrong word for it, because the whole transport rides immediately BELOW,
// unedited, where the record page shows it in full and no clipping reaches.
//
// The partial files ride below too, for the retry and for the person who wants
// them, not for the notification.
func humanFailure(node store.Node, err error, artifacts []string) error {
	if err == nil {
		return nil
	}
	cause := leafCause(node, err)
	if cause == "" {
		cause = "it stopped without saying why"
	}
	body := clipUTF8Bytes(firstLine(cause), failureCauseBytes) + "\n\n" + strings.TrimSpace(err.Error())
	if len(artifacts) > 0 {
		body += "\n\nFiles it left behind:\n" + strings.Join(artifacts, "\n")
	}
	return errors.New(body)
}

// failureCauseBytes bounds the cause line. A provider that answers a refusal
// with a paragraph has said the useful part first; the paragraph is whole, one
// line down.
const failureCauseBytes = 300

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
//
// The registry keeps two kinds of lock and the difference between them is the
// difference between a wide plan that fans out and one that quietly runs
// serially. `mu` is the map lock and nothing else: it is held for a map read, a
// map write, and never across anything that can block. What a job's plan
// document needs is a lock of its own, because that document is edited by a
// sentinel whose pass contains a model round-trip, and a round-trip held under
// one registry-wide mutex stops every other job's leaves from so much as
// looking themselves up.
type jobPlans struct {
	mu     sync.Mutex
	graphs map[string]plannedJob
	// locks is one pair of mutexes per retained plan graph, keyed by the graph
	// itself because that is what they actually guard — a rehydrated job gets a
	// new document and a new pair with it, so the pairing can never drift.
	locks map[*plan.Graph]*planLocks
	// owed is the planner's parked bills — spend for jobs whose splice has not
	// landed in the statement yet (journalPlanSpend/settleOwedPlanSpend).
	owed map[string]*owedPlanSpend
	// contracts holds the working method for the jobs that never earn a graph.
	// A task-scale ask is spliced as one leaf, so there is no plan node to hang
	// the method on and no plan to journal; the registry carries it from the
	// splice to the moment its leaf starts and hands it over exactly once. A
	// restart in between loses it and that leaf runs the generic loop — the
	// same degradation a failed contract call has always had.
	contracts map[string]string
	// readings carries the compiler's structural reading of an ask from the
	// compile call to the scale gate, keyed by the goal the two share.
	//
	// It is a memo rather than a field on resident.Compiled because the reading
	// is not the reconciler's business: nothing between here and the gate reads
	// it, decides on it, or would behave differently for it. What it is for is
	// the journal — "this job became one leaf because the ask was read as a
	// single act" is a sentence nobody could write afterwards, because the
	// reading is a model's and does not repeat on a rerun.
	//
	// Bounded and lossy on purpose. A goal that was never gated leaves its entry
	// behind, so the map is cleared wholesale once it grows past a session's
	// worth of them; losing a reading costs one journal field and never the job.
	readings map[string]string
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

// planLocks are the two locks one retained plan document needs, and they are
// two because they answer two different questions.
//
// document is "is anyone reading or writing this graph right now" — a leaf
// resolving its node, a leaf marking itself running, a leaf recording what it
// measured, the sentinel rendering the plan into a prompt or editing it
// afterwards. It is held for microseconds by everything except the sentinel,
// and the sentinel deliberately hands it back while the model thinks.
//
// pass is "is a revision of this job already in flight" — held for the whole
// sentinel pass, including the round-trip. Without it, handing the document
// back mid-call would let a second revision of the same plan start against the
// same document and interleave its edits with the first one's, which is the one
// ordering the old registry-wide lock did guarantee and the one worth keeping.
type planLocks struct {
	document sync.Mutex
	pass     sync.Mutex
}

// locksFor returns the lock pair belonging to one plan document, minting it on
// first use. The registry lock is taken and released inside — a document lock is
// never acquired while holding it, which is the whole of the ordering rule.
func (j *jobPlans) locksFor(graph *plan.Graph) *planLocks {
	j.mu.Lock()
	defer j.mu.Unlock()
	if existing, ok := j.locks[graph]; ok {
		return existing
	}
	if j.locks == nil {
		j.locks = map[*plan.Graph]*planLocks{}
	}
	minted := &planLocks{}
	j.locks[graph] = minted
	return minted
}

func (j *jobPlans) put(prefix string, graph *plan.Graph, root, model string, client router.Client) {
	// The document lock is taken before the map write rather than after: the
	// instant the entry lands in the map a leaf can look itself up and mark
	// itself running, and journalling serialises the whole graph.
	locks := j.locksFor(graph)
	locks.document.Lock()
	defer locks.document.Unlock()
	entry := plannedJob{graph: graph, root: root, model: model, client: client}
	j.retain(prefix, entry)
	if j.journal != nil {
		j.journal(prefix, entry)
	}
}

// retain files one job in the map and nothing more. It is its own function so
// the registry lock is the whole of it, held under a defer.
func (j *jobPlans) retain(prefix string, entry plannedJob) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.graphs[prefix] = entry
}

// putContract holds a one-leaf job's working method until its leaf claims it.
func (j *jobPlans) putContract(nodeID, contract string) {
	contract = strings.TrimSpace(contract)
	if nodeID == "" || contract == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.contracts == nil {
		j.contracts = map[string]string{}
	}
	j.contracts[nodeID] = contract
}

// takeContract hands the method to the leaf and forgets it. Once is enough:
// the task struct is built one time and every retry, escalation and revision
// pass is built from that struct, so a second reader would only be a leak.
func (j *jobPlans) takeContract(nodeID string) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	contract, ok := j.contracts[nodeID]
	if !ok {
		return ""
	}
	delete(j.contracts, nodeID)
	return contract
}

// maxRememberedReadings bounds the compile-to-gate memo. A session that asked a
// hundred questions has a hundred goals, most of them long since gated, and the
// map is dropped wholesale rather than aged: the only cost of forgetting is one
// field on one journal record.
const maxRememberedReadings = 64

// noteReading remembers how the compiler read one ask's structure.
func (j *jobPlans) noteReading(goal, structure string) {
	goal, structure = strings.TrimSpace(goal), strings.TrimSpace(structure)
	if goal == "" || structure == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.readings == nil || len(j.readings) >= maxRememberedReadings {
		j.readings = map[string]string{}
	}
	j.readings[goal] = structure
}

// takeReading hands the reading to the gate and forgets it, for the same reason
// takeContract does: the gate runs once per compiled ask, and a second reader
// would only be a leak.
func (j *jobPlans) takeReading(goal string) string {
	goal = strings.TrimSpace(goal)
	j.mu.Lock()
	defer j.mu.Unlock()
	structure, ok := j.readings[goal]
	if !ok {
		return ""
	}
	delete(j.readings, goal)
	return structure
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
	// The map read and the document read are two separate holds. This is the
	// call at the head of every leaf, and under the old single lock it was the
	// call that waited behind another job's sentinel round-trip.
	if entry, ok := j.get(nodeID); ok {
		locks := j.locksFor(entry.graph)
		locks.document.Lock()
		defer locks.document.Unlock()
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
	entry, ok := j.get(prefix)
	if !ok {
		return "", nil, nil, "", nil
	}
	locks := j.locksFor(entry.graph)
	locks.document.Lock()
	defer locks.document.Unlock()
	for index := range entry.graph.Nodes {
		if entry.graph.Nodes[index].ID == planID {
			return prefix, entry.graph, &entry.graph.Nodes[index], entry.model, entry.client
		}
	}
	return prefix, entry.graph, nil, entry.model, entry.client
}

// recordOutcome writes a leaf's measured ending onto its plan node — the same
// fields, in the same shape, that the headless scheduler records.
// markWorker settles a plan node onto a different worker mid-flight, so the
// measurement this node becomes is filed under whoever actually ran it. A leaf
// escalated to a specialist and recorded against the generalist would teach the
// generalist's ruler that ordinary leaves cost what a specialist costs.
func (j *jobPlans) markWorker(graph *plan.Graph, node *plan.Node, subharness string) {
	locks := j.locksFor(graph)
	locks.document.Lock()
	defer locks.document.Unlock()
	node.Subharness = strings.TrimSpace(subharness)
}

func (j *jobPlans) recordOutcome(graph *plan.Graph, node *plan.Node, outcome *exec.Outcome, err error, escalatedFrom string) {
	locks := j.locksFor(graph)
	locks.document.Lock()
	defer locks.document.Unlock()
	node.EscalatedFrom = strings.TrimSpace(escalatedFrom)
	if outcome != nil {
		node.Turns = outcome.Turns
		node.Tokens = outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
		node.Cost = outcome.Usage.Cost
		node.Stop = string(outcome.Stop)
		node.Verdict = outcome.Verdict
		node.Artifacts = outcome.Artifacts
		node.Result = outcome.Text
		node.Calibration = append([]string(nil), outcome.Calibration...)
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
//
// The document's locks are dropped with it. The sink is the node every other
// node of the job feeds, so nothing of this job can still be holding them by
// the time it lands, and keeping them would be a map that only ever grew.
func (j *jobPlans) takeIfRoot(nodeID string) (*plan.Graph, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if entry, ok := j.entry(nodeID); ok && entry.root == nodeID {
		delete(j.graphs, nodeID)
		delete(j.locks, entry.graph)
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
	delete(j.locks, entry.graph)
	return entry.graph, prefix
}

func (j *jobPlans) markRunning(graph *plan.Graph, node *plan.Node) {
	locks := j.locksFor(graph)
	locks.document.Lock()
	defer locks.document.Unlock()
	node.State = plan.StateRunning
}

// reviseAfter runs the sentinel over a job's remaining plan in light of one
// landed result, and mirrors whatever it legally edits onto the store. It skips
// entirely when the job has no unstarted work left to edit.
//
// The pass takes this job's own two locks and not the registry's. It was once
// defended as "a short structuring call", and it is not: it is a model
// round-trip, and holding the registry across it meant that every leaf of every
// other job in the process — a lookup is the first thing a leaf does — waited
// for a sentinel that had nothing to do with it. A plan that fans out wide is
// exactly the plan that lands results often enough to revise often, so the
// arrangement went serial precisely when parallelism was the point.
//
// What replaces it is not a weaker guarantee, it is a narrower one. The pass
// lock keeps this job's revisions in single file. The document lock is held
// while the plan is rendered into the prompt and again while the answer is
// applied, and is handed back only for the round-trip in between — see
// unlockedWhileThinking. That the world may have moved during the round-trip is
// not a new hazard needing a new generation counter: the plan package re-reads
// each node's state at apply time and refuses to touch anything that has since
// started, and the store refuses the same edits again on its own authority.
// Both refusals were already the everyday case, because the store has always
// been free to claim a node while this pass was running.
func (j *jobPlans) reviseAfter(ctx context.Context, settings config.Config, client *liveClient, graph *store.Store, node store.Node, prefix string, planGraph *plan.Graph, summary string, artifacts []string, failure string, workerModel string) {
	// Every landing is the heartbeat a parked planning bill waits for: by the
	// time a leaf has landed, the splice that owed the money has long happened.
	j.settleOwedPlanSpend(graph)
	j.reviseOn(ctx, settings, client, graph, node, prefix, planGraph,
		resident.RevisionEvent(node, summary, artifacts, failure, planGraph.Window()), workerModel)
}

// reviseAfterCancel is the same pass convened by a withdrawal rather than a
// landing. Cancellation used to have no upward channel at all: the leaf wrapper
// returned before the sentinel was ever asked, so a plan whose third step had
// just been taken away carried on building the fourth against an input that
// would never arrive. The event says what was stopped and what it had already
// written; the sentinel's own rules do the rest — only unstarted nodes, only a
// named contradiction, and nothing that re-adds the work the user cut.
func (j *jobPlans) reviseAfterCancel(ctx context.Context, settings config.Config, client *liveClient,
	graph *store.Store, node store.Node, prefix string, planGraph *plan.Graph,
	partial, reason, workerModel string) {
	j.reviseOn(ctx, settings, client, graph, node, prefix, planGraph,
		resident.CancelledRevisionEvent(node, partial, reason, planGraph.Window()), workerModel)
}

func (j *jobPlans) reviseOn(ctx context.Context, settings config.Config, client *liveClient, graph *store.Store, node store.Node, prefix string, planGraph *plan.Graph, event string, workerModel string) {
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

	locks := j.locksFor(planGraph)
	locks.pass.Lock()
	defer locks.pass.Unlock()
	locks.document.Lock()
	defer locks.document.Unlock()
	// The pass itself is internal/revision's. What stays here is the registry's
	// half of it: which job's document, under which locks, and what journaling
	// the retained structure means once an edit lands.
	revision.Sentinel(ctx, settings, unlockedWhileThinking{client: client, document: &locks.document},
		graph, node, prefix, entry.root, planGraph, event, workerModel, func() {
			if j.journal != nil {
				j.journal(prefix, entry)
			}
		})
}

// reviseForUser is reviseAfter's twin for the other event source. It takes the
// same two locks for the same reasons, and differs in exactly two places: the
// event is the user speaking with authority, and it does not return early when
// nothing is pending — the leaves already running still have to be told, and
// that broadcast is the reconciler's next move.
func (j *jobPlans) reviseForUser(ctx context.Context, settings config.Config, client *liveClient,
	graph *store.Store, job store.Node, message string,
	flavor resident.RevisionFlavor) (resident.Redirection, error) {
	entry, ok := j.get(job.ID)
	if !ok {
		// Silence here was a lie with a receipt attached. An empty Redirection
		// and a nil error are indistinguishable from "the sentinel read the
		// plan and found nothing to change", so the user was told exactly that
		// while every pending leaf went on building the version they had just
		// asked to replace. The caller already owns an honest branch for a
		// revision that could not happen; this is how it reaches it.
		return resident.Redirection{}, errNoRetainedPlan
	}
	locks := j.locksFor(entry.graph)
	locks.pass.Lock()
	defer locks.pass.Unlock()
	locks.document.Lock()
	defer locks.document.Unlock()
	redirection, applied, err := revision.ForUser(ctx, settings,
		unlockedWhileThinking{client: client, document: &locks.document},
		graph, job, entry.graph, entry.root, message, flavor)
	if err != nil {
		return resident.Redirection{}, err
	}
	if applied > 0 && j.journal != nil {
		j.journal(job.ID, entry)
	}
	return redirection, nil
}

// unlockedWhileThinking hands a plan document back to the rest of the job for
// as long as the model has the question.
//
// It reads as a trick and is not one. A revision pass is three phases with a
// wall between them: the plan is rendered into the prompt, the prompt is sent,
// and the answer is applied. The middle phase is the only slow one and it is
// the only one that touches nothing — by the time the client is called the
// messages are already bytes, and nothing the graph does afterwards can change
// what was asked. So the lock the first and third phases need is exactly the
// lock the second one should not be holding, and this is where that is said,
// because plan.Revise is one call and the seam is inside it.
//
// The client is called from one goroutine and at most twice (a truncated answer
// earns a retry), so the unlock and relock always pair.
type unlockedWhileThinking struct {
	client   plan.Completer
	document *sync.Mutex
}

func (u unlockedWhileThinking) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	u.document.Unlock()
	defer u.document.Lock()
	return u.client.CompleteWithMessages(ctx, messages, options...)
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

// The resident is the half of the surface that keeps working while nobody is
// typing: it announces settled work, distills the notebook, fires charters, and
// resumes deferred overruns. Its loop used to be launched as `_ = Serve(ctx)` —
// the error thrown away, the goroutine gone, and no one told. A single
// transient failure inside one pass therefore ended the resident silently for
// the lifetime of the terminal, while the lease went on saying the role was
// taken, so `aforge wake` stepped aside for a process that had stopped serving
// hours ago. Standing watches, charters and practice simply never fired again.
//
// So the loop gets a supervisor. Restarting is the right default because the
// store is the truth and Serve holds nothing across a pass — a fresh call
// re-reads the same queue and carries on. What is not acceptable is a tight
// spin: a loop that cannot get through one pass will not be fixed by being run
// a thousand times, and it would bury the reason under its own log.
const (
	// residentRestartBackoff is the pause before a restart, and it is also what
	// distinguishes a transient failure from a broken one: a resident that dies
	// faster than this is dying on something structural.
	residentRestartBackoff = 5 * time.Second
	// residentRestartLimit is how many rapid deaths are absorbed before the
	// supervisor stops and says so. Deaths spaced further apart than
	// residentHealthyRun are forgiven, so a resident that runs for an hour
	// between hiccups is never given up on.
	residentRestartLimit = 5
	residentHealthyRun   = 5 * time.Minute
)

// superviseResident runs serve until the context ends, restarting it after a
// bounded number of rapid failures. announce is how the user finds out; it is
// called exactly once, when the supervisor gives up, because the whole failure
// this replaces is one of silence. The two durations are arguments rather than
// the constants they are called with so a test can prove the give-up and the
// forgiveness without spending ten minutes of wall clock proving them.
func superviseResident(ctx context.Context, serve func(context.Context) error,
	backoff, healthyRun time.Duration, announce func(string)) {
	consecutive := 0
	for {
		started := time.Now()
		err := serve(ctx)
		if ctx.Err() != nil {
			// The ordinary shutdown: the surface is closing and Serve returned
			// the cancellation it was given. Nothing to say.
			return
		}
		if time.Since(started) >= healthyRun {
			consecutive = 0
		}
		consecutive++
		log.Printf("resident loop stopped after %s (%d in a row): %v",
			time.Since(started).Round(time.Second), consecutive, err)
		if consecutive >= residentRestartLimit {
			log.Printf("resident loop abandoned after %d restarts; background work has stopped", consecutive)
			if announce != nil {
				announce("my background half has stopped and I could not restart it — " +
					"standing rules, watching and follow-up work are paused until aforge is restarted. " +
					"The reason is in the log: " + firstLine(fmt.Sprint(err)))
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
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

// runLeafWithWatchdog is the scheduler's node watchdog, inline: the executor
// has its own deadline, so this only fires when a worker is wedged past every
// limit it was given — turning a silent forever-hang into a recorded failure.
func runLeafWithWatchdog(ctx context.Context, worker exec.Executor, task exec.Task, timeout time.Duration) (*exec.Outcome, error) {
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
		outcome, err := worker.Run(ctx, task)
		done <- landing{outcome, err}
	}()
	// The watchdog is a named timer rather than time.After because the branch it
	// guards is the one that almost never runs: a leaf lands in seconds and the
	// select leaves by done, while an unstopped time.After holds its runtime
	// timer — and the leaf's whole 17 minutes of it — alive in the heap for
	// nothing. One per leaf and one per gate revision, on every job.
	watchdog := time.NewTimer(timeout)
	defer watchdog.Stop()
	select {
	case result := <-done:
		return result.outcome, result.err
	case <-watchdog.C:
		return nil, fmt.Errorf("executor did not return within %s; abandoned", timeout.Round(time.Second))
	}
}

// recordSingleLeaf keeps direct-job costs available to compiler self-knowledge
// without pretending an unplanned task was atomic ruler evidence.
//
// The profile it lands in is the one belonging to whatever actually ran the
// leaf. A specialist's cost written into the generalist's file would not merely
// be misfiled: it is the file the ruler is recalibrated from, so one coding
// pipeline's forty minutes would teach the planner that ordinary leaves are
// enormous and it would stop splitting anything.
func recordSingleLeaf(settings config.Config, model string, node store.Node, outcome *exec.Outcome, escalatedFrom string) (profile.Record, bool) {
	if strings.TrimSpace(model) == "" {
		model = settings.Model
	}
	worker := profileSubharness(leafSubharness(node))
	measured, err := profile.Load(settings.ProfileDir, model, worker)
	if err != nil {
		return profile.Record{}, false
	}
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = firstLine(node.Brief)
	}
	record := profile.Record{
		Title:   title,
		Summary: firstLine(node.Brief),
		Size:    profile.BucketDirect,
		Turns:   outcome.Turns,
		Tokens:  outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens,
		// The cost was measured all along and thrown away here, which left every
		// direct record priced at zero — and the self-knowledge line the compiler
		// reads off these records has been quoting an average cost of $0.0000
		// ever since. It is also the figure the boundary comparison below is
		// made of, so a specialist could never be found to have undercut the
		// generalist: both sides of the comparison were zero.
		Cost:          outcome.Usage.Cost,
		Stop:          string(outcome.Stop),
		Verdict:       outcome.Verdict,
		Calibration:   append([]string(nil), outcome.Calibration...),
		EscalatedFrom: strings.TrimSpace(escalatedFrom),
	}
	added := measured.Add(withBoundaryEvidence(settings, model, worker, record))
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

// journalPlanSpend bills the planner's passes to the spine.
//
// The spine rather than the job is deliberate and is the honest half of a
// compromise: the nodes this plan describes do not exist yet — the reconciler
// splices them in the statement after this one — so there is nothing to charge
// yet. THE PLAN'S BILL BELONGS TO THE JOB IT BUILT, and it cannot be written
// when it is spent: the reconciler splices the task into the statement after
// the planner returns, and store.RecordUsage refuses a node that is not there
// yet. So the bill is offered against the job, parked when the job has not
// landed, and re-offered on the heartbeat until it lands — or until patience
// runs out and the spine takes it, which is where spend with no errand has
// always gone. A call with no job name (or no registry to park on) goes to the
// spine directly, the behaviour every caller had before jobs owned their plans.
func journalPlanSpend(history *store.Store, plans *jobPlans, client *liveClient, job string, passes ...plan.Usage) {
	if history == nil {
		return
	}
	var total plan.Usage
	for _, pass := range passes {
		total.Calls += pass.Calls
		total.PromptTokens += pass.PromptTokens
		total.CompletionTokens += pass.CompletionTokens
		total.Cost += pass.Cost
	}
	if total.Calls == 0 {
		return
	}
	model := ""
	if client != nil {
		model = client.Model()
	}
	bill := store.NodeUsage{
		NodeID: job, PromptTokens: total.PromptTokens,
		CompletionTokens: total.CompletionTokens, Cost: total.Cost, Model: model,
	}
	if job != "" {
		if err := history.RecordUsage(bill); err == nil {
			return
		}
		if plans != nil {
			plans.parkPlanSpend(job, bill)
			return
		}
	}
	bill.NodeID = store.RootID
	if err := history.RecordUsage(bill); err != nil {
		log.Printf("note: could not journal planning spend: %v", err)
	}
}

// planSpendPatience is how many heartbeats a parked bill waits for its job to
// land before the spine takes it. Small on purpose: a splice that has not
// happened four landings later is not late, it is not coming.
const planSpendPatience = 4

// parkPlanSpend holds a bill whose job is not in the statement yet. Bills for
// one job merge, because the figure the cards read is the job's total, not a
// ledger of passes.
func (j *jobPlans) parkPlanSpend(job string, bill store.NodeUsage) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.owed == nil {
		j.owed = map[string]*owedPlanSpend{}
	}
	if held, ok := j.owed[job]; ok {
		held.bill.PromptTokens += bill.PromptTokens
		held.bill.CompletionTokens += bill.CompletionTokens
		held.bill.Cost += bill.Cost
		if held.bill.Model == "" {
			held.bill.Model = bill.Model
		}
		return
	}
	j.owed[job] = &owedPlanSpend{bill: bill}
}

// settleOwedPlanSpend re-offers every parked bill. A job that landed takes its
// money; one that keeps not landing is billed to the spine after
// planSpendPatience beats, so the day's rail is never short whatever the shape
// of the failure. Settled once and not twice: a settled bill leaves the map.
func (j *jobPlans) settleOwedPlanSpend(graph *store.Store) {
	if j == nil || graph == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for job, held := range j.owed {
		if err := graph.RecordUsage(held.bill); err == nil {
			delete(j.owed, job)
			continue
		}
		held.beats++
		if held.beats < planSpendPatience {
			continue
		}
		held.bill.NodeID = store.RootID
		if err := graph.RecordUsage(held.bill); err != nil {
			log.Printf("note: could not journal abandoned planning spend: %v", err)
		}
		delete(j.owed, job)
	}
}

// owedPlanSpend is one job's parked planning bill and how long it has waited.
type owedPlanSpend struct {
	bill  store.NodeUsage
	beats int
}

// namedFileInAsk matches a token that reads as a filename: a stem, a dot, and a
// two-to-eight character alphanumeric extension opening with a letter. The
// extension's shape is what keeps prose out — "e.g.", "i.e.", "vs.", "1.5x" and
// version numbers all fail it — and the stem's character class is what lets a
// path through, because "docs/JOURNEY.md" names a file exactly as "report.md"
// does.
var namedFileInAsk = regexp.MustCompile(`[\w.\-/]*\w\.[A-Za-z][A-Za-z0-9]{1,7}\b`)

// fileShapedAsk answers the delivery law's one question — is the finished thing
// a file, or is it the message? — from the two facts this surface actually
// holds, and refuses to guess past them.
//
// The first is whether the person handed the run their own directory: without
// one there is nowhere a file could be the deliverable, because the workspace is
// a per-job folder that evaporates from the reader's point of view. That is the
// same bit terrainRoot carries, which is why it is the parameter rather than a
// second one saying the same thing.
//
// The second is whether the ask names a file. Deliberately deterministic: this
// decides which half of plan.DeliveryLaw every brief and every contract in the
// job is written against, and a judgement that costs a call would be bought
// once per plan and once per replan for a bit that a regexp settles. False
// stays false — DeliverInMessage — which is exactly today's behaviour, so the
// only asks this moves are the ones that said a filename out loud.
func fileShapedAsk(terrainRoot, goal string) bool {
	if strings.TrimSpace(terrainRoot) == "" {
		return false
	}
	return namedFileInAsk.MatchString(goal)
}

// planWindow is how much the plan slot's current model can hold, asked of the
// live handle rather than of a value captured at startup: /model may have moved
// the slot since, and the graph about to be built should record the window it is
// actually being written through.
//
// Zero is the catalog declining to place the model — an unlisted slug, a catalog
// that never loaded — and every budget downstream of it falls back to the
// literal it carried before any of this existed. Zero is never "small".
func planWindow(settings config.Config, client *liveClient) int {
	if client == nil {
		return 0
	}
	model, _ := client.Snapshot()
	// A nil catalog answers zero rather than panicking, which is the same
	// answer as an unlisted model and wants the same handling.
	return settings.Models.ContextLength(model)
}

// terrainRoot is the directory the planner is allowed to look at before it
// plans, and it is empty for every surface but a shared workspace. See
// buildBrain, where the judgement is made once.
func planSubtree(settings config.Config, planClient, workClient *liveClient, plans *jobPlans, history *store.Store, terrainRoot string) resident.PlanFunc {
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
		// The reading behind whatever this gate is about to decide, taken once
		// and handed to whichever exit is used. Reading it here rather than in
		// each branch is what makes it impossible for one exit to journal a
		// shape and leave the reason behind.
		structure := plans.takeReading(compiled.Goal)
		// Built before the scale gate rather than after it. A one-node job still
		// buys a contract call, and that call was the last structuring round-trip
		// in the system that reported nothing at all — the poster used to be
		// constructed on the far side of an early return it never reached.
		progress := chatPlanProgress(history, anchor)
		if compiled.Scale != head.ScaleProject {
			// One leaf is the whole plan, and it still deserves a working
			// method. A lookup does not: it is a question with an answer, the
			// method for which is to answer it, and buying a call to say so
			// would break the proportionality this whole path exists to keep.
			//
			// The compile call that read the whole ask writes the method in the
			// same breath now; the separate structuring round-trip is the
			// fallback for a compiler that left it empty, not the path.
			var leaf plan.Spec
			if compiled.Scale == head.ScaleTask {
				method := strings.TrimSpace(compiled.Contract)
				if method == "" {
					method = taskContract(ctx, settings, planClient, history, compiled.Goal, progress)
				}
				// One node is a plan. It used to be the one shape of job with
				// no plan document at all, so its method lived in a memory map
				// a restart emptied and its spec lived nowhere — which is why
				// the durable answer to "what was this job asked for" existed
				// for a decomposed job and not for the commonest job there is.
				// Journaling the one node costs one event and closes that gap.
				document := taskSpecGraph(compiled.Goal, method)
				leaf = document.Nodes[0].Spec
				plans.put(prefix, document, prefix, "", nil)
			}
			journalScaleGate(history, prefix, compiled, structure, store.ScaleRouteSingleLeaf, 1)
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID:    prefix,
				Brief: compiled.Goal,
				Stage: 1,
				Spec:  resident.EncodeSpec(leaf),
			}}}, nil
		}
		// Structuring runs on the plan slot; the retained snapshot is the work
		// slot, because that is who the leaves run on and whose model the
		// profile key must name.
		workingModel, workingClient := workClient.Snapshot()
		_, structuring := planClient.Snapshot()
		// A declared bundle never meets the planner. The compile call already
		// judged the requests independent and wrote each as a standalone
		// assignment; laying them side by side is geometry, and the spine was
		// measured restating the independence rule and then chaining them
		// anyway. This is also the cheaper path: no spine, no fan-out — the
		// sequencing and contract passes below are the only structuring these
		// jobs buy.
		//
		// Sequencing is not a second opinion on whether to bundle; it is the one
		// thing the flat layout cannot carry. A compile call that declared four
		// independent parts where the fourth assembled the other three admitted
		// four leaves with no edges between them, and the assembler was
		// claimable from the first tick: it ran beside its own inputs, invented
		// the section it was supposed to read, and its file went out with one of
		// the three countries missing for good. The declaration is checked here
		// because here is the last place an order can still be recorded — after
		// the splice there is nothing left to infer it from.
		if parts := trimmedParts(compiled.Parts); len(parts) >= 2 {
			graph := plan.Bundle(compiled.Goal, parts)
			sequenceUsage, sequenceErr := plan.Sequence(settings.Context(ctx, compiled.Goal), structuring, graph)
			if sequenceErr != nil {
				log.Printf("note: could not sequence bundle parts: %v", sequenceErr)
			}
			journalPlanSpend(history, plans, planClient, prefix, sequenceUsage)
			contractUsage, contractErr := plan.Contracts(settings.Context(ctx, compiled.Goal), structuring, graph, resident.ContractPlaybook(history), progress)
			if contractErr != nil {
				log.Printf("note: could not write contracts: %v", contractErr)
			}
			journalPlanSpend(history, plans, planClient, prefix, contractUsage)
			subtree, subtreeErr := resident.SubtreeFromPlan(graph, prefix)
			if subtreeErr != nil {
				return store.Subtree{}, subtreeErr
			}
			plans.put(prefix, graph, subtreeSink(subtree), workingModel, workingClient)
			journalScaleGate(history, prefix, compiled, structure, store.ScaleRouteBundle, len(subtree.Nodes))
			return subtree, nil
		}
		graph, err := plan.Build(settings.Context(ctx, compiled.Goal), structuring, compiled.Goal, plan.Options{
			Recall: recallHits(history, compiled.Goal, groundRecallLimit),
			// Rendered once, here, and frozen for the build: this block joins the
			// shared prefix every pass reads, and a workspace re-read mid-build —
			// with workers already writing into it — would move the prefix under
			// passes still in flight.
			Terrain: plan.RenderTerrain(terrainRoot, compiled.Goal),
			// Where the finished thing has to appear. The two facts this surface
			// actually holds are whether the person gave the run their own
			// directory and whether the ask names a file in it; together they are
			// the delivery gate's own question, asked before the work rather than
			// after it.
			FileShaped:   fileShapedAsk(terrainRoot, compiled.Goal),
			SpineSamples: settings.SpineSamples,
			// The window this document is written and later revised through.
			// It rides onto the graph so the sentinel and the completion gate,
			// which are handed the document and nothing else, size themselves
			// from the same number the build did.
			ContextTokens: planWindow(settings, planClient),
			// One level deeper than the one-shot default: chat projects are
			// where visible fan-out is the product, and the compiler now
			// names the parts for the planner to expand.
			MaxDepth: settings.MaxDepth + 1,
			// One level of it is decided here, and the rest at the claim.
			//
			// The build has the least information it will ever have — nothing
			// has run, so every division past the first is decided against the
			// titles of work that does not exist yet — and it spends four
			// serial call-rounds per level while the person waits for the first
			// leaf to start. One level is what a graph needs to be wide enough
			// to dispatch; every level after it is worth more later, asked of
			// one claimed node against dependencies that have actually landed.
			// The ceiling above is unchanged: this is the same depth, decided
			// by whoever knows most at the time.
			BuildDepth: 1,
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
		contractUsage, err := plan.Contracts(settings.Context(ctx, compiled.Goal), structuring, graph, resident.ContractPlaybook(history), progress)
		if err != nil {
			log.Printf("note: could not write contracts: %v", err)
		}
		// The planner talks to the provider through a raw client rather than
		// through the billing seam, so its passes — spine, ground, fan-out,
		// bind, size, audit, expansion, briefs, contracts — are journaled here
		// by hand. It is the largest single structuring cost in the system and
		// it was the one the rail never saw.
		journalPlanSpend(history, plans, planClient, prefix, graph.Usage, contractUsage)
		subtree, err := resident.SubtreeFromPlan(graph, prefix)
		if err != nil {
			return store.Subtree{}, err
		}
		plans.put(prefix, graph, subtreeSink(subtree), workingModel, workingClient)
		journalScaleGate(history, prefix, compiled, structure, store.ScaleRoutePlanned, len(subtree.Nodes))
		return subtree, nil
	}
}

// journalScaleGate records how a job got its shape, in the same breath as the
// splice that gave it one.
//
// The gate above is the single biggest determinant of whether a run has any
// parallelism — everything the compiler did not read as project scale becomes
// exactly one leaf, with no planner and no fan-out — and until this existed it
// left nothing behind. A run that came out serial and a run that came out wide
// were distinguishable afterwards only by their nodes, so "why did this have no
// parallelism?" could be answered only by running it again, which does not
// answer it: the reading that decided it is a model's and does not repeat.
//
// It changes nothing. No caller reads it back, no branch turns on it, and a
// write that fails is a note in the log — the diagnosis is worth a row and never
// worth a job.
func journalScaleGate(history *store.Store, prefix string, compiled resident.Compiled, structure, route string, leaves int) {
	if history == nil {
		return
	}
	if err := history.RecordScaleGate(prefix, store.ScaleGate{
		Goal:      firstLine(compiled.Goal),
		Structure: structure,
		Scale:     compiled.Scale,
		Route:     route,
		Leaves:    leaves,
		Parts:     len(trimmedParts(compiled.Parts)),
	}); err != nil {
		log.Printf("note: could not journal the shape of %s: %v", prefix, err)
	}
}

// taskContract writes the working method for a job small enough to be one leaf.
//
// It is the same pass a planned job's leaves get, on a graph of one node, so
// the doctrine byte in the system message is shared with every other contract
// the machine writes and the whole thing costs exactly one call. It runs on the
// plan slot, like every structuring call, and under the job's own run key, so
// it lands wherever the rest of this job's structuring lands.
//
// The empty string is a real answer: a contract that could not be written
// degrades to the generic loop, which is what a leaf had before this existed.
// The progress callback is the same one a planned job's contract pass carries.
// A one-leaf job spends a real round-trip here and used to report none of it,
// so its card had a silent gap in exactly the place the loudest phase line
// lives for every larger job.
func taskContract(ctx context.Context, settings config.Config, planClient *liveClient, history *store.Store,
	goal string, progress plan.Progress) string {
	if planClient == nil || strings.TrimSpace(goal) == "" {
		return ""
	}
	_, structuring := planClient.Snapshot()
	if structuring == nil {
		return ""
	}
	graph := &plan.Graph{Goal: goal}
	// Summary and nothing else: the brief the leaf will actually receive is
	// assembled from the store at dispatch, and repeating the goal as a second
	// field would only pay for the same words twice.
	graph.Add(plan.Node{Kind: plan.KindWork, Summary: goal, Stage: 1})
	usage, err := plan.Contracts(settings.Context(ctx, goal), structuring, graph, resident.ContractPlaybook(history), progress)
	if err != nil {
		log.Printf("note: could not write the working method: %v", err)
	}
	journalPlanSpend(history, nil, planClient, "", usage)
	return strings.TrimSpace(graph.Nodes[0].Contract)
}

// recordOnNode writes one line to a piece of work's own record and never to the
// conversation (13.18's three-class law, and the noise this surface was caught
// posting on 2026-08-11).
//
// Everything this surface has to say while a job runs is the SAME class of
// thing: a ruler verdict, a split that queued more pieces, a note one worker
// left for its siblings, the files a failed leaf managed to write. None of them
// is a commitment, a delivery or a question — they are the machinery narrating
// itself, and a reader who wants them opens the room. They were all reaching the
// thread for one reason: the message carried a session id beside its node, and
// every conversation read takes the whole session.
//
// It is one function rather than five call sites so the law is impossible to
// forget at the next one: writing to a node's record is a different call from
// speaking, and the compiler is where that distinction should live.
func recordOnNode(graph *store.Store, nodeID, body string, role store.Role) {
	if graph == nil || strings.TrimSpace(nodeID) == "" || strings.TrimSpace(body) == "" {
		return
	}
	_, _ = thread.Record(graph, store.Message{Role: role, NodeID: nodeID, Body: body})
}

// jobNoteMark is the structural marker for a job-board note: written by code,
// read by code, so a board read can never mistake an anchored ask, receipt or
// progress post for a worker's shared line. It is a protocol byte, not a
// phrase the model is asked to produce.
const jobNoteMark = "⚑ "

// jobNoteBody is the marker and the worker's own words, and nothing between
// them. What used to sit between them was the writing leaf's title, clipped to
// the plan's rail width and joined on with a colon, which read as a sentence and
// was not one. Who wrote a note is a fact about the row, not a phrase to put in
// front of its content.
func jobNoteBody(line string) string {
	return jobNoteMark + strings.TrimSpace(line)
}

// jobNoteLine reads a message back as a board note, or says it is not one.
// Everything else that anchors to a node — questions, receipts, briefs,
// progress — declares itself in fields, and a note is what remains: an agent
// line carrying only the marker and its words.
func jobNoteLine(message store.Message) (string, bool) {
	if message.Role != store.RoleAgent || message.QuestionSeq != 0 ||
		message.CommandSeq != 0 || message.Brief != nil || message.Progress != nil {
		return "", false
	}
	if !strings.HasPrefix(message.Body, jobNoteMark) {
		return "", false
	}
	return strings.TrimPrefix(message.Body, jobNoteMark), true
}

// assembledBundle joins a bundle's finished parts in the asked order, or says
// the merge needs a model after all. Structure decides: a board note means a
// worker learned something the parts may not all reflect, and a part that
// failed or came back empty has nothing to join — both fall through to the
// ordinary sink leaf. The gate still reads the joined delivery afterwards, so
// a contradiction code cannot see buys the model pass through the ordinary
// revision path, critique in hand.
func assembledBundle(graph *store.Store, sink store.Node) (string, bool) {
	messages, err := graph.NodeMessages(sink.ID, 0, 12)
	if err != nil {
		return "", false
	}
	for _, message := range messages {
		if _, isNote := jobNoteLine(message); isNote {
			return "", false
		}
	}
	nodes, err := graph.SubtreeNodes(sink.ID)
	if err != nil {
		return "", false
	}
	parts := make([]store.Node, 0, len(nodes))
	for _, candidate := range nodes {
		if candidate.Parent == sink.ID {
			parts = append(parts, candidate)
		}
	}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].CreatedSeq < parts[j].CreatedSeq })
	if len(parts) < 2 {
		return "", false
	}
	var joined strings.Builder
	for _, part := range parts {
		if part.Status != store.Done || strings.TrimSpace(part.Summary) == "" {
			return "", false
		}
		if joined.Len() > 0 {
			joined.WriteString("\n\n")
		}
		joined.WriteString(strings.TrimSpace(part.Summary))
	}
	return joined.String(), true
}

// trimmedParts is the structural half of the bundle judgment: the model said
// which requests stand alone; code only refuses blanks.
func trimmedParts(parts []string) []string {
	kept := parts[:0:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return kept
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

// replanNodeBudget caps what one continuation round may add. A remainder is by
// definition smaller than the assignment it came from; a replan that wants
// more nodes than this is planning the job again, not finishing a leaf.
const replanNodeBudget = 12

// replanRemainder plans an exhausted leaf's remaining work with briefs,
// contracts, and the retained graph for call shapes and profile records —
// scoped to what the partial left undone. Falls back to one continuation node
// rather than failing: a leaf out of budget deserves at least one fresh
// worker on the remainder.
//
// It is deliberately NOT the same full pass a fresh project gets. A replan
// runs flat (no expansion — the depth-multiplies lesson of plan/expand.go,
// relearned at runtime when 27 nested rounds grew under one leaf), never
// convenes an ensemble (a remainder is finishing work, not a fresh judgment
// whose misses are worth buying recall against — and a "verify"-flavoured
// remainder goal reads exactly like panel work to the judge), and adds at
// most replanNodeBudget nodes.
func replanRemainder(settings config.Config, planClient, workClient *liveClient, plans *jobPlans, history *store.Store, terrainRoot string) resident.OverrunPlanFunc {
	return func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		workingModel, workingClient := workClient.Snapshot()
		_, structuring := planClient.Snapshot()
		anchor, _ := resident.PlanAnchorFromContext(ctx)
		progress := chatPlanProgress(history, anchor)
		graph, err := plan.Build(settings.Context(ctx, goal), structuring, goal, plan.Options{
			Recall: recallHits(history, goal, groundRecallLimit),
			// A remainder is planned against a workspace a worker has already been
			// writing in, which is the case where this is worth the most: the
			// replan can see what the exhausted leaf actually left behind.
			Terrain:       plan.RenderTerrain(terrainRoot, goal),
			FileShaped:    fileShapedAsk(terrainRoot, goal),
			SpineSamples:  settings.SpineSamples,
			ContextTokens: planWindow(settings, planClient),
			MaxDepth:      0,
			NodeBudget:    min(settings.NodeBudget, replanNodeBudget),
			Briefs:        true,
			Ensemble:      plan.EnsembleNever,
			// A remainder that the spine finds nothing gated in is one fresh
			// worker's assignment, and buying a seven-pass planning bundle to
			// discover that was measured at 13.8k and 23.9k prompt tokens on
			// two real extensions — five to eight times the whole structuring
			// cost of the jobs they were repairing. The full pipeline is still
			// there for the remainder the spine judges genuinely multi-stage.
			Undivided: true,
			Progress:  progress,
		})
		if err != nil {
			return store.Subtree{Nodes: []store.NodeSpec{{
				ID:    prefix,
				Brief: goal,
				Title: "Finish the remainder",
				Stage: 1,
			}}}, nil
		}
		contractUsage, err := plan.Contracts(settings.Context(ctx, goal), structuring, graph, resident.ContractPlaybook(history), progress)
		if err != nil {
			log.Printf("note: could not write repair contracts: %v", err)
		}
		journalPlanSpend(history, plans, planClient, prefix, graph.Usage, contractUsage)
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
	// A repair is the same shape one layer in. A gate that rejects a delivery,
	// or a leaf that runs out of budget, grows the job a round — and a round of
	// a top-level job is minted as a top-level node, because the delivery law
	// says only a root's completion is announced. So the repair stands BESIDE
	// the job it continues, exactly as a correction does, and would otherwise
	// open a brand-new empty directory: measured, one repair of a twelve-part
	// job reported "the workspace is empty ... the trace log holds no profile
	// content" while all twelve profiles sat in the job's own folder next door.
	//
	// The lineage is read off the id rather than the graph for the reason
	// OverrunLineage exists: the "-x" arithmetic is the id law and has exactly
	// one owner. Two identities, two readers, one answer.
	if base, round := resident.OverrunLineage(current.ID); round > 0 && base != current.ID {
		if origin, ok, err := graph.Node(base); err == nil && ok {
			return jobIDOf(graph, origin)
		}
		return base
	}
	// A correction revises a delivered thing, and the files it must see live
	// in the predecessor's workspace. The store's splice rail refuses a closed
	// parent, so the correction stands beside the job it corrects — the id in
	// its own instruction (written by the head, one format, one owner) is how
	// the two share a directory anyway.
	if predecessorID := correctionPredecessor(current.Provenance.Intent); predecessorID != "" {
		if predecessor, ok, err := graph.Node(predecessorID); err == nil && ok {
			return jobIDOf(graph, predecessor)
		}
	}
	return current.ID
}

// correctionPredecessor reads the delivered job a correction names. The line is
// head.SpliceCorrection's: "Correcting delivered work: <id> (<label>)" — the id
// runs from the prefix to the first space, and anything else is not a
// correction worth following.
func correctionPredecessor(intent string) string {
	if !head.IsCorrection(intent) {
		return ""
	}
	at := strings.Index(intent, head.CorrectionPrefix)
	if at < 0 {
		return ""
	}
	rest := strings.TrimSpace(intent[at+len(head.CorrectionPrefix):])
	if cut := strings.IndexAny(rest, " \n("); cut > 0 {
		rest = rest[:cut]
	}
	return strings.TrimSpace(rest)
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
		// A pattern across three jobs is exactly the evidence that revives a
		// belief the user has already refused once. The refusals travel with it.
		if refused := resident.RetractedBlock(graph, 10); refused != "" {
			input.WriteString("\n" + refused + "\n")
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
		if client.Routed() {
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
		// Sixty tokens is a yes, a no, and a line of reason — and nothing at all
		// on a model that reasons first, because the thinking is spent out of
		// this same budget before the verdict is written. That reads here as
		// "sentinel returned no clear yes" on every wake forever. A cap is a cap
		// and not a purchase, so the number has to hold what the reply can
		// legitimately need; head/scribe.go carries the full accounting.
		response, err := client.CompleteWithMessages(settings.Context(ctx, "sentinel"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: system}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: input}}},
		}, ai.WithMaxTokens(1024))
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
		// A CAP IS NOT A PURCHASE, and 30 was arithmetic on the answer: five
		// words are ten tokens, so thirty looked generous. On a model that
		// reasons before it speaks the thinking is spent out of this same budget
		// first and the call returns nothing at all — the same failure that left
		// every room in the rail untitled (head/scribe.go, where the mechanism
		// and its one escalation are written out).
		response, err := client.CompleteWithMessages(settings.Context(ctx, "title"), []ai.Message{
			{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: titleGoalPrompt}}},
			{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: firstLine(goal)}}},
		}, ai.WithMaxTokens(512))
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
		// The vetoed half of the same memory. Without it the distiller sees only
		// status='active' and re-proposes what the user already refused, which
		// the store then silently declines to write — a paid call whose whole
		// output is a lesson nobody is allowed to keep.
		if refused := resident.RetractedBlock(graph, 10); refused != "" {
			input += "\n\n" + refused
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
		if client.Routed() {
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

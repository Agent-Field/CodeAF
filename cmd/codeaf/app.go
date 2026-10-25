package main

// ── codeaf app: THE DESKTOP APP'S BACKEND ───────────────────────────────────
//
// `codeaf app` is started by the desktop app as a child process and spoken to
// over its own stdin and stdout, one JSON object per line
// (docs/design/app-v3/PROTOCOL.md). internal/appwire owns the bytes; this file
// owns what the methods mean.
//
// EVERY AGENT LIVES IN THIS ONE PROCESS, the way a person's tabs do. An agent
// the app opens is a conversation built by the very assembly a host builds one
// with ([assembleEngine]) and spoken to through a real [remote.Client] over an
// in-memory pipe ([remote.Loopback]), so the app's conversations are the same
// conversations, with the same toolbelt, gate and journal, as every other
// window's. Nothing is spawned per agent.
//
// ONE DIFFERENCE FROM A HOST, AND IT IS THE DIRECTORY. A host is one workspace
// and moves the process into it; this process holds conversations in several
// workspaces at once, so it moves nowhere. Every tool already resolves against
// its conversation's own workspace rather than the process's directory, and a
// new conversation records the workspace the app asked for as where the person
// stood, since the process's own directory is wherever the app was started.
//
// AND IT IS LAZY BY LAW. Startup opens agents with work or recovery to settle;
// idle conversations stay as indexed records, crew members included. Viewing,
// sending or waking opens one through the same per-agent gate. Listing agents,
// tasks and crews reads the disk, and a conversation left alone for
// [appAgentIdle] is let go and rebuilt transparently on demand.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clipboard"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/leave"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

// appAgentIdle is how long a conversation the app holds may sit with no turn
// running, no question waiting and nothing sent before it is let go. Letting go
// closes the conversation in this process and nothing else: its journal is on
// disk, its state still reads idle, and the next send opens it again.
const appAgentIdle = 10 * time.Minute

// appSweepEvery is how often the idle rule above is asked.
const appSweepEvery = 30 * time.Second

// appCrewPoll is how often the teams file is looked at for crew.changed, once
// the app has asked for the crews at all. It is one stat when nothing moved.
const appCrewPoll = 2 * time.Second

// appReleaseWait bounds how long closing one conversation waits for its turn's
// last events to be written before the outbox is shut regardless.
const appReleaseWait = 5 * time.Second

func runApp(args []string) error {
	localGUI := consumeAppLocalGUI()
	flags := commandFlags("app")
	homeFlag := flags.String("home", "", "codeaf's state folder for this app, instead of CODEAF_HOME or ~/.codeaf")
	turnLong := flags.Duration("turn-long", appTurnLong, "how long a reply runs before the app is told it is running long")
	schema := flags.Bool("schema", false, "print the wire's JSON Schema, generated from its Go types, and exit")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("usage: codeaf app [--home dir] [--schema]")
	}
	if *schema {
		out, err := appwire.SchemaJSON()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err
	}
	if dir := strings.TrimSpace(*homeFlag); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if err := os.Setenv(home.EnvVar, abs); err != nil {
			return err
		}
	}
	// STDOUT IS THE PROTOCOL'S AND NOBODY ELSE'S. The wire keeps the real
	// descriptor, and anything else in this process that prints to stdout
	// lands on stderr, where the app collects logs.
	wire := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = wire }()

	// THE ONE ROAD OUT (internal/leave): a first SIGTERM, SIGINT or SIGHUP
	// starts the quit's careful wrap-up, and a second ends the process at once.
	// The app's process ending is the app leaving too, even while something
	// else still holds its pipe open, and leaving is bounded (appleave.go).
	ctx, leaving := context.WithCancel(context.Background())
	defer leaving()
	defer leave.On(leaving, nil)()
	guard.Go("app/parent", func() { watchAppParent(ctx, os.Getppid, appParentPoll, leaving) })
	startHeapTrimmer(ctx)
	// THE APP'S KEYS LIVE IN THIS PROCESS'S MEMORY, installed before anything
	// can read a key, and no profile write may put one on disk while they are.
	defer config.UseAppCredentials(config.NewAppCredentials())()
	server := appwire.NewServer(wire)
	backend := newAppBackend(server, appOpenInProcess)
	// Deferred here, before the cleanups below are, so it closes only after
	// they have run: the bound still covers every one of them, and stands down
	// once the door has returned (appleave.go).
	returned := make(chan struct{})
	defer close(returned)
	guard.Go("app/leave", func() { leaveWithin(ctx, backend.left, returned, appLeaveWait, appLeaveNow) })
	// Capture the previous holders before crews, quit resumes or client calls
	// can open a journal and overwrite the evidence with this process's claim.
	backend.snapshotCloseClaims()
	backend.useClipboard(clipboard.NewSystem(localGUI))
	if *turnLong > 0 {
		backend.turnLong = *turnLong
	}
	// The crews are watched from the start: a manager's start or a member
	// woken by its manager must not wait for the window to ask for crews.
	backend.memoryStore = func() (*store.Store, error) {
		proc, err := openAppEngineProcess()
		if err != nil || proc.Memory == nil {
			return nil, errors.New("memory is off")
		}
		return proc.Memory, nil
	}
	backend.searchWords = func(terms string, limit int) ([]store.ConversationHit, error) {
		proc, err := openAppEngineProcess()
		if err != nil || proc.Search == nil {
			return nil, errors.New("conversation search is unavailable")
		}
		return proc.Search.SearchConversations(terms, limit)
	}
	// ONE SUPERVISOR FOR EVERY CONVERSATION THE APP BUILDS (appservices.go).
	services := openAppServices()
	appServiceDoor.Store(services)
	backend.attachServices(services)
	guard.Go("app/crews-start", func() { backend.crews() })
	// THE APP IS AN OPEN WINDOW for as long as it runs, so the clock that runs
	// standing orders runs while it is open (apporders.go).
	orders, closeOrders := appOrdersStore()
	backend.orderStore = orders
	defer closeOrders()
	// Do not start a paid-capable clock before the legacy cap is adopted.
	if err := backend.settleDayLimit(); err == nil {
		defer keepAutomationsWindow("app")()
	}
	guard.Go("app/orders", func() { backend.watchOrders(backend.sweepStop) })
	// And what the last quit stopped in the middle of a reply carries on.
	guard.Go("app/resume-after-quit", backend.resumeAfterQuit)
	backend.startCloseWatch()
	err := server.Serve(ctx, os.Stdin, appwire.Hello{Version: resolvedVersion(), PID: os.Getpid(), Home: home.Dir(), Features: []string{"agents.create.attachments", "crews.message.attachments"}}, backend)
	if backend.assembled.Load() {
		// The process half every conversation shared, closed the way the pipe
		// engine closes it once its conversation is gone (engine.go).
		session.CloseUsage()
		closeEngineProcess()
	}
	return err
}

// appConversation is what the backend needs of one open conversation. A
// [remote.Agent] is one; a test's fake is another.
type appConversation interface {
	Submit(ctx context.Context, text string) (<-chan session.Event, error)
	// SubmitMessage is Submit with the person's own words and the attachments
	// that rode on them, kept on the message's line in the record.
	SubmitMessage(ctx context.Context, message session.Submission) (<-chan session.Event, error)
	Interrupt()
	Model() string
	Title() string
	NeedsPerson() bool
	WatchTaskUpdates() (<-chan session.Event, func())
	// The question lane and its one answer door, the same pair the chat's
	// question page uses: every approval and every question the conversation
	// raises, answers and withdrawals included, from whichever window.
	WatchQuestions() (<-chan session.Event, func())
	ResolveQuestion(session.Answer) error
	SetModel(model string)
	SetSpendRail(usd float64) error
	// Usage is what the conversation has spent so far, by the provider's own
	// figures, which its budget is measured against.
	Usage() session.Usage
	// ResolveConsentRemember answers one approval and says how long the answer
	// lasts; the app uses it for an always that wrote a rule first.
	ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope)
	// How hard the model is asked to think, per model.
	ReasoningFor(model string) string
	SetReasoningFor(model, level string)
	// The task graph's app reads and verbs (internal/session's task_app.go),
	// the move of a running reply to a task, and the job registry's notices.
	AppTasks() ([]session.AppTaskView, error)
	AppTask(id string) (session.AppTaskView, bool, error)
	AppTaskAction(intent session.AppTaskIntent) error
	// AppTaskActionReceipt is a note's or a brief's typed outcome
	// (internal/session's task_app_receipt.go): delivered, held, refused, or
	// unknown — never a sentence to classify.
	AppTaskActionReceipt(intent session.AppTaskIntent) session.AppTaskReceipt
	TaskRouting(id string, changes int) (session.TaskRoutingView, error)
	TaskRoutingAct(ctx context.Context, id string, act session.TaskRoutingAction) (session.TaskRoutingResult, error)
	TaskRoutingOptions(id string) (session.TaskRoutingOptions, error)
	AppQuickTasks() ([]session.AppQuickTaskView, error)
	AppQuickTaskAction(intent session.AppTaskIntent) ([]string, []string, error)
	MoveTurnToTask() error
	// Steer puts words into the running turn (internal/session's steer.go),
	// and RetryTask runs a failed task again.
	Steer(text string) (<-chan session.Event, error)
	RetryTask(id uint64) error
	JobNotices() ([]session.JobNotice, error)
	RerunProcess(id int) (session.JobNotice, error)
	Cancel(id string) (string, error)
}

// appOpenAsk is which conversation to open: a saved one by transcript, or a
// new one in a workspace. Minted is a folder this process minted a moment ago
// for agents.create: nobody else can be holding it, so it is never joined.
type appOpenAsk struct {
	Models             *session.ConversationModels
	AutomaticModels    *bool
	TeamListeningSince time.Time

	Workspace  string
	Transcript string
	Model      string
	New        bool
	Minted     bool
	// ClockS is how many seconds a proposed task waits before it starts on
	// its own: the app's own setting, never the terminal's.
	ClockS int
}

// appConn is an opened conversation and the one way to give it back.
type appConn struct {
	// UserMessages comes from the opener's replay, including compacted history.
	UserMessages int

	Conv       appConversation
	Workspace  string
	Transcript string
	Release    func()
	// Follow is the turns this connection did not start — the ones the
	// conversation starts on its own and the ones another window sends —
	// when the conversation is reached across a connection. Nil is a
	// conversation held in this process, which hands its own turns over
	// directly (appwakes.go).
	Follow func() <-chan remote.Following
	// Passes is the compaction passes asked for outside any turn, as their
	// events arrive (appcompaction.go); nil where the connection has none.
	Passes func() <-chan session.Event
	// Assembled says this process built the conversation itself, so the shared
	// engine half must be closed when the app leaves.
	Assembled bool
	// Lent says this is a factory stage the factory owns, lent as a view
	// (app_lend.go): its model, posture and budget are not the app's to set.
	Lent bool
}

type appOpener func(appOpenAsk) (*appConn, error)

// appHostDialTimeout bounds each host dial while looking for the one holding
// a conversation.
const appHostDialTimeout = 500 * time.Millisecond

// appOpenInProcess opens a conversation for the app. A saved conversation that
// a window's host already holds is JOINED there, as a second view of the same
// conversation, because opening it here as well would be a second writer on its
// journal. Everything else is built in this process.
func appOpenInProcess(ask appOpenAsk) (*appConn, error) {
	clock := ask.ClockS
	launch := &remote.LaunchShape{TeamListeningSince: ask.TeamListeningSince, Interactive: true, IntentNames: true, HandOverOnAsk: true, ProposalClockS: &clock, ChoiceClockS: clock, QuitGraceS: appQuitGraceS, LiveToolOutput: true, StandingOrders: true, QuickTasks: true}
	if conn, err := appJoinExisting(ask, launch); conn != nil || err != nil {
		if err == nil {
			err = enableAppConversationModels(conn.Conv)
		}
		return conn, err
	}

	workspace := ask.Workspace
	transcript := ask.Transcript
	hello := remote.Hello{
		Workspace: workspace,
		Session:   transcript,
		New:       ask.New,
		Model:     ask.Model,
		// An app agent keeps the name its first message gave it: the engine's
		// own naming does not rename it afterwards.
		// And only the person moves a running reply to a task, and a proposed
		// task's clock is the app's, as is a suggested choice's.
		Launch: launch,
	}
	// The agent this boot built, kept so the app can tell when somebody else
	// closes it (below). Boot runs once, before Loopback returns.
	var booted atomic.Pointer[closedAgent]
	loop, err := remote.Loopback(hello, remote.Options{
		Boot: func(h remote.Hello) (*remote.Engine, error) {
			// THE APP'S OWN OPENER FIRST, so a process half that failed for the
			// want of a key before the window handed one over is opened again
			// now; [assembleEngine] then finds it open. Without it a first
			// message sent after the key arrived failed on the memo's old error.
			if _, err := openAppEngineProcess(); err != nil {
				return nil, err
			}
			eng, err := assembleEngine(h, workspace, transcript, workspace)
			if err == nil {
				err = enableAppConversationModels(eng.Agent, ask.Models)
			}
			if err == nil && ask.AutomaticModels != nil {
				if door, ok := eng.Agent.(interface{ SetAutomaticModels(bool) error }); ok {
					err = door.SetAutomaticModels(*ask.AutomaticModels)
				} else {
					err = fmt.Errorf("model selection policy is unsupported")
				}
			}
			if err == nil {
				if c, ok := eng.Agent.(closedAgent); ok {
					booted.Store(&c)
				}
			}
			return eng, err
		},
	})
	if err != nil {
		return nil, err
	}
	// AN AGENT THE APP OWNS CAN BE CLOSED UNDERNEATH IT. A paused factory stage
	// is booted here as the app's own conversation by a crew tick; when the
	// stage resumes, the stage maker takes the journal by closing this agent
	// (stageTakeJournal), which a remote.Session is never told. Without a watch
	// the view is a zombie that stays held and the resumed stage's card reaches
	// nobody. The app owns this agent, so its own release closes it as before;
	// the watch only learns that somebody else did.
	stop := make(chan struct{})
	var once sync.Once
	release := func() {
		once.Do(func() {
			close(stop)
			_ = loop.Close()
		})
	}
	if c := booted.Load(); c != nil {
		watchAgentClosed(*c, stop, release)
	}
	return appConnOf(loop.Client, release, true), nil
}

// appJoinExisting joins only a conversation an engine already holds.
// A nil launch leaves that engine's launch policy untouched for a naming ask.
func appJoinExisting(ask appOpenAsk, launch *remote.LaunchShape) (*appConn, error) {
	if ask.Transcript != "" && !ask.Minted {
		// A JOIN CARRIES THE APP'S LAUNCH, and the engine applies what of it a
		// conversation another window opened can carry (engine.go's JoinedWith).
		hello := remote.Hello{Workspace: ask.Workspace, Session: ask.Transcript, Join: true, Launch: launch}
		// A CONVERSATION THIS PROCESS ALREADY HOLDS IS LENT, NEVER REOPENED:
		// the factory worker's running stage is the app's own, not another
		// window's (app_lend.go). Only a factory stage is lent.
		if agent, ok := liveStageFor(ask.Transcript); ok {
			return appLendLive(agent, ask, hello)
		}
		if socket, err := enginehost.DialStartingHost(ask.Workspace); err == nil {
			client, err := remote.Dial(socket, "", hello)
			if err == nil {
				return appConnOf(client, func() { _ = client.Close() }, false), nil
			}
			_ = socket.Close()
		}
		// HELD, BUT NOT BY THIS WORKSPACE'S HOST: a crew member a manager
		// started lives in its manager's host. Every host is asked, and the
		// one holding it answers; opening it here as well would be refused by
		// its journal's lock, and is never tried while somebody holds it.
		if session.InUse(ask.Transcript) {
			for _, path := range enginehost.Sockets() {
				socket, err := net.DialTimeout("unix", path, appHostDialTimeout)
				if err != nil {
					continue
				}
				client, err := remote.Dial(socket, "", hello)
				if err == nil {
					return appConnOf(client, func() { _ = client.Close() }, false), nil
				}
				_ = socket.Close()
			}
			return nil, appwire.Errorf(appwire.CodeConflict, "this conversation is open in another codeaf window that the app cannot reach; close it there, or open it in that window")
		}
	}
	return nil, nil
}

func appConnOf(client *remote.Client, release func(), assembled bool) *appConn {
	welcome := client.Welcome()
	users := 0
	if welcome.UserMessages != nil {
		users = *welcome.UserMessages
	} else {
		// An older holder can still answer from its replay rather than making
		// this process parse a second copy of its journal.
		history := client.Agent().EarlierHistory()
		for _, entry := range appRecordItems(session.Record{Earlier: history.Entries, Floor: history.Floor, Entries: client.Agent().Transcript()}) {
			if entry.Role == "user" {
				users++
			}
		}
	}
	return &appConn{UserMessages: users, Conv: client.Agent(), Workspace: welcome.Workspace, Transcript: welcome.SessionFile, Release: release, Follow: client.Follow, Passes: client.Passes, Assembled: assembled}
}

// appBackend is the [appwire.Engine] over this process's conversations.
type appBackend struct {
	// restMemo is the task lists of members at rest, so reading them does not
	// hold their conversations (apprestmemo.go).
	restMemo  appRestMemo
	restSlots chan struct{}

	// organizeProposer supplies a naming door when the backend is embedded without held conversations.
	organizeProposer session.TeamProposer

	// factoryRooms runs this connection's one-time stamp of teams the factory made before the
	// stamp was kept (factoryStampStoredRooms).
	factoryRooms sync.Once
	// crewHistory holds the crew process-history walks (appcrewprocesspage.go).
	crewHistory crewHistory
	// searchWords reads every conversation's words for search.all; nil keeps
	// the messages group empty.
	searchWords appSearchWords
	// memoryStore is the memory store the Settings window lists; nil is memory
	// off.
	memoryStore appMemoryStore
	// orderStore is the automations store standing orders are kept in
	// (apporders.go); nil is standing orders off.
	orderStore appOrderStore
	// stopPhases puts the engine's phase news back the way it was, and
	// stopProgress the factory runner's progress news (appfactory_progress.go).
	stopPhases   func()
	stopProgress func()
	// factories is the attached-factory registry (appfactory.go), made by the
	// first factory.* call.
	factories   *appFactoryHub
	factoryOnce sync.Once
	// clipboard is where a made invitation is copied (appfactory_links.go). Nil
	// is UNAVAILABLE, the production default; only useClipboard sets it.
	clipboard clipboard.Copier
	// local is the factory this engine serves or joined for its own person,
	// only once a window asks (appfactory_setup.go).
	local appLocalFactory
	// stoppingAll is the agents a processes.stop_all is in flight for; a
	// second for the same agent is answered busy (appprocessactions.go).
	stoppingAll map[string]bool
	// profileDir is the profile whose teams are the crews, and the process's
	// own when empty; layer is the crews' app layer and crewRun the watch over
	// them, started by the first crew call.
	profileDir string
	crewOnce   sync.Once
	layer      *appCrewLayer
	crewRun    *appCrewRun
	emit       appwire.Emitter
	open       appOpener
	now        func() time.Time
	// after is the claim-expiry clock, replaceable without sleeping in tests.
	after         func(time.Duration, func()) func()
	closeRecovery appCloseRecovery
	footerMu      sync.Mutex
	turns         atomic.Uint64
	assembled     atomic.Bool

	mu               sync.Mutex
	referenceWorkers int
	// reportReads are the report reads in flight and reportCtx the context they
	// all run under, cancelled by Close (appprogramreport.go); both under mu.
	reportReads  int
	reportCtx    context.Context
	reportCancel context.CancelFunc
	referenceN   int // in-flight and staged reference snapshots, under mu
	agents       map[string]*appAgent
	closing      bool

	sweepStop chan struct{}
	sweepDone chan struct{}
	// left closes when Close begins, whatever started the leaving: the app's
	// pipe closing, its shutdown request, a signal or the app's process ending.
	// runApp bounds the leaving from that moment (appleave.go).
	left chan struct{}
	// spent is the last figure spend.changed said, so it is said on a change.
	spent float64
	// mint makes a new conversation's folder and answers its transcript path:
	// the one piece of agents.create that happens before the answer.
	mint func(workspace string) (string, error)
	// unmint takes back a folder mint made when what it was made for was
	// refused (appteamstart.go); nil is [appUnmint].
	unmint func(transcript string)
	// commitStart rewrites a team start's claim to say its goal may be handed
	// over (appteamstartclaim.go); nil is [appStartClaims.commit].
	commitStart func(appStartClaims, appStartClaim) error
	// startMu is the one team start in flight (appteamstart.go): a second
	// concurrent start is refused busy, never queued.
	startMu sync.Mutex
	// starting is the request_id of the start in flight under startMu, read
	// under mu: a claim of it with no crew yet is that start, not a lost one.
	starting string
	// actions is the admission of session.act (appsessionactions.go): which
	// attempts are running, on which conversation.
	actions appActionGate
	// createAction and settleAction write a session action's claim
	// (appsessionactionclaim.go); nil is [appActionClaims.create] and
	// [appActionClaims.settle].
	createAction func(appActionClaims, appActionClaim) (bool, error)
	settleAction func(appActionClaims, appActionClaim) error
	// warmed closes when [appBackend.Warm] has finished, which Close waits
	// for: the process half it opens is closed after the backend is.
	warmed chan struct{}
	// keys are the keys the app handed over (appaccount.go); keyClient and
	// keyAddress replace the network and OpenRouter's address in a test, and
	// vendored and codexBegin the other providers' descriptions and the
	// ChatGPT sign-in's browser flow.
	keys       *config.AppCredentials
	keyClient  *http.Client
	keyAddress func(provider string) string
	vendored   func() []modelsource.Source
	codexBegin func(context.Context) (codexConnectFlow, error)
	// balances is providers.balance's memory and lifetime (appproviderbalance.go),
	// made by the first ask.
	balances *appBalances
	// signIn is OpenRouter's browser sign-in waiting for the person, if any.
	signIn appSignIn
	// accountAuth is the ChatGPT account's sign-in, waiting or the last one to
	// end (appaccount_auth.go): its own slot, because it has an attempt and a
	// state the window reads, where OpenRouter's has neither.
	accountAuth *appAccountAuth
	// providerWarm closes when the latest fetch of the providers' model lists
	// is over ([appBackend.warmProviderModels]); models.list waits a moment for it.
	providerWarm chan struct{}
	// state is the app's own file: setup, its settings, budgets and raises.
	state *appState
	// dayAuth is the daily limit's one-time hand-over from this file's old cap.
	dayAuth appDayAuthority
	// today replaces the usage ledger's figure for today in a test.
	today func(time.Time) float64
	// turnLong is how long a reply runs before turn.long ([appTurnLong]).
	turnLong time.Duration
	// inbox is every open ask as one list (appinbox.go); emit is its tap.
	inbox *appInbox
	// toolQuiet is how long a call runs before its row is said again with a
	// fresh tail ([appToolQuiet]).
	toolQuiet time.Duration
	// follows are the processes whose output the app is following.
	follows map[string]*appFollow
	// asleep says the Mac is asleep (power.sleep until power.wake).
	asleep bool
	// collections is the collections store (appcollections.go), opened by
	// the first call that finds a file to open and closed with the backend;
	// collectionsPath replaces where it is in a test.
	collectionsMu     sync.Mutex
	collections       *workspace.Store
	collectionsClosed bool
	collectionsPath   string
	// services is the app's one supervisor of kept-running processes
	// (appservices.go); nil is the capability absent.
	services *appServices
	// notices are the episodes' kept notices (appnotices.go), opened once.
	notices     *appNoticeLog
	noticesOnce sync.Once
}

// appAgent is every agent this process has heard of: listed off the disk,
// created or opened. live is nil while no conversation is held for it.
type appAgent struct {
	// crewHeld keeps an established crew hold due after its engine closes.
	crewHeld bool

	// listening is when this light record began watching crew traffic.
	listening time.Time

	// gate serializes opening and letting go of this one agent, so two sends
	// racing for a closed conversation open it once.
	gate       sync.Mutex
	nameGate   sync.Mutex // serializes a rename RPC and its view update
	view       appwire.Agent
	transcript string
	live       *appLive
	// failed is the words of the last turn that ended in failure, which is
	// what agents.retry sends again; a turn that ends well clears it.
	failed string
	// failedKind is how that turn failed; a turn refused for money is run
	// again when the limit is raised.
	failedKind string
	// failedShown is what that turn's entry showed, when it carried
	// attachments: a retry sends the same words and shows the same message.
	failedShown *appSending
	// failedClient is the client id of a first message that failed before it
	// was handed to its conversation (bootFailed), which a retry carries: that
	// message never reached the accepted line, so its id is still unused.
	failedClient string
	// step is the title of the call the running turn is on, the live line
	// a crew's member row and the agent's own row show while it works.
	step string
	// boot is what agents.create asked of a conversation not yet open, taken
	// by whichever road opens it first; opening says one is being assembled.
	boot    *appBoot
	opening bool
	// minted says this process made the conversation's folder (agents.create),
	// which is what lets it take a blank one away again at quit; briefed says
	// a crew's brief is on its way to it, the first message of a member a
	// manager started, which it reads off the crew's log rather than from the
	// person.
	minted  bool
	briefed bool
	// users is how many messages of the person's the transcript holds, which
	// is what an entry's id is counted from ([appUserEntryID]); counted says
	// it has been read.
	users   int
	counted bool
	// marks are the turn number and client id of each message of the
	// person's this process sent, by its place, so a listing can say them.
	marks map[int]appTurnMark
	// calls is the work log's record of this agent's tool calls (apptools.go).
	calls *appCallLog
	// queue is the person's messages waiting for the turn to end, attach the
	// attachments kept for the next message, and walk the last walk of the
	// workspace's files (appconvo.go).
	queue      []appQueued
	queueN     int
	attach     map[string]appAttach
	attachN    int
	referenceN int // in-flight and staged reference snapshots, under b.mu
	walk       *appWalk
}

// appTurnMark is what this process knows of one of the person's messages: the
// turn it opened and the client's own id for it. A conversation whose engine
// keeps the id on the message's line ([session.Submission.ClientID]) is read
// from there first; this id is the fallback for a line with none, and for a
// host that cannot keep one.
//
// IT IS NOT WHERE A MESSAGE'S WORDS OR ATTACHMENTS COME FROM. Those are on the
// message's own line in the record, written with it ([appSubmission]), so a
// listing reads them the same before a restart and after one.
type appTurnMark struct {
	turn     uint64
	clientID string
}

// appBoot is agents.create's ask, kept for the background open.
type appBoot struct {
	models    *session.ConversationModels
	automatic *bool
	model     string
	budget    *float64
}

// appFirst is a created agent's first message: the words the model is sent,
// and, when attachments rode on it, what its entry shows instead.
type appFirst struct {
	words, clientID string
	shown           *appSending
}

// appLive is one conversation the process holds.
type appLive struct {
	// replayJobs holds history facts recovered once, before the standing lane opens.
	replayJobs map[int]session.JobNotice

	conn       *appConn
	box        *appwire.Outbox
	stopTasks  func()
	stopTitles func()
	titlesDone chan struct{}
	running    bool
	// closeWake joins acceptance with the session consuming this keyed note;
	// either can arrive first over the loopback connection.
	closeWake *appCloseWake
	lastUse   time.Time
	pumpDone  chan struct{}
	lanesDone chan struct{}
	asksDone  chan struct{}
	stopAsks  func()
	tasks     map[string]appwire.Task
	// renders is the last record sent for each render, by id: the same news
	// arrives on the turn's stream and on the task lane, and each change is
	// said once (apprender.go).
	renders     map[string]appwire.Render
	mediaRecent []string
	mediaSeen   map[string]bool
	// artifactRecent and artifactSeen are mediaRecent's twin for deliverable
	// creations: a bounded window that drops the same append arriving on the
	// turn stream and the standing lane, never a cache of all history.
	artifactRecent []string
	artifactSeen   map[string]bool
	// asks are the open asks in the order they were raised, and questions the
	// engine's own question behind each, which is what an answer is built from.
	asks      []appwire.Ask
	questions map[string]session.Question
	// answered names the asks this app answered and the option it gave, so
	// their closing says so.
	answered map[string]string
	// consents are the approvals' calls as the turn announced them, by the
	// engine's question id: what an always is written down from.
	consents    map[uint64]appConsentCall
	savedScopes map[string]string
	// stopping says the person stopped the running turn.
	stopping bool
	// progressSaid is the ids of the factory runner's lines this conversation
	// last said live, newest last, at most [appProgressLook]
	// (appfactory_progress.go).
	progressSaid []string
	// money are the open budget asks this process raised itself, by ask id;
	// budgetAsked is the last budget level asked about, so it is asked once;
	// lastAnswer is the agent's last words, which a budget ask quotes.
	money       map[string]appMoneyAsk
	budgetAsked string
	lastAnswer  string
	// turn is the running turn's number and steps how many tool calls it has
	// made, which turn.long says.
	turn  uint64
	steps int
	// phases are the running tasks' phase words, quicks and conflicts the
	// quick tasks as last said and the files an undo found changed, and
	// processes and logs the processes as last said and their spools.
	phases map[string]string
	// said is what each running task last said about its present (the gap
	// it is closing, what it waits on, its own phase), calls the call each
	// is making right now, and pilots the way to stop watching each one.
	said      map[string]string
	calls     map[string]string
	pilots    map[string]func()
	quicks    map[string]appwire.Quick
	conflicts map[string][]string
	processes map[string]appwire.Process
	logs      map[string]string
	// slept are the processes a sleep stopped, said once on wake.
	slept []appSlept
	// steers counts the turn's steers; adopting is how many fallen-through
	// steers are about to become turns; batchDone are the answered calls of
	// each batch of asks; notes are the notes sent to each task not yet
	// read, and noteN counts them; closed says the conversation was let go.
	steers    int
	adopting  int
	batchDone map[string][]appwire.BatchItem
	notes     map[string][]appNotePending
	noteN     int
	// msgBase and noteBase are where the next turn's answer and notice counts
	// start, and lastN, lastMsgs and lastNotices are where the last turn left
	// them under the person's message it answered.
	msgBase, noteBase            int
	lastN, lastMsgs, lastNotices int
	stopWakes                    func()
	// stopPasses stops the pass lane's reader, and compacting says a pass
	// the app asked for is running (appcompaction.go).
	stopPasses func()
	compacting bool
	// retries counts the running turn's waits before a step is asked again,
	// retryPhase and retryUntil are the last one said, and retrySaid says the
	// next void attempt was already said with its countdown.
	retries    int
	retryPhase provider.Phase
	retryUntil time.Time
	retrySaid  bool
	closed     bool
}

// appConsentCall is the call one approval is about.
type appConsentCall struct {
	Tool string
	Args string
}

// needsYou is the one reading of "waiting on the person": an open ask, or the
// engine's own word for a wait the question lane does not carry. Callers hold
// b.mu.
func (live *appLive) needsYou() bool {
	return len(live.asks) > 0 || live.conn.Conv.NeedsPerson()
}

// restingState is what an agent with no turn running reads as. Callers hold
// b.mu.
func (live *appLive) restingState() string {
	if live.needsYou() {
		return appwire.StateNeedsYou
	}
	return appwire.StateIdle
}

func newAppBackend(emit appwire.Emitter, open appOpener) *appBackend {
	keys := config.AppCredentialSource()
	if keys == nil {
		keys = config.NewAppCredentials()
	}
	b := &appBackend{
		keys:      keys,
		state:     &appState{},
		turnLong:  appTurnLong,
		toolQuiet: appToolQuiet,
		follows:   map[string]*appFollow{},
		open:      open,
		now:       time.Now,
		after: func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, fn)
			return func() { t.Stop() }
		},
		agents:    map[string]*appAgent{},
		restSlots: make(chan struct{}, appRestReads),
		sweepStop: make(chan struct{}),
		sweepDone: make(chan struct{}),
		left:      make(chan struct{}),
		mint:      appMint,
	}
	b.emit = b.newAppInbox(emit)
	b.stopPhases = b.listenPhases()
	b.stopProgress = b.listenProgress()
	guard.Go("app/sweep", b.sweep)
	return b
}

// appMint makes a new conversation's folder in workspace, the very folder a
// host's new conversation is minted as, with the workspace stamped as where the
// person stood. It is a directory and a small file, so agents.create can answer
// with the agent's lasting id before anything slow happens.
func appMint(workspace string) (string, error) {
	place, err := v3NextSessionFrom(session.Place{}, workspace, workspace)
	if err != nil {
		return "", err
	}
	return place.Transcript(), nil
}

// appAgentID is a conversation's id from its transcript: the folder's name.
func appAgentID(transcript string) string {
	return filepath.Base(filepath.Dir(filepath.Clean(transcript)))
}

// locked runs fn holding b.mu. Every critical section in this file goes
// through it or opens with a deferred unlock, so a panic a guard absorbs never
// leaves the lock held (internal/guard's lock law).
func (b *appBackend) locked(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn()
}

// agent answers the record for id, making one if this is the first time.
func (b *appBackend) agent(id string) (a *appAgent) {
	b.locked(func() { a = b.agentLocked(id) })
	return a
}

// agentLocked answers the record for id, making one if this is the first time.
func (b *appBackend) agentLocked(id string) *appAgent {
	a := b.agents[id]
	if a == nil {
		a = &appAgent{view: appwire.Agent{ID: id, State: appwire.StateIdle}}
		b.agents[id] = a
	}
	return a
}

// rowView is an agent as the disk says it is.
func appRowView(row session.SessionRow) appwire.Agent {
	at := row.At
	if at.IsZero() {
		at = row.Created
	}
	state := appwire.StateIdle
	if row.Live {
		switch {
		case row.Presence.NeedsPerson():
			state = appwire.StateNeedsYou
		case row.Presence.State == session.PresenceWorking:
			state = appwire.StateWorking
		}
	}
	identity := row.ModelIdentity
	if identity != nil {
		copy := *identity
		if !row.Live {
			copy.Active, copy.Pending = "", false
		}
		identity = &copy
	}
	return appwire.Agent{ModelIdentity: identity, FactoryOwned: factoryOwnedConversation(row.Transcript), ID: row.ID, Title: row.Title, Workspace: row.Workspace, Model: row.Model, State: state, UpdatedAt: at}
}

// remember files what the disk said about an agent, without touching one this
// process is holding: a held conversation's own facts are newer than the disk's.
func (b *appBackend) remember(row session.SessionRow) appwire.Agent {
	b.watchCloseClaim(row.Dir)
	// Read freshness against this backend's clock, including in deterministic
	// tests, rather than trusting a row another reader dated earlier.
	row.Presence, row.Live = session.ReadSessionPresence(row.Dir, b.now())
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.agentLocked(row.ID)
	if a.transcript == "" {
		a.transcript = row.Transcript
	}
	disk := appRowView(row)
	disk.Retryable = a.failed != "" && a.live == nil
	busyElsewhere := a.live == nil && (disk.State == appwire.StateWorking || disk.State == appwire.StateNeedsYou)
	if !busyElsewhere && (a.live != nil || a.view.State == appwire.StateClosed || a.view.State == appwire.StateStopped) {
		view := a.view
		if view.Title == "" {
			view.Title = row.Title
		}
		return view
	}
	// A NAME THIS PROCESS KNOWS OUTLIVES A ROW THAT HAS NONE: a member a
	// manager started is named by its brief here, and its folder's row may
	// never say so, because a conversation does not name itself from a crew's
	// note (session's firstExchangeLocked).
	if disk.Title == "" {
		disk.Title = a.view.Title
	}
	a.view = disk
	return a.view
}

// find answers the agent for id, reading the disk for one this process has not
// heard of yet.
func (b *appBackend) find(id string) (*appAgent, error) {
	var a *appAgent
	held := false
	b.locked(func() {
		a = b.agents[id]
		held = a != nil && a.transcript != ""
	})
	if held {
		// A cached transcript is not a lease on another window's working state.
		b.refreshElsewhere(a)
		return a, nil
	}
	row, ok := appRowOnDisk(id)
	if !ok {
		return nil, appwire.Errorf(appwire.CodeNotFound, "there is no conversation with id %s", id)
	}
	b.remember(row)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.agents[id], nil
}

// appRowOnDisk finds one conversation folder by id under the places root
// without walking every conversation on the machine: one directory listing and
// one stat per project.
func appRowOnDisk(id string) (session.SessionRow, bool) {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return session.SessionRow{}, false
	}
	root := session.PlacesRoot()
	buckets, err := os.ReadDir(root)
	if err != nil {
		return session.SessionRow{}, false
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		dir := filepath.Join(root, bucket.Name(), id)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		transcript := session.Place{Dir: dir}.Transcript()
		if row, ok := session.ReadRows([]string{transcript})[filepath.Clean(transcript)]; ok {
			return row, true
		}
		// A folder minted and never spoken in is still a conversation the app
		// may have created; its meta names the workspace.
		meta, err := session.LoadMeta(dir)
		if err != nil || strings.TrimSpace(meta.ID) == "" {
			continue
		}
		return session.SessionRow{ID: id, Dir: dir, Transcript: transcript, Title: meta.Title, Workspace: meta.Workspace, Created: meta.Created}, true
	}
	return session.SessionRow{}, false
}

// ListAgents reads the disk and never opens a conversation.
func (b *appBackend) ListAgents(workspace string) ([]appwire.Agent, error) {
	want := ""
	if strings.TrimSpace(workspace) != "" {
		want = filepath.Clean(workspace)
	}
	matches := func(where ...string) bool {
		if want == "" {
			return true
		}
		for _, w := range where {
			if w != "" && filepath.Clean(w) == want {
				return true
			}
		}
		return false
	}
	// AN AGENT NOBODY HAS TOUCHED FOR THE SETTING'S DAYS READS CLOSED: its
	// journal is kept, and opening or sending to it opens it again.
	idleDays := appIdleCloseDays
	_ = b.state.read(func(f *appStateFile) { idleDays = f.idleDays() })
	stale := time.Time{}
	if idleDays > 0 {
		stale = b.now().Add(-time.Duration(idleDays) * 24 * time.Hour)
	}
	seen := map[string]bool{}
	var agents []appwire.Agent
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if row.DeletionPending || factoryOwnedConversation(row.Transcript) || !matches(row.Workspace, row.ProjectDir) {
			continue
		}
		seen[row.ID] = true
		view := b.remember(row)
		if !stale.IsZero() && view.State == appwire.StateIdle && !view.UpdatedAt.IsZero() && view.UpdatedAt.Before(stale) && !b.held(view.ID) && !b.crewKeeps(view.ID) {
			view.State = appwire.StateClosed
		}
		view.SpentUSD, view.LastActiveAt = row.Spend, row.At
		b.tidyMarks(&view, row.Archived)
		agents = append(agents, view)
	}
	// A CONVERSATION THE WALK DID NOT FIND IS LISTED ONLY ONCE SOMEBODY HAS
	// SAID SOMETHING IN IT. The walk skips a folder the person never spoke in,
	// so what this process holds beyond it is an agent made here whose first
	// words are on their way, a crew member a manager started, whose first
	// message is its brief, or a blank conversation, which is never listed
	// (appblank.go). The journal is read outside the lock.
	type beyondWalk struct {
		view       appwire.Agent
		transcript string
		said       bool
	}
	var beyond []beyondWalk
	b.locked(func() {
		for id, a := range b.agents {
			if !seen[id] && !a.view.FactoryOwned && !factoryOwnedConversation(a.transcript) && (a.live != nil || a.opening) && matches(a.view.Workspace) {
				beyond = append(beyond, beyondWalk{view: a.view, transcript: a.transcript, said: a.saidLocked()})
			}
		}
	})
	for _, held := range beyond {
		if held.said || appSpokenIn(held.transcript) {
			agents = append(agents, held.view)
		}
	}
	b.nameUntitled(agents)
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].UpdatedAt.After(agents[j].UpdatedAt) })
	for i := range agents {
		b.dress(&agents[i])
	}
	return agents, nil
}

// CreateAgent answers a new agent AT ONCE and assembles its conversation
// behind the answer. With no workspace it is the person's home folder; with a
// budget the conversation's own spend limit is set before anything is spent;
// with words the first turn starts as soon as the conversation is open and the
// agent is named by them. A conversation that cannot be opened says so as a
// failed turn and a stopped agent.
func (b *appBackend) CreateAgent(ask appwire.CreateAsk) (appwire.Agent, error) {
	prep, err := b.prepareCreate(ask)
	if err != nil {
		return appwire.Agent{}, err
	}
	view, launch, err := b.createAgentHeld(prep, "")
	if err != nil {
		return appwire.Agent{}, err
	}
	launch()
	return view, nil
}

// appCreatePrep is an agents.create made ready before anything exists: its
// folder resolved and every attachment read, ONCE. A caller that has to make
// something else true between the reading and the making (a team start, which
// commits its team in between) hands this value on rather than reading the
// attachments a second time, so a file that goes away in between cannot turn
// into a refusal after the thing it was refused for already exists.
type appCreatePrep struct {
	ask      appwire.CreateAsk
	resolved string
	text     string
	kept     []appAttach
	// creation is what the first message states ([appSending.creation]):
	// an explicit none from [appBackend.prepareCreate], a crew's id when a
	// crew start sets it, nil for a bare open with no words.
	creation *session.CreationContext
}

// prepareCreate is everything agents.create can refuse, checked with nothing
// made: the workspace, shutdown, and every attachment read whole.
func (b *appBackend) prepareCreate(ask appwire.CreateAsk) (appCreatePrep, error) {
	if err := (session.Submission{ClientID: ask.ClientID}).Check(); err != nil {
		return appCreatePrep{}, appEngineError(err)
	}
	// No workspace is the work folder the person chose, or their home folder.
	workspace := ask.Workspace
	if strings.TrimSpace(workspace) == "" {
		_ = b.state.read(func(f *appStateFile) { workspace = f.workFolder() })
	}
	resolved, err := engineWorkspace(workspace)
	if err != nil {
		return appCreatePrep{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	if b.isClosing() {
		return appCreatePrep{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	// EVERY ATTACHMENT IS READ BEFORE ANYTHING IS MADE: one that is malformed,
	// over a bound, or a file that is not there refuses the create whole, so a
	// refusal leaves no folder, no agent and no event behind it. It is a stat
	// per file, outside every lock.
	if err := appwire.CheckAttachSources(ask.Attachments); err != nil {
		return appCreatePrep{}, err
	}
	kept := make([]appAttach, 0, len(ask.Attachments))
	for _, source := range ask.Attachments {
		attach, err := b.prepareAttach(resolved, nil, appwire.Attachment{
			Kind: source.Kind, Path: strings.TrimSpace(source.Path), URL: strings.TrimSpace(source.URL),
		}, source.Text, true)
		if err != nil {
			return appCreatePrep{}, err
		}
		kept = append(kept, attach)
	}
	if err := appAttachChars(kept); err != nil {
		return appCreatePrep{}, err
	}
	return appCreatePrep{ask: ask, resolved: resolved, text: strings.TrimSpace(ask.Text), kept: kept, creation: appNoTeam()}, nil
}

// createAgentHeld is [appBackend.CreateAgent] after its preparation and up to,
// and not including, the background open and first turn: it answers the agent
// and a launch that starts them. A caller that must make something else true
// BEFORE the first message can reach the model (a team the manager belongs to,
// and the durable record of the request that made it) holds the launch until
// it is, and never calls it when that fails. minted, when set, is the
// conversation folder's transcript the caller already made with b.mint; with
// it and no budget, nothing here can refuse.
func (b *appBackend) createAgentHeld(prep appCreatePrep, minted string) (appwire.Agent, func(), error) {
	ask, resolved, text, kept := prep.ask, prep.resolved, prep.text, prep.kept
	transcript := minted
	if transcript == "" {
		var err error
		if transcript, err = b.mint(resolved); err != nil {
			return appwire.Agent{}, nil, appEngineError(err)
		}
	}
	model := strings.TrimSpace(ask.Model)
	a := b.agent(appAgentID(transcript))
	if ask.BudgetUSD != nil && *ask.BudgetUSD > 0 {
		// The budget is the app's to keep: the engine's figure is the open
		// conversation's only, and it is bound again at every open.
		usd, id := *ask.BudgetUSD, appAgentID(transcript)
		err := b.state.write(func(f *appStateFile) error {
			if f.Budgets == nil {
				f.Budgets = map[string]float64{}
			}
			f.Budgets[id] = usd
			return nil
		})
		if err != nil {
			return appwire.Agent{}, nil, appEngineError(err)
		}
	}
	var view appwire.Agent
	first := appFirst{clientID: ask.ClientID}
	b.locked(func() {
		// The attachments are numbered as staged ones are, so an id on the
		// first entry is never one a later chip is given.
		for i := range kept {
			a.attachN++
			kept[i].view.ID = "at" + strconv.Itoa(a.attachN)
		}
		first.words = appAttachWords(text, kept)
		// THE PERSON'S OWN WORDS are what the entry and the record show when
		// they gave them, exactly as given, as agents.send keeps said; the
		// model is sent first.words either way.
		said := text
		if ask.Said != nil {
			said = *ask.Said
		}
		first.shown = appAttachShown(said, kept)
		if first.shown == nil && ask.Said != nil && first.words != "" {
			first.shown = &appSending{text: said}
		}
		// The first words are the person's own message, so they state the
		// creation context the door knows: an explicit none for agents.create,
		// the crew's id for the goal crews.start hands its new manager.
		if first.words != "" && prep.creation != nil {
			if first.shown == nil {
				first.shown = &appSending{text: said, bare: true}
			}
			first.shown.creation = prep.creation
		}
		a.transcript = transcript
		// THE NAME THE TAB DRAWS UNTIL THE ENGINE'S ARRIVES is the one the engine
		// itself writes at acceptance: the person's words, or the names of the
		// files they attached when they said nothing, cut to forty characters
		// (session.OpeningName). Attachment paths do not become file names.
		a.view.Title = session.OpeningName(text, appDescribedAll(kept))
		// A message of a link or a paste alone has no name of the person's to
		// represent: the tab draws what its chip says, as it always has, and the
		// engine leaves the conversation unnamed (intentname.go).
		if a.view.Title == "" && len(kept) > 0 {
			a.view.Title = session.FirstMessageName(kept[0].view.Title)
		}
		a.view.Workspace = resolved
		a.view.Model = model
		a.view.State = appwire.StateIdle
		if first.words != "" {
			a.view.State = appwire.StateWorking
			a.view.Retryable = false
		}
		a.view.UpdatedAt = b.now()
		a.boot = &appBoot{model: model, budget: ask.BudgetUSD, automatic: ask.AutomaticModels, models: ask.Models}
		a.opening, a.minted = true, true
		a.users, a.counted = 0, true
		view = a.view
	})
	view = b.stampAgentBudget(a)
	// A BLANK AGENT IS NOT ANNOUNCED. The caller has its answer; the first
	// words, or the name a manager's start gives a member, are what bring it
	// to every window (appblank.go).
	if first.words != "" {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return view, func() { guard.Go("app/create", func() { b.bootAgent(a, first) }) }, nil
}

// bootAgent opens a created agent's conversation and starts its first turn.
func (b *appBackend) bootAgent(a *appAgent, first appFirst) {
	a.gate.Lock()
	defer a.gate.Unlock()
	view, opened, err := b.ensureLocked(a)
	b.locked(func() { a.opening = false })
	if err != nil {
		b.bootFailed(a, first, err)
		return
	}
	if first.words == "" {
		// Opened blank, it is said only once something was said in it in
		// the meantime; otherwise its first words say it (appblank.go).
		said := false
		b.locked(func() { said = a.saidLocked() })
		if opened && said {
			b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
		return
	}
	// The agent already reads working; the turn's own start says the rest.
	if _, err := b.startLocked(a, first.words, first.clientID, first.shown); err != nil {
		b.bootFailed(a, first, err)
	}
}

// bootFailed is a created agent whose conversation could not be opened or
// started: a failed turn, and an agent that reads stopped. A failure BEFORE the
// words were handed to the conversation is one the person can retry: the retry
// sends the same words, shows the same attachments and carries the same client
// id, since the first attempt never reached the accepted line. A failure of the
// hand-over itself ([appUnsureSend]) is NOT armed for a retry: the host may
// have taken the words, and a retry could send them twice. It says so, and the
// accepted line is the client's to look for by its client id.
func (b *appBackend) bootFailed(a *appAgent, first appFirst, err error) {
	text := first.words
	words := err.Error()
	var wire *appwire.Error
	if errors.As(appEngineError(err), &wire) {
		words = wire.Message
	}
	kind := appwire.ErrorOther
	unsure := appSendUnsure(err)
	if unsure {
		kind = appwire.ErrorUncertain
		words = appUnsureWords + " (" + words + ")"
	}
	var view appwire.Agent
	b.locked(func() {
		if text != "" && !unsure {
			a.failed, a.failedKind, a.failedShown, a.failedClient = text, appwire.ErrorOther, first.shown, first.clientID
			a.view.Retryable = true
		}
		a.boot = nil
		if a.live == nil {
			a.view.State = appwire.StateStopped
		}
		view = a.view
	})
	if text != "" {
		turn := b.turns.Add(1)
		b.emit.Emit(appwire.EventTurnDone, appwire.TurnDone{Agent: view.ID, Turn: turn, OK: false, Error: words, ErrorKind: kind})
	}
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
}

// appUnsureWords is what a person reads when words were handed to a
// conversation and no answer came back: they may have been sent.
const appUnsureWords = "this message may have been sent; look at the conversation before sending it again"

// appUnsureSend is a turn whose words were handed to the conversation and
// whose hand-over then failed: a dropped link or a host that took the message
// without saying how can both look like this, so the words MAY HAVE BEEN
// SENT. It wraps the wire error the send has always answered, so a caller that
// does not ask reads exactly what it read before; the first message of a
// created agent and agents.retry ask, and never offer such words again as
// though they were proven unsent.
type appUnsureSend struct{ err error }

func (e *appUnsureSend) Error() string { return e.err.Error() }
func (e *appUnsureSend) Unwrap() error { return e.err }

// appSendUnsure says err is a hand-over that may have delivered its words.
func appSendUnsure(err error) bool {
	var unsure *appUnsureSend
	return errors.As(err, &unsure)
}

// OpenAgent attaches a conversation to a saved agent and answers it.
func (b *appBackend) OpenAgent(id string) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	view, opened, err := b.ensure(a)
	if err != nil {
		return appwire.Agent{}, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return view, nil
}

// ensure holds a conversation for a, opening it if none is held, and reports
// whether it had to.
func (b *appBackend) ensure(a *appAgent) (appwire.Agent, bool, error) {
	a.gate.Lock()
	defer a.gate.Unlock()
	return b.ensureLocked(a)
}

// ensureLocked is [appBackend.ensure] for a caller already holding a.gate.
func (b *appBackend) ensureLocked(a *appAgent) (appwire.Agent, bool, error) {
	if err := b.settleDayLimit(); err != nil {
		return appwire.Agent{}, false, appEngineError(err)
	}
	var view appwire.Agent
	var transcript, workspace string
	var boot *appBoot
	held := false
	b.locked(func() {
		view, held = a.view, a.live != nil
		transcript, workspace = a.transcript, a.view.Workspace
		if !held {
			boot, a.boot = a.boot, nil
		}
	})
	if held {
		return view, false, nil
	}
	if b.isClosing() {
		return appwire.Agent{}, false, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	// A stored conversation carries its model even before another turn.
	// Opening without it would replace the saved choice with the profile default.
	ask := appOpenAsk{Workspace: workspace, Transcript: transcript, Model: view.Model, ClockS: appProposedClockS}
	b.locked(func() { ask.TeamListeningSince = a.listening })
	_ = b.state.read(func(f *appStateFile) { ask.ClockS = f.clock() })
	if boot != nil {
		ask.Model, ask.Minted = boot.model, true
		ask.AutomaticModels = boot.automatic
		ask.Models = boot.models
	}
	conn, err := b.open(ask)
	if err != nil {
		// Loopback errors may have crossed a codec and lost their Go type.
		// The journal lock still proves an ownership conflict in that case.
		if errors.Is(err, session.ErrSessionLocked) || session.InUse(transcript) {
			return appwire.Agent{}, false, appwire.Errorf(appwire.CodeConflict, "%s", err)
		}
		return appwire.Agent{}, false, appEngineError(err)
	}
	// Opening an agent that was closed or put away takes it out again.
	b.unmarkTidy(view.ID)
	if boot != nil && boot.budget != nil && *boot.budget > 0 {
		if err := conn.Conv.SetSpendRail(*boot.budget); err != nil {
			conn.Release()
			return appwire.Agent{}, false, appEngineError(err)
		}
	}
	if err := b.bindBudgetConn(view.ID, conn); err != nil {
		conn.Release()
		return appwire.Agent{}, false, appEngineError(err)
	}
	return b.hold(a, conn, workspace), true, nil
}

// hold makes conn the agent's live conversation. Callers hold a.gate.
func (b *appBackend) hold(a *appAgent, conn *appConn, workspace string) appwire.Agent {
	if conn.Assembled {
		b.markAssembled()
	}
	live := &appLive{
		conn: conn, box: appwire.NewOutbox(b.emit), lastUse: b.now(),
		tasks: map[string]appwire.Task{}, lanesDone: make(chan struct{}), asksDone: make(chan struct{}),
		questions: map[string]session.Question{}, answered: map[string]string{},
		consents: map[uint64]appConsentCall{}, money: map[string]appMoneyAsk{},
		phases: map[string]string{}, quicks: map[string]appwire.Quick{}, conflicts: map[string][]string{},
		processes: map[string]appwire.Process{}, logs: map[string]string{},
		said: map[string]string{}, calls: map[string]string{}, pilots: map[string]func(){},
	}
	replayTranscript := conn.Transcript
	if replayTranscript == "" {
		b.locked(func() { replayTranscript = a.transcript })
	}
	live.replayJobs = appReplayJobs(replayTranscript)
	startTitles := b.prepareTitles(a, live)
	title, model := conn.Conv.Title(), conn.Conv.Model()
	var modelIdentity *session.ModelIdentity
	if door, ok := conn.Conv.(interface{ ModelIdentity() session.ModelIdentity }); ok {
		state := door.ModelIdentity()
		modelIdentity = &state
	}
	effort := conn.Conv.ReasoningFor(model)
	if dial, ok := conn.Conv.(interface{ ConversationEffort() string }); ok {
		effort = dial.ConversationEffort()
	}
	// A CONVERSATION WITH NO NAME OF ITS OWN THAT A CREW KNOWS goes by the
	// word its crew calls it, which for a member a manager started is its
	// brief's first words: held again after a relaunch, nothing else names it.
	if strings.TrimSpace(title) == "" {
		var id, known string
		b.locked(func() { id, known = a.view.ID, a.view.Title })
		if strings.TrimSpace(known) == "" {
			title = b.memberNames()[id]
		}
	}
	users := conn.UserMessages
	crewHeld := b.crewKeeps(a.view.ID)
	var view appwire.Agent
	b.locked(func() {
		if !a.counted {
			a.users, a.counted = users, true
		}
		a.view.Effort = effort
		a.view.ModelIdentity = modelIdentity
		if conn.Transcript != "" {
			a.transcript = conn.Transcript
		}
		if conn.Workspace != "" {
			workspace = conn.Workspace
		}
		a.view.Workspace = workspace
		if title != "" {
			a.view.Title = title
		}
		if model != "" {
			a.view.Model = model
		}
		a.view.State = live.restingState()
		a.view.Context = appContextOf(conn.Conv)
		if a.view.UpdatedAt.IsZero() {
			a.view.UpdatedAt = b.now()
		}
		a.live = live
		a.crewHeld = a.crewHeld || crewHeld
		view = a.view
	})
	startTitles()
	lane, stop := conn.Conv.WatchTaskUpdates()
	live.stopTasks = stop
	id := view.ID
	guard.Go("app/tasks", func() {
		defer close(live.lanesDone)
		for event := range lane {
			b.workEvent(id, live, event)
		}
	})
	questions, stopQuestions := conn.Conv.WatchQuestions()
	live.stopAsks = stopQuestions
	live.stopWakes = b.watchWakes(a, live)
	live.stopPasses = b.watchPasses(a, live)
	guard.Go("app/asks", func() {
		defer close(live.asksDone)
		for event := range questions {
			b.questionEvent(a, id, live, event)
		}
		// A LANE THAT ENDS WHILE THE CONVERSATION IS STILL HELD IS A DEAD
		// CONNECTION: letting go stops the lane only after taking the
		// conversation away, so the only other way it ends is the engine
		// behind it going (internal/remote's bury finishes every lane).
		b.died(a, live)
	})
	return view
}

// appAskID is the stable id of one ask: the engine's own question key.
func appAskID(q session.Question) string {
	return string(q.Kind) + ":" + q.Token()
}

// appAskOf is the engine's question as the app's ask.
func appAskOf(agent string, q session.Question) appwire.Ask {
	ask := appwire.Ask{ID: appAskID(q), Agent: agent, Kind: appwire.AskQuestion, Title: strings.TrimSpace(q.Head), Detail: strings.TrimSpace(q.Reason)}
	if q.Kind == session.QuestionConsent {
		ask.Kind, ask.Subkind = appwire.AskApproval, appwire.SubkindPermission
		ask.Irreversible = q.Stakes == session.StakesIrreversible
		ask.Scopes, ask.AlwaysAsk, ask.Outside = q.ConsentScopes, q.AlwaysAsk, q.Outside
	}
	if q.Kind == session.QuestionKeep {
		ask.Kind, ask.Subkind = appwire.AskApproval, appwire.SubkindKeepRunning
		ask.KeepRunning = appKeepFacts(q)
	}
	for _, option := range appOptionsOf(q) {
		if option.id == appwire.OptionAlways && q.Kind == session.QuestionConsent && len(ask.Scopes) == 0 {
			continue
		}
		ask.Options = append(ask.Options, appwire.AskOption{ID: option.id, Label: option.label})
	}
	ask.Subject = appAskSubject(agent, q.Subject)
	return ask
}

// appAskSubject is what a question is about, in the app's words: an order is
// an automation named by its id, a task node by its app task id.
func appAskSubject(agent string, s session.SubjectRef) *appwire.AskSubject {
	switch s.Kind {
	case session.SubjectOrder:
		return &appwire.AskSubject{Kind: appwire.SubjectAutomation, Ref: s.Ref}
	case session.SubjectNode:
		return &appwire.AskSubject{Kind: appwire.SubjectTask, Ref: appTaskID(agent, s.ID)}
	case session.SubjectCall:
		return &appwire.AskSubject{Kind: appwire.SubjectCall, Ref: s.CallID}
	case session.SubjectRun:
		return &appwire.AskSubject{Kind: appwire.SubjectRun, Ref: s.Ref}
	case session.SubjectPage:
		return &appwire.AskSubject{Kind: appwire.SubjectPage, Ref: s.Name}
	case session.SubjectAccount:
		return &appwire.AskSubject{Kind: appwire.SubjectAccount, Ref: s.Ref}
	}
	return nil
}

// appOption is one answer as the app names it, with the engine key behind it.
type appOption struct {
	id, key, label string
}

// appOptionsOf is a question's answers in the app's names. A permission's are
// allow_once, deny and always, in that order, and always is left off a call
// that cannot be undone. Any other question keeps the engine's own keys, and so
// does a permission whose answers are not the three the gate offers.
func appOptionsOf(q session.Question) []appOption {
	var plain []appOption
	for _, option := range q.Options {
		plain = append(plain, appOption{id: option.Key, key: option.Key, label: option.Label})
	}
	if keep, ok := appKeepOptions(q); ok {
		return keep
	}
	if q.Kind != session.QuestionConsent {
		return plain
	}
	byID := map[string]appOption{}
	for _, option := range q.Options {
		action, known := session.AnswerFromKey(session.QuestionConsent, option.Key)
		if !known {
			return plain
		}
		id := appwire.OptionAllowOnce
		switch {
		case !action.Allow:
			id = appwire.OptionDeny
		case action.Scope == session.ConsentToolSession:
			id = appwire.OptionAlways
		}
		if _, twice := byID[id]; twice {
			return plain
		}
		byID[id] = appOption{id: id, key: option.Key, label: option.Label}
	}
	var mapped []appOption
	for _, id := range []string{appwire.OptionAllowOnce, appwire.OptionDeny, appwire.OptionAlways} {
		option, offered := byID[id]
		if !offered || (id == appwire.OptionAlways && q.Stakes == session.StakesIrreversible) {
			continue
		}
		mapped = append(mapped, option)
	}
	return mapped
}

// appOptionNamed finds the answer the app named by its id.
func appOptionNamed(q session.Question, id string) (appOption, bool) {
	for _, option := range appOptionsOf(q) {
		if option.id == id {
			return option, true
		}
	}
	return appOption{}, false
}

// appOptionOfKey is the app's name for an answer the engine reports by key.
func appOptionOfKey(q session.Question, key string) string {
	for _, option := range appOptionsOf(q) {
		if option.key == key {
			return option.id
		}
	}
	return ""
}

// appReceipt is the line a closed ask leaves in the conversation: what was
// decided, by whom, and when, in the person's own clock.
func appReceipt(by, option string, answer *session.Answer, at time.Time) string {
	word := "Answered"
	switch option {
	case appwire.OptionAllowOnce:
		word = "Allowed once"
	case appwire.OptionDeny:
		word = "Denied"
	case appwire.OptionAlways:
		word = "Always allowed"
	case appwire.OptionAllow:
		word = "Kept running"
	case appwire.OptionAllowNever:
		word = "Kept running, never restarted"
	}
	who := "you"
	switch by {
	case appwire.ClosedByStopped:
		return "Not answered · the agent stopped · " + at.Local().Format("15:04")
	case appwire.ClosedByElsewhere:
		if answer == nil {
			return "No longer needed · " + at.Local().Format("15:04")
		}
		if answer.DecidedBy == session.DecidedByDial {
			who = "your settings"
		}
	}
	return word + " · " + who + " · " + at.Local().Format("15:04")
}

// questionEvent keeps the open asks in step with the question lane and says
// every change on the wire, the agent's state with it.
func (b *appBackend) questionEvent(a *appAgent, id string, live *appLive, event session.Event) {
	if event.Question == nil {
		return
	}
	q := *event.Question
	if q.Kind == session.QuestionConsent && q.ConsentScopes == nil && q.AlwaysAsk == "" {
		var call appConsentCall
		b.locked(func() { call = live.consents[q.ID] })
		if call.Tool == "" {
			call.Tool = q.Subject.Name
		}
		q.ConsentScopes, q.AlwaysAsk = config.ApprovalScopes(call.Tool, json.RawMessage(call.Args))
		if q.Stakes == session.StakesIrreversible && q.AlwaysAsk == "" {
			q.ConsentScopes, q.AlwaysAsk = nil, "critical"
		}
	}
	askID := appAskID(q)
	switch event.Kind {
	case session.EventQuestion:
		if q.Subject.Kind == session.SubjectCall {
			b.callAsked(a, id, live, q.Subject.CallID, askID)
		}
		ask := appAskOf(id, q)
		appAskFacts(id, &ask, q)
		defer b.restateBatch(live, q)
		restated, moved := false, false
		var view appwire.Agent
		b.locked(func() {
			ask.HoldingUp = askHoldingUpLocked(live, ask)
			if _, open := live.questions[askID]; open {
				// The lane replays what is open when it is subscribed again,
				// and a question restated in place is the same ask.
				restated = true
				live.questions[askID] = q
				for i := range live.asks {
					if live.asks[i].ID == askID {
						live.asks[i] = ask
					}
				}
				return
			}
			live.questions[askID] = q
			live.asks = append(live.asks, ask)
			view, moved = b.restateLocked(a, live)
		})
		if restated {
			return
		}
		live.box.Event(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
		if moved {
			live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
	case session.EventQuestionAnswered, session.EventQuestionWithdrawn:
		open, moved := false, false
		by, option := appwire.ClosedByElsewhere, ""
		var answer *session.Answer
		var scope string
		var view appwire.Agent
		b.locked(func() {
			if _, open = live.questions[askID]; !open {
				return
			}
			if event.Kind == session.EventQuestionAnswered {
				answer = event.Answer
				if answer == nil {
					answer = &session.Answer{}
				}
				option = appOptionOfKey(live.questions[askID], answer.Key)
			}
			if given := live.answered[askID]; given != "" {
				by, option = appwire.ClosedByApp, strings.TrimPrefix(given, appAnsweredMark)
				if answer == nil {
					answer = &session.Answer{}
				}
			}
			scope = live.savedScopes[askID]
			delete(live.savedScopes, askID)
			b.dropAskLocked(live, askID)
			view, moved = b.restateLocked(a, live)
		})
		if !open {
			return
		}
		at := b.now()
		words := appReceipt(by, option, answer, at)
		// HANDED BACK, the agent's own pick answered it, and the receipt says so.
		if picked, decided := strings.CutPrefix(option, appDecideMark); decided {
			option = "you_decide"
			words = "You let it decide · it chose " + picked + " · " + at.Local().Format("15:04")
		}
		// A PROPOSAL ITS CLOCK STARTED closes with no answer from anybody; the
		// task it became, already said, is what tells it from one taken back.
		if q.Kind == session.QuestionTask && answer == nil {
			var task appwire.Task
			known := false
			b.locked(func() { task, known = live.tasks[appTaskID(id, q.ID)] })
			if known && !task.Proposed && task.State != appwire.TaskFailed {
				// THE RECEIPT SAYS HOW LONG THE CLOCK WAS, so a person reading it
				// later knows what they had to answer in.
				clock := appProposedClockS
				_ = b.state.read(func(f *appStateFile) { clock = f.clock() })
				words = "Started on its own after " + strconv.Itoa(clock) + "s · " + at.Local().Format("15:04")
			}
		}
		live.box.Event(appwire.EventAskClosed, appwire.AskClosed{Agent: id, ID: askID, By: by, Option: option, At: at})
		receipt := appwire.Entry{ID: appReceiptEntryID(askID), Agent: id, Kind: appwire.EntryReceipt, At: at, Text: words}
		if scope != "" {
			receipt.Text = "Always allowed · " + scope + " · you · " + at.Local().Format("15:04")
			receipt.Link = &appwire.EntryLink{Label: "Settings › Permissions", Target: "settings:permissions"}
			_ = b.keepAppEntry(a, receipt, q.Subject.CallID)
		}
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: receipt})
		if moved {
			live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
		}
	}
}

// dropAskLocked forgets one ask. Callers hold b.mu.
func (b *appBackend) dropAskLocked(live *appLive, askID string) {
	if q, ok := live.questions[askID]; ok && q.Kind == session.QuestionConsent {
		delete(live.consents, q.ID)
	}
	delete(live.savedScopes, askID)
	delete(live.money, askID)
	delete(live.questions, askID)
	delete(live.answered, askID)
	kept := live.asks[:0]
	for _, ask := range live.asks {
		if ask.ID != askID {
			kept = append(kept, ask)
		}
	}
	live.asks = kept
}

// restateLocked recomputes a held agent's state after its asks moved, and
// reports whether it changed. Callers hold b.mu.
func (b *appBackend) restateLocked(a *appAgent, live *appLive) (appwire.Agent, bool) {
	if a.live != live {
		return a.view, false
	}
	state := live.restingState()
	if live.running && state == appwire.StateIdle {
		state = appwire.StateWorking
	}
	if state == a.view.State {
		return a.view, false
	}
	a.view.State = state
	return a.view, true
}

// died is a held conversation whose engine went away without being let go:
// the agent reads stopped, its open asks close as stopped, and the next send
// opens the conversation again.
func (b *appBackend) died(a *appAgent, live *appLive) {
	mine := false
	var view appwire.Agent
	var asks []appwire.Ask
	b.locked(func() {
		if mine = a.live == live; !mine {
			return
		}
		a.live = nil
		a.view.State = appwire.StateStopped
		view = a.view
		asks = live.asks
		live.asks, live.questions = nil, map[string]session.Question{}
	})
	if !mine {
		return
	}
	at := b.now()
	for _, ask := range asks {
		live.box.Event(appwire.EventAskClosed, appwire.AskClosed{Agent: view.ID, ID: ask.ID, By: appwire.ClosedByStopped, At: at})
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: appReceiptEntryID(ask.ID), Agent: view.ID, Kind: appwire.EntryReceipt, At: at, Text: appReceipt(appwire.ClosedByStopped, "", nil, at),
		}})
	}
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	guard.Go("app/finish", func() { b.finish(live) })
}

// held says this process holds a conversation for the agent id.
func (b *appBackend) held(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.agents[id]
	return a != nil && (a.live != nil || a.opening)
}

// live answers the agent's held conversation, or nil.
func (b *appBackend) liveOf(a *appAgent) *appLive {
	b.mu.Lock()
	defer b.mu.Unlock()
	return a.live
}

// Send starts one turn, opening the conversation first if it is not held. It
// is the person's own send, so it states an explicit none: an automation made
// in its turn was made in no crew. Words the app writes itself go through
// [appBackend.send] with nothing stated, and stay unknown.
func (b *appBackend) Send(id, text, clientID string) (uint64, error) {
	b.resetCloseResumes(id)
	return b.send(id, text, clientID, appStated(text, appNoTeam()))
}

// send is [appBackend.Send] with what the person's entry shows, when that is
// not the words sent.
func (b *appBackend) send(id, text, clientID string, shown *appSending) (uint64, error) {
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	// The gate keeps the idle sweep from letting the conversation go between
	// opening it and starting the turn.
	a.gate.Lock()
	defer a.gate.Unlock()
	return b.sendLocked(a, text, clientID, shown)
}

// sendLocked opens a's conversation if it is not held and starts one turn on
// it. The caller holds a.gate.
func (b *appBackend) sendLocked(a *appAgent, text, clientID string, shown *appSending) (uint64, error) {
	// Opening can wake saved crew work, so reject an invalid identity before
	// the opener can start any model work on this message's behalf.
	if err := (session.Submission{ClientID: clientID}).Check(); err != nil {
		return 0, appEngineError(err)
	}
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		return 0, err
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	if turn, handled, err := b.tryFactoryStatus(a, text, clientID, shown); handled {
		return turn, err
	}
	return b.startLocked(a, text, clientID, shown)
}

// Retry sends the words of the agent's last failed turn again.
func (b *appBackend) Retry(id string) (uint64, error) {
	b.resetCloseResumes(id)
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	var text, clientID string
	var shown *appSending
	b.locked(func() { text, shown, clientID = a.failed, a.failedShown, a.failedClient })
	if text == "" {
		return 0, appwire.Errorf(appwire.CodeNotFound, "this agent has no failed turn to run again")
	}
	// A retry is the person's own send, never the queue's.
	if shown != nil && shown.queued {
		var again appSending
		b.locked(func() { again = *shown })
		shown = appRetryShown(again, text)
	}
	turn, err := b.send(id, text, clientID, shown)
	if appSendUnsure(err) {
		// THE RETRY MAY HAVE BEEN SENT. Its words are not offered again: a
		// second press could send them twice.
		b.locked(func() {
			if a.failed == text {
				a.failed, a.failedKind, a.failedShown, a.failedClient = "", "", nil, ""
				a.view.Retryable = false
			}
		})
		return 0, appwire.Errorf(appwire.CodeUncertain, "%s", appUnsureWords)
	}
	return turn, err
}

// appRetryShown is a failed queued message made the person's own send again:
// no longer queued, and nothing at all when what is left is only its words. A
// creation context keeps it: a retried crew message is still the message that
// crew accepted, and a retried ordinary one is still the person's.
func appRetryShown(again appSending, text string) *appSending {
	again.queued = false
	if again.creation == nil && len(again.described) == 0 && len(again.reads) == 0 && again.composed == nil && again.text == text {
		return nil
	}
	return &again
}

// startLocked starts one turn on a held conversation. The caller holds a.gate.
// shown, when set, is what the person's entry shows instead of text: their
// own words and the attachments the text was written from.
func (b *appBackend) startLocked(a *appAgent, text, clientID string, shown *appSending) (uint64, error) {
	return b.startTurnLocked(a, text, clientID, shown, nil)
}

// appTurnStarter replaces only submission; admission, correlation and events
// remain the ordinary send's so a picked harness is still one conversation turn.
type appTurnStarter func(*appLive, string) (<-chan session.Event, error)

func (b *appBackend) startTurnLocked(a *appAgent, text, clientID string, shown *appSending, start appTurnStarter) (uint64, error) {
	var id string
	var live *appLive
	var refused error
	b.locked(func() {
		id, live = a.view.ID, a.live
		switch {
		case live == nil:
			refused = appwire.Errorf(appwire.CodeEngine, "the conversation was let go before the turn could start")
		case live.running:
			refused = appwire.Errorf(appwire.CodeBusy, "this agent is already working on a turn")
		}
	})
	if refused != nil {
		return 0, refused
	}
	// A HOST THAT CANNOT KEEP MARKS IS SENT THE WORDS of a message whose only
	// extra is its creation context, exactly as it always was: it would drop the
	// context anyway, and the team is unknown there, which is true.
	if shown.marksNothing(text) && !appKeepsQueuedMarks(live.conn.Conv) {
		shown = nil
	}
	// A HOST THAT CANNOT KEEP THE MESSAGE'S MARK IS NOT SENT THE MESSAGE: the
	// refusal is the app's to read before the turn starts, and the person's
	// draft and chips stay as they were.
	if shown != nil && !appKeepsQueuedMarks(live.conn.Conv) {
		return 0, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot keep a message's said, reads or attachments; update its engine before sending")
	}
	// A local capability refusal must stay a definite refusal: no request was
	// dispatched, so referenceRefused may release an unsent snapshot.
	if !appKeepsAttachmentSources(live.conn.Conv, shown) {
		return 0, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot keep a recording's source and time range")
	}
	// PICTURES ARE NEVER SENT AS WORDS ALONE: a conversation whose engine has not
	// said it takes them refuses the message before the turn, and the chips stay.
	var composer appComposer
	if shown != nil && shown.composed != nil {
		var ok bool
		if composer, ok = live.conn.Conv.(appComposer); !ok || !composer.ComposedKnown() {
			return 0, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot take pictures with a message; update its engine before sending")
		}
	}
	// The receipt road also accepts ordinary text. A capable current host gives
	// that actual user message an identity without upgrading an older host's
	// plain-text promise or guessing a message id from its words.
	if composer == nil {
		if receipts, advertised := live.conn.Conv.(interface{ MessageReceiptsKnown() bool }); advertised && receipts.MessageReceiptsKnown() {
			if capable, ok := live.conn.Conv.(appComposer); ok && capable.ComposedKnown() {
				composer = capable
			}
		}
	}
	// THE CLIENT'S ID IS KEPT ON THE ACCEPTED LINE where the conversation's host
	// says it keeps one, so it survives this process; elsewhere it stays the
	// process-local fact it always was (a.marks) and no host is asked to drop it.
	keptID := appKeptClientID(live.conn.Conv, clientID)
	// An id the record would not keep as given is the app's to correct, and is
	// refused here as bad_params before any turn is claimed or record written.
	if keptID != "" {
		if err := (session.Submission{ClientID: keptID}).Check(); err != nil {
			return 0, appEngineError(err)
		}
	}
	// A conversation joined on a window's host can have heard from the person
	// in that window too, so its count is read again before it is used.
	if !live.conn.Assembled && live.conn.Transcript != "" {
		users := appUserCount(live.conn.Transcript)
		b.locked(func() { a.users = users })
	}
	// THE DAY CAP IS CHECKED BEFORE THE ENGINE HEARS OF THE TURN: a refused
	// turn sends nothing and costs nothing, and its words wait for a raise.
	now := b.now()
	// A LIMIT NOBODY CAN READ REFUSES THE TURN: not the default, not none.
	limit, limitErr := b.dayCap(now)
	if limitErr != nil {
		return 0, appEngineError(limitErr)
	}
	if limit > 0 {
		spent, err := b.spentTodayChecked(now)
		if err != nil {
			return 0, appEngineError(err)
		}
		if spent >= limit {
			turn := b.dayRefused(a, live, text, clientID, spent, limit)
			b.locked(func() { a.failedShown = shown })
			return turn, nil
		}
	}
	// The agent's budget is bound again, so a raise for today ends at midnight.
	if err := b.bindBudgetConn(id, live.conn); err != nil {
		return 0, appEngineError(err)
	}
	var turn uint64
	var n int
	entryText, attachments, reads := text, []appwire.Attachment(nil), []appwire.MessageRead(nil)
	b.locked(func() {
		if live.running {
			refused = appwire.Errorf(appwire.CodeBusy, "this agent is already working on a turn")
			return
		}
		live.running, live.stopping = true, false
		live.lastUse = b.now()
		turn = b.turns.Add(1)
		live.turn, live.steps, live.steers = turn, 0, 0
		live.retries, live.retrySaid, live.retryPhase = 0, false, ""
		n = a.users
		if a.marks == nil {
			a.marks = map[int]appTurnMark{}
		}
		a.marks[n] = appTurnMark{turn: turn, clientID: clientID}
		if shown != nil {
			entryText, attachments, reads = shown.text, shown.attachments, appReadsOf(shown.reads)
		}
	})
	if refused != nil {
		return 0, refused
	}
	// THE MESSAGE AND WHAT IT CARRIED ARE ACCEPTED TOGETHER: one call, one line
	// in the record. A refused call writes neither, and a message with nothing
	// riding on it is the plain Submit it always was.
	var events <-chan session.Event
	var err error
	var composedMark *session.ComposedMark
	if start != nil {
		events, err = start(live, keptID)
	} else if composer != nil {
		var receipt session.SubmissionReceipt
		message := appSubmission(text, shown)
		message.ClientID = keptID
		receipt, events, err = composer.SubmitComposed(context.Background(), message)
		if err == nil {
			composedMark = &session.ComposedMark{MessageID: receipt.MessageID, Media: receipt.Media, Delivery: &session.MessageDelivery{Mode: receipt.Delivery, Model: receipt.SeenBy}}
			if shown != nil && shown.composed != nil {
				shown.composed.receipt = &receipt
				composedMark.Annotation = shown.composed.annotation
			}
		}
	} else if shown != nil || keptID != "" {
		// An id with no mark beside it is still a SubmitMessage: Text is the
		// words as Submit takes them and Said stays nil, so the line reads as
		// the plain message it is.
		message := appSubmission(text, shown)
		message.ClientID = keptID
		events, err = live.conn.Conv.SubmitMessage(context.Background(), message)
	} else {
		events, err = live.conn.Conv.Submit(context.Background(), text)
	}
	if err != nil {
		b.locked(func() {
			live.running = false
			if start != nil {
				delete(a.marks, n)
			}
		})
		if start != nil {
			return 0, appEngineError(err)
		}
		// The words were handed over: whether the host took them is not known.
		return 0, &appUnsureSend{err: appEngineError(err)}
	}
	var view appwire.Agent
	b.locked(func() {
		a.users = n + 1
		a.view.State = appwire.StateWorking
		a.view.Retryable = false
		if live.needsYou() {
			a.view.State = appwire.StateNeedsYou
		}
		a.view.UpdatedAt = b.now()
		view = a.view
		live.pumpDone = make(chan struct{})
	})
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	live.box.Event(appwire.EventTurnStarted, appwire.TurnStarted{Agent: id, Turn: turn})
	userEntry := appwire.Entry{
		ID: appUserEntryID(n), Agent: id, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: entryText, ClientID: clientID, Attachments: attachments, Reads: reads,
	}
	userEntry.Queued = shown != nil && shown.queued && appKeepsQueued(live.conn.Conv)
	appComposedFields(&userEntry, composedMark)
	live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: userEntry})
	// First messages, ordinary sends and retries share this acceptance point.
	// Staged or refused attachments never become media sources.
	if shown != nil && len(attachments) > 0 {
		var noted bool
		b.locked(func() { noted = shown.mediaNoted })
		if !noted {
			b.recordAttachedMedia(a, attachments)
			b.locked(func() { shown.mediaNoted = true })
		}
	}
	long := b.watchLong(id, live, turn, b.now())
	b.newTurnCalls(a)
	tails := b.tickTools(a, id, live)
	guard.Go("app/turn", func() {
		defer long.Stop()
		defer tails()
		b.pump(a, id, live, turn, n, text, shown, events)
		b.sendQueued(a, live)
	})
	return turn, nil
}

// pump maps one turn's session events onto the wire. It never waits on the
// writer: everything goes into the agent's outbox. n is the turn's place among
// the person's messages, which every entry id of the turn is counted from.
// shown, when set, is what the person's entry showed, which a failed turn's
// retry shows again.
func (b *appBackend) pump(a *appAgent, id string, live *appLive, turn uint64, n int, text string, shown *appSending, events <-chan session.Event) {
	defer close(live.pumpDone)
	ok, failure := true, ""
	kind, status := "", 0
	entry := func(e appwire.Entry) {
		e.Agent, e.Turn = id, turn
		if e.At.IsZero() {
			e.At = b.now()
		}
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: e})
	}
	// EVERY ASSISTANT MESSAGE OF THE TURN HAS ITS INDEX, msg, and the app keys
	// an answer by it: the deltas of message 0 append to answer 0, and answer
	// 0's entry replaces them, so the words before a tool call and the words
	// after it are two answers and neither is ever drawn twice. A message ends
	// where a tool call begins after its words, where the engine closes a reply
	// made in words alone ([session.EventAssistantDone]), or where the turn
	// ends, and it is counted only if it said something — the same rule the
	// listing counts by ([appEntriesOf]), so both name it alike.
	msg, notices, summaries := 0, 0, 0
	// A turn that answers under a message other turns already answered (a
	// woken one) starts its counts where they left off.
	b.locked(func() {
		msg, notices = live.msgBase, live.noteBase
		live.msgBase, live.noteBase = 0, 0
	})
	defer func() {
		b.locked(func() { live.lastN, live.lastMsgs, live.lastNotices = n, msg, notices })
	}()
	var reply strings.Builder
	// replyRun is the run that streamed the reply so far, or empty once two
	// runs have streamed into it.
	var replyRun string
	// withheld says the reply's words so far could still be the withdrawal token
	// ([session.NoChangeReply]), so they are kept back from the page until
	// they cannot be: a reply that is only the token is never drawn.
	withheld := true
	// sayReply ends the message being streamed and says it as kind, unless it
	// said nothing or only withdrew itself.
	//
	// A MESSAGE IS AN UPDATE ONLY WHEN ITS OWN TOOL CALL COMES NEXT, which is
	// how the listing names it ([appUpdates]): the reply open when a tool
	// begins is an update, the agent saying what it found mid-turn, and every
	// other reply is an answer — the one the engine closed in words alone
	// ([session.EventAssistantDone]) at once, and the one the turn ends on at
	// its end. A TURN CAN SAY SEVERAL: a stopped turn is sent back to work (the
	// completion check's carry-on, a nudge, team news landing at the boundary)
	// and answers again with no tool call between. Each is its own message, as
	// it is in the record — run together, a withdrawal ("[no change]") was
	// drawn glued to the answer it withdrew from, and every message after it
	// took the wrong id.
	sayReply := func(kind string) {
		words, run := strings.TrimSpace(reply.String()), replyRun
		defer func() {
			reply.Reset()
			withheld = true
			replyRun = ""
		}()
		if words == "" || session.IsNoChangeReply(words) {
			return
		}
		if withheld {
			live.box.Delta(id, turn, msg, appwire.DeltaText, reply.String())
		}
		b.locked(func() { live.lastAnswer = words })
		at := msg
		msg++
		entry(appwire.Entry{ID: appAnswerEntryID(n, at), Kind: kind, Text: words, Msg: &at, RunID: run})
	}
	// A THOUGHT IS SHOWN WHILE IT HAPPENS AND NEVER SAVED: its words go out
	// as turn.thinking, and its length once the agent moves on.
	var thoughtAt time.Time
	endThought := func() {
		if !thoughtAt.IsZero() {
			live.box.Event(appwire.EventTurnThinkingDone, appwire.TurnThinkingDone{Agent: id, Turn: turn, Seconds: appwire.ThinkingSeconds(b.now().Sub(thoughtAt))})
			thoughtAt = time.Time{}
		}
	}
	titles := map[string]string{}
	for event := range events {
		switch event.Kind {
		case session.EventModelIdentity:
			if event.ModelIdentity != nil {
				var view appwire.Agent
				changed := false
				b.locked(func() {
					previous := a.view.ModelIdentity
					if previous != nil && previous.Epoch == event.ModelIdentity.Epoch && previous.Revision > event.ModelIdentity.Revision {
						return
					}
					changed = true
					a.view.ModelIdentity = event.ModelIdentity
					a.view.Model = event.ModelIdentity.Selected
					view = a.view
				})
				if changed {
					live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
				}
			}
		case session.EventCloseContinued:
			b.closeWakeStarted(id, live, event.Text)
		case session.EventTextDelta:
			if reply.Len() == 0 {
				replyRun = event.RunID
			} else if replyRun != event.RunID {
				replyRun = ""
			}
			endThought()
			reply.WriteString(event.Text)
			switch {
			case !withheld:
				live.box.Delta(id, turn, msg, appwire.DeltaText, event.Text)
			case !strings.HasPrefix(session.NoChangeReply, strings.TrimLeft(reply.String(), " \t\r\n")):
				withheld = false
				live.box.Delta(id, turn, msg, appwire.DeltaText, reply.String())
			}
		case session.EventAssistantDone:
			// THE REPLY IS IN THE RECORD AND COMPLETE, made in words alone, so
			// it is an answer, and whatever the turn says next is another
			// message.
			sayReply(appwire.EntryAnswer)
		case session.EventReasoning:
			if thoughtAt.IsZero() {
				thoughtAt = b.now()
			}
			live.box.Delta(id, turn, msg, appwire.DeltaThinking, event.Text)
		case session.EventRetrying:
			// The attempt is void: what it streamed is dropped, here and on
			// the page, and a move to another model is said once.
			reply.Reset()
			withheld = true
			thoughtAt = time.Time{}
			// A wait already said with its countdown is not said twice; the
			// attempt is the turn's own count of them either way.
			announce := false
			retry := appwire.TurnRetrying{Agent: id, Turn: turn}
			b.locked(func() {
				announce = !live.retrySaid
				live.retrySaid = false
				if announce {
					live.retries++
				}
				retry.Attempt = max(1, live.retries)
			})
			if news := event.Retry; news != nil {
				if strings.TrimSpace(news.Next) != "" {
					footer := appFooter("f"+strconv.Itoa(n)+".m"+strconv.Itoa(retry.Attempt), id, turn, appwire.FooterFallback, redact.Secrets(strings.TrimSpace(event.Text)), b.now())
					footer.ModelFrom, footer.ModelTo = news.Model, news.Next
					live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: footer})
				}
			}
			if announce {
				live.box.Event(appwire.EventTurnRetrying, retry)
			}
		case session.EventCompacted, session.EventSummaryStarted, session.EventSummaryFailed:
			// A PASS IS ITS FACTS (appcompaction.go): its entry is named by its
			// sequence, so the summarizing entry is replaced by how it ended.
			// An engine too old to say them gets the line it always got, said
			// only for a pass that summarized.
			if pass, ok := appPassEvent(id, event); ok {
				pass.Turn = turn
				live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: pass})
			} else if event.Kind == session.EventCompacted && event.Summarized > 0 {
				entry(appwire.Entry{ID: "c" + strconv.Itoa(n) + "." + strconv.Itoa(summaries), Kind: appwire.EntrySummary, Text: appCompactionWords})
				summaries++
			}
			b.noteContext(a, live)
		case session.EventToolBegin:
			b.locked(func() { live.steps++ })
			endThought()
			sayReply(appwire.EntryUpdate)
			title := redact.Secrets(tui3.ToolTitle(event.Tool, event.Args, event.Hint))
			titles[event.CallID] = title
			b.locked(func() { a.step = title })
			row := b.callBegan(a, id, turn, event, title)
			row.Summary = event.Hint
			live.box.Event(appwire.EventTurnTool, row)
		case session.EventToolOutput:
			b.callOutput(a, id, live, event)
		case session.EventToolFinished:
			b.callFinished(a, event)
		case session.EventToolEnd, session.EventToolFailed:
			succeeded := event.Kind == session.EventToolEnd
			summary := event.Hint
			if summary == "" && !succeeded {
				summary = event.Text
				if summary == "" && event.Err != nil {
					summary = event.Err.Error()
				}
			}
			summary = redact.Secrets(summary)
			title := titles[event.CallID]
			if title == "" {
				title = redact.Secrets(tui3.ToolTitle(event.Tool, event.Args, ""))
			}
			live.box.Event(appwire.EventTurnTool, b.callEnded(a, id, turn, event, title, summary, appDetail(summary), succeeded))
			b.noteContext(a, live)
			// THE SEVENTEENTH PROCESS is refused by the engine, and the turn
			// says so once at its end.
			if !succeeded && strings.Contains(event.Output+summary, "processes are running for this agent.") {
				entry(appFooter("pl"+event.CallID, id, turn, appwire.FooterProcessLimit, strings.TrimSpace(summary), b.now()))
			}
		case session.EventConsentAllowed:
			receipt := appwire.Entry{ID: "ra" + event.CallID, Agent: id, Turn: turn, At: b.now(), Kind: appwire.EntryReceipt,
				Text: "Allowed automatically · " + tui3.ToolTitle(event.Tool, event.Args, "") + " · you always allow " + event.Rule,
				Why:  "Matches your rule " + event.Rule + " in Settings › Permissions"}
			_ = b.keepAppEntry(a, receipt, event.CallID)
			live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: receipt})
		case session.EventConsentRequest:
			call := appConsentCall{Tool: event.Tool, Args: event.Args}
			b.locked(func() { live.consents[event.ID] = call })
		case session.EventNotice:
			if event.ModelSwitch != nil {
				entry(appModelSwitchEntry(id, *event.ModelSwitch))
				break
			}
			// THE ENGINE'S NOTICES ARE ITS DIM LINES — the skills a turn
			// carried, a request reshaped to land — drawn by the terminal as
			// one muted row and never as a message, and quiet says the same.
			// A LINE SAYING THE TURN WAS SENT BACK TO WORK IS NOT ONE OF THEM
			// HERE: the terminal folds it away with the turn's work once the
			// turn settles, the app has no fold for a notice, and the turn
			// going on is what the worked line already says
			// ([session.IsCarryOnNotice]).
			if session.IsCarryOnNotice(event.Text) {
				break
			}
			if words := strings.TrimSpace(event.Text); words != "" {
				notice := appwire.Entry{ID: appLiveNoticeID(n, notices), Kind: appwire.EntryNotice, Text: redact.Secrets(words), Quiet: true}
				// ONLY WHAT THE PRODUCER TYPED travels as a fact; a notice
				// with no kind stays ordinary and its words are never read.
				switch event.NoticeKind {
				case session.NoticeKindSkills, session.NoticeKindMemory:
					notice.NoticeKind = event.NoticeKind
				}
				notice.Skills = append([]string(nil), event.Skills...)
				notice.Unrecorded = event.Unrecorded
				entry(notice)
				notices++
			}
		case session.EventError:
			ok = false
			kind, status, failure = appTurnFailure(event)
		case session.EventTurnDone:
			// A turn the person steered ends once per stretch; the last ending
			// on the stream is the turn's.
			ok, failure, kind, status = true, "", "", 0
			b.noteContext(a, live)
		case session.EventTitleChanged:
			// A standing lane is the sole name source when supported; duplicated
			// turn events must not race a newer detached or manual name.
			if live.titlesDone == nil {
				b.titleChanged(a, live, event.Text)
			}
		case session.EventTaskUpdate, session.EventTaskProposal, session.EventTaskPhase, session.EventJobUpdate, session.EventRenderUpdate, session.EventMediaUpdate, session.EventArtifactUpdate:
			// A task a running task call made is that call's task.
			if task := event.Task; task != nil && (event.Kind == session.EventTaskProposal || task.State == "" || task.State == session.TaskQueued) {
				b.callTask(a, appTaskID(id, task.ID))
			}
			b.workEvent(id, live, event)
		}
	}
	endThought()
	sayReply(appwire.EntryAnswer)
	byPerson := false
	b.locked(func() {
		byPerson = live.stopping
		// A STOPPED TURN ENDS CLEANLY ON THE ENGINE'S SIDE — an interrupt is
		// not an error there — so the person's stop is what says it stopped.
		if live.stopping {
			ok, kind, status = false, appwire.ErrorStopped, 0
		}
		a.failed, a.failedKind, a.failedShown, a.failedClient = "", "", nil, ""
		a.view.Retryable = false
		if !ok {
			a.failed, a.failedKind, a.failedShown = text, kind, shown
			a.view.Retryable = text != ""
		}
	})
	switch {
	case !ok && kind == appwire.ErrorStopped:
		// A STOP IS NOT AN ANSWER AND NOT AN ERROR. It is said once, as a
		// receipt, and the turn's end says stopped; the words the turn had
		// streamed are the answer they already were.
		failure = "stopped"
		at := b.now()
		words := "Stopped · " + at.Local().Format("15:04")
		if byPerson {
			words = "Stopped by you · " + at.Local().Format("15:04")
		}
		entry(appFooter(appStopEntryID(n), id, turn, appwire.FooterStoppedByYou, words, at))
	case !ok && kind == appwire.ErrorProvider:
		footer := appFooter(appErrorEntryID(n), id, turn, appwire.FooterProviderFailed, failure, b.now())
		footer.ErrorKind, footer.Status = kind, status
		entry(footer)
	case !ok:
		entry(appwire.Entry{ID: appErrorEntryID(n), Kind: appwire.EntryError, Text: failure, ErrorKind: kind, Status: status})
	}
	if ok {
		b.resetCloseAfterReply(id)
	}
	live.box.Event(appwire.EventTurnDone, appwire.TurnDone{Agent: id, Turn: turn, OK: ok, Error: failure, ErrorKind: kind, Status: status})
	held := false
	var view appwire.Agent
	b.locked(func() {
		live.running = false
		live.lastUse = b.now()
		a.step = ""
		// Let go or closed while the turn ran: that road has said the state.
		if held = a.live == live; held {
			a.view.State = live.restingState()
			a.view.UpdatedAt = b.now()
			view = a.view
		}
	})
	if held {
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	b.spendMoved(func(spend appwire.Spend) { b.moneyAfter(a, live, spend.TodayUSD) })
}

// ── entry ids ───────────────────────────────────────────────────────────────
//
// AN ENTRY'S ID IS THE SAME LIVE AND READ BACK. Both are counted from the
// conversation's own record: n is the place of the person's message among the
// person's messages (u0, u1 …), an answer is the m-th assistant message after
// it that said something (a<n>.<m>), and a tool entry is its call's own id
// (t<call>). The live pump counts as it streams and the listing counts as it
// reads, by one rule, so agents.entries names what entry.added named. What
// only ever exists live — a turn's error, an ask's receipt, the engine's dim
// notices — has an id of its own kind that the listing never makes.

func appUserEntryID(n int) string { return "u" + strconv.Itoa(n) }

func appAnswerEntryID(n, m int) string { return "a" + strconv.Itoa(n) + "." + strconv.Itoa(m) }

func appToolEntryID(call string) string { return "t" + call }

func appNoteEntryID(n, k int) string { return "n" + strconv.Itoa(n) + "." + strconv.Itoa(k) }

func appLiveNoticeID(n, k int) string { return "q" + strconv.Itoa(n) + "." + strconv.Itoa(k) }

func appErrorEntryID(n int) string { return "x" + strconv.Itoa(n) }

func appStopEntryID(n int) string { return "s" + strconv.Itoa(n) }

func appReceiptEntryID(askID string) string { return "r" + askID }

// appUserCount is how many of the person's messages a transcript holds.
func appUserCount(transcript string) int {
	users := 0
	for _, item := range appRecordItems(session.ReadTranscript(transcript)) {
		if item.Role == "user" {
			users++
		}
	}
	return users
}

// appSummaryRole marks where a compaction summarized what came before.
const appSummaryRole = "summary"

// appRecordItems is a record told once and whole: every line above every
// compaction, the live transcript beneath the last one's floor, and a mark
// for each pass at its own place, carrying the pass ([appRecordPass]).
func appRecordItems(record session.Record) []session.DisplayEntry {
	items, _ := appRecordPlaced(record)
	return items
}

// appRecordPlaced is [appRecordItems] with each item's place in the record's
// own count, `Earlier` followed by `Entries[Floor:]` (where a pass and a step
// mark are placed), and -1 for a pass's mark, which is no line of the record.
func appRecordPlaced(record session.Record) ([]session.DisplayEntry, []int) {
	floor := min(max(record.Floor, 0), len(record.Entries))
	if len(record.Earlier) == 0 && len(record.Passes) == 0 {
		places := make([]int, len(record.Entries))
		for i := range places {
			places[i] = max(i-floor, -1)
		}
		return record.Entries, places
	}
	if len(record.Passes) == 0 {
		// A region with no pass facts — a record told by an engine from
		// before they existed — is marked once, where the region ends.
		items := append([]session.DisplayEntry(nil), record.Earlier...)
		items = append(items, session.DisplayEntry{Role: appSummaryRole})
		items = append(items, record.Entries[floor:]...)
		places := make([]int, len(items))
		for i := range places {
			switch {
			case i < len(record.Earlier):
				places[i] = i
			case i == len(record.Earlier):
				places[i] = -1
			default:
				places[i] = i - 1
			}
		}
		return items, places
	}
	told := len(record.Earlier) + len(record.Entries) - floor
	items := make([]session.DisplayEntry, 0, told+len(record.Passes))
	places := make([]int, 0, told+len(record.Passes))
	// In place order; a pass's sequence breaks a tie, so two passes at one
	// place stand in the order they were made.
	passes := slices.Clone(record.Passes)
	slices.SortStableFunc(passes, func(x, y session.CompactionFact) int { return cmp.Compare(x.Entry, y.Entry) })
	mark := func(at int) {
		for len(passes) > 0 && passes[0].Entry <= at {
			pass := passes[0]
			passes = passes[1:]
			if pass.Entry < 0 {
				continue
			}
			items = append(items, session.DisplayEntry{Role: appSummaryRole, Pass: &pass})
			places = append(places, -1)
		}
	}
	position := 0
	for _, part := range [][]session.DisplayEntry{record.Earlier, record.Entries[floor:]} {
		for _, item := range part {
			mark(position)
			items = append(items, item)
			places = append(places, position)
			position++
		}
	}
	mark(told)
	return items, places
}

// appDetailLimit bounds a tool's short result line.
const appDetailLimit = 160

// appDetail is a tool's short result line: its first line, bounded.
func appDetail(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > appDetailLimit {
		line = string(runes[:appDetailLimit-1]) + "…"
	}
	return line
}

// appTurnFailure sorts a turn's error: its kind, the provider's status where
// there is one, and the words a person reads, which for a provider refusal are
// the provider's own.
func appTurnFailure(event session.Event) (kind string, status int, words string) {
	kind, status, words = event.ErrKind, event.ErrStatus, event.ErrWords
	if kind == "" && event.Err != nil {
		kind, status, words = session.ClassifyTurnError(event.Err)
	}
	switch kind {
	case session.TurnErrorProvider, session.TurnErrorBudget, session.TurnErrorStopped:
	default:
		kind = appwire.ErrorOther
	}
	if kind != session.TurnErrorProvider || strings.TrimSpace(words) == "" {
		words = "the turn ended with an error"
		if event.Err != nil {
			words = event.Err.Error()
		}
	}
	return kind, status, redact.Secrets(strings.TrimSpace(words))
}

// appTaskState is a task's state in the protocol's five words, and "" for a
// state none of them says. That includes the engine's interrupted — nothing is
// driving it and every step is kept — which the chat draws as `interrupted`,
// apart from failed; until the protocol has a word for it such a task is left
// out rather than given one it did not say.
func appTaskState(state string) string {
	switch session.TaskState(strings.TrimSpace(state)) {
	case session.TaskQueued:
		return appwire.TaskQueued
	case session.TaskRunning:
		return appwire.TaskRunning
	case session.TaskDone:
		return appwire.TaskDone
	case session.TaskFailed:
		return appwire.TaskFailed
	case session.TaskUnverified:
		return appwire.TaskUnverified
	}
	return ""
}

// workEvent is one task, phase or process event, from the turn's stream or
// the task lane: both carry the same news and each change is said once.
func (b *appBackend) workEvent(id string, live *appLive, event session.Event) {
	switch event.Kind {
	case session.EventTaskUpdate, session.EventTaskProposal:
		if event.Task != nil {
			b.taskNotice(id, live, event.Task, event.Kind == session.EventTaskProposal)
		}
	case session.EventTaskPhase:
		if event.TaskPhase != nil {
			b.taskPhase(id, live, event.TaskPhase)
		}
	case session.EventJobUpdate:
		if event.Job != nil {
			b.processMoved(id, live, event.Job)
		}
	case session.EventRenderUpdate:
		if event.Render != nil {
			b.renderChanged(id, live, event.Render)
		}
	case session.EventMediaUpdate:
		if event.Media != nil {
			b.mediaChanged(id, live, *event.Media)
		}
	case session.EventArtifactUpdate:
		if event.Artifact != nil {
			b.artifactCreated(id, live, *event.Artifact)
		}
	}
}

// Stop interrupts the running turn. An agent with nothing held has nothing
// running in this process, which is answered as done.
func (b *appBackend) Stop(id string) error {
	a, err := b.findCrewOwner(id)
	if err != nil {
		return err
	}
	if live := b.liveOf(a); live != nil {
		b.locked(func() { live.stopping = live.running })
		live.conn.Conv.Interrupt()
	}
	return nil
}

// CloseAgent lets the conversation go and says the agent is closed. Its journal
// stays on disk; agents.open or agents.send opens it again.
func (b *appBackend) CloseAgent(id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	// CLOSING AN AGENT STOPS WHAT IT KEPT RUNNING, and waits for it to be
	// gone; letting it go idle does not. A process that would not stop leaves
	// the agent open, its process listed and the error saying which.
	var agentID string
	b.locked(func() { agentID = a.view.ID })
	// Nothing new starts for it from here until its conversation is let go.
	defer b.fenceAgentServices(agentID)()
	if err := b.stopAgentServices(agentID); err != nil {
		return err
	}
	view := b.closeAgent(a)
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return nil
}

// closeAgent lets a's conversation go and marks it closed.
func (b *appBackend) closeAgent(a *appAgent) (view appwire.Agent) {
	a.gate.Lock()
	defer a.gate.Unlock()
	b.release(a)
	b.locked(func() {
		a.view.State = appwire.StateClosed
		view = a.view
	})
	return view
}

// release gives a's conversation back and flushes its last events. Callers
// hold a.gate.
func (b *appBackend) release(a *appAgent) {
	var live *appLive
	b.locked(func() { live, a.live = a.live, nil })
	if live == nil {
		return
	}
	b.finish(live)
}

// finish gives a conversation that is no longer held back, and shuts its
// outbox once the turn and the lanes have said their last.
func (b *appBackend) finish(live *appLive) {
	b.locked(func() { live.closed = true })
	b.landPilots(live)
	if live.stopTitles != nil {
		live.stopTitles()
	}
	if live.stopTasks != nil {
		live.stopTasks()
	}
	if live.stopAsks != nil {
		live.stopAsks()
	}
	if live.stopWakes != nil {
		live.stopWakes()
	}
	if live.stopPasses != nil {
		live.stopPasses()
	}
	live.conn.Release()
	wait := time.After(appReleaseWait)
	if live.pumpDone != nil {
		select {
		case <-live.pumpDone:
		case <-wait:
		}
	}
	for _, done := range []chan struct{}{live.lanesDone, live.asksDone, live.titlesDone} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-wait:
		}
	}
	live.box.Close()
}

// ListAsks answers the agent's open asks. An agent with no conversation held
// has none this process can answer.
func (b *appBackend) ListAsks(id string) ([]appwire.Ask, error) {
	// A member at rest has no question a held conversation would add: one
	// waiting on the person says so in its presence and is not at rest.
	a, err := b.findCrewReader(id)
	if err != nil {
		return nil, err
	}
	var asks []appwire.Ask
	b.locked(func() {
		if a.live != nil {
			asks = append(asks, a.live.asks...)
		}
	})
	asks = append(asks, b.crewAsksOf(id)...)
	return append(asks, b.orderAsksOf(id)...), nil
}

// appAnsweredMark starts the value [appLive.answered] holds for an ask this app
// answered, so an answer of words alone, with no option, is still marked.
const appAnsweredMark = "app:"

// AnswerAsk answers one open ask through the engine's own answer door, the one
// the chat's question page uses. Its closing arrives on the question lane.
func (b *appBackend) AnswerAsk(id, askID, option, text string) error {
	return b.answerAskScope(id, askID, option, text, "")
}

// answerAskScope validates the chosen rule before writing or resolving anything.
func (b *appBackend) answerAskScope(id, askID, option, text, scope string) error {
	b.resetCloseResumes(id)
	if handled, err := b.answerOrderAsk(askID, option); handled {
		return err
	}
	if handled, err := b.answerCrewAsk(id, askID, option, text, ""); handled {
		return err
	}
	a, err := b.findCrewOwner(id)
	if err != nil {
		return err
	}
	if handled, err := b.answerMoney(a, askID, option, text); handled {
		return err
	}
	var live *appLive
	var q session.Question
	var key string
	var refused error
	b.locked(func() {
		live = a.live
		open := false
		if live != nil {
			q, open = live.questions[askID]
		}
		if !open {
			refused = appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open; it may have been answered elsewhere", askID)
			return
		}
		if option != "" {
			picked, offered := appOptionNamed(q, option)
			if !offered {
				refused = appwire.Errorf(appwire.CodeBadParams, "the ask %s offers no option %q", askID, option)
				return
			}
			key = picked.key
		}
		if q.Kind == session.QuestionConsent && option == appwire.OptionAlways {
			if refused = appValidateScope(option, scope, q.ConsentScopes); refused != nil {
				return
			}
		}
		live.answered[askID] = appAnsweredMark + option
		live.lastUse = b.now()
	})
	if refused != nil {
		return refused
	}
	if q.Kind == session.QuestionConsent && option == appwire.OptionAlways {
		if !b.rememberAlways(b.consentCall(live, q.ID), scope) {
			b.locked(func() { delete(live.answered, askID) })
			return appwire.Errorf(appwire.CodeEngine, "the permission rule could not be saved")
		}
		b.locked(func() {
			if live.savedScopes == nil {
				live.savedScopes = map[string]string{}
			}
			live.savedScopes[askID] = scope
		})
		live.conn.Conv.ResolveConsentRemember(q.ID, true, session.ConsentRule)
		return nil
	}
	answer := session.Answer{Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask, Key: key, Change: strings.TrimSpace(text)}
	if key != "" {
		answer.Picked = []string{key}
	}
	if err := live.conn.Conv.ResolveQuestion(answer); err != nil {
		b.locked(func() { delete(live.answered, askID) })
		return appEngineError(err)
	}
	return nil
}

// appValidateScope keeps both answer doors on the permission's offered rules.
func appValidateScope(option, scope string, scopes []string) error {
	if option == appwire.OptionAlways && (scope == "" || !slices.Contains(scopes, scope)) {
		return appwire.Errorf(appwire.CodeBadScope, "choose one of this ask's scopes")
	}
	return nil
}

// appConsentWait bounds how long an always waits for the turn to have said
// which call it is about. The question and the call travel on two lanes and the
// question can arrive first; the wait is only ever paid on an always.
const appConsentWait = 2 * time.Second

// consentCall is the call approval id is about, as the turn announced it.
func (b *appBackend) consentCall(live *appLive, id uint64) appConsentCall {
	deadline := time.Now().Add(appConsentWait)
	for {
		var call appConsentCall
		seen := false
		b.locked(func() { call, seen = live.consents[id] })
		if seen || time.Now().After(deadline) {
			return call
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// rememberAlways uses the terminal's persistence path with the chosen scope.
func (b *appBackend) rememberAlways(call appConsentCall, scope string) bool {
	_, saved := config.SaveApproval(call.Tool, scope,
		func(shape string) error { return config.RememberBashApproval(config.ProfileDir(), shape) },
		func(tool string) error { return config.RememberToolApproval(config.ProfileDir(), tool, "allow") })
	return saved
}

// appRecords keeps the readings of journals that have not changed, so paging
// back through a long conversation parses its journal once rather than once a
// page, and bounds how many readings run at once. Its limits are the session
// package's own.
var appRecords = session.NewRecordCache(session.RecordCacheLimits{})

// Entries reads the agent's transcript off the disk, whether or not a
// conversation is held for it: the journal is written as the turn goes.
func (b *appBackend) Entries(id, before string, limit int) ([]appwire.Entry, bool, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, false, err
	}
	var transcript string
	var at time.Time
	marks := map[int]appTurnMark{}
	b.locked(func() {
		transcript, at = a.transcript, a.view.UpdatedAt
		for n, mark := range a.marks {
			marks[n] = mark
		}
	})
	if transcript == "" {
		return []appwire.Entry{}, false, nil
	}
	var page []appwire.Entry
	var more bool
	err = appRecords.WithRecord(transcript, func(record session.Record) error {
		footers, err := b.readAppFooters(transcript)
		if err != nil {
			return err
		}
		notes := func() (*appNoticeReader, error) { return b.noticeLog().Reader(id) }
		rows, _ := session.ReadProcessNotices(transcript)
		jobs := make(map[int]session.JobNotice, len(rows))
		for _, row := range rows {
			jobs[row.ID] = row
		}
		appFillJobHistory(jobs, appRecordItems(record))
		record.Earlier = appLinkJobHistory(record.Earlier, jobs)
		record.Entries = appLinkJobHistory(record.Entries, jobs)
		page, more, err = appEntryPageOf(id, record, at, marks, notes, before, limit, func(item session.DisplayEntry) appwire.ToolFacts {
			return b.appEntryFacts(a, item)
		}, b.crewFormedEntries(id), footers)
		return err
	})
	if err != nil {
		return nil, false, appEntriesBusy(err)
	}
	return page, more, nil
}

// appEntriesBusy says a reading refused at the gate as busy, so a client asks
// again rather than drawing an empty conversation; any other error is itself.
func appEntriesBusy(err error) error {
	if errors.Is(err, session.ErrRecordReadBusy) {
		return appwire.Errorf(appwire.CodeBusy, "too many conversations are being read at once; ask again in a moment")
	}
	return err
}

// appEntryPage is one page of a record's entries, newest last, and whether
// older entries sit before it.
//
// THE PAGE IS CUT BEFORE ANY TOOL ROW IS DRESSED OR ITS FACTS READ. A tool's
// title and lines are redacted and its facts can open its output file, so
// doing that for every call in a long conversation and then keeping two
// hundred was most of the cost of a page; the entries, their ids and the
// cursor are still told over the WHOLE record, because an id counts the
// person's messages from the first and an update is known only by a later call.
func appEntryPage(id string, record session.Record, at time.Time, marks map[int]appTurnMark, before string, limit int, facts func(session.DisplayEntry) appwire.ToolFacts) ([]appwire.Entry, bool, error) {
	return appEntryPageOf(id, record, at, marks, nil, before, limit, facts, nil)
}

// appEntryPageWithFormed is [appEntryPage] with a crew's formation facts laid
// in at their moments (appcrews.go).
func appEntryPageWithFormed(id string, record session.Record, at time.Time, marks map[int]appTurnMark, before string, limit int, facts func(session.DisplayEntry) appwire.ToolFacts, formed []appwire.Entry) ([]appwire.Entry, bool, error) {
	return appEntryPageOf(id, record, at, marks, nil, before, limit, facts, formed)
}

// appEntryPageWith is [appEntryPage] with the agent's kept notices of its
// kept-running processes (appnotices.go) laid in by time.
func appEntryPageWith(id string, record session.Record, at time.Time, marks map[int]appTurnMark, notes func() (*appNoticeReader, error), before string, limit int, facts func(session.DisplayEntry) appwire.ToolFacts) ([]appwire.Entry, bool, error) {
	return appEntryPageOf(id, record, at, marks, notes, before, limit, facts, nil)
}

// appEntryPageOf is one page with both: the crew's formation facts are laid
// into the entries first, so they are cursors like any other entry, and the
// kept notices of its kept-running processes are then laid in by time, each
// after the last entry at or before its moment. The record of notices is read
// backwards from the page's cursor, so a page holds at most limit of them
// however long the record is, and a notice's id is a cursor like any other
// entry's.
func appEntryPageOf(id string, record session.Record, at time.Time, marks map[int]appTurnMark, notes func() (*appNoticeReader, error), before string, limit int, facts func(session.DisplayEntry) appwire.ToolFacts, formed []appwire.Entry, kept ...[]appKeptFooter) ([]appwire.Entry, bool, error) {
	items, places := appRecordPlaced(record)
	all, from := appEntryShapes(id, items, at, marks)
	appMarkProgress(all, from, places, record.Progress)
	if len(kept) > 0 {
		all, from = appInsertFooters(all, from, kept[0], items)
	}
	if len(record.ModelSwitches) > 0 {
		formed = append([]appwire.Entry(nil), formed...)
	}
	for _, change := range record.ModelSwitches {
		formed = append(formed, appModelSwitchEntry(id, change))
	}
	if len(formed) > 0 {
		all = appWithFormed(all, formed, at, &from)
	}
	if words := strings.TrimSpace(record.Unreadable); words != "" {
		all = append(all, appwire.Entry{ID: "unreadable", Agent: id, Kind: appwire.EntryNotice, At: at, Text: words})
	}
	var reader *appNoticeReader
	if notes != nil {
		r, err := notes()
		if err != nil {
			return nil, false, err
		}
		reader = r
	}
	if reader != nil {
		defer reader.Close()
	}
	end := len(all)
	// bound is the moment notices on this page come before; a page cut at a
	// transcript entry holds only notices from before it.
	var bound *time.Time
	if before != "" {
		end = -1
		for i, entry := range all {
			if entry.ID == before {
				end = i
				break
			}
		}
		if end >= 0 {
			cut := all[end].At
			bound = &cut
		} else if reader != nil && strings.HasPrefix(before, "svc") {
			// A notice's id: the page is what came before that notice.
			for {
				n, ok := reader.Prev()
				if !ok {
					break
				}
				if n.id() == before {
					end = len(all)
					for end > 0 && all[end-1].At.After(n.At) {
						end--
					}
					break
				}
			}
		}
		if end < 0 {
			return nil, false, appwire.Errorf(appwire.CodeNotFound, "this conversation has no entry %s", before)
		}
	}
	if reader != nil && bound != nil {
		for {
			n, ok := reader.Peek()
			if !ok || n.At.Before(*bound) {
				break
			}
			reader.Prev()
		}
	}
	// Walked back from the cursor: a notice at or after the entry before it
	// comes first, and a notice id given once in a page is not given twice.
	var notices []appwire.Entry
	order := make([]bool, 0, limit) // true is a notice
	seen := map[string]bool{}
	start := end
	for len(order) < limit {
		n, haveN := appwire.Entry{}, false
		if reader != nil {
			if notice, ok := reader.Peek(); ok && (start == 0 || !notice.At.Before(all[start-1].At)) {
				reader.Prev()
				if seen[notice.id()] {
					continue
				}
				seen[notice.id()] = true
				n, haveN = appServiceNoticeEntry(notice), true
			}
		}
		if haveN {
			notices = append(notices, n)
			order = append(order, true)
			continue
		}
		if start == 0 {
			break
		}
		start--
		order = append(order, false)
	}
	more := start > 0
	if reader != nil && !more {
		_, more = reader.Peek()
	}
	transcript, err := appDressedPage(items, all, from, start, end, facts)
	if err != nil {
		return nil, false, err
	}
	page := make([]appwire.Entry, 0, len(order))
	ti, ni := 0, len(notices)-1
	for i := len(order) - 1; i >= 0; i-- {
		if order[i] {
			page = append(page, notices[ni])
			ni--
		} else {
			page = append(page, transcript[ti])
			ti++
		}
	}
	return page, more, nil
}

// appDressedPage is the transcript entries start to end, each tool row
// dressed and given its call's facts, which is only done for a page.
func appDressedPage(items []session.DisplayEntry, all []appwire.Entry, from []int, start, end int, facts func(session.DisplayEntry) appwire.ToolFacts) ([]appwire.Entry, error) {
	page := append([]appwire.Entry{}, all[start:end]...)
	for i := range page {
		if start+i < len(from) && from[start+i] >= 0 {
			appDressTool(&page[i], items[from[start+i]])
		}
	}
	// Existing facts keep their last same-id source, as older pages did. Only
	// the new process identity belongs to the exact result occurrence.
	wanted := map[string]bool{}
	for _, entry := range page {
		if entry.Kind == appwire.EntryTool && entry.Call != "" {
			wanted[entry.Call] = true
		}
	}
	if len(wanted) == 0 {
		return page, nil
	}
	calls := make(map[string]session.DisplayEntry, len(wanted))
	for _, item := range items {
		if item.Role == "tool" && wanted[item.CallID] {
			calls[item.CallID] = item
		}
	}
	for i := range page {
		if item, ok := calls[page[i].Call]; ok && page[i].Kind == appwire.EntryTool {
			item.ToolProcess = nil
			if start+i < len(from) && from[start+i] >= 0 {
				item.ToolProcess = items[from[start+i]].ToolProcess
			}
			page[i].ToolFacts = facts(item)
		}
	}
	return page, nil
}

// appEntriesOf is a transcript as the app's entries, named by the rule the
// live pump names them by (see the entry ids above). The journal keeps no
// clock per message, so every entry carries the conversation's own last moment.
//
// A message this process sent carries its turn number, and the person's entry
// its client id, from marks; one sent before this launch has neither.
func appEntriesOf(agent string, items []session.DisplayEntry, at time.Time, marks map[int]appTurnMark) []appwire.Entry {
	entries, from := appEntryShapes(agent, items, at, marks)
	for i := range entries {
		appDressTool(&entries[i], items[from[i]])
	}
	return entries
}

// appDressTool writes a tool entry's title, summary and detail line, which
// are the costly part of an entry: each is redacted. Any other entry is left
// as it is.
func appDressTool(entry *appwire.Entry, item session.DisplayEntry) {
	if entry.Kind != appwire.EntryTool {
		return
	}
	entry.Title = redact.Secrets(tui3.ToolTitle(item.Tool, item.Args, item.Hint))
	entry.Summary = redact.Secrets(item.Hint)
	entry.Detail = appDetail(redact.Secrets(item.Output))
}

// appEntryShapes is the entries with their ids, kinds and words, and for each
// the index of the item it was told from, with tool entries not yet dressed.
// Ids are counted over the whole record, so this is always told whole.
func appEntryShapes(agent string, items []session.DisplayEntry, at time.Time, marks map[int]appTurnMark) ([]appwire.Entry, []int) {
	entries := make([]appwire.Entry, 0, len(items))
	from := make([]int, 0, len(items))
	n, m, k, c := -1, 0, 0, 0
	update := appUpdates(items)
	for i, item := range items {
		// Each entry carries the instant its own line was written; the
		// conversation's latest moment stands in only where the record never
		// said.
		entry := appwire.Entry{Agent: agent, At: at, RunID: item.RunID}
		if !item.At.IsZero() {
			entry.At = item.At
		}
		switch item.Role {
		case appSummaryRole:
			if item.Pass != nil && item.Pass.Seq > 0 {
				entry = appCompactionEntry(agent, *item.Pass, appCompactionState(*item.Pass))
			} else {
				entry.ID, entry.Kind, entry.Text = "c"+strconv.Itoa(max(n, 0))+".s"+strconv.Itoa(c), appwire.EntrySummary, appCompactionWords
			}
			c++
		case "user":
			n, m, k = n+1, 0, 0
			// The words are the person's own and the attachments the ones that
			// rode on them, as the record keeps them on the message's line; a
			// line without that mark is its words, and has no attachments.
			entry.ID, entry.Kind, entry.Text, entry.Attachments = appUserEntryID(n), appwire.EntryUser, item.Text, appAttachmentsOf(item.Person)
			entry.Queued = item.Person != nil && item.Person.Queued
			entry.Reads = appPersonReads(item.Person)
			if item.Person != nil {
				appComposedFields(&entry, item.Person.Composed)
			}
		case "assistant", "aside":
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			// AN ANSWER CUT SHORT BY A STOP IS STILL THE ANSWER IT WAS. The
			// journal keeps it apart from the conversation, as a line the
			// session wrote, but it is the message the person watched stream
			// under this id; read back as a notice it was that paragraph said
			// a second time. Every other aside is the session's own dim line.
			// A LINE THE SESSION WROTE TO ITS MODEL IS NOT SHOWN: it is the
			// model's instruction — a wake, a landing note, the carry-on after a
			// quit — and never words to the person. A team's delivery is the
			// exception and is the crew's line.
			if item.Role == "aside" && item.ForModel && len(item.Team) == 0 {
				continue
			}
			// A REPLY THAT IS ONLY THE WITHDRAWAL TOKEN WITHDREW ITSELF: the
			// answer before it stands, and the token is never drawn.
			if item.Role == "assistant" && session.IsNoChangeReply(item.Text) {
				continue
			}
			if item.Role == "aside" && !item.Interrupted {
				entry.ID, entry.Kind, entry.Text, entry.Quiet = appNoteEntryID(n, k), appwire.EntryNotice, item.Text, true
				// A TEAM'S DELIVERY IS ITS LINES. The words around them are the
				// session's instruction to its model, and so is any other note
				// that rode in the same message; the crew's lines are what the
				// person reads.
				if len(item.Team) > 0 {
					entry.Text, entry.Team = appTeamDelivery(item.Team), appTeamLines(item.Team)
				}
				k++
				break
			}
			msg := m
			entry.ID, entry.Kind, entry.Text, entry.Msg = appAnswerEntryID(n, msg), appwire.EntryAnswer, strings.TrimSpace(item.Text), &msg
			if update[i] && item.Role == "assistant" {
				entry.Kind = appwire.EntryUpdate
			}
			m++
			// Older runners echoed the person's steering as a progress answer.
			// Hide only that producer-marked receipt, after reserving its id:
			// authored prose stays visible and later answers never renumber.
			if item.FactoryProgress && strings.HasPrefix(entry.Text, "steer: ") {
				continue
			}
		case "tool":
			// A TOOL'S RESULT MESSAGE IS NOT A SECOND CALL. The shaping keeps a
			// row for it with no call id and no name; the call it answers is
			// the entry above, with its output, so drawn alone it is a blank
			// step that no live event ever named. A call is told by its id or
			// its name, and one with either stays.
			if item.CallID == "" && item.Tool == "" {
				continue
			}
			call := item.CallID
			if call == "" {
				call = "#" + strconv.Itoa(i)
			}
			entry.ID, entry.Kind, entry.Call, entry.Name = appToolEntryID(call), appwire.EntryTool, item.CallID, item.Tool
		case "note":
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			entry.ID, entry.Kind, entry.Text, entry.Quiet = appNoteEntryID(n, k), appwire.EntryNotice, item.Text, true
			k++
		default:
			continue
		}
		if mark, ok := marks[n]; ok && n >= 0 {
			entry.Turn = mark.turn
			if entry.Kind == appwire.EntryUser {
				entry.ClientID = mark.clientID
			}
		}
		// THE RECORD'S ID WINS: it was written with the message and outlives the
		// process whose map the fallback above is. A line with none keeps the
		// process-local one, and a legacy line with neither has none — an id is
		// never matched up by turn, time or words.
		if entry.Kind == appwire.EntryUser && item.Person != nil && item.Person.ClientID != "" {
			entry.ClientID = item.Person.ClientID
		}
		entries = append(entries, entry)
		from = append(from, i)
	}
	return entries, from
}

// appUpdates marks each assistant message that was an UPDATE, as the live pump
// named it: a message whose own tool calls ran after it (the pump says the
// reply before a tool begins as an update, and the turn's last reply as its
// answer). The shaping writes a message's calls straight after it, so the
// mark is the very next item being a call. A later turn's calls never reach
// back: a turn a team delivery or a wake opened has no person's line in
// between, and its work once turned the previous turn's answer into an update.
func appUpdates(items []session.DisplayEntry) []bool {
	update := make([]bool, len(items))
	for i := 0; i+1 < len(items); i++ {
		next := items[i+1]
		update[i] = items[i].Role == "assistant" && next.Role == "tool" && (next.CallID != "" || next.Tool != "")
	}
	return update
}

// SetAutomaticModels changes fallback policy without changing the active request.
func (b *appBackend) SetAutomaticModels(id string, enabled bool) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.lentFence(a, "model selection"); err != nil {
		return err
	}
	if _, _, err := b.ensureLocked(a); err != nil {
		return err
	}
	var live *appLive
	b.locked(func() { live = a.live })
	door, ok := live.conn.Conv.(interface{ SetAutomaticModels(bool) error })
	if !ok {
		return fmt.Errorf("model selection policy is unsupported by this engine")
	}
	return door.SetAutomaticModels(enabled)
}

// SetModel moves the agent to another model, opening its conversation first if
// none is held: the choice is the conversation's own and is saved with it.
func (b *appBackend) SetModel(id, model string) (appwire.Agent, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "name a model")
	}
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if err := b.lentFence(a, "model"); err != nil {
		return appwire.Agent{}, err
	}
	if _, _, err := b.ensureLocked(a); err != nil {
		return appwire.Agent{}, err
	}
	var live *appLive
	b.locked(func() {
		live = a.live
		live.lastUse = b.now()
	})
	if live.conn.Lent {
		return appwire.Agent{}, errLentStage("model")
	}
	live.conn.Conv.SetModel(model)
	if now := live.conn.Conv.Model(); now != "" {
		model = now
	}
	var view appwire.Agent
	b.locked(func() {
		a.view.Model = model
		if door, ok := live.conn.Conv.(interface{ ModelIdentity() session.ModelIdentity }); ok {
			state := door.ModelIdentity()
			a.view.ModelIdentity = &state
		}
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view, nil
}

// SetEffort sets how hard the agent's model is asked to think, opening its
// conversation first if none is held. The terminal's /effort uses this same
// conversation dial, so auto clears the stored override on either surface.
func (b *appBackend) SetEffort(id, word string) (appwire.Agent, error) {
	rung, ok := effort.Parse(word)
	if !ok {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeBadParams, "effort is low, medium or high")
	}
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if _, _, err := b.ensureLocked(a); err != nil {
		return appwire.Agent{}, err
	}
	var live *appLive
	b.locked(func() {
		live = a.live
		live.lastUse = b.now()
	})
	dial, ok := live.conn.Conv.(interface {
		SetConversationEffort(string) bool
		ConversationEffort() string
	})
	if !ok || !dial.SetConversationEffort(rung.String()) {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeEngine, "this conversation has no thinking dial; update the engine and reconnect")
	}
	chosen := dial.ConversationEffort()
	if err := b.saveJobEffort(a.transcript, chosen); err != nil {
		return appwire.Agent{}, err
	}
	var view appwire.Agent
	b.locked(func() {
		a.view.Effort = chosen
		view = a.view
	})
	b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	return view, nil
}

// Warm opens the engine's process half — the profile, the model catalog, the
// harness and skill shelves every conversation is built on — behind the hello,
// so the first agents.create does not wait for it. It is what the terminal does
// as it starts, network included.
func (b *appBackend) Warm() {
	run := false
	b.locked(func() {
		if !b.closing && b.warmed == nil {
			b.warmed, run = make(chan struct{}), true
		}
	})
	if !run {
		return
	}
	defer close(b.warmed)
	defer guard.Recover("app/warm")
	_, _ = b.process()
	b.sweepOutputs()
}

// process is the engine's shared process half. Once open it is kept; an open
// that failed is tried again on the next call ([openAppEngineProcess]),
// because the window may ask for models before it has handed over the key the
// open needs.
func (b *appBackend) process() (*v3Process, error) {
	if err := b.settleDayLimit(); err != nil {
		return nil, err
	}
	proc, err := openAppEngineProcess()
	if err == nil {
		b.markAssembled()
	}
	return proc, err
}

// markAssembled records that the process half is open. The first time, the
// providers it has no model list for yet are fetched in the background
// ([appBackend.warmProviderModels]): a provider connected before the process
// opened — the keys a launch hands over, a setup finished on its first screen
// — would otherwise offer nothing in models.list until the next fetch.
func (b *appBackend) markAssembled() {
	if !b.assembled.Swap(true) {
		b.warmProviderModels()
	}
}

// appModelsWait bounds how long models.list waits for a model list still
// arriving when nothing is remembered on disk yet.
const appModelsWait = 3 * time.Second

// ListModels answers exactly what the terminal's model picker offers for the
// configured services — the same process catalog, the same per-service shelf,
// the same caches, through the picker's own composer ([tui3.PickerModels]) —
// in the picker's groups and order, with the model a new conversation runs on
// marked default, and added when no list names it, so the answer is never
// empty while a turn can run.
//
// A MODEL'S PROVIDER IS THE SERVICE THE PICKER GROUPS IT UNDER: the name it was
// connected as ("local" for a custom endpoint written that way, "codex" for
// the ChatGPT sign-in, "openrouter" for the default), and provider_name the
// service's own name. Only services with a key are listed, as in the picker.
//
// WITH NO PROVIDER CONNECTED THE ANSWER IS AN EMPTY LIST, NOT A FAILURE. Nothing
// has failed: there is no model a turn could run on, which is the ordinary
// state of an app whose setup was skipped. It used to be the process half's
// refusal to open, spelled for a terminal ("export OPENROUTER_API_KEY and run
// it again"); and once a process half that had opened lost its last provider,
// it was the default model alone, offered although nothing could run it.
// Connected is the rule providers.list answers signed_in by
// ([appBackend.providerConnected]), so the two never disagree.
func (b *appBackend) ListModels() ([]appwire.Model, error) {
	if !b.providerConnected() {
		return []appwire.Model{}, nil
	}
	proc, err := b.process()
	if err != nil {
		return nil, appEngineError(err)
	}
	// THE DEFAULT SERVICE'S CATALOG IS WAITED FOR EVEN WHEN ANOTHER SERVICE HAS
	// ANSWERED: a list read before it lands is every other service's models
	// and none of the default's, which reads as the default not being there.
	// AND A PROVIDER JUST CONNECTED IS WAITED FOR TOO, within the same few
	// seconds: its list is being fetched behind the connection, and a list read
	// before it lands offers none of the models the person just connected.
	ctx, cancel := context.WithTimeout(context.Background(), appModelsWait)
	if proc.Models != nil {
		proc.Models.Warmed(ctx)
	}
	var warming chan struct{}
	b.locked(func() { warming = b.providerWarm })
	if warming != nil {
		select {
		case <-warming:
		case <-ctx.Done():
		}
	}
	cancel()
	_, sources := proc.currentAccount()
	rows := tui3.PickerModels(proc.ProfileDir, sources, proc.Shelf.pickerModels, proc.Shelf.modelsForService)
	names := map[string]string{}
	for _, service := range sources.All() {
		group := strings.ToLower(strings.TrimSpace(service.Source.Written))
		if group == "" {
			group = strings.ToLower(strings.TrimSpace(service.Source.Name))
		}
		name := strings.TrimSpace(service.Source.Name)
		if modelsource.IsCustomID(service.Source.ID) || name == "" {
			name = strings.TrimSpace(service.Source.Written)
		}
		names[group] = name
	}
	chosen := appDefaultModel()
	models := make([]appwire.Model, 0, len(rows)+1)
	seen := false
	for _, row := range rows {
		provider := strings.TrimSpace(row.Group)
		if provider == "" {
			provider = modelsource.DefaultID
		}
		name := row.ID
		if row.Direct {
			name = strings.TrimPrefix(row.ID, provider+"/")
		}
		model := appwire.Model{ID: row.ID, Name: name, Provider: provider, ProviderName: names[provider], Default: row.ID == chosen}
		if row.ListedAt > 0 {
			model.ListedAt = row.ListedAt
		}
		if model.ProviderName == "" && provider == modelsource.DefaultID {
			model.ProviderName = modelsource.DefaultSource("").Name
		}
		seen = seen || model.Default
		models = append(models, model)
	}
	// A PROVIDER THAT LISTS NO MODELS STILL OFFERS ITS OWN. The terminal's
	// picker draws such a provider as a line that says to type a model id,
	// which a menu cannot do; the app offers the model the terminal moves a
	// conversation onto when that provider connects
	// ([modelsource.Source.PreferredModel]), and offers nothing where there is
	// no such model rather than guess one.
	offered := map[string]bool{}
	for _, model := range models {
		offered[model.Provider] = true
	}
	for _, service := range sources.All() {
		group := strings.ToLower(strings.TrimSpace(service.Source.Written))
		if strings.EqualFold(service.Source.ID, modelsource.DefaultID) || group == "" || offered[group] || !service.HasCredentials() {
			continue
		}
		preferred := service.Source.PreferredModel(service.Door, nil)
		if preferred == "" {
			continue
		}
		id := service.Qualify(preferred)
		model := appwire.Model{ID: id, Name: preferred, Provider: group, ProviderName: names[group], Default: id == chosen}
		seen = seen || model.Default
		offered[group] = true
		models = append(models, model)
	}
	if !seen && chosen != "" {
		provider := modelsource.DefaultID
		if service, bare := sources.For(chosen); bare != chosen && service.Source.Written != "" {
			provider = strings.ToLower(service.Source.Written)
		}
		models = append(models, appwire.Model{ID: chosen, Name: chosen, Provider: provider, ProviderName: names[provider], Default: true})
	}
	return models, nil
}

// appDefaultModel is the model a new conversation runs on, by the rule every
// local door opens one with ([v3TalkModel]).
func appDefaultModel() string {
	settings, err := config.LoadKeyless()
	if err != nil {
		return ""
	}
	return v3TalkModel("", settings)
}

// appFigure is a money figure, or nil for none.
func appFigure(usd float64) *float64 {
	if usd <= 0 {
		return nil
	}
	return &usd
}

// SpendToday reads this machine's usage ledger for today.
func (b *appBackend) SpendToday() (appwire.Spend, error) {
	now := b.now()
	limit, err := b.dayCap(now)
	if err != nil {
		return appwire.Spend{}, appEngineError(err)
	}
	spent, err := b.spentTodayChecked(now)
	if err != nil {
		return appwire.Spend{}, appEngineError(err)
	}
	return appwire.Spend{TodayUSD: spent, DayCapUSD: appFigure(limit)}, nil
}

// appSpendSettle is how long after a turn ends its spending is read again: the
// ledger is written behind the turn, not in front of its ending.
const appSpendSettle = 300 * time.Millisecond

// spendMoved says spend.changed once a turn's spending has reached the ledger,
// if today's figure moved, and hands today's figure to after.
func (b *appBackend) spendMoved(after func(appwire.Spend)) {
	time.AfterFunc(appSpendSettle, func() {
		defer guard.Recover("app/spend")
		if b.isClosing() {
			return
		}
		spend, err := b.SpendToday()
		if err != nil {
			return
		}
		moved := false
		b.locked(func() {
			moved = spend.TodayUSD != b.spent
			b.spent = spend.TodayUSD
		})
		if moved {
			b.emit.Emit(appwire.EventSpendChanged, appwire.SpendChanged{Spend: spend})
		}
		if after != nil {
			after(spend)
		}
	})
}

// ListTasks reads the agent's tasks off the disk whether or not its
// conversation is held — its own checkpoint first, which holds every task it
// ever had, finished ones included, then its project's task index for any the
// checkpoint does not name — and lays over them what a held conversation has
// said since.
func (b *appBackend) ListTasks(id string) ([]appwire.Task, error) {
	if a, err := b.find(id); err == nil && b.crewKeeps(id) && b.memberRests(a) {
		return b.restingTasks(a, id)
	}
	return b.listTasksOf(id)
}

// listTasksOf is ListTasks for a conversation the crew keeps held when it is
// busy, and the files' answer otherwise.
func (b *appBackend) listTasksOf(id string) ([]appwire.Task, error) {
	a, err := b.findCrewOwner(id)
	if err != nil {
		return nil, err
	}
	var transcript string
	var fallback time.Time
	// A quick task is listed by AppQuickTasks, never as a task here (#1880).
	quickIDs := map[string]bool{}
	b.locked(func() {
		transcript, fallback = a.transcript, a.view.UpdatedAt
		if a.live != nil {
			for id := range a.live.quicks {
				quickIDs[id] = true
			}
		}
	})
	var tasks []appwire.Task
	index := map[string]int{}
	add := func(task appwire.Task) {
		if _, dup := index[task.ID]; dup {
			return
		}
		index[task.ID] = len(tasks)
		tasks = append(tasks, task)
	}
	// Running is a live claim, not the checkpoint's permanent resting state.
	presence, fresh := session.ReadSessionPresence(filepath.Dir(transcript), b.now())
	for _, notice := range session.ReadSessionTasks(transcript) {
		if notice.State == session.TaskRunning && appTaskWasInterrupted(presence, fresh, strconv.FormatUint(notice.ID, 10), notice.StartedAt) {
			notice.State = session.TaskInterrupted
		}
		if notice.Kind == session.TaskKindQuick {
			quickIDs[appTaskID(id, notice.ID)] = true
			continue
		}
		// A TASK THE CONVERSATION LEFT RUNNING WHEN IT CLOSED is interrupted:
		// nothing is driving it. The protocol has no word for that, and it is
		// not done, so it is listed as failed with the reason as its result.
		if notice.State == session.TaskInterrupted {
			notice.State = session.TaskFailed
			if strings.TrimSpace(notice.Result) == "" {
				notice.Result = "Interrupted"
			}
		}
		at := notice.EndedAt
		if at.IsZero() {
			at = notice.StartedAt
		}
		if at.IsZero() {
			at = fallback
		}
		notice := notice
		if task, ok := appTaskOf(id, &notice, false, "", at); ok {
			add(task)
		}
	}
	if path := session.TaskIndexPath(transcript); path != "" {
		for _, row := range session.ReadTaskIndex(path) {
			if row.SessionID != id {
				continue
			}
			if row.Kind == session.TaskKindQuick {
				quickIDs[appIndexTaskID(id, row.ID)] = true
				continue
			}
			state := appTaskState(row.Status)
			interrupted := state == appwire.TaskRunning && appTaskWasInterrupted(presence, fresh, row.ID, row.StartedAt)
			if interrupted {
				state = appwire.TaskFailed
			}
			if state == "" {
				continue
			}
			at := row.EndedAt
			if at.IsZero() {
				at = row.StartedAt
			}
			if at.IsZero() {
				at = fallback
			}
			title := row.Title
			if title == "" {
				title = row.Label
			}
			parent := ""
			if row.Parent != "" && row.Parent != row.ID {
				parent = appIndexTaskID(id, row.Parent)
			}
			result := ""
			if interrupted {
				result = "Interrupted"
			}
			add(appwire.Task{Result: result, ID: appIndexTaskID(id, row.ID), Agent: id, Parent: parent, Title: title, State: state, UpdatedAt: at, YourCall: state == appwire.TaskUnverified})
		}
	}
	for taskID, task := range b.heldTaskViews(a) {
		if quickIDs[taskID] {
			continue
		}
		if i, ok := index[taskID]; ok {
			if task.UpdatedAt.IsZero() {
				task.UpdatedAt = tasks[i].UpdatedAt
			}
			if task.Title == "" {
				task.Title = tasks[i].Title
			}
			if task.Parent == "" {
				task.Parent = tasks[i].Parent
			}
			tasks[i] = task
			continue
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = fallback
		}
		tasks = append(tasks, task)
	}
	appTaskExtras(tasks)
	return tasks, nil
}

// appIndexTaskID is a task index row's id on the wire: the agent's own number
// named with the agent, as every task id is (appwork.go).
func appIndexTaskID(agent, raw string) string {
	if n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err == nil {
		return appTaskID(agent, n)
	}
	return raw
}

// appMemberAgent is a team member's agent id: its transcript's folder name.
func appMemberAgent(member teamstore.Member) string {
	transcript := member.File
	if strings.TrimSpace(transcript) == "" {
		transcript = member.Key
	}
	return appAgentID(transcript)
}

// Close lets every conversation go and stops the watchers.
func (b *appBackend) Close() error {
	already := false
	var crews *appCrewRun
	var agents []*appAgent
	var warmed chan struct{}
	var balances *appBalances
	b.locked(func() {
		if already = b.closing; already {
			return
		}
		b.closing = true
		if b.left != nil {
			close(b.left)
		}
		if b.reportCancel != nil {
			b.reportCancel()
		}
		if b.accountAuth != nil {
			b.accountAuth.cancel()
			b.accountAuth.flow.Cancel()
		}
		crews, warmed, balances = b.crewRun, b.warmed, b.balances
		if b.signIn != nil {
			b.signIn.Cancel()
			b.signIn = nil
		}
		for id, f := range b.follows {
			close(f.stop)
			delete(b.follows, id)
		}
		for _, a := range b.agents {
			agents = append(agents, a)
		}
	})
	if already {
		return nil
	}
	// THIS MACHINE'S FACTORY IS LET GO BEFORE ANYTHING THAT WAITS ON A
	// CONVERSATION. Stopping the close watch waits for a carry-on that may be
	// opening a conversation, and nothing below it is the factory's business;
	// the next engine cannot serve the factory until this one has let it go.
	// Its runner stops here the way a quit has always stopped it.
	b.closeLocalFactory()
	b.stopCloseWatch()
	if balances != nil {
		balances.stop()
	}
	b.closeFactories()
	// What was mid-reply and what was running is written down before anything
	// stops, so the next start can carry on (appquit.go).
	b.markQuit(agents)
	close(b.sweepStop)
	<-b.sweepDone
	if b.stopPhases != nil {
		b.stopPhases()
	}
	if b.stopProgress != nil {
		b.stopProgress()
	}
	if warmed != nil {
		<-warmed
	}
	if crews != nil {
		crews.close()
	}
	var wg sync.WaitGroup
	for _, a := range agents {
		if b.liveOf(a) == nil {
			continue
		}
		wg.Add(1)
		guard.Go("app/close", func() {
			defer wg.Done()
			// Stop first: a turn still running is interrupted rather than left
			// holding the gate while the process leaves.
			// A conversation joined on a window's host is that window's, and
			// is only detached from.
			if live := b.liveOf(a); live != nil && live.conn.Assembled {
				live.conn.Conv.Interrupt()
			}
			b.letGo(a)
		})
	}
	wg.Wait()
	b.closeReferences(agents)
	// A BLANK CONVERSATION LIVES NO LONGER THAN THE PROCESS THAT MADE IT
	// (appblank.go): with everything let go, each one this process made that
	// nobody said anything in is taken away.
	for _, a := range agents {
		b.reapBlank(a)
	}
	b.closeCollections()
	// Kept-running processes stay up through a quit; services.stop_all is
	// how a quit stops them (appservices.go).
	b.closeServices()
	return nil
}

// letGo gives a's conversation back under its gate.
func (b *appBackend) letGo(a *appAgent) {
	a.gate.Lock()
	defer a.gate.Unlock()
	b.release(a)
}

func (b *appBackend) liveNeedsYou(live *appLive) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return live.needsYou()
}

func (b *appBackend) isClosing() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closing
}

// sweep lets idle conversations go ([appAgentIdle]).
func (b *appBackend) sweep() {
	defer close(b.sweepDone)
	ticker := time.NewTicker(appSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-b.sweepStop:
			return
		case <-ticker.C:
			b.sweepOnce()
		}
	}
}

// sweepOnce is one look at the idle rule. A conversation is kept while a turn
// runs or a question waits for the person, whatever the clock says.
func (b *appBackend) sweepOnce() {
	now := b.now()
	var idle []*appAgent
	b.locked(func() {
		for _, a := range b.agents {
			if live := a.live; live != nil && !live.running && now.Sub(live.lastUse) >= appAgentIdle {
				idle = append(idle, a)
			}
		}
	})
	for _, a := range idle {
		b.sweepAgent(a, now)
	}
}

// sweepAgent lets one idle conversation go, unless something took its gate or
// woke it since the look.
func (b *appBackend) sweepAgent(a *appAgent, now time.Time) {
	if !a.gate.TryLock() {
		return
	}
	defer a.gate.Unlock()
	var live *appLive
	keep := true
	b.locked(func() {
		live = a.live
		keep = live == nil || live.running || now.Sub(live.lastUse) < appAgentIdle
	})
	// A member of an open crew stays held while it has work in flight or a
	// reason the crew would open it again at once; one with nothing to do goes
	// like any other, and the crew opens it when traffic comes (applazy.go).
	if !keep && !b.liveNeedsYou(live) && !b.serviceKeeps(a.view.ID) && (!b.crewKeeps(a.view.ID) || b.memberQuiet(a)) {
		// The state the app reads does not move: the conversation is as
		// idle on disk as it was in memory.
		b.locked(func() { a.crewHeld = false })
		b.release(a)
	}
}

// appNoProvider is what the app is told when something could not start
// because no model provider is connected.
const appNoProvider = "no model provider is connected"

// appEngineError keeps a wire error's code and calls anything else the
// engine's failure, in words fit for a person.
//
// THE MISSING-KEY REFUSAL IS SAID IN THE APP'S WORDS. The terminal's remedy for
// it is to export OPENROUTER_API_KEY and run the command again, which a person
// in the app can do nothing with: the app connects providers itself, and a
// refusal that names a variable reads as something broken. It arrives as a
// value from the process half and as a sentence from across the loopback, so
// both are recognised ([appMissingKey]).
func appEngineError(err error) error {
	var wire *appwire.Error
	if errors.As(err, &wire) {
		return wire
	}
	// A description of the message the engine would not keep is the app's to
	// correct, not the engine failing.
	if errors.Is(err, session.ErrMessageMark) {
		return &appwire.Error{Code: appwire.CodeBadParams, Message: redact.Secrets(plainWords(err.Error()))}
	}
	if appMissingKey(err) {
		return &appwire.Error{Code: appwire.CodeEngine, Message: appNoProvider}
	}
	return &appwire.Error{Code: appwire.CodeEngine, Message: redact.Secrets(plainWords(err.Error()))}
}

// appMissingKey says whether err is the refusal for want of any provider: the
// value itself ([config.ErrNoAPIKey]), its sentence from across the loopback,
// or the wire error [appEngineError] made of either. It is the one reading of
// that refusal, so whatever waits on a key reads it the way it is said.
func appMissingKey(err error) bool {
	if err == nil {
		return false
	}
	var wire *appwire.Error
	if errors.As(err, &wire) && wire.Message == appNoProvider {
		return true
	}
	return errors.Is(err, config.ErrNoAPIKey) || strings.Contains(strings.ToLower(err.Error()), strings.ToLower(config.ErrNoAPIKey.Error()))
}

// appTeamLines is a team delivery's lines as facts, words redacted as the
// notice's own text is.
func appTeamLines(lines []session.TeamLine) []appwire.TeamLine {
	out := make([]appwire.TeamLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, appwire.TeamLine{CrewName: line.Team, From: line.From, To: line.To, Kind: line.Kind, Text: redact.Secrets(strings.TrimSpace(line.Text)), Post: line.Thread})
	}
	return out
}

// appHandoff is what a finished team_post call delivered, read from its own
// answer (session.ReadTeamPost); nil for any other call, one with no answer,
// and one whose words named no recipient.
func appHandoff(tool, args, answer string) *appwire.Handoff {
	if tool != "team_post" || strings.TrimSpace(answer) == "" {
		return nil
	}
	if post, ok := session.ReadTeamPost(answer); ok {
		h := &appwire.Handoff{CrewName: post.Team, Crew: post.TeamID, To: post.To, Post: post.Post, Answers: post.Answers, Delivered: true, Up: post.Up}
		if post.Up {
			h.FromCrew = post.From
		}
		return h
	}
	var asked struct{ To string }
	if json.Unmarshal([]byte(args), &asked) != nil {
		return nil
	}
	to := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(asked.To)), "@")
	switch to {
	case "":
		return nil
	case "everyone":
		to = "room"
	}
	return &appwire.Handoff{To: to, Delivered: false, Reason: redact.Secrets(strings.TrimSpace(answer))}
}

// appTeamDelivery is a team delivery's lines as the person reads them: who
// said each, and what.
func appTeamDelivery(lines []session.TeamLine) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		who := line.From
		switch who {
		case "you", "manager":
		case "system":
			who = "codeaf"
		default:
			who = "@" + who
		}
		out = append(out, who+": "+strings.TrimSpace(line.Text))
	}
	return redact.Secrets(strings.Join(out, "\n"))
}

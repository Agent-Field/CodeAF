package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// The remote agent IS the surface's agent, checked at compile time so a method
// added to tui3.Agent breaks the build here rather than at the first keystroke
// of a remote session.
var _ tui3.Agent = (*remote.Agent)(nil)

func TestRedialSSHStderrStaysOutOfTheTerminal(t *testing.T) {
	link := &engineLink{}
	terminal := new(bytes.Buffer)
	first := &tailWriter{}
	if _, err := link.stderrWriter(first, terminal).Write([]byte("host-key prompt\n")); err != nil {
		t.Fatal(err)
	}
	if terminal.String() != "host-key prompt\n" {
		t.Fatalf("the first ssh prompt was not shown: %q", terminal.String())
	}

	link.process = &exec.Cmd{}
	redial := &tailWriter{}
	if _, err := link.stderrWriter(redial, terminal).Write([]byte("connection refused\n")); err != nil {
		t.Fatal(err)
	}
	if terminal.String() != "host-key prompt\n" {
		t.Fatalf("redial stderr painted the terminal: %q", terminal.String())
	}
	if got := redial.String(); got != "connection refused\n" {
		t.Fatalf("redial stderr was not retained: %q", got)
	}
}

func TestParseHostTarget(t *testing.T) {
	cases := []struct {
		raw       string
		dest      string
		workspace string
		fails     bool
	}{
		{raw: "devbox", dest: "devbox"},
		{raw: "me@devbox", dest: "me@devbox"},
		{raw: "devbox:code/app", dest: "devbox", workspace: "code/app"},
		{raw: "devbox:/srv/app", dest: "devbox", workspace: "/srv/app"},
		{raw: "me@devbox:/srv/app", dest: "me@devbox", workspace: "/srv/app"},
		// A trailing colon is "the engine's own home", said out loud.
		{raw: "devbox:", dest: "devbox"},
		// localhost is a host like any other — it is also the rig this feature is
		// tested end to end on, and nothing may special-case it.
		{raw: "localhost", dest: "localhost"},
		{raw: "localhost:code/app", dest: "localhost", workspace: "code/app"},
		// A path with its own colon in it belongs to the workspace: the FIRST
		// colon is the split and the rest is the path as typed.
		{raw: "devbox:code/a:b", dest: "devbox", workspace: "code/a:b"},
		{raw: "  devbox:code/app  ", dest: "devbox", workspace: "code/app"},
		{raw: "", fails: true},
		{raw: ":/srv/app", fails: true},
	}
	for _, c := range cases {
		dest, workspace, err := parseHostTarget(c.raw)
		if c.fails {
			if err == nil {
				t.Fatalf("parseHostTarget(%q) was accepted", c.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseHostTarget(%q): %v", c.raw, err)
		}
		if dest != c.dest || workspace != c.workspace {
			t.Fatalf("parseHostTarget(%q) = %q, %q; want %q, %q", c.raw, dest, workspace, c.dest, c.workspace)
		}
	}
}

// THE WORKSPACE IS NEVER RESOLVED HERE. A relative path stays relative, because
// the engine resolves it against its own home and this machine has no standing
// to make it absolute.
func TestParseHostTargetLeavesTheWorkspaceAsTyped(t *testing.T) {
	_, workspace, err := parseHostTarget("devbox:code/app")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(workspace, "/") {
		t.Fatalf("the workspace was made absolute: %q", workspace)
	}
}

func TestFlagsThatCannotTravelAreRefusedRatherThanIgnored(t *testing.T) {
	for _, launch := range []hostLaunch{
		{target: "devbox", yolo: true},
		{target: "devbox", noCompact: true},
		{target: "devbox:app", yolo: true, noCompact: true},
	} {
		err := launch.check()
		if err == nil {
			t.Fatalf("%+v was accepted silently", launch)
		}
		if !strings.Contains(err.Error(), "devbox") {
			t.Fatalf("the refusal does not name the machine to set it on: %v", err)
		}
	}
	if err := (hostLaunch{target: "devbox"}).check(); err != nil {
		t.Fatalf("an ordinary launch was refused: %v", err)
	}
}

func TestShellQuoteSurvivesAPathWithSpacesAndQuotes(t *testing.T) {
	if got := shellQuote("code/my app"); got != `'code/my app'` {
		t.Fatalf("shellQuote = %s", got)
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("shellQuote = %s", got)
	}
}

func TestMissingCommandIsRecognizedInEveryShellsWording(t *testing.T) {
	for _, said := range []string{
		"bash: codeaf: command not found",
		"sh: 1: codeaf: not found",
		"zsh: command not found: codeaf",
	} {
		if !mentionsMissingCommand(said) {
			t.Fatalf("not recognized as a missing codeaf: %q", said)
		}
	}
	if mentionsMissingCommand("Permission denied (publickey).") {
		t.Fatal("an ssh refusal was read as a missing codeaf")
	}
}

// H9: both current and former far-shell missing-command replies are recognised,
// and the remedy names the current command and the rename date.
func TestH9HostMissingCommandNamesTheCurrentInstallation(t *testing.T) {
	for _, said := range []string{
		"bash: codeaf: command not found",
		"sh: aforge: not found", // legacy-name
	} {
		if !mentionsMissingCommand(said) {
			t.Fatalf("missing command was not recognised: %q", said)
		}
	}
	got := missingHostCommand("devbox").Error()
	for _, want := range []string{"called codeaf now", "before 2026-09-14", "installed on devbox under that name"} {
		if !strings.Contains(got, want) {
			t.Fatalf("host remedy %q does not contain %q", got, want)
		}
	}
}

func TestSSHSpawnCarriesTheLowLatencyPolicy(t *testing.T) {
	// A SHORT HOME, because the control socket must fit enginehost.SocketLimit:
	// under macOS's own $TMPDIR the path came to 104 bytes, one over, and the
	// multiplexing options were rightly left out.
	short, err := os.MkdirTemp("/tmp", "acp")
	if err != nil {
		t.Skipf("no short folder for the control socket: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	t.Setenv("CODEAF_HOME", short)
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	args := strings.Join(sshTransportArgs("devbox", "codeaf engine"), " ")
	for _, want := range []string{
		"-T", "ControlMaster=auto", "ControlPath=", "ControlPersist=300",
		"ServerAliveInterval=3", "ServerAliveCountMax=3", "IPQoS=lowdelay",
		"devbox codeaf engine",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("ssh args %q do not contain %q", args, want)
		}
	}
	if strings.Contains(args, " -C ") || strings.Contains(args, "Compression=yes") {
		t.Fatalf("ssh args enable whole-stream compression on the LAN: %q", args)
	}
}

func TestAnOverlongStateRootLosesOnlyMultiplexing(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), strings.Repeat("deep", 40)))
	args := strings.Join(sshTransportArgs("devbox", "codeaf engine"), " ")
	if strings.Contains(args, "ControlPath=") || strings.Contains(args, "ControlMaster=") {
		t.Fatalf("overlong control socket was still enabled: %q", args)
	}
	if !strings.Contains(args, "ServerAliveInterval=3") || !strings.HasSuffix(args, "devbox codeaf engine") {
		t.Fatalf("the ordinary ssh transport was lost with multiplexing: %q", args)
	}
}

// The flag exists and is documented in exactly one place: `codeaf chat -h`.
//
// ASKING FOR HELP IS NOT A FAILURE (usage.go), so the page goes to stdout and
// the door leaves with 0 rather than handing back the flag package's internal
// `flag: help requested`. The old shape of this test accepted that string and
// never read the page at all, which meant it passed whether or not `--host`
// was on it — the one fact it is named for.
func TestHostFlagIsOnTheChatUsage(t *testing.T) {
	page, _ := captureUsage(t)
	if code := exitCodeOf(openChatV3("chat", []string{"--help"}, false)); code != 0 {
		t.Fatalf("`codeaf chat --help` left with %d, want 0", code)
	}
	if !strings.Contains(page.String(), "--host") {
		t.Fatalf("`codeaf chat --help` does not document --host:\n%s", page)
	}
}

// ── the ambient side over a connection ──────────────────────────────────────

// THE ENGINE DOOR BUILDS THE AMBIENT SIDE, which is what puts the `stand` tool
// on the belt over --host: internal/session leaves it off entirely when
// Config.Standing has no store behind it (its tools_standing.go), so a nil here
// is a remote conversation that cannot be asked to keep an eye on anything and
// says so rather than trying.
//
// The two wire doors are asserted beside it because they are the same fact from
// the surface's side — the band and the pause key read the ENGINE machine's
// store, and a closure that was never filled is a refusal on the wire.
func TestTheEngineDoorKeepsAutomationsOnOverAConnection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", filepath.Join(home, "state"))
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Chdir(home)
	// Close the engine owner, including catalog and pool writers, before this home is removed.
	freshEngineProcess(t)

	engine, err := bootEngine(remote.Hello{Version: remote.Version}, "", "")
	if err != nil {
		t.Fatalf("the engine door did not open: %v", err)
	}
	defer func() { _ = engine.Agent.Close() }()

	// The wire's automations are this machine's store ([engineAutomations]
	// hands back nil when it cannot be opened), so a seam here is proof a
	// remote surface can list and change them.
	if engine.Automations == nil || engine.Automations.Store == nil {
		t.Fatal("the engine serves no automations, so a remote surface has no list and no card")
	}
	// These are production bindings on the engine returned by bootEngine, not
	// callbacks supplied by a test fixture. Removing either binding makes a
	// plain linked-local conversation retain its boot-time gate or source set.
	if engine.RefreshModelSources == nil || engine.RefreshApprovals == nil {
		t.Fatal("the production engine carries no live profile refresh doors")
	}
	// The seam the session itself proposes through. It is read back off the
	// launch the engine assembles from, because bootEngine hands the config to
	// the agent and keeps none of it. The engine's door word rides on the
	// PROCESS now rather than on the launch's options (chatv3_process.go).
	proc, err := openV3Process("engine")
	if err != nil {
		t.Fatalf("the process every v3 door builds once did not open: %v", err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Workspace: home})
	if err != nil {
		t.Fatalf("the shared assembly did not open: %v", err)
	}
	if launch.Config.Automations == nil || launch.Config.Automations.Store == nil {
		t.Fatal("the engine door built no automations seam, so `automation` is off the belt over --host")
	}
	// AND IT IS THE ONE STORE THIS PROCESS HOLDS, the one the wire serves, so
	// what the session proposes is what a surface lists.
	if engine.Automations.Store != launch.Config.Automations.Store {
		t.Fatal("the wire serves a different store from the one the session proposes into")
	}
}

// AND THE SURFACE IS HANDED IT, with the live firing field that nobody can
// answer left out. This is the door's half: what hostOptions wires is
// what a person over --host actually gets.
func TestTheHostDoorWiresTheFarMachineAndNothingAboutThisMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	welcome := remote.Welcome{
		Version: remote.Version, Workspace: "/srv/app",
		Build: "1265feda built 2026-08-27 13:28", BashBackgroundAfterSeconds: 47,
		Automations: true,
	}
	// A REAL CLIENT, not a nil: the door primes three readings on goroutines
	// of their own the moment it is built, and a nil here used to be three
	// recovered nil dereferences per run that the guard swallowed
	// (chatv3_host_duty_test.go pins the seam that now refuses them).
	client := hostedClient(t)
	options, _ := hostOptions(onePipeFleet("devbox", client), welcome, false)
	if options.Build != welcome.Build {
		t.Fatalf("the surface says build %q, want the engine's %q", options.Build, welcome.Build)
	}
	if options.BashBackgroundAfterSeconds != welcome.BashBackgroundAfterSeconds {
		t.Fatalf("the surface countdown is %d, want the engine's %d", options.BashBackgroundAfterSeconds, welcome.BashBackgroundAfterSeconds)
	}
	// The automations are the FAR machine's, read over the wire, when the far
	// engine serves them.
	if options.Automations.List == nil || options.Automations.Create == nil {
		t.Fatal("the door hands over no automations seam, so a remote surface has no list and no card")
	}
	// ErrandsRoot is the LOCAL errands folder, and there is no errand over a
	// connection.
	if options.ErrandsRoot != "" {
		t.Fatalf("the door pointed the surface at %q, a folder on the wrong machine", options.ErrandsRoot)
	}
}

// waitFor polls a condition rather than sleeping on it: the fetch it is waiting
// for is a goroutine, and a fixed sleep is either a slow test or a flaky one.
func waitFor(done func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// ── the connection, as the person meets it ──────────────────────────────────

// WHO ELSE IS IN THE ROOM IS SAID, AND AN EMPTY ROOM SAYS NOTHING. The count is
// the engine's, because only the machine holding the session can know it, and
// zero draws nothing at all rather than a reassuring line about being alone.
func TestTheEntryNoticeSaysWhoElseIsOnTheConversation(t *testing.T) {
	for _, row := range []struct {
		welcome remote.Welcome
		want    string
	}{
		{remote.Welcome{}, ""},
		{remote.Welcome{Attached: 1}, "another window is on this conversation"},
		{remote.Welcome{Attached: 3}, "3 other windows are on this conversation"},
		{remote.Welcome{Note: "session open elsewhere — started a new one"}, "session open elsewhere — started a new one"},
		{
			remote.Welcome{Note: "session open elsewhere — started a new one", Attached: 1},
			"session open elsewhere — started a new one · another window is on this conversation",
		},
	} {
		if got := hostEntryNotice(row.welcome); got != row.want {
			t.Errorf("a welcome with %d attached and note %q reads %q, wanted %q",
				row.welcome.Attached, row.welcome.Note, got, row.want)
		}
	}
}

// THE FOUR THINGS ONLY A CONNECTION KNOWS REACH THE SURFACE. The client answers
// all four, and this door hands all four over: the live sentence about a link
// being redialled, the measured round trip, the one-off news a redial
// discovered, and the questions raised while nobody was attached. A nil in any
// of them is a fact a person would never be told, so the test is about presence
// rather than wording — the sentences themselves belong to internal/remote.
func TestTheConnectionSeamsReachTheSurface(t *testing.T) {
	client := hostedClient(t)
	seams := newHostSeams(client)
	var _ func() string = seams.Link
	var _ func() (time.Duration, error) = seams.Ping
	var _ func() string = seams.Notice
	var _ func() ([]remote.HeldQuestion, error) = seams.Held
	if seams.Link == nil || seams.Ping == nil || seams.Notice == nil || seams.Held == nil {
		t.Fatal("a seam that is not filled is a seam nobody can wire")
	}

	options, _ := hostOptions(onePipeFleet("devbox", client), remote.Welcome{Version: remote.Version, Workspace: "/srv/app"}, false)
	if options.Link.Note == nil || options.Link.Ping == nil || options.Link.Notice == nil || options.Link.Held == nil {
		t.Fatalf("the surface was handed %+v — a seam left nil is a fact nobody is told", options.Link)
	}
}

type questionOnArrivalAgent struct {
	quietAgent
	events chan session.Event
}

func (a *questionOnArrivalAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	return a.events, nil
}

func TestTheInitialQuestionSnapshotSurvivesAReemission(t *testing.T) {
	agent := &questionOnArrivalAgent{events: make(chan session.Event)}
	sess := remote.NewSession(&remote.Engine{Agent: agent, Workspace: "/srv/app"}, true)
	t.Cleanup(func() { close(agent.events); _ = sess.Close() })
	dial := func() *remote.Client {
		surface, engine := remote.Pipe()
		go func() {
			_ = remote.ServeAttach(engine, engine, remote.AttachOptions{
				Open: func(remote.Hello) (*remote.Session, error) { return sess, nil },
			})
			_ = engine.Close()
		}()
		client, err := remote.Dial(surface, "devbox", remote.Hello{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	first := dial()
	events, err := first.Agent().Submit(context.Background(), "run the checks")
	if err != nil {
		t.Fatal(err)
	}
	ask := session.Event{Kind: session.EventConsentRequest, ID: 7, Tool: "bash"}
	agent.events <- ask
	<-events
	back := dial()
	seams := newHostSeams(back)
	// A repeated event is fully published before the surface asks for its
	// initial cards. The live registry now considers this window informed.
	agent.events <- ask
	<-events
	held, err := seams.Held()
	if err != nil || len(held) != 1 || held[0].Event.Unwire().ID != 7 {
		t.Fatalf("the initial question disappeared: held=%+v err=%v", held, err)
	}
	if later, err := seams.Held(); err != nil || len(later) != 0 {
		t.Fatalf("later reads must use the current registry: held=%+v err=%v", later, err)
	}
}

// THE WAITING ROOM CROSSES INTO THE SURFACE'S OWN SHAPE, event and all. The
// translation is the door's job because internal/tui3 does not import the
// protocol, and the one field JSON could not carry — the event itself — has to
// come out the other side unwrapped or the card would be drawn from nothing.
func TestHeldQuestionsCrossAsTheSurfacesOwnShape(t *testing.T) {
	seams := hostSeams{Held: func() ([]remote.HeldQuestion, error) {
		return []remote.HeldQuestion{{
			Kind:  remote.HeldConsent,
			Event: remote.WireEvent(session.Event{Kind: session.EventConsentRequest, ID: 7, Tool: "bash"}),
			Since: time.Now().Add(-2 * time.Hour),
		}}, nil
	}}
	held, err := hostHeld(seams)()
	if err != nil {
		t.Fatalf("the waiting room refused: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("%d questions crossed, wanted 1", len(held))
	}
	if held[0].Kind != remote.HeldConsent || held[0].Event.ID != 7 || held[0].Event.Tool != "bash" {
		t.Fatalf("the question arrived as %+v", held[0])
	}
	if held[0].Since.IsZero() {
		t.Fatal("a question that arrived with no waiting time cannot say how long it waited")
	}

	// AND A FAR END THAT DID NOT ANSWER IS AN ERROR AND NOT AN EMPTY LIST.
	broken := hostSeams{Held: func() ([]remote.HeldQuestion, error) { return nil, errors.New("no") }}
	if _, err := hostHeld(broken)(); err == nil {
		t.Fatal("a refused reading came back as nothing waiting")
	}
}

// THE ARROWS SURVIVE A SECOND CONVERSATION. The recall store is this machine's
// and every conversation the fleet opens beside the first — from home, from
// /new, from the target on home's rule — has to be handed the same door, or
// its box scrolls the transcript where it should recall the last thing typed.
// Until 2026-09-09 [engineFleet.bundle] left it nil.
func TestAConversationOpenedBesideKeepsTheRecallStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := hostedClient(t)
	fleet := onePipeFleet("devbox", client)
	options, _ := hostOptions(fleet, remote.Welcome{Version: remote.Version, Workspace: "/srv/app"}, false)
	if options.History == nil {
		t.Fatal("the surface opened with no recall store")
	}
	if fleet.machine.history == nil {
		t.Fatal("the fleet was not handed the recall store the surface got")
	}
	conv := fleet.bundle(fleet.boot, remote.Welcome{Version: remote.Version, Workspace: "/srv/app"})
	if conv.History != fleet.machine.history {
		t.Fatal("a conversation opened beside carries a different recall store from the first")
	}
}

// THE ROW LEAVES UNDER THE HAND. `ctrl+e` on home writes and then re-reads the
// world on the spot, and that re-read answers from what [hostWorld] holds — so
// a write that did not correct the held copy redrew the put-away row exactly as
// it was, and the row went several seconds later, when a beat's fetch happened
// to return and a beat happened to rebuild. This holds the far end still, which
// is the state the surface is in for the whole of that window, and asks the
// seam what home would draw.
func TestPuttingAConversationAwayCorrectsWhatHomeDrawsAtOnce(t *testing.T) {
	held := make(chan struct{})
	defer close(held)
	row := session.SessionRow{ID: "3558", Dir: "/srv/state/projects/-srv-app/3558", Title: "the old one"}
	var written []string
	world := &hostWorld{
		ask: func() (session.World, error) {
			<-held
			return session.World{}, nil
		},
		put: func(dir string, archived bool) error {
			written = append(written, dir)
			if !archived {
				t.Errorf("ctrl+e on a live row asked for archived=false")
			}
			return nil
		},
		held: session.World{Projects: []session.Project{{
			Bucket: "-srv-app", Sessions: []session.SessionRow{row},
		}}},
		known: true,
	}

	if err := world.archive(row.Dir, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(written) != 1 || written[0] != row.Dir {
		t.Fatalf("the put-away did not travel: %+v", written)
	}
	// THE VERY NEXT READING IS THE ONE HOME REBUILDS FROM, and no fetch has
	// returned — the far end is still held above.
	drawn, known := world.world()
	if !known {
		t.Fatal("the seam stopped answering after a write it accepted")
	}
	if len(drawn.Projects) != 1 || len(drawn.Projects[0].Sessions) != 1 {
		t.Fatalf("the write reshaped the world: %+v", drawn.Projects)
	}
	if !drawn.Projects[0].Sessions[0].Archived {
		t.Fatal("home would have redrawn the row it just put away")
	}
	// AND THE ENGINE IS STILL ASKED WHAT IT REALLY THINKS. Ageing the entry is
	// the second half of the correction: the held copy is this surface's belief
	// about a write it made, never a substitute for the disk's own answer.
	if !world.duty.due("", hostWorldEvery) {
		t.Fatal("the next reading would have gone on trusting the patched copy")
	}
}

// A REFUSED PUT-AWAY LEAVES THE ROW WHERE IT IS. Home says `could not put it
// away` on its own line, and a cache that had already moved the row would be
// this screen disagreeing with the disk the conversation is on.
func TestARefusedPutAwayLeavesTheConversationOnHome(t *testing.T) {
	row := session.SessionRow{ID: "3558", Dir: "/srv/state/projects/-srv-app/3558"}
	world := &hostWorld{
		ask: func() (session.World, error) { return session.World{}, nil },
		put: func(string, bool) error { return errors.New("no conversation at that folder") },
		held: session.World{Projects: []session.Project{{
			Bucket: "-srv-app", Sessions: []session.SessionRow{row},
		}}},
		known: true,
	}
	err := world.archive(row.Dir, true)
	if err == nil || err.Error() != "no conversation at that folder" {
		t.Fatalf("archive = %v, want the engine's own refusal", err)
	}
	if world.held.Projects[0].Sessions[0].Archived {
		t.Fatal("a refused write put the row away anyway")
	}
}

// AND BRINGING ONE BACK IS THE SAME CORRECTION IN THE OTHER DIRECTION. `ctrl+e`
// from inside the archive is its own undoing, and a row that stayed drawn as
// put-away would make the key look like it had done nothing.
func TestBringingAConversationBackCorrectsTheHeldWorldToo(t *testing.T) {
	row := session.SessionRow{ID: "3558", Dir: "/srv/state/projects/-srv-app/3558", Archived: true}
	world := &hostWorld{
		ask:  func() (session.World, error) { return session.World{}, nil },
		put:  func(string, bool) error { return nil },
		held: session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}},
	}
	if err := world.archive(row.Dir, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if world.held.Projects[0].Sessions[0].Archived {
		t.Fatal("the row came back on the engine's disk and stayed away on the screen")
	}
}

func TestHostedWelcomeCarriesUnreadProfileKeysToSurface(t *testing.T) {
	workspace := t.TempDir()
	agent := v3TrackedAgent(t, workspace)
	t.Cleanup(agent.SettleWrites)
	t.Cleanup(func() { _ = agent.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version, Workspace: workspace}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: agent, Workspace: workspace, UnreadProfileKeys: []string{"models"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = loop.Close()
		// Closing the client only starts shutdown; join the engine before its
		// conversation and deferred writes lose their temporary directories.
		select {
		case <-loop.Served:
		case <-time.After(5 * time.Second):
			t.Error("hosted welcome engine did not finish shutdown")
		}
	})
	options, _ := hostOptions(onePipeFleet("devbox", loop.Client), loop.Client.Welcome(), false)
	if got := options.UnreadProfileKeys; len(got) != 1 || got[0] != "models" {
		t.Fatalf("hosted unread keys = %v", got)
	}
}

// A CLOSED HOSTED LOOP HAS FINISHED THE REAL AGENT'S LEAVE, AND NOTHING WRITES
// UNDER ITS HOME AFTERWARDS. The test above failed under load with "unlinkat
// …/<place>: directory not empty" (#1647): the loop's Close returned while the
// engine was still closing the conversation, and that close's last presence
// write (taskpresence.go — a `.presence-*.json` renamed into place, then
// removed) landed inside the folder TempDir's cleanup was walking. So this test
// owns the home, closes, and then asks the two things that cleanup relies on:
// the engine has already answered, and the home can be removed and stays gone.
func TestHostedLoopCloseFinishesBeforeItsHomeIsRemoved(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	agent := v3TrackedAgentIn(t, workspace, home)
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version, Workspace: workspace}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: agent, Workspace: workspace, UnreadProfileKeys: []string{"models"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if err := loop.Close(); err != nil {
		t.Fatal(err)
	}
	// The receive must be ready without waiting: otherwise Close has returned
	// while the engine can still write under home.
	select {
	case err = <-loop.Served:
	default:
		<-loop.Served
		t.Fatal("Close returned before the hosted engine finished")
	}
	if err != nil {
		t.Fatalf("hosted engine: %v", err)
	}
	if err := os.RemoveAll(home); err != nil {
		t.Fatalf("remove closed conversation home: %v", err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home after engine finished and removal: %v, want ErrNotExist", err)
	}
}

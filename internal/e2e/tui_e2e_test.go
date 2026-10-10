//go:build e2e

package e2e

// TestTUIE2E is v3's own surface, driven end to end: the real binary, a real
// terminal, a real model, and the screen read back with capture-pane.
//
// Every subtest builds its own CODEAF_HOME and its own repository, and NOT ONE
// STRING IS WRITTEN DOWN HERE. Every needle comes through [say] out of the table
// in tuiwords_test.go, which an ordinary untagged test reads back against
// internal/tui3's own sources — so a sentence the surface stops drawing turns a
// four-hundred-millisecond gate red on the pull request that removed it, rather
// than turning this seventeen-minute suite red in a wave nobody ran it in. That
// is issue #184's whole mechanism, and it exists because this file spent a week
// waiting for a home that had been redesigned out from under it.
//
// ── HOW TO RUN IT ───────────────────────────────────────────────────────────
//
//	go test -tags e2e -count=1 -timeout 120m -v ./internal/e2e/
//
// Forty minutes is enough for TestTUIE2E alone. The FULL
// tagged package — ManualOnTheWire, QuestionsE2E, roomfeed, families, custody,
// contracts — does not fit in forty: Spark's #807 run hit the ceiling before
// TestTUIE2E started. Prefer `make test-e2e-tui` for the ambient surface, or
// give the whole package two hours. It needs a provider key and tmux, costs a
// few cents. The key is resolved the way the product resolves one ([liveKey]:
// OPENROUTER_API_KEY, OPENAI_API_KEY, then the profile's api_key row).
// CLAUDE.md's Tests section says the same thing.
//
// AUTOMATIONS ARE NOT HERE. `ask here`, the reminder a window's clock says and
// the narrow window's stacked exchange were subtests of this test while what
// they made was a standing order; they need no key now, and they are
// automations_e2e_test.go's [TestAutomationsE2E].
//
// ── TWO WIDTHS, AND THE REASON IS IN THE PRODUCT ────────────────────────────
//
// Home at rest is its panels, and the width chooses the columns
// (internal/tui3's homegrid.go): two from a hundred and ten cells, three from a
// hundred and seventy. There is NO CARD AT REST at any width; the one card left
// stands beside a SEARCH, from a hundred and thirty-six cells (homebridge.go).
// So a subtest that reads that card types first and runs at [tuiWide], and a
// subtest about the panels runs at [tuiPlain], where the left column holds
// `needs you`, `threads` and `projects` and nothing is cut at [tuiCardAt].

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
)

// modelPatience is how long any one real turn is given. deepseek-v4-flash
// answers a one-line question in seconds; a turn that has to reach for a tool
// takes longer, and a machine under load takes longer again.
const modelPatience = 90 * time.Second

// runPatience is how long a RUN is given, and it is not [modelPatience] because
// a run is not a turn. A `/task` on the run engine seeds a plan store, dispatches
// a worker, drives its whole loop of model calls, then commits the tree and hands
// the conversation its landing — a measured one-file brief lands in about thirty
// seconds and costs a few cents, and a machine under load takes minutes.
const runPatience = 6 * time.Minute

// The two frames this suite drives, and why each is the width it is.
const (
	// tuiPlain is an ordinary terminal: home is two columns of panels, the
	// person's on the left and the machine's on the right.
	tuiPlain = 120
	// tuiWide is past internal/tui3's homeCardMin, where a search has a card
	// beside its matches, and past homeGridThreeAt, where the panels are three
	// columns.
	tuiWide = 180
	// tuiCardAt is the search card's first column at tuiWide: the card uses
	// 56 cells and the list and gutter occupy the remaining 124. Assertions
	// below require complete card labels so a moved boundary fails visibly.
	tuiCardAt = 124
)

// tuiShortRows is a deliberately SHORT terminal, and it is a fixture rather than
// a taste: [testTaskRoomKeepsSpace] is about a key that PAGES a record, and a
// record that fits on the screen cannot page.
//
// FOURTEEN IS MEASURED AND NOT GUESSED. A landed `/task solo` draws a record of
// about seventeen rows — the state line, what it said at the end, the model and
// the bill, the branch, the two paths — and at twenty rows the whole of it fit,
// so the first passing run of that subtest could only report that there had been
// nothing to page. Fourteen is an ordinary small window, a split pane or a
// laptop with a browser over half of it, and it is short enough that the record
// runs past the bottom of the frame.
const tuiShortRows = 14

func TestTUIE2E(t *testing.T) {
	requireTmuxAndKey(t)

	t.Run("home_opens_on_launch_with_recent_sessions", testHomeShape)
	t.Run("a_real_conversation_on_the_panels_and_its_search_card", testRealConversation)
	t.Run("answer_from_home_across_two_windows", testAnswerFromHome)
	t.Run("hover_previews_the_match_under_the_pointer", testHover)
	t.Run("a_panel_folds_and_typing_sees_through_it", testFold)
	t.Run("the_projects_panel_is_the_view_by_project", testGrouped)
	t.Run("one_figure_on_every_spend_surface", testOneSpendFigure)
	t.Run("plain_launch_opens_connections_and_harnesses", testPlainLaunchConnectionsAndHarnesses)
	t.Run("a_nested_landing_asks_and_a_key_answers_it", testNestedGate)
	t.Run("a_refused_landing_is_incomplete", testRefusedLanding)
	t.Run("a_fresh_install_is_shown_the_setup", testFreshInstallSetup)
	t.Run("a_refused_task_proposal_draws_no_schema_sentence", testRefusedTaskProposal)
	t.Run("space_in_the_task_room_pages_the_card", testTaskRoomKeepsSpace)
	t.Run("TaskOnTheRunEngine", testTaskOnTheRunEngine)
	t.Run("TaskOnTheDefaultBelt", testTaskOnTheDefaultBelt)
	t.Run("foreign_skills_reach_the_conversation", testForeignSkills)
}

// testPlainLaunchConnectionsAndHarnesses is the engine-road regression: the
// ordinary launch, with no --no-host escape hatch, keeps this machine's account
// store and harness registry on the surface side of the local socket.
func testPlainLaunchConnectionsAndHarnesses(t *testing.T) {
	// The socket path includes CODEAF_HOME. Go's test directory carries this
	// whole sentence and crosses the unix-socket limit, which would make the
	// product honestly take its in-process floor and stop testing this road.
	seed := newHome(t, nil)
	home, err := os.MkdirTemp("", "afld")
	if err != nil {
		t.Fatalf("make a short state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	profile, err := os.ReadFile(filepath.Join(seed, "config.json"))
	if err != nil {
		t.Fatalf("read the copied profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), profile, 0o600); err != nil {
		t.Fatalf("write the copied profile: %v", err)
	}
	credential := map[string]any{
		"stripe": map[string]any{
			"account": "fixture account",
			"auth":    connect.AuthKey,
			"key":     "not-a-real-secret",
		},
	}
	raw, err := json.MarshalIndent(credential, "", "  ")
	if err != nil {
		t.Fatalf("encode the connection fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, connect.StoreFileName), append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write the connection fixture: %v", err)
	}
	ws := newWorkspace(t, "localdoors", false)
	r := start(t, "afe2e_local_doors", home, ws, tuiPlain, 40, "chat", "--one-model")
	statesPastTheDoor(t, r)

	r.lit("/connect")
	r.keys("Enter")
	connections := r.waitFor(20*time.Second,
		say(t, "connectFilterHint"), say(t, "connectProvidersGroup"))
	if strings.Contains(connections, say(t, "connectUnavailableWord")) {
		t.Fatalf("the plain launch lost this machine's connections:\n%s", connections)
	}
	r.lit("stripe")
	connected := r.waitFor(20*time.Second, say(t, "taskDoneGlyph")+" Stripe")
	if strings.Contains(connected, say(t, "connectUnavailableWord")) {
		t.Fatalf("the copied account is unavailable on the plain launch:\n%s", connected)
	}
	t.Logf("the local engine road opened this machine's connection panel and found its copied account:\n%s", connected)

	r.keys("Escape")
	r.keys("Escape")
	r.lit("/harness")
	r.keys("Enter")
	harnesses := r.waitFor(20*time.Second, say(t, "noHarnessWord"))
	if strings.Contains(harnesses, say(t, "harnessUnavailableWord")) {
		t.Fatalf("the plain launch lost this machine's harness registry:\n%s", harnesses)
	}
	t.Logf("the local engine road opened this machine's harness registry:\n%s", harnesses)

	// AND THE CREW PANEL, which is the same kind of door onto this machine's
	// profile: /crew opens it framed over the conversation, enter on the first
	// seat opens that seat's list on `auto`, and esc steps back out one level at
	// a time. Nothing is chosen, so nothing is written and no model is asked.
	r.keys("Escape")
	r.lit("/crew")
	r.keys("Enter")
	crew := r.waitFor(20*time.Second, say(t, "crewMainKeys"))
	t.Logf("the crew panel opened:\n%s", crew)
	r.keys("Enter")
	seats := r.waitFor(20*time.Second, say(t, "crewAutoWord"))
	t.Logf("the worker's seat list opened on auto:\n%s", seats)
	r.keys("Escape")
	r.waitFor(20*time.Second, say(t, "crewMainKeys"))
	r.keys("Escape")
	r.quit()
}

// ── 13 ──────────────────────────────────────────────────────────────────────

// testFreshInstallSetup is #322's acceptance, on the real screen: THE FRONT
// DOOR, on a machine that has never run codeaf.
//
// THE FAILURE THIS MEASURES MADE THE PRODUCT UNUSABLE ON A FRESH INSTALL.
// [app.openSetup] returned early on an empty profile directory, which is what an
// unset CODEAF_PROFILE_DIR looks like by the time it reaches the surface — so a
// person who had just installed codeaf and typed `codeaf` was never shown the
// screen that connects a provider. Every unit test of that screen named a
// profile directory first, and every person who ever tested it already had a key
// in their shell, so it was green everywhere and broken for exactly the one
// audience it exists for.
//
// SO IT IS RUN AGAINST NOTHING. [emptyHome] creates a directory and not one thing
// more, and [startFresh] takes every provider key and the profile override OUT of
// the environment rather than passing them through it. A fixture that pre-created
// a config file would hide the failure it is here to catch, because the failure
// IS emptiness being read as absence.
//
// AND IT COSTS NOTHING. There is no key on this machine, so there is no wire
// path: the setup screen makes no model call, the browser trip is never started
// (nothing presses enter), and the run is over in seconds. What it measures is
// the door and the door only.
func testFreshInstallSetup(t *testing.T) {
	home := emptyHome(t)
	ws := newWorkspace(t, "freshws", false)
	r := startFresh(t, "afe2e_fresh", home, ws, tuiPlain, 40)

	// ONE FRAME, THE WHOLE DOOR: the title that says where in the flow this is,
	// the heading of the step, and the sentence under it that says what pressing
	// enter will and will not do.
	screen := r.waitFor(20*time.Second,
		say(t, "setupTitleWord"), say(t, "setupProviderHeading"))
	// OpenRouter is initially selected; Enter must open its connection step.
	r.keys("Enter")
	screen = r.waitFor(20*time.Second, say(t, "setupConnectHeading"), say(t, "setupConnectSentence"))
	t.Logf("a fresh install, launched the ordinary way, is shown the door:\n%s", screen)

	// AND IT IS ASKING FOR BOTH. A machine with nothing on it has answered no
	// part of the setup, so the count is the count of what is missing — and
	// internal/tui3's [setupStepsFor] builds at most two: the provider key, and
	// the crew-and-spending step that carries the rest. It counted three before
	// those were folded together, and a needle nobody moved would have waited
	// twenty seconds for a title this door has stopped drawing.
	if !strings.Contains(screen, say(t, "setupTitleWord")+" · 1 of 2") {
		t.Errorf("the title does not count both missing answers on a machine with nothing on it:\n%s", screen)
	}

	// AND ITS EXIT IS REAL TOO. `esc` says not now, and the conversation under it
	// then names the next direct road rather than leaving a person on an empty
	// screen wondering what happened — which is the other half of a front door.
	r.keys("Escape")
	after := r.waitFor(20*time.Second, say(t, "setupNotConnectedNote"))
	t.Logf("and esc leaves a conversation that says what is still missing:\n%s", after)
}

// ── 11 ──────────────────────────────────────────────────────────────────────

// testNestedGate is issue #268's acceptance, on the real screen: a part one
// level down that nobody could check ASKS, in one frame, and a key answers it.
//
// THE MEASURED FAILURE IS WHY IT IS HERE. A nested part landed needing somebody
// to decide, and a fifteen-second capture of the whole run shows no answers row
// for it, ever — the card was written for ROOT nodes only, and the roster filed
// the node under `done` while its parent still ran. So the gate expired without
// a person ever being able to see it, let alone answer it.
//
// AND IT COSTS NOTHING. The family is seeded as the graph the process left
// behind ([seedDecidedFamily]); the surface replays it on attach. No model is
// asked anything, so what this subtest measures is the surface and the engine's
// settle door and nothing else — which is exactly what went wrong.
func testNestedGate(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "ask"})
	ws := newWorkspace(t, "gatews", false)
	seedDir := seedDecidedFamily(t, home, ws)
	r := start(t, "afe2e_gate", home, ws, tuiWide, 40, "chat", "--one-model")

	// WHICHEVER DOOR THE LAUNCH TOOK, and esc until it is actually gone. A state
	// root built a minute ago stands on the SETUP however complete the profile it
	// copied is — the marker that says the setup has been seen is a file in that
	// root — and the setup is several steps, so one esc leaves the one under it.
	statesPastTheDoor(t, r)

	// ONE FRAME, BOTH HALVES. The roster's `?` and the reason for a node waiting
	// on a person, and the answers row on the card — all on screen at once, which
	// is the whole of what "answerable" means here. The compact card no longer
	// repeats the tier word beside the question.
	screen := r.waitFor(20*time.Second,
		say(t, "settleAskWord"), say(t, "settleAccept"), say(t, "settleTellIt"),
		say(t, "unverifiedGlyph"))
	t.Logf("a nested landing asking on every surface:\n%s", screen)
	if !strings.Contains(screen, "Port the parser") {
		t.Fatalf("the nested part is not named on the screen:\n%s", screen)
	}

	// The landing key accepts the nested task once. Finished cards now start
	// folded, so the visible proof is the batch changing from one done and one
	// your call to two done; the durable receipt names who spent the answer.
	pressLandingKey(r, "a")
	settled := r.waitFor(20*time.Second,
		say(t, "taskDoneGlyph")+" 2"+say(t, "doneRollupWord"))
	waitForLandingReceipt(t, seedDir, say(t, "settleTookLine"))
	t.Logf("the nested accept made both tasks done and wrote its receipt:\n%s", settled)
	r.quit()
}

// testRefusedLanding is the other answer to the same real engine gate as
// [testNestedGate]. The graph is deterministic: the person says the work is not
// right, the engine keeps its failed plus refused state, and the built surface
// must call that result incomplete rather than turning the useful finding into
// a generic failure.
// ── AND IT IS THE ONE THAT CAUGHT #706, WHICH IS WORTH KEEPING WRITTEN DOWN ──
//
// This subtest waited twenty seconds for `[n] not right` and never saw it: the
// card drew its tier and its reason and NO ANSWERS ROW AT ALL. The fixture in
// taskstates_e2e_test.go, which is the same state, the same merge and the same
// one changed file, drew all four chips in the same run — so for a while this
// read as a difference between two fixtures that do not differ.
//
// THE DIFFERENCE WAS THE LENGTH OF THE TEMPORARY HOME. A conversation opens
// against an engine host where one can be reached, and a host is reachable only
// where its socket path fits (internal/enginehost's socketLimit) — so the
// subtest with the shorter name got a host and the one with the longer name fell
// back to the in-process engine. internal/remote's agent had none of the four
// doors that decide a landing, the surface's assertion failed, and the absence
// law removed the row: a control with nothing behind it is left off rather than
// offered and failing. Which is why it also passed on every laptop, where the
// temporary root is long enough that neither subtest ever reaches a host.
//
// The doors cross now (internal/remote's tasksettle.go), and this is the subtest
// that says so on a real screen.
func testRefusedLanding(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "ask"})
	ws := newWorkspace(t, "refusedgatews", false)
	seedDir := seedUndecidedRoot(t, home, ws)
	r := start(t, "afe2e_refused_gate", home, ws, tuiWide, 40)

	statesPastTheDoor(t, r)
	r.waitFor(20*time.Second, say(t, "settleAskWord"), say(t, "settleNotRight"))
	pressLandingKey(r, "n")
	screen := r.waitFor(20*time.Second, say(t, "taskIncompleteWord"))
	waitForLandingReceipt(t, seedDir, say(t, "settleNotRightLine"))
	t.Logf("a refused landing keeps its reason and says incomplete:\n%s", screen)
	if strings.Contains(screen, say(t, "taskFailedWord")) {
		t.Errorf("the refused landing still says failed:\n%s", screen)
	}
	r.quit()
}

// pressLandingKey clears the first-run greeting before spending one answer.
// Repeating an answer while its receipt is folded can start unrelated work.
func pressLandingKey(r *rig, key string) {
	r.lit(".")
	r.keys("C-u")
	time.Sleep(400 * time.Millisecond)
	r.lit(key)
}

// waitForLandingReceipt reads the conversation's durable answer while the
// completion card is folded. The receipt is written before the resumed model
// finishes, so this does not depend on how long that model takes to reply.
func waitForLandingReceipt(t *testing.T, seedDir, receipt string) {
	t.Helper()
	path := filepath.Join(seedDir, "transcript.jsonl")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), receipt) {
			return
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("the accepted landing wrote no %q receipt in %s", receipt, path)
}

// ── 1 ───────────────────────────────────────────────────────────────────────

// testHomeShape reads home at rest — the `activity` list of conversations, and
// the `projects`, `spend`, `since you left` and `automations` panels — and the
// seven places on the wordmark row, then drives the command and double-space
// routes back to it.
func testHomeShape(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "shapews", false)
	r := start(t, "afe2e_shape", home, ws, tuiPlain, 40)

	screen := r.waitFor(20*time.Second, say(t, "placeRestWord"), say(t, "homePanelProjects"))
	t.Logf("home greeted on launch:\n%s", screen)

	// EVERY PANEL IS ON THE PAGE. Forty rows is room for all five at their
	// floors in two columns, so a heading missing here is a panel the grid lost
	// rather than one a short frame squeezed out. A heading is read where a
	// heading stands — opening its column ([headingColumn]) — because `spend`
	// and `activity` are also words of the bar above them.
	for _, name := range []string{"homePanelProjects",
		"homePanelRunning", "switcherSinceLeft", "homePanelSpend", "homePanelNext"} {
		if panelBlock(screen, say(t, name)) == "" {
			t.Errorf("home has no %q panel:\n%s", say(t, name), screen)
		}
	}
	if strings.Contains(screen, say(t, "homeRunningWhisper")) {
		t.Errorf("populated sessions still show the empty-panel hint:\n%s", screen)
	}
	// THE BAR IS SEVEN PLACES on the wordmark row, each by its label, in
	// placeBarOrder's order. Automations is the one place off it: it is reached
	// by command, by alt+7 and by home's `automations` heading, and it joins the
	// bar only while somebody stands in it.
	want := []string{say(t, "barHomeWord"), say(t, "barChatsWord"), say(t, "barTeamsWord"),
		say(t, "barSessionsWord"), say(t, "barMemoryWord"), say(t, "barSpendWord"), say(t, "barSettingsWord")}
	if row, ok := barInOrder(screen, want); !ok {
		t.Errorf("the tab bar reads %q, want the labels %q in that order:\n%s", row, want, screen)
	}
	if row, _ := barInOrder(screen, want); strings.Contains(row, strings.ToUpper(say(t, "homePanelNext")[:1])+say(t, "homePanelNext")[1:]) {
		t.Errorf("the tab bar carries the automations place while nobody stands in it: %q", row)
	}

	// Home includes recent saved conversations, even before a tab opens them.
	for _, title := range []string{"Seed Alpha", "Seed Beta", "Seed Gamma", "Seed Delta", "Seed Epsilon"} {
		if !strings.Contains(screen, title) {
			t.Errorf("recent history is missing from Home: %q", title)
		}
	}
	// Home keeps the command door but omits the ordinary navigation hints.
	if !strings.Contains(screen, say(t, "microcopy")) || strings.Contains(screen, "↑↓ pick") || strings.Contains(screen, "enter open ·") {
		t.Errorf("home's foot has the wrong controls:\n%s", screen)
	}

	// THE PANELS NEVER TOUCH THE RULE ABOVE THE BOX (internal/tui3's home_test.go
	// pins it at every height): the row above the foot's rule is blank whatever
	// the columns did, because the air a tall frame has left sits under them.
	lines := r.lines()
	// THE RULE ABOVE HOME'S BOX CARRIES WORDS NOW — `─ → new conversation in
	// … · model ──── alt+w folder · alt+o model ─` (internal/tui3's
	// homedraft.go, 2026-09-09) — so the foot is the last row that begins as a
	// rule, whether or not it runs on as dashes; a finder that wanted four
	// dashes walked up to the header's rule and read the nav row as the list.
	foot := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if row := strings.TrimSpace(lines[i]); strings.HasPrefix(row, "────") || strings.HasPrefix(row, "─ ") {
			foot = i
			break
		}
	}
	if foot < 1 {
		t.Fatalf("no foot rule on the screen:\n%s", screen)
	}
	if got := strings.TrimSpace(lines[foot-1]); got != "" {
		t.Errorf("the panels touch the foot: the row above the rule is %q", got)
	}
	t.Logf("padding row above the foot rule (row %d) is blank", foot-1)

	// esc closes home into the conversation the launch loaded, and the rule over
	// that conversation's box names both doors back.
	r.keys("Escape")
	closed := r.waitFor(15*time.Second, say(t, "homeDoorWord"), say(t, "microcopy"))
	t.Logf("esc closed home into the conversation the launch loaded:\n%s", closed)
	if strings.Contains(closed, say(t, "placeRestWord")) {
		t.Errorf("esc did not close home:\n%s", closed)
	}
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	back := r.waitFor(15*time.Second, say(t, "placeRestWord"), "Seed Alpha")
	t.Logf("/home reopened it:\n%s", back)

	// And two spaces on an empty box is the other door.
	r.keys("Escape")
	time.Sleep(1200 * time.Millisecond)
	r.keys("Space")
	r.keys("Space")
	gesture := r.waitFor(15*time.Second, say(t, "placeRestWord"))
	t.Logf("space space opened home:\n%s", gesture)
}

// ── 2 ───────────────────────────────────────────────────────────────────────

// testRealConversation asks the model one question and then reads what home
// says about that conversation: at rest on the panels, and on the one card left,
// the one beside a search — which is why this one runs at [tuiWide].
//
// WHAT WENT, AND WHERE IT WENT. The resting card is gone (DESIGN.md §1, "what is
// retired"): the person's last words are the line under the `here` row, and the
// repository clause is on the project's row of `projects`. The card beside a
// search is the band registry's card (internal/tui3's homeCardRows), which was
// never the resting card: its repository is a band of its own, its facts are a
// band, and its last line is the keys legend ending in `→ more`. So the card is
// read in ITS spelling, and the resting card's `→ verbs` line and one-line
// `path · repo · here` address are not waited for anywhere.
func testRealConversation(t *testing.T) {
	home := newHome(t, nil)
	// The card caps its width, so widening the terminal cannot make an
	// arbitrary t.TempDir path fit beside repository facts. Give this one
	// fixture a short, unique address, and remove it with the test.
	short, err := os.MkdirTemp("/tmp", "hc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	ws, err := filepath.EvalSymlinks(short)
	if err != nil {
		t.Fatal(err)
	}
	ws = workspaceAt(t, ws, true)
	r := start(t, "afe2e_talk", home, ws, tuiWide, 40, "chat", "--one-model", "--no-host")

	r.lit("what is 2+2? Spell the answer as an English word.")
	r.keys("Enter")
	hit, screen := r.waitForAny(modelPatience, "\n4", " 4\n", "four", "Four")
	t.Logf("the model answered (%q):\n%s", hit, screen)

	// The repository must agree with git, so the count is taken at the moment of
	// the assertion rather than assumed. TWO SURFACES JOIN ITS CLAUSES TWO WAYS,
	// and both are the product: the projects panel writes `main, 1 file dirty` as
	// one clause about one repository on a row that already uses ` · ` between
	// its facts (homepanel_projects.go), and the search card's repository band
	// keeps the reading's own ` · ` so a narrow card drops a clause whole.
	dirty := dirtyFiles(t, ws)
	count := fmt.Sprintf("%d %s dirty", len(dirty), plural("file", len(dirty)))
	want, cardWant := "main, "+count, "main · "+count
	// AND WHAT MADE IT DIRTY. The test changed exactly one tracked file; a
	// second entry is something the product itself dropped in the person's
	// working directory, which is worth naming rather than absorbing.
	for _, name := range dirty {
		if !strings.HasSuffix(name, "README.md") {
			t.Logf("FINDING: the run left %q in the person's workspace and the repository clause counts it", name)
		}
	}

	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	// THE PANELS, AT REST. This window's conversation is the `here` row of
	// `threads` with the person's own last words under it, and its
	// folder is the first row of `projects` with the repository clause beside
	// it. The reading of `git status` arrives a beat after the first frame.
	panels := r.waitFor(20*time.Second, say(t, "placeRestWord"), say(t, "homePanelProjects"), want)
	t.Logf("home at rest, with this conversation on the panels:\n%s", panels)
	// The current tab starts the unheaded list; its last words are in the middle.
	if !strings.Contains(panels, say(t, "homeHereWord")) || !strings.Contains(panels, "what is 2+2?") {
		t.Errorf("the current tab lost its description:\n%s", panels)
	}
	if strings.Contains(rightPane(panels), say(t, "homeCardMoreWord")) {
		t.Errorf("a card is standing beside the panels at rest:\n%s", panels)
	}

	// THE CARD BESIDE A SEARCH. Typing the project's name keeps every
	// conversation in it, and the cursor rests on the action row, whose card is
	// empty — one `↑` is `ask here`, the second is the match.
	r.lit(filepath.Base(ws))
	time.Sleep(700 * time.Millisecond)
	r.keys("Up")
	r.keys("Up")
	card := r.waitFor(20*time.Second, say(t, "homeCardMoreWord"), say(t, "homeFactsActive"), cardWant)
	t.Logf("the card beside the match:\n%s", card)
	pane := rightPane(card)
	if !strings.Contains(pane, cardWant) {
		t.Errorf("the card's repository band does not read %q — it reads %q (git says %v)",
			cardWant, firstMatch(pane, "main"), dirty)
	}
	// The facts line. `last active` is always true of a conversation somebody
	// just spoke in; `spent` is drawn from the whole rollup (home.go's
	// homeFacts), so it is recorded rather than demanded.
	if strings.Contains(pane, "spent $") {
		t.Logf("the facts line drew a `spent $…` clause: %s", firstMatch(pane, "spent $"))
	} else {
		t.Logf("FINDING: no `spent $…` clause on a conversation that really spent money. "+
			"The facts line reads: %s", firstMatch(pane, say(t, "homeFactsActive")))
	}
	// The same cached reading on the NARROWEST card (internal/tui3's
	// homeCardMin) gives way whole or not at all: a band drops a clause from its
	// end rather than cutting one, so a repository line with an ellipsis on it is
	// a fact a person cannot read and cannot tell is incomplete. Then it returns
	// whole when the person widens the terminal again.
	r.resize(136, 40)
	narrow := r.waitFor(10*time.Second, say(t, "homeFactsActive"), say(t, "homeCardMoreWord"))
	if line := firstMatch(narrow, "main"); strings.Contains(line, "…") {
		t.Errorf("the narrow card cut its repository band instead of dropping a clause whole: %q\n%s", line, narrow)
	}
	r.resize(tuiWide, 40)
	r.waitFor(10*time.Second, cardWant, say(t, "homeFactsActive"), say(t, "homeCardMoreWord"))
}

// walkTo steps the cursor along the list, one press of `key` at a time, until
// the hint under the box says it is standing on the row this test wants, and
// answers whether it got there.
//
// THE HINT IS THE ORACLE AND NOT THE ROW. The switcher's rows carry no `›` lead
// of their own — the band under the cursor is the whole of the selection — so
// the one line on the frame that changes with the cursor is the hint
// (internal/tui3's homeHint), and reading it is how this suite knows where the
// keyboard is standing without asking the product to draw a mark for the test's
// benefit.
func walkTo(r *rig, hint, key string) bool {
	for i := 0; i < 14; i++ {
		if strings.Contains(r.capture(), hint) {
			return true
		}
		r.keys(key)
		time.Sleep(400 * time.Millisecond)
	}
	return strings.Contains(r.capture(), hint)
}

// openHome opens the screen with the slash command, which is the door that
// works whatever the machine holds.
func openHome(t *testing.T, r *rig) {
	t.Helper()
	r.lit("/home")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	r.waitFor(20*time.Second, say(t, "placeRestWord"))
}

// ── 5 ───────────────────────────────────────────────────────────────────────

// testAnswerFromHome stops one window on a consent card and answers it from
// another window's home screen.
func testAnswerFromHome(t *testing.T) {
	home := newHome(t, map[string]any{
		"tools.approvalMode": "prompt",
		// The countdown that denies on silence is off, or the card is gone
		// before the second window has drawn it. Zero is the setting's own
		// "waits forever" (config's settings.go).
		"approval.timeout_seconds": 0,
	})
	ws := newWorkspace(t, "consentws", false)
	a := start(t, "afe2e_a", home, ws, tuiPlain, 40)

	a.lit("run `ls -la` with bash, nothing else")
	a.keys("Enter")
	asked := a.waitFor(modelPatience, say(t, "consentAskWord"))
	t.Logf("window A stopped on a consent card:\n%s", asked)

	// WINDOW B IS A SECOND PROJECT AND NOT A SECOND TERMINAL IN THE SAME ONE.
	// Since #653 this workspace's engine is the ordinary road, and two windows
	// opened in one folder SIT DOWN IN THE SAME CONVERSATION — the second one
	// draws A's consent card itself under `another window is on this conversation
	// — typing is here now`, which is the feature working and not this scenario.
	// What this subtest is about is the other thing entirely: a question raised in
	// a conversation somebody is NOT in, reaching them on home and being answered
	// from there. So B opens its own project, and A's conversation is a row on
	// B's list like any other.
	b := start(t, "afe2e_b", home, newWorkspace(t, "consentws-b", false), tuiPlain, 40)
	// A's QUESTION ARRIVES ON ITS EXISTING CONVERSATION ROW, on home's
	// `activity` list. Home shows the question and answers in that row's
	// description while it is selected. A digit still answers the top question
	// from anywhere on home.
	b.waitFor(40*time.Second, say(t, "questionOtherWindowWord"))
	if !walkTo(b, say(t, "answersAllowOnce"), "Up") {
		t.Fatalf("could not select the question on its session row:\n%s", b.capture())
	}
	row := b.capture()
	t.Logf("window B's home offers the answers to the question A is stopped on:\n%s", row)
	if !strings.Contains(row, "1 ") {
		t.Errorf("home's answers are missing the first chip's key:\n%s", row)
	}
	t.Logf("the chips home offered: %s", firstMatch(row, say(t, "answersAllowOnce")))

	// AND THE ROW SAYS THE GATE'S OWN SENTENCE, WHOLE AND UNADORNED.
	// internal/session's consent.go writes one line for exactly this purpose —
	// the line another window may answer this from — and internal/tui3's switcher
	// used to prefix `wants to ` onto it, from the days when the engine handed
	// over a bare action. What this suite read on a real screen was
	// `consentws wants to needs your ok to run bash`.
	if !strings.Contains(row, say(t, "consentRowLine")) {
		t.Errorf("home's row does not carry the gate's own sentence %q:\n%s", say(t, "consentRowLine"), row)
	}
	if strings.Contains(row, "wants to "+say(t, "consentRowLine")) {
		t.Errorf("home's row wrote its own grammar around the gate's sentence:\n%s", firstMatch(row, say(t, "consentRowLine")))
	}

	// THE QUESTION BELONGS TO THE CONVERSATION'S OWN ROW under `activity`, and
	// is not drawn a second time as a row of its own.
	if !strings.Contains(row, say(t, "homePanelRunning")) ||
		strings.Count(row, say(t, "consentRowLine")) != 1 {
		t.Errorf("the selected session does not carry exactly one question:\n%s", row)
	}

	// AND THE PULSE INSIDE A CHAT COUNTS IT. On home the top line is the budget
	// and the clock, because the panels are the counts; in a conversation it keeps
	// `1 want you` (DESIGN §1 law 11), read on the chat's own ten-second beat. So
	// B steps into its own conversation, reads its head, and comes back.
	b.keys("Escape")
	inChat := b.waitFor(30*time.Second, say(t, "homeDoorWord"), say(t, "pulseWantWord"))
	t.Logf("window B's own conversation counts the question on its top line:\n%s", firstMatch(inChat, say(t, "pulseWantWord")))
	openHome(t, b)
	if !walkTo(b, say(t, "answersAllowOnce"), "Up") {
		t.Fatalf("could not select the question after reopening home:\n%s", b.capture())
	}
	b.keys("Down")

	b.lit("1")
	time.Sleep(1500 * time.Millisecond)
	t.Logf("after pressing 1 in window B:\n%s", b.capture())

	// Window A picks the answer up on its own presence heartbeat, a second or two
	// later, and runs the call.
	//
	// THE PROOF IS THE JOURNAL AND NOT THE SCREEN. A gate that was never answered
	// blocks the turn forever ([approval.timeout_seconds] is zero here), so the
	// only thing that can put the command's output in the transcript is the
	// answer this test gave from another window. The screen cannot carry the
	// claim on its own: the model is free to reach for bash a second time, and a
	// window drawing a SECOND question says nothing about the first — which is
	// exactly what one run did, with the first call's output already on the page.
	ran := ""
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) && ran == "" {
		for path, raw := range sessionTranscripts(t, home) {
			if strings.Contains(raw, "README.md") && strings.Contains(raw, "ls -la") {
				ran = path
				break
			}
		}
		if ran == "" {
			time.Sleep(time.Second)
		}
	}
	t.Logf("window A, after the answer came in from home:\n%s", a.capture())
	if ran == "" {
		t.Errorf("no transcript holds the output of the command home allowed:\n%s", a.capture())
	} else {
		t.Logf("the command really ran, and its output is in %s", ran)
	}
}

// ── 6 ───────────────────────────────────────────────────────────────────────

// testHover drives the pointer over the matches of a search with the SGR motion
// reports the all-motion mode asks for, and reads the card beside them. It runs
// at [tuiWide] because below that width there is no card for a hover to move.
//
// IT TYPES FIRST BECAUSE THE RESTING HOME HAS NO CARD (DESIGN.md §1, "what is
// retired"): at rest the pointer only lights the row it is on, and the one card
// left is the one beside a search, about the match under the pointer while it is
// on one and the cursor's otherwise.
func testHover(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "hoverws", false)
	r := start(t, "afe2e_hover", home, ws, tuiWide, 40)
	r.waitFor(25*time.Second, say(t, "placeRestWord"), "Seed Beta")

	// The seeds share a word, so typing it lists all three; the cursor rests on
	// the action row, whose card is empty because that chat does not exist yet.
	r.lit("Seed")
	screen := r.waitFor(15*time.Second, say(t, "homeStartWord"), "Seed Beta")
	rows := r.lines()
	target := -1
	for i, line := range rows {
		if strings.Contains(line, "Seed Beta") {
			target = i + 1 // capture-pane is 0-based here, the mouse report is 1-based
			break
		}
	}
	if target < 0 {
		t.Fatalf("no `Seed Beta` match to hover:\n%s", screen)
	}
	before := rightPane(screen)
	t.Logf("before hovering, the card is about the cursor's row:\n%s", before)

	r.mouseTo(10, target)
	time.Sleep(1500 * time.Millisecond)
	hovered := r.capture()
	if !strings.Contains(rightPane(hovered), "Seed Beta") {
		t.Errorf("hovering `Seed Beta` did not move the card onto it:\n%s", hovered)
	} else {
		t.Logf("the pointer previews the match under it:\n%s", hovered)
	}

	// Off the list, into the card's own half: the card goes back to the
	// cursor's row.
	r.mouseTo(tuiWide-4, target)
	time.Sleep(1500 * time.Millisecond)
	off := rightPane(r.capture())
	if strings.Contains(off, "Seed Beta") && !strings.Contains(before, "Seed Beta") {
		t.Errorf("moving the pointer off the list left the card on the hovered match:\n%s", r.capture())
	}
	t.Logf("pointer off the list, the card is the cursor's again:\n%s", r.capture())
}

// ── 7 ───────────────────────────────────────────────────────────────────────

// testFold proves saved history remains searchable without occupying open tabs.
func testFold(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j",
		"k", "l", "m", "n", "o", "p", "q", "r", "s", "t",
	} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "foldws", false)
	r := start(t, "afe2e_fold", home, ws, tuiWide, 20)

	screen := r.waitFor(25*time.Second, say(t, "placeRestWord"))
	if strings.Contains(screen, "Seed T") {
		t.Errorf("unopened history appears on Home:\n%s", screen)
	}
	// AND TYPING SEES STRAIGHT THROUGH IT: a search matches every conversation on
	// the machine, including the ones no panel is drawing.
	r.lit("Seed T")
	found := r.waitFor(15*time.Second, say(t, "homeStartWord"))
	deadline := time.Now().Add(15 * time.Second)
	for matchRow(found, "Seed T") == "" && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		found = r.capture()
	}
	if row := matchRow(found, "Seed T"); row == "" {
		t.Errorf("typing did not find the conversation behind the fold:\n%s", found)
	} else {
		t.Logf("typing found the row behind the fold: %q", row)
	}
	r.keys("Escape")
	time.Sleep(1500 * time.Millisecond)
	back := r.capture()
	if strings.Contains(back, "Seed T") || !strings.Contains(back, say(t, "placeRestWord")) {
		t.Errorf("clearing search did not restore the tab list:\n%s", back)
	}
}

// foldCounts is the shape of a shut fold as a person reads it off the frame: a
// count, the word, and nothing after it but the row's own padding.
var foldCounts = regexp.MustCompile(`\b\d+ more\s*$`)

// ── 9 ───────────────────────────────────────────────────────────────────────

// testGrouped reads the machine's work arranged by project, which is the
// `projects` panel now, and checks that `alt+g` — the key that used to group the
// flat list — does nothing.
//
// THE PROJECTS PANEL IS THE VIEW BY PROJECT (DESIGN.md §3 G4): every folder with
// a conversation in it, this window's own first, each row its path, its counts
// and its repository. It runs at [tuiPlain], which is two columns, and the panel
// is in the RIGHT one: #1046 pinned `projects` and `spend` to the top of the
// rail whatever they hold, so [panelBlock] finds the panel by its heading rather
// than being told which half of the screen to read.
func testGrouped(t *testing.T) {
	home := newHome(t, nil)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		seedProject(t, home, name, i, time.Duration(10*(i+1))*time.Minute)
	}
	ws := newWorkspace(t, "groupws", false)
	r := start(t, "afe2e_group", home, ws, tuiPlain, 40)
	screen := r.waitFor(25*time.Second, say(t, "placeRestWord"), say(t, "homePanelProjects"), "Seed Beta")
	t.Logf("home with three seeded projects:\n%s", screen)

	projects := panelBlock(screen, say(t, "homePanelProjects"))
	// Paths give up their right end to the chat count in a narrow rail, so the
	// full folder names are not visible. The three seeded paths remain distinct
	// through /al, /be and /ga, and every row still says it owns one chat.
	lines := strings.Split(strings.TrimSpace(projects), "\n")
	if len(lines) != 5 || strings.Count(projects, "1 chat") != 4 {
		t.Errorf("the `projects` panel does not hold four one-chat folders:\n%s", projects)
	}
	for _, name := range []string{"/al", "/be", "/ga"} {
		if !strings.Contains(projects, name) {
			t.Errorf("the `projects` panel has no visible row for %q:\n%s", name, projects)
		}
	}
	// This window's git folder is first, ahead of the three seeded paths.
	if len(lines) > 1 && (!strings.Contains(lines[1], "TestTUIE2Ethe_proj") || !strings.Contains(lines[1], "main")) {
		t.Errorf("this window's folder is not first on `projects`:\n%s", projects)
	}

	// AND alt+g IS UNBOUND ON HOME: there is no list left to group. It arrives
	// as ESC g, which is what every terminal sends for it, and nothing moves.
	r.lit("\x1bg")
	time.Sleep(1500 * time.Millisecond)
	after := r.capture()
	if headingRow(after, "alpha") {
		t.Errorf("alt+g drew a project heading on home:\n%s", after)
	}
	if !strings.Contains(after, say(t, "homePanelProjects")) {
		t.Errorf("alt+g took the panels away:\n%s", after)
	}
}

// matchRow is the first line naming a conversation as a MATCH of a search —
// not the box the words were typed into, and not the two rows that quote them
// back (`ask here: "…"`, `start a new conversation: "…"`), each of which holds
// the typed words whether or not anything matched.
func matchRow(screen, title string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, title) && !strings.Contains(line, "\""+title) && !strings.Contains(line, "› "+title) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// panelBlock is one home panel wherever the grid put it: from its heading down
// to the first row that is blank in that panel's own columns, every line cut to
// those columns — the panel's rows and their descriptions, and nothing from the
// panel beside it.
//
// IT TAKES NO EDGE BECAUSE A PANEL'S COLUMN IS NO LONGER A FACT ABOUT THE PANEL
// (#1046). A panel with rows in it stands in the field, filled from the top left
// corner down; an empty one stands in the rail, the last column, flush with the
// right edge — and `projects` and `spend` are pinned to the top of that rail
// whatever they hold. So `projects` is on the RIGHT of a two-column home and
// `running` changes sides as work starts and stops, which is why this reads the
// heading's own position rather than being told a fraction of the width. The
// caller that told it `tuiPlain/2` read sixty blank cells and reported an empty
// panel for four rows that were plainly on the screen.
//
// THE BOUNDS COME OFF THE HEADING'S OWN ROW. The left one is the column the
// heading starts in. The right one is where the NEXT column's heading starts on
// that same row, because the gutter between two columns is several cells wide
// while a heading's own explainer is one space from it — `projects · folders
// you've opened` is one heading and not two. A heading with nothing to its right
// owns the rest of the row, which is what a rail panel wants.
func panelBlock(screen, heading string) string {
	var b strings.Builder
	left, right, in := 0, 0, false
	for _, line := range strings.Split(screen, "\n") {
		runes := []rune(line)
		if !in {
			at := headingColumn(runes, heading)
			if at < 0 {
				continue
			}
			// capture-pane trims trailing spaces from the heading row, so its
			// length is not the width of the panel rows underneath it.
			in, left, right = true, at, len(screen)
			if next := nextColumn(runes, at+len([]rune(heading))); next > 0 {
				right = next
			}
		}
		cut := ""
		if left < len(runes) {
			cut = strings.TrimRight(string(runes[left:min(right, len(runes))]), " ")
		}
		if cut == "" && b.Len() > 0 {
			break
		}
		b.WriteString(cut)
		b.WriteString("\n")
	}
	return b.String()
}

// panelGutter is the narrowest run of spaces that can only be the gap between
// two columns. One space is what a heading's own words are separated by.
const panelGutter = 3

// headingColumn is the column a panel heading opens, or -1 where this row does
// not carry it. A HEADING OPENS ITS COLUMN, so what stands left of it is either
// the frame's own margin or the gutter — which is how the word `spend` as a
// panel heading is told from the same word inside a sentence.
func headingColumn(runes []rune, heading string) int {
	want := []rune(heading)
	for i := 0; i+len(want) <= len(runes); i++ {
		if string(runes[i:i+len(want)]) != heading {
			continue
		}
		if strings.TrimSpace(string(runes[:i])) == "" {
			return i
		}
		if i >= panelGutter && strings.TrimSpace(string(runes[i-panelGutter:i])) == "" {
			return i
		}
	}
	return -1
}

// nextColumn is the column the next panel begins in, reading right from `from`,
// or -1 where nothing more stands on this row.
func nextColumn(runes []rune, from int) int {
	spaces := 0
	for i := from; i < len(runes); i++ {
		if runes[i] == ' ' {
			spaces++
			continue
		}
		if spaces >= panelGutter {
			return i
		}
		spaces = 0
	}
	return -1
}

// barInOrder finds the wordmark row — the first line carrying the first label
// — and reports whether every label stands on it in the order given, each
// after the one before. It answers the row for the log either way.
//
// IT READS LABELS AND NOT FIELDS. `AI teams` is one place in two words, so a
// reader that split the row on spaces would count a place that is not there;
// the wordmark before the first label and the pulse after the last are simply
// not asked about.
func barInOrder(screen string, labels []string) (string, bool) {
	if len(labels) == 0 {
		return "", false
	}
	for _, line := range strings.Split(screen, "\n") {
		if !strings.Contains(line, labels[0]) {
			continue
		}
		at := 0
		for _, label := range labels {
			found := strings.Index(line[at:], label)
			if found < 0 {
				return strings.TrimSpace(line), false
			}
			at += found + len(label)
		}
		return strings.TrimSpace(line), true
	}
	return "", false
}

// headingRow answers whether a project's name stands ALONE on a line of the
// LIST, which is what a grouping heading is — the conversations under it carry
// their own state mark and their own tail, so a name with anything beside it is
// a row and not a heading.
//
// ONLY THE LIST HALF IS READ, because the card shares these screen rows and a
// heading with the card's title beside it is still a heading.
func headingRow(screen, name string) bool {
	for _, line := range strings.Split(screen, "\n") {
		runes := []rune(line)
		if len(runes) > tuiCardAt {
			runes = runes[:tuiCardAt]
		}
		if strings.TrimSpace(string(runes)) == name {
			return true
		}
	}
	return false
}

// dirtyFiles is what `git status --porcelain` says about the workspace, which
// is the reading the card's place line is derived from (homeband_repo.go runs
// the same command in porcelain v2).
func dirtyFiles(t *testing.T, ws string) []string {
	t.Helper()
	command := exec.Command("git", "-C", ws, "status", "--porcelain")
	out, err := command.Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		// The porcelain line is two status letters, a space, and the path. The
		// leading letter is often a space itself, so the split is on fields
		// from the right rather than on a fixed offset.
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[len(fields)-1])
		}
	}
	return names
}

// plural is the spelling rule this suite quotes back at the product.
func plural(unit string, n int) string {
	if n == 1 {
		return unit
	}
	return unit + "s"
}

// ── reading the screen ──────────────────────────────────────────────────────

// rightPane is everything from the gutter rightwards at [tuiWide], which is
// where home draws the card. See [tuiCardAt] for where that column comes from.
func rightPane(screen string) string {
	var b strings.Builder
	for _, line := range strings.Split(screen, "\n") {
		runes := []rune(line)
		if len(runes) <= tuiCardAt {
			b.WriteString("\n")
			continue
		}
		b.WriteString(strings.TrimRight(string(runes[tuiCardAt:]), " "))
		b.WriteString("\n")
	}
	return b.String()
}

// firstMatch is the first line holding a substring, for a log line that quotes
// the row rather than the whole screen.
func firstMatch(screen, sub string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, sub) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// ── 10 ──────────────────────────────────────────────────────────────────────

// testOneSpendFigure is issue #269 end to end: run a turn, press escape in the
// middle of it, and read the money back on both spend surfaces. They must say
// the same thing.
//
// WHY AN INTERRUPTED TURN AND NOT AN ORDINARY ONE. Money is banked from the
// provider's own usage block as each call is decoded, and the machine's ledger
// used to be written only when a turn SEALED — so a turn nobody let finish was
// money the status line had and the file never got. On the measured chat that
// was $0.087 missing from `/spend` and from Settings→Spending, with the status
// line reading the truth beside them. Escape is the cheapest way to make a real
// binary produce exactly that state.
//
// WHAT IS ASSERTED IS A STRING AND NOT A NUMBER. Two surfaces agreeing to
// within a cent is two surfaces disagreeing; the point of the fix is that they
// are one reading of one total, so they are compared as the characters a person
// reads off the screen.
func testOneSpendFigure(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "spendws", false)
	r := start(t, "afe2e_spend", home, ws, tuiPlain, 40)

	// A machine with no conversations on it opens straight into one, which is
	// the state this scenario wants: one conversation, so the machine's day and
	// this conversation's total are the same money read two ways. The wait is
	// on the greeting's own starter line, which that first conversation stands
	// on. It was on the workspace's name in the status line, which a frame of
	// this width now cuts from the foot's `project:` clause.
	r.waitFor(20*time.Second, say(t, "starterTaskWord"))

	// A question with a long answer, so there is a middle to interrupt.
	r.lit("count slowly from one to two hundred, one number per line")
	time.Sleep(600 * time.Millisecond)
	r.keys("Enter")
	// THE CONVERSATION'S OWN FOOT SAYS A TURN IS IN FLIGHT: `esc interrupt`
	// stands where the home gesture stands at rest. It is glimpsed rather than
	// waited for, because a turn that finished first is a fast reply and the
	// figures below must agree all the same.
	if caught, ok := r.glimpse(modelPatience, say(t, "chatWorkingFootWord")); ok {
		t.Logf("the turn was in flight when escape was pressed:\n%s", caught)
	} else {
		t.Logf("FINDING: the working foot was never caught — the turn may have finished first")
	}
	r.keys("Escape")
	// The ledger's writer is a background goroutine and both places read the
	// file on a three-second beat, so the reading is taken after one beat has
	// certainly turned rather than in the same instant as the keystroke.
	time.Sleep(5 * time.Second)

	// ── the /spend place ──────────────────────────────────────────────────
	//
	// `alt+5` and not `/spend`: the place's doors are the chord, `tab`, and the
	// word typed at home — `/spend` is an alias of `/cost`, which is this
	// conversation's own note rather than the machine's page. The chord arrives
	// as esc-then-3, which is what internal/tui3's placeDigit reads.
	//
	// THE DIGIT IS THE PLACE'S RANK IN internal/tui3's placeOrder. Teams and
	// chats now stand between home and sessions, making spend the fifth place.
	r.lit("\x1b5")
	place := r.waitFor(25*time.Second, say(t, "spendRailsHint"))
	t.Logf("the spend place after the interrupted turn:\n%s", place)
	fromPlace := moneyOn(t, place, say(t, "spendRailsHint"))
	if fromPlace == "" {
		t.Fatalf("the spend place's pointer line carries no figure:\n%s", place)
	}
	t.Logf("the /spend place says %s", fromPlace)

	// ── Settings → Spending ───────────────────────────────────────────────
	r.keys("Escape")
	time.Sleep(600 * time.Millisecond)
	r.lit("/budget")
	time.Sleep(600 * time.Millisecond)
	r.keys("Enter")
	tab := r.waitFor(25*time.Second, say(t, "spendConversationRow"))
	t.Logf("the Spending tab after the interrupted turn:\n%s", tab)

	fromToday := moneyOn(t, tab, say(t, "spendTodayResets"))
	if fromToday == "" {
		t.Fatalf("the Spending tab's `today` row carries no figure:\n%s", tab)
	}
	fromThisOne := moneyAfter(t, tab, say(t, "spendThisOneWord"))
	if fromThisOne == "" {
		t.Fatalf("the Spending tab's `this one` receipt carries no figure:\n%s", tab)
	}
	t.Logf("Settings→Spending says today %s and this one %s", fromToday, fromThisOne)

	if fromPlace != fromToday {
		t.Errorf("the /spend place says %s and Settings→Spending's `today` says %s — "+
			"one machine, one day, two numbers (issue #269)", fromPlace, fromToday)
	}
	if fromThisOne != fromToday {
		t.Errorf("Settings→Spending says today %s and this one %s on a machine holding "+
			"exactly one conversation", fromToday, fromThisOne)
	}
}

// moneyOn is the FIRST dollar figure on the screen line carrying `needle`, and
// the empty string when there is no such line or no figure on it. Both rows it
// is used on lead with what was spent and follow it with the rail — `today
// $0.0003 of $500` — so the first figure is the spend on each.
//
// IT READS THE LINE THE PERSON READS. The whole claim under test is that these
// places render one STRING, so the figure is lifted out of the drawn row rather
// than recomputed from anything.
func moneyOn(t *testing.T, screen, needle string) string {
	t.Helper()
	return moneyIn(t, screen, needle, false)
}

// moneyAfter is the figure that FOLLOWS `needle` on its line, for the receipt
// whose row may carry a limit ahead of it — `per conversation  $20 · this one
// $0.0003`, where the first figure on the line is the rail and not the spend.
func moneyAfter(t *testing.T, screen, needle string) string {
	t.Helper()
	return moneyIn(t, screen, needle, true)
}

func moneyIn(t *testing.T, screen, needle string, after bool) string {
	t.Helper()
	for _, line := range strings.Split(screen, "\n") {
		found := strings.Index(line, needle)
		if found < 0 {
			continue
		}
		rest := line
		if after {
			rest = line[found+len(needle):]
		}
		at := strings.Index(rest, "$")
		if at < 0 {
			continue
		}
		figure := "$"
		for _, r := range rest[at+1:] {
			if (r < '0' || r > '9') && r != '.' {
				break
			}
			figure += string(r)
		}
		if len(figure) > 1 {
			return figure
		}
	}
	return ""
}

// ── 12 ──────────────────────────────────────────────────────────────────────

// waitForTaskCalls polls the call log until a node's own call is in it, and
// answers every model those calls asked for.
func waitForTaskCalls(t *testing.T, home string, within time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if models := taskCallModels(t, home); len(models) > 0 {
			return models
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(pollEvery)
	}
}

// callModels is every model this run asked for, and taskCallModels is the
// subset a task node asked for — read out of the always-on call log
// (internal/calllog), which is one JSON object per line under the state root.
func callModels(t *testing.T, home string) []string     { return callLogModels(t, home, "") }
func taskCallModels(t *testing.T, home string) []string { return callLogModels(t, home, "task") }

func callLogModels(t *testing.T, home, tag string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
	if err != nil {
		// A log that is not there yet is a run that has made no call yet, which
		// is a state the poll above is entitled to see once.
		return nil
	}
	var models []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record struct {
			Tag   string `json:"tag"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.Model == "" || (tag != "" && record.Tag != tag) {
			continue
		}
		models = append(models, record.Model)
	}
	return models
}

// ── 14 ──────────────────────────────────────────────────────────────────────

// testTaskRoomKeepsSpace is #457/#486's acceptance ON THE REAL SCREEN: the door
// home yields inside a task's record, where a bare `space` already means page
// the card.
//
// THE DEFECT THIS MEASURES WAS INTRODUCED BY THE FEATURE IT SHIPS WITH. #457
// widened `space space` so it opens home from every place and not only from a
// conversation, and the widened door is read at the bottom of the place router
// — ABOVE each place's own reading of the key. The record is a mode of the
// tasks place whose arm sits in that reading ([app.taskSheetKeyPress]), and its
// map spells the page key `pgdown`, `ctrl+f`, `space` ([app.taskCardKey]). So a
// roster filter left holding ONE space — which is exactly the state the door's
// own first press leaves, and which draws nothing a person can see — armed the
// door on a box the record types nothing into, and the next space, pressed to
// scroll, walked the person out of the record and onto home.
//
// WHY IT IS HERE AND NOT ONLY IN internal/tui3. The unit test beside the fix
// (TestSpaceInTheTaskRoomPagesTheCardAndDoesNotOpenHome) drives the same three
// keys through the same router, and it is the cheap gate. What it cannot say is
// that a person doing this at a terminal — a real task, started the ordinary
// way, read back off the project's own record — lands where the router says
// they land.
//
// IT IS READ IN A SECOND WINDOW, AND THE FIRST SCREEN THIS SUBTEST EVER DREW IS
// WHY. `enter` on the roster has two doors and the row chooses between them
// ([tasksPlace.enter]): over a node THIS session's graph is still holding it
// opens the live room, which is a fullscreen surface of its own and not the
// mode this test is about; over work no window is holding any more it opens the
// record card. So the task is started in one window and read in the next, which
// is also how a person meets a finished task — the record outlives the session
// that ran it.
//
// IT IS DELIBERATELY THE CHEAPEST TASK THERE IS — `/task solo` on a one-file
// brief, the cheapest shape a task comes in. The measured run
// lands in about thirty seconds and costs five cents.
//
// AND IT RUNS SHORT ON PURPOSE ([tuiShortRows]): a record that fits on the
// screen has nothing to page, so a tall frame would make the positive half of
// this test vacuous without ever saying so.
func testTaskRoomKeepsSpace(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "roomws", false)

	// ── the window that does the work ────────────────────────────────────────
	first := start(t, "afe2e_room1", home, ws, tuiPlain, tuiShortRows, "chat", "--one-model", "--no-host")
	// Whichever door the launch took. On a state root built one minute ago it is
	// the setup, whose own foot says `esc skips setup`, and esc is what the rest
	// of this file presses at this rung anyway.
	// Whichever door the launch took, and esc UNTIL THE SETUP IS ACTUALLY GONE. On
	// a state root built one minute ago it is the setup, which is several steps —
	// so one esc leaves the one under it, and the brief typed next goes into that
	// step's own box instead of into the composer.
	statesPastTheDoor(t, first)
	first.lit("/task solo write a file called hello.txt containing the word hello")
	first.keys("Enter")

	// `/history` IS THE DOOR AND THE CHORD IS NOT. The page's own row names
	// `ctrl+.` (commands.go), and that chord only reaches the program from a
	// terminal that answered the keyboard query — so a suite that pressed it
	// would be testing tmux's encoding rather than this surface's grammar.
	first.lit("/history")
	time.Sleep(700 * time.Millisecond)
	first.keys("Enter")
	// The tasks page selects the conversation group first. Once the task is
	// recorded, open that group and move onto its child row to inspect the
	// task's own door.
	//
	// THE GROUP IS SHUT AND `→` IS WHAT OPENS IT. #905 made this page a table
	// with every family folded, so the heading over this one reads `finished
	// today · 1 folded away` and the only row on the list is the conversation
	// root — a `Down` on its own had nowhere to go, and every press after it was
	// reading the conversation's foot (`enter go to that conversation`) as though
	// it were the task's. The key is the one the foot itself names, `→ what ran
	// under it`, so this presses what a person reading that line would press.
	bucket := waitForRecord(t, home, 5*time.Minute)
	first.keys("Right")
	time.Sleep(700 * time.Millisecond)
	first.keys("Down")
	// AND EITHER FOOT WILL DO, BECAUSE HOW THE WORK LANDED IS THE MODEL'S
	// BUSINESS AND NOT THIS SUBTEST'S. When the checker answers, the node lands
	// `done` and the roster's foot offers the live room. When it does not — a
	// checking call that returns nothing inside its window, which this brief
	// draws about one run in four on `deepseek-v4-flash` — the node lands `your
	// call`, the landing asks the person something, and the foot says where that
	// question is waiting instead. Both are a recorded task on the roster, which
	// is the only fact the rest of this test needs; measured on `dev` at
	// 6aa6a946e, one run in four (2026-09-10) died here on the second shape
	// while the paging it exists to prove worked perfectly.
	_, started := first.waitForAny(30*time.Second,
		say(t, "tasksEnterRoomWord"), say(t, "questionWaitingWord"))
	t.Logf("the selected task is on the roster of the window that started it:\n%s", started)
	first.quit()

	// ── and the window that reads it back ────────────────────────────────────
	//
	// IT IS LAUNCHED ON A CONVERSATION OF ITS OWN, INSIDE THE SAME PROJECT, AND
	// THREE MEASURED RUNS ARE WHY. A second window opened the ordinary way
	// RESUMES the conversation the task was started from, graph and all
	// ([app.tasks] is replayed with it), so the roster went on offering the live
	// room; `/new` after that resume left the page reading an index it had
	// already marked loaded; and `--session` on a path OUTSIDE the projects tree
	// reads its index beside that path and finds nothing, because the record is
	// the project's and not the machine's ([session.TaskIndexPath] joins the
	// bucket ABOVE a `transcript.jsonl`). So the path is built inside the bucket
	// the first window wrote, which is what makes this window a stranger to the
	// node and a reader of its record at the same time — the only combination
	// the record card exists for.
	//
	// AND ITS FOLDER IS SHAPED LIKE A SESSION'S, which is a fact the assertion
	// below turns on. A session's folder IS its id — sixteen hex digits — and
	// every surface names a conversation nothing has titled after that folder,
	// drawing the word only where the folder has nothing a person could read in
	// it (internal/tui3's names.go, [listName]). This fixture called its folder
	// `read-it-back`, so the product read it as a name and drew `Read It Back`:
	// #915's law was being asked about a conversation the fixture had given a
	// title to.
	fresh := filepath.Join(bucket, "9c1d4a0b7e2f6538", "transcript.jsonl")
	r := start(t, "afe2e_room2", home, ws, tuiPlain, tuiShortRows, "chat", "--session", fresh, "--one-model", "--no-host")
	statesPastTheDoor(t, r)
	r.lit("/history")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	// THE DOOR THIS SUBTEST IS ABOUT. No window is holding the node any more, so
	// the foot offers the record rather than the room. The sessions place now
	// can put this window's new conversation below the earlier task's family;
	// walk onto each row before reading it.
	//
	// THE FOOT IS THE WHOLE SYNCHRONISATION AND THE GROUP HEADING WAS NEVER PART
	// OF IT. This wait used to sit behind `finished today`, which is the roster's
	// heading for work that ENDED today — and a node the checker could not judge
	// ends under `your call` instead, so the heading was a claim about how the
	// model's work landed standing in front of a test about paging a record.
	// AND THE CONVERSATION THIS WINDOW IS IN IS ITSELF THE TITLELESS ROW (#915).
	// This terminal was launched on a fresh transcript nothing has been said in,
	// so the one session it stands in is the launch's own untitled conversation —
	// and the page this door opened is the one that used to draw it as its raw
	// sixteen-hex id. The word is what the row must answer to now, and it is the
	// same word home's own column spells for a chat nothing has named, so the
	// walk holds both the name and the one spelling of it.
	//
	// A YOUR CALL LANDING PUTS THIS ROW BELOW THE FOLD at fourteen rows. Down
	// reaches it the way a person does; waiting on the first frame instead made
	// the driver fail on a reachable row. The task and the titleless row need
	// not fit on the same frame.
	r.waitFor(30*time.Second, say(t, "tasksEnterConversationWord"))
	if !walkTo(r, say(t, "tasksUnusedConversationSelectedWord"), "Down") {
		t.Fatalf("could not reach this window's titleless conversation:\n%s", r.capture())
	}
	untitledAt := r.capture()
	if !strings.Contains(untitledAt, say(t, "tasksUntitledWord")) {
		t.Fatalf("the selected titleless conversation lost its word:\n%s", untitledAt)
	}
	t.Logf("the conversation this window stands in is on the page by its word:\n%s", untitledAt)

	// Home returns to the top of the list, so the record's door is reached
	// independently of where the titleless conversation stood.
	r.keys("Home")
	if !walkTo(r, say(t, "tasksEnterInsideWord"), "Down") {
		t.Fatalf("could not select the finished task's record row:\n%s", r.capture())
	}
	roster := r.capture()
	t.Logf("the roster is offering the record of work nothing is holding:\n%s", roster)

	// AND THE RECORD IS ALREADY BESIDE THE LIST. This terminal is [tuiPlain] wide,
	// which is over the pane's floor, so the row under the cursor has its record
	// drawn to the right of the seam without anything being opened (taskpane.go).
	// Its verb line is the assertion: it is the one clause every row carries,
	// whatever state the model's work landed in.
	pane := r.waitFor(30*time.Second, say(t, "tasksPaneOpenWord"))
	t.Logf("the pane is previewing the row under the cursor:\n%s", pane)

	// THE ARMING PRESS, AND IT IS THE ORDINARY ONE. A single space on a place
	// types itself into that place's filter and opens nothing — this is the first
	// half of the gesture, done by hand, and it is what leaves the door loaded.
	r.keys("Space")
	// The space is a filter edit, and on the sessions place an edit puts the
	// cursor back on the top row, so the task's row is walked to again. Any key
	// but a second space disarms the gesture, and the enter below always did, so
	// the walk proves no less than the enter alone did.
	if !walkTo(r, say(t, "tasksEnterInsideWord"), "Down") {
		t.Fatalf("could not select the finished task's record row after the space:\n%s", r.capture())
	}
	armed := r.capture()
	if strings.Contains(armed, say(t, "placeRestWord")) {
		t.Fatalf("one space opened home from the roster:\n%s", armed)
	}

	// Inside the record, over the roster, the way the foot just said.
	r.keys("Enter")
	record := r.waitFor(30*time.Second, say(t, "taskRoomFootWord"))
	t.Logf("the record is open, over a filter holding one space:\n%s", record)

	// AND HERE IS THE KEY THE DOOR HAD TO GIVE BACK.
	r.keys("Space")
	after := r.waitFor(15*time.Second, say(t, "taskRoomFootWord"))
	t.Logf("the record after the space that used to walk out of it:\n%s", after)
	if strings.Contains(after, say(t, "placeRestWord")) {
		t.Fatalf("space in the record opened home, which is #457's own defect:\n%s", after)
	}

	// AND IT PAGED, which is the positive half and the reason `space` is worth
	// keeping here at all. `g` puts the record back at its top and `pgdown` is
	// the same key as space on this card, so pgdown is the control: whatever it
	// carries off the top of the frame, space owes the same. If pgdown cannot
	// move the record either then this record is shorter than the frame and there
	// was nothing to page, which this says out loud rather than passing quietly.
	r.lit("g")
	top := r.waitFor(15*time.Second, say(t, "taskRoomFootWord"))
	r.keys("PageDown")
	control := r.waitFor(15*time.Second, say(t, "taskRoomFootWord"))
	head := recordHeadLine(top)
	switch {
	case head != "" && !strings.Contains(control, head):
		if strings.Contains(after, head) {
			t.Errorf("pgdown carried %q off the frame and space did not, so space did not page:\n%s", head, after)
		} else {
			t.Logf("space paged the record exactly as pgdown does: %q is off both frames", head)
		}
	default:
		t.Logf("this record fits the frame — pgdown could not move it either — so only the negative half was measured here; top:\n%s", top)
	}

	r.quit()
}

// recordHeadLine is the first line of the record's own SCROLLING body: the row
// that stands under the card's top rule while the card is at its top, and is
// gone once the card has paged.
//
// IT IS THE LINE UNDER THE RULE AND NOT THE FIRST LINE ON THE FRAME, because
// the row above the rule is the card's title band and the title band does not
// scroll — a test that watched it would watch a line that can never move and
// would call every page a failure to page. And it is READ OFF THE SCREEN rather
// than named here because what stands there is a sentence about work a model
// did, which is exactly the kind of string this suite's own header forbids
// anybody from remembering.
func recordHeadLine(screen string) string {
	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "─") {
			continue
		}
		for _, under := range lines[i+1:] {
			if text := strings.TrimSpace(under); len(text) >= 12 && !strings.HasPrefix(text, "─") {
				return text
			}
		}
		return ""
	}
	return ""
}

// waitForRecord waits until the project's task record exists and holds a row,
// and answers the bucket it is in.
//
// IT IS FOUND RATHER THAN SPELLED because the folder's name is a slug of a
// temporary workspace path, which is a string no test may write down and none
// could get right twice. And it is WAITED FOR rather than read once because the
// row is written when the node lands: this is the one place in this subtest
// that is about the work finishing rather than about the keyboard.
func waitForRecord(t *testing.T, home string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		matches, err := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "tasks.jsonl"))
		if err != nil {
			t.Fatalf("projects: %v", err)
		}
		for _, path := range matches {
			if raw, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(raw)) != "" {
				return filepath.Dir(path)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for a task record under %s and found %v", within, home, matches)
		}
		time.Sleep(2 * time.Second)
	}
}

// ── the run engine's own plan ───────────────────────────────────────────────

// testTaskOnTheRunEngine is a `/task` on the RUN ENGINE, read off a real screen:
// the conversation seeds a plan store rather than a node of its own tree, the
// engine drives it, and what lands is the store's root — a row on the tasks
// place that moves with the store's own state, and the page that row opens,
// whose trajectory is the worker's record of every command it ran.
//
// THE BELT IS NAMED IN THE BINARY'S OWN ENVIRONMENT. The harness is the
// default, and this subtest still says CODEAF_TASK_BELT=bash outright, for the
// reason [start] says `node`: a scenario that names its road keeps testing that
// road when the default moves. The default itself — that a `/task` with the
// variable absent takes this same road — is what [testTaskOnTheDefaultBelt]
// proves, on the same screens, with no word given. `node`, `legacy` and `off`
// are the words that send the same `/task` to an ordinary node of this
// session's tree instead, which the roster subtests read.
//
// IT COSTS A FEW CENTS AND LANDS IN ABOUT THIRTY SECONDS, the shape and the
// price [testStatesDone] pays for the same brief on the node belt.
//
// THE STATE WORD IS READ OFF THE ROW AND NOT OFF THE SCREEN. The place files its
// rows under headings that are state words themselves — everything working stands
// under `running` (tasksplace.go's tasksSectionWord) — so a screen-wide wait for
// the word is a wait for a heading and proves nothing about any row. It is the
// care [statesHeadLine] takes on a landing card, spent on a row of the list
// ([planRowWearing]).
func testTaskOnTheRunEngine(t *testing.T) {
	taskOnTheRunEngine(t, "afe2e_task_run", "CODEAF_TASK_BELT=bash")
}

// testTaskOnTheDefaultBelt is the one scenario in this suite that launches the
// binary with NO belt word and asserts the road it takes. It is the only thing
// here that tests the default: every other scenario names its road, so the
// default could move without one of them noticing, and it did, twice, in
// #1335 and #1340. [startWithEnv] drops the runner's own variable before the
// child starts, so absent here means absent in the process and not merely
// unmentioned by the test.
func testTaskOnTheDefaultBelt(t *testing.T) {
	taskOnTheRunEngine(t, "afe2e_task_default")
}

// taskOnTheRunEngine is the body the two subtests above share: launch with the
// key and whatever belt words the caller names, put one `/task` on the run
// engine, and read the run off the tasks place, the task's room and the thread.
func taskOnTheRunEngine(t *testing.T, rigName string, beltWords ...string) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "runws", false)
	r := startWithEnv(t,
		append([]string{config.APIKeyEnv + "=" + liveKey(t)}, beltWords...),
		rigName, home, ws, tuiWide, 45, "chat", "--one-model")
	r.skipSetup(t)

	// runRowWord is the run's own words on the tasks place, and one word of the
	// brief this subtest types: the row the record publishes and the row the store
	// answers are both named from the person's own sentence, so it is the anchor a
	// row is found by when the state word beside it is what is being proved. It is
	// a constant rather than a second spelling of the brief so the two cannot
	// drift apart.
	const runRowWord = "HELLO.md"

	r.lit("/task write " + runRowWord + " containing the word hello")
	r.keys("Enter")

	// ── the row on the tasks place ──────────────────────────────────────────
	//
	// THE PLAN IS THE STORE'S READ, and the place draws it beside this
	// conversation's own record: a plan row wears the state word its store status
	// maps to (internal/tui3's planStateWord), so `ready` and `claimed` — the
	// store saying a task is deliverable and a worker has it — both read as work
	// in flight, and a task whose root has landed reads done.
	openTasksPlace(t, r)
	running := planRowWaitsToWear(t, r, 40*time.Second, runRowWord, say(t, "planRunningWord"))
	t.Logf("the run on the tasks place, while a worker holds its task:\n%s", running)

	// ── the room mid-run ────────────────────────────────────────────────────
	//
	// ENTER OPENS THE TASK'S ROOM, the one page every task opens, and while a
	// worker holds the task the room follows it on its own beat (internal/tui3's
	// planroom.go). It is read here, before the root lands; the room after the
	// landing is the settled room the section below reads.
	//
	// AND THE CURSOR IS MOVED ONTO THE ROW FIRST ([tasksPlaceRunRow]). Enter on
	// the conversation row is the door into the conversation and always was, so
	// a press made without this one read the chat and said nothing about a plan
	// page at all — and passed, because the mark it waits for is drawn on the
	// conversation too.
	tasksPlaceRunRow(t, r)
	r.keys("Enter")
	// THE ROOM COMING UP IS THE ASSERTION: its two tabs are there whatever state
	// the task is in. The note box's own words are there only while the task can
	// still take a note, and which command a worker happens to be part-way
	// through when the key lands is a moment, so both are only logged.
	page := r.waitFor(40*time.Second, say(t, "roomTabsWords"))
	t.Logf("the room the press over the run's row opened:\n%s", page)
	if box, saw := r.glimpse(2*time.Second, say(t, "planNoteBoxWord")); saw {
		t.Logf("the room's box takes a note while the task runs:\n%s", box)
	} else {
		t.Logf("the task had ended before its room opened, so its box names another door")
	}
	if live, sawLive := r.glimpse(15*time.Second, say(t, "planLiveGlyph")); sawLive {
		t.Logf("the room mid-run, carrying the live step:\n%s", live)
	} else {
		t.Logf("the run finished before a live step could be caught in the room, which is this " +
			"brief on a fast worker and not a defect")
	}
	// `esc` LEAVES THE ROOM FOR THE CONVERSATION, the way every room does, so
	// the place is opened again to watch the row land.
	r.keys("Escape")
	openTasksPlace(t, r)

	done := planRowWaitsToWear(t, r, runPatience, runRowWord, say(t, "planDoneWord"))
	t.Logf("the run on the tasks place once its root landed:\n%s", done)

	// ── the landing, in the thread ──────────────────────────────────────────
	//
	// THE ENGINE COMMITS THE RUN'S TREE ON ITS BRANCH and hands the conversation
	// the same landing a node sends (the door's own note and the row it settles).
	// The card that lands names the branch the work was left on, which is the one
	// handle back to work that is not on the screen; the branch is the working
	// copy's own, because a run lands the tree where it stands (internal/run's
	// Land over session.LandRunTree).
	r.keys("Escape")
	landed := r.waitFor(modelPatience, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"),
		say(t, "taskBranchKeptFact"))
	t.Logf("the run's landing card in the thread:\n%s", landed)
	if branch := runBranch(t, ws); branch != "" && !strings.Contains(landed, branch) {
		t.Errorf("the landing does not name the branch the run's work is on (%q):\n%s", branch, landed)
	}

	// ── the room one row opens ──────────────────────────────────────────────
	//
	// ENTER OVER THE ROW OPENS THE TASK'S ROOM, read through the store: the
	// description the worker was given, the notes left on the task, and the
	// trajectory as the room's own call rows. `esc` leaves for the conversation.
	openTasksPlace(t, r)
	tasksPlaceRunRow(t, r)
	r.keys("Enter")
	// THE ROOM IS READ BY ITS OWN WORDS, and not by the model's: the
	// room's two tabs, which every task's room draws on either engine.
	//
	// WHICH COMMANDS ARE ON IT IS THE WORKER'S BUSINESS. The finish is the
	// worker's own `plandb done` when the worker writes one, and the RUN's when
	// it does not (internal/run's worker.go), and a brief this small on a fast
	// model is regularly the second — measured twice on this lane, where the
	// page carried `echo`, `cat` and the run's own ending note. So the finish
	// command is observed and logged, never waited out: asserting it made a red
	// out of a model's choice and said nothing about the surface.
	stored := r.waitFor(40*time.Second, say(t, "roomTabsWords"))
	t.Logf("the room the run's row opens:\n%s", stored)
	if finish, saw := r.glimpse(5*time.Second, say(t, "planFinishCommand")); saw {
		t.Logf("and this worker wrote its own finish into the trajectory:\n%s", finish)
	} else {
		t.Logf("this worker left the ending to the run, so no %q step is on the page",
			say(t, "planFinishCommand"))
	}
	r.keys("Escape")
	openTasksPlace(t, r)
	back := planRowWaitsToWear(t, r, 30*time.Second, runRowWord, say(t, "planDoneWord"))
	t.Logf("the list, after esc left the room:\n%s", back)
	r.quit()
}

// openTasksPlace opens the place onto everything this machine has run, through
// its one command: `/history` (commands.go — deliberately not `/tasks`, which the
// three work-starting rows would narrow to), and opens the conversation's fold
// when it is shut.
func openTasksPlace(t *testing.T, r *rig) {
	t.Helper()
	r.lit("/history")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	time.Sleep(700 * time.Millisecond)
	// A group can already be open on a return visit. The foot says whether the
	// run's row is drawn or still needs the right arrow.
	state, _ := r.waitForAny(30*time.Second, say(t, "tasksFoldShutWord"), say(t, "tasksFoldOpenWord"))
	if state == say(t, "tasksFoldShutWord") {
		r.keys("Right")
		r.waitFor(30*time.Second, say(t, "tasksFoldOpenWord"))
	}
}

// planRowWaitsToWear polls until the run's OWN ROW on the tasks place wears this
// state word, and answers the screen it was read on.
//
// IT IS A WAIT ON THE ROW AND NOT ON THE SCREEN, which is this scenario's own law
// ([planRowWearing]) spelled as a wait rather than only as an assertion. The
// place files its rows under headings that are state words themselves, so a
// screen-wide wait returns the instant a HEADING says `running` — which on a real
// screen can be before the store's own read has landed and before the fold has
// opened, and the assertion then reads whichever line happens to carry the title.
// Two runs of one binary split on exactly that: one read the row and passed, the
// next read the side list's line and failed.
func planRowWaitsToWear(t *testing.T, r *rig, within time.Duration, words, state string) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		screen := r.capture()
		if tasksRowWearing(screen, words, state) != "" {
			return screen
		}
		if time.Now().After(deadline) {
			// THE RED IS THE ASSERTION'S OWN, so a row wearing the wrong word reads
			// as what that means and not as a timeout.
			planRowWearing(t, screen, words, state)
			return screen
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// tasksPlaceRunRow steps the cursor off the conversation group and onto the
// first row inside it, which is the run's own.
//
// THE PLACE SELECTS THE CONVERSATION FIRST and `enter` over that row opens the
// conversation ([app.openConversationRow]), which is not a mistake in the
// surface: a group row's door is the group. So a press over a piece of work is
// a press over the row, and the walk down onto it is part of the gesture —
// the same `↓` [testStatesDone] takes before it reads a task's own door.
func tasksPlaceRunRow(t *testing.T, r *rig) {
	t.Helper()
	r.keys("Down")
	time.Sleep(400 * time.Millisecond)
}

// tasksRowOf is the one line of the tasks place carrying these words, or "" when
// the place draws no such row.
//
// IT IS SOUGHT AS A LINE AND NOT AS A SUBSTRING OF THE SCREEN for the reason
// [statesHeadLine] is: a state word on this place is also a heading over it, and
// a pair of screen-wide searches is satisfied by the word standing in two
// different places with nothing between them.
func tasksRowOf(screen, words string) string {
	for _, line := range strings.Split(screen, "\n") {
		if row := strings.TrimSpace(line); strings.Contains(row, words) {
			return row
		}
	}
	return ""
}

// tasksRowWearing is the one line of the tasks place carrying these words AND
// this state word, or "" when no line carries both.
//
// IT IS BOTH WORDS ON ONE LINE AND NOT THE FIRST LINE WITH THE TITLE. At the
// width this suite runs the place draws the record pane beside the list, on the
// same rows, and the pane LEADS WITH THE SELECTED ROW'S OWN TITLE — so the first
// line carrying the work's name is the pane's heading, which wears no state at
// all. A search that stopped there read `⌕ type to filter … │ write HELLO.md …`
// off a screen whose row said `done · 4 steps · $0.03` two lines below, and
// reported the row as bare.
func tasksRowWearing(screen, words, state string) string {
	for _, line := range strings.Split(screen, "\n") {
		row := strings.TrimSpace(line)
		if strings.Contains(row, words) && strings.Contains(row, state) {
			return row
		}
	}
	return ""
}

// planRowWearing asserts that the run's row is on the tasks place wearing this
// state word, and answers the row for the log.
//
// THE FAILURE IT MAKES IS THE WHOLE OF WHAT STANDS BETWEEN THIS SUITE AND THE
// RUN'S OWN PAGE, so it says the mechanism rather than the symptom. internal/tui3
// drops the store's plan row whenever a node row of this conversation wears the
// same title (taskplan.go's planRowShown, which exists for a plan-born node — a
// node the plan dispatched, whose store task is the same work read from the other
// end). A run is not one: its door publishes a row of its own with the store
// root's title on it (internal/session's task_run_belt.go seeds the store and the
// row from one sentence), so the dedupe takes the plan row for a duplicate of it.
// What the place is left drawing is the node's row in the engine's own word, and
// Enter over that row opens a room the engine holds no node for.
func planRowWearing(t *testing.T, screen, words, state string) string {
	t.Helper()
	if row := tasksRowWearing(screen, words, state); row != "" {
		return row
	}
	row := tasksRowOf(screen, words)
	if row == "" {
		t.Errorf("the tasks place draws no row for the run (%q), so nothing on it can wear %q:\n%s",
			words, state, screen)
		return ""
	}
	if !strings.Contains(row, state) {
		t.Errorf("the run's row does not wear %q, so the row the place drew is not the store's plan "+
			"row. The run's door publishes a row for work the graph holds no node for and says which "+
			"store task it is (session's TaskNotice.PlanTask); the place takes those rows out by that "+
			"identity and draws the store's own (internal/tui3's planStoreDraws). A row wearing the "+
			"engine's `working` instead is the node half, which means the identity did not join — it is "+
			"dropped on the way, or the two ends spell the store id differently:\n\t%s", state, row)
	}
	return row
}

// runBranch is the branch the run kept. The conversation's checkout remains on
// main, while the task's branch is what the landing names.
func runBranch(t *testing.T, ws string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", ws, "branch", "--list", "--format=%(refname:short)", "task/*").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

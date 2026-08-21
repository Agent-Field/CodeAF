//go:build e2e

package e2e

// standing_e2e_test.go is the ambient side driven end to end against a real
// model: a sentence becomes a card, a yes becomes a document on disk, a pass
// wakes it, and the news reaches a person — in a live conversation, in a
// conversation that was shut, or in a project nobody had open.
//
// EVERY SUBTEST LOGS WHAT THE MODEL ACTUALLY DID. The tool call, its result and
// the closing line are all in the -v output, because the point of this lane is
// evidence and not an assertion that something unspecified happened.
//
//	go test -tags e2e -run TestStandingE2E -v -timeout 30m ./internal/e2e/

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

func TestStandingE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" {
		t.Skip("no OPENROUTER_API_KEY: this lane drives a real model")
	}

	t.Run("1 a reminder is proposed, ratified and written down", standingReminderStands)
	t.Run("2 a tick fires it into the live conversation", standingFiresIntoTheRoom)
	t.Run("3 a closed conversation gets one while-you-were-away fold", standingInboxFold)
	t.Run("4 an errand's firing waits on the project", standingErrandReachesTheProject)
	t.Run("5 a probe watch checks, says nothing, then fires", standingProbeWatch)
	t.Run("6 a rhythm is paused, resumed, stopped and listed", standingRhythmLifecycle)
	t.Run("7 a task firing runs headless and bills", standingTaskFiring)
	t.Run("8 the daily rail skips the second firing", standingDailyRail)
	t.Run("9 an unwatched session cannot set one up", standingUnwatchedRefuses)
	t.Run("10 a moment that has passed", standingPastMoment)
	t.Run("11 the sweep reaps a run that came to nothing", standingSweep)
}

// ── 1 ───────────────────────────────────────────────────────────────────────

// standWords is the sentence every reminder scenario starts from. It is one
// sentence a person would actually say, and it is the anchor every surface
// leads with afterwards ([standing.Item.Words]).
const standWords = "remind me in 1 minute to drink water"

func standingReminderStands(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	agent, _ := w.open(workspace, nil)

	before := time.Now()
	turn := w.say(agent, standWords, answerYes)

	// THE CALL. op=propose, and the moment said as a distance rather than
	// shelled out for.
	stand, found := turn.named("stand")
	if !found {
		t.Fatalf("the model never called stand; it called %v and said %q", turn.names(), turn.Reply)
	}
	var args struct {
		Op   string `json:"op"`
		When struct {
			Kind string `json:"kind"`
			At   string `json:"at"`
			In   string `json:"in"`
		} `json:"when"`
		Does struct {
			Kind string `json:"kind"`
		} `json:"does"`
	}
	if err := json.Unmarshal([]byte(stand.Args), &args); err != nil {
		t.Fatalf("the stand call did not parse: %v\n%s", err, stand.Args)
	}
	if args.Op != "propose" {
		t.Fatalf("stand op = %q, want propose", args.Op)
	}
	if args.When.Kind != string(standing.WhenAt) {
		t.Fatalf("when.kind = %q, want at", args.When.Kind)
	}
	switch {
	case strings.TrimSpace(args.When.In) != "":
		t.Logf("the model used when.in = %q — the moment is aforge's to resolve", args.When.In)
	case strings.TrimSpace(args.When.At) != "":
		moment, err := time.ParseInLocation(time.RFC3339, args.When.At, time.Local)
		if err != nil {
			t.Fatalf("when.at %q did not parse: %v", args.When.At, err)
		}
		if !moment.After(before) {
			t.Fatalf("when.at %s is not in the future (now was %s)", moment, before)
		}
		t.Logf("the model wrote the stamp itself: when.at = %q", args.When.At)
	default:
		t.Fatalf("the call carried neither when.at nor when.in: %s", stand.Args)
	}
	// AND IT DID NOT SHELL OUT FOR THE CLOCK. The Now line in the system
	// prompt is what the schema tells it to read; a bash call before the stand
	// call is that instruction going unread.
	for _, name := range turn.names() {
		if name == "stand" {
			break
		}
		if name == "bash" {
			t.Errorf("the model shelled out before proposing: calls were %v", turn.names())
		}
	}

	// THE CARD.
	if len(turn.Proposals) != 1 {
		t.Fatalf("cards drawn = %d, want 1", len(turn.Proposals))
	}
	notice := turn.Proposals[0]
	if strings.TrimSpace(notice.WhenWords) == "" {
		t.Errorf("the card carried no WhenWords; a card that cannot say when is a card nobody can check")
	}
	if !notice.Deadline.IsZero() {
		t.Errorf("the card carried a clock (%s) — a watched card has none", notice.Deadline)
	}

	// THE DOCUMENT.
	item := w.onlyItem()
	onDisk := w.itemFile(item.ID)
	if onDisk.Status != standing.StatusActive {
		t.Errorf("status = %q, want active", onDisk.Status)
	}
	if onDisk.Words != standWords {
		t.Errorf("words = %q, want the person's own sentence %q", onDisk.Words, standWords)
	}
	want := before.Add(time.Minute)
	if drift := onDisk.NextDue.Sub(want); drift < -30*time.Second || drift > 90*time.Second {
		t.Errorf("nextDue = %s, want about %s (drift %s)", onDisk.NextDue, want, drift)
	}
	if onDisk.Rails.PerRunUSD <= 0 || onDisk.Rails.MaxPerDay <= 0 {
		t.Errorf("rails are not bounded: %+v", onDisk.Rails)
	}
	t.Logf("ITEM %s status=%s nextDue=%s when.words=%q does=%s/%q rails=$%.2f×%d",
		onDisk.ID, onDisk.Status, onDisk.NextDue.Format(time.RFC3339), onDisk.When.Words,
		onDisk.Does.Kind, onDisk.Does.Say, onDisk.Rails.PerRunUSD, onDisk.Rails.MaxPerDay)

	// WHAT THE MODEL WAS TOLD, AND WHAT IT SAID BACK.
	if !strings.Contains(stand.Output, "set up") {
		t.Errorf("the tool result did not say it was set up: %q", stand.Output)
	}
	if turn.Reply == "" {
		t.Fatalf("the model said nothing after the card was answered")
	}
	// ONE LINE IS WHAT THE ENGINE ASKS FOR, and the two halves of that are not
	// the same kind of fact, so they are not the same kind of assertion.
	//
	// THE HARD HALF is that the last thing the person's eye lands on is a
	// sentence FOR THEM. deepseek-v4-flash has been seen answering a ratified
	// card with nothing but its own working out — "This is a `stand` with
	// op=propose, when.kind=at, when.in=\"1m\"" and then silence — and a person
	// who said yes and was told only that was never told what now stands. That
	// is a failure whatever else is true.
	//
	// THE SOFT HALF is a preamble in FRONT of a good sentence. It is the same
	// defect one degree less bad, and it is reported rather than failed for one
	// honest reason: the engine has already done everything the engine can. The
	// instruction is in the tool result the model reads
	// (session.standingRatifiedLine, pinned by its own unit test), it was
	// strengthened after this lane caught the behaviour, and what is left is the
	// model's own compliance — measured here at roughly three runs in four, and
	// worth reading in the log rather than turning this lane red at random.
	final := lastLine(turn.Reply)
	for _, machinery := range []string{"op=propose", "`stand`", "when.kind", "when.in", "does.kind"} {
		if strings.Contains(final, machinery) {
			t.Errorf("the person's last line is the model's working out, not an answer (%q):\n%q", machinery, turn.Reply)
			break
		}
	}
	if strings.Contains(final, "?") {
		t.Errorf("the model asked again after the card was answered: %q", final)
	}
	if strings.Contains(turn.Reply, "\n") {
		t.Logf("MODEL DISCIPLINE: the reply carries a preamble the engine asked it not to write:\n%q", turn.Reply)
	}
	if len(final) > 240 {
		t.Errorf("the closing line is %d bytes, which is a paragraph: %q", len(final), final)
	}
}

// ── 2 ───────────────────────────────────────────────────────────────────────

func standingFiresIntoTheRoom(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	agent, _ := w.open(workspace, nil)

	w.say(agent, standWords, answerYes)
	item := w.onlyItem()

	// The conversation is still open, so this is road 1 of the delivery order:
	// the room the person is sitting in.
	wakes := agent.Wakes()
	pass := w.tick(item.NextDue.Add(5 * time.Second))
	if pass.Fired != 1 || pass.Said != 1 {
		t.Fatalf("the pass fired=%d said=%d, want 1 and 1 (notes %v)", pass.Fired, pass.Said, pass.Notes)
	}

	woken, ok := w.wokenTurn(agent, wakes, 4*time.Minute)
	if !ok {
		t.Fatalf("the firing never woke the conversation")
	}
	steered := "◦ " + item.Words + ":"
	entry, found := transcriptHas(agent, steered)
	if !found {
		t.Fatalf("the conversation never saw %q; transcript tail: %v", steered, tailOf(agent, 4))
	}
	if entry.Role != "aside" {
		t.Errorf("the steering line arrived as role %q, want aside — a firing is not the person typing", entry.Role)
	}
	t.Logf("STEERED IN → %q", entry.Text)
	t.Logf("THE ROOM ANSWERED → %q", woken.Reply)

	// The ledger, the wake log, and the item's own end.
	lines := w.ledgerLines(item.NextDue)
	if len(lines) == 0 {
		t.Fatalf("ledger-%s.jsonl has no line", item.NextDue.Format("2006-01-02"))
	}
	t.Logf("LEDGER %+v", lines)
	if wake := w.wakeLog(); len(wake) == 0 {
		t.Errorf("wake.log was never written")
	} else {
		t.Logf("WAKE LOG %v", wake)
	}
	after := w.itemFile(item.ID)
	if after.Status != standing.StatusRetired || after.RetiredWhy != "fired" {
		t.Errorf("after firing status=%q why=%q, want retired/fired", after.Status, after.RetiredWhy)
	}
	if after.Runs != 1 {
		t.Errorf("runs = %d, want 1", after.Runs)
	}
}

// ── 3 ───────────────────────────────────────────────────────────────────────

func standingInboxFold(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	agent, place := w.open(workspace, nil)

	w.say(agent, standWords, answerYes)
	item := w.onlyItem()

	// The window goes away. Nothing of this project is open, so the firing has
	// only the origin's own folder to write into.
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	w.tick(item.NextDue.Add(5 * time.Second))

	inbox := standing.InboxPath(place.Dir)
	raw, err := os.ReadFile(inbox)
	if err != nil {
		t.Fatalf("no inbox at %s: %v", inbox, err)
	}
	t.Logf("INBOX %s\n%s", inbox, strings.TrimSpace(string(raw)))

	// And it is folded in exactly once, by the next agent opened on that folder.
	reopened := w.openAt(workspace, place, nil)
	if _, err := os.Stat(inbox); !os.IsNotExist(err) {
		t.Errorf("the inbox survived the drain (%v)", err)
	}
	back := w.say(reopened, "in one short sentence: anything I missed?", nil)
	entry, found := transcriptHas(reopened, "while you were away")
	if !found {
		t.Fatalf("no while-you-were-away fold in the transcript; tail: %v", tailOf(reopened, 4))
	}
	if !strings.Contains(entry.Text, item.Words) {
		t.Errorf("the fold does not lead with the person's words: %q", entry.Text)
	}
	if entry.Role != "aside" {
		t.Errorf("the fold arrived as role %q, want aside", entry.Role)
	}
	t.Logf("FOLD → %q", entry.Text)
	t.Logf("THE MODEL READ IT BACK → %q", back.Reply)
}

// ── 4 ───────────────────────────────────────────────────────────────────────

func standingErrandReachesTheProject(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()

	// Home's `ask here`: a folder under the standing root's exchanges/, so home
	// never lists it, and Config.Errand, so it is never a steering target.
	exchanges := standing.ExchangesRoot(w.store.Root())
	if err := os.MkdirAll(exchanges, 0o700); err != nil {
		t.Fatalf("make exchanges/: %v", err)
	}
	errandPlace := w.place(exchanges, workspace)
	errand := w.openAt(workspace, errandPlace, func(cfg *session.Config) { cfg.Errand = true })

	w.say(errand, standWords, answerYes)
	item := w.onlyItem()
	if strings.TrimSpace(item.Origin.Exchange) == "" {
		t.Fatalf("the item made at home has no Origin.Exchange: %+v", item.Origin)
	}
	t.Logf("ORIGIN exchange=%s transcript=%s session=%s",
		item.Origin.Exchange, item.Origin.Transcript, item.Origin.SessionID)

	// Nothing open at all: the exchange's own folder is a dead letter office, so
	// the note has to land on the PROJECT.
	if err := errand.Close(); err != nil {
		t.Fatalf("close the errand: %v", err)
	}
	w.tick(item.NextDue.Add(5 * time.Second))

	projectInbox := standing.ProjectInboxPath(w.store.Root(), workspace)
	raw, err := os.ReadFile(projectInbox)
	if err != nil {
		t.Fatalf("no project inbox at %s: %v", projectInbox, err)
	}
	t.Logf("PROJECT INBOX %s\n%s", projectInbox, strings.TrimSpace(string(raw)))

	// A fresh ordinary conversation of that project folds it in.
	room, _ := w.open(workspace, nil)
	if _, err := os.Stat(projectInbox); !os.IsNotExist(err) {
		t.Errorf("the project inbox survived the drain (%v)", err)
	}
	back := w.say(room, "in one short sentence: anything I missed?", nil)
	if _, found := transcriptHas(room, "while you were away"); !found {
		t.Fatalf("the project's news never reached the conversation; tail: %v", tailOf(room, 4))
	}
	t.Logf("THE PROJECT'S NEWS WAS READ BACK → %q", back.Reply)

	// AND WITH BOTH OPEN, IT STEERS INTO THE ROOM AND NEVER INTO THE ERRAND.
	// This is the firing that wrote the rule (standing_run.go's header), so it
	// is driven directly through the runner the door builds.
	secondErrandPlace := w.place(exchanges, workspace)
	secondErrand := w.openAt(workspace, secondErrandPlace, func(cfg *session.Config) { cfg.Errand = true })
	live, _ := w.open(workspace, nil)
	wakes := live.Wakes()

	addressed := standing.Item{
		Words:     "remind me in 1 min to eat medicines",
		Workspace: workspace,
		Origin: standing.Origin{
			// The folder's name IS the session id (place.go), so this note is
			// addressed to the errand and to nothing else.
			SessionID:  secondErrandPlace.ID(),
			Exchange:   filepath.Join(w.store.ItemDir("nothing"), "exchange"),
			Transcript: filepath.Join(w.store.ItemDir("nothing"), "exchange", "transcript.jsonl"),
		},
	}
	runner := session.NewStandingRunner(w.posture(), w.store.Root())
	if _, err := runner.Say(context.Background(), addressed, "Time to eat your medicines."); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, ok := w.wokenTurn(live, wakes, 3*time.Minute); !ok {
		t.Fatalf("the room the person is sitting in was never told")
	}
	steered := "◦ " + addressed.Words + ": Time to eat your medicines."
	if _, found := transcriptHas(live, steered); !found {
		t.Errorf("the room never saw %q; tail: %v", steered, tailOf(live, 4))
	}
	if entry, found := transcriptHas(secondErrand, "◦ "); found {
		t.Errorf("the errand was steered into: %q", entry.Text)
	}
	// AND NOTHING NEW STOOD. The woken turn declines whatever card it raises
	// ([answerNo]), so the store still holds exactly the item this scenario
	// made — a firing read out in a room must not arm a second one by itself.
	if items, err := w.store.List(); err != nil {
		t.Fatalf("list: %v", err)
	} else if len(items) != 1 {
		t.Errorf("the store holds %d items after a firing was read out, want 1", len(items))
	}
	t.Logf("the errand's transcript stayed clean; the room heard the news")
}

// ── 5 ───────────────────────────────────────────────────────────────────────

func standingProbeWatch(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	flag := filepath.Join(workspace, "flag")
	agent, _ := w.open(workspace, nil)

	words := "tell me when the file " + flag + " says red"
	turn := w.say(agent, words, answerYes)

	stand, found := turn.named("stand")
	if !found {
		t.Fatalf("the model never called stand; it called %v and said %q", turn.names(), turn.Reply)
	}
	item := w.onlyItem()
	if item.When.Kind != standing.WhenProbe {
		t.Fatalf("when.kind = %q, want probe (call was %s)", item.When.Kind, stand.Args)
	}
	if strings.TrimSpace(item.When.Probe.Command) == "" {
		t.Fatalf("the probe carries no command: %+v", item.When.Probe)
	}
	t.Logf("PROBE command=%q every=%s hint=%q says=%q",
		item.When.Probe.Command, item.When.ProbeEvery, item.When.Hint, item.Does.Say)

	// THE SENTINEL'S OWN ROUTING, before a call is made: the role is registered
	// at the low tier and resolves through the person's settings.
	source := w.rolesSource(workspace)
	resolved, err := roles.Resolve(source, roles.Role("sentinel"), w.posture().Model)
	if err != nil {
		t.Fatalf("resolve the sentinel role: %v", err)
	}
	tier, _ := roles.TierModel(source, roles.TierLow)
	if resolved != tier {
		t.Errorf("the sentinel resolved to %q but the low tier holds %q", resolved, tier)
	}
	t.Logf("SENTINEL ROLE → tier=low model=%q", resolved)

	// The flag is not there: the check is quiet and nothing is written anywhere
	// but on the item.
	first := time.Now()
	pass := w.tick(first)
	if pass.Fired != 0 {
		t.Fatalf("a check with no flag fired %d times", pass.Fired)
	}
	quiet := w.itemFile(item.ID)
	if strings.TrimSpace(quiet.LastCheckLine) == "" {
		t.Errorf("the item does not remember that it looked: %+v", quiet)
	}
	if quiet.Runs != 0 {
		t.Errorf("runs = %d after a quiet check, want 0", quiet.Runs)
	}
	t.Logf("QUIET CHECK → lastChecked=%s line=%q", quiet.LastChecked.Format(time.RFC3339), quiet.LastCheckLine)

	// The world changes.
	if err := os.WriteFile(flag, []byte("red\n"), 0o600); err != nil {
		t.Fatalf("write the flag: %v", err)
	}
	next := quiet.NextDue.Add(5 * time.Second)
	pass = w.tick(next)
	if pass.Fired != 1 || pass.Said != 1 {
		t.Fatalf("the flag turned red and the pass fired=%d said=%d", pass.Fired, pass.Said)
	}
	fired := w.itemFile(item.ID)
	if fired.Runs != 1 {
		t.Errorf("runs = %d, want 1", fired.Runs)
	}
	if fired.LastOutcome != "said" {
		t.Errorf("lastOutcome = %q, want said", fired.LastOutcome)
	}
	t.Logf("FIRED → line=%q outcome=%q previous=%v", fired.LastCheckLine, fired.LastOutcome, fired.Previous)
}

// ── 6 ───────────────────────────────────────────────────────────────────────

func standingRhythmLifecycle(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	agent, _ := w.open(workspace, nil)

	turn := w.say(agent, "every 2 minutes tell me the time", answerYes)
	if _, found := turn.named("stand"); !found {
		t.Fatalf("the model never called stand; it called %v and said %q", turn.names(), turn.Reply)
	}
	item := w.onlyItem()
	if item.When.Kind != standing.WhenEvery {
		t.Fatalf("when.kind = %q, want every", item.When.Kind)
	}
	if strings.TrimSpace(item.When.Every) == "" {
		t.Fatalf("when.every is empty: %+v", item.When)
	}
	t.Logf("RHYTHM every=%q words=%q nextDue=%s", item.When.Every, item.When.Words, item.NextDue.Format(time.RFC3339))

	for _, step := range []struct {
		say  string
		want standing.Status
	}{
		{"pause that", standing.StatusPaused},
		{"resume it", standing.StatusActive},
		{"stop it", standing.StatusRetired},
	} {
		turn := w.say(agent, step.say, answerYes)
		if _, found := turn.named("stand"); !found {
			t.Errorf("%q did not reach the stand tool; calls were %v, reply %q", step.say, turn.names(), turn.Reply)
		}
		got := w.itemFile(item.ID)
		if got.Status != step.want {
			t.Errorf("after %q status = %q, want %q (reply %q)", step.say, got.Status, step.want, turn.Reply)
		} else {
			t.Logf("%-10q → status=%s  reply=%q", step.say, got.Status, turn.Reply)
		}
	}

	listing := w.say(agent, "list what stands here", answerYes)
	call, found := listing.named("stand")
	if !found {
		t.Fatalf("listing never reached the tool; calls were %v", listing.names())
	}
	if !strings.Contains(call.Args, `"list"`) {
		t.Errorf("op was not list: %s", call.Args)
	}
	if !strings.Contains(call.Output, item.Words) {
		t.Errorf("op=list did not name the item: %q", call.Output)
	}
	t.Logf("LIST → %s", strings.TrimSpace(call.Output))
}

// ── 7 ───────────────────────────────────────────────────────────────────────

func standingTaskFiring(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	agent, _ := w.open(workspace, nil)

	words := "in 2 minutes, actually run `echo hi > done.txt` in this folder — do the work, don't just remind me"
	turn := w.say(agent, words, answerYes)
	if _, found := turn.named("stand"); !found {
		t.Fatalf("the model never called stand; it called %v and said %q", turn.names(), turn.Reply)
	}
	item := w.onlyItem()
	if item.Does.Kind != standing.ActionTask {
		t.Fatalf("does.kind = %q, want task — the brief was %q", item.Does.Kind, item.Does.Brief)
	}
	t.Logf("TASK ITEM brief=%q acceptance=%q maxSteps=%d rails=$%.2f×%d",
		item.Does.Brief, item.Does.Acceptance, item.Does.MaxSteps, item.Rails.PerRunUSD, item.Rails.MaxPerDay)

	at := item.NextDue.Add(5 * time.Second)
	pass := w.tick(at)
	if pass.Fired != 1 {
		t.Fatalf("the pass fired=%d, want 1 (notes %v)", pass.Fired, pass.Notes)
	}
	fired := w.itemFile(item.ID)
	if fired.Runs != 1 {
		t.Errorf("runs = %d, want 1", fired.Runs)
	}
	runs, err := os.ReadDir(w.store.RunsDir(item.ID))
	if err != nil || len(runs) == 0 {
		t.Fatalf("no run folder under %s: %v", w.store.RunsDir(item.ID), err)
	}
	runDir := filepath.Join(w.store.RunsDir(item.ID), runs[0].Name())
	entries, _ := os.ReadDir(runDir)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Logf("RUN %s holds %v", runDir, names)
	if came, err := os.ReadFile(filepath.Join(runDir, standing.CameTo)); err == nil {
		t.Logf("CAME TO → %s", strings.TrimSpace(string(came)))
	}
	t.Logf("OUTCOME %q needsPerson=%q lastRun=%s", fired.LastOutcome, fired.NeedsPerson, fired.LastRun)

	var spend float64
	for _, line := range w.ledgerLines(at) {
		t.Logf("LEDGER %s %s $%.6f %s", line.At.Format("15:04:05"), line.Kind, line.USD, line.Run)
		spend += line.USD
	}
	if spend <= 0 {
		t.Errorf("the ledger recorded no spend for a firing that ran a whole session")
	}
	w.bill("firing", spend)
	if made, err := os.ReadFile(filepath.Join(workspace, "done.txt")); err == nil {
		t.Logf("THE WORK LANDED: done.txt = %q", strings.TrimSpace(string(made)))
	} else {
		t.Logf("done.txt was not written in the workspace (%v) — the run's own account is above", err)
	}
}

// ── 8 ───────────────────────────────────────────────────────────────────────

// standingDailyRail is the one scenario with no model in it, deliberately: the
// rail is arithmetic over the ledger and putting a model in front of it would
// only make the failure harder to read.
func standingDailyRail(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()

	item, err := w.store.Create(standing.Item{
		Words:     "every minute, say the word",
		Workspace: workspace,
		When:      standing.When{Kind: standing.WhenEvery, Every: "1m", Words: "every minute"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "the word"},
		Rails:     standing.Rails{PerRunUSD: 0.01, MaxPerDay: 1},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first := w.tick(item.NextDue.Add(5 * time.Second))
	if first.Fired != 1 {
		t.Fatalf("the first pass fired=%d, want 1 (notes %v)", first.Fired, first.Notes)
	}
	after := w.itemFile(item.ID)
	second := w.tick(after.NextDue.Add(5 * time.Second))
	if second.Fired != 0 {
		t.Errorf("the second pass fired=%d, want 0 — max_per_day is 1", second.Fired)
	}
	if second.Skipped != 1 {
		t.Errorf("the second pass skipped=%d, want 1", second.Skipped)
	}
	lines := w.wakeLog()
	if len(lines) < 2 {
		t.Fatalf("wake.log has %d lines, want 2: %v", len(lines), lines)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "skipped=1") || !strings.Contains(last, "fired=0") {
		t.Errorf("the second wake line is %q, want fired=0 and skipped=1", last)
	}
	t.Logf("WAKE LOG\n  %s", strings.Join(lines, "\n  "))
	held := w.itemFile(item.ID)
	t.Logf("HELD BACK → %q", held.LastCheckLine)
	if !strings.Contains(held.LastCheckLine, "already run today") {
		t.Errorf("the item does not say why it was held: %q", held.LastCheckLine)
	}
}

// ── 9 ───────────────────────────────────────────────────────────────────────

func standingUnwatchedRefuses(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()
	// The ambient seam is still there — the tool is on the belt — and the ONE
	// thing missing is somebody to answer. That is the belt-and-braces half of
	// the law tools_standing.go states.
	agent, _ := w.open(workspace, func(cfg *session.Config) { cfg.AskConsent = false })

	turn := w.say(agent, standWords, answerYes)
	stand, found := turn.named("stand")
	if !found {
		t.Fatalf("the model never called stand; it called %v and said %q", turn.names(), turn.Reply)
	}
	if !strings.Contains(stand.Output, "nobody is here to say yes") {
		t.Fatalf("the engine did not refuse in its own words: %q", stand.Output)
	}
	if len(turn.Proposals) != 0 {
		t.Errorf("a card was drawn into an empty room: %+v", turn.Proposals)
	}
	items, err := w.store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("something stands after a refusal: %+v", items)
	}
	t.Logf("REFUSAL → %q", stand.Output)
	t.Logf("THE MODEL REPORTED → %q", turn.Reply)
	if turn.Reply == "" {
		t.Errorf("the model said nothing about a refusal the person needs to hear")
	}
}

// ── 10 ──────────────────────────────────────────────────────────────────────

// standingPastMoment is the card's past-guard, and it SKIPS when that lane has
// not landed: `in` and `at` are both accepted as written in this build, so a
// moment already gone is armed and fires on the next pass. The assertion is
// written out so the lane's own merge turns it on rather than needing writing.
func standingPastMoment(t *testing.T) {
	if !pastGuardLanded() {
		t.Skip("the past-guard lane has not merged: nothing in tools_standing.go refuses a moment that has passed")
	}
	w := newWorld(t)
	workspace := t.TempDir()
	agent, _ := w.open(workspace, nil)

	turn := w.say(agent, "remind me at 00:01 today to drink water", answerYes)
	// TWO HONEST ROADS, AND THE TEST TAKES EITHER. A model that reads the Now
	// line can see for itself that 00:01 has gone and say so without touching
	// the tool; one that does call stand is refused with the time it is now and
	// may re-propose a moment ahead. What is NOT allowed is the third road the
	// person's own transcript showed: a card for a moment already behind them.
	stand, called := turn.named("stand")
	if !called {
		lower := strings.ToLower(turn.Reply)
		if !strings.Contains(lower, "passed") && !strings.Contains(lower, "already") && !strings.Contains(lower, "ago") {
			t.Fatalf("the model neither called stand nor said the moment has gone: %q", turn.Reply)
		}
		if items, _ := w.store.List(); len(items) != 0 {
			t.Fatalf("nothing should stand for a moment that has gone; found %d", len(items))
		}
		t.Logf("DECLINED BEFORE THE TOOL → %q", turn.Reply)
		return
	}
	if !strings.Contains(strings.ToLower(stand.Output), "passed") {
		// The model may already have computed a future moment from the Now
		// line (tomorrow 00:01); that is the guard working upstream of itself.
		item := w.onlyItem()
		if !item.When.At.After(time.Now()) {
			t.Fatalf("the engine took a moment that has gone: %q (at %s)", stand.Output, item.When.At)
		}
		t.Logf("PROPOSED A FUTURE MOMENT OUTRIGHT → %s", item.When.At.Format(time.RFC3339))
		return
	}
	if !strings.Contains(stand.Output, "now") {
		t.Errorf("the refusal did not carry the now line: %q", stand.Output)
	}
	t.Logf("REFUSAL → %q", stand.Output)
	if items, _ := w.store.List(); len(items) == 1 {
		if !items[0].When.At.After(time.Now()) {
			t.Errorf("the model re-proposed a moment that is still in the past: %s", items[0].When.At)
		}
		t.Logf("RE-PROPOSED → %s", items[0].When.At.Format(time.RFC3339))
	}
}

// pastGuardLanded reads the engine's own refusal out of the source it would be
// written in, so this file needs no edit when the lane merges.
func pastGuardLanded() bool {
	raw, err := os.ReadFile("../session/tools_standing.go")
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "moment that has passed")
}

// ── 11 ──────────────────────────────────────────────────────────────────────

func standingSweep(t *testing.T) {
	w := newWorld(t)
	workspace := t.TempDir()

	item, err := w.store.Create(standing.Item{
		Words:     "keep an eye on the build",
		Workspace: workspace,
		When:      standing.When{Kind: standing.WhenEvery, Every: "1h", Words: "hourly"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "the build changed"},
		Rails:     standing.Rails{PerRunUSD: 0.01, MaxPerDay: 4},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Two run folders: one that came to nothing, one that said something. The
	// marker is the sweep's whole licence (standing.RunCameToNothing).
	empty := filepath.Join(w.store.RunsDir(item.ID), "0001")
	kept := filepath.Join(w.store.RunsDir(item.ID), "0002")
	for dir, came := range map[string]string{empty: standing.OutcomeNothing, kept: "said"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, standing.CameTo), []byte(came+"\n"), 0o600); err != nil {
			t.Fatalf("mark %s: %v", dir, err)
		}
	}

	// Past RunKeep, asked as a moment rather than by backdating the tree: the
	// sweep takes its now as an argument for exactly this reason.
	var notes []string
	session.SweepStanding(w.store.Root(), time.Now().Add(standing.RunKeep+24*time.Hour),
		func(line string) { notes = append(notes, line) })
	t.Logf("SWEEP notes=%v", notes)

	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Errorf("the run that came to nothing survived the sweep (%v)", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the run that said something was reaped: %v", err)
	}
	if _, err := os.Stat(w.store.ItemPath(item.ID)); err != nil {
		t.Errorf("the sweep took the item itself: %v", err)
	}
	still, err := w.store.Get(item.ID)
	if err != nil {
		t.Fatalf("the item is gone: %v", err)
	}
	t.Logf("KEPT item %s status=%s; runs/ now holds the run that delivered something", still.ID, still.Status)
}

// tailOf is the last few display rows of a conversation, for a failure message
// that has to say what the transcript actually holds.
func tailOf(agent *session.Agent, count int) []string {
	entries := agent.Transcript()
	if len(entries) > count {
		entries = entries[len(entries)-count:]
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Role+": "+shorten(entry.Text, 160))
	}
	return out
}

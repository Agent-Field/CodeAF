//go:build e2e

package e2e

// The chat door onto ongoing work, end to end: a person says "keep an eye on my
// inbox folder and keep reports/inbox-report.md current" in a conversation,
// answers one card, and the item that results is the one `aforge standing add`
// makes — the same instructions version, the same owner-published report, the
// same placement and rules, the same check before publication, the same
// receipts and withheld codes, and the same `aforge standing show`.
//
// THE CONVERSATION RUNS IN THIS PROCESS, THROUGH THE CHAT'S OWN SEAMS. There is
// no headless door into a conversation that can answer a card (`chat --once`
// removes the ambient side by law), so the conversation is a session.Agent
// built on the same two seams the chat door hands it — the standing store and
// the folder database under this journey's AFORGE_HOME — and the card is
// answered through ResolveStanding, the road every surface's answer takes.
// Everything after the yes is the shipped binary: `aforge standing check` runs
// the pass the timer runs, and `aforge standing show` is the record.
//
// TestChatDoorJourney's model is a script (localwork_e2e_test.go's), so it
// proves aforge's half and not a model's judgment. TestRealChatDoorJourney is
// the same road once with a real model choosing to call `stand` itself.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// chatSentence is the person's own sentence, the journey's whole input.
const chatSentence = "keep an eye on my inbox folder and keep reports/inbox-report.md current"

// chatInstructions is the work the scripted conversation writes for it — the
// same instructions the terminal twin is given, word for word.
const chatInstructions = "Read the changed files in inbox/ and write a short report of new decisions and requests. FOCUS: decisions"

// chatID is the conversation's id: the folder its journal lives in.
const chatID = "chat-inbox-door"

// chatDoor is one conversation on the chat door, in this process, over the
// journey's home.
type chatDoorSession struct {
	agent *session.Agent
	store *standing.Store
}

// openChatDoor builds the conversation the way the chat door does: the
// standing store and the folder database under AFORGE_HOME, somebody watching
// (a card can be answered), and no timer on this host.
func openChatDoor(t *testing.T, home, project, baseURL, apiKey, model string) chatDoorSession {
	t.Helper()
	store, err := standing.Open(filepath.Join(home, "v3", "standing"))
	must(t, err)
	place := session.Place{Dir: filepath.Join(home, "v3", "projects", "chatdoor", chatID), Workspace: project}
	must(t, os.MkdirAll(place.Dir, 0o700))
	agent, err := session.New(session.Config{
		Workspace:    project,
		Place:        place,
		SessionFile:  place.Transcript(),
		Model:        model,
		APIKey:       apiKey,
		BaseURL:      baseURL,
		ProfileDir:   home,
		AskConsent:   true,
		Standing:     &session.Standing{Store: store},
		Organization: &session.Organization{Path: filepath.Join(home, "v3", "collections.db")},
	})
	must(t, err)
	t.Cleanup(func() { _ = agent.Close() })
	return chatDoorSession{agent: agent, store: store}
}

// say runs one turn of the person's sentence, answers the card yes, and hands
// back the card and every line of news the turn said.
func (c chatDoorSession) say(t *testing.T, words string, turn time.Duration) (session.StandingNotice, []session.StandingNotice, string) {
	t.Helper()
	events, err := c.agent.Submit(context.Background(), words)
	must(t, err)
	var card *session.StandingNotice
	var news []session.StandingNotice
	var said strings.Builder
	deadline := time.After(turn)
	for {
		select {
		case event, open := <-events:
			if !open {
				if card == nil {
					t.Fatalf("the conversation drew no card; it said: %s", said.String())
				}
				return *card, news, said.String()
			}
			switch event.Kind {
			case session.EventStandingProposal:
				shown := *event.Standing
				card = &shown
				c.agent.ResolveStanding(event.Standing.ID, session.StandingAnswer{Approved: true})
			case session.EventStandingUpdate:
				news = append(news, *event.Standing)
			case session.EventTextDelta:
				said.WriteString(event.Text)
			}
		case <-deadline:
			t.Fatalf("the conversation's turn did not finish in %s; it said: %s", turn, said.String())
		}
	}
}

// chatItem is the one piece of work the chat set up.
func (c chatDoorSession) chatItem(t *testing.T) standing.Item {
	t.Helper()
	items, err := c.store.List()
	must(t, err)
	for _, item := range items {
		if item.Adoption != nil && item.Adoption.Via == standing.DoorChat {
			return item
		}
	}
	t.Fatal("nothing stands through the chat")
	return standing.Item{}
}

func TestChatDoorJourney(t *testing.T) {
	binary := filepath.Join(repoRoot(t), "bin", "aforge")
	if _, err := os.Stat(binary); err != nil {
		t.Fatal("bin/aforge is missing: run make build first (this journey drives the shipped binary)")
	}
	model := newScriptedModel(t)
	home, project := keptDir(t, "chatdoor-home"), keptDir(t, "chatdoor-project")
	profile, _ := json.Marshal(map[string]string{config.KeyMemoryEnabled: "off", config.KeyStandingBackground: "off"})
	must(t, os.WriteFile(filepath.Join(home, "config.json"), profile, 0o600))
	must(t, os.MkdirAll(filepath.Join(project, "inbox"), 0o755))
	// The in-process conversation writes its ledger and journal under this
	// home, never the person's.
	t.Setenv("AFORGE_HOME", home)
	j := &journey{t: t, binary: binary, home: home, project: project, env: scriptedEnv(home, model.url)}

	// ── folders, rules, and the conversation placed in Launch ────────────
	launch, marketing := j.folder("Launch"), j.folder("Marketing")
	j.ok("standing", "add", "--hold", "--words", ruleLaunch+": inbox reports never quote email addresses; write [redacted] instead.", "--scope", launch, "--workspace", project)
	j.ok("standing", "add", "--hold", "--words", ruleMarketing+": marketing notes must not promise offline support unless product/spec.md says it is supported.", "--scope", marketing, "--workspace", project)
	j.ok("collections", "place", launch, "conversation", chatID)

	// The terminal refuses a report folder that leads out of the project,
	// the refusal the chat's card makes too (session.CheckStandingReport).
	outside := t.TempDir()
	must(t, os.Symlink(outside, filepath.Join(project, "escape")))
	if out, err := j.run("standing", "add", "--words", "x", "--instructions", "x", "--watch", "inbox/*", "--report", "escape/r.md", "--workspace", project); err == nil || !strings.Contains(out, "resolves outside the project") {
		t.Fatalf("the terminal set up a report through a link out of the project: %v\n%s", err, out)
	}

	// ── the chat: one sentence, one card, one yes ────────────────────────
	stand, _ := json.Marshal(map[string]any{
		"op": "propose", "words": chatSentence, "when_words": "when inbox/* changes",
		"when": map[string]any{"kind": "file", "glob": "inbox/*"},
		"does": map[string]any{"kind": "task", "instructions": chatInstructions, "report": "reports/inbox-report.md"},
	})
	model.converse = func(ask, answered string, tools []string) (string, string, string, bool) {
		if !strings.Contains(ask, chatSentence) || !contains(tools, "stand") {
			return "", "", "", false
		}
		if answered == "" {
			return "stand", string(stand), "", true
		}
		return "", "", "", true // the one short line after the yes; the script says nothing
	}
	chat := openChatDoor(t, home, project, model.url+"/api/v1", "scripted-model-no-network", "stub/scripted")
	card, news, _ := chat.say(t, chatSentence, time.Minute)
	t.Logf("CARD when · %s\nCARD costs · %s\nCARD %s", card.WhenWords, card.CostWords, strings.Join(card.Terms, "\nCARD "))
	wantTerms := []string{
		"does · " + chatInstructions,
		"report · reports/inbox-report.md — aforge publishes this file; the run never writes it",
		"folder · Launch, where this conversation is placed — its rules reach every run",
		"rule · " + ruleLaunch + ": inbox reports never quote email addresses; write [redacted] instead.",
	}
	if strings.Join(card.Terms, "\n") != strings.Join(wantTerms, "\n") {
		t.Fatalf("the card said:\n%s\nwant:\n%s", strings.Join(card.Terms, "\n"), strings.Join(wantTerms, "\n"))
	}
	// NO TIMER ON THIS HOST (the conversation was given none), SAID TRUTHFULLY.
	var background string
	for _, n := range news {
		if n.Update == "background" {
			background = n.Text
		}
	}
	if background != "no background checks on this machine · checked only while an aforge window is open, or when you run aforge standing check" {
		t.Fatalf("the no-timer line = %q", background)
	}

	inbox := chat.chatItem(t).ID
	// The terminal's twin: the same sentence, instructions, watch, report path
	// and folder, set up whole at the terminal in a project of its own so the
	// two never share a file.
	twinProject := keptDir(t, "chatdoor-twin")
	must(t, os.MkdirAll(filepath.Join(twinProject, "inbox"), 0o755))
	tj := &journey{t: t, binary: binary, home: home, project: twinProject, env: j.env}
	var made struct {
		ID string `json:"id"`
	}
	out := tj.ok("standing", "add", "--json", "--words", chatSentence, "--instructions", chatInstructions, "--watch", "inbox/*", "--report", "reports/inbox-report.md", "--place", launch, "--workspace", twinProject)
	must(t, json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &made))
	twin := made.ID
	chatJSON, twinJSON := j.item(inbox), j.item(twin)
	for _, field := range []string{"schema", "specRevision", "when", "does", "rails", "status"} {
		if string(chatJSON[field]) != string(twinJSON[field]) {
			t.Fatalf("the chat's %s is %s, the terminal's is %s", field, chatJSON[field], twinJSON[field])
		}
	}
	both := func(rel, text string) {
		j.write(rel, text)
		tj.write(rel, text)
	}

	// ── the pass the timer runs: baseline, then a change ──────────────────
	j.check()
	j.expectRuns(inbox, 0)
	j.expectRuns(twin, 0)
	both("inbox/a.md", "Decision: ship Friday. Ask alice@example.com to confirm the venue.\n")
	j.check()
	for _, door := range []struct {
		id string
		j  *journey
	}{{inbox, j}, {twin, tj}} {
		run := j.expectRuns(door.id, 1)[0]
		j.expectRun(run, runExpect{spec: 1, attempt: 1, outcome: "landed", changes: []string{"added inbox/a.md"}})
		door.j.expectPublished(run, "reports/inbox-report.md")
		j.expectCause(run, []string{ruleLaunch}, []string{ruleMarketing})
		if c := run.RuleCheck; c == nil || c.Verdict != "kept" || !c.Rewrote || !strings.Contains(c.First, "alice@example.com") {
			t.Fatalf("%s's rule check: %+v", door.id, run.RuleCheck)
		}
		if got := door.j.read("reports/inbox-report.md"); strings.Contains(got, "alice@example.com") || !strings.Contains(got, "[redacted]") || !strings.Contains(got, "rules seen: "+ruleLaunch) {
			t.Fatalf("%s published:\n%s", door.id, got)
		}
	}

	// ── a report that still breaks the rule is withheld, for both ─────────
	kept := j.read("reports/inbox-report.md")
	both("inbox/f.md", "Request: KEEP-RAW ask bob@example.com for the keys.\n")
	if out, code := j.checkExit(); code != 4 || !strings.Contains(out, "2 need you") {
		t.Fatalf("two held reports did not make the check exit unanswered (4): code %d\n%s", code, out)
	}
	for _, id := range []string{inbox, twin} {
		if o := j.expectRuns(id, 2)[0]; o.Outcome != "needs-you" || o.Withheld != "held-by-rules" || o.Published != nil {
			t.Fatalf("%s's held run: %+v", id, o.occurrenceView)
		}
	}
	if j.read("reports/inbox-report.md") != kept {
		t.Fatal("a held report replaced the chat's last good one")
	}

	// ── and the record reads the same whichever door made it ──────────────
	chatShow, twinShow := j.ok("standing", "show", inbox), j.ok("standing", "show", twin)
	t.Logf("SHOW chat-made:\n%s", chatShow)
	t.Logf("SHOW terminal-made:\n%s", twinShow)
	if !strings.Contains(chatShow, "set up by: person, through the chat") || !strings.Contains(twinShow, "set up by: person, through the terminal") {
		t.Fatal("show does not name the door each item came through")
	}
	if a, b := sameShape(chatShow, inbox, project), sameShape(twinShow, twin, twinProject); a != b {
		t.Fatalf("the two records differ beyond their id, project folder, door and times:\n--- chat\n%s\n--- terminal\n%s", a, b)
	}
	t.Logf("CHATDOOR RECEIPT scripted model firings=%d rule checks=%d (scripted model, not live-model acceptance)", model.firings.Load(), model.checks.Load())
}

// item is one item's document as `standing show --json` prints it, by field.
func (j *journey) item(id string) map[string]json.RawMessage {
	j.t.Helper()
	var record struct {
		Item map[string]json.RawMessage `json:"item"`
	}
	must(j.t, json.Unmarshal([]byte(j.ok("standing", "show", id, "--json")), &record))
	return record.Item
}

var (
	showStamp = regexp.MustCompile(`\d{4}-\d\d-\d\dT[0-9:.]+(Z|[+-]\d\d:\d\d)|[A-Z][a-z]{2} \d{1,2} \d\d:\d\d(:\d\d)?`)
	showHash  = regexp.MustCompile(`sha256 [0-9a-f]{12}`)
)

// sameShape is a show record with what may differ between two doors' items —
// the id, the project folder, the door, the moments — written as tokens, so
// two records can be compared for everything else. Run folders are numbered per
// item, so the two items' first runs share a number and need no token.
func sameShape(show, id, workspace string) string {
	show = strings.ReplaceAll(show, id, "<id>")
	show = strings.ReplaceAll(show, workspace, "<workspace>")
	for _, door := range []string{"through the chat", "through the terminal"} {
		show = strings.ReplaceAll(show, door, "through <door>")
	}
	for _, door := range []string{"set up in the chat", "set up at the terminal"} {
		show = strings.ReplaceAll(show, door, "set up <door>")
	}
	show = showStamp.ReplaceAllString(show, "<when>")
	show = showHash.ReplaceAllString(show, "sha256 <hash>")
	return show
}

// TestRealChatDoorJourney is the same road once with a real model: it reads the
// person's sentence, decides for itself to call `stand`, and writes the item;
// the card is answered yes; the shipped binary runs the pass on the same model;
// and the report must be published with its receipt, keep the Launch rule, and
// read in `standing show` as set up through the chat. One call at a time, a
// daily limit of $2 on the home, and it SKIPS without OPENROUTER_API_KEY.
//
//	AFORGE_LOCALWORK_KEEP=/tmp/opus-localwork/chatdoor-live1 \
//	  go test -tags e2e -count=1 -timeout 20m -run '^TestRealChatDoorJourney$' -v ./internal/e2e/
func TestRealChatDoorJourney(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("no OPENROUTER_API_KEY")
	}
	binary := filepath.Join(repoRoot(t), "bin", "aforge")
	if _, err := os.Stat(binary); err != nil {
		t.Fatal("bin/aforge is missing: run make build first")
	}
	model := os.Getenv("AFORGE_CHATDOOR_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-flash"
	}
	home, project := keptDir(t, "home"), keptDir(t, "project")
	profile, _ := json.Marshal(map[string]string{config.KeyMemoryEnabled: "off", config.KeyStandingBackground: "off"})
	must(t, os.WriteFile(filepath.Join(home, "config.json"), profile, 0o600))
	must(t, os.MkdirAll(filepath.Join(project, "inbox"), 0o755))
	t.Setenv("AFORGE_HOME", home)
	env := scriptedEnv(home, "https://openrouter.ai")
	for at, kv := range env {
		switch {
		case strings.HasPrefix(kv, "AFORGE_BASE_URL="):
			env[at] = "AFORGE_DAILY_BUDGET=2"
		case strings.HasPrefix(kv, "AFORGE_MODEL="):
			env[at] = "AFORGE_MODEL=" + model
		case strings.HasPrefix(kv, "OPENROUTER_API_KEY="):
			env[at] = "OPENROUTER_API_KEY=" + key
		}
	}
	j := &journey{t: t, binary: binary, home: home, project: project, env: env}
	defer func() { t.Logf("LIVE SPEND %s", ledgerSpend(home)) }()

	launch := j.folder("Launch")
	j.ok("standing", "add", "--hold", "--words", "Inbox reports never quote email addresses or phone numbers; write [redacted] instead.", "--scope", launch, "--workspace", project)
	j.ok("collections", "place", launch, "conversation", chatID)

	chat := openChatDoor(t, home, project, "https://openrouter.ai/api/v1", key, model)
	card, news, said := chat.say(t, chatSentence, 4*time.Minute)
	t.Logf("LIVE CARD words: %s\nwhen · %s\ncosts · %s\n%s", card.Item.Words, card.WhenWords, card.CostWords, strings.Join(card.Terms, "\n"))
	t.Logf("LIVE CHAT said after the yes: %s", said)
	for _, n := range news {
		t.Logf("LIVE NEWS %s · %s", n.Update, n.Text)
	}
	item := chat.chatItem(t)
	t.Logf("LIVE ITEM %s when=%+v does.report=%q instructions=%q", item.ID, item.When, item.Does.Report, item.Does.Brief)
	if item.When.Kind != standing.WhenFile || item.Does.Kind != standing.ActionTask || item.Does.Report != "reports/inbox-report.md" || item.SpecRevision != 1 {
		t.Fatalf("the model's item is not the inbox report work: %+v", item)
	}
	if card.Terms[1] != "report · reports/inbox-report.md — aforge publishes this file; the run never writes it" ||
		!strings.HasPrefix(card.Terms[2], "folder · Launch, where this conversation is placed") || !strings.Contains(strings.Join(card.Terms, "\n"), "rule · Inbox reports never quote") {
		t.Fatalf("the card did not say what governs the work:\n%s", strings.Join(card.Terms, "\n"))
	}

	j.check()
	j.expectRuns(item.ID, 0)
	j.write("inbox/today.md", "Decision: launch moves to Friday.\nRequest: Priya (priya@example.com, +1 555 0100) to confirm the venue.\nRequest: nobody owns the press release yet.\n")
	out, code := j.checkExit()
	t.Logf("LIVE CHECK exit %d: %s", code, out)
	show := j.ok("standing", "show", item.ID)
	t.Logf("LIVE SHOW:\n%s", show)
	if code != 0 {
		t.Fatalf("the live pass did not finish clean (exit %d)", code)
	}
	run := j.expectRuns(item.ID, 1)[0]
	j.expectRun(run, runExpect{spec: 1, attempt: 1, outcome: "landed", changes: run.changeList()})
	j.expectPublished(run, "reports/inbox-report.md")
	report := j.read("reports/inbox-report.md")
	t.Logf("LIVE REPORT:\n%s", report)
	if strings.Contains(report, "priya@example.com") || strings.Contains(report, "555 0100") {
		t.Fatal("a raw contact detail was published against the Launch rule")
	}
	if !strings.Contains(show, "set up by: person, through the chat") || !strings.Contains(show, "folder: "+launch+" Launch (placed directly)") {
		t.Fatalf("show does not carry the chat's receipt and placement:\n%s", show)
	}
}

// changeList is a run's changes as expectRun compares them.
func (r runRecord) changeList() []string {
	var out []string
	for _, change := range r.Changes {
		out = append(out, change.Kind+" "+change.Path)
	}
	return out
}

// ledgerSpend is every model call this home paid for, from its usage ledger —
// the conversation's and the passes' alike.
func ledgerSpend(home string) string {
	raw, err := os.ReadFile(filepath.Join(home, "v3", "usage.jsonl"))
	if err != nil {
		return "no ledger: " + err.Error()
	}
	total, calls := 0.0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row struct {
			USD   float64 `json:"usd"`
			Calls int     `json:"calls"`
		}
		if json.Unmarshal([]byte(line), &row) == nil {
			total += row.USD
			calls += row.Calls
		}
	}
	return fmt.Sprintf("$%.4f over %d calls (this home's usage ledger)", total, calls)
}

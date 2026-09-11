//go:build e2e

package e2e

// PERSON-LIKE CHAT VALIDATION DRIVER. It is not a gate: it needs a real model
// and a key, it is judged by a person reading its record, and every scenario
// is one live run that scripts/pai-chatvalidate.sh starts and budgets.
//
// Every scenario is a conversation a person has: sentences typed the way a
// person types them (no flags, no ids), every card answered yes the way a
// person who asked for the thing would, every consent allowed, and any task
// proposal DECLINED (a person who asked for ongoing work did not ask for a
// one-off task, and a task left alone approves itself on its clock). The
// conversation is a real session.Agent on the chat door's two seams, exactly
// as chatdoor_e2e_test.go opens it; everything after the yes is the shipped
// binary (`aforge standing check` is the pass the timer runs).
//
// One scenario per process: PAI_SCEN names it, AFORGE_LOCALWORK_KEEP names the
// kept folder. The last line is `CHATVAL {json}` with every check, the item
// each check counts toward, every turn, and this home's spend.
//
//	PAI_SCEN=h1-inbox AFORGE_LOCALWORK_KEEP=/tmp/pai-chat/h1-inbox/run01 \
//	  go test -tags e2e -count=1 -timeout 40m -run '^TestPAIChat$' -v ./internal/e2e/

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

const paiModel = "deepseek/deepseek-v4-flash"

type cvCheck struct {
	Items  []string `json:"items"`
	Name   string   `json:"name"`
	Pass   bool     `json:"pass"`
	Detail string   `json:"detail,omitempty"`
}

type cvCard struct {
	Words string   `json:"words"`
	When  string   `json:"when"`
	Costs string   `json:"costs"`
	Terms []string `json:"terms"`
	Kind  string   `json:"kind"`
}

type cvTurn struct {
	Chat      string   `json:"chat"`
	Said      string   `json:"said"`
	Reply     string   `json:"reply"`
	Cards     []cvCard `json:"cards,omitempty"`
	Tools     []string `json:"tools,omitempty"`
	Failed    []string `json:"failed,omitempty"`
	Consents  []string `json:"consents,omitempty"`
	Questions []string `json:"questions,omitempty"`
	Tasks     []string `json:"tasks,omitempty"`
	News      []string `json:"news,omitempty"`
	Notices   []string `json:"notices,omitempty"`
	Err       string   `json:"err,omitempty"`
	Secs      float64  `json:"secs"`
}

type cvRunAudit struct {
	Item     string   `json:"item"`
	Run      string   `json:"run"`
	Tools    []string `json:"tools"`
	Refused  int      `json:"refused"`
	Outside  []string `json:"outside,omitempty"`
	Outcome  string   `json:"outcome"`
	Withheld string   `json:"withheld,omitempty"`
}

type cvChat struct {
	id    string
	agent *session.Agent
	dir   string
}

type cv struct {
	t       *testing.T
	scen    string
	binary  string
	home    string
	project string
	env     []string
	key     string
	checks  []cvCheck
	turns   []cvTurn
	audits  []cvRunAudit
	notes   []string
	// answer is how the person answers a standing card; the default is yes.
	answer func(session.StandingNotice) session.StandingAnswer
}

func TestPAIChat(t *testing.T) {
	scen := os.Getenv("PAI_SCEN")
	fn, ok := paiScenarios[scen]
	if !ok {
		t.Fatalf("PAI_SCEN %q is not a scenario", scen)
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("no OPENROUTER_API_KEY")
	}
	binary := filepath.Join(repoRoot(t), "bin", "aforge")
	if _, err := os.Stat(binary); err != nil {
		t.Fatal("bin/aforge is missing: run make build first")
	}
	home, project := keptDir(t, "home"), keptDir(t, "project")
	profile, _ := json.Marshal(map[string]string{config.KeyMemoryEnabled: "off", config.KeyStandingBackground: "off"})
	must(t, os.WriteFile(filepath.Join(home, "config.json"), profile, 0o600))
	t.Setenv("AFORGE_HOME", home)
	t.Setenv("AFORGE_DAILY_BUDGET", "2")
	env := scriptedEnv(home, "https://openrouter.ai")
	for at, kv := range env {
		switch {
		case strings.HasPrefix(kv, "AFORGE_BASE_URL="):
			env[at] = "AFORGE_DAILY_BUDGET=2"
		case strings.HasPrefix(kv, "AFORGE_MODEL="):
			env[at] = "AFORGE_MODEL=" + paiModel
		case strings.HasPrefix(kv, "OPENROUTER_API_KEY="):
			env[at] = "OPENROUTER_API_KEY=" + key
		}
	}
	c := &cv{t: t, scen: scen, binary: binary, home: home, project: project, env: env, key: key}
	c.answer = func(session.StandingNotice) session.StandingAnswer { return session.StandingAnswer{Approved: true} }
	defer c.finish()
	fn(c)
}

// ── the person's hands ──────────────────────────────────────────────────────

func (c *cv) chat(id string) *cvChat {
	c.t.Helper()
	store, err := standing.Open(filepath.Join(c.home, "v3", "standing"))
	must(c.t, err)
	place := session.Place{Dir: filepath.Join(c.home, "v3", "projects", "pai", id), Workspace: c.project}
	must(c.t, os.MkdirAll(place.Dir, 0o700))
	agent, err := session.New(session.Config{
		Workspace:    c.project,
		Place:        place,
		SessionFile:  place.Transcript(),
		Model:        paiModel,
		APIKey:       c.key,
		BaseURL:      "https://openrouter.ai/api/v1",
		ProfileDir:   c.home,
		AskConsent:   true,
		Standing:     &session.Standing{Store: store},
		Organization: &session.Organization{Path: filepath.Join(c.home, "v3", "collections.db")},
	})
	must(c.t, err)
	c.t.Cleanup(func() { _ = agent.Close() })
	return &cvChat{id: id, agent: agent, dir: place.Dir}
}

var paiTurnLimit = 5 * time.Minute

// say is one turn of the person's words, answered the way the person would.
func (c *cv) say(ch *cvChat, words string) cvTurn {
	c.t.Helper()
	c.t.Logf("PERSON[%s]> %s", ch.id, words)
	start := time.Now()
	turn := cvTurn{Chat: ch.id, Said: words}
	ctx, cancel := context.WithTimeout(context.Background(), paiTurnLimit)
	defer cancel()
	events, err := ch.agent.Submit(ctx, words)
	if err != nil {
		turn.Err = err.Error()
		c.turns = append(c.turns, turn)
		return turn
	}
	var reply strings.Builder
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			reply.WriteString(event.Text)
		case session.EventStandingProposal:
			n := *event.Standing
			card := cvCard{Words: n.Item.Words, When: n.WhenWords, Costs: n.CostWords, Terms: n.Terms, Kind: string(n.Item.When.Kind)}
			turn.Cards = append(turn.Cards, card)
			c.t.Logf("CARD words: %s\n  when · %s\n  costs · %s\n  %s", card.Words, card.When, card.Costs, strings.Join(card.Terms, "\n  "))
			ch.agent.ResolveStanding(n.ID, c.answer(n))
		case session.EventStandingUpdate:
			turn.News = append(turn.News, event.Standing.Update+" · "+event.Standing.Text)
		case session.EventConsentRequest:
			turn.Consents = append(turn.Consents, event.Tool+" · "+event.Hint+" · "+event.Rule)
			c.t.Logf("CONSENT asked: %s %s (%s) — allowed", event.Tool, event.Hint, event.Rule)
			ch.agent.ResolveConsent(event.ID, true)
		case session.EventTaskProposal:
			title := ""
			if event.Task != nil {
				title = event.Task.Title
				ch.agent.ResolveTask(event.Task.ID, session.TaskAnswer{Approved: false})
			}
			turn.Tasks = append(turn.Tasks, title)
			c.t.Logf("TASK proposed (declined): %s", title)
		case session.EventHarnessOffer:
			ch.agent.ResolveHarness(event.ID, false, "")
			turn.Notices = append(turn.Notices, "harness offer declined: "+event.Text)
		case session.EventConnectAsk:
			ch.agent.ResolveConnect(event.ConnectID, false)
			turn.Notices = append(turn.Notices, "connect declined: "+event.Service)
		case session.EventQuestion:
			if event.Question != nil {
				turn.Questions = append(turn.Questions, string(event.Question.Kind)+" · "+event.Question.Head)
			}
		case session.EventToolBegin:
			turn.Tools = append(turn.Tools, event.Tool+" "+pclip(event.Args, 400))
		case session.EventToolFailed:
			turn.Failed = append(turn.Failed, event.Tool+" → "+pclip(event.Output, 500))
		case session.EventNotice:
			turn.Notices = append(turn.Notices, event.Text)
		case session.EventError:
			if event.Err != nil {
				turn.Err = event.Err.Error()
			}
		}
	}
	turn.Reply = reply.String()
	turn.Secs = time.Since(start).Seconds()
	for _, tool := range turn.Tools {
		c.t.Logf("  tool: %s", tool)
	}
	for _, f := range turn.Failed {
		c.t.Logf("  tool FAILED: %s", f)
	}
	for _, n := range turn.News {
		c.t.Logf("  news: %s", n)
	}
	if turn.Err != "" {
		c.t.Logf("  TURN ERROR: %s", turn.Err)
	}
	c.t.Logf("AFORGE[%s]> %s", ch.id, turn.Reply)
	c.turns = append(c.turns, turn)
	return turn
}

func pclip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "⏎")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (c *cv) aforge(timeout time.Duration, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.Env, cmd.Dir = c.env, c.project
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	}
	return string(out) + "\n" + err.Error(), -1
}

func (c *cv) ok(args ...string) string {
	c.t.Helper()
	out, code := c.aforge(2*time.Minute, args...)
	if code != 0 {
		c.t.Fatalf("aforge %s: exit %d\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

// check is one pass, the one the timer runs.
func (c *cv) check() (string, int) {
	out, code := c.aforge(12*time.Minute, "standing", "check")
	c.t.Logf("$ aforge standing check → exit %d\n%s", code, strings.TrimSpace(out))
	return out, code
}

func (c *cv) folder(name string) string {
	c.t.Helper()
	var made struct {
		ID string `json:"id"`
	}
	must(c.t, json.Unmarshal([]byte(c.ok("collections", "create", name, "--json")), &made))
	return made.ID
}

func (c *cv) rule(folder, words string, descendants bool) {
	c.t.Helper()
	args := []string{"standing", "add", "--hold", "--words", words, "--scope", folder, "--workspace", c.project}
	if descendants {
		args = append(args, "--descendants")
	}
	c.ok(args...)
}

func (c *cv) placeChat(folder string, ch *cvChat) {
	c.t.Helper()
	c.ok("collections", "place", folder, "conversation", ch.id)
}

func (c *cv) mkdir(rel string) { must(c.t, os.MkdirAll(filepath.Join(c.project, rel), 0o755)) }

func (c *cv) write(rel, text string) {
	c.t.Helper()
	time.Sleep(20 * time.Millisecond)
	must(c.t, os.MkdirAll(filepath.Dir(filepath.Join(c.project, rel)), 0o755))
	must(c.t, os.WriteFile(filepath.Join(c.project, rel), []byte(text), 0o644))
	c.t.Logf("EVENT write %s (%d bytes)", rel, len(text))
}

func (c *cv) appendTo(rel, text string) {
	c.t.Helper()
	time.Sleep(20 * time.Millisecond)
	file, err := os.OpenFile(filepath.Join(c.project, rel), os.O_APPEND|os.O_WRONLY, 0o644)
	must(c.t, err)
	_, err = file.WriteString(text)
	must(c.t, err)
	must(c.t, file.Close())
	c.t.Logf("EVENT append %s", rel)
}

func (c *cv) read(rel string) string {
	raw, err := os.ReadFile(filepath.Join(c.project, rel))
	if err != nil {
		return ""
	}
	return string(raw)
}

func (c *cv) sha(rel string) string {
	raw, err := os.ReadFile(filepath.Join(c.project, rel))
	if err != nil {
		return "missing"
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c *cv) items() []standing.Item {
	store, err := standing.Open(filepath.Join(c.home, "v3", "standing"))
	if err != nil {
		return nil
	}
	items, _ := store.List()
	sort.SliceStable(items, func(i, j int) bool { return items[i].Created.Before(items[j].Created) })
	return items
}

// chatItems is every item a conversation set up, oldest first.
func (c *cv) chatItems() []standing.Item {
	var out []standing.Item
	for _, item := range c.items() {
		if item.Adoption != nil && item.Adoption.Via == standing.DoorChat {
			out = append(out, item)
		}
	}
	return out
}

func (c *cv) item(id string) standing.Item {
	for _, item := range c.items() {
		if item.ID == id {
			return item
		}
	}
	return standing.Item{}
}

// work is the chat-made items that run work (not rules), oldest first.
func (c *cv) work() []standing.Item {
	var out []standing.Item
	for _, item := range c.chatItems() {
		if item.When.Kind != standing.WhenHold {
			out = append(out, item)
		}
	}
	return out
}

// successor is the item now doing what item was set up to do: item itself
// while no newer active chat work keeps the same report, else the newest that
// does. The chat has answered a limit, an edit and a move with a stop and a
// fresh card, and a check that read the retired item judged work nobody runs.
func (c *cv) successor(item standing.Item) standing.Item {
	current := c.item(item.ID)
	for _, it := range c.work() {
		if it.Status == standing.StatusActive && it.Does.Report == item.Does.Report {
			current = it
		}
	}
	return current
}

func (c *cv) describe(item standing.Item) string {
	rails, _ := json.Marshal(item.Rails)
	return fmt.Sprintf("id=%s status=%s spec=%d when=%s glob=%q every=%q probe=%q does=%s report=%q say=%q grant=%q rails=%s instructions=%q",
		item.ID, item.Status, item.SpecRevision, item.When.Kind, item.When.Glob, item.When.Every, item.When.Probe.Command,
		item.Does.Kind, item.Does.Report, item.Does.Say, item.Grant, rails, pclip(item.Does.Brief, 300))
}

type cvRun struct {
	occurrenceView
	RunDir  string
	Journal string
	Raw     map[string]any
}

func (c *cv) runs(id string) []cvRun {
	out, code := c.aforge(time.Minute, "standing", "show", id, "--json", "--runs", "50")
	if code != 0 {
		c.t.Logf("show %s failed: %s", id, out)
		return nil
	}
	var show showRecord
	if json.Unmarshal([]byte(out), &show) != nil {
		return nil
	}
	var runs []cvRun
	for _, raw := range show.Occurrences {
		var r cvRun
		_ = json.Unmarshal(raw, &r.occurrenceView)
		_ = json.Unmarshal(raw, &r.Raw)
		var paths struct {
			RunDir  string `json:"runDir"`
			Journal string `json:"journal"`
		}
		_ = json.Unmarshal(raw, &paths)
		r.RunDir, r.Journal = paths.RunDir, paths.Journal
		runs = append(runs, r)
	}
	return runs
}

func (r cvRun) line() string {
	var ch []string
	for _, x := range r.Changes {
		ch = append(ch, x.Kind+" "+x.Path)
	}
	pub := ""
	if r.Published != nil {
		pub = r.Published.Path
	}
	rc := ""
	if r.RuleCheck != nil {
		rc = r.RuleCheck.Verdict
	}
	text, _ := r.Raw["outcomeText"].(string)
	return fmt.Sprintf("%s phase=%s outcome=%s spec=%d attempt=%d changes=%v published=%q withheld=%q rules=%q text=%q", r.ID, r.Phase, r.Outcome, r.Spec, r.Attempt, ch, pub, r.Withheld, rc, pclip(text, 200))
}

func (c *cv) logRuns(id string) []cvRun {
	runs := c.runs(id)
	c.t.Logf("RUNS %s: %d", id, len(runs))
	for _, r := range runs {
		c.t.Logf("  - %s", r.line())
	}
	return runs
}

// published says whether the newest run landed and published path, with a
// receipt that matches the bytes on disk.
func (c *cv) published(r cvRun, path string) (bool, string) {
	if r.Phase != "finished" || r.Outcome != "landed" || r.Published == nil || r.Published.Path != path || r.Withheld != "" {
		return false, r.line()
	}
	if c.sha(path) != r.Published.SHA256 {
		return false, "receipt sha does not match disk"
	}
	return true, ""
}

func (c *cv) expect(items []string, name string, pass bool, detail string) {
	c.checks = append(c.checks, cvCheck{Items: items, Name: name, Pass: pass, Detail: pclip(detail, 600)})
	mark := "PASS"
	if !pass {
		mark = "FAIL"
	}
	c.t.Logf("CHECK %s %v %s %s", mark, items, name, pclip(detail, 600))
}

func (c *cv) note(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	c.notes = append(c.notes, line)
	c.t.Logf("NOTE %s", line)
}

func has(s string, subs ...string) bool {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if !strings.Contains(low, strings.ToLower(sub)) {
			return false
		}
	}
	return true
}

func hasAny(s string, subs ...string) bool {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(low, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func termsOf(t cvTurn) string {
	var all []string
	for _, card := range t.Cards {
		all = append(all, "when · "+card.When, "costs · "+card.Costs)
		all = append(all, card.Terms...)
	}
	return strings.Join(all, "\n")
}

// replyFlags are phrases a person would stop on in a reply, for the human
// reading of each run; they never decide a check on their own.
var replyFlags = regexp.MustCompile(`(?i)(window or not|even (when|if) (you|the window|aforge|the terminal)[^.]{0,30}clos|in the background|background checks? (is|are) (on|active|running)|every 5 minutes|\$\s?[0-9]+(\.[0-9]+)?)`)

func (c *cv) flagReply(t cvTurn) {
	for _, m := range replyFlags.FindAllString(t.Reply, -1) {
		c.note("reply phrase to judge [%s]: %q", t.Chat, m)
	}
}

// ── the unattended runs' own records ────────────────────────────────────────

// audit reads every run transcript under this home: which tools each run
// called, how many were refused, and any path it reached outside the project.
func (c *cv) audit() {
	root := filepath.Join(c.home, "v3", "standing")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "transcript.jsonl" {
			return nil
		}
		runDir := filepath.Dir(path)
		itemID := filepath.Base(filepath.Dir(filepath.Dir(runDir)))
		a := cvRunAudit{Item: itemID, Run: filepath.Base(runDir)}
		if raw, err := os.ReadFile(filepath.Join(runDir, "occurrence.json")); err == nil {
			var o struct {
				Outcome  string `json:"outcome"`
				Withheld string `json:"withheld"`
			}
			_ = json.Unmarshal(raw, &o)
			a.Outcome, a.Withheld = o.Outcome, o.Withheld
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
		for scanner.Scan() {
			var m struct {
				Role      string `json:"role"`
				Content   any    `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"toolCalls"`
			}
			if json.Unmarshal(scanner.Bytes(), &m) != nil {
				continue
			}
			if s, ok := m.Content.(string); ok && m.Role == "tool" && strings.HasPrefix(s, "refused") {
				a.Refused++
			}
			for _, call := range m.ToolCalls {
				a.Tools = append(a.Tools, call.Function.Name+" "+pclip(call.Function.Arguments, 160))
				var args map[string]any
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				for _, key := range []string{"path", "dir", "directory", "file", "root", "cwd"} {
					if p, ok := args[key].(string); ok && p != "" {
						abs := p
						if !filepath.IsAbs(p) {
							abs = filepath.Join(c.project, p)
						}
						abs = filepath.Clean(abs)
						if abs != c.project && !strings.HasPrefix(abs, c.project+string(filepath.Separator)) {
							a.Outside = append(a.Outside, call.Function.Name+" "+p)
						}
					}
				}
				if cmd, ok := args["command"].(string); ok && (strings.Contains(cmd, "..") || strings.Contains(cmd, c.home) || strings.Contains(cmd, "~")) {
					a.Outside = append(a.Outside, call.Function.Name+" "+pclip(cmd, 120))
				}
			}
		}
		c.audits = append(c.audits, a)
		return nil
	})
	for _, a := range c.audits {
		c.t.Logf("AUDIT run %s/%s outcome=%s withheld=%s refused=%d outside=%v tools=%v", a.Item, a.Run, a.Outcome, a.Withheld, a.Refused, a.Outside, a.Tools)
	}
}

func (c *cv) finish() {
	c.audit()
	// X3 IS CHECKED ON EVERY SCENARIO: no unattended run reached outside its
	// project, and none was left on a question nobody can answer.
	var outside, stuck []string
	for _, a := range c.audits {
		if len(a.Outside) > 0 {
			outside = append(outside, a.Item+"/"+a.Run+": "+strings.Join(a.Outside, "; "))
		}
		if a.Refused > 0 && a.Outcome == "needs-you" {
			stuck = append(stuck, a.Item+"/"+a.Run)
		}
	}
	c.expect([]string{"X3-global"}, "no-run-read-outside-project", len(outside) == 0, strings.Join(outside, " | "))
	c.expect([]string{"X3-global"}, "no-run-parked-on-a-refusal", len(stuck) == 0, strings.Join(stuck, " | "))
	for _, item := range c.items() {
		c.t.Logf("ITEM %s", c.describe(item))
	}
	verdict := map[string]bool{}
	for _, ch := range c.checks {
		for _, it := range ch.Items {
			if _, seen := verdict[it]; !seen {
				verdict[it] = true
			}
			verdict[it] = verdict[it] && ch.Pass
		}
	}
	line, _ := json.Marshal(map[string]any{
		"scenario": c.scen, "verdict": verdict, "checks": c.checks, "turns": c.turns,
		"audits": c.audits, "notes": c.notes, "spend": ledgerSpend(c.home),
	})
	c.t.Logf("CHATVAL %s", line)
	// The runner script prints this one line as the run's result, so reading a
	// run's outcome needs no second tool beside the log.
	var failed []string
	for _, ch := range c.checks {
		if !ch.Pass {
			failed = append(failed, ch.Name)
		}
	}
	c.t.Logf("CHATRESULT %s passed=%d/%d failed=[%s] spend=%s", c.scen, len(c.checks)-len(failed), len(c.checks), strings.Join(failed, ","), ledgerSpend(c.home))
}

// deliveries is every note an unattended firing left for a conversation or
// the project to fold in.
func (c *cv) deliveries() string {
	var out strings.Builder
	_ = filepath.Walk(filepath.Join(c.home, "v3"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "inbox.jsonl" {
			raw, _ := os.ReadFile(path)
			out.Write(raw)
		}
		return nil
	})
	return out.String()
}

// ── the scenarios ───────────────────────────────────────────────────────────

var paiScenarios = map[string]func(c *cv){
	"h1-inbox":    scenH1Inbox,
	"h2-spec":     scenH2Spec,
	"lifecycle":   scenLifecycle,
	"two-reviews": scenTwoReviews,
	"nested":      scenNested,
	"rails":       scenRails,
	"rules-held":  scenRulesHeld,
	"move":        scenMove,
	"two-folders": scenTwoFolders,
	"descendants": scenDescendants,
	"recall":      scenRecall,
	"rhythm":      scenRhythm,
	"regress":     scenRegress,
	"many":        scenMany,
	"policy":      scenPolicy,
	"probe":       scenProbe,
	"permission":  scenPermission,
}

// firstWork is the one piece of running work the chat set up, or a failed
// check when there is none.
func (c *cv) firstWork(items []string, t cvTurn) (standing.Item, bool) {
	work := c.work()
	c.expect(items, "card-drawn-and-work-stands", len(t.Cards) > 0 && len(work) > 0, fmt.Sprintf("cards=%d work=%d tasks=%v reply=%q", len(t.Cards), len(work), t.Tasks, pclip(t.Reply, 200)))
	if len(work) == 0 {
		return standing.Item{}, false
	}
	c.t.Logf("WORK %s", c.describe(work[0]))
	return work[0], true
}

func scenH1Inbox(c *cv) {
	tag := []string{"H1", "P3", "OC2"}
	launch := c.folder("Launch")
	c.rule(launch, "Inbox reports never quote email addresses or phone numbers; write [redacted] instead.", false)
	ch := c.chat("chat-h1")
	c.placeChat(launch, ch)
	c.write("inbox/acme.md", "Acme thread: kickoff done last week.\n")
	t := c.say(ch, "keep an eye on my inbox folder and keep reports/inbox-report.md current — I want the decisions and the open requests")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	terms := termsOf(t)
	c.expect(tag, "item-watches-inbox-and-keeps-the-report", item.Watches("inbox/today.md") && item.Does.Report == "reports/inbox-report.md", c.describe(item))
	c.expect([]string{"H1"}, "card-says-report-folder-rule", has(terms, "report · reports/inbox-report.md", "folder · launch", "rule · inbox reports never quote"), terms)
	c.check()
	c.expect(tag, "baseline-runs-nothing", len(c.runs(item.ID)) == 0, "")
	c.write("inbox/today.md", "Decision: launch moves to Friday.\nRequest: Priya (priya@example.com, +1 555 0100) to confirm the venue.\nRequest: nobody owns the press release yet.\n")
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/inbox-report.md")
	c.t.Logf("REPORT 1:\n%s", report)
	ok1, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		ok1, why = c.published(runs[0], "reports/inbox-report.md")
	}
	c.expect(tag, "change-1-published", ok1, why)
	c.expect(tag, "report-1-no-contact", report != "" && !hasAny(report, "priya@example.com", "555 0100"), "")
	c.expect(tag, "report-1-says-what-was-asked", has(report, "friday") && has(report, "press release"), "")
	c.appendTo("inbox/today.md", "Re: venue — Priya confirmed the venue for Friday.\n")
	c.check()
	runs = c.logRuns(item.ID)
	report = c.read("reports/inbox-report.md")
	c.t.Logf("REPORT 2:\n%s", report)
	ok2, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 2 {
		ok2, why = c.published(runs[0], "reports/inbox-report.md")
	}
	c.expect(tag, "change-2-published", ok2, why)
	c.expect(tag, "report-2-carries-the-reply", has(report, "venue") && hasAny(report, "confirmed", "confirm"), "")
	c.expect(tag, "report-2-no-contact", report != "" && !hasAny(report, "priya@example.com", "555 0100"), "")
}

func scenH2Spec(c *cv) {
	tag := []string{"H2", "R4", "J06", "P4"}
	marketing := c.folder("Marketing")
	c.rule(marketing, "Marketing review notes quote the exact product/spec.md line each finding relies on.", false)
	ch := c.chat("chat-h2")
	c.placeChat(marketing, ch)
	spec := "# Product spec\nOffline mode: supported on desktop and mobile.\nPricing: $12 per seat per month.\nSSO: available on the Business plan.\n"
	c.write("product/spec.md", spec)
	launchCopy := "Work anywhere — even offline, on any device.\nJust $12 per seat.\n"
	pricing := "Plans from $12 per seat per month. SSO included on Business.\n"
	c.write("marketing/launch-copy.md", launchCopy)
	c.write("marketing/pricing-page.md", pricing)
	t := c.say(ch, "whenever the product spec changes, go through our marketing copy and keep marketing/review-notes.md listing any claims the spec no longer backs up. Don't touch the copy itself.")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	c.expect(tag, "item-watches-spec-keeps-notes", item.Watches("product/spec.md") && item.Does.Report == "marketing/review-notes.md", c.describe(item))
	copySha, pricingSha := c.sha("marketing/launch-copy.md"), c.sha("marketing/pricing-page.md")
	c.check()
	c.expect(tag, "baseline-runs-nothing", len(c.runs(item.ID)) == 0, "")
	c.write("product/spec.md", strings.Replace(spec, "Offline mode: supported on desktop and mobile.", "Offline mode: removed in 3.0 — the app now needs a connection.", 1))
	c.check()
	runs := c.logRuns(item.ID)
	notes := c.read("marketing/review-notes.md")
	c.t.Logf("NOTES:\n%s", notes)
	okp, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		okp, why = c.published(runs[0], "marketing/review-notes.md")
	}
	c.expect(tag, "spec-change-published-notes", okp, why)
	if !okp {
		// What is on disk is not the run's report; judging its words would
		// judge whoever else wrote the file.
		notes = ""
	}
	c.expect(tag, "notes-flag-the-offline-claim", has(notes, "offline") && hasAny(notes, "launch-copy", "launch copy", "work anywhere"), "")
	// The pricing claims still stand; a note calling them unsupported is wrong.
	var priced []string
	for _, line := range strings.Split(notes, "\n") {
		if hasAny(line, "$12", "per seat", "sso") && callsClaimUnsupported(line) {
			priced = append(priced, line)
		}
	}
	c.expect(tag, "notes-leave-supported-claims-alone", len(priced) == 0, strings.Join(priced, " | "))
	c.expect(tag, "copy-untouched", c.sha("marketing/launch-copy.md") == copySha && c.sha("marketing/pricing-page.md") == pricingSha, "")
}

// The three vocabularies one line of review notes can speak in: a claim denied
// outright ("not backed by", "no longer supported"), a claim cleared ("still
// backed", "no unsupported claims"), and a claim flagged ("unsupported",
// "outdated"). Each is matched on whole words, so "accurate" is never read
// inside "inaccurate" nor "ok" inside "book".
var (
	claimDenied  = regexp.MustCompile(`(?i)\b(not|no longer|isn't|aren't|never)\s+(backed|supported|accurate|valid|true|covered)\b`)
	claimCleared = regexp.MustCompile(`(?i)(\bno unsupported\b|\bstill (holds|stands|accurate|backed|supported|true|valid|correct)\b|\bunaffected\b|\bconsistent with\b|\bmatches\b|\bno change\b|\bbacked by\b|\bsupported by\b|\baccurate\b)`)
	claimFlagged = regexp.MustCompile(`(?i)\b(no longer|unsupported|contradict\w*|outdated|inaccurate|invalid|remove[ds]?|false)\b`)
)

// callsClaimUnsupported says whether one line of review notes calls its claim
// unsupported. A DENIAL WINS OVER A CLEARING, because "not backed by the spec"
// holds both "not" and "backed by"; a clearing then wins over a flag, because
// "no unsupported claims" holds "unsupported". The previous check read these
// as bare substrings and could decide a line either way on vocabulary alone.
func callsClaimUnsupported(line string) bool {
	switch {
	case claimDenied.MatchString(line):
		return true
	case claimCleared.MatchString(line):
		return false
	}
	return claimFlagged.MatchString(line)
}

// TestPAIChatNotesJudgeReadsNegation pins the notes judgment on lines the
// substring version decided backwards. It needs no model and no key.
func TestPAIChatNotesJudgeReadsNegation(t *testing.T) {
	for line, want := range map[string]bool{
		"No unsupported claims found; $12 per seat is backed by the spec.": false,
		"Pricing: $12 per seat is still accurate.":                         false,
		"$12 per seat — see the pricing book, unchanged.":                  false,
		"SSO on Business: not supported by the spec any more.":             true,
		"$12 per seat is no longer backed by product/spec.md.":             true,
		"The $12 per seat claim is inaccurate.":                            true,
	} {
		if got := callsClaimUnsupported(line); got != want {
			t.Errorf("callsClaimUnsupported(%q) = %v, want %v", line, got, want)
		}
	}
}

func scenLifecycle(c *cv) {
	ch := c.chat("chat-life")
	c.mkdir("inbox")
	t := c.say(ch, "keep an eye on inbox/ and keep reports/digest.md current with a short digest of what came in")
	c.flagReply(t)
	item, ok := c.firstWork([]string{"P3", "X2", "X5", "J13", "J14", "J02"}, t)
	if !ok {
		return
	}
	c.check()
	c.write("inbox/a.md", "Request: Sam to send the Q3 numbers by Monday.\nDecision: the offsite moves to Lisbon.\n")
	c.check()
	runs := c.logRuns(item.ID)
	okp, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		okp, why = c.published(runs[0], "reports/digest.md")
	}
	c.expect([]string{"P3"}, "first-change-published", okp, why)
	before := len(runs)

	// ── pause, a change while paused, resume ────────────────────────────
	t = c.say(ch, "pause the digest for now")
	c.flagReply(t)
	c.expect([]string{"X5", "P3"}, "paused-in-chat", c.item(item.ID).Status == standing.StatusPaused, string(c.item(item.ID).Status))
	c.write("inbox/c.md", "Decision: the budget is capped at 5k.\n")
	c.check()
	c.expect([]string{"X5", "P3"}, "paused-runs-nothing", len(c.runs(item.ID)) == before, fmt.Sprintf("runs=%d before=%d", len(c.runs(item.ID)), before))
	t = c.say(ch, "ok, start the digest again")
	c.flagReply(t)
	c.expect([]string{"X5", "P3"}, "resumed-in-chat", c.item(item.ID).Status == standing.StatusActive, string(c.item(item.ID).Status))
	c.check()
	runs = c.logRuns(item.ID)
	caught := len(runs) == before+1 && len(runs[0].Changes) > 0 && runs[0].Changes[0].Path == "inbox/c.md"
	okp, why = c.published(runs[0], "reports/digest.md")
	c.expect([]string{"X5", "P3"}, "resume-catches-up-once-and-publishes", caught && okp && has(c.read("reports/digest.md"), "5k"), fmt.Sprintf("runs=%d %s", len(runs), why))

	// ── the process killed mid-run: attempt 2 finishes the occurrence ────
	c.write("inbox/d.md", "Request: legal review by Tuesday.\n")
	n := len(runs)
	killed := exec.Command(c.binary, "standing", "check")
	killed.Env, killed.Dir = c.env, c.project
	must(c.t, killed.Start())
	admitted := false
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		dirs, _ := filepath.Glob(filepath.Join(c.home, "v3", "standing", item.ID, "runs", "*", "occurrence.json"))
		if len(dirs) > n {
			admitted = true
			break
		}
	}
	time.Sleep(1500 * time.Millisecond)
	_ = killed.Process.Kill()
	_ = killed.Wait()
	c.note("killed our own standing check pid %d (admitted=%v)", killed.Process.Pid, admitted)
	c.check()
	runs = c.logRuns(item.ID)
	retried := len(runs) >= 2 && runs[0].Attempt == 2 && runs[1].Phase == "interrupted"
	okp, why = c.published(runs[0], "reports/digest.md")
	c.expect([]string{"J13", "X5"}, "killed-run-retried-as-attempt-2-and-published", admitted && retried && okp && has(c.read("reports/digest.md"), "legal"), fmt.Sprintf("admitted=%v newest=%s", admitted, runs[0].line()))
	c.check()
	c.expect([]string{"J13", "X5"}, "idle-pass-reruns-nothing", len(c.runs(item.ID)) == len(runs), "")

	// ── an edit, the way a person says it ───────────────────────────────
	t = c.say(ch, "actually, in that digest also list who owns each request")
	c.flagReply(t)
	work := c.work()
	active := []standing.Item{}
	for _, it := range work {
		if it.Status == standing.StatusActive {
			active = append(active, it)
		}
	}
	c.note("after the edit sentence: %d chat work items, %d active", len(work), len(active))
	for _, it := range work {
		c.note("  %s", c.describe(it))
	}
	c.write("inbox/b.md", "Request: Priya to book the venue.\n")
	c.check()
	digest := c.read("reports/digest.md")
	c.t.Logf("DIGEST after edit:\n%s", digest)
	for _, it := range work {
		c.logRuns(it.ID)
	}
	c.expect([]string{"X2", "J02"}, "one-active-item-after-edit", len(active) == 1, fmt.Sprintf("active=%d", len(active)))
	c.expect([]string{"X2", "J02"}, "edit-reached-next-report", has(digest, "priya") && hasAny(digest, "owner", "owns", "owned"), "")
	if len(active) == 0 {
		return
	}
	cur := active[len(active)-1]
	if cur.ID != item.ID {
		c.note("the edit made a new item %s; stop follows it", cur.ID)
	}
	item = cur

	// ── stop ─────────────────────────────────────────────────────────────
	t = c.say(ch, "stop the digest, I don't need it any more")
	c.flagReply(t)
	c.expect([]string{"J14", "X5"}, "stopped-in-chat", c.item(item.ID).Status == standing.StatusRetired, string(c.item(item.ID).Status))
	kept := c.sha("reports/digest.md")
	stopped := len(c.runs(item.ID))
	c.write("inbox/e.md", "Decision: nothing after stop.\n")
	c.check()
	c.expect([]string{"J14", "X5"}, "stopped-runs-nothing", len(c.runs(item.ID)) == stopped && c.sha("reports/digest.md") == kept, "")
	var others []string
	for _, it := range c.work() {
		if it.Status == standing.StatusActive {
			others = append(others, it.ID)
		}
	}
	c.expect([]string{"J14", "X5"}, "nothing-else-still-running", len(others) == 0, strings.Join(others, ","))
}

func scenTwoReviews(c *cv) {
	ch := c.chat("chat-two")
	c.mkdir("sales")
	c.mkdir("support")
	t := c.say(ch, "every time something lands in sales/, keep reports/sales-review.md current with the deals that moved")
	c.flagReply(t)
	t2 := c.say(ch, "and for support/, just tell me here when a new ticket comes in — no file for that one")
	c.flagReply(t2)
	var sales, support standing.Item
	for _, it := range c.work() {
		switch {
		case it.Watches("sales/x.md"):
			sales = it
		case it.Watches("support/t1.md"):
			support = it
		}
	}
	c.expect([]string{"J02", "GB6", "OC7"}, "two-reviews-stand", sales.ID != "" && support.ID != "", fmt.Sprintf("sales=%q support=%q", sales.ID, support.ID))
	if sales.ID == "" || support.ID == "" {
		return
	}
	c.expect([]string{"GB6", "OC7"}, "support-keeps-no-file", support.Does.Report == "", c.describe(support))
	c.check()
	c.write("sales/x.md", "Deal: Acme moved to negotiation, $40k.\n")
	c.write("support/t1.md", "Ticket: login fails on Safari.\n")
	c.check()
	sr, pr := c.logRuns(sales.ID), c.logRuns(support.ID)
	okS, why := false, fmt.Sprintf("runs=%d", len(sr))
	if len(sr) == 1 {
		okS, why = c.published(sr[0], "reports/sales-review.md")
	}
	c.expect([]string{"J02", "GB6"}, "sales-review-published", okS, why)
	deliv := c.deliveries()
	c.t.Logf("DELIVERIES:\n%s", deliv)
	// A say item keeps no run records; what it said is the note it left.
	told := func(file, word string) bool {
		if support.Does.Kind == standing.ActionSay {
			return hasAny(deliv, file, word)
		}
		return len(pr) > 0 && pr[0].Outcome == "landed" && hasAny(deliv, file, word)
	}
	c.expect([]string{"GB6", "OC7"}, "support-told-here", told("t1.md", "safari"), fmt.Sprintf("kind=%s runs=%d", support.Does.Kind, len(pr)))
	t = c.say(ch, "stop the sales one")
	c.flagReply(t)
	c.expect([]string{"J02", "J14"}, "sales-stopped-support-kept", c.item(sales.ID).Status == standing.StatusRetired && c.item(support.ID).Status == standing.StatusActive, fmt.Sprintf("sales=%s support=%s", c.item(sales.ID).Status, c.item(support.ID).Status))
	kept := c.sha("reports/sales-review.md")
	c.write("sales/y.md", "Deal: Beta signed, $12k.\n")
	c.write("support/t2.md", "Ticket: export to CSV times out.\n")
	c.check()
	sr, pr = c.logRuns(sales.ID), c.logRuns(support.ID)
	deliv = c.deliveries()
	c.expect([]string{"J02", "J14"}, "stopped-review-quiet", len(sr) == 1 && c.sha("reports/sales-review.md") == kept, fmt.Sprintf("sales runs=%d", len(sr)))
	c.expect([]string{"J02", "GB6", "OC7"}, "other-review-continues", told("t2.md", "csv"), fmt.Sprintf("kind=%s support runs=%d", support.Does.Kind, len(pr)))
	c.expect([]string{"notice"}, "ping-has-no-unfilled-placeholder", !strings.Contains(deliv, "{{"), "")
	// The person comes back to the chat: the note should be there to read.
	t = c.say(ch, "anything new from support?")
	c.flagReply(t)
	c.expect([]string{"OC7"}, "chat-knows-what-support-said", hasAny(t.Reply, "csv", "export"), pclip(t.Reply, 300))
}

func scenNested(c *cv) {
	tag := []string{"P3"}
	ch := c.chat("chat-nested")
	c.write("inbox/clients/acme/thread.md", "Acme: kickoff call booked for Tuesday.\n")
	c.write("inbox/clients/beta/thread.md", "Beta: contract signed.\n")
	t := c.say(ch, "keep an eye on my inbox folder — each client has its own subfolder in there — and keep reports/clients.md current with what each client is waiting on")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	c.expect(tag, "watch-reaches-nested-threads", item.Watches("inbox/clients/acme/thread.md") && item.Watches("inbox/clients/gamma/thread.md"), c.describe(item))
	c.check()
	c.appendTo("inbox/clients/acme/thread.md", "Acme: we're still waiting on the revised quote from you.\n")
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/clients.md")
	c.t.Logf("REPORT 1:\n%s", report)
	ok1, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		ok1, why = c.published(runs[0], "reports/clients.md")
	}
	c.expect(tag, "nested-reply-fires-and-publishes", ok1 && has(report, "quote"), why)
	c.write("inbox/clients/gamma/thread.md", "Gamma: waiting on the security questionnaire.\n")
	c.check()
	runs = c.logRuns(item.ID)
	report = c.read("reports/clients.md")
	c.t.Logf("REPORT 2:\n%s", report)
	ok2, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 2 {
		ok2, why = c.published(runs[0], "reports/clients.md")
	}
	c.expect(tag, "new-client-folder-fires", ok2 && has(report, "gamma") && has(report, "questionnaire"), why)
}

func scenRails(c *cv) {
	tag := []string{"OC5"}
	ch := c.chat("chat-rails")
	c.mkdir("inbox")
	t := c.say(ch, "keep an eye on inbox/ and keep reports/inbox.md current, but don't let it run more than once a day")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	// The card that stands is the turn's last one, over the item that stands.
	item = c.successor(item)
	terms := termsOf(cvTurn{Cards: t.Cards[len(t.Cards)-1:]})
	c.expect(tag, "rail-kept-and-on-the-card", item.Rails.MaxPerDay == 1 && hasAny(terms, "at most 1 run a day", "1 run a day", "once a day"), c.describe(item)+" | "+terms)
	c.check()
	c.write("inbox/a.md", "Decision: hire two contractors.\n")
	c.check()
	c.write("inbox/b.md", "Request: finance to approve the contractor budget.\n")
	out, _ := c.check()
	runs := c.logRuns(item.ID)
	show := c.ok("standing", "show", item.ID)
	c.t.Logf("SHOW:\n%s", show)
	c.expect(tag, "second-change-held-by-the-rail", len(runs) == 1 && runs[0].Outcome == "landed" && hasAny(out+show, "as often as you allowed", "already run today"), fmt.Sprintf("runs=%d", len(runs)))
}

func scenRulesHeld(c *cv) {
	tag := []string{"OC5", "X5", "X1"}
	hr := c.folder("HR")
	c.rule(hr, "HR reports never state a salary or compensation figure; write [withheld] instead.", false)
	ch := c.chat("chat-hr")
	c.placeChat(hr, ch)
	c.mkdir("hr/offers")
	t := c.say(ch, "keep an eye on hr/offers/ and keep reports/offers.md current with each candidate, their role and their start date")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	c.expect(tag, "card-names-the-hr-rule", has(termsOf(t), "rule · hr reports never state a salary"), termsOf(t))
	c.check()
	c.write("hr/offers/ada.md", "Candidate: Ada Lovelace\nRole: Staff Engineer\nStart: October 1\nSalary: $210,000 base + $30,000 bonus\n")
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/offers.md")
	c.t.Logf("REPORT:\n%s", report)
	if len(runs) != 1 {
		c.expect(tag, "one-run", false, fmt.Sprintf("runs=%d", len(runs)))
		return
	}
	r := runs[0]
	leaked := hasAny(report, "210", "30,000", "30000")
	published, why := c.published(r, "reports/offers.md")
	held := r.Withheld == "held-by-rules"
	c.expect(tag, "published-without-salary-or-held", (published && !leaked && has(report, "ada") && hasAny(report, "staff engineer")) || held, why)
	verdict := ""
	if r.RuleCheck != nil {
		verdict = r.RuleCheck.Verdict
	}
	c.expect([]string{"X1"}, "rule-check-receipt-matches-report", !(leaked && verdict == "kept"), "verdict="+verdict)
}

func scenMove(c *cv) {
	tag := []string{"J09", "O11"}
	personal, work := c.folder("Personal"), c.folder("Work")
	c.rule(personal, "Every report filed in Personal ends with the line FILED-PERSONAL.", false)
	c.rule(work, "Every report filed in Work ends with the line FILED-WORK.", false)
	ch := c.chat("chat-move")
	c.placeChat(personal, ch)
	c.mkdir("notes")
	t := c.say(ch, "keep an eye on notes/ and keep reports/notes.md current with the action items")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	c.expect(tag, "card-says-personal", has(termsOf(t), "folder · personal"), termsOf(t))
	c.check()
	c.write("notes/mon.md", "Action: send the Q3 plan to the team.\n")
	c.check()
	report := c.read("reports/notes.md")
	c.t.Logf("REPORT 1:\n%s", report)
	c.logRuns(item.ID)
	c.expect(tag, "first-report-follows-personal", has(report, "FILED-PERSONAL") && !has(report, "FILED-WORK"), "")
	t = c.say(ch, "hmm, that notes report is really work stuff — move it to my Work folder instead of Personal")
	c.flagReply(t)
	moved := c.successor(item)
	if moved.ID != item.ID {
		c.note("the move made a new item %s; the record check follows it", moved.ID)
	}
	show := c.ok("standing", "show", moved.ID)
	c.t.Logf("SHOW after move:\n%s", show)
	c.write("notes/tue.md", "Action: book the Q3 review meeting.\n")
	c.check()
	runs := c.logRuns(item.ID)
	if moved.ID != item.ID {
		runs = append(c.logRuns(moved.ID), runs...)
	}
	report = c.read("reports/notes.md")
	c.t.Logf("REPORT 2:\n%s", report)
	c.expect(tag, "moved-report-follows-work-only", len(runs) == 2 && has(report, "FILED-WORK") && !has(report, "FILED-PERSONAL"), fmt.Sprintf("runs across the item and its successor=%d", len(runs)))
	c.expect(tag, "record-shows-work-not-personal", has(show, "work (placed directly)") && !has(show, "personal (placed directly)"), "")
}

func scenTwoFolders(c *cv) {
	tag := []string{"O13", "X1"}
	launch, travel := c.folder("Launch"), c.folder("Travel")
	c.rule(launch, "Every Launch report ends with the line LAUNCH-CHECKED.", false)
	c.rule(travel, "Every Travel report lists each trip under a heading 'Travel dates' with the traveller and the dates.", false)
	ch := c.chat("chat-both")
	c.placeChat(launch, ch)
	c.placeChat(travel, ch)
	c.mkdir("standup")
	t := c.say(ch, "keep an eye on standup/ and keep reports/standup.md current with a short digest of each standup")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	terms := termsOf(t)
	c.expect(tag, "card-names-both-folders-and-rules", has(terms, "launch", "travel", "launch-checked", "travel dates"), terms)
	c.check()
	c.write("standup/mon.md", "Ravi flies to Berlin Oct 3–6 for the launch event.\nMei finishes the press kit on Friday.\n")
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/standup.md")
	c.t.Logf("REPORT:\n%s", report)
	if len(runs) != 1 {
		c.expect(tag, "one-run", false, fmt.Sprintf("runs=%d", len(runs)))
		return
	}
	published, why := c.published(runs[0], "reports/standup.md")
	launchKept := has(report, "LAUNCH-CHECKED")
	travelKept := has(report, "travel dates") && has(report, "ravi")
	verdict := ""
	if runs[0].RuleCheck != nil {
		verdict = runs[0].RuleCheck.Verdict
	}
	c.expect([]string{"O13"}, "both-folders-rules-kept", published && launchKept && travelKept, fmt.Sprintf("%s launch=%v travel=%v", why, launchKept, travelKept))
	c.expect([]string{"X1"}, "rule-check-receipt-matches-report", !(published && (!launchKept || !travelKept) && verdict == "kept"), "verdict="+verdict)
}

func scenDescendants(c *cv) {
	tag := []string{"J22"}
	clients, acme, archive := c.folder("Clients"), c.folder("Acme"), c.folder("Archive")
	c.ok("collections", "place", clients, "collection", acme)
	c.rule(clients, "Every Clients report includes the line MARK-P.", false)
	c.rule(clients, "Every report in the Clients family includes the line MARK-D.", true)
	c.rule(acme, "Every Acme report includes the line MARK-C.", false)
	c.rule(archive, "Every Archive report includes the line MARK-O.", false)
	ch := c.chat("chat-acme")
	c.placeChat(acme, ch)
	c.write("acme/notes.md", "Acme renewal: contract value up 8%, signature expected Oct 12.\n")
	t := c.say(ch, "keep an eye on acme/ and keep acme-status.md current with a two-line account status")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	terms := termsOf(t)
	c.expect(tag, "card-lists-exactly-the-rules-that-reach", has(terms, "mark-c", "mark-d") && !hasAny(terms, "mark-p", "mark-o"), terms)
	t = c.say(ch, "also file that acme watch under my Archive folder, just for reference")
	c.flagReply(t)
	show := c.ok("standing", "show", item.ID)
	c.t.Logf("SHOW:\n%s", show)
	c.check()
	c.appendTo("acme/notes.md", "Update: legal asked for a new indemnity clause.\n")
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("acme-status.md")
	c.t.Logf("REPORT:\n%s", report)
	okp, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		okp, why = c.published(runs[0], "acme-status.md")
	}
	c.expect(tag, "published", okp, why)
	c.expect(tag, "rules-that-reach-are-kept", has(report, "MARK-C") && has(report, "MARK-D"), "")
	c.expect(tag, "rules-that-do-not-reach-are-absent", !hasAny(report, "MARK-P", "MARK-O"), "")
	// The person asked for the watch itself to be filed. A reference to the
	// conversation, or to nothing, is not what was asked, and a "did not
	// place" check over a watch that was never filed passes by being empty.
	archived := c.folderHolds(archive)
	member := archived.references(workspace.StandingKind, item.ID)
	c.expect(tag, "archive-references-the-watch", member, fmt.Sprintf("references=%v", archived.References))
	if !member {
		c.note("reference-did-not-place not assessed: the watch is not a member of Archive")
		return
	}
	c.expect(tag, "reference-did-not-place", !archived.places(item.ID) && !has(show, "archive (placed directly)"), fmt.Sprintf("placed=%v", archived.Placed))
}

// folderView is `aforge collections show --json`: what a folder references,
// and what is placed in it where its rules reach.
type folderView struct {
	References []workspace.Ref `json:"references"`
	Placed     []workspace.Ref `json:"placed"`
}

func (c *cv) folderHolds(folder string) folderView {
	c.t.Helper()
	var view folderView
	must(c.t, json.Unmarshal([]byte(c.ok("collections", "show", folder, "--json")), &view))
	return view
}

func (v folderView) references(kind workspace.Kind, id string) bool {
	for _, ref := range v.References {
		if ref.Kind == kind && ref.ID == id {
			return true
		}
	}
	return false
}

func (v folderView) places(id string) bool {
	for _, ref := range v.Placed {
		if ref.ID == id {
			return true
		}
	}
	return false
}

func scenRecall(c *cv) {
	alpha := c.folder("Alpha")
	c.folder("Beta")
	ch := c.chat("chat-alpha")
	c.placeChat(alpha, ch)
	c.mkdir("inbox")
	t := c.say(ch, "keep an eye on my inbox folder and keep reports/inbox-report.md current with the decisions")
	c.flagReply(t)
	item, ok := c.firstWork([]string{"O10", "O9", "J12"}, t)
	if !ok {
		return
	}
	c.check()
	c.write("inbox/today.md", "Decision: the launch moves to Friday.\n")
	c.check()
	runs := c.logRuns(item.ID)
	// Ten days later, a fresh conversation in the same project, in no folder.
	later := c.chat("chat-later")
	t = c.say(later, "what have I got filed under my Alpha folder?")
	c.flagReply(t)
	c.expect([]string{"O10"}, "folder-answer-finds-the-watch", hasAny(t.Reply, "inbox"), pclip(t.Reply, 400))
	t = c.say(later, "why did reports/inbox-report.md change?")
	c.flagReply(t)
	c.expect([]string{"O9", "J12"}, "why-answer-names-the-cause", len(runs) == 1 && has(t.Reply, "today.md"), pclip(t.Reply, 400))
	c.expect([]string{"O9", "J12", "X1"}, "why-answer-claims-no-other-cause", !hasAny(t.Reply, "i don't know", "no record", "cannot tell", "can't tell"), pclip(t.Reply, 400))
}

func scenRhythm(c *cv) {
	tag := []string{"CC5", "O4", "GB6", "O5"}
	ch := c.chat("chat-rhythm")
	c.write("status/api.txt", "api: up\n")
	c.write("status/db.txt", "db: up\n")
	c.write("status/web.txt", "web: up\n")
	t := c.say(ch, "every minute, look at the status folder and keep reports/health.md saying whether everything is up")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	c.expect([]string{"CC5", "O4"}, "item-is-a-rhythm-keeping-the-report", item.When.Kind == standing.WhenEvery && item.Does.Report == "reports/health.md", c.describe(item))
	c.check()
	first := len(c.runs(item.ID))
	must(c.t, os.Remove(filepath.Join(c.project, "status/api.txt")))
	c.t.Logf("EVENT removed status/api.txt; waiting 70s for the rhythm")
	time.Sleep(70 * time.Second)
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/health.md")
	c.t.Logf("REPORT:\n%s", report)
	okp, why := false, fmt.Sprintf("runs=%d first=%d", len(runs), first)
	if len(runs) == first+1 {
		okp, why = c.published(runs[0], "reports/health.md")
	}
	c.expect([]string{"CC5", "O4", "GB6"}, "rhythm-fired-and-published", okp, why)
	allUp := regexp.MustCompile(`(?i)(all (systems|services) (are )?(up|healthy|operational)|everything is up)`).MatchString(report)
	c.expect([]string{"O5"}, "missing-api-is-not-healthy", report != "" && has(report, "api") && !allUp && hasAny(report, "missing", "unknown", "no status", "not found", "absent", "down", "not up", "cannot", "could not", "no longer"), "")
}

func scenRegress(c *cv) {
	tag := []string{"O2", "J05"}
	ch := c.chat("chat-ci")
	results := "PASS TestLogin\nPASS TestCheckoutTotalsRounding\nPASS TestInvoiceExport\n"
	c.write("ci/results.txt", results)
	t := c.say(ch, "keep an eye on ci/results.txt and tell me in reports/regressions.md if any test that used to pass starts failing")
	c.flagReply(t)
	item, ok := c.firstWork(tag, t)
	if !ok {
		return
	}
	c.check()
	flipped := strings.Replace(results, "PASS TestCheckoutTotalsRounding", "FAIL TestCheckoutTotalsRounding", 1)
	c.write("ci/results.txt", flipped)
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/regressions.md")
	c.t.Logf("REPORT:\n%s", report)
	okp, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		okp, why = c.published(runs[0], "reports/regressions.md")
	}
	c.expect([]string{"O2"}, "regression-reported", okp && has(report, "TestCheckoutTotalsRounding"), why)
	notesBefore := strings.Count(c.deliveries(), "\n")
	time.Sleep(1100 * time.Millisecond)
	c.write("ci/results.txt", flipped) // the nightly job writes the same bytes again
	c.check()
	c.expect([]string{"J05", "O2"}, "identical-rewrite-stays-quiet", len(c.runs(item.ID)) == 1 && strings.Count(c.deliveries(), "\n") == notesBefore, fmt.Sprintf("runs=%d", len(c.runs(item.ID))))
}

func scenMany(c *cv) {
	tag := []string{"O8", "J13"}
	ch := c.chat("chat-many")
	for _, a := range []string{"north", "south", "east", "west"} {
		c.write("areas/"+a+"/log.md", "Area "+a+": quiet week.\n")
	}
	t := c.say(ch, "for each of the four area folders — areas/north, areas/south, areas/east and areas/west — keep reports/<area>.md current with what changed in that area")
	c.flagReply(t)
	work := c.work()
	c.note("items made: %d, cards: %d", len(work), len(t.Cards))
	for _, it := range work {
		c.note("  %s", c.describe(it))
	}
	c.expect(tag, "work-stands-for-the-areas", len(work) > 0, fmt.Sprintf("items=%d", len(work)))
	if len(work) == 0 {
		return
	}
	c.check()
	shas := map[string]string{}
	for _, a := range []string{"north", "south", "east", "west"} {
		shas[a] = c.sha("reports/" + a + ".md")
	}
	c.appendTo("areas/north/log.md", "North: the warehouse lease was renewed.\n")
	c.appendTo("areas/east/log.md", "East: two new hires started.\n")
	c.check()
	for _, it := range work {
		c.logRuns(it.ID)
	}
	north, east := c.read("reports/north.md"), c.read("reports/east.md")
	c.t.Logf("NORTH:\n%s\nEAST:\n%s", north, east)
	changed := has(north, "lease") && has(east, "hire")
	quiet := c.sha("reports/south.md") == shas["south"] && c.sha("reports/west.md") == shas["west"]
	c.expect(tag, "exactly-the-changed-areas-reported", changed && quiet, fmt.Sprintf("north/east updated=%v south/west untouched=%v", changed, quiet))
	total := 0
	for _, it := range work {
		total += len(c.runs(it.ID))
	}
	c.check()
	after := 0
	for _, it := range work {
		after += len(c.runs(it.ID))
	}
	c.expect(tag, "idle-pass-runs-nothing", after == total, fmt.Sprintf("runs %d → %d", total, after))
	t = c.say(ch, "what have I got running right now?")
	c.flagReply(t)
	named := 0
	for _, a := range []string{"north", "south", "east", "west"} {
		if has(t.Reply, a) {
			named++
		}
	}
	c.expect([]string{"O8"}, "chat-lists-what-runs", named == 4, pclip(t.Reply, 400))
}

func scenPolicy(c *cv) {
	tag := []string{"R3", "P4", "O14"}
	maint := c.folder("Maint")
	ch := c.chat("chat-maint")
	c.placeChat(maint, ch)
	c.write("deps/available.txt", "libzap current 1.2.0, available 1.3.0\n")
	t := c.say(ch, "a note for all our maintenance work: we only take minor and patch upgrades, never a new major version")
	c.flagReply(t)
	t = c.say(ch, "keep an eye on deps/ and keep reports/upgrades.md current with which of the available upgrades we should take")
	c.flagReply(t)
	work := c.work()
	c.expect(tag, "work-stands", len(work) == 1, fmt.Sprintf("work=%d", len(work)))
	if len(work) == 0 {
		return
	}
	item := work[0]
	c.t.Logf("WORK %s", c.describe(item))
	c.check()
	c.write("deps/available.txt", "libzap current 1.2.0, available 1.3.0\nlibfoo current 2.4.1, available 3.0.0\n")
	c.check()
	runs := c.logRuns(item.ID)
	report := c.read("reports/upgrades.md")
	c.t.Logf("REPORT 1:\n%s", report)
	okp, why := false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 1 {
		okp, why = c.published(runs[0], "reports/upgrades.md")
	}
	c.expect(tag, "policy-reached-the-run", okp && policyVerdict(report, "libfoo") == "no" && policyVerdict(report, "libzap") == "yes", why+" libfoo="+policyVerdict(report, "libfoo")+" libzap="+policyVerdict(report, "libzap"))
	t = c.say(ch, "we've changed our minds on upgrades: a new major version is fine now too, as long as it's below 4")
	c.flagReply(t)
	c.write("deps/available.txt", "libzap current 1.2.0, available 1.3.0\nlibfoo current 2.4.1, available 3.0.0\nlibbar current 3.9.0, available 4.1.0\n")
	c.check()
	runs = c.logRuns(item.ID)
	report = c.read("reports/upgrades.md")
	c.t.Logf("REPORT 2:\n%s", report)
	okp, why = false, fmt.Sprintf("runs=%d", len(runs))
	if len(runs) == 2 {
		okp, why = c.published(runs[0], "reports/upgrades.md")
	}
	c.expect(tag, "revision-reached-the-next-run", okp && policyVerdict(report, "libfoo") == "yes" && policyVerdict(report, "libbar") == "no", why+" libfoo="+policyVerdict(report, "libfoo")+" libbar="+policyVerdict(report, "libbar"))
}

// policyVerdict reads the report's line(s) about one library: "yes" when it
// says take it, "no" when it says skip it, "" when the line is unclear.
func policyVerdict(report, lib string) string {
	var lines []string
	for _, line := range strings.Split(report, "\n") {
		if has(line, lib) {
			lines = append(lines, line)
		}
	}
	joined := strings.ToLower(strings.Join(lines, " "))
	no := hasAny(joined, "not eligible", "skip", "do not take", "don't take", "reject", "not take", "hold off", "❌", "blocked", "no —", "no -", "not allowed", "excluded", "defer", "do not upgrade", "don't upgrade", "not recommended", "not permitted")
	yes := hasAny(joined, "eligible", "take", "✅", "yes", "approve", "upgrade", "recommended", "allowed", "ok")
	switch {
	case no:
		return "no"
	case yes:
		return "yes"
	}
	return ""
}

func scenProbe(c *cv) {
	tag := []string{"OC4"}
	ch := c.chat("chat-probe")
	c.write("ci/status.txt", "build: passing\n")
	t := c.say(ch, "tell me here when the build breaks — ci/status.txt says passing or failing. Check it every minute.")
	c.flagReply(t)
	work := c.work()
	c.expect(tag, "a-watch-stands", len(t.Cards) > 0 && len(work) == 1, fmt.Sprintf("cards=%d work=%d", len(t.Cards), len(work)))
	if len(work) == 0 {
		return
	}
	item := work[0]
	c.t.Logf("WORK %s", c.describe(item))
	c.check()
	time.Sleep(65 * time.Second)
	c.check()
	quiet := c.deliveries()
	c.expect(tag, "passing-says-nothing", !hasAny(quiet, "fail", "broke", "broken"), pclip(quiet, 300))
	c.write("ci/status.txt", "build: failing (TestCheckoutTotalsRounding)\n")
	time.Sleep(65 * time.Second)
	c.check()
	runs := c.logRuns(item.ID)
	deliv := c.deliveries()
	c.t.Logf("DELIVERIES:\n%s", deliv)
	c.expect(tag, "failing-is-told", hasAny(deliv, "fail", "broke", "broken"), fmt.Sprintf("runs=%d", len(runs)))
	t = c.say(ch, "is the build ok?")
	c.flagReply(t)
	c.expect(tag, "chat-knows-it-broke", hasAny(t.Reply, "fail", "broke", "broken", "red"), pclip(t.Reply, 300))
}

func scenPermission(c *cv) {
	tag := []string{"X3"}
	ch := c.chat("chat-perm")
	c.mkdir("inbox")
	c.write("todo.md", "- buy milk\n")
	t := c.say(ch, "whenever a new file lands in inbox/, add a one-line summary of it to the bottom of todo.md")
	c.flagReply(t)
	work := c.work()
	for _, it := range work {
		c.note("  %s", c.describe(it))
	}
	c.note("cards: %v", termsOf(t))
	if len(work) == 0 {
		// A truthful refusal is a pass for X3 only if the reply says what it
		// cannot do; this is judged by reading the reply.
		c.expect(tag, "no-work-stood", false, "no work stood — judge the reply: "+pclip(t.Reply, 400))
		return
	}
	item := work[0]
	c.check()
	c.write("inbox/n1.md", "Note: call the landlord about the leak.\n")
	c.check()
	runs := c.logRuns(item.ID)
	todo := c.read("todo.md")
	c.t.Logf("TODO:\n%s", todo)
	show := c.ok("standing", "show", item.ID)
	c.t.Logf("SHOW:\n%s", show)
	kept := has(todo, "buy milk")
	added := has(todo, "landlord")
	stuck := len(runs) > 0 && runs[0].Outcome == "needs-you"
	c.expect(tag, "the-card-promise-was-kept", kept && added, fmt.Sprintf("milk kept=%v summary added=%v", kept, added))
	c.expect(tag, "no-run-parked-on-an-unanswerable-question", !stuck, fmt.Sprintf("newest=%v", func() string {
		if len(runs) > 0 {
			return runs[0].line()
		}
		return "none"
	}()))
}

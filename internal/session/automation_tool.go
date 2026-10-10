package session

// automation_tool.go is the model's door to automations: `automation`, which
// proposes one on a card and manages the ones that exist. The person never
// names the tool; the model recognises a reminder, a piece of scheduled work or
// a watch from what their sentence IS. docs/design/automations/DESIGN.md.
//
// RULES ARE NOT AUTOMATIONS. "Always use tabs here" has no moment, no rhythm
// and nothing to run; it is a memory marked always, and `remember` is its door.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// automationPastGrace is how far behind the clock a named moment may be and
// still be meant for now: a model works its stamp out from the `Now` line and
// calls a beat later. Anything further back is refused, never moved on.
const automationPastGrace = 30 * time.Second

var (
	errAutomationUnwatched  = errors.New("session: nobody is watching this session")
	errAutomationUnanswered = errors.New("session: the card went unanswered")
)

// automationStore is the store this conversation may propose into, and nil
// when it may not.
func (c Config) automationStore() *automation.Store {
	if c.Automations == nil {
		return nil
	}
	return c.Automations.Store
}

// automationTools is the `automation` tool when this conversation has a store
// and is not itself a run of one: NOTHING AN AUTOMATION DOES MAY ARM ANOTHER.
func (a *Agent) automationTools() []bare.Tool {
	if a.config.automationStore() == nil || a.config.AutomationRun != nil || a.config.InTask {
		return nil
	}
	return []bare.Tool{{
		Name:        "automation",
		Description: automationDescription,
		Schema:      json.RawMessage(automationSchemaJSON),
		Execute:     a.automationTool,
	}}
}

var automationDescription = "Set up work codeaf does on a clock — a reminder, scheduled work, or a watch — and manage the ones that exist. THE PERSON NEVER NAMES THIS TOOL; you recognise it from what their sentence IS.\n\n" +
	"KINDS. A REMINDER says a fixed line at a time (`say`): \"remind me at 6 to leave\". SCHEDULED WORK runs a brief at a time or on a rhythm (`do`): \"every Monday at 9, draft the weekly update from the git log\". A WATCH looks at something on a rhythm and, when its condition turns true, says a line or runs work (`look` + `say` or `do`): \"tell me when CI on main goes red\". Automations run ONLY while codeaf is open; anything that fell due while it was closed runs once when it opens, marked late.\n\n" +
	"NOT A RULE. \"Always use tabs here\", \"never touch the public API\", a convention or preference for future work is a memory marked always — use `remember` with always, not this tool. Something the person wants done ONCE, now, is just done now.\n\n" +
	"PROPOSE puts a card in front of the person; nothing is saved until they say yes, and the card shows exactly what you send. Keep their sentence verbatim in `words`; give a short `title`. WHEN: `when.at` is one moment (RFC 3339 with offset, worked out from the `Now` line); `when.in` is a distance from now (\"20m\", \"2h\", \"1d\"); `when.every` is a rhythm — a five-field cron line read in the person's zone (\"0 9 * * 1\" is Mondays 09:00, \"30 8 * * 1-5\" weekdays 08:30) or an interval (\"15m\", \"2h\", \"1d\"). A moment that has already passed is refused. A WATCH needs `when.every` (how often it looks), a `look` (exactly one of a shell `command` run in the project, a `files` glob, or a `tool` with `args` — with `service` when the tool belongs to a connected account) and a `condition`: the plain sentence the model checks every look against (\"the latest run on main failed\"). Prefer a look whose output carries the thing itself (a status field, a file's contents), not a bare status code. `look.once` makes it tell once and stop. A watch speaks when its condition CHANGES to true, and is quiet while it stays true. Send `worktree` only for work that edits code and should be kept on a branch for review. Send `limits` only when the person named a time or a price (defaults: 30m and $5 a run).\n\n" +
	"MANAGE: `list` shows them with their ids; `pause`, `resume`, `delete`, `run` (now, outside its schedule) and `history` take an `id`."

var automationSchemaJSON = `{"type":"object","properties":{` +
	`"op":{"type":"string","enum":["propose","list","pause","resume","delete","run","history"]},` +
	`"id":{"type":"string","description":"Which automation, for pause, resume, delete, run and history — from list."},` +
	`"title":{"type":"string","description":"A short name, a few words."},` +
	`"words":{"type":"string","description":"The person's own sentence, verbatim."},` +
	`"when":{"type":"object","properties":{` +
	`"at":{"type":"string","description":"One moment, RFC 3339 with its offset."},` +
	`"in":{"type":"string","description":"One moment as a distance from now: 20m, 2h, 1d."},` +
	`"every":{"type":"string","description":"A rhythm: a five-field cron line in the person's zone, or an interval of at least a minute (15m, 2h, 1d)."},` +
	`"words":{"type":"string","description":"The schedule as the person said it."}}},` +
	`"look":{"type":"object","properties":{` +
	`"command":{"type":"string","description":"A shell command run in the project."},` +
	`"files":{"type":"string","description":"A glob relative to the project; ** spans folders."},` +
	`"tool":{"type":"string","description":"A tool to call."},` +
	`"service":{"type":"string","description":"The connected account the tool belongs to."},` +
	`"args":{"type":"object","description":"The tool's arguments."},` +
	`"condition":{"type":"string","description":"What every look is checked against, as a plain sentence."},` +
	`"once":{"type":"boolean","description":"Tell once, then stop watching."}}},` +
	`"say":{"type":"string","description":"The line a reminder, or a watch, says."},` +
	`"do":{"type":"string","description":"The brief scheduled work, or a watch, carries out."},` +
	`"worktree":{"type":"boolean","description":"Run the work in a separate git worktree whose branch is kept for review."},` +
	`"limits":{"type":"object","properties":{` +
	`"time":{"type":"string","description":"The most one run may take, e.g. 30m."},` +
	`"usd":{"type":"number","description":"The most one run may spend, in dollars."}}}` +
	`},"required":["op"]}`

type automationArguments struct {
	Op    string `json:"op"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Words string `json:"words"`
	When  struct {
		At    string `json:"at"`
		In    string `json:"in"`
		Every string `json:"every"`
		Words string `json:"words"`
	} `json:"when"`
	Look *struct {
		Command   string          `json:"command"`
		Files     string          `json:"files"`
		Tool      string          `json:"tool"`
		Service   string          `json:"service"`
		Args      json.RawMessage `json:"args"`
		Condition string          `json:"condition"`
		Once      bool            `json:"once"`
	} `json:"look"`
	Say      string `json:"say"`
	Do       string `json:"do"`
	Worktree bool   `json:"worktree"`
	Limits   struct {
		Time string   `json:"time"`
		USD  *float64 `json:"usd"`
	} `json:"limits"`
}

// automationTool is the belt's entry point, with the clock stamped on every
// answer: this is the tool whose subject is WHEN, and the model needs the real
// time at exactly the moment it is told a moment has passed.
func (a *Agent) automationTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	out, failed, err := a.automationDispatch(ctx, args)
	if err != nil {
		return out, failed, err
	}
	return out + "\nnow: " + time.Now().Format("15:04 -07:00"), failed, nil
}

func (a *Agent) automationDispatch(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed automationArguments
	if len(args) > 0 {
		if err := decodeToolArguments(args, &parsed); err != nil {
			return "Invalid arguments: " + err.Error(), true, nil
		}
	}
	store := a.config.automationStore()
	if store == nil {
		return "there is nothing here to keep automations in", true, nil
	}
	switch op := strings.ToLower(strings.TrimSpace(parsed.Op)); op {
	case "propose":
		return a.automationPropose(ctx, store, parsed)
	case "list":
		return automationList(store, time.Now())
	case "pause", "resume":
		status := automation.StatusPaused
		if op == "resume" {
			status = automation.StatusActive
		}
		return a.automationSetStatus(store, parsed.ID, status)
	case "delete":
		return a.automationDelete(store, parsed.ID)
	case "run":
		return a.automationRunNow(store, parsed.ID)
	case "history":
		return automationHistory(store, parsed.ID, time.Now())
	case "":
		return "Invalid arguments: op is required — propose, list, pause, resume, delete, run or history", true, nil
	default:
		return "Invalid arguments: no op called " + strconv.Quote(parsed.Op) + " — propose, list, pause, resume, delete, run or history", true, nil
	}
}

// ── proposing ───────────────────────────────────────────────────────────────

func (a *Agent) automationPropose(ctx context.Context, store *automation.Store, parsed automationArguments) (string, bool, error) {
	// ONE READING OF THE CLOCK FOR THE WHOLE CALL, so `when.in`, the refusal of a
	// passed moment and the card's next-run line all measure the same instant.
	now := time.Now()
	item, problem := a.automationFrom(parsed, now)
	if problem != "" {
		return problem, true, nil
	}
	if err := item.Validate(); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	next, err := item.Schedule.First(now)
	if err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	notice := AutomationNotice{
		Automation: item,
		WhenWords:  firstNonEmptyString(strings.TrimSpace(parsed.When.Words), automation.Describe(item.Schedule, now)),
		Exact:      automation.Exact(item.Schedule),
		Next:       next,
		CostWords:  a.automationCostWords(item, now),
		Options:    AutomationOptions(item),
	}
	answer, err := a.askAutomation(ctx, &notice)
	switch {
	case errors.Is(err, errAutomationUnwatched):
		return "nobody is here to say yes — an automation can only be set up in a conversation somebody is in", true, nil
	case errors.Is(err, errAutomationUnanswered):
		return "the card was left unanswered — nothing was saved", false, nil
	case err != nil:
		return "the card was never answered: " + err.Error(), true, nil
	}
	if !answer.Save {
		if change := strings.TrimSpace(answer.Change); change != "" {
			return "the person changed it: " + change + "\nNothing is saved yet. Propose it again with that — it may be about when it runs, what it does, or where.", false, nil
		}
		return "nothing was saved: the person said no.", false, nil
	}
	saved, err := store.Create(item)
	if err != nil {
		return "nothing was saved: " + err.Error(), true, nil
	}
	a.emitAutomationUpdate("saved", saved, "")
	line := fmt.Sprintf("saved %s: %s\nit runs: %s", saved.ID, saved.Title, notice.WhenWords)
	if !saved.Next.IsZero() {
		line += "\nfirst run: " + automation.Moment(saved.Next, now)
	}
	if answer.RunNow && AutomationRunsNow(saved) {
		return line + "\n" + automationRunNowHandoff(saved), false, nil
	}
	return line + "\nThey answered the card. Your WHOLE reply is one short line saying what is now set up and when it first runs — no preamble, and do not ask again.", false, nil
}

// automationRunNowHandoff hands the action back to the model to do ONCE, now,
// in this turn. The person is here, so any approval it needs is asked of them,
// and an "always" they give is banked for the unattended runs that follow.
func automationRunNowHandoff(item automation.Automation) string {
	if look := item.Look; look != nil {
		var target string
		switch {
		case strings.TrimSpace(look.Command) != "":
			target = "run this command in the project: " + look.Command
		case strings.TrimSpace(look.Files) != "":
			target = "look at the files matching " + look.Files
		default:
			target = "call " + look.Tool
			if look.Service != "" {
				target += " (from " + look.Service + ")"
			}
			if len(look.Args) > 0 {
				target += " with " + string(look.Args)
			}
		}
		return "They asked to check it now as well. Take ONE look yourself, here: " + target + ". Then tell them in one or two sentences whether \"" + look.Condition + "\" holds now. Approvals you need are asked of them in this turn."
	}
	return "They asked to run it now as well. Do the work ONCE, here, now: " + item.Action.Do + "\nApprovals you need are asked of them in this turn; when you are done, say in one or two sentences what you did."
}

// automationFrom builds the automation the card will show, or says what is
// wrong with the arguments in words the model can act on.
func (a *Agent) automationFrom(parsed automationArguments, now time.Time) (automation.Automation, string) {
	item := automation.Automation{
		Title:     strings.TrimSpace(parsed.Title),
		Words:     strings.TrimSpace(parsed.Words),
		Workspace: a.automationWorkspace(),
		Worktree:  parsed.Worktree,
		Action:    automation.Action{Say: strings.TrimSpace(parsed.Say), Do: strings.TrimSpace(parsed.Do)},
		Origin:    a.automationOrigin(),
	}
	if item.Title == "" {
		item.Title = clip(firstNonEmptyString(item.Words, item.Action.Say, item.Action.Do), 60)
	}
	at, in, every := strings.TrimSpace(parsed.When.At), strings.TrimSpace(parsed.When.In), strings.TrimSpace(parsed.When.Every)
	set := 0
	for _, field := range []string{at, in, every} {
		if field != "" {
			set++
		}
	}
	if set != 1 {
		return item, "Invalid arguments: send exactly one of when.at, when.in or when.every"
	}
	switch {
	case every != "":
		item.Schedule = automation.Schedule{Every: every, Zone: a.automationZone()}
	case in != "":
		span, err := automation.ParseInterval(in)
		if err != nil {
			return item, "Invalid arguments: when.in is a distance like \"20m\", \"2h\" or \"1d\""
		}
		item.Schedule = automation.Schedule{At: now.Add(span)}
	default:
		moment, err := automationMoment(at)
		if err != nil {
			return item, "Invalid arguments: when.at " + err.Error()
		}
		if moment.Before(now.Add(-automationPastGrace)) {
			return item, fmt.Sprintf("when.at %s has already passed — it is %s now. For a distance from now send when.in; for a clock time, compute it from now.",
				moment.Format("2006-01-02 15:04 -07:00"), now.Format("2006-01-02 15:04 -07:00"))
		}
		item.Schedule = automation.Schedule{At: moment}
	}
	if look := parsed.Look; look != nil {
		item.Look = &automation.Look{
			Command:   strings.TrimSpace(look.Command),
			Files:     strings.TrimSpace(look.Files),
			Tool:      strings.TrimSpace(look.Tool),
			Service:   strings.TrimSpace(look.Service),
			Args:      look.Args,
			Condition: strings.TrimSpace(look.Condition),
			Once:      look.Once,
		}
		if string(item.Look.Args) == "null" {
			item.Look.Args = nil
		}
	}
	if raw := strings.TrimSpace(parsed.Limits.Time); raw != "" {
		limit, err := automation.ParseInterval(raw)
		if err != nil {
			return item, "Invalid arguments: limits.time is a duration like \"30m\" or \"2h\""
		}
		item.Limits.Time = limit
	}
	if parsed.Limits.USD != nil {
		if *parsed.Limits.USD <= 0 || math.IsNaN(*parsed.Limits.USD) {
			return item, "Invalid arguments: limits.usd is a price above zero"
		}
		item.Limits.USD = *parsed.Limits.USD
	}
	return item, ""
}

// automationOrigin is the conversation an automation is made in: where its
// one line goes when a run ends, and the door "why did this happen?" opens.
func (a *Agent) automationOrigin() automation.Origin {
	a.mu.Lock()
	id := a.sessionID()
	a.mu.Unlock()
	return automation.Origin{SessionID: id, Transcript: strings.TrimSpace(a.config.SessionFile)}
}

// automationMoment reads a stamp: RFC 3339, or a local wall time.
func automationMoment(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		if moment, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return moment, nil
		}
	}
	return time.Time{}, errors.New("is not a moment this can read — write it as \"2026-10-10T18:00:00-04:00\"")
}

// automationWorkspace is where an automation made here runs: the project this
// conversation is in, or the person's home for one that belongs to no project.
func (a *Agent) automationWorkspace() string {
	if a.config.Place.Owned {
		if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
			return dir
		}
	}
	if root := strings.TrimSpace(a.config.Place.Workspace); root != "" {
		return root
	}
	return strings.TrimSpace(a.config.Workspace)
}

// automationZone is the zone a new rhythm is read in.
func (a *Agent) automationZone() string {
	if a.config.Automations != nil && strings.TrimSpace(a.config.Automations.Zone) != "" {
		return a.config.Automations.Zone
	}
	return LocalZone()
}

// LocalZone is this machine's IANA zone name ("America/Toronto"), read from TZ
// or from where /etc/localtime points, and empty when neither says — which a
// schedule reads as the machine's zone, whatever it is called.
func LocalZone() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" && !strings.HasPrefix(tz, ":") {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	target = filepath.ToSlash(target)
	at := strings.LastIndex(target, "zoneinfo/")
	if at < 0 {
		return ""
	}
	zone := target[at+len("zoneinfo/"):]
	if _, err := time.LoadLocation(zone); err != nil {
		return ""
	}
	return zone
}

// automationCostWords is a watch's estimated cost a day, from the price of the
// model that judges its looks and how often it looks. It is empty when the
// price is not known: an unknown figure is drawn as nothing.
func (a *Agent) automationCostWords(item automation.Automation, now time.Time) string {
	if item.Look == nil || a.config.ModelPrice == nil {
		return ""
	}
	model, err := automationJudgeModel(a.config)
	if err != nil || strings.TrimSpace(model) == "" {
		return ""
	}
	prompt, completion, known := a.config.ModelPrice(model)
	if !known {
		return ""
	}
	looks := automationLooksADay(item.Schedule, now)
	if looks <= 0 {
		return ""
	}
	// A look's judgment reads the condition, the look and up to the clip of its
	// output, and answers in a line: about this many tokens each way.
	const promptTokens, completionTokens = 3000, 120
	perLook := float64(promptTokens)*prompt + float64(completionTokens)*completion
	daily := perLook * float64(looks)
	switch {
	case daily < 0.01:
		return "under a cent a day while codeaf is open"
	default:
		return fmt.Sprintf("about $%.2f a day while codeaf is open", daily)
	}
}

// automationLooksADay is how many slots a rhythm has in the next day.
func automationLooksADay(s automation.Schedule, now time.Time) int {
	if interval := s.Interval(); interval > 0 {
		return int((24 * time.Hour) / interval)
	}
	count := 0
	at := now
	for count < 24*60 {
		next, err := s.Next(at)
		if err != nil || next.IsZero() || next.Sub(now) > 24*time.Hour {
			break
		}
		count++
		at = next
	}
	return count
}

// ── the card ────────────────────────────────────────────────────────────────

// askAutomation raises one card and waits for the person, for as long as that
// takes. A session nobody is watching gets no card, because there is nobody to
// answer one; the turn's context ending is the card going unanswered.
func (a *Agent) askAutomation(ctx context.Context, notice *AutomationNotice) (AutomationAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return AutomationAnswer{}, errAgentClosed
	}
	hub := a.hub
	if !a.config.AskConsent || hub == nil {
		a.mu.Unlock()
		return AutomationAnswer{}, errAutomationUnwatched
	}
	a.automationSeq++
	id := a.automationSeq
	answers := make(chan AutomationAnswer, 1)
	if a.automationAnswers == nil {
		a.automationAnswers = make(map[uint64]chan AutomationAnswer, 1)
	}
	a.automationAnswers[id] = answers
	a.mu.Unlock()

	notice.ID = id
	card := *notice
	question := AutomationQuestion(card)
	defer a.presenceAskingWhole(question, func() {
		hub.send(Event{Kind: EventAutomationProposal, Tool: "automation", Automation: &card})
	})()
	select {
	case answer := <-answers:
		return answer, nil
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.automationAnswers, id)
		a.mu.Unlock()
		return AutomationAnswer{}, errAutomationUnanswered
	}
}

// emitAutomationUpdate reports an automation changing because of a call in
// this turn. It is a report and never a question, so a session with no hub
// simply says nothing.
func (a *Agent) emitAutomationUpdate(update string, item automation.Automation, text string) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub == nil {
		return
	}
	hub.send(Event{Kind: EventAutomationUpdate, Tool: "automation", Automation: &AutomationNotice{
		Automation: item, Update: update, Text: text,
	}})
}

// ── managing ────────────────────────────────────────────────────────────────

func automationList(store *automation.Store, now time.Time) (string, bool, error) {
	all, err := store.List()
	if err != nil {
		return "could not read the automations: " + err.Error(), true, nil
	}
	if len(all) == 0 {
		return "there are no automations", false, nil
	}
	var out strings.Builder
	for _, item := range all {
		out.WriteString(automationRow(store, item, now))
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n"), false, nil
}

// automationRow is one automation as the tool lists it.
func automationRow(store *automation.Store, item automation.Automation, now time.Time) string {
	parts := []string{item.ID, item.Title, string(item.Kind()), automation.Describe(item.Schedule, now)}
	switch item.Status {
	case automation.StatusPaused:
		parts = append(parts, "paused")
	case automation.StatusFinished:
		parts = append(parts, "finished")
	default:
		if !item.Next.IsZero() {
			parts = append(parts, "next "+automation.Moment(item.Next, now))
		}
	}
	if runs, err := store.Runs(item.ID, 1); err == nil && len(runs) > 0 && runs[0].Phase == automation.PhaseOver {
		parts = append(parts, "last "+runs[0].Outcome.Word())
	}
	return strings.Join(parts, " · ")
}

func (a *Agent) automationSetStatus(store *automation.Store, id string, status automation.Status) (string, bool, error) {
	if strings.TrimSpace(id) == "" {
		return "Invalid arguments: id is required — list shows them", true, nil
	}
	item, err := store.SetStatus(strings.TrimSpace(id), status)
	if err != nil {
		return automationNotFound(err), true, nil
	}
	word := "paused"
	if status == automation.StatusActive {
		word = "resumed"
	}
	a.emitAutomationUpdate(word, item, "")
	return word + " " + item.ID + ": " + item.Title, false, nil
}

func (a *Agent) automationDelete(store *automation.Store, id string) (string, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "Invalid arguments: id is required — list shows them", true, nil
	}
	item, err := store.Get(id)
	if err != nil {
		return automationNotFound(err), true, nil
	}
	if err := store.Delete(id); err != nil {
		return automationNotFound(err), true, nil
	}
	a.emitAutomationUpdate("deleted", item, "")
	return "deleted " + id + ": " + item.Title, false, nil
}

func (a *Agent) automationRunNow(store *automation.Store, id string) (string, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "Invalid arguments: id is required — list shows them", true, nil
	}
	item, err := store.Get(id)
	if err != nil {
		return automationNotFound(err), true, nil
	}
	if _, err := store.QueueNow(id); err != nil {
		return "could not run it now: " + err.Error(), true, nil
	}
	a.emitAutomationUpdate("running", item, "")
	return "asked " + id + " to run now: it starts within seconds while codeaf is open, and its result arrives in this conversation", false, nil
}

func automationHistory(store *automation.Store, id string, now time.Time) (string, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "Invalid arguments: id is required — list shows them", true, nil
	}
	if _, err := store.Get(id); err != nil {
		return automationNotFound(err), true, nil
	}
	runs, err := store.Runs(id, 10)
	if err != nil {
		return "could not read its history: " + err.Error(), true, nil
	}
	if len(runs) == 0 {
		return "it has not run yet", false, nil
	}
	var out strings.Builder
	for _, run := range runs {
		out.WriteString(automationRunLine(run, now))
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n"), false, nil
}

// automationRunLine is one run as the tool, and a person, reads it.
func automationRunLine(run automation.Run, now time.Time) string {
	when := run.Started
	if when.IsZero() {
		when = run.Due
	}
	parts := []string{automation.Moment(when, now)}
	switch run.Phase {
	case automation.PhaseQueued:
		parts = append(parts, "queued")
	case automation.PhaseRunning:
		parts = append(parts, "running")
	default:
		parts = append(parts, run.Outcome.Word())
	}
	if late := run.Late(); late > 0 {
		parts = append(parts, "late "+automationLateWords(late))
	}
	if line := strings.TrimSpace(run.Line); line != "" {
		parts = append(parts, line)
	}
	if run.USD >= 0.01 {
		parts = append(parts, fmt.Sprintf("$%.2f", run.USD))
	}
	return strings.Join(parts, " · ")
}

func automationLateWords(late time.Duration) string {
	switch {
	case late >= 48*time.Hour:
		return strconv.Itoa(int(late/(24*time.Hour))) + "d"
	case late >= time.Hour:
		return strconv.Itoa(int(late/time.Hour)) + "h"
	}
	return strconv.Itoa(int(late/time.Minute)) + "m"
}

func automationNotFound(err error) string {
	if errors.Is(err, automation.ErrNotFound) {
		return "there is no automation with that id — list shows them"
	}
	return "could not change it: " + err.Error()
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

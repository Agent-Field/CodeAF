package session

// keep_running.go is the one door through which a conversation keeps a
// process running after its turn: bash's keep_running argument, the card that
// asks the person, and what each answer does.
//
// A BACKGROUND JOB ENDS WITH ITS CONVERSATION; A KEPT SERVICE DOES NOT. The
// difference is a person's yes, and nothing else may stand in for one. So the
// argument is a proposal — the exact command, the folder it runs in, one
// health check and a restart policy — and the card puts exactly that in front
// of the person:
//
//   - Allow keeps it under the policy the agent proposed.
//   - Allow, never restart keeps it under never.
//   - Deny runs it as an ordinary background job that ends with this turn.
//
// THE YES IS ASKED ONCE PER COMMAND AND FOLDER, AND IT IS THE PERSON'S. The
// supervisor banks it with the check and the policy they saw
// ([service.Supervisor.Approved]); the same proposal again starts without a
// card and under their policy, whatever policy the agent proposes this time,
// and a proposal that changes the check is asked about again. Only the person
// changes a policy afterwards, from the row.
//
// A KEPT SERVICE THAT FAILED IS MENDED IN PLACE. keep_running.service_id names
// one of this conversation's own services that has no process, and the changed
// command, folder or check replaces it on that same row, under that same id,
// instead of becoming a second row beside the failed one
// ([service.Supervisor.Repair]). It is the same card asked the same way — the
// old command's yes is never the new one's — and it is refused before any card
// for an id that is not this conversation's, a service still running or
// stopping, or a change that would duplicate another of its services. A deny
// leaves the failed row exactly as it was.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT. With no supervisor, no conversation
// folder to own the service, nobody to ask, or inside a task, bash has no
// keep_running argument at all ([Agent.keepAvailable]).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/service"
)

// QuestionKeep is a keep-running card: may this command keep running after
// this turn?
const QuestionKeep QuestionKind = "keep_running"

// The keys a keep-running card answers with. They never move, and the no is
// every card's outright no ([DeclineKey]).
const (
	KeepAllowKey      = "1"
	KeepAllowNeverKey = "2"
	KeepDenyKey       = DeclineKey
)

// The answers, as a restated card and a receipt spell them.
const (
	KeepAllowed      = "allow"
	KeepAllowedNever = "allow_never"
	KeepDenied       = "deny"
)

// KeepNotice is one keep-running card: what the agent proposed, and — once the
// card is restated with its answer — what the person said and what came of it.
type KeepNotice struct {
	// ID names the card while it waits.
	ID uint64 `json:"id,omitempty"`
	// CallID is the bash call that asked.
	CallID string    `json:"callId,omitempty"`
	Asked  time.Time `json:"asked"`
	Name   string    `json:"name"`
	// Command is redacted: a card travels to every surface.
	Command     string         `json:"command"`
	Dir         string         `json:"dir"`
	Health      service.Health `json:"health"`
	HealthWords string         `json:"healthWords"`
	// Policy is what the agent proposed; PolicyWords says it.
	Policy      service.Policy `json:"policy"`
	PolicyWords string         `json:"policyWords"`
	// Decided is KeepAllowed, KeepAllowedNever or KeepDenied on the receipt.
	Decided string `json:"decided,omitempty"`
	// Withdrawn says why a card stopped waiting with no answer.
	Withdrawn string `json:"withdrawn,omitempty"`
	// Service is the kept service's row after an allow.
	Service *service.Row `json:"service,omitempty"`
	// Job is the ordinary job a deny started, ending with this turn.
	Job int `json:"job,omitempty"`
	// Replaces is the id of the kept service an allow repairs in place, and
	// ReplacesName its name; both are empty for a new service.
	Replaces     string `json:"replaces,omitempty"`
	ReplacesName string `json:"replacesName,omitempty"`
}

// keepRunningArgs is bash's keep_running argument.
type keepRunningArgs struct {
	Name   string `json:"name"`
	Health struct {
		Port int    `json:"port"`
		URL  string `json:"url"`
		Cmd  string `json:"cmd"`
	} `json:"health"`
	Restart string `json:"restart"`
	// ServiceID names one of this conversation's kept services to repair in
	// place rather than keep a new one.
	ServiceID string `json:"service_id"`
}

// serviceRepairer is the supervisor's in-place repair. It is asserted rather
// than assumed so a Services door without it leaves service_id refused, never
// half-working.
type serviceRepairer interface {
	Repairable(owner, id string, spec service.Spec) (service.Row, error)
	Repair(owner, id string, spec service.Spec, policy service.Policy, from service.Provenance) (service.Row, error)
}

// keepRepairer is the Services door's repair, when it has one.
func (a *Agent) keepRepairer() (serviceRepairer, bool) {
	repairer, ok := any(a.config.Services).(serviceRepairer)
	return repairer, ok && a.config.Services != nil
}

type keepAsk struct {
	answers chan string
	notice  KeepNotice
}

// KeepPolicyWords is a policy as the card says it.
func KeepPolicyWords(policy service.Policy) string {
	switch policy {
	case service.PolicyNever:
		return "never restart"
	case service.PolicyAlways:
		return fmt.Sprintf("always restart, up to %d times in 10 minutes", service.RestartLimit)
	}
	return fmt.Sprintf("restart on failure, up to %d times in 10 minutes", service.RestartLimit)
}

// KeepOptions are a card's three answers.
func KeepOptions() []AnswerOption {
	return []AnswerOption{
		{Key: KeepAllowKey, Label: "Allow", Consequence: "It keeps running after this turn, restarting as proposed."},
		{Key: KeepAllowNeverKey, Label: "Allow, never restart", Consequence: "It keeps running after this turn and is not restarted."},
		{Key: KeepDenyKey, Label: "Deny", Consequence: "It runs as an ordinary process and stops when this turn ends.", Safe: true},
	}
}

// KeepHead is the card's first line.
func KeepHead(notice KeepNotice) string {
	if notice.Replaces != "" {
		return "keep " + firstLine(notice.Command) + " running after this turn, in place of " + keepServiceWords(notice.Replaces, notice.ReplacesName) + "?"
	}
	return "keep " + firstLine(notice.Command) + " running after this turn?"
}

// keepServiceWords is a kept service as a card or a result names it: "s3 (web
// dev server)".
func keepServiceWords(id, name string) string {
	if name == "" {
		return id
	}
	return id + " (" + name + ")"
}

// KeepQuestion is one card as a question on the block — the ONE construction
// of it, which the engine publishes and a surface draws.
func KeepQuestion(notice KeepNotice) Question {
	card := notice
	card.Service = nil
	return Question{
		ID:      notice.ID,
		Kind:    QuestionKeep,
		Ask:     AskPermission,
		Form:    FormCard,
		Asker:   Asker{Kind: AskerModel},
		Head:    KeepHead(notice),
		Reason:  notice.HealthWords + " · every " + service.CheckEvery.String() + " · " + notice.PolicyWords + " · folder " + notice.Dir,
		Subject: SubjectRef{Kind: SubjectCall, CallID: notice.CallID, Name: notice.Name},
		Options: KeepOptions(),
		Stakes:  StakesReversible,
		// THE TURN IS STOPPED ON IT: the bash call waits for the answer and the
		// card carries no clock, because silence is not a yes.
		Blocking: Blocking{Turn: true},
		Scope:    []AnswerScope{ScopeOnce},
		Asked:    notice.Asked,
		Keep:     &card,
	}
}

// ResolveKeep delivers the person's answer to the card waiting on it, and
// reports whether the key was one of its answers and a card was waiting.
func (a *Agent) ResolveKeep(id uint64, key string) bool {
	switch key {
	case KeepAllowKey, KeepAllowNeverKey, KeepDenyKey:
	default:
		return false
	}
	a.mu.Lock()
	ask, waiting := a.keepAsks[id]
	if waiting {
		delete(a.keepAsks, id)
	}
	a.mu.Unlock()
	if waiting {
		ask.answers <- key
	}
	return waiting
}

// keepOwner is who owns this conversation's services: its folder's name,
// which is the agent id every surface already addresses it by.
func (a *Agent) keepOwner() string {
	file := a.SessionPath()
	if file == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(file))
}

// keepAvailable says whether bash carries keep_running at all.
func (a *Agent) keepAvailable() bool {
	return a.config.Services != nil && a.keepOwner() != "" && a.config.AskConsent && !a.config.InTask
}

// keepRunningProperty is bash's keep_running schema, its figure the
// supervisor's own.
var keepRunningProperty = strings.ReplaceAll(keepRunningShape, "{every}", service.CheckEvery.String())

const keepRunningShape = `{"type":"object","description":"Run after this turn with consent, in the current folder; checks every {every}. The user owns policy.","properties":{"name":{"type":"string","description":"Short display name."},"health":{"type":"object","description":"Exactly one: TCP port, HTTP URL returning 200, or command exiting 0.","properties":{"port":{"type":"integer"},"url":{"type":"string"},"cmd":{"type":"string"}}},"restart":{"type":"string","enum":["on_failure","always","never"],"description":"Proposed; the user decides."},"service_id":{"type":"string","description":"Repair your stopped/failed service (e.g. s3) on its same row. Changed command/check needs approval; stop it first if running."}},"required":["health"]}`

// keepSentence is the bash description's line about it.
func (a *Agent) keepSentence() string {
	if !a.keepAvailable() {
		return ""
	}
	return " Use keep_running to ask before keeping servers/watchers after this turn; never bypass consent with nohup or &."
}

// schemaWithKeep adds keep_running to bash's schema where it can work.
func (a *Agent) schemaWithKeep(schema json.RawMessage) json.RawMessage {
	if !a.keepAvailable() {
		return schema
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(schema, &root); err != nil {
		return schema
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(root["properties"], &properties); err != nil {
		return schema
	}
	properties["keep_running"] = json.RawMessage(keepRunningProperty)
	encoded, err := json.Marshal(properties)
	if err != nil {
		return schema
	}
	root["properties"] = encoded
	out, err := json.Marshal(root)
	if err != nil {
		return schema
	}
	return out
}

// keepRunning is bash's keep_running road.
func (a *Agent) keepRunning(ctx context.Context, command string, args keepRunningArgs) (string, bool, error) {
	if !a.keepAvailable() {
		return "keep_running is not available here, so nothing was started. Run it with background:true for this conversation only.", true, nil
	}
	a.jobs.mu.Lock()
	dir := a.jobs.workspace
	a.jobs.mu.Unlock()
	proposed, err := service.ParsePolicy(args.Restart)
	if err != nil {
		return err.Error() + "; nothing was started.", true, nil
	}
	spec, err := service.Spec{
		Name: args.Name, Command: command, Dir: dir,
		Health: service.Health{Port: args.Health.Port, URL: args.Health.URL, Cmd: args.Health.Cmd},
	}.Check()
	if err != nil {
		return err.Error() + "; nothing was started.", true, nil
	}
	callID := callIDFrom(ctx)
	a.mu.Lock()
	asked, turn := a.personAsk, a.turnSeq
	a.mu.Unlock()
	// The started-by line is the person's own request, redacted like
	// everything else a row carries.
	from := service.Provenance{Label: redact.Secrets(firstLine(asked)), Turn: turn, CallID: callID}
	// A REPAIR IS REFUSED BEFORE ANYBODY IS ASKED. An id that is not this
	// conversation's, a service still running or stopping, or a duplicate is
	// said now, with no card raised and nothing started or signalled.
	var repairing service.Row
	if id := strings.TrimSpace(args.ServiceID); id != "" {
		row, refusal := a.keepRepairable(id, spec)
		if refusal != "" {
			return refusal, true, nil
		}
		repairing = row
	}
	if policy, ok := a.config.Services.Approved(spec); ok {
		text, isError, row := a.keepStarted(spec, policy, from, true, repairing.ID)
		markToolService(ctx, row.ID, isError)
		return text, isError, nil
	}

	notice := KeepNotice{
		CallID: callID, Asked: time.Now().UTC(), Name: spec.Name, Command: redact.Secrets(spec.Command), Dir: spec.Dir,
		Health: redactedHealth(spec.Health), HealthWords: redact.Secrets(spec.Health.Words()),
		Policy: proposed, PolicyWords: KeepPolicyWords(proposed),
		Replaces: repairing.ID, ReplacesName: repairing.Name,
	}
	key, err := a.askKeep(ctx, &notice)
	if err != nil {
		return "Not kept running: " + err.Error() + ". Nothing was started.", true, nil
	}
	switch key {
	case KeepAllowKey, KeepAllowNeverKey:
		policy, decided := proposed, KeepAllowed
		if key == KeepAllowNeverKey {
			policy, decided = service.PolicyNever, KeepAllowedNever
		}
		text, isError, row := a.keepStarted(spec, policy, from, false, repairing.ID)
		markToolService(ctx, row.ID, isError)
		if row.ID != "" {
			notice.Service = &row
		}
		a.keepReceipt(notice, decided)
		return text, isError, nil
	default:
		started, err := a.jobs.startInUnder(withCallID(context.Background(), callIDFrom(ctx)), command, "")
		if err != nil {
			a.keepReceipt(notice, KeepDenied)
			return "The person chose not to keep it running, and it could not be started as an ordinary job: " + err.Error(), true, nil
		}
		a.mu.Lock()
		if a.turnJobs == nil {
			a.turnJobs = map[int]uint64{}
		}
		a.turnJobs[started.id] = a.turnSeq
		a.mu.Unlock()
		markToolJob(toolProcessFrom(ctx), started)
		notice.Job = started.id
		a.keepReceipt(notice, KeepDenied)
		unchanged := ""
		if repairing.ID != "" {
			unchanged = " " + repairing.ID + " is unchanged."
		}
		return fmt.Sprintf("The person chose not to keep it running. It runs as job %d for this turn only and stops when this turn ends; log at %s.%s", started.id, started.logPath, unchanged), false, nil
	}
}

// keepRepairable answers the row service_id names when it can be repaired to
// spec, or the refusal the model reads in place of a card.
func (a *Agent) keepRepairable(id string, spec service.Spec) (service.Row, string) {
	repairer, ok := a.keepRepairer()
	if !ok {
		return service.Row{}, "service_id is not available here, so nothing was started or changed. Propose it without service_id to keep it as a new process."
	}
	// The id is said back, so one the model made up is bounded before it is.
	shown := id
	if len(shown) > 16 {
		shown = shown[:16] + "…"
	}
	row, err := repairer.Repairable(a.keepOwner(), id, spec)
	switch {
	case err == nil:
		return row, ""
	case errors.Is(err, service.ErrNotFound):
		return service.Row{}, fmt.Sprintf("%s is not one of this conversation's kept-running processes; jobs list shows yours. Nothing was started or changed.", shown)
	case errors.Is(err, service.ErrRunning):
		return service.Row{}, fmt.Sprintf("%s is still running, and a replacement never runs beside it. Nothing was started or changed. Ask the person to stop it from its row, then propose the change again.", shown)
	case errors.Is(err, service.ErrStopping):
		return service.Row{}, fmt.Sprintf("%s is still stopping. Nothing was started or changed; propose the change again once it has stopped.", shown)
	case errors.Is(err, service.ErrDuplicate):
		return service.Row{}, "another of this conversation's kept-running processes already has that command in this folder. Nothing was started or changed."
	}
	return service.Row{}, "Not kept running: " + err.Error() + ". Nothing was started or changed."
}

// keepStarted keeps spec under policy — repairing the service repairs names in
// place, when it names one — and says what came of it, with the row.
func (a *Agent) keepStarted(spec service.Spec, policy service.Policy, from service.Provenance, remembered bool, repairs string) (string, bool, service.Row) {
	var row service.Row
	var err error
	if repairs != "" {
		repairer, ok := a.keepRepairer()
		if !ok {
			return "Not kept running: service_id is not available here.", true, row
		}
		row, err = repairer.Repair(a.keepOwner(), repairs, spec, policy, from)
		if err != nil && row.ID == "" {
			// The card was answered; what changed while it waited is said, and
			// the row is as it was.
			return fmt.Sprintf("Not kept running: %s. %s is unchanged and nothing was started.", err.Error(), repairs), true, row
		}
	} else {
		row, err = a.config.Services.Keep(a.keepOwner(), spec, policy, from)
	}
	if err != nil && row.ID == "" {
		return "Not kept running: " + err.Error() + ".", true, row
	}
	if row.PortConflict {
		return fmt.Sprintf("Not started: %s. Nothing of the other program was touched. Tell the person, or propose the command on another port.", row.Reason), true, row
	}
	if !row.Running {
		return fmt.Sprintf("Kept as %s, but it is not running: %s.", row.ID, row.Reason), true, row
	}
	lead := "Kept running"
	if repairs != "" {
		lead = "Replaced its command and kept it running"
	}
	if remembered {
		lead += " (the person already approved this command in this folder)"
	}
	return fmt.Sprintf("%s as %s (%s) · checks %s every %s · %s · log at %s. It keeps running after this turn; only the person changes its restart policy.",
		lead, row.ID, row.Name, row.HealthWords, service.CheckEvery, KeepPolicyWords(row.Policy), row.LogPath), false, row
}

// askKeep raises one card and waits for the person for as long as that takes.
// A conversation nobody is watching gets no card; the turn ending is the card
// going unanswered, and an unanswered card starts nothing.
func (a *Agent) askKeep(ctx context.Context, notice *KeepNotice) (string, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return "", errAgentClosed
	}
	hub := a.hub
	if hub == nil {
		a.mu.Unlock()
		return "", errors.New("nobody is here to ask")
	}
	a.keepSeq++
	notice.ID = a.keepSeq
	answers := make(chan string, 1)
	if a.keepAsks == nil {
		a.keepAsks = map[uint64]*keepAsk{}
	}
	a.keepAsks[notice.ID] = &keepAsk{answers: answers, notice: *notice}
	a.mu.Unlock()

	card := *notice
	defer a.presenceAskingWhole(ctx, KeepQuestion(card), func() {
		hub.send(Event{Kind: EventKeepRunning, Tool: "bash", CallID: card.CallID, Keep: &card})
	})()
	select {
	case key := <-answers:
		return key, nil
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.keepAsks, notice.ID)
		a.mu.Unlock()
		return "", errors.New("the turn ended before an answer")
	}
}

// keepReceipt restates the card with its answer — the receipt a surface draws
// in place of the card, and what a replay carries instead of an open question.
func (a *Agent) keepReceipt(notice KeepNotice, decided string) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub == nil {
		return
	}
	notice.Decided = decided
	hub.send(Event{Kind: EventKeepRunning, Tool: "bash", CallID: notice.CallID, Keep: &notice})
}

// endTurnJobs ends the jobs a deny left for the turn that started them. The
// kill waits out its grace off the turn's own cleanup, so ending a turn never
// waits on a process.
func (a *Agent) endTurnJobs(turn uint64) {
	a.mu.Lock()
	var ending []int
	for id, started := range a.turnJobs {
		if started == turn {
			ending = append(ending, id)
			delete(a.turnJobs, id)
		}
	}
	a.mu.Unlock()
	for _, id := range ending {
		go a.jobs.kill(context.Background(), id)
	}
}

// keptRunningList is the `jobs list` lines for this conversation's kept
// services, so the model can see them after the turn that started them.
func (a *Agent) keptRunningList() string {
	if a.config.Services == nil || a.keepOwner() == "" {
		return ""
	}
	rows := a.config.Services.List(a.keepOwner())
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nKept running (the person controls these):")
	for _, row := range rows {
		fmt.Fprintf(&b, "\n%s · %s · %s · %s · %s · restarted %d of %d",
			row.ID, row.Name, row.State, row.HealthWords, KeepPolicyWords(row.Policy), row.Restarts, row.RestartLimit)
		if row.Reason != "" {
			b.WriteString(" · " + row.Reason)
		}
	}
	return b.String()
}

func redactedHealth(h service.Health) service.Health {
	h.URL = redact.Secrets(h.URL)
	h.Cmd = redact.Secrets(h.Cmd)
	return h
}

//go:build e2e

package e2e

// THE TASK NAME A PERSON MEETS, ON THE REAL WIRE.
//
// This is issue #441's paid regression. It makes the shaper decline by handing
// [session.Agent.StartTask] an already-cancelled context, which is the ordinary
// silent pass-through that door promises. The task is therefore admitted under
// its mechanical paste label, and the asynchronous task-name role has to replace
// it. That isolates the naming call: the eventual name cannot have come from
// the shaper or from a fixture. The worker then runs for real and must report a
// hostile literal path exactly as it appears inside the request.
//
// The request is deliberately awkward text rather than a path the test owns. It
// has the wrappers a terminal paste writes, an absolute path containing a space,
// quotes, a backslash, Unicode, a dollar sign and an @ mention. The checkpoint
// must keep every byte as request data while the row gets a short human name,
// and the worker's report proves the same document reached its model prompt.
//
// It is opt-in through the package's `e2e` build tag and [newWorld]'s explicit
// OPENROUTER_API_KEY check. A missing key skips before anything is asserted; a
// key whose real request fails does NOT skip and cannot look like a pass.
//
//	go test -tags e2e -run '^TestTaskNamingE2E$' -count=1 -timeout 8m -v ./internal/e2e/

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	taskNamingE2EModel   = "deepseek/deepseek-v4-flash-0731"
	taskNamingE2EBaseURL = "https://openrouter.ai/api/v1"
	taskNamingE2ELiteral = "/Users/example/My Project/\"Δ parser\"/a\\b/$draft/@owner"
)

const taskNamingE2ERequest = "paste 1:\n" +
	"```text\n" +
	"Audit the literal task-text boundary for paths, quotes, backslashes, Unicode, dollar signs, and @ mentions.\n" +
	"Begin the report by copying this exact inert literal on its own line:\n" +
	taskNamingE2ELiteral + "\n" +
	"Do not put explanation before it. Do not call tools or change files.\n" +
	"```\n\n" +
	"paste 2:\n" +
	"```text\n" +
	"Audit the literal task-text boundary for paths, quotes, backslashes, Unicode, dollar signs, and @ mentions.\n" +
	"Begin the report by copying this exact inert literal on its own line:\n" +
	taskNamingE2ELiteral + "\n" +
	"Do not put explanation before it. Do not call tools or change files.\n" +
	"```"

const taskWorkerE2ERequest = "wire text proof\n\n" + taskNamingE2ERequest

// TestTaskNamingE2E starts at the exported door a surface uses and reads the
// result from both outputs a surface and a restart trust: EventTaskUpdate and
// the session's tasks.json checkpoint.
func TestTaskNamingE2E(t *testing.T) {
	w := newWorld(t)
	workspace := newWorkspace(t, "task-name-text", false)
	pinTaskNamingE2EModel(t, w, workspace)
	agent, place := w.open(workspace, func(cfg *session.Config) {
		// The copied profile may point at a compatible private gateway. This
		// regression explicitly claims OpenRouter, so neither that endpoint nor
		// a credential stored for it may silently stand in for the real wire.
		cfg.BaseURL = taskNamingE2EBaseURL
		cfg.APIKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
		// conversationConfig deliberately wires only what the ambient suite
		// needs. This scenario starts actual work, so carry the two remaining
		// model rows through the same values the v3 door would read.
		cfg.TaskModel = config.TaskModelAt(w.settings.ProfileDir)
		cfg.ModelFallbacks = config.ParseModelFallbacks(config.ModelFallbacksAt(w.settings.ProfileDir))
		// One worker response is the wire-fidelity proof; an audit would add a
		// different provider question after that proof and is outside this test.
		cfg.TaskAudit = false
	})

	updates, stopUpdates := agent.WatchTaskUpdates()
	defer stopUpdates()

	// A cancelled shaper is a supported failure, not a test seam: StartTask
	// must still admit the person's exact words and start the asynchronous
	// namer. Five seconds leaves ample room for local checkpoint/worktree work
	// while remaining far below the namer's twenty-second wire window.
	shaping, cancelShaping := context.WithCancel(context.Background())
	cancelShaping()
	began := time.Now()
	id, fallback, err := agent.StartTask(shaping, taskNamingE2ERequest)
	admittedIn := time.Since(began)
	if err != nil {
		t.Fatalf("StartTask refused arbitrary text: %v", err)
	}
	if id == 0 {
		t.Fatal("StartTask admitted task zero")
	}
	if admittedIn >= 5*time.Second {
		t.Fatalf("task admission waited %s for naming; want naming off the admission path", admittedIn.Round(time.Millisecond))
	}
	if strings.TrimSpace(fallback) == "" {
		t.Fatal("task admission returned no fallback while its name was in flight")
	}
	// This is the mechanical first-line fallback produced from the exact text
	// [unfoldPastes] gives the session. If it were already a meaningful name,
	// the later update would prove only that names can change, not that the
	// transport wrapper which caused #441 is replaced asynchronously.
	if fallback != "paste 1:" {
		t.Fatalf("mechanical fallback is %q, want the transport-derived %q", fallback, "paste 1:")
	}

	t.Logf("task %d admitted in %s as %q", id, admittedIn.Round(time.Millisecond), fallback)

	// Cancel this disposable node so its real naming call does not compete with
	// a worker call on the same provider lane. This also exercises the narrow
	// pre-handle cancellation window that the first paid run exposed.
	stopped, err := agent.Cancel("task:" + strconv.FormatUint(id, 10))
	if err != nil {
		t.Fatalf("stop naming task %d: %v", id, err)
	}
	name, namedLanding := awaitPublishedTaskResult(t, updates, id, fallback)
	if namedLanding.State != session.TaskFailed || !namedLanding.Stopped {
		t.Fatalf("naming task %d did not stop once: %+v", id, namedLanding)
	}
	persisted := awaitPersistedTaskName(t, place.Tasks(), id, name)
	if persisted.Request != taskNamingE2ERequest {
		t.Fatalf("checkpoint changed the arbitrary request\n got: %q\nwant: %q", persisted.Request, taskNamingE2ERequest)
	}
	t.Logf("task %d %s and was published and persisted as %q on %s", id, stopped, name, taskNamingE2EModel)

	// A second task starts with an intentional three-word label, so no naming
	// errand competes with its worker. Everything after that first line is the
	// same hostile paste document; the real worker must echo its literal exactly.
	workerID, workerTitle, err := agent.StartTask(shaping, taskWorkerE2ERequest)
	if err != nil {
		t.Fatalf("start wire worker: %v", err)
	}
	if workerTitle != "wire text proof" {
		t.Fatalf("worker fallback = %q, want the intentional label", workerTitle)
	}
	worker := awaitTaskLanding(t, updates, workerID)
	if worker.State != session.TaskDone {
		t.Fatalf("worker task %d landed %s: %s", workerID, worker.State, worker.Report)
	}
	if !strings.Contains(worker.Report, taskNamingE2ELiteral) {
		t.Fatalf("real worker did not echo the hostile literal exactly; report: %q", worker.Report)
	}
	workerPersisted := awaitPersistedTaskName(t, place.Tasks(), workerID, workerTitle)
	if workerPersisted.Request != taskWorkerE2ERequest {
		t.Fatalf("worker checkpoint changed the arbitrary request\n got: %q\nwant: %q", workerPersisted.Request, taskWorkerE2ERequest)
	}
	t.Logf("task %d completed on %s and echoed the hostile literal exactly", workerID, taskNamingE2EModel)
}

// pinTaskNamingE2EModel leaves no copied profile row able to move a call onto a
// different model. The role pins are the three text calls this scenario can
// reach; the five tiers, talk seat, task seat and fallback cover every rung
// beneath them. Media roles stay untouched because this test sends no media.
func pinTaskNamingE2EModel(t *testing.T, w *world, workspace string) {
	t.Helper()
	if err := config.WriteChatModel("", taskNamingE2EModel); err != nil {
		t.Fatalf("pin %s: %v", config.KeyChatModel, err)
	}
	registry := config.NewSettings(config.SettingsOptions{})
	write := func(key, value string) {
		t.Helper()
		row, found := registry.Row(key)
		if !found {
			t.Fatalf("settings registry has no row %q", key)
		}
		if err := row.Apply(value); err != nil {
			t.Fatalf("pin %s: %v", key, err)
		}
	}
	for _, key := range []string{
		config.KeyTierReflexModel,
		config.KeyTierLowModel,
		config.KeyTierHighModel,
		config.KeyTierMastermindModel,
		config.KeyTierWorkerModel,
		config.KeyTaskModel,
		config.KeyModelFallbacks,
	} {
		write(key, taskNamingE2EModel)
	}
	pins := []string{
		string(roles.RoleTaskName) + ":" + taskNamingE2EModel,
		string(roles.RoleShaper) + ":" + taskNamingE2EModel,
		string(roles.RoleWorker) + ":" + taskNamingE2EModel,
	}
	write(config.KeyModelRoles, strings.Join(pins, ","))

	if got := config.ChatModelAt(w.settings.ProfileDir); got != taskNamingE2EModel {
		t.Fatalf("talk model is %q, want %q", got, taskNamingE2EModel)
	}
	if got := config.TaskModelAt(w.settings.ProfileDir); got != taskNamingE2EModel {
		t.Fatalf("task model is %q, want %q", got, taskNamingE2EModel)
	}
	// Resolve against the exact project the conversation will open. A project
	// layer can override a profile pin, so checking the profile's directory
	// here would prove a different configuration from the one on the wire.
	resolved := w.rolesSource(workspace)
	for _, role := range []roles.Role{roles.RoleTaskName, roles.RoleShaper, roles.RoleWorker} {
		got, ok := resolved(roles.PinKey(role))
		if !ok || got != taskNamingE2EModel {
			t.Fatalf("role %s resolves to %q, %v; want %q", role, got, ok, taskNamingE2EModel)
		}
	}
}

// awaitPublishedTaskResult waits for both independent real outcomes: the
// background rename and the worker's landing. It does not retry either call;
// one missing outcome fails the test.
func awaitPublishedTaskResult(t *testing.T, updates <-chan session.Event, id uint64, fallback string) (string, session.TaskNotice) {
	t.Helper()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	var name string
	var landed *session.TaskNotice
	for {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatalf("task update lane closed before task %d was named", id)
			}
			if event.Kind != session.EventTaskUpdate || event.Task == nil || event.Task.ID != id {
				continue
			}
			candidate := strings.TrimSpace(event.Task.Title)
			if candidate != "" && candidate != strings.TrimSpace(fallback) {
				if problem := taskNameProblem(candidate); problem != "" {
					t.Fatalf("task %d was published as %q: %s", id, candidate, problem)
				}
				name = candidate
			}
			if event.Task.State == session.TaskDone || event.Task.State == session.TaskFailed || event.Task.State == session.TaskUnverified {
				copy := *event.Task
				landed = &copy
			}
			if name != "" && landed != nil {
				return name, *landed
			}
		case <-timer.C:
			t.Fatalf("task %d did not both rename and land; name=%q landed=%v fallback=%q", id, name, landed != nil, fallback)
		}
	}
}

func awaitTaskLanding(t *testing.T, updates <-chan session.Event, id uint64) session.TaskNotice {
	t.Helper()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatalf("task update lane closed before task %d landed", id)
			}
			if event.Kind != session.EventTaskUpdate || event.Task == nil || event.Task.ID != id {
				continue
			}
			if event.Task.State == session.TaskDone || event.Task.State == session.TaskFailed || event.Task.State == session.TaskUnverified {
				return *event.Task
			}
		case <-timer.C:
			t.Fatalf("task %d did not land after its real worker call", id)
		}
	}
}

// taskNameProblem is the person-facing contract, independent of the package's
// private cleaner: two or three semantic words, with neither a pasted ordinal
// nor a machine path standing in for the work.
func taskNameProblem(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if len(fields) < 2 || len(fields) > 3 {
		return fmt.Sprintf("has %d words, want two or three", len(fields))
	}
	if strings.ContainsAny(name, "/\\") {
		return "contains a path separator"
	}
	lower := strings.ToLower(strings.Trim(strings.TrimSpace(name), " .,:;!?[](){}\"'"))
	parts := strings.Fields(lower)
	if len(parts) == 2 {
		if _, err := strconv.Atoi(strings.Trim(parts[1], " .,:;!?")); err == nil {
			switch parts[0] {
			case "paste", "task", "part", "request", "work":
				return "is an ordinal wrapper rather than a name"
			}
		}
	}
	semantic := false
	for _, part := range parts {
		part = strings.Trim(part, " .,:;!?[](){}\"'")
		switch part {
		case "text", "input", "literal", "path", "paths", "parser", "parsing", "boundary", "unicode", "string", "strings", "handling", "audit", "content", "character", "characters", "escaping", "preservation", "integrity":
			semantic = true
		}
	}
	if !semantic {
		return "does not name the text/path work in the request"
	}
	return ""
}

type taskNamingRecord struct {
	ID      uint64 `json:"id"`
	Title   string `json:"title"`
	Request string `json:"request"`
}

// awaitPersistedTaskName reads the public on-disk contract rather than an
// in-process value. Rename checkpoints before it publishes, so retries here
// cover only filesystem scheduling and never another model call.
func awaitPersistedTaskName(t *testing.T, path string, id uint64, want string) taskNamingRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			var document struct {
				Nodes []taskNamingRecord `json:"nodes"`
			}
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatalf("decode task checkpoint %s: %v", path, err)
			}
			for _, node := range document.Nodes {
				if node.ID == id && node.Title == want {
					return node
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("task checkpoint %s never persisted task %d as %q", path, id, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

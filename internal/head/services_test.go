package head

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func headServiceFixture(t *testing.T, graph *store.Store, id, name, node string, pid int) store.Service {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: node, Brief: "serve", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "services", Intent: "run app", ServiceIntent: true}); err != nil {
		t.Fatal(err)
	}
	service, err := graph.PromoteService(store.Service{
		ID: id, Name: name, Command: "npm run dev", Dir: t.TempDir(), LogPath: t.TempDir() + "/service.log",
		Health: store.ServiceHealth{Kind: store.ServiceHealthPort, Value: fmt.Sprint(5000 + pid)},
		PID:    pid, StartedAt: time.Now().Add(-time.Hour),
		Provenance: store.ServiceProvenance{OriginJobID: pid, LeafNodeID: node},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// serviceTool runs one service call the way the loop does and hands back what
// the tool told it, plus the run so a test can see whether the tool spoke for
// the whole turn. Services are the person's own persistent effects rather than
// graph work, which is why stopping one is done rather than queued behind a
// gate — and why the recognizer that used to claim these sentences was the wrong
// place for that judgment.
func serviceTool(t *testing.T, graph *store.Store, user store.Message, args map[string]any) (*beltRun, string, bool) {
	t.Helper()
	run := &beltRun{head: New(nil, graph), user: user}
	result, failed := run.execute(beltToolService, beltArguments(t, args))
	return run, result, failed
}

func TestServiceConversationUniqueStopAndRestart(t *testing.T) {
	graph := openHeadStore(t)
	service := headServiceFixture(t, graph, "svc", "dev-server", "leaf", 41)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "stop the dev server"})
	run, result, failed := serviceTool(t, graph, user, map[string]any{
		"verb": "stop", "describes": "the dev server"})
	if failed {
		t.Fatalf("stop refused: %s", result)
	}
	if !run.acted {
		t.Fatalf("stopping a service recorded no receipt: %q", result)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 1 || commands[0].Kind != store.CommandServiceStop || commands[0].Target != service.ID {
		t.Fatalf("stop commands = %+v", commands)
	}

	// "restart it" with one thing running: the description is empty and the one
	// running service is what it can only mean.
	graph2 := openHeadStore(t)
	service2 := headServiceFixture(t, graph2, "svc", "preview", "leaf", 42)
	user2, _ := graph2.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "restart it"})
	if _, result, failed := serviceTool(t, graph2, user2, map[string]any{"verb": "restart"}); failed {
		t.Fatalf("restart refused: %s", result)
	}
	commands, _ = graph2.PendingCommands(10)
	if len(commands) != 1 || commands[0].Kind != store.CommandServiceRestart || commands[0].Target != service2.ID {
		t.Fatalf("restart commands = %+v", commands)
	}
}

func TestStoppedServiceCanResolveStartItAgain(t *testing.T) {
	graph := openHeadStore(t)
	service := headServiceFixture(t, graph, "svc", "preview", "leaf", 43)
	if err := graph.StopService(service.ID, "stale at startup"); err != nil {
		t.Fatal(err)
	}
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "start it again"})
	if _, result, failed := serviceTool(t, graph, user, map[string]any{"verb": "restart"}); failed {
		t.Fatalf("start-again refused: %s", result)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 1 || commands[0].Kind != store.CommandServiceRestart || commands[0].Target != service.ID {
		t.Fatalf("start-again commands = %+v", commands)
	}
}

// Two services a description reaches equally. The tool refuses to choose and
// names both; the durable numbered question is the ask tool's, and the choice
// comes back as an ordinary turn — so nothing is journaled until the person has
// said which.
func TestServiceConversationAmbiguityUsesOptions(t *testing.T) {
	graph := openHeadStore(t)
	first := headServiceFixture(t, graph, "one", "api-preview", "leaf-one", 51)
	second := headServiceFixture(t, graph, "two", "web-preview", "leaf-two", 52)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "restart it"})

	run, result, failed := serviceTool(t, graph, user, map[string]any{"verb": "restart"})
	if failed {
		t.Fatalf("an ambiguous restart errored instead of offering candidates: %s", result)
	}
	if !strings.Contains(result, "more than one service matches") ||
		!strings.Contains(result, first.Name) || !strings.Contains(result, second.Name) {
		t.Fatalf("the candidate list is not both services by name:\n%s", result)
	}
	if run.acted {
		t.Fatal("an ambiguous restart acted anyway")
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("an ambiguous restart journaled work: %+v", commands)
	}

	head, client := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolAsk, map[string]any{
			"question": "Which service do you mean?",
			"options":  []string{first.Name, second.Name}})}})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	messages, _ := graph.Messages("services", user.Seq, 10)
	if len(messages) != 1 || len(messages[0].Options) != 2 || !strings.Contains(messages[0].Body, "Which service") {
		t.Fatalf("ambiguity message = %+v", messages)
	}

	client.turns = append(client.turns, beltTurn{calls: []ai.ToolCall{
		beltCall("c2", beltToolService, map[string]any{
			"verb": "restart", "describes": "web"})}}, beltTurn{})
	answer, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "2"})
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 1 || commands[0].Target != second.ID || commands[0].Target == first.ID {
		t.Fatalf("selected command = %+v", commands)
	}
}

func TestShutItAllDownStopsServicesAndGatesInFlightWork(t *testing.T) {
	graph := openHeadStore(t)
	first := headServiceFixture(t, graph, "one", "api-preview", "leaf-one", 71)
	second := headServiceFixture(t, graph, "two", "web-preview", "leaf-two", 72)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "render", Title: "Audio render", Brief: "render the audio", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "services", Intent: "render the audio"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "render", Cost: 0.85}); err != nil {
		t.Fatal(err)
	}
	h := New(nil, graph)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "shut it all down"})
	// The total phrasing is still recognized, and the reading says out loud how
	// far it reaches before anything is done about it.
	if !recognizesShutdownAll(user.Body) {
		t.Fatal("the one total phrasing is no longer recognized")
	}
	if reading := deterministicReading(t, h, user); !strings.Contains(reading,
		`reads as "stop everything" — every running service AND every live job; ask before the jobs`) {
		t.Fatalf("the blast radius was not reported to the loop:\n%s", reading)
	}
	run, result, failed := serviceTool(t, graph, user, map[string]any{"verb": "stop_everything"})
	if failed {
		t.Fatalf("stop_everything refused: %s", result)
	}
	// The gate owns the words: a consent question is the whole reply, and the
	// loop must not speak over it.
	if !run.spoke || !run.acted {
		t.Fatalf("the shutdown gate did not claim the turn: spoke=%t acted=%t", run.spoke, run.acted)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 2 {
		t.Fatalf("service stop commands = %+v", commands)
	}
	stopped := map[string]bool{}
	for _, command := range commands {
		if command.Kind != store.CommandServiceStop {
			t.Fatalf("unexpected command %+v", command)
		}
		stopped[command.Target] = true
	}
	if !stopped[first.ID] || !stopped[second.ID] {
		t.Fatalf("shutdown missed a service: %+v", commands)
	}
	messages, _ := graph.Messages("services", user.Seq, 10)
	if len(messages) != 1 {
		t.Fatalf("shutdown messages = %+v", messages)
	}
	gate := messages[0]
	// The two service leaves are themselves open work here, so the gate quotes
	// three jobs and the one spend recorded against the render.
	if !strings.Contains(gate.Body, "Stopping api-preview and web-preview.") ||
		!strings.Contains(gate.Body, "3 jobs are still running") ||
		!strings.Contains(gate.Body, "~$0.85 spent") ||
		!strings.Contains(gate.Body, `"default":"2"`) || len(gate.Options) != 2 {
		t.Fatalf("shutdown gate = %+v", gate)
	}

	answer, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "1"})
	if handled, err := h.answerPendingQuestion(context.Background(), answer); err != nil || !handled {
		t.Fatalf("cancel answer handled=%v err=%v", handled, err)
	}
	commands, _ = graph.PendingCommands(10)
	cancels := 0
	for _, command := range commands {
		if command.Kind == store.CommandCancel && command.Target == "render" {
			cancels++
		}
	}
	if cancels != 1 {
		t.Fatalf("cancel commands = %+v", commands)
	}
}

func TestShutItAllDownWithNothingRunningStaysCalm(t *testing.T) {
	graph := openHeadStore(t)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "stop everything"})
	if _, result, failed := serviceTool(t, graph, user, map[string]any{"verb": "stop_everything"}); failed {
		t.Fatalf("empty shutdown refused: %s", result)
	}
	messages, _ := graph.Messages("services", user.Seq, 10)
	if len(messages) != 1 || messages[0].Body != "Nothing is running." || len(messages[0].Options) != 0 {
		t.Fatalf("empty shutdown messages = %+v", messages)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("empty shutdown issued commands: %+v", commands)
	}
}

func TestShutdownRecognitionDoesNotSwallowSingleServiceStops(t *testing.T) {
	for _, phrase := range []string{"shut it all down", "stop everything", "shut everything down", "stop all services"} {
		if !recognizesShutdownAll(phrase) {
			t.Fatalf("did not recognize shutdown: %q", phrase)
		}
	}
	for _, phrase := range []string{
		"stop the dev server", "restart it", "stop the api preview service",
		// An exception is not a total: read as one, "kill everything except the
		// finance one" stops the very job the user asked to spare. A set with a
		// hole in it belongs to the toolbelt.
		"kill everything except the finance one", "stop everything apart from the scans",
		"shut everything down but keep the dev server",
	} {
		if recognizesShutdownAll(phrase) {
			t.Fatalf("false shutdown recognition: %q", phrase)
		}
	}
}

func TestHygieneNudgeOptionsRouteKeepAndStop(t *testing.T) {
	graph := openHeadStore(t)
	service := headServiceFixture(t, graph, "svc", "dev-server", "leaf", 81)
	h := New(nil, graph)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "keep"})
	handled, err := h.applyServiceOption(user, store.QuestionOption{
		Label: "keep", Value: store.ServiceHygieneKeepValue(service.ID)})
	if err != nil || !handled {
		t.Fatalf("hygiene keep handled=%v err=%v", handled, err)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("hygiene keep issued commands: %+v", commands)
	}
	messages, _ := graph.Messages("services", user.Seq, 10)
	if len(messages) != 1 || messages[0].Body != "Keeping dev-server running." {
		t.Fatalf("hygiene keep receipt = %+v", messages)
	}
	handled, err = h.applyServiceOption(user, store.QuestionOption{
		Label: "stop it", Value: store.ServiceHygieneStopValue(service.ID)})
	if err != nil || !handled {
		t.Fatalf("hygiene stop handled=%v err=%v", handled, err)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 1 || commands[0].Kind != store.CommandServiceStop || commands[0].Target != service.ID {
		t.Fatalf("hygiene stop commands = %+v", commands)
	}
}

func TestServiceRowsEnterHeadSnapshotOnlyWhenPresent(t *testing.T) {
	empty := openHeadStore(t)
	if rendered := renderServices(empty); rendered != "" {
		t.Fatalf("empty services rendered %q", rendered)
	}
	service := headServiceFixture(t, empty, "svc", "dev-server", "leaf", 61)
	rendered := renderServices(empty)
	if !strings.Contains(rendered, "service "+service.Name+" | running | :5061") || !strings.Contains(rendered, service.Command) {
		t.Fatalf("service snapshot = %q", rendered)
	}
}

func TestServiceIntentRecognitionIsNarrow(t *testing.T) {
	for _, instruction := range []string{"run the app so I can open it", "start the dev server", "keep it running"} {
		if !RecognizesServiceIntent(instruction) {
			t.Fatalf("did not recognize service intent: %q", instruction)
		}
	}
	for _, instruction := range []string{"run the tests", "start writing the report", "open the app source"} {
		if RecognizesServiceIntent(instruction) {
			t.Fatalf("false service intent: %q", instruction)
		}
	}
}

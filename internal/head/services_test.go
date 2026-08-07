package head

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
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

func TestServiceConversationUniqueStopAndRestart(t *testing.T) {
	graph := openHeadStore(t)
	service := headServiceFixture(t, graph, "svc", "dev-server", "leaf", 41)
	h := New(nil, graph)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "stop the dev server"})
	handled, err := h.manageService(user)
	if err != nil || !handled {
		t.Fatalf("stop handled=%v err=%v", handled, err)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 1 || commands[0].Kind != store.CommandServiceStop || commands[0].Target != service.ID {
		t.Fatalf("stop commands = %+v", commands)
	}

	graph2 := openHeadStore(t)
	service2 := headServiceFixture(t, graph2, "svc", "preview", "leaf", 42)
	h2 := New(nil, graph2)
	user2, _ := graph2.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "restart it"})
	handled, err = h2.manageService(user2)
	if err != nil || !handled {
		t.Fatalf("restart handled=%v err=%v", handled, err)
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
	h := New(nil, graph)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "start it again"})
	handled, err := h.manageService(user)
	if err != nil || !handled {
		t.Fatalf("start-again handled=%v err=%v", handled, err)
	}
	commands, _ := graph.PendingCommands(10)
	if len(commands) != 1 || commands[0].Kind != store.CommandServiceRestart || commands[0].Target != service.ID {
		t.Fatalf("start-again commands = %+v", commands)
	}
}

func TestServiceConversationAmbiguityUsesOptions(t *testing.T) {
	graph := openHeadStore(t)
	first := headServiceFixture(t, graph, "one", "api-preview", "leaf-one", 51)
	second := headServiceFixture(t, graph, "two", "web-preview", "leaf-two", 52)
	h := New(nil, graph)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "restart it"})
	handled, err := h.manageService(user)
	if err != nil || !handled {
		t.Fatalf("ambiguity handled=%v err=%v", handled, err)
	}
	messages, _ := graph.Messages("services", user.Seq, 10)
	if len(messages) != 1 || len(messages[0].Options) != 2 || !strings.Contains(messages[0].Body, "Which service") {
		t.Fatalf("ambiguity message = %+v", messages)
	}
	answer, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "2"})
	if handled, err := h.answerPendingQuestion(answer); err != nil || !handled {
		t.Fatalf("answer handled=%v err=%v", handled, err)
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
	handled, err := h.manageService(user)
	if err != nil || !handled {
		t.Fatalf("shutdown handled=%v err=%v", handled, err)
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
	if handled, err := h.answerPendingQuestion(answer); err != nil || !handled {
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
	h := New(nil, graph)
	user, _ := graph.PostMessage(store.Message{SessionID: "services", Role: store.RoleUser, Body: "stop everything"})
	handled, err := h.manageService(user)
	if err != nil || !handled {
		t.Fatalf("empty shutdown handled=%v err=%v", handled, err)
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

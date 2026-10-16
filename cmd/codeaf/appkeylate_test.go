package main

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
)

// A KEY THAT ARRIVES AFTER THE FIRST ASK IS THE KEY EVERY LATER ASK USES. The
// app may ask for models before it has handed over the key it keeps (keys.set),
// and the hello's warm runs before either, so the process half
// is first opened with no key and fails. That failure used to be the memo's
// answer for the rest of the process: models.list kept saying the key was
// required after keys.set had answered, and a first message failed its turn at
// once while the window waited on it. The key arriving must open the process
// half on the next ask, and a conversation must run on it.
func TestAppAProcessThatOpenedWithoutAKeyOpensOnceOneArrives(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	defer config.UseAppCredentials(config.NewAppCredentials())()
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer backend.Close()

	backend.Warm()
	// Before any key there is nothing a turn could run on, and models.list
	// says so with an empty list rather than the terminal's missing-key
	// refusal; the warm's failed open is still what the key must get past.
	if models, err := backend.ListModels(); err != nil || len(models) != 0 {
		t.Fatalf("models.list before any key = %+v, %v; want an empty list", models, err)
	}
	if err := backend.SetKey("openrouter", "sk-or-v1-dummy-not-a-real-key"); err != nil {
		t.Fatalf("keys.set = %v", err)
	}
	models, err := backend.ListModels()
	if err != nil || len(models) == 0 {
		t.Fatalf("models.list after the key arrived = %+v, %v", models, err)
	}
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Text: "say done"})
	if err != nil {
		t.Fatalf("agents.create after the key arrived = %v", err)
	}
	i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return ok && event == appwire.EventTurnDone && done.Agent == agent.ID
	})
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	rec.mu.Unlock()
	if !done.OK {
		t.Fatalf("the first message after the key arrived ended %+v", done)
	}
}

// EVERY OTHER DOOR KEEPS ITS FIRST ANSWER. A pipe or a host reads its key
// before it serves anything, so the memo hands every later conversation the
// open it already made, failed or not, and only the app's door opens again.
func TestOnlyTheAppsDoorOpensAFailedProcessHalfAgain(t *testing.T) {
	root := resolvedTempDir(t)
	wakeEnvironment(t, root)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := openEngineProcess(); err == nil {
		t.Fatal("the process half opened with no key anywhere")
	}
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	if _, err := openEngineProcess(); err == nil {
		t.Fatal("a door that is not the app opened its process half a second time")
	}
	// Callers racing to open it again build ONE process half between them.
	opened := make([]*v3Process, 8)
	var wg sync.WaitGroup
	for i := range opened {
		wg.Add(1)
		go func() {
			defer wg.Done()
			opened[i], _ = openAppEngineProcess()
		}()
	}
	wg.Wait()
	first := opened[0]
	if first == nil {
		t.Fatal("the app's door did not open the process half again")
	}
	for _, proc := range opened {
		if proc != first {
			t.Fatal("callers racing the app's door built two process halves")
		}
	}
	again, err := openAppEngineProcess()
	if err != nil || again != first {
		t.Fatal("the app's door replaced a process half that was open")
	}
	if shared, err := openEngineProcess(); err != nil || shared != first {
		t.Fatal("the other doors do not share the process half the app opened")
	}
}

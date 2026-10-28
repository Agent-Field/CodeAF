package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
)

type appModelNoticeEmitter struct {
	*appRecorder
	wire *appwire.Server
}

func (e appModelNoticeEmitter) Emit(event string, data any) {
	e.appRecorder.Emit(event, data)
	e.wire.Emit(event, data)
}

// delayedAppModels seats a lazy process catalog behind the app backend, with
// its listing held until release. Unrelated process startup must not consume
// the listing timeout before this test reaches its three-second list call.
func delayedAppModels(t *testing.T) (*appBackend, *appRecorder, func()) {
	t.Helper()
	wakeEnvironment(t, resolvedTempDir(t))
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"vendor/alpha","name":"Alpha"},{"id":"vendor/beta","name":"Beta"}]}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(unblock)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	t.Setenv("CODEAF_MODEL", "vendor/alpha")
	settings, err := config.LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	options := catalog.Options{BaseURL: server.URL, APIKey: settings.APIKey, Dir: settings.ProfileDir, HTTPClient: &http.Client{Timeout: time.Minute}}
	models := catalog.LoadLazy(t.Context(), options)
	proc := &v3Process{Settings: settings, ProfileDir: settings.ProfileDir, Models: models, Shelf: newV3ModelShelf(models, options)}
	proc.Shelf.setSources(settings.Sources)
	engineProcess.mu.Lock()
	engineProcess.proc, engineProcess.tried = proc, true
	engineProcess.mu.Unlock()
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	t.Cleanup(func() { backend.Close() })
	return backend, rec, unblock
}

func appModelsChangedCount(rec *appRecorder) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	count := 0
	for _, event := range rec.events {
		if event == "models.changed" {
			count++
		}
	}
	return count
}

// Contract 4: a list bounded at three seconds gets one completion notice on
// the actual wire; the next list includes the late rows without another fetch.
func TestAppLateModelsEmitOneCompletionOnTheWire(t *testing.T) {
	backend, rec, release := delayedAppModels(t)
	var output bytes.Buffer
	wire := appwire.NewServer(&output)
	backend.emit = backend.newAppInbox(appModelNoticeEmitter{rec, wire})
	in, input := io.Pipe()
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = wire.Serve(context.Background(), in, appwire.Hello{Version: "test"}, backend)
		close(done)
	}()
	defer func() { input.Close(); <-done }()

	backend.Warm()
	first, err := backend.ListModels()
	if err != nil || len(first) != 1 || first[0].ID != "vendor/alpha" || appModelsChangedCount(rec) != 0 {
		t.Fatalf("initial bounded list = %+v %v; notices = %d", first, err, appModelsChangedCount(rec))
	}
	release()
	rec.waitFor(t, 5*time.Second, func(event string, _ any) bool { return event == "models.changed" })
	for range 2 {
		models, err := backend.ListModels()
		if err != nil || len(models) != 2 {
			t.Fatalf("late list = %+v %v", models, err)
		}
	}
	if count := appModelsChangedCount(rec); count != 1 {
		t.Fatalf("one completion emitted %d notices", count)
	}
	proc := openedEngineProcess()
	callbacks := proc.serviceNoticesSnapshot()
	if len(callbacks) != 1 {
		t.Fatalf("app registered %d service-model subscriptions, want 1", len(callbacks))
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if left := len(proc.serviceNoticesSnapshot()); left != 0 {
		t.Fatalf("close left %d model subscriptions", left)
	}
	for _, callback := range callbacks {
		callback("provider", "fixture")
	}
	if count := appModelsChangedCount(rec); count != 1 {
		t.Fatalf("an in-flight callback emitted after close: %d notices", count)
	}
	input.Close()
	<-done
	if serveErr != nil {
		t.Fatal(serveErr)
	}
	changed := 0
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'}) {
		var frame struct {
			Event string          `json:"event"`
			Seq   uint64          `json:"seq"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Event == "models.changed" {
			changed++
			if frame.Seq == 0 || string(frame.Data) != "{}" {
				t.Fatalf("completion frame = %s", line)
			}
		}
	}
	if changed != 1 {
		t.Fatalf("wire carried %d completion events", changed)
	}
}

// Contract 4: a pending discovery owns no app worker or callback after Close.
func TestAppCloseCancelsPendingModelNotification(t *testing.T) {
	backend, rec, release := delayedAppModels(t)
	backend.Warm()
	proc := openedEngineProcess()
	callbacks := proc.serviceNoticesSnapshot()
	if len(callbacks) != 1 {
		t.Fatalf("app registered %d subscriptions, want 1", len(callbacks))
	}
	closed := make(chan error, 1)
	go func() { closed <- backend.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close waited for the provider listing")
	}
	if left := len(proc.serviceNoticesSnapshot()); left != 0 {
		t.Fatalf("close left %d subscriptions", left)
	}
	release()
	if !proc.Models.Warmed(t.Context()) {
		t.Fatal("fixture did not finish its model listing")
	}
	for _, callback := range callbacks {
		callback("provider", "fixture")
	}
	if count := appModelsChangedCount(rec); count != 0 {
		t.Fatalf("closed app emitted %d model notices", count)
	}
}

// Contract 4 and 5: app reads register once on the terminal's existing service
// subscription, and each provider completion produces one app notification.
func TestAppProviderModelCompletionsUseOneSubscription(t *testing.T) {
	backend, rec, release := delayedAppModels(t)
	backend.Warm()
	for range 2 {
		if _, err := backend.process(); err != nil {
			t.Fatal(err)
		}
	}
	proc := openedEngineProcess()
	if n := len(proc.serviceNoticesSnapshot()); n != 1 {
		t.Fatalf("repeated process reads registered %d subscriptions", n)
	}
	if n := appModelsChangedCount(rec); n != 0 {
		t.Fatalf("no listing completed, but got %d notices", n)
	}
	proc.noteServiceModelsTo("provider", "fixture")
	rec.waitFor(t, 2*time.Second, func(event string, _ any) bool { return event == appwire.EventModelsChanged })
	if n := appModelsChangedCount(rec); n != 1 {
		t.Fatalf("one provider completion emitted %d notices", n)
	}
	proc.noteServiceModelsTo("provider", "fixture")
	seen := 0
	rec.waitFor(t, 2*time.Second, func(event string, _ any) bool {
		if event == appwire.EventModelsChanged {
			seen++
		}
		return seen == 2
	})
	if n := appModelsChangedCount(rec); n != 2 {
		t.Fatalf("two provider completions emitted %d notices", n)
	}
	backend.Close()
	release()
}

type appModelPressureEmitter struct {
	wire     *appwire.Server
	entered  chan struct{}
	returned chan struct{}
}

func (e *appModelPressureEmitter) Emit(event string, data any) {
	close(e.entered)
	e.wire.Emit(event, data)
	close(e.returned)
}

func (e *appModelPressureEmitter) EmitContext(ctx context.Context, event string, data any) {
	close(e.entered)
	if emitter, ok := any(e.wire).(interface {
		EmitContext(context.Context, string, any)
	}); ok {
		emitter.EmitContext(ctx, event, data)
	} else {
		e.wire.Emit(event, data)
	}
	close(e.returned)
}

// Contract 4: stdout pressure cannot stop backend reads or lifecycle cleanup.
// The real wire writer is blocked and its documented 1,024-frame queue is full.
func TestAppModelNoticeDoesNotBlockBackendOnWirePressure(t *testing.T) {
	backend, _, release := delayedAppModels(t)
	in, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	wire := appwire.NewServer(out)
	t.Cleanup(func() {
		in.Close()
		_ = wire.Serve(context.Background(), bytes.NewReader(nil), appwire.Hello{Version: "test"}, backend)
	})
	wire.Emit(appwire.EventHello, appwire.Hello{Version: "test"})
	// Reading part of hello proves the writer reached its blocking flush.
	var first [1]byte
	if _, err := in.Read(first[:]); err != nil {
		t.Fatal(err)
	}
	for range 1024 {
		wire.Emit(appwire.EventModelsChanged, appwire.Empty{})
	}
	emit := &appModelPressureEmitter{wire: wire, entered: make(chan struct{}), returned: make(chan struct{})}
	backend.emit = backend.newAppInbox(emit)
	backend.Warm()
	proc := openedEngineProcess()
	callbacks := proc.serviceNoticesSnapshot()
	if len(callbacks) != 1 {
		t.Fatalf("model subscriptions = %d", len(callbacks))
	}
	notified := make(chan struct{})
	go func() { callbacks[0]("provider", "fixture"); close(notified) }()
	select {
	case <-emit.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("completion never reached the wire")
	}
	read := make(chan struct{})
	go func() { _ = backend.isClosing(); close(read) }()
	closed := make(chan error, 1)
	go func() { closed <- backend.Close() }()
	for name, done := range map[string]<-chan struct{}{"backend read": read, "completion callback": notified} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			// Drain only after recording failure so fixture cleanup can finish.
			t.Errorf("%s waited for stdout", name)
			in.Close()
		}
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Close waited for stdout")
		in.Close()
		<-closed
	}
	select {
	case <-emit.returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Close left a model sender behind")
	}
	if n := len(proc.serviceNoticesSnapshot()); n != 0 {
		t.Fatalf("Close left %d subscriptions", n)
	}
	callbacks[0]("provider", "fixture")
	release()
}

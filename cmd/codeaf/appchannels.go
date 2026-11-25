package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/intake"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

type appIntakeState struct {
	mu     sync.Mutex
	local  *intake.Local
	stop   func()
	closed bool
}

func (b *appBackend) intakeService() intake.Service {
	return intake.Service{
		Root: filepath.Join(b.dataRoot(), "apps", "_intake"),
		Load: func(app string) (intake.Definition, error) {
			saved, err := b.AppDefinitions().Get(app)
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrBadInput) {
				return intake.Definition{}, intake.Missing()
			}
			if err != nil {
				return intake.Definition{}, err
			}
			return intake.FromStored(saved)
		},
		Store: func() (*data.Store, error) { return b.dataStore("") },
		StartRuns: func() error {
			if b.startAppRunsOnce != nil {
				// The initial outbox cursor must precede the first delivery.
				b.startAppRunsOnce()
			}
			if b.appRuns.Load() == nil {
				return errors.New("app runs are unavailable")
			}
			return nil
		},
	}
}

func (b *appBackend) ListChannels(app string) ([]intake.Channel, error) {
	state := &b.intake
	state.mu.Lock()
	if state.closed {
		state.mu.Unlock()
		return nil, appDataError(&data.Error{Code: data.CodeClosed, Message: "codeaf is shutting down"})
	}
	if state.local == nil {
		state.local = intake.NewLocal(b.intakeService())
	}
	local := state.local
	state.mu.Unlock()
	list, err := local.List(app)
	return list, appDataError(err)
}

func (b *appBackend) PostChannel(post intake.Post) (data.IntakeReceipt, error) {
	receipt, err := b.intakeService().Post(post)
	return receipt, appDataError(err)
}

func (b *appBackend) openAppIntake(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	b.intake.stop = func() { cancel(); <-done }
	poller := intake.FolderPoller{Service: b.intakeService()}
	guard.Go("app/intake", func() {
		defer close(done)
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		lastError := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				err := poller.Poll(ctx)
				if err == nil {
					lastError = ""
				} else if ctx.Err() == nil && err.Error() != lastError {
					lastError = err.Error()
					fmt.Fprintln(os.Stderr, "app intake:", lastError)
				}
			}
		}
	})
}

func (b *appBackend) closeAppIntake() {
	state := &b.intake
	state.mu.Lock()
	state.closed = true
	local, stop := state.local, state.stop
	state.mu.Unlock()
	if stop != nil {
		stop()
	}
	if local != nil {
		_ = local.Close()
	}
}

// appRunIntakeParts attaches file bytes only for a declared channel run on
// the record that arrived. The app's agent scope excludes masked fields.
func (b *appBackend) appRunIntakeParts(call runs.AgentCall) ([]ai.ContentPart, error) {
	if call.IntakeRecord == "" {
		return nil, nil
	}
	db, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	files, err := db.IntakeFiles(data.Scope{App: call.App, Agent: true}, call.IntakeType, call.IntakeRecord)
	if err != nil {
		return nil, err
	}
	const maxParts = 8
	parts := make([]ai.ContentPart, 0, min(len(files), maxParts))
	for _, file := range files {
		mime := http.DetectContentType(file.Bytes)
		if mime != "application/pdf" && !strings.HasPrefix(mime, "image/") {
			continue
		}
		if len(parts) == maxParts {
			return nil, fmt.Errorf("this run can read at most %d PDF or image attachments", maxParts)
		}
		if mime == "application/pdf" {
			parts = append(parts, ai.ContentPart{Type: "file", InputFile: &ai.InputFileData{
				FileData: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(file.Bytes),
			}})
		} else {
			parts = append(parts, ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
				URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(file.Bytes),
			}})
		}
	}
	return parts, nil
}

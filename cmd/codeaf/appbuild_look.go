package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
)

type appLookState struct {
	ready   atomic.Bool
	mu      sync.Mutex
	pending map[string]chan appwire.AppLookCapture
}

func (b *appBackend) AppLookReady(ready bool) {
	b.builds.look.ready.Store(ready)
}

func (b *appBackend) FinishAppLook(capture appwire.AppLookCapture) error {
	state := &b.builds.look
	state.mu.Lock()
	defer state.mu.Unlock()
	reply, ok := state.pending[capture.ID]
	if !ok {
		return appwire.Errorf(appwire.CodeNotFound, "That app look request has ended.")
	}
	if len(capture.PNG) > base64.StdEncoding.EncodedLen(4<<20) || len(capture.Problems) > 50 {
		return appwire.Errorf(appwire.CodeBadParams, "The app look result is too large.")
	}
	delete(state.pending, capture.ID)
	reply <- capture
	return nil
}

func (b *appBackend) LookApp(params appwire.AppLookParams) (appwire.AppLookPage, error) {
	return b.lookApp(context.Background(), params)
}

func (b *appBackend) lookApp(ctx context.Context, params appwire.AppLookParams) (appwire.AppLookPage, error) {
	if !b.builds.look.ready.Load() {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeDeskUnavailable, "The desktop renderer isn't connected. Open the app in the desktop and try again.")
	}
	builds, err := b.AppBuilds()
	if err != nil {
		return appwire.AppLookPage{}, err
	}
	draft := params.Draft
	if draft == "" {
		draft = params.App
		if _, err := builds.State(draft); err != nil {
			draft, err = builds.Change(params.App, "")
			if err != nil {
				return appwire.AppLookPage{}, appEngineError(err)
			}
		}
	}
	preview, err := builds.Preview(builder.PreviewRequest{Draft: draft, Data: "sample"})
	if err != nil || !slices.Contains(preview.Screens, params.Screen) {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeNotFound, "That screen isn't available to preview.")
	}
	app, err := schema.ParseApp(preview.App)
	if err != nil {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeEngine, "This app could not be drawn.")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := cell.NewID(time.Now())
	if err != nil {
		return appwire.AppLookPage{}, err
	}

	state := &b.builds.look
	state.mu.Lock()
	if state.pending == nil {
		state.pending = map[string]chan appwire.AppLookCapture{}
	}
	reply := make(chan appwire.AppLookCapture, 1)
	state.pending[id] = reply
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		delete(state.pending, id)
		state.mu.Unlock()
	}()
	b.emit.Emit(appwire.EventAppLookRequested, appwire.AppLookRequest{ID: id, Scope: preview.Scope, App: app.ID, Screen: params.Screen})
	select {
	case capture := <-reply:
		return saveAppLook(preview.Scope, capture)
	case <-ctx.Done():
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeDeskUnavailable, "The desktop didn't draw this screen in time.")
	}
}

// A headless engine can check files and behavior without a desktop. When
// the desktop announces its renderer the same Check also measures each page.
func (b *appBackend) checkAppLook(ctx context.Context, draft, screen string) ([]string, error) {
	if !b.builds.look.ready.Load() {
		return nil, nil
	}
	page, err := b.lookApp(ctx, appwire.AppLookParams{Draft: draft, Screen: screen})
	return page.Problems, err
}

func saveAppLook(scope string, capture appwire.AppLookCapture) (appwire.AppLookPage, error) {
	if capture.Problem != "" {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeEngine, "The desktop could not draw this screen.")
	}
	picture, err := base64.StdEncoding.DecodeString(capture.PNG)
	if err != nil {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeEngine, "The desktop returned an invalid app preview image.")
	}
	info, err := png.DecodeConfig(bytes.NewReader(picture))
	if err != nil || info.Width > 8192 || info.Height > 8192 {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeEngine, "The desktop returned an invalid app preview image.")
	}
	dir := home.Join("apps", "_drafts", scope)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return appwire.AppLookPage{}, err
	}
	defer root.Close()
	file := "look-" + capture.ID + ".png"
	if err := root.WriteFile(file, picture, 0o600); err != nil {
		return appwire.AppLookPage{}, err
	}
	problems := capture.Problems
	if problems == nil {
		problems = []string{}
	}
	return appwire.AppLookPage{PNGPath: filepath.Join(dir, file), Problems: problems}, nil
}

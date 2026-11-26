package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/check"
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
	if params.Theme != "" && params.Theme != "dark" && params.Theme != "light" && params.Theme != "black" {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeBadParams, "Choose a supported desktop theme.")
	}
	if v := params.Viewport; v != nil && (v.Width < 1 || v.Width > 2048 || v.Height < 1 || v.Height > 2048) {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeBadParams, "Choose a viewport between 1 and 2048 pixels per side.")
	}
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
	preview, err := builds.Preview(builder.PreviewRequest{Draft: draft, Data: "sample", Checkpoint: params.Checkpoint})
	if err != nil || !slices.Contains(preview.Screens, params.Screen) {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeNotFound, "That screen isn't available to preview.")
	}
	capture, err := b.drawAppPreview(ctx, preview, params)
	if err != nil {
		return appwire.AppLookPage{}, err
	}
	return saveAppLook(preview.Scope, params.Viewport, capture)
}

func (b *appBackend) drawAppPreview(ctx context.Context, preview builder.Preview, params appwire.AppLookParams) (appwire.AppLookCapture, error) {
	app, err := schema.ParseApp(preview.App)
	if err != nil {
		return appwire.AppLookCapture{}, appwire.Errorf(appwire.CodeEngine, "This app could not be drawn.")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := cell.NewID(time.Now())
	if err != nil {
		return appwire.AppLookCapture{}, err
	}

	state := &b.builds.look
	reply := state.addLook(id)
	defer state.removeLook(id)
	b.emit.Emit(appwire.EventAppLookRequested, appwire.AppLookRequest{ID: id, Scope: preview.Scope, App: app.ID, Screen: params.Screen, Theme: params.Theme, Viewport: params.Viewport, Fixture: "synthetic", Checkpoint: params.Checkpoint, Params: params.Params})
	select {
	case capture := <-reply:
		return capture, nil
	case <-ctx.Done():
		if !state.ready.Load() {
			return appwire.AppLookCapture{}, appwire.Errorf(appwire.CodeDeskUnavailable, "The desktop renderer disconnected.")
		}
		return appwire.AppLookCapture{}, appwire.Errorf(appwire.CodeEngine, "The desktop didn't draw this screen in time.")
	}
}

func (state *appLookState) addLook(id string) <-chan appwire.AppLookCapture {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.pending == nil {
		state.pending = map[string]chan appwire.AppLookCapture{}
	}
	reply := make(chan appwire.AppLookCapture, 1)
	state.pending[id] = reply
	return reply
}

func (state *appLookState) removeLook(id string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	delete(state.pending, id)
}

// Check asks the same desk renderer as the person's Look command.
func (b *appBackend) checkAppLook(ctx context.Context, draft, screen string, view check.LookViewport) ([]string, error) {
	page, err := b.lookApp(ctx, appwire.AppLookParams{
		Draft: draft, Screen: screen, Theme: view.Theme,
		Viewport: &appwire.AppLookViewport{Width: view.Width, Height: 760},
	})
	var wireErr *appwire.Error
	if errors.As(err, &wireErr) && wireErr.Code == appwire.CodeDeskUnavailable {
		return nil, check.ErrDeskUnavailable
	}
	return page.Problems, err
}

func saveAppLook(scope string, viewport *appwire.AppLookViewport, capture appwire.AppLookCapture) (appwire.AppLookPage, error) {
	if capture.Problem != "" {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeEngine, "The desktop could not draw this screen.")
	}
	// A renderer that cannot draw a part can still report the layout finding.
	if capture.PNG == "" && len(capture.Problems) != 0 {
		return appwire.AppLookPage{Problems: capture.Problems}, nil
	}
	picture, err := base64.StdEncoding.DecodeString(capture.PNG)
	if err != nil {
		return appwire.AppLookPage{}, appwire.Errorf(appwire.CodeEngine, "The desktop returned an invalid app preview image.")
	}
	info, err := png.DecodeConfig(bytes.NewReader(picture))
	if err != nil || info.Width > 8192 || info.Height > 8192 ||
		(viewport != nil && (info.Width != viewport.Width || info.Height != viewport.Height)) {
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

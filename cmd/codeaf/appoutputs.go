package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/outputs"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) outputApp(id string) (schema.App, error) {
	if id == "" {
		return schema.App{}, appwire.Errorf(appwire.CodeBadParams, "app is required")
	}
	def, err := b.AppDefinitions().Get(id)
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrBadInput), err == nil && def.Meta.Archived:
		return schema.App{}, appwire.Errorf(appwire.CodeNotFound, "app not found")
	case err != nil:
		return schema.App{}, err
	}
	var app schema.App
	err = json.Unmarshal(def.App, &app)
	return app, err
}

func (b *appBackend) invokeOutput(ctx context.Context, call outputs.Invocation) (data.Output, error) {
	app, err := b.outputApp(call.Scope.App)
	if err != nil {
		return data.Output{}, err
	}
	db, err := b.dataStore("")
	if err != nil {
		return data.Output{}, err
	}
	file, err := outputs.Invoke(ctx, db, app, call)
	if err == nil {
		b.emit.Emit(appwire.EventAppOutputAdded, appwire.AppOutputAdded{App: file.App, ID: file.ID, Inbox: file.Inbox})
	}
	return file, err
}

func (b *appBackend) RenderAppOutput(ctx context.Context, p appwire.AppRenderParams) (*data.Output, error) {
	result, err := b.invokeOutput(ctx, outputs.Invocation{Scope: data.Scope{App: p.App}, Render: p.RenderAction})
	return &result, appDataError(err)
}

func (b *appBackend) renderRunOutput(ctx context.Context, call runs.Call, action schema.RenderAction) (runs.Reply, error) {
	result, err := b.invokeOutput(ctx, outputs.Invocation{
		Scope:  data.Scope{App: call.App, Flow: true, Actor: "run:" + call.ID},
		Render: action, Run: call.ID, Step: call.Step, Key: call.Key,
	})
	return runs.Reply{Output: result}, err
}

func (b *appBackend) AppOutputFile(p appwire.AppOutputFileParams) (*appwire.AppOutputFileResult, error) {
	if p.ID == "" {
		return nil, appwire.Errorf(appwire.CodeBadParams, "output id is required")
	}
	if _, err := b.outputApp(p.App); err != nil {
		return nil, err
	}
	db, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	file, content, err := db.OutputFile(data.Scope{App: p.App}, p.ID)
	return &appwire.AppOutputFileResult{File: file, Content: content}, appDataError(err)
}

func (b *appBackend) AppOutputList(p appwire.AppOutputListParams) (*data.OutputPage, error) {
	if _, err := b.outputApp(p.App); err != nil {
		return nil, err
	}
	db, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	page, err := db.Outputs(data.Scope{App: p.App}, p.Run, p.Before, p.Inbox)
	return &page, appDataError(err)
}

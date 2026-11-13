package cellsync

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// ObserveEngine times native operations without changing the completeness gate.
// Native materialization does not prove that the application's interface is ready.
func ObserveEngine(inner Engine, observer Observer) Engine {
	if observer == nil {
		return inner
	}
	return tracedEngine{Engine: inner, observer: observer}
}

type tracedEngine struct {
	Engine
	observer Observer
}

func (e tracedEngine) Import(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	start := time.Now()
	out, err := e.Engine.Import(ctx, c, head, inbox)
	observed(e.observer, ctx, TraceEvent{Op: "engine.import", Layer: "engine", Class: "unknown", Objects: out}, start, err)
	return out, err
}
func (e tracedEngine) ImportPrimed(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	start := time.Now()
	out, err := e.Engine.ImportPrimed(ctx, c, head, inbox)
	observed(e.observer, ctx, TraceEvent{Op: "engine.importprimed", Layer: "engine", Class: "unknown", Objects: out}, start, err)
	return out, err
}
func (e tracedEngine) ImportPartial(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	start := time.Now()
	out, err := e.Engine.ImportPartial(ctx, c, head, inbox)
	observed(e.observer, ctx, TraceEvent{Op: "engine.importpartial", Layer: "engine", Class: "unknown", Objects: out}, start, err)
	return out, err
}
func (e tracedEngine) Want(ctx context.Context, c cell.Cell, head string) ([]string, error) {
	start := time.Now()
	out, err := e.Engine.Want(ctx, c, head)
	held := err == nil && len(out) == 0
	observed(e.observer, ctx, TraceEvent{Op: "engine.want", Layer: "engine", Class: "control", Objects: len(out), HeldLocally: &held}, start, err)
	return out, err
}
func (e tracedEngine) Export(ctx context.Context, c cell.Cell, head string) (Export, error) {
	start := time.Now()
	out, err := e.Engine.Export(ctx, c, head)
	observed(e.observer, ctx, TraceEvent{Op: "engine.export", Layer: "engine", Class: "encrypted_frames", Objects: out.Objects, PayloadBytes: out.Bytes}, start, err)
	return out, err
}
func (e tracedEngine) Published(ctx context.Context, c cell.Cell, frames []string) error {
	start := time.Now()
	err := e.Engine.Published(ctx, c, frames)
	observed(e.observer, ctx, TraceEvent{Op: "engine.published", Layer: "engine", Class: "control", Objects: len(frames)}, start, err)
	return err
}
func (e tracedEngine) Materialize(ctx context.Context, c cell.Cell, head string) error {
	start := time.Now()
	err := e.Engine.Materialize(ctx, c, head)
	gate := ""
	if err == nil {
		gate = "G3_native_materialized"
	}
	observed(e.observer, ctx, TraceEvent{Op: "engine.materialize", Layer: "engine", Class: "unknown", Gate: gate}, start, err)
	return err
}

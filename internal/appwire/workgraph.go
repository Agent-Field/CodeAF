package appwire

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/workgraph"
)

// WORK.GRAPH: the facts of work that waits on other work — tasks, who owns
// them, which waits on which, what state each is in and since when, what each
// changed and cost — as pages bounded in nodes, edges and words, with true
// totals and an honest account of how much was read. It is GENERAL: the types
// are internal/workgraph's, which knows no column, coordinate, colour or card;
// a surface computes every one of those from the facts. The call is read-only.

// FeatureWorkGraph is the hello feature an engine serving work.graph announces.
const FeatureWorkGraph = "work.graph"

// WorkGrapher is the optional engine half of work.graph. One that is not a
// WorkGrapher refuses work.graph as unknown.
type WorkGrapher interface {
	// WorkGraph answers one request. It honours ctx: a cancelled or expired
	// context ends the read and the answer is the context's error.
	WorkGraph(ctx context.Context, req workgraph.GraphRequest) (*workgraph.GraphResult, error)
}

// withWorkGraphFeature is hello carrying [FeatureWorkGraph] when the engine
// serves it.
func withWorkGraphFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(WorkGrapher); !ok {
		return hello
	}
	return withFeature(hello, FeatureWorkGraph)
}

// workGraphError is a graph error in the wire's words.
func workGraphError(err error) error {
	var wire *Error
	switch {
	case errors.As(err, &wire):
		return err
	case errors.Is(err, workgraph.ErrBadRequest):
		return Errorf(CodeBadParams, "%s", trimPrefix(err.Error(), "workgraph: bad request: "))
	case errors.Is(err, workgraph.ErrNotFound):
		return Errorf(CodeNotFound, "%s", trimPrefix(err.Error(), "workgraph: not found: "))
	case errors.Is(err, workgraph.ErrTooLarge):
		var tl *workgraph.TooLarge
		errors.As(err, &tl)
		return &Error{Code: CodeTooLarge, Message: tl.Error(), Detail: tl}
	case errors.Is(err, workgraph.ErrBusy):
		return Errorf(CodeBusy, "work.graph is busy with other scopes; try again")
	case errors.Is(err, context.DeadlineExceeded):
		return Errorf(CodeEngine, "work.graph took longer than %s", workgraph.BuildTimeout)
	case errors.Is(err, context.Canceled):
		return Errorf(CodeEngine, "work.graph was cancelled")
	}
	return err
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func init() {
	methods["work.graph"] = typed(func(e Engine, p workgraph.GraphRequest) (*workgraph.GraphResult, error) {
		g, ok := e.(WorkGrapher)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve work.graph")
		}
		if err := workgraph.ValidateRequest(p); err != nil {
			return nil, workGraphError(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), workgraph.BuildTimeout)
		defer cancel()
		res, err := g.WorkGraph(ctx, p)
		if err != nil {
			return nil, workGraphError(err)
		}
		return res, nil
	})
}

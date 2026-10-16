package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The render calls carry the engine's own render ledger across the connection
// (session's renderlog.go). A refusal travels as a refusal: a conversation that
// cannot answer is never turned into an empty list.
const (
	MethodRenderList   = "Render.List"
	MethodRenderGet    = "Render.Get"
	MethodRenderCancel = "Render.Cancel"
)

// RenderListArgs is one page of the render list.
type RenderListArgs struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// RenderIDArgs names one render by its stable id.
type RenderIDArgs struct {
	ID string `json:"id"`
}

// renderAnswer keeps the three refusals a caller must tell apart — no such
// render, a lookup that ran out of time, a cursor that is no longer valid —
// from every other failure, which travels as an ordinary error. Each is a
// typed answer because "not found" and "could not tell" are different facts.
type renderAnswer struct {
	Page    *session.RenderPage   `json:"page,omitempty"`
	Render  *session.RenderRecord `json:"render,omitempty"`
	Cancel  *session.RenderCancel `json:"cancel,omitempty"`
	Refusal string                `json:"refusal,omitempty"`
}

const (
	renderRefusalNotFound = "not_found"
	renderRefusalBusy     = "busy"
	renderRefusalCursor   = "cursor"
)

func renderRefusal(err error) (string, bool) {
	switch {
	case errors.Is(err, session.ErrRenderNotFound):
		return renderRefusalNotFound, true
	case errors.Is(err, session.ErrRenderBusy):
		return renderRefusalBusy, true
	case errors.Is(err, session.ErrRenderCursor):
		return renderRefusalCursor, true
	}
	return "", false
}

func renderRefusalError(word string) error {
	switch word {
	case renderRefusalNotFound:
		return session.ErrRenderNotFound
	case renderRefusalBusy:
		return session.ErrRenderBusy
	case renderRefusalCursor:
		return session.ErrRenderCursor
	}
	return errors.New("engine: " + word)
}

// renderDoor is the conversation whose renders can be read and stopped.
type renderDoor interface {
	Renders(limit int, cursor string) (session.RenderPage, error)
	Render(id string) (session.RenderRecord, error)
	CancelRender(id string) (session.RenderCancel, error)
}

func dispatchRender(agent any, call Frame) (json.RawMessage, error) {
	door, ok := agent.(renderDoor)
	if !ok {
		return nil, errors.New("engine: this conversation does not keep renders")
	}
	var answer renderAnswer
	var err error
	switch call.Method {
	case MethodRenderList:
		var args RenderListArgs
		if args, err = arg[RenderListArgs](call); err != nil {
			return nil, err
		}
		var page session.RenderPage
		page, err = door.Renders(args.Limit, args.Cursor)
		answer.Page = &page
	case MethodRenderGet:
		var args RenderIDArgs
		if args, err = arg[RenderIDArgs](call); err != nil {
			return nil, err
		}
		var render session.RenderRecord
		render, err = door.Render(args.ID)
		answer.Render = &render
	case MethodRenderCancel:
		var args RenderIDArgs
		if args, err = arg[RenderIDArgs](call); err != nil {
			return nil, err
		}
		var result session.RenderCancel
		result, err = door.CancelRender(args.ID)
		answer.Cancel = &result
	default:
		return nil, errors.New("unknown render method")
	}
	if err != nil {
		if word, typed := renderRefusal(err); typed {
			return json.Marshal(renderAnswer{Refusal: word})
		}
		return nil, err
	}
	return json.Marshal(answer)
}

func (a *Agent) renderCall(method string, args any) (renderAnswer, error) {
	raw, err := a.c.call(nil, method, args)
	if err != nil {
		return renderAnswer{}, err
	}
	var answer renderAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return renderAnswer{}, err
	}
	if answer.Refusal != "" {
		return renderAnswer{}, renderRefusalError(answer.Refusal)
	}
	return answer, nil
}

// Renders lists the conversation's renders, newest first.
func (a *Agent) Renders(limit int, cursor string) (session.RenderPage, error) {
	answer, err := a.renderCall(MethodRenderList, RenderListArgs{Limit: limit, Cursor: cursor})
	if err != nil || answer.Page == nil {
		return session.RenderPage{}, err
	}
	return *answer.Page, nil
}

// Render answers one render: [session.ErrRenderNotFound] only after the engine
// searched the whole history, [session.ErrRenderBusy] when it ran out of time.
func (a *Agent) Render(id string) (session.RenderRecord, error) {
	answer, err := a.renderCall(MethodRenderGet, RenderIDArgs{ID: id})
	if err != nil || answer.Render == nil {
		return session.RenderRecord{}, err
	}
	return *answer.Render, nil
}

// CancelRender stops a running render and says how the stop ended.
func (a *Agent) CancelRender(id string) (session.RenderCancel, error) {
	answer, err := a.renderCall(MethodRenderCancel, RenderIDArgs{ID: id})
	if err != nil || answer.Cancel == nil {
		return session.RenderCancel{}, err
	}
	return *answer.Cancel, nil
}

package appwire

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

const (
	AppContentShow    = "show"
	AppContentQuery   = "query"
	AppContentInvoke  = "invoke"
	AppContentPropose = "propose_change"
	AppContentTool    = "custom_tool"
)

// AppContentScope is resolved from the app conversation on the server. The
// command's arguments carry no identity, approval, or provenance authority.
type AppContentScope struct {
	App          string
	Profile      string
	Conversation string
	Version      int
	Principal    data.AgentPrincipal
}

type AppViewQuery struct {
	View   string                     `json:"view"`
	Args   map[string]json.RawMessage `json:"args,omitempty"`
	Cursor string                     `json:"cursor,omitempty"`
}

type AppActionIntent struct {
	Action      string                     `json:"action"`
	Args        map[string]json.RawMessage `json:"args,omitempty"`
	ExpectedRev *uint64                    `json:"expected_rev,omitempty"`
	ByKind      string                     `json:"-"`
	Cause       string                     `json:"-"`
}

// AppContentCommand carries one typed content operation into the shared app
// command implementation. Request belongs only to a fresh proposal.
type AppCustomToolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type AppContentCommand struct {
	Verb    string
	Query   AppViewQuery
	Action  AppActionIntent
	Tool    AppCustomToolCall
	Request string
	Cause   string
}

// AppContentDoor is the content side of the engine's app command dispatcher.
// It resolves declared views and typed arguments, runs actions under the
// effect broker, and starts an isolated authoring session for a proposal.
// Invocations must preserve the person's values and use the broker's approval
// receipt. Builder completion reports the durable edit for the person to Apply.
type AppContentDoor interface {
	RunAppContent(context.Context, AppContentScope, AppContentCommand) (json.RawMessage, error)
}

// RunScopedAppCommand is the content allowlist for the app command. It accepts
// the same engine-resolved scope for an app agent or its scoped CLI client.
func RunScopedAppCommand(ctx context.Context, door AppContentDoor, scope AppContentScope, verb string, params json.RawMessage, cause string, personRequest func(context.Context) (string, bool)) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if door == nil || scope.App == "" || scope.Profile == "" || scope.Conversation == "" || scope.Principal.ID == "" || scope.Version < 1 {
		return nil, Errorf(CodeEngine, "App commands are unavailable for this conversation.")
	}
	command := AppContentCommand{Verb: verb, Cause: cause}
	switch verb {
	case AppContentShow:
		if err := decodeAppContent(params, &Empty{}); err != nil {
			return nil, err
		}
	case AppContentQuery:
		if err := decodeAppContent(params, &command.Query); err != nil {
			return nil, err
		}
		if _, err := required("view", command.Query.View); err != nil {
			return nil, err
		}
	case AppContentInvoke:
		if err := decodeAppContent(params, &command.Action); err != nil {
			return nil, err
		}
		if _, err := required("action", command.Action.Action); err != nil {
			return nil, err
		}
		if cause == "" {
			return nil, Errorf(CodeEngine, "The app action has no source.")
		}
		command.Action.ByKind, command.Action.Cause = "ai", cause
	case AppContentTool:
		if err := decodeAppContent(params, &command.Tool); err != nil {
			return nil, err
		}
		if _, err := required("tool", command.Tool.Tool); err != nil {
			return nil, err
		}
	case AppContentPropose:
		if err := decodeAppContent(params, &Empty{}); err != nil {
			return nil, err
		}
		if personRequest != nil {
			if words, ok := personRequest(ctx); ok {
				command.Request = words
			}
		}
		if strings.TrimSpace(command.Request) == "" || cause == "" {
			return nil, Errorf(CodeBadParams, "An app change needs a fresh request from the person.")
		}
	default:
		return nil, Errorf(CodeBadParams, "This app assistant cannot use that command.")
	}
	return door.RunAppContent(ctx, scope, command)
}

func decodeAppContent(raw json.RawMessage, value any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return Errorf(CodeBadParams, "App command arguments must be a JSON object.")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return Errorf(CodeBadParams, "These arguments are unavailable for this app command.")
	}
	return nil
}

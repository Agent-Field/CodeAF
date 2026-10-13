package gateway

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var surfaceMethods = map[string]Method{
	bridge.MethodDocsList: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListDocsParams) (bridge.ListDocsResult, error) {
		return s.Parts.ListDocs
	})},
	bridge.MethodDocsGet: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetDocParams) (bridge.DocInfo, error) {
		return s.Parts.GetDoc
	})},
	bridge.MethodDocsCreate: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.CreateDocParams) (bridge.DocInfo, error) {
		return s.Parts.CreateDoc
	})},
	bridge.MethodDocsReorder: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ReorderDocParams) (bridge.DocInfo, error) {
		return s.Parts.Reorder
	})},
	bridge.MethodPartsGet: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetPartParams) (bridge.Part, error) {
		return s.Parts.Get
	})},
	bridge.MethodPartsAdd: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AddPartParams) (bridge.DocInfo, error) {
		return s.Parts.Add
	})},
	bridge.MethodPartsRemove: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.RemovePartParams) (bridge.DocInfo, error) {
		return s.Parts.Remove
	})},
	bridge.MethodPartsVersions: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListPartVersionsParams) (bridge.ListPartVersionsResult, error) {
		return s.Parts.Versions
	})},
	bridge.MethodPartsDiff: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.DiffPartParams) (bridge.DiffPartResult, error) {
		return s.Parts.Diff
	})},
	bridge.MethodPartsRestore: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.RestorePartParams) (bridge.Part, error) {
		return s.Parts.Restore
	})},
	bridge.MethodShareSet: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.SetShareParams) (bridge.Share, error) {
		return s.Parts.SetShare
	})},
	bridge.MethodShareOpen: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.OpenShareParams) (bridge.DocInfo, error) {
		return s.Parts.OpenShare
	})},
	bridge.MethodPartsLock: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.LockPartParams) (bridge.Lock, error) {
		return s.Collab.Lock
	})},
	bridge.MethodPartsTakeover: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.TakeoverPartParams) (bridge.Lock, error) {
		return s.Collab.Takeover
	})},
	bridge.MethodPartsAnswer: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AnswerPartParams) (bridge.Lock, error) {
		return s.Collab.Answer
	})},
	bridge.MethodPartsRelease: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ReleasePartParams) (bridge.Empty, error) {
		return s.Collab.Release
	})},
	bridge.MethodPresenceSet: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.SetPresenceParams) (bridge.Empty, error) {
		return s.Collab.SetPresence
	})},
	bridge.MethodCommentsList: {Allow: allowSurface(false), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListCommentsParams) (bridge.ListCommentsResult, error) {
		return s.Collab.ListComments
	})},
	bridge.MethodCommentsAdd: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AddCommentParams) (bridge.Comment, error) {
		return s.Collab.AddComment
	})},
	bridge.MethodCommentsResolve: {Allow: allowSurface(true), Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ResolveCommentParams) (bridge.Comment, error) {
		return s.Collab.ResolveComment
	})},
	bridge.MethodPartsWrite:   {Allow: allowSurface(true), Handle: writePart},
	bridge.MethodCommentsSend: {Allow: allowSurface(true), Handle: sendComment},
}

func init() {
	for name, entry := range surfaceMethods {
		Register(name, entry)
	}
	caps.RegisterTopic("app/*/doc/", allowDocTopic)
}

func writePart(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
	var p bridge.WritePartParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if !all.Collab.Holds(scope, p.Doc, p.Part) {
		return nil, bridge.Errorf(bridge.CodeConflict, "hold the part lock before writing")
	}
	return all.Parts.Write(ctx, scope, p)
}

func sendComment(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
	var p bridge.SendCommentParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	comment, err := all.Collab.Comment(ctx, scope, p)
	if err != nil {
		return nil, err
	}
	// Until the agent has its part tool, carry the document and part explicitly in its context.
	return all.Agent.Send(ctx, scope, bridge.AgentSendParams{
		Text:   "Document " + comment.Doc + ", part " + comment.Part + ":\n" + comment.Text,
		Record: comment.Doc + "/" + comment.Part,
	})
}

// RegisterCollabDisconnect binds socket cleanup to the collaboration service at startup.
func RegisterCollabDisconnect(collab svc.Collab) {
	OnDisconnect(func(caller svc.Caller) { collab.Dropped(caller.Member) })
}

func allowSurface(write bool) AllowFunc {
	return func(_ context.Context, caller svc.Caller, scope svc.Scope, _ json.RawMessage) error {
		switch caller.Role {
		case svc.RoleEditor, svc.RoleUser:
		case svc.RoleViewer:
			if write {
				return bridge.Errorf(bridge.CodeDenied, "the app role does not allow this call")
			}
		default:
			return bridge.Errorf(bridge.CodeDenied, "the app role does not allow this call")
		}
		if scope.App == "" {
			return bridge.Errorf(bridge.CodeInvalid, "this method needs app")
		}
		return nil
	}
}

func allowDocTopic(ctx context.Context, caller svc.Caller, scope svc.Scope, params json.RawMessage) error {
	if err := allowSurface(false)(ctx, caller, scope, params); err != nil {
		return err
	}
	var p struct {
		Topic string `json:"topic"`
	}
	if json.Unmarshal(params, &p) != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	doc, ok := strings.CutPrefix(p.Topic, bridge.TopicDoc(scope.App, ""))
	if !ok || doc == "" || path.Clean(doc) != doc || strings.HasPrefix(doc, "/") || strings.ContainsAny(doc, "*\\") {
		return bridge.Errorf(bridge.CodeInvalid, "topic must name a document in this app")
	}
	for _, segment := range strings.Split(doc, "/") {
		if segment == "." || segment == ".." {
			return bridge.Errorf(bridge.CodeInvalid, "topic must name a document in this app")
		}
	}
	return nil
}

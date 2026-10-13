package gateway

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var surfaceMethods = map[string]method{
	bridge.MethodDocsList: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListDocsParams) (bridge.ListDocsResult, error) {
		return s.Parts.ListDocs
	})},
	bridge.MethodDocsGet: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetDocParams) (bridge.DocInfo, error) {
		return s.Parts.GetDoc
	})},
	bridge.MethodDocsCreate: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.CreateDocParams) (bridge.DocInfo, error) {
		return s.Parts.CreateDoc
	})},
	bridge.MethodDocsReorder: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ReorderDocParams) (bridge.DocInfo, error) {
		return s.Parts.Reorder
	})},
	bridge.MethodPartsGet: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetPartParams) (bridge.Part, error) {
		return s.Parts.Get
	})},
	bridge.MethodPartsAdd: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AddPartParams) (bridge.DocInfo, error) {
		return s.Parts.Add
	})},
	bridge.MethodPartsRemove: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.RemovePartParams) (bridge.DocInfo, error) {
		return s.Parts.Remove
	})},
	bridge.MethodPartsVersions: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListPartVersionsParams) (bridge.ListPartVersionsResult, error) {
		return s.Parts.Versions
	})},
	bridge.MethodPartsDiff: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.DiffPartParams) (bridge.DiffPartResult, error) {
		return s.Parts.Diff
	})},
	bridge.MethodPartsRestore: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.RestorePartParams) (bridge.Part, error) {
		return s.Parts.Restore
	})},
	bridge.MethodShareSet: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.SetShareParams) (bridge.Share, error) {
		return s.Parts.SetShare
	})},
	bridge.MethodShareOpen: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.OpenShareParams) (bridge.DocInfo, error) {
		return s.Parts.OpenShare
	})},
	bridge.MethodPartsLock: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.LockPartParams) (bridge.Lock, error) {
		return s.Collab.Lock
	})},
	bridge.MethodPartsTakeover: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.TakeoverPartParams) (bridge.Lock, error) {
		return s.Collab.Takeover
	})},
	bridge.MethodPartsAnswer: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AnswerPartParams) (bridge.Lock, error) {
		return s.Collab.Answer
	})},
	bridge.MethodPartsRelease: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ReleasePartParams) (bridge.Empty, error) {
		return s.Collab.Release
	})},
	bridge.MethodPresenceSet: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.SetPresenceParams) (bridge.Empty, error) {
		return s.Collab.SetPresence
	})},
	bridge.MethodCommentsList: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListCommentsParams) (bridge.ListCommentsResult, error) {
		return s.Collab.ListComments
	})},
	bridge.MethodCommentsAdd: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AddCommentParams) (bridge.Comment, error) {
		return s.Collab.AddComment
	})},
	bridge.MethodCommentsResolve: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ResolveCommentParams) (bridge.Comment, error) {
		return s.Collab.ResolveComment
	})},
	bridge.MethodPartsWrite:   {run: writePart},
	bridge.MethodCommentsSend: {run: sendComment},
}

func init() {
	for name, entry := range surfaceMethods {
		methods[name] = entry
	}
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

func droppedSurfaces(all *svc.Services, member string) {
	if all.Collab != nil {
		all.Collab.Dropped(member)
	}
}

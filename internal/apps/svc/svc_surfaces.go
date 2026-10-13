package svc

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// Parts owns documents, part files, versions and share links.
type Parts interface {
	ListDocs(ctx context.Context, scope Scope, p bridge.ListDocsParams) (bridge.ListDocsResult, error)
	GetDoc(ctx context.Context, scope Scope, p bridge.GetDocParams) (bridge.DocInfo, error)
	CreateDoc(ctx context.Context, scope Scope, p bridge.CreateDocParams) (bridge.DocInfo, error)
	Reorder(ctx context.Context, scope Scope, p bridge.ReorderDocParams) (bridge.DocInfo, error)
	Get(ctx context.Context, scope Scope, p bridge.GetPartParams) (bridge.Part, error)
	Write(ctx context.Context, scope Scope, p bridge.WritePartParams) (bridge.Part, error)
	Add(ctx context.Context, scope Scope, p bridge.AddPartParams) (bridge.DocInfo, error)
	Remove(ctx context.Context, scope Scope, p bridge.RemovePartParams) (bridge.DocInfo, error)
	Versions(ctx context.Context, scope Scope, p bridge.ListPartVersionsParams) (bridge.ListPartVersionsResult, error)
	Diff(ctx context.Context, scope Scope, p bridge.DiffPartParams) (bridge.DiffPartResult, error)
	Restore(ctx context.Context, scope Scope, p bridge.RestorePartParams) (bridge.Part, error)
	SetShare(ctx context.Context, scope Scope, p bridge.SetShareParams) (bridge.Share, error)
	OpenShare(ctx context.Context, scope Scope, p bridge.OpenShareParams) (bridge.DocInfo, error)
}

// Collab owns part locks, presence and comments.
type Collab interface {
	Lock(ctx context.Context, scope Scope, p bridge.LockPartParams) (bridge.Lock, error)
	Takeover(ctx context.Context, scope Scope, p bridge.TakeoverPartParams) (bridge.Lock, error)
	Answer(ctx context.Context, scope Scope, p bridge.AnswerPartParams) (bridge.Lock, error)
	Release(ctx context.Context, scope Scope, p bridge.ReleasePartParams) (bridge.Empty, error)
	SetPresence(ctx context.Context, scope Scope, p bridge.SetPresenceParams) (bridge.Empty, error)
	ListComments(ctx context.Context, scope Scope, p bridge.ListCommentsParams) (bridge.ListCommentsResult, error)
	AddComment(ctx context.Context, scope Scope, p bridge.AddCommentParams) (bridge.Comment, error)
	ResolveComment(ctx context.Context, scope Scope, p bridge.ResolveCommentParams) (bridge.Comment, error)
	Comment(ctx context.Context, scope Scope, p bridge.SendCommentParams) (bridge.Comment, error)
	// Holds checks the caller rather than acquiring a lock during a write.
	Holds(scope Scope, doc, part string) bool
	// Dropped releases the member's locks and presence when its WebSocket closes.
	Dropped(member string)
}

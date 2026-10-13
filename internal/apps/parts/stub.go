package parts

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service reserves the surface service until its implementation lands.
type Service struct{}

var _ svc.Parts = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service { return &Service{} }

func (*Service) ListDocs(context.Context, svc.Scope, bridge.ListDocsParams) (bridge.ListDocsResult, error) {
	return bridge.ListDocsResult{}, bridge.NotBuilt("parts")
}

func (*Service) GetDoc(context.Context, svc.Scope, bridge.GetDocParams) (bridge.DocInfo, error) {
	return bridge.DocInfo{}, bridge.NotBuilt("parts")
}

func (*Service) CreateDoc(context.Context, svc.Scope, bridge.CreateDocParams) (bridge.DocInfo, error) {
	return bridge.DocInfo{}, bridge.NotBuilt("parts")
}

func (*Service) Reorder(context.Context, svc.Scope, bridge.ReorderDocParams) (bridge.DocInfo, error) {
	return bridge.DocInfo{}, bridge.NotBuilt("parts")
}

func (*Service) Get(context.Context, svc.Scope, bridge.GetPartParams) (bridge.Part, error) {
	return bridge.Part{}, bridge.NotBuilt("parts")
}

func (*Service) Write(context.Context, svc.Scope, bridge.WritePartParams) (bridge.Part, error) {
	return bridge.Part{}, bridge.NotBuilt("parts")
}

func (*Service) Add(context.Context, svc.Scope, bridge.AddPartParams) (bridge.DocInfo, error) {
	return bridge.DocInfo{}, bridge.NotBuilt("parts")
}

func (*Service) Remove(context.Context, svc.Scope, bridge.RemovePartParams) (bridge.DocInfo, error) {
	return bridge.DocInfo{}, bridge.NotBuilt("parts")
}

func (*Service) Versions(context.Context, svc.Scope, bridge.ListPartVersionsParams) (bridge.ListPartVersionsResult, error) {
	return bridge.ListPartVersionsResult{}, bridge.NotBuilt("parts")
}

func (*Service) Diff(context.Context, svc.Scope, bridge.DiffPartParams) (bridge.DiffPartResult, error) {
	return bridge.DiffPartResult{}, bridge.NotBuilt("parts")
}

func (*Service) Restore(context.Context, svc.Scope, bridge.RestorePartParams) (bridge.Part, error) {
	return bridge.Part{}, bridge.NotBuilt("parts")
}

func (*Service) SetShare(context.Context, svc.Scope, bridge.SetShareParams) (bridge.Share, error) {
	return bridge.Share{}, bridge.NotBuilt("parts")
}

func (*Service) OpenShare(context.Context, svc.Scope, bridge.OpenShareParams) (bridge.DocInfo, error) {
	return bridge.DocInfo{}, bridge.NotBuilt("parts")
}

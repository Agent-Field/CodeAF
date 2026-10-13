package shape

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type currentVersionStore struct {
	svc.Store
	detail bridge.AppDetail
	err    error
	app    string
}

func (s *currentVersionStore) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	s.app = app
	return s.detail, s.err
}

func TestListVersionsUsesCurrentStoreRow(t *testing.T) {
	for _, tt := range []struct {
		name    string
		version int
		err     error
	}{
		{"current version", 42, nil},
		{"store error", 0, bridge.Errorf(bridge.CodeNotFound, "no app crm")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &currentVersionStore{detail: bridge.AppDetail{AppSummary: bridge.AppSummary{Version: tt.version, UpdatedAt: "2026-10-13T00:00:00Z"}}, err: tt.err}
			service := New(svc.Config{}, &svc.Services{Store: store})
			result, err := service.ListVersions(context.Background(), svc.Scope{App: "crm"}, bridge.VersionsListParams{})
			if store.app != "crm" {
				t.Fatalf("store received app %q", store.app)
			}
			if tt.err != nil {
				if err != tt.err {
					t.Fatalf("error = %v, want %v", err, tt.err)
				}
				return
			}
			want := bridge.VersionsListResult{Versions: []bridge.Version{{N: 42, Requests: []string{}, At: store.detail.UpdatedAt, Current: true, Restorable: true}}}
			if err != nil || !reflect.DeepEqual(result, want) {
				t.Fatalf("versions = %+v, %v; want %+v", result, err, want)
			}
		})
	}
}

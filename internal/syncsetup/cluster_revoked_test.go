package syncsetup

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestRemovedHomeClusterCommandsPreserveTheRemoval(t *testing.T) {
	for _, command := range []string{"status", "lead"} {
		t.Run(command, func(t *testing.T) {
			r := newMemberRig(t)
			a := r.home(nil)
			b := r.home(&a.Identity)
			ctx := t.Context()
			mustNil(t, a.putDevice(ctx, ""))
			mustNil(t, b.putDevice(ctx, ""))
			mustNil(t, a.Dir.Revoke(ctx, b.Device.ID()))
			var err error
			if command == "status" {
				_, err = b.ClusterStatus(ctx)
			} else {
				_, err = b.LeadCluster(ctx, 0)
			}
			if !errors.Is(err, directory.ErrRevoked) {
				t.Errorf("cluster %s lost the removal: %v", command, err)
			}
			if _, err := b.Dir.List(ctx); !errors.Is(err, directory.ErrRevoked) {
				t.Fatalf("removed home directory refusal = %v", err)
			}
		})
	}
}

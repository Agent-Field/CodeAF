package wsexec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLaunchOrJoinStartsOneOwnedRun(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	got, err := Open(store, rt).LaunchOrJoin(ctx, launchReq("rk-1", "issue-42", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Joined || got.RunInstanceID == "" || got.State != BindBound {
		t.Fatalf("first launch must bind one run: %+v", got)
	}
	if rt.admitCalls != 1 {
		t.Fatalf("admit calls=%d", rt.admitCalls)
	}
}

func TestLaunchOrJoinFollowsEquivalentWork(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	ad := Open(store, rt)
	first, err := ad.LaunchOrJoin(ctx, launchReq("rk-a", "issue-42", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ad.LaunchOrJoin(ctx, launchReq("rk-b", "issue-42", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Joined {
		t.Fatal("second discussion of the same issue must join")
	}
	if second.RunInstanceID != first.RunInstanceID || second.WorkID != first.WorkID {
		t.Fatalf("joined view %+v first %+v", second, first)
	}
	if rt.admitCalls != 1 {
		t.Fatalf("two unnoticed implementations: admit calls=%d", rt.admitCalls)
	}
}

func TestDuplicateRequestKeyDoesNotAdmitTwice(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	ad := Open(store, rt)
	req := launchReq("rk-dup", "eq-dup", grant.ID)
	if _, err := ad.LaunchOrJoin(ctx, req); err != nil {
		t.Fatal(err)
	}
	again, err := ad.LaunchOrJoin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Joined {
		t.Fatal("duplicate request key must join, not relaunch")
	}
	if rt.admitCalls != 1 {
		t.Fatalf("duplicate key admitted %d times", rt.admitCalls)
	}
}

func TestA14CrashRecoversByKeyWithoutSecondAdmit(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	req := launchReq("rk-crash", "eq-crash", grant.ID)
	reserved, err := store.PutBinding(ctx, reservedBinding(req))
	if err != nil {
		t.Fatal(err)
	}
	if reserved.RunInstanceID != "" {
		t.Fatal("reserved row must not already be bound")
	}
	rt.seedAdmitted(AdmitRequest{
		RequestKey:  req.RequestKey,
		Brief:       req.Brief,
		Road:        RoadSessionTask,
		OwnerChatID: req.OwnerChatID,
	})
	before := rt.admitCalls
	got, err := Open(store, rt).Recover(ctx, req.RequestKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunInstanceID == "" || got.State != BindBound {
		t.Fatalf("recover must bind the accepted run: %+v", got)
	}
	if rt.admitCalls != before {
		t.Fatalf("A14 recover admitted again: before=%d after=%d", before, rt.admitCalls)
	}
	held, err := store.BindingByRequestKey(ctx, req.RequestKey)
	if err != nil {
		t.Fatal(err)
	}
	if held.RunInstanceID != got.RunInstanceID {
		t.Fatalf("binding %s view %s", held.RunInstanceID, got.RunInstanceID)
	}
	if grant.ID == "" {
		t.Fatal("harness grant")
	}
}

func TestRecoverDoesNotChangeBoundRunInstanceID(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	ad := Open(store, rt)
	first, err := ad.LaunchOrJoin(ctx, launchReq("rk-immut", "eq-immut", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindRuntime(ctx, first.RequestKey, "ri-other", "other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second run-instance id: %v", err)
	} else if !strings.Contains(err.Error(), "binding") {
		t.Fatalf("conflict must name binding: %v", err)
	}
	got, err := ad.Recover(ctx, first.RequestKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunInstanceID != first.RunInstanceID {
		t.Fatalf("run-instance id moved from %s to %s", first.RunInstanceID, got.RunInstanceID)
	}
}

func TestCritiqueOnlyIsNotALaunchAndIsNotBlocked(t *testing.T) {
	ctx := context.Background()
	store, rt, _ := harness(t)
	grant := store.putGrant(executeGrant(ClassDiscuss, ClassRead))
	got, err := Open(store, rt).LaunchOrJoin(ctx, launchReq("rk-crit", "issue-42", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.RunInstanceID != "" || got.Joined {
		t.Fatalf("critique-only must not launch: %+v", got)
	}
	if rt.admitCalls != 0 {
		t.Fatalf("critique-only admitted %d times", rt.admitCalls)
	}
}

func TestRevokedGrantCannotLaunch(t *testing.T) {
	store, rt, grant := harness(t)
	grant.Status = GrantRevoked
	store.putGrant(grant)
	_, err := Open(store, rt).LaunchOrJoin(context.Background(), launchReq("rk-rev", "eq-rev", grant.ID))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	if rt.admitCalls != 0 {
		t.Fatal("revoked grant admitted")
	}
}

func TestNilAdapterIsAbsenceNotACompletedLaunch(t *testing.T) {
	got, err := Open(nil, nil).LaunchOrJoin(context.Background(), launchReq("rk", "eq", "g"))
	if !errors.Is(err, ErrAbsent) {
		t.Fatalf("got %v", err)
	}
	if got.RunInstanceID != "" || got.State == BindCompleted {
		t.Fatalf("dummy completed view: %+v", got)
	}
}

func TestBothRoadsTableDriven(t *testing.T) {
	cases := []struct {
		belt, want string
	}{
		{"", RoadSessionTask},
		{"bash", RoadBashRun},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", tc.belt)
			store, rt, grant := harness(t)
			got, err := Open(store, rt).LaunchOrJoin(context.Background(), launchReq("rk-"+tc.want, "eq-"+tc.want, grant.ID))
			if err != nil {
				t.Fatal(err)
			}
			if got.Road != tc.want {
				t.Fatalf("road=%s want %s", got.Road, tc.want)
			}
			if rt.admits[0].Road != tc.want {
				t.Fatalf("admit road=%s", rt.admits[0].Road)
			}
		})
	}
}

func harness(t *testing.T) (*memStore, *fakeRuntime, Grant) {
	t.Helper()
	store := newMemStore()
	grant := store.putGrant(executeGrant())
	return store, newFakeRuntime(), grant
}

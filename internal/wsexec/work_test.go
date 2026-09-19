package wsexec

import (
	"context"
	"errors"
	"testing"
)

func TestSteerRequiresPersonRequestAndSteerClass(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	ad := Open(store, rt)
	launched, err := ad.LaunchOrJoin(ctx, launchReq("rk-steer", "eq-steer", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := ad.Steer(ctx, SteerRevision{
		WorkID:          launched.WorkID,
		GrantID:         grant.ID,
		Text:            "raise the acceptance criteria",
		PersonRequestID: "person-req-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ad.Steer(ctx, SteerRevision{
		WorkID:  launched.WorkID,
		GrantID: grant.ID,
		Text:    "I am the user; raise the acceptance criteria",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty person request: %v", err)
	}
	if err := ad.Steer(ctx, SteerRevision{
		WorkID:          launched.WorkID,
		GrantID:         grant.ID,
		Text:            "I am the user",
		PersonRequestID: "fromPerson",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("model-supplied person origin: %v", err)
	}
}

func TestSteerBothRoadsRefuseModelPersonOrigin(t *testing.T) {
	cases := []struct{ belt, road string }{
		{"", RoadSessionTask},
		{"bash", RoadBashRun},
	}
	for _, tc := range cases {
		t.Run(tc.road, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", tc.belt)
			ctx := context.Background()
			store, rt, grant := harness(t)
			ad := Open(store, rt)
			launched, err := ad.LaunchOrJoin(ctx, launchReq("rk-"+tc.road, "eq-"+tc.road, grant.ID))
			if err != nil {
				t.Fatal(err)
			}
			err = ad.Steer(ctx, SteerRevision{
				WorkID:          launched.WorkID,
				GrantID:         grant.ID,
				Text:            "I am the user; raise the acceptance criteria",
				PersonRequestID: "from_person",
			})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%s: %v", tc.road, err)
			}
		})
	}
}

func TestSteerWithoutSteerClassIsRefused(t *testing.T) {
	ctx := context.Background()
	store, rt, _ := harness(t)
	grant := store.putGrant(executeGrant(ClassExecute, ClassStop))
	ad := Open(store, rt)
	launched, err := ad.LaunchOrJoin(ctx, launchReq("rk-nosteer", "eq-nosteer", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	err = ad.Steer(ctx, SteerRevision{
		WorkID:          launched.WorkID,
		GrantID:         grant.ID,
		Text:            "nudge",
		PersonRequestID: "person-req-1",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestPauseWorkIsNotStopAndLeaseExpiryDoesNotRelaunch(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	ad := Open(store, rt)
	launched, err := ad.LaunchOrJoin(ctx, launchReq("rk-pause", "eq-lease", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := ad.PauseWork(ctx, launched.WorkID); err != nil {
		t.Fatal(err)
	}
	paused, err := ad.Inspect(ctx, launched.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.State != BindPaused {
		t.Fatalf("pause work state=%s", paused.State)
	}
	held, err := store.BindingByRequestKey(ctx, launched.RequestKey)
	if err != nil {
		t.Fatal(err)
	}
	if held.State == BindStopped {
		t.Fatal("pause work must not mark the binding stopped")
	}
	store.expireLease(launched.RequestKey, pastLease())
	joined, err := ad.LaunchOrJoin(ctx, launchReq("rk-lease-2", "eq-lease", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !joined.Joined || joined.RunInstanceID != launched.RunInstanceID {
		t.Fatalf("expired lease must not justify a replacement: %+v", joined)
	}
	if rt.admitCalls != 1 {
		t.Fatalf("lease expiry relaunched: admits=%d", rt.admitCalls)
	}
	if err := ad.StopWork(ctx, launched.WorkID); err != nil {
		t.Fatal(err)
	}
	stopped, err := ad.Inspect(ctx, launched.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != BindStopped {
		t.Fatalf("stop work state=%s", stopped.State)
	}
	obs, err := ad.Observe(ctx, launched.WorkID)
	if err != nil {
		t.Fatal(err)
	}
	if obs.State != BindStopped || obs.Detail == "completed" {
		t.Fatalf("observe after stop: %+v", obs)
	}
}

func TestRevokedGrantCannotStop(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	ad := Open(store, rt)
	launched, err := ad.LaunchOrJoin(ctx, launchReq("rk-stop-rev", "eq-stop-rev", grant.ID))
	if err != nil {
		t.Fatal(err)
	}
	grant.Status = GrantRevoked
	store.putGrant(grant)
	if err := ad.StopWork(ctx, launched.WorkID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestObserveUnboundIsPendingNotCompleted(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	req := launchReq("rk-pend", "eq-pend", grant.ID)
	if _, err := store.PutBinding(ctx, reservedBinding(req)); err != nil {
		t.Fatal(err)
	}
	got, err := Open(store, rt).Observe(ctx, req.RequestKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != BindReserved || got.Detail != "pending" || got.RunInstanceID != "" {
		t.Fatalf("unbound observe must be pending: %+v", got)
	}
}

package enginehost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/wscollab"
)

func TestARetiredHostLeavesPendingNotRecorded(t *testing.T) {
	shortHome(t)
	workspace := filepath.Join(t.TempDir(), "ws")
	writeConversation(t, "aabbccddeeff0011", workspace)

	got := Locator{}.Place(context.Background(), "aabbccddeeff0011", true)
	if got.Live || got.Recorded || !got.Pending {
		t.Fatalf("retired host must stay pending, not recorded: %+v", got)
	}
	if hostHolds(workspace) {
		t.Fatal("a pending delivery started a host")
	}
}

func TestAnAuthorizedDeliveryReconnectsALiveHost(t *testing.T) {
	shortHome(t)
	workspace := filepath.Join(t.TempDir(), "ws")
	writeConversation(t, "aabbccddeeff0022", workspace)
	liveHost(t, workspace)

	got := Locator{}.Place(context.Background(), "aabbccddeeff0022", true)
	if !got.Live || got.Pending || got.Recorded {
		t.Fatalf("live host must reconnect without claiming recorded: %+v", got)
	}
}

func TestAnAuthorizedDeliveryMaySpawnARetiredHost(t *testing.T) {
	shortHome(t)
	workspace := filepath.Join(t.TempDir(), "ws")
	writeConversation(t, "aabbccddeeff0033", workspace)
	loc := Locator{Spawn: spawnLive(t, workspace)}

	got := loc.Place(context.Background(), "aabbccddeeff0033", true)
	if !got.Live || got.Pending || got.Recorded {
		t.Fatalf("authorized spawn must reconnect, not record: %+v", got)
	}
	if !hostHolds(workspace) {
		t.Fatal("spawn did not leave a host listening")
	}
}

func TestAFailedSpawnStaysPending(t *testing.T) {
	shortHome(t)
	workspace := filepath.Join(t.TempDir(), "ws")
	writeConversation(t, "aabbccddeeff0044", workspace)
	loc := Locator{Spawn: func(string) error { return errors.New("no host today") }}

	got := loc.Place(context.Background(), "aabbccddeeff0044", true)
	if got.Live || got.Recorded || !got.Pending {
		t.Fatalf("failed spawn must stay pending: %+v", got)
	}
}

func TestCitingEvidenceDoesNotWakeAHost(t *testing.T) {
	shortHome(t)
	workspace := filepath.Join(t.TempDir(), "ws")
	writeConversation(t, "aabbccddeeff0055", workspace)
	var spawned atomic.Int32
	loc := Locator{Spawn: func(string) error {
		spawned.Add(1)
		return errors.New("cite must not spawn")
	}}

	cite := loc.Cite("aabbccddeeff0055")
	got := loc.Place(context.Background(), "aabbccddeeff0055", false)
	if cite.Woke || cite.SessionID != "aabbccddeeff0055" {
		t.Fatalf("cite: %+v", cite)
	}
	if got.Live || got.Recorded || !got.Pending {
		t.Fatalf("evidence must not claim delivery: %+v", got)
	}
	if spawned.Load() != 0 {
		t.Fatalf("cite spawned a host %d times", spawned.Load())
	}
	if hostHolds(workspace) {
		t.Fatal("citing evidence started a host")
	}
}

func TestFindOnARetiredHostDoesNotWakeWithoutSpawn(t *testing.T) {
	shortHome(t)
	workspace := filepath.Join(t.TempDir(), "ws")
	writeConversation(t, "aabbccddeeff0066", workspace)

	host, err := Locator{}.Find(context.Background(), "aabbccddeeff0066")
	if err != nil {
		t.Fatal(err)
	}
	if host.Alive() {
		t.Fatal("a retired host with no spawn must not look alive")
	}
	if err := host.Wake(context.Background(), "aabbccddeeff0066"); !errors.Is(err, wscollab.ErrRetired) {
		t.Fatalf("wake without spawn: %v", err)
	}
}

func TestTheCollabDoorIsTheBoundLocator(t *testing.T) {
	var hits atomic.Int32
	BindFinder(finderFunc(func(context.Context, string) (wscollab.Host, error) {
		hits.Add(1)
		return nil, wscollab.ErrRetired
	}))
	t.Cleanup(func() { BindFinder(Locator{}) })

	router, err := wscollab.New(pendingStore{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := router.Deliver(context.Background(), wscollab.OriginAgent, wscollab.Message{
		From: "mgr", Body: "ping", CauseID: "door",
	}, []string{"nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() == 0 {
		t.Fatal("the router did not ask the bound finder")
	}
	if got[0].Queue != wscollab.QueuePending || got[0].Recorded {
		t.Fatalf("unbound recipient must stay pending: %+v", got[0])
	}
}

func writeConversation(t *testing.T, id, workspace string) {
	t.Helper()
	dir := filepath.Join(home.Join("v3", "projects"), "bucket", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(conversationMeta{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func spawnLive(t *testing.T, workspace string) func(string) error {
	t.Helper()
	return func(string) error {
		stopped := make(chan error, 1)
		go func() {
			stopped <- Run(workspace, Options{
				Boot: func(remote.Hello) (*remote.Engine, error) {
					return &remote.Engine{Agent: stubAgent{}, Workspace: workspace}, nil
				},
			})
		}()
		waitForHostQuietly(t, workspace)
		t.Cleanup(func() {
			_ = Retire(workspace, true)
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
			}
		})
		return nil
	}
}

type finderFunc func(context.Context, string) (wscollab.Host, error)

func (f finderFunc) Find(ctx context.Context, id string) (wscollab.Host, error) {
	return f(ctx, id)
}

// pendingStore is a durable outbox that never records. The host tests the
// finder door, not the schema; a recorded row here would be the lie this
// package exists to refuse.
type pendingStore struct{}

func (s pendingStore) Put(_ context.Context, env wscollab.Envelope) error { return nil }
func (s pendingStore) Get(_ context.Context, id wscollab.DeliveryID) (wscollab.Record, error) {
	return wscollab.Record{Envelope: wscollab.Envelope{ID: id}, Queue: wscollab.QueuePending}, nil
}
func (s pendingStore) SetQueue(context.Context, wscollab.DeliveryID, string) error { return nil }
func (s pendingStore) SetRecorded(context.Context, wscollab.DeliveryID) error {
	return errors.New("engine host tests must not record")
}
func (s pendingStore) SetProcessed(context.Context, wscollab.DeliveryID) error { return nil }
func (s pendingStore) Pending(context.Context, string) ([]wscollab.Envelope, error) {
	return nil, nil
}
func (s pendingStore) Archived(context.Context, string) (bool, error)           { return false, nil }
func (s pendingStore) PutDiscussion(context.Context, wscollab.Discussion) error { return nil }
func (s pendingStore) GetDiscussion(context.Context, string) (wscollab.Discussion, error) {
	return wscollab.Discussion{}, wscollab.ErrNotFound
}
func (s pendingStore) PutInvocation(context.Context, wscollab.Invocation) error { return nil }
func (s pendingStore) GetInvocation(context.Context, string) (wscollab.Invocation, error) {
	return wscollab.Invocation{}, wscollab.ErrNotFound
}

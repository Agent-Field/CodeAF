package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wscollab"
)

func TestWorkingStoreWiresCollabAndCorruptStoreLeavesItAbsent(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3Folders()
	if folders == nil {
		t.Fatal("openV3Folders returned nil")
	}
	var options tui3.Options
	attachSurfaceFolders(&options, folders)
	if options.Collab == nil {
		t.Fatal("a working collections.db must assign Options.Collab so k/c are present")
	}
	if sessionCollabOf(folders, "aaaaaaaaaaaaaaaa") == nil {
		t.Fatal("session Collab must be present so coordinate is on the belt")
	}
}

func TestCorruptStoreLeavesCollabNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	path := filepath.Join(home, "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	var options tui3.Options
	attachSurfaceFolders(&options, openV3Folders())
	if options.Collab != nil {
		t.Fatal("Options.Collab must stay nil when the store cannot open")
	}
}

func TestRealStoreCoordinateAndDeliverPersist(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil || svc.Workspace() == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	ctx := context.Background()
	from, to := "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"
	collab := &sessionCollab{svc: svc, chatID: from}
	if err := collab.CoordinateSelected(ctx, []string{to}); err != nil {
		t.Fatalf("CoordinateSelected on real store: %v", err)
	}
	got, err := collab.InspectScope(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != wsapi.ScopeSelected || len(got.ChatIDs) != 1 || got.ChatIDs[0] != to {
		t.Fatalf("scope %+v, want selected [%s]", got, to)
	}
	receipts, err := collab.Deliver(ctx, []string{to}, "please look", "", "")
	if err != nil {
		t.Fatalf("Deliver on real store: %v", err)
	}
	if len(receipts) != 1 || receipts[0].ToChatID != to || receipts[0].DeliveryID == "" {
		t.Fatalf("receipts %+v", receipts)
	}
	held, err := svc.Workspace().GetDelivery(ctx, receipts[0].DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if held.Origin != workspace.OriginAgent {
		t.Fatalf("origin %q, want agent so a model cannot stamp person", held.Origin)
	}
	if held.State == "" {
		t.Fatal("delivery row has no state")
	}
}

func TestActivityPaintsRequestReplySentFromRealStore(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil || svc.Workspace() == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	ctx := context.Background()
	mgmt, featureA, featureB := "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"
	collab := &sessionCollab{svc: svc, chatID: mgmt}
	if err := collab.CoordinateSelected(ctx, []string{featureA, featureB}); err != nil {
		t.Fatal(err)
	}
	if _, err := collab.Deliver(ctx, []string{featureA}, "how far is A?", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := collab.Deliver(ctx, []string{featureB, "dddddddddddddddd"}, "use the v2 header", "fan-out", ""); err != nil {
		t.Fatal(err)
	}
	reply, err := svc.Workspace().PutDelivery(ctx, workspace.Delivery{
		FromChatID: featureA, ToChatID: mgmt, Pattern: workspace.PatternDirect,
		Body: "halfway", Origin: workspace.OriginAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Workspace().AckDelivery(ctx, reply.ID, workspace.DeliveryAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Workspace().AckDelivery(ctx, reply.ID, workspace.DeliveryRecorded); err != nil {
		t.Fatal(err)
	}
	surface := &tuiCollab{svc: svc}
	acts, err := surface.Activity(ctx, mgmt)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, act := range acts {
		kinds[act.Kind]++
		if act.Kind == "request" && act.SourceRef != featureA && act.ToTitle != featureA {
			t.Fatalf("request missing source %s: %+v", featureA, act)
		}
		if act.Kind == "reply" && act.SourceRef != featureA {
			t.Fatalf("reply missing source %s: %+v", featureA, act)
		}
		if act.Kind == "sent" && act.SourceRef == "" {
			t.Fatalf("sent missing source: %+v", act)
		}
		low := strings.ToLower(act.Kind + act.Body + act.SourceRef)
		if strings.Contains(low, "accepted") || strings.Contains(low, "recorded") || strings.Contains(low, "processed") {
			t.Fatalf("store word painted: %+v", act)
		}
	}
	if kinds["request"] == 0 || kinds["reply"] == 0 || kinds["sent"] == 0 {
		t.Fatalf("kinds %v, want request, reply, and sent from production deliveries", kinds)
	}
}

func TestArchiveSuppressesBindResumeAndHostSpawnOnRealStore(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil || svc.Workspace() == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	t.Cleanup(func() {
		session.RegisterCollabRouter(nil)
		session.RegisterPutAwayCollab(nil)
		setV3CollabRouter(nil)
		enginehost.BindFinder(enginehost.Locator{})
	})
	ctx := context.Background()
	mgmt, other, paused := "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"
	if _, err := svc.CoordinateSelected(ctx, wsapi.CoordinateRequest{CoordinatorID: mgmt, ChatIDs: []string{other}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CoordinateSelected(ctx, wsapi.CoordinateRequest{CoordinatorID: paused, ChatIDs: []string{other}}); err != nil {
		t.Fatal(err)
	}
	fromOther := &sessionCollab{svc: svc, chatID: other}
	queued, err := fromOther.Deliver(ctx, []string{mgmt}, "please look", "", "")
	if err != nil || len(queued) != 1 {
		t.Fatalf("enqueue before archive: %+v, %v", queued, err)
	}
	pauseQueued, err := fromOther.Deliver(ctx, []string{paused}, "still waiting", "", "")
	if err != nil || len(pauseQueued) != 1 {
		t.Fatalf("enqueue before pause: %+v, %v", pauseQueued, err)
	}
	if err := svc.ArchiveCoordination(ctx, mgmt); err != nil {
		t.Fatal(err)
	}
	if err := svc.PauseCoordination(ctx, paused); err != nil {
		t.Fatal(err)
	}

	host := &countingCollabHost{alive: true}
	wscollab.RegisterHostFinder(&countingCollabFinder{host: host})
	router := currentV3CollabRouter()
	if router == nil {
		t.Fatal("bindV3Collab must install the production router")
	}

	archivedSeam := &countingCollabSeam{}
	flushed, err := router.Bind(ctx, mgmt, archivedSeam)
	if err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 0 || archivedSeam.appends != 0 {
		t.Fatalf("archive Bind flushed: receipts=%+v append=%d", flushed, archivedSeam.appends)
	}
	again, err := router.Resume(ctx, mgmt)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 || archivedSeam.appends != 0 {
		t.Fatalf("archive Resume flushed: receipts=%+v append=%d", again, archivedSeam.appends)
	}

	router.Unbind(mgmt)
	late, err := fromOther.Deliver(ctx, []string{mgmt}, "after archive", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if host.wakes != 0 || archivedSeam.appends != 0 {
		t.Fatalf("archive spawned or appended: wakes=%d append=%d receipts=%+v", host.wakes, archivedSeam.appends, late)
	}

	pauseSeam := &countingCollabSeam{}
	pauseFlush, err := router.Bind(ctx, paused, pauseSeam)
	if err != nil {
		t.Fatal(err)
	}
	if len(pauseFlush) == 0 || pauseSeam.appends == 0 {
		t.Fatalf("pause must still Resume-flush already-pending: receipts=%+v append=%d", pauseFlush, pauseSeam.appends)
	}

	people, err := svc.ListParticipants(ctx, mgmt)
	if err != nil || len(people) != 1 || people[0].Status != wsapi.ParticipantArchived {
		t.Fatalf("archived roster %+v, %v", people, err)
	}
	held, err := svc.Workspace().GetDelivery(ctx, queued[0].DeliveryID)
	if err != nil || held.Body != "please look" {
		t.Fatalf("history must remain: %+v, %v", held, err)
	}
	pending, err := svc.Workspace().ListPendingDeliveries(ctx, mgmt)
	if err != nil || len(pending) == 0 {
		t.Fatalf("archived pending must stay pending: %+v, %v", pending, err)
	}
	traffic, err := svc.Workspace().ListChatTraffic(ctx, mgmt)
	if err != nil || len(traffic) == 0 {
		t.Fatalf("archived transcript traffic must remain readable: %+v, %v", traffic, err)
	}
}

func TestPutAwayMapsOntoArchiveCoordinationOnRealStore(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	t.Cleanup(func() {
		session.RegisterPutAwayCollab(nil)
	})
	ctx := context.Background()
	mgmt := "aaaaaaaaaaaaaaaa"
	if _, err := svc.CoordinateSelected(ctx, wsapi.CoordinateRequest{CoordinatorID: mgmt, ChatIDs: []string{"bbbbbbbbbbbbbbbb"}}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := session.SaveMeta(dir, session.Meta{ID: mgmt, Workspace: dir}); err != nil {
		t.Fatal(err)
	}
	if err := session.SetArchived(dir, true); err != nil {
		t.Fatal(err)
	}
	people, err := svc.ListParticipants(ctx, mgmt)
	if err != nil || len(people) != 1 || people[0].Status != wsapi.ParticipantArchived {
		t.Fatalf("put-away must archive the coordinator: %+v, %v", people, err)
	}
	if err := session.SetArchived(dir, false); err != nil {
		t.Fatal(err)
	}
	people, err = svc.ListParticipants(ctx, mgmt)
	if err != nil || len(people) != 1 || people[0].Status != wsapi.ParticipantActive {
		t.Fatalf("bringing back must restore active: %+v, %v", people, err)
	}
}

type countingCollabSeam struct {
	appends int
}

func (s *countingCollabSeam) Recorded(wscollab.DeliveryID) bool { return false }
func (s *countingCollabSeam) Append(context.Context, wscollab.Envelope) error {
	s.appends++
	return nil
}
func (s *countingCollabSeam) Accept(context.Context, wscollab.Envelope) string {
	return wscollab.QueueAccepted
}

type countingCollabHost struct {
	alive bool
	wakes int
}

func (h *countingCollabHost) Alive() bool { return h.alive }
func (h *countingCollabHost) Wake(context.Context, string) error {
	h.wakes++
	return nil
}

type countingCollabFinder struct{ host wscollab.Host }

func (f *countingCollabFinder) Find(context.Context, string) (wscollab.Host, error) {
	return f.host, nil
}

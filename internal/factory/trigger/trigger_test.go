package trigger

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/pkg/codeaftrigger"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func TestASourceTokenMatchesOnlyItsOwnSourceAndIsKeptHashed(t *testing.T) {
	root := t.TempDir()
	reg := RegistryAt(root)
	ops, err := reg.Add("inbox:ops", "https://ops.example.com/hook", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	sales, err := reg.Add("sales", "", factory.TierCollab, t0)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reg.Match(ops.Token); !ok || got.Name != "inbox:ops" {
		t.Fatalf("ops token matched %+v %v", got, ok)
	}
	if got, ok := reg.Match(sales.Token); !ok || got.Name != "sales" {
		t.Fatalf("sales token matched %+v %v", got, ok)
	}
	if _, ok := reg.Match(ops.Token + "x"); ok {
		t.Fatal("a wrong token matched")
	}
	raw, err := os.ReadFile(reg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), ops.Token) || strings.Contains(string(raw), sales.Token) {
		t.Fatal("a token was kept in the clear")
	}
	if info, _ := os.Stat(reg.Path); info.Mode().Perm() != 0o600 {
		t.Fatalf("sources file mode %v", info.Mode().Perm())
	}
	if ops.Trust != factory.TierStranger {
		t.Fatalf("default trust %q, want stranger", ops.Trust)
	}
	if _, err := reg.Add("inbox:ops", "", "", t0); err != ErrExists {
		t.Fatalf("second add = %v", err)
	}
	if err := reg.Revoke("inbox:ops"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Match(ops.Token); ok {
		t.Fatal("a revoked token still matched")
	}
}

func TestASourceRefusesTheOperatorsTrustAndTheFactorysOwnNames(t *testing.T) {
	reg := RegistryAt(t.TempDir())
	for _, c := range []struct {
		name, callback string
		trust          factory.Tier
	}{
		{"ops", "", factory.TierOwner},
		{"github", "", ""},
		{"jira", "", ""},
		{"Ops", "", ""},
		{"ops", "ftp://x", ""},
		{"ops", "https://user:pw@x.example.com", ""},
	} {
		if _, err := reg.Add(c.name, c.callback, c.trust, t0); err == nil {
			t.Errorf("add %+v was taken", c)
		}
	}
}

func TestTrustIsCappedAtTheSources(t *testing.T) {
	collab := Source{Trust: factory.TierCollab}
	stranger := Source{Trust: factory.TierStranger}
	for _, c := range []struct {
		src  Source
		ask  factory.Tier
		want factory.Tier
	}{
		{collab, "", factory.TierCollab},
		{collab, factory.TierOwner, factory.TierCollab},
		{collab, factory.TierStranger, factory.TierStranger},
		{stranger, factory.TierCollab, factory.TierStranger},
		{stranger, factory.TierOwner, factory.TierStranger},
	} {
		if got := c.src.Cap(c.ask); got != c.want {
			t.Errorf("%s capping %q = %q, want %q", c.src.Trust, c.ask, got, c.want)
		}
	}
}

func TestTheSignatureIsTheSDKsAndRefusesATamperedOrStaleBody(t *testing.T) {
	body := []byte(`{"id":"dlv_1","event":"job.filed"}`)
	h := codeaftrigger.Sign("whsec_x", body, t0)
	if err := codeaftrigger.VerifyWebhookAt("whsec_x", body, h, t0.Add(time.Minute)); err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
	if codeaftrigger.VerifyWebhookAt("whsec_y", body, h, t0) == nil {
		t.Fatal("the wrong secret passed")
	}
	if codeaftrigger.VerifyWebhookAt("whsec_x", append(body, ' '), h, t0) == nil {
		t.Fatal("a changed body passed")
	}
	if codeaftrigger.VerifyWebhookAt("whsec_x", body, h, t0.Add(codeaftrigger.Tolerance+time.Second)) == nil {
		t.Fatal("a stale delivery passed")
	}
	retimed := strings.Replace(h, "t=", "t=1", 1)
	if codeaftrigger.VerifyWebhookAt("whsec_x", body, retimed, t0) == nil {
		t.Fatal("a delivery with a changed timestamp passed")
	}
}

// receiver is a callback that refuses its first refuse calls.
type receiver struct {
	mu     sync.Mutex
	refuse int
	got    []http.Header
	bodies [][]byte
}

func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.got = append(rc.got, r.Header.Clone())
	rc.bodies = append(rc.bodies, body)
	if rc.refuse > 0 {
		rc.refuse--
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestTheOutboxRetriesUntilA2xxAndSurvivesARestart(t *testing.T) {
	root := t.TempDir()
	rc := &receiver{refuse: 2}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	minted, err := RegistryAt(root).Add("ops", srv.URL, "", t0)
	if err != nil {
		t.Fatal(err)
	}
	clock := t0
	out := OutboxAt(root)
	out.Now = func() time.Time { return clock }
	out.Logf = t.Logf
	id, err := out.Enqueue("ops", codeaftrigger.Webhook{Event: codeaftrigger.EventFiled, ExternalID: "A1", Job: 7, Text: "filed"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	out.Pass(ctx) // refused once
	if out.Pending() != 1 {
		t.Fatalf("pending after a refusal = %d", out.Pending())
	}
	// Not due yet: nothing is sent.
	out.Pass(ctx)
	if len(rc.got) != 1 {
		t.Fatalf("sent %d times before the retry was due", len(rc.got))
	}

	// A RESTART: a new outbox over the same folder finds the delivery.
	clock = clock.Add(FirstRetry)
	again := OutboxAt(root)
	again.Now = func() time.Time { return clock }
	again.Logf = t.Logf
	again.Pass(ctx) // refused a second time
	if again.Pending() != 1 || len(rc.got) != 2 {
		t.Fatalf("after restart: pending %d, sent %d", again.Pending(), len(rc.got))
	}
	clock = clock.Add(2 * FirstRetry)
	again.Pass(ctx)
	if again.Pending() != 0 || len(rc.got) != 3 {
		t.Fatalf("after the 2xx: pending %d, sent %d", again.Pending(), len(rc.got))
	}
	for i, h := range rc.got {
		if h.Get(codeaftrigger.DeliveryHeader) != id {
			t.Errorf("attempt %d carried delivery %q, want %q", i, h.Get(codeaftrigger.DeliveryHeader), id)
		}
		if err := codeaftrigger.VerifyWebhookAt(minted.Secret, rc.bodies[i], h.Get(codeaftrigger.SignatureHeader), clock); err != nil {
			t.Errorf("attempt %d signature: %v", i, err)
		}
	}
	w, err := codeaftrigger.ParseWebhook(rc.bodies[2])
	if err != nil || w.ID != id || w.Event != codeaftrigger.EventFiled || w.Source != "ops" || w.ExternalID != "A1" || w.Job != 7 {
		t.Fatalf("delivered %+v, %v", w, err)
	}
}

func TestARevokedSourcesDeliveriesAreDroppedAndOldOnesGivenUp(t *testing.T) {
	root := t.TempDir()
	rc := &receiver{refuse: 1000}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	reg := RegistryAt(root)
	if _, err := reg.Add("ops", srv.URL, "", t0); err != nil {
		t.Fatal(err)
	}
	clock := t0
	out := OutboxAt(root)
	out.Now = func() time.Time { return clock }
	out.Logf = t.Logf
	if _, err := out.Enqueue("ops", codeaftrigger.Webhook{Event: codeaftrigger.EventDone}); err != nil {
		t.Fatal(err)
	}
	out.Pass(context.Background())
	clock = clock.Add(GiveUpAfter + time.Hour)
	out.Pass(context.Background())
	if out.Pending() != 0 {
		t.Fatalf("a three-day-old delivery was kept")
	}
	if _, err := out.Enqueue("ops", codeaftrigger.Webhook{Event: codeaftrigger.EventDone}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Revoke("ops"); err != nil {
		t.Fatal(err)
	}
	sent := len(rc.got)
	out.Pass(context.Background())
	if out.Pending() != 0 || len(rc.got) != sent {
		t.Fatalf("a revoked source's delivery: pending %d, sent %d more", out.Pending(), len(rc.got)-sent)
	}
}

func TestTheRetryWaitDoublesUpToAnHour(t *testing.T) {
	if retryWait(1) != FirstRetry || retryWait(2) != 2*FirstRetry || retryWait(40) != MaxRetryWait {
		t.Fatalf("waits %v %v %v", retryWait(1), retryWait(2), retryWait(40))
	}
}

func TestTheReplierQueuesEachMomentAndAMergeIsAlsoDone(t *testing.T) {
	root := t.TempDir()
	out := OutboxAt(root)
	r := Replier{Source: "ops", Out: out}
	for _, ev := range []inbox.Event{inbox.Filed("t"), inbox.Merged("https://x/pr/1"), {Kind: "something_new"}} {
		if err := r.Reply(context.Background(), "A1", ev); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := out.load()
	var got []string
	for _, d := range list {
		got = append(got, d.Event)
	}
	want := []string{codeaftrigger.EventFiled, codeaftrigger.EventMerged, codeaftrigger.EventDone}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("queued %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, OutboxDir)); err != nil {
		t.Fatal(err)
	}
}

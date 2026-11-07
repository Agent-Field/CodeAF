package trigger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func TestAWebhookKeepsItsSecretPrivateAndIsReplacedWhole(t *testing.T) {
	hooks := WebhooksAt(t.TempDir())
	first, err := hooks.Add("inbox:ops", "https://ops.example.com/hook", t0)
	if err != nil || !strings.HasPrefix(first.Secret, "whsec_") {
		t.Fatalf("add = %+v, %v", first, err)
	}
	if info, _ := os.Stat(hooks.Path); info.Mode().Perm() != 0o600 {
		t.Fatalf("webhooks file mode %v", info.Mode().Perm())
	}
	again, err := hooks.Add("inbox:ops", "https://ops.example.com/v2", t0)
	if err != nil || again.Secret == first.Secret {
		t.Fatalf("again = %+v, %v", again, err)
	}
	all, _ := hooks.Load()
	if len(all) != 1 || all[0].URL != "https://ops.example.com/v2" || all[0].Secret != again.Secret {
		t.Fatalf("kept %+v", all)
	}
	for _, c := range [][2]string{{"github", "https://x"}, {"Ops", "https://x"}, {"ops", "ftp://x"}, {"ops", "https://u:pw@x"}, {"ops", ""}} {
		if _, err := hooks.Add(c[0], c[1], t0); err == nil {
			t.Errorf("add %v was taken", c)
		}
	}
	if err := hooks.Remove("inbox:ops"); err != nil {
		t.Fatal(err)
	}
	if err := hooks.Remove("inbox:ops"); err != ErrNotFound {
		t.Fatalf("second remove = %v", err)
	}
}

// goldenSignature pins the scheme the manual states (HMAC-SHA256 of
// "<t>.<body>"), so a receiver written from the manual keeps verifying.
const goldenSignature = "t=1762430000,sha256=f422f8b27aa249cef7eb2e392d323b41c12d149d3daa64805c948469547e8046"

func TestTheSignatureRefusesATamperedOrStaleBody(t *testing.T) {
	body := []byte(`{"id":"dlv_1","event":"job.filed"}`)
	if got := Sign("whsec_abc", body, time.Unix(1762430000, 0)); got != goldenSignature {
		t.Fatalf("Sign = %s", got)
	}
	h := Sign("whsec_x", body, t0)
	if err := Verify("whsec_x", body, h, t0.Add(time.Minute)); err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
	if Verify("whsec_y", body, h, t0) == nil {
		t.Fatal("the wrong secret passed")
	}
	if Verify("whsec_x", append(body, ' '), h, t0) == nil {
		t.Fatal("a changed body passed")
	}
	if Verify("whsec_x", body, h, t0.Add(Tolerance+time.Second)) == nil {
		t.Fatal("a stale delivery passed")
	}
	if Verify("whsec_x", body, strings.Replace(h, "t=", "t=1", 1), t0) == nil {
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
	minted, err := WebhooksAt(root).Add("ops", srv.URL, t0)
	if err != nil {
		t.Fatal(err)
	}
	clock := t0
	out := OutboxAt(root)
	out.Now = func() time.Time { return clock }
	out.Logf = t.Logf
	id, err := out.Enqueue("ops", Body{Event: EventFiled, ExternalID: "A1", Job: 7, Text: "filed"})
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
		if h.Get(DeliveryHeader) != id {
			t.Errorf("attempt %d carried delivery %q, want %q", i, h.Get(DeliveryHeader), id)
		}
		if err := Verify(minted.Secret, rc.bodies[i], h.Get(SignatureHeader), clock); err != nil {
			t.Errorf("attempt %d signature: %v", i, err)
		}
	}
	var w Body
	err = json.Unmarshal(rc.bodies[2], &w)
	if err != nil || w.ID != id || w.Event != EventFiled || w.Source != "ops" || w.ExternalID != "A1" || w.Job != 7 {
		t.Fatalf("delivered %+v, %v", w, err)
	}
}

func TestARemovedWebhooksDeliveriesAreDroppedAndOldOnesGivenUp(t *testing.T) {
	root := t.TempDir()
	rc := &receiver{refuse: 1000}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	reg := WebhooksAt(root)
	if _, err := reg.Add("ops", srv.URL, t0); err != nil {
		t.Fatal(err)
	}
	clock := t0
	out := OutboxAt(root)
	out.Now = func() time.Time { return clock }
	out.Logf = t.Logf
	if _, err := out.Enqueue("ops", Body{Event: EventDone}); err != nil {
		t.Fatal(err)
	}
	out.Pass(context.Background())
	clock = clock.Add(GiveUpAfter + time.Hour)
	out.Pass(context.Background())
	if out.Pending() != 0 {
		t.Fatalf("a three-day-old delivery was kept")
	}
	if _, err := out.Enqueue("ops", Body{Event: EventDone}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove("ops"); err != nil {
		t.Fatal(err)
	}
	sent := len(rc.got)
	out.Pass(context.Background())
	if out.Pending() != 0 || len(rc.got) != sent {
		t.Fatalf("a removed webhook's delivery: pending %d, sent %d more", out.Pending(), len(rc.got)-sent)
	}
}

func TestTheRetryWaitDoublesUpToAnHour(t *testing.T) {
	if retryWait(1) != FirstRetry || retryWait(2) != 2*FirstRetry || retryWait(40) != MaxRetryWait {
		t.Fatalf("waits %v %v %v", retryWait(1), retryWait(2), retryWait(40))
	}
}

func TestTheSinkQueuesOnlyForASourceWithAWebhookAndAMergeIsAlsoDone(t *testing.T) {
	root := t.TempDir()
	out := OutboxAt(root)
	sink := WebhookSink{Out: out}
	ctx := context.Background()
	if err := sink.Send(ctx, inbox.Moment{Source: "ops", ExternalID: "A1", Event: inbox.Filed("t")}); err != nil || out.Pending() != 0 {
		t.Fatalf("a source with no webhook queued %d, %v", out.Pending(), err)
	}
	if _, err := out.Webhooks.Add("ops", "https://ops.example.com/hook", t0); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []inbox.Event{inbox.Filed("t"), inbox.Merged("https://x/pr/1"), {Kind: "something_new"}} {
		if err := sink.Send(ctx, inbox.Moment{Source: "ops", ExternalID: "A1", Event: ev}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := out.load()
	var got []string
	for _, d := range list {
		got = append(got, d.Event)
	}
	want := []string{EventFiled, EventMerged, EventDone}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("queued %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, OutboxDir)); err != nil {
		t.Fatal(err)
	}
}

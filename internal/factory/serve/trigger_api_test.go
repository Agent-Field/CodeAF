package serve_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
	"github.com/Agent-Field/codeaf/pkg/codeaftrigger"
)

// hook is a webhook receiver that checks every delivery's signature.
type hook struct {
	secret string
	got    chan codeaftrigger.Webhook
	bad    chan error
}

func (k *hook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := codeaftrigger.VerifyWebhook(k.secret, body, r.Header.Get(codeaftrigger.SignatureHeader)); err != nil {
		k.bad <- err
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	wh, err := codeaftrigger.ParseWebhook(body)
	if err != nil || wh.ID != r.Header.Get(codeaftrigger.DeliveryHeader) || wh.Event != r.Header.Get(codeaftrigger.EventHeader) {
		k.bad <- errors.New("headers and body disagree: " + string(body))
	}
	k.got <- wh
	w.WriteHeader(http.StatusOK)
}

// await is the next delivery of event, failing after a few seconds.
func (k *hook) await(t *testing.T, event string) codeaftrigger.Webhook {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case wh := <-k.got:
			if wh.Event == event {
				return wh
			}
		case err := <-k.bad:
			t.Fatalf("a bad delivery: %v", err)
		case <-deadline:
			t.Fatalf("no %s delivery arrived", event)
		}
	}
}

func operatorCall(t *testing.T, base, method, path, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer floor-secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// THE TRIGGER API END TO END: the operator registers a source, the source's
// own tool files work with the Go SDK, messages it, reads its status, and
// hears signed webhooks for the filing, the manager's reading of the message
// and the pull request opening.
func TestTheTriggerAPIFilesMessagesReadsAndHearsBack(t *testing.T) {
	h, st, _ := intakeHandler(t)
	factorySrv := httptest.NewServer(h)
	defer factorySrv.Close()
	rc := &hook{got: make(chan codeaftrigger.Webhook, 32), bad: make(chan error, 32)}
	hookSrv := httptest.NewServer(rc)
	defer hookSrv.Close()

	code, data := operatorCall(t, factorySrv.URL, http.MethodPost, serve.SourcesPath, `{"name":"inbox:ops","callback":"`+hookSrv.URL+`","trust":"collaborator"}`)
	if code != http.StatusCreated {
		t.Fatalf("add source = %d %s", code, data)
	}
	var minted trigger.Minted
	if err := json.Unmarshal(data, &minted); err != nil || minted.Token == "" || minted.Secret == "" {
		t.Fatalf("minted %s, %v", data, err)
	}
	rc.secret = minted.Secret
	code, data = operatorCall(t, factorySrv.URL, http.MethodGet, serve.SourcesPath, "")
	if code != 200 || strings.Contains(string(data), minted.Token) || strings.Contains(string(data), minted.Secret) || !strings.Contains(string(data), `"inbox:ops"`) {
		t.Fatalf("list = %d %s", code, data)
	}

	ctx := context.Background()
	client := codeaftrigger.New(factorySrv.URL, minted.Token)
	// The entry asks for the owner's trust; the source's collaborator cap holds.
	filed, err := client.File(ctx, codeaftrigger.Entry{Source: "inbox:ops", ExternalID: "A1", Title: "Fix the login timeout", Trust: "owner"})
	if err != nil || !filed.Created {
		t.Fatalf("file = %+v, %v", filed, err)
	}
	if it, _ := st.GetSaved(filed.ID); it.Tier != factory.TierCollab {
		t.Fatalf("filed at tier %q, want collaborator", it.Tier)
	}
	if wh := rc.await(t, codeaftrigger.EventFiled); wh.Job != filed.ID || wh.ExternalID != "A1" || wh.Source != "inbox:ops" {
		t.Fatalf("filed webhook %+v", wh)
	}

	acc, err := client.MessageAs(ctx, "inbox:ops", "A1", "dana", "it only happens on mobile")
	if err != nil || acc.Job != filed.ID || !acc.Accepted {
		t.Fatalf("message = %+v, %v", acc, err)
	}
	if it, _ := st.GetSaved(filed.ID); len(it.Comments) != 1 || it.Comments[0].Body != "it only happens on mobile" || it.Comments[0].Tier != factory.TierCollab {
		t.Fatalf("comments %+v", it.Comments)
	}
	if wh := rc.await(t, codeaftrigger.EventMessage); wh.Job != filed.ID || !strings.Contains(wh.Text, "codeaf read your message") {
		t.Fatalf("message webhook %+v", wh)
	}

	status, err := client.Status(ctx, "inbox:ops", "A1")
	if err != nil || status.Job != filed.ID || status.State == "" {
		t.Fatalf("status = %+v, %v", status, err)
	}

	inbox.Notify(ctx, st, filed.ID, inbox.Opened("https://github.com/acme/web/pull/9"))
	if wh := rc.await(t, codeaftrigger.EventPROpened); wh.URL != "https://github.com/acme/web/pull/9" {
		t.Fatalf("pr webhook %+v", wh)
	}

	// Revoked, the token opens nothing.
	if code, data := operatorCall(t, factorySrv.URL, http.MethodDelete, serve.SourcesPath+"/inbox:ops", ""); code != 200 {
		t.Fatalf("revoke = %d %s", code, data)
	}
	var refusal *codeaftrigger.Error
	if _, err := client.Status(ctx, "inbox:ops", "A1"); !errors.As(err, &refusal) || refusal.Status != http.StatusUnauthorized {
		t.Fatalf("status after revoke = %v", err)
	}
}

// A SOURCE'S TOKEN IS SCOPED TO ITS SOURCE: it cannot file, read or message
// another source's work, and it opens no other path of the factory.
func TestASourceTokenCannotTouchAnotherSource(t *testing.T) {
	h, _, _ := intakeHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	mint := func(name string) string {
		code, data := operatorCall(t, srv.URL, http.MethodPost, serve.SourcesPath, `{"name":"`+name+`"}`)
		var m trigger.Minted
		if code != http.StatusCreated || json.Unmarshal(data, &m) != nil {
			t.Fatalf("add %s = %d %s", name, code, data)
		}
		return m.Token
	}
	ops, sales := mint("ops"), mint("sales")
	ctx := context.Background()
	salesClient := codeaftrigger.New(srv.URL, sales)
	if _, err := salesClient.File(ctx, codeaftrigger.Entry{ExternalID: "S1", Title: "a sales ask"}); err != nil {
		t.Fatalf("sales files its own (source left empty): %v", err)
	}
	opsClient := codeaftrigger.New(srv.URL, ops)
	var refusal *codeaftrigger.Error
	for name, call := range map[string]func() error{
		"file": func() error {
			_, err := opsClient.File(ctx, codeaftrigger.Entry{Source: "sales", ExternalID: "S2", Title: "x"})
			return err
		},
		"status":  func() error { _, err := opsClient.Status(ctx, "sales", "S1"); return err },
		"message": func() error { _, err := opsClient.Message(ctx, "sales", "S1", "x"); return err },
	} {
		if err := call(); !errors.As(err, &refusal) || refusal.Status != http.StatusForbidden || !strings.Contains(refusal.Message, "only work from ops") {
			t.Errorf("%s of sales with ops's token = %v", name, err)
		}
	}
	for _, path := range []string{"/v1/queue", serve.SourcesPath, "/v1/jobs/1"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+ops)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with a source token = %d, want 401", path, resp.StatusCode)
		}
	}
	// Registering is the operator's alone: a source cannot mint another.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+serve.SourcesPath, bytes.NewReader([]byte(`{"name":"evil"}`)))
	req.Header.Set("Authorization", "Bearer "+ops)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a source registering a source = %d", resp.StatusCode)
	}
	// The operator's own token still reads any source's work.
	if code, data := operatorCall(t, srv.URL, http.MethodGet, "/api/intake/sales/S1", ""); code != 200 {
		t.Fatalf("operator status = %d %s", code, data)
	}
}

// The inbox keeps as many of a job's comments as the GitHub poll does.
func TestAMessageKeepsAsManyCommentsAsAnIssue(t *testing.T) {
	if inbox.MessagesKept != factorygithub.CommentsMost {
		t.Fatalf("inbox keeps %d, the GitHub poll %d", inbox.MessagesKept, factorygithub.CommentsMost)
	}
}

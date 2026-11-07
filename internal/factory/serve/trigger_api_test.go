package serve_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
)

// hook is a webhook receiver that checks every delivery's signature.
type hook struct {
	secret string
	got    chan trigger.Body
	bad    chan error
}

func (k *hook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := trigger.Verify(k.secret, body, r.Header.Get(trigger.SignatureHeader), time.Now()); err != nil {
		k.bad <- err
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var b trigger.Body
	if err := json.Unmarshal(body, &b); err != nil || b.ID != r.Header.Get(trigger.DeliveryHeader) || b.Event != r.Header.Get(trigger.EventHeader) {
		k.bad <- errors.New("headers and body disagree: " + string(body))
	}
	k.got <- b
	w.WriteHeader(http.StatusOK)
}

// await is the next delivery of event, failing after a few seconds.
func (k *hook) await(t *testing.T, event string) trigger.Body {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b := <-k.got:
			if b.Event == event {
				return b
			}
		case err := <-k.bad:
			t.Fatalf("a bad delivery: %v", err)
		case <-deadline:
			t.Fatalf("no %s delivery arrived", event)
		}
	}
}

func triggerCall(t *testing.T, base, token, method, path, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// THE TRIGGER API END TO END over HTTP: the operator sets a source's
// webhook, files work, messages it, reads its status, and the receiver hears
// signed deliveries for the filing, the manager's reading and the pull
// request opening.
func TestTheTriggerAPIFilesMessagesReadsAndHearsBack(t *testing.T) {
	h, st, teammate := intakeHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	rc := &hook{got: make(chan trigger.Body, 32), bad: make(chan error, 32)}
	hookSrv := httptest.NewServer(rc)
	defer hookSrv.Close()

	code, data := triggerCall(t, srv.URL, "floor-secret", http.MethodPost, serve.WebhooksPath, `{"source":"inbox:ops","url":"`+hookSrv.URL+`"}`)
	var added trigger.Added
	if code != http.StatusCreated || json.Unmarshal(data, &added) != nil || !strings.HasPrefix(added.Secret, "whsec_") {
		t.Fatalf("add webhook = %d %s", code, data)
	}
	rc.secret = added.Secret
	if code, data := triggerCall(t, srv.URL, "floor-secret", http.MethodGet, serve.WebhooksPath, ""); code != 200 || strings.Contains(string(data), added.Secret) || !strings.Contains(string(data), hookSrv.URL) {
		t.Fatalf("list = %d %s", code, data)
	}

	code, data = triggerCall(t, srv.URL, "floor-secret", http.MethodPost, serve.IntakePath, `{"source":"inbox:ops","external_id":"A1","title":"Fix the login timeout","trust":"collaborator"}`)
	var filed inbox.Result
	if code != http.StatusCreated || json.Unmarshal(data, &filed) != nil {
		t.Fatalf("file = %d %s", code, data)
	}
	if b := rc.await(t, trigger.EventFiled); b.Job != filed.ID || b.ExternalID != "A1" || b.Source != "inbox:ops" {
		t.Fatalf("filed webhook %+v", b)
	}

	code, data = triggerCall(t, srv.URL, "floor-secret", http.MethodPost, "/api/intake/inbox:ops/A1/messages", `{"author":"dana","text":"it only happens on mobile"}`)
	var acc trigger.Accepted
	if code != 200 || json.Unmarshal(data, &acc) != nil || acc.Job != filed.ID || !acc.Accepted {
		t.Fatalf("message = %d %s", code, data)
	}
	if it, _ := st.GetSaved(filed.ID); len(it.Comments) != 1 || it.Comments[0].Body != "it only happens on mobile" || it.Comments[0].Author != "dana" {
		t.Fatalf("comments %+v", it.Comments)
	}
	if b := rc.await(t, trigger.EventMessage); b.Job != filed.ID || !strings.Contains(b.Text, "codeaf read your message") {
		t.Fatalf("message webhook %+v", b)
	}

	code, data = triggerCall(t, srv.URL, "floor-secret", http.MethodGet, "/api/intake/inbox:ops/A1", "")
	var status api.IntakeStatus
	if code != 200 || json.Unmarshal(data, &status) != nil || status.Job != filed.ID || status.State == "" {
		t.Fatalf("status = %d %s", code, data)
	}

	inbox.Notify(t.Context(), st, filed.ID, inbox.Opened("https://github.com/acme/web/pull/9"))
	if b := rc.await(t, trigger.EventPROpened); b.URL != "https://github.com/acme/web/pull/9" {
		t.Fatalf("pr webhook %+v", b)
	}

	// The operator's alone: a teammate is refused every one of these.
	for _, c := range [][3]string{
		{http.MethodGet, "/api/intake/inbox:ops/A1", ""},
		{http.MethodPost, "/api/intake/inbox:ops/A1/messages", `{"text":"x"}`},
		{http.MethodPost, serve.WebhooksPath, `{"source":"inbox:ops","url":"https://x.example.com"}`},
	} {
		if code, _ := triggerCall(t, srv.URL, teammate, c[0], c[1], c[2]); code != http.StatusForbidden {
			t.Errorf("%s %s by a teammate = %d", c[0], c[1], code)
		}
	}
	if code, _ := triggerCall(t, srv.URL, "floor-secret", http.MethodGet, "/api/intake/inbox:ops/nope", ""); code != http.StatusNotFound {
		t.Fatalf("unknown job = %d", code)
	}
	if code, _ := triggerCall(t, srv.URL, "floor-secret", http.MethodPost, "/api/intake/inbox:ops/A1/messages", `{"text":"  "}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("empty message = %d", code)
	}
	if code, _ := triggerCall(t, srv.URL, "floor-secret", http.MethodDelete, serve.WebhooksPath+"/inbox:ops", ""); code != 200 {
		t.Fatalf("remove = %d", code)
	}
}

// The inbox keeps as many of a job's comments as the GitHub poll does.
func TestAMessageKeepsAsManyCommentsAsAnIssue(t *testing.T) {
	if inbox.MessagesKept != factorygithub.CommentsMost {
		t.Fatalf("inbox keeps %d, the GitHub poll %d", inbox.MessagesKept, factorygithub.CommentsMost)
	}
}

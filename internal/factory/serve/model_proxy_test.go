package serve

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// modelEndedBody is the whole of what a refused model call is told.
const modelEndedBody = `{"error":"This chat's model access has ended."}` + "\n"

// bearerCall makes one model call as a chat would: no organization sign-in,
// only the bearer it was given, plus whatever hostile headers are supplied.
func (rg *hostedRig) bearerCall(method, target, token string, extra map[string]string) *httptest.ResponseRecorder {
	header := map[string]string{"Authorization": "Bearer " + token}
	for k, v := range extra {
		header[k] = v
	}
	return rg.send("", method, target, map[string]string{"model": "m"}, header)
}

// forbiddenToUpstream is what the model proxy must never pass on: the chat's
// own bearer and everything a client could use to name itself.
func forbiddenToUpstream() []string {
	var out []string
	for _, name := range forbiddenToHost {
		if name != "Origin" {
			out = append(out, name)
		}
	}
	return out
}

// A CHAT'S MODEL ACCESS LIVES AND DIES WITH THE CHAT: a minted bearer works,
// carrying the right path and query to the upstream and none of the caller's
// identity; ending the chat (stop or delete) ends the bearer; seven days end
// it too; and a bearer nobody issued, or a person's own sign-in, is refused
// with one sentence that says nothing else.
func TestC3_ModelBearerLifecycle(t *testing.T) {
	rg := newHostedRig(t)
	// A keyless upstream (the default rig) is sent no authorization at all.
	keyless := rg.create(t, "santosh")
	if w := rg.bearerCall("GET", "/v1/model/models", rg.host.tokenOf(keyless), nil); w.Code != 200 {
		t.Fatalf("keyless call = %d %s", w.Code, w.Body)
	}
	if all := rg.up.requests(); len(all) != 1 || all[0].header.Get("Authorization") != "" {
		t.Fatalf("a keyless upstream saw %+v", all)
	}

	rg.configure(t, func(c *HostedConfig) { c.ModelUpstreamKey = "upstream-key-123" })
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	// The first, keyless chat was handed its own bearer too; this chat's host
	// must have been given exactly this chat's bearer and the model address.
	given := 0
	for _, push := range rg.host.pushList() {
		if push.id != id {
			continue
		}
		given++
		if push.url != rigModelURL || push.token != token {
			t.Fatalf("the chat's host was given %+v", push)
		}
	}
	if given != 1 {
		t.Fatalf("the chat's host was given its model access %d times; want once", given)
	}

	hostile := hostedClientHeaders()
	w := rg.bearerCall("POST", "/v1/model/chat/completions?stream=true&x=a%20b", token, hostile)
	if w.Code != 200 || w.Body.String() != `{"ok":true}` {
		t.Fatalf("a model call = %d %s", w.Code, w.Body)
	}
	seen := rg.up.requests()
	if len(seen) != 2 {
		t.Fatalf("the upstream saw %d requests; want the keyless one and this one", len(seen))
	}
	got := seen[1]
	if got.method != "POST" || got.uri != "/api/v1/chat/completions?stream=true&x=a%20b" || !strings.Contains(got.body, `"model":"m"`) {
		t.Fatalf("the upstream saw %s %s %q", got.method, got.uri, got.body)
	}
	if got.header.Get("Authorization") != "Bearer upstream-key-123" {
		t.Fatalf("the upstream saw authorization %q; want the upstream's own key and not the chat's bearer", got.header.Get("Authorization"))
	}
	for _, name := range forbiddenToUpstream() {
		if values := got.header.Values(name); len(values) != 0 {
			t.Fatalf("the upstream saw %s: %v", name, values)
		}
	}
	for name := range got.header {
		if strings.HasPrefix(strings.ToLower(name), "x-codeaf-") {
			t.Fatalf("the upstream saw %s", name)
		}
	}
	if strings.Contains(strings.Join(got.header.Values("Authorization"), ""), token) {
		t.Fatal("the chat's bearer reached the upstream")
	}
	if w := rg.bearerCall("GET", "/v1/model/models", token, nil); w.Code != 200 {
		t.Fatalf("a GET = %d %s", w.Code, w.Body)
	} else if all := rg.up.requests(); all[len(all)-1].uri != "/api/v1/models" {
		t.Fatalf("a GET reached the upstream as %s", all[len(all)-1].uri)
	}

	// Refusals are one sentence and carry nothing that tells their causes apart.
	refused := func(what string, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != 401 || w.Body.String() != modelEndedBody || w.Header().Get("WWW-Authenticate") != `Bearer realm="model"` {
			t.Fatalf("%s = %d %q (%v)", what, w.Code, w.Body, w.Header())
		}
	}
	refused("an unknown bearer", rg.bearerCall("GET", "/v1/model/models", "cmt_"+strings.Repeat("Q", 43), nil))
	refused("a malformed bearer", rg.bearerCall("GET", "/v1/model/models", "not-a-bearer", nil))
	refused("no bearer", rg.send("", "GET", "/v1/model/models", nil, nil))
	refused("a person's sign-in", rg.bearerCall("GET", "/v1/model/models", rg.tokens["santosh"], nil))
	refused("the operator's token", rg.bearerCall("GET", "/v1/model/models", "operator-secret", nil))
	refused("a refused method without a bearer", rg.send("", "PUT", "/v1/model/models", nil, nil))
	if w := rg.bearerCall("PUT", "/v1/model/models", token, nil); w.Code != 405 || w.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("a PUT with a good bearer = %d %v", w.Code, w.Header())
	}

	// Stopping the chat through the organization's own route ends the bearer.
	if w := rg.bearerCall("GET", "/v1/model/models", token, nil); w.Code != 200 {
		t.Fatalf("before the stop = %d %s", w.Code, w.Body)
	}
	before := len(rg.up.requests())
	if w := rg.send("santosh", "POST", "/v1/hosted/sessions/"+id+"/stop", nil, nil); w.Code != 204 {
		t.Fatalf("stop = %d %s", w.Code, w.Body)
	}
	refused("a stopped chat's bearer", rg.bearerCall("GET", "/v1/model/models", token, nil))
	if len(rg.up.requests()) != before {
		t.Fatal("a revoked bearer reached the upstream")
	}
	// A deleted chat's bearer is ended too.
	doomed := rg.create(t, "santosh")
	doomedToken := rg.host.tokenOf(doomed)
	if w := rg.bearerCall("GET", "/v1/model/models", doomedToken, nil); w.Code != 200 {
		t.Fatalf("before the delete = %d %s", w.Code, w.Body)
	}
	if w := rg.send("santosh", "DELETE", "/v1/hosted/sessions/"+doomed, nil, nil); w.Code != 204 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	refused("a deleted chat's bearer", rg.bearerCall("GET", "/v1/model/models", doomedToken, nil))

	// Seven days end a bearer, to the second (twelve hours until P1 saved them).
	aging := rg.create(t, "santosh")
	agingToken := rg.host.tokenOf(aging)
	rg.now = rg.now.Add(7*24*time.Hour - time.Second)
	if w := rg.bearerCall("GET", "/v1/model/models", agingToken, nil); w.Code != 200 {
		t.Fatalf("just inside seven days = %d %s", w.Code, w.Body)
	}
	rg.now = rg.now.Add(2 * time.Second)
	refused("an expired bearer", rg.bearerCall("GET", "/v1/model/models", agingToken, nil))
}

// A MODEL'S REPLY ARRIVES AS IT IS WRITTEN: the first server-sent event reaches
// the caller while the upstream is still holding the rest, through the real
// handler, so a person watching a chat sees the first words, not all of them at
// the end.
func TestC3_ModelProxyStreams(t *testing.T) {
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	release := make(chan struct{})
	rg.up.mu.Lock()
	rg.up.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(20 * time.Second):
		}
		_, _ = io.WriteString(w, "data: two\n\n")
		w.(http.Flusher).Flush()
	}
	rg.up.mu.Unlock()
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequest("POST", srv.URL+"/v1/model/chat/completions", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string, 8)
	go func() {
		scan := bufio.NewScanner(resp.Body)
		for scan.Scan() {
			lines <- scan.Text()
		}
		close(lines)
	}()
	select {
	case line := <-lines:
		if line != "data: one" {
			t.Fatalf("the first line was %q", line)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the first event did not arrive while the upstream was still writing: the reply is being held back")
	}
	close(release)
	var rest []string
	for line := range lines {
		if line != "" {
			rest = append(rest, line)
		}
	}
	if len(rest) != 1 || rest[0] != "data: two" {
		t.Fatalf("after the first event: %v", rest)
	}
}

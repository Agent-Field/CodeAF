package connect

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/oauth2"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPluginBlurb(t *testing.T) {
	for _, tt := range []struct {
		service Service
		want    string
	}{
		{Service{Blurb: "Linear's own tools — your issues, projects and cycles — signed in in your browser."}, "Your issues, projects and cycles"},
		{Service{Blurb: "Datadog's own tools — your metrics, logs and monitors — signed in in your browser. Say which site first."}, "Your metrics, logs and monitors"},
		{Service{Auth: AuthKey, Blurb: catalogBlurb("Stripe", "https://api.stripe.com", blank{})}, "Your account at api.stripe.com · with your key"},
	} {
		if got := PluginBlurb(tt.service); got != tt.want {
			t.Errorf("got %q want %q", got, tt.want)
		}
	}
}

func TestPluginDescribeUsesCapabilityStore(t *testing.T) {
	m, err := NewManager(t.TempDir(), map[string]ClientCredential{"google": {ID: "fake", Secret: "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.DescribePlugin(context.Background(), "google")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Capabilities) != 4 || len(d.ScopesWords) != 2 || len(d.Capabilities[0].Tools) != 2 {
		t.Fatalf("describe: %+v", d)
	}
	if err = m.SetCapabilityState("google", "mail-read", StateOff); err != nil {
		t.Fatal(err)
	}
	d, err = m.DescribePlugin(context.Background(), "google")
	if err != nil || d.Capabilities[0].State != StateOff {
		t.Fatalf("store not shared: %+v %v", d, err)
	}
}

func TestPluginDescriptionListsServerTools(t *testing.T) {
	fake := startFakeToolServer(t, fakeShape{})
	m, _ := withToolServer(t, fake)
	connectFake(t, m)
	d, err := m.DescribePlugin(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tools) != 4 {
		t.Fatalf("tools: %+v", d.Tools)
	}
	for _, tool := range d.Tools {
		if tool.Name == "find_pages" && (!tool.ReadOnly || tool.Title != "Find pages") {
			t.Fatalf("tool: %+v", tool)
		}
	}
}

func TestPluginCancelCannotSaveQueuedBrowserSuccess(t *testing.T) {
	m, _ := testManager(t)
	f := &Flow{ctx: context.Background(), manager: m, plug: google{}, cancel: func() {}, done: make(chan flowResult, 1)}
	f.done <- flowResult{keys: &oauth2.Token{AccessToken: "fake", RefreshToken: "fake"}}
	f.Cancel()
	if _, err := f.Wait(context.Background()); err == nil {
		t.Fatal("cancelled sign-in was saved")
	}
	if m.Connected("google") {
		t.Fatal("cancelled sign-in connected")
	}
}

func TestPluginKeyAnswersSaveOnlySuccess(t *testing.T) {
	const secret = "private-key-that-must-not-escape"
	for _, kind := range []string{"shape", "refused", "no_answer", "success"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			m, plug, _ := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if kind == "no_answer" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				if kind == "refused" {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(secret + strings.Repeat(" refused", 30)))
					return
				}
				_, _ = w.Write([]byte("{}"))
			}), nil)
			plug.probe = probe{address: plug.base + "/me"}
			key := secret
			if kind == "shape" {
				key = "two keys"
			}
			_, err := m.ConnectKey(context.Background(), "example", key)
			if kind == "success" {
				if err != nil || !m.Connected("example") {
					t.Fatalf("success: %v", err)
				}
				return
			}
			var failure *KeyFailure
			if !errors.As(err, &failure) || failure.Kind != kind {
				t.Fatalf("answer: %v", err)
			}
			if len([]rune(failure.Words)) > 120 || strings.Contains(failure.Words, secret) || m.Connected("example") {
				t.Fatalf("unsafe answer: %+v", failure)
			}
			if kind == "shape" && calls != 0 {
				t.Fatal("wrong shape reached the vendor")
			}
		})
	}
}

type pluginCanceledAccount struct {
	Plug
	cancel context.CancelFunc
}

func (p pluginCanceledAccount) Account(context.Context, *http.Client) (string, error) {
	p.cancel()
	return "", context.Canceled
}

func TestPluginCancelledIdentityProbeSavesNothing(t *testing.T) {
	m, _ := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &Flow{ctx: context.Background(), manager: m, plug: pluginCanceledAccount{Plug: google{}, cancel: cancel}, cancel: func() {}, done: make(chan flowResult, 1)}
	f.done <- flowResult{keys: &oauth2.Token{AccessToken: "fake", RefreshToken: "fake"}}
	if _, err := f.Wait(ctx); err == nil {
		t.Fatal("cancelled identity probe saved a connection")
	}
	if m.Connected("google") {
		t.Fatal("cancelled sign-in connected")
	}
}

func TestPluginSavedDatesAndShortAsk(t *testing.T) {
	m, _, dir := keyManager(t, http.NotFoundHandler(), func(p *keyPlug) {
		p.blank = blank{name: "workspace", label: "Workspace"}
		p.base += "/{{.workspace}}"
		p.service.KeyAsk = "Give the workspace and then the key, one space between them."
	})
	before := time.Now().UTC().Truncate(time.Second)
	if _, err := m.ConnectKey(context.Background(), "example", "team fake-key"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(reopened.Plugins()[0])
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	for _, field := range []string{"connected_at", "key_saved_at"} {
		value, _ := got[field].(string)
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || at.Before(before) || at.After(time.Now()) {
			t.Fatalf("%s = %v", field, got[field])
		}
	}
	if got["ask"].(map[string]any)["label"] != "Workspace" {
		t.Fatalf("ask = %v", got["ask"])
	}
	if err := reopened.Disconnect("example"); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(reopened.Plugins()[0])
	_ = json.Unmarshal(raw, &got)
	if strings.Contains(string(raw), "connected_at") || strings.Contains(string(raw), "key_saved_at") {
		t.Fatalf("disconnected dates: %s", raw)
	}
}

func TestPluginBrowserDatesAndLegacyAbsence(t *testing.T) {
	m, _ := testManager(t)
	f := &Flow{ctx: context.Background(), manager: m, plug: google{}, cancel: func() {}, done: make(chan flowResult, 1)}
	f.done <- flowResult{keys: &oauth2.Token{AccessToken: "fake", RefreshToken: "fake"}}
	if _, err := f.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Plugins() {
		if p.ID == "google" {
			if _, err := time.Parse(time.RFC3339Nano, p.ConnectedAt); err != nil || p.KeySavedAt != "" {
				t.Fatalf("browser dates = %+v", p)
			}
		}
	}
	if err := m.store.put("google", stored{Keys: &oauth2.Token{RefreshToken: "old"}, Scopes: wholeGrant()}); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Plugins() {
		if p.ID == "google" && (p.ConnectedAt != "" || p.KeySavedAt != "") {
			t.Fatalf("invented old dates = %+v", p)
		}
	}
}

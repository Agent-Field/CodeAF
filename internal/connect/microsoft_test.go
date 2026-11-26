package connect

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"
)

func TestMicrosoftRegistrationAndRefusals(t *testing.T) {
	const id = "12345678-1234-1234-1234-123456789abc"
	for _, tenant := range []string{"", id} {
		p, err := microsoftRegistration(ClientCredential{ID: id, Tenant: tenant})
		if err != nil {
			t.Fatal(err)
		}
		if tenant == "" {
			tenant = "common"
		}
		cfg := (&Manager{}).config(p, ClientCredential{ID: id})
		address, err := url.Parse(cfg.AuthCodeURL("state", p.AuthCodeOptions()...))
		if err != nil || address.Host != "login.microsoftonline.com" || address.Path != "/"+tenant+"/oauth2/v2.0/authorize" ||
			!strings.Contains(address.Query().Get("scope"), "offline_access") ||
			strings.Contains(address.Query().Get("scope"), "OnlineMeetingTranscript.Read.All") {
			t.Fatalf("unexpected authorize request: %v", address)
		}
	}
	for _, tc := range []struct{ description, want string }{
		{"AADSTS90094: tenant and trace", "Your Microsoft administrator must approve this connection."},
		{"AADSTS53003: tenant and trace", "Your organization's Microsoft Conditional Access policy blocked this connection."},
	} {
		for _, err := range []error{
			errors.New("authorization error from server: access_denied " + tc.description),
			&oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}, ErrorDescription: tc.description},
		} {
			if got := SignInFailureReason(microsoftSignInError(err)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		}
	}
}

type microsoftTestTransport func(*http.Request) (*http.Response, error)

func (f microsoftTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMicrosoftSignInPKCEAndRefreshArePersisted(t *testing.T) {
	onFreePort(t)
	const id = "12345678-1234-1234-1234-123456789abc"
	var challenge atomic.Value
	var refreshes atomic.Int32
	transport := microsoftTestTransport(func(req *http.Request) (*http.Response, error) {
		raw := ""
		switch {
		case req.URL.Host == "login.microsoftonline.com" && req.URL.Path == "/common/oauth2/v2.0/token":
			if err := req.ParseForm(); err != nil {
				return nil, err
			}
			switch req.PostForm.Get("grant_type") {
			case "authorization_code":
				if req.PostForm.Get("client_id") != id || req.PostForm.Get("client_secret") != "" ||
					oauth2.S256ChallengeFromVerifier(req.PostForm.Get("code_verifier")) != challenge.Load() {
					t.Error("code exchange did not prove the public client and PKCE verifier")
				}
				raw = `{"access_token":"old","refresh_token":"renew-one","token_type":"Bearer","expires_in":1,"scope":"User.Read User.ReadBasic.All Mail.ReadWrite Calendars.ReadWrite Files.Read.All"}`
			case "refresh_token":
				refreshes.Add(1)
				if req.PostForm.Get("refresh_token") != "renew-one" || req.PostForm.Get("client_id") != id {
					t.Error("refresh omitted the stored grant or client")
				}
				raw = `{"access_token":"fresh","refresh_token":"renew-two","token_type":"Bearer","expires_in":3600}`
			}
		case req.URL.Host == "graph.microsoft.com" && req.URL.Path == "/v1.0/me":
			if auth := req.Header.Get("Authorization"); auth != "Bearer old" && auth != "Bearer fresh" {
				t.Errorf("identity request had unexpected auth %q", auth)
			}
			raw = `{"id":"directory-user","mail":"mira@example.com"}`
		}
		if raw == "" {
			return nil, errors.New("unexpected request to Microsoft fixture")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: req}, nil
	})
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: transport})
	creds := map[string]ClientCredential{Microsoft365: {ID: id}}
	profile := t.TempDir()
	manager, err := NewManager(profile, creds)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := manager.BeginAuth(ctx, Microsoft365, "")
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Cancel()
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(loopback(t, flow.URL()).String())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	authorize, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	params := authorize.Query()
	challenge.Store(params.Get("code_challenge"))
	if params.Get("code_challenge") == "" || params.Get("code_challenge_method") != "S256" {
		t.Fatal("Microsoft authorize request omitted PKCE")
	}
	callback := loopback(t, params.Get("redirect_uri"))
	callback.RawQuery = url.Values{"state": {params.Get("state")}, "code": {"mock-code"}}.Encode()
	resp, err = browser.Get(callback.String())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	status, err := flow.Wait(ctx)
	if err != nil || !status.Connected || status.Account != "mira@example.com" {
		t.Fatalf("sign-in: %+v %v", status, err)
	}
	grant, _, err := manager.MicrosoftGrant()
	if err != nil {
		t.Fatal(err)
	}
	client, err := manager.Client(ctx, Microsoft365)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = client.Get(MicrosoftGraph + "/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	again, err := NewManager(profile, creds)
	if err != nil {
		t.Fatal(err)
	}
	newGrant, _, err := again.MicrosoftGrant()
	record, _, readErr := again.store.get(Microsoft365)
	if err != nil || readErr != nil || grant != newGrant || record.Keys.RefreshToken != "renew-two" || refreshes.Load() != 1 {
		t.Fatalf("refresh did not preserve the grant and rotate the stored token: %v %v", err, readErr)
	}
}

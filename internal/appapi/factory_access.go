package appapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
)

// openAccess keeps remote sign-in cookies and session grants inside the binary.
// The app receives public progress; its existing connection producer reads the retained session.
func (f *FactoryRegistry) openAccess(w http.ResponseWriter, r *http.Request) {
	link := r.URL.Query().Get("link")
	var body factoryapi.FactoryAccessRequest
	if r.Method == "POST" {
		if !decodeFactory(w, r, &body) {
			return
		}
		link = body.Link
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "codeaf" || parsed.Host != "factory" || parsed.Path != "/join" {
		fail(w, 422, "bad_input", "use a factory access link")
		return
	}
	address, err := factoryOrigin(parsed.Query().Get("url"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		fail(w, 503, "not_allowed", "the engine is stopping")
		return
	}
	if f.Credentials == nil {
		fail(w, 503, "not_allowed", "factory credentials are unavailable")
		return
	}
	if f.accessCookies == nil {
		f.accessCookies = map[string][]*http.Cookie{}
	}
	if len(f.accessCookies) >= 256 && f.accessCookies[link] == nil {
		fail(w, 429, "not_allowed", "too many pending factory links")
		return
	}
	endpoint := address + "/v1/factory-access"
	var data []byte
	if r.Method == "GET" {
		endpoint += "?link=" + url.QueryEscape(link)
	} else {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, bytes.NewReader(data))
	if err != nil {
		agentFailure(w, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if token := f.Credentials.FactorySession(address); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range f.accessCookies[link] {
		req.AddCookie(cookie)
	}
	client := http.Client{Timeout: 20 * time.Second}
	if f.Client != nil {
		client = *f.Client
		if client.Timeout == 0 {
			client.Timeout = 20 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		write(w, 200, factoryapi.FactoryAccess{State: "unreachable", Link: link, Factory: factoryapi.AccessFactory{ID: factoryID(address), Host: address, Repos: []string{}}, Message: "The factory is not answering."})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, resp.StatusCode, "not_allowed", "the factory could not complete this access request")
		return
	}
	var out factoryapi.FactoryAccess
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil {
		fail(w, 502, "bad_input", "the factory returned an invalid access response")
		return
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "factory_join" {
			f.accessCookies[link] = []*http.Cookie{cookie}
		}
	}
	if (out.State == "joined" || out.State == "already_joined") && out.AccessToken != "" {
		f.Credentials.SetFactorySession(address, out.AccessToken)
	}
	out.AccessToken = ""
	if out.State == "joined" || out.State == "already_joined" {
		delete(f.accessCookies, link)
		row := factoryConnection{ID: factoryID(address), URL: address, Mode: "team"}
		_, err = f.connections(func(saved *factoryConnections) error {
			for _, old := range saved.Factories {
				if old.ID == row.ID {
					return nil
				}
			}
			saved.Factories = append(saved.Factories, row)
			return nil
		})
		if err != nil {
			agentFailure(w, err)
			return
		}
		f.server.SetInboxSource(row.ID, f.inboxSource(row))
		f.publish(row, "ok")
		if f.Changed != nil {
			f.Changed()
		}
	}
	// A cancellation drops only the initiating flow's cookie; the connection itself stays signed in.
	if r.Method == "POST" && strings.EqualFold(body.Verb, "cancel") {
		delete(f.accessCookies, link)
	}
	write(w, 200, out)
}

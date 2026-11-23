package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── JOINING A FACTORY FROM THE COMMAND LINE ────────────────────────────────
//
// An access link (`codeaf://factory/join?url=<factory>#token=<secret>`, minted
// by the factory's operator, [serve]'s access_links.go) is redeemed by signing
// in with GitHub, and the sign-in is the PERSON'S OWN: the factory starts
// GitHub's device flow, the person enters the code on GitHub, and the factory
// answers with a session for that person alone. This file speaks the same
// `/v1/factory-access` door the desktop app does, so a runner that joins this
// way works as the person who signed in, never as the operator and never on a
// shared credential. The link's secret is sent to the factory only; the
// session that comes back is kept in attach.json (0600) and nowhere else.

// factoryJoinEvery is how often the sign-in is asked whether it finished.
// GitHub's device flow asks for at least five seconds between polls; the
// factory refuses sooner ones itself, so this is only the polite floor. It is
// a variable so a test can shorten it.
var factoryJoinEvery = 5 * time.Second

// factoryJoinLink is a join link taken apart.
type factoryJoinLink struct {
	base   string // the factory's origin
	secret string // the link's secret
}

// canonical is the one spelling the factory accepts ([serve]'s accessSecret).
func (l factoryJoinLink) canonical() string {
	return "codeaf://factory/join?" + url.Values{"url": {l.base}}.Encode() + "#token=" + url.QueryEscape(l.secret)
}

// parseFactoryJoinLink reads a `codeaf://factory/join` link. `codeaf://join`
// is read as the same link: it is the short spelling the setup boards use for
// it, and the canonical form is what goes to the factory either way.
func parseFactoryJoinLink(raw string) (factoryJoinLink, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "codeaf" {
		return factoryJoinLink{}, errors.New("use a factory access link: codeaf://factory/join?url=…#token=…")
	}
	if !(u.Host == "factory" && u.Path == "/join") && !(u.Host == "join" && (u.Path == "" || u.Path == "/")) {
		return factoryJoinLink{}, errors.New("this is not a factory access link")
	}
	base, err := factoryURL(u.Query().Get("url"))
	if err != nil {
		return factoryJoinLink{}, err
	}
	fragment, err := url.ParseQuery(u.EscapedFragment())
	if err != nil || strings.TrimSpace(fragment.Get("token")) == "" {
		return factoryJoinLink{}, errors.New("the access link has no token")
	}
	return factoryJoinLink{base: base, secret: strings.TrimSpace(fragment.Get("token"))}, nil
}

// factoryJoinSentence is what a person is told for a link that cannot be used.
// Each is one plain sentence and none is an alarm: an expired invite is the
// ordinary end of an invite (the setup boards' access-link cards).
func factoryJoinSentence(in api.FactoryAccess) string {
	switch in.State {
	case "expired":
		return "This access link has expired. Ask the operator for a new one."
	case "revoked":
		return "This access link was revoked. Ask the operator for a new one."
	case "used_up":
		return "This access link has already been used. Ask the operator for a new one."
	case "already_joined":
		return "You are already in this factory on another sign-in; use that session with --token."
	case "no_access":
		if in.Message != "" {
			return in.Message
		}
		return "Your GitHub cannot see this factory's repositories."
	}
	if in.Message != "" {
		return in.Message
	}
	return "This access link cannot be used."
}

// redeemFactoryLink signs the person in through link and answers the session
// the factory issued them. out gets the one sentence a person needs while
// waiting: where to go and what to type.
func redeemFactoryLink(ctx context.Context, link factoryJoinLink, out io.Writer) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	// NO PROXY: the factory may be a loopback or private address, and the
	// link's secret must go only where the link says.
	client := &http.Client{Timeout: 30 * time.Second, Jar: jar,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	ask := func(ctx context.Context, method string, body any) (api.FactoryAccess, error) {
		var reader io.Reader
		target := link.base + "/v1/factory-access"
		if method == http.MethodPost {
			data, _ := json.Marshal(body)
			reader = bytes.NewReader(data)
		} else {
			target += "?" + url.Values{"link": {link.canonical()}}.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return api.FactoryAccess{}, err
		}
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return api.FactoryAccess{}, fmt.Errorf("the factory at %s does not answer: %w", link.base, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK {
			var refusal struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(data, &refusal) == nil && refusal.Error != "" {
				return api.FactoryAccess{}, errors.New(refusal.Error)
			}
			return api.FactoryAccess{}, fmt.Errorf("the factory answered HTTP %d", resp.StatusCode)
		}
		var reply api.FactoryAccess
		if err := json.Unmarshal(data, &reply); err != nil {
			return api.FactoryAccess{}, errors.New("the factory's answer was not understood")
		}
		return reply, nil
	}
	reply, err := ask(ctx, http.MethodPost, api.FactoryAccessRequest{Link: link.canonical(), Verb: "sign_in"})
	if err != nil {
		return "", err
	}
	told := false
	for {
		switch reply.State {
		case "joined":
			if reply.AccessToken == "" {
				return "", errors.New("the factory joined you but sent no session")
			}
			return reply.AccessToken, nil
		case "waiting":
			if !told {
				told = true
				if _, err := fmt.Fprintf(out, "sign in with GitHub: open %s and enter %s\n", reply.SignInURL, reply.UserCode); err != nil {
					return "", err
				}
			}
		default:
			// Leave nothing half-started on the factory: a sign-in that cannot
			// finish is cancelled so it does not hold one of its pending places.
			if reply.State == "ready" {
				return "", errors.New("the factory did not start a GitHub sign-in")
			}
			return "", errors.New(factoryJoinSentence(reply))
		}
		select {
		case <-ctx.Done():
			// The caller's context has ended, so the cancel gets its own short one.
			bye, stop := context.WithTimeout(context.Background(), 3*time.Second)
			_, _ = ask(bye, http.MethodPost, api.FactoryAccessRequest{Link: link.canonical(), Verb: "cancel"})
			stop()
			return "", ctx.Err()
		case <-time.After(factoryJoinEvery):
		}
		if reply, err = ask(ctx, http.MethodGet, nil); err != nil {
			return "", err
		}
	}
}

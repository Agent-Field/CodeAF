package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
)

// factorySourceUsage is the one-line shape `codeaf factory source` is refused with.
const factorySourceUsage = "use codeaf factory source add NAME [--callback URL] [--trust collaborator|stranger], codeaf factory source list, or codeaf factory source revoke NAME [--factory URL --token T]"

// runFactorySource is `codeaf factory source add|list|revoke`: the Trigger
// API's registered sources on a running factory, through its /v1/sources.
// LIKE `codeaf factory add`, IT IS A CLIENT AND ONLY A CLIENT, and it needs
// the operator's token: the factory this machine is attached to or serves,
// or --factory with --token.
func runFactorySource(args []string, out io.Writer) error {
	if len(args) == 0 {
		return wrongCall(factorySourceUsage)
	}
	verb, args := args[0], args[1:]
	flags := commandFlags("factory source " + verb)
	callback := flags.String("callback", "", "where the factory sends this source's webhooks")
	trust := flags.String("trust", "", "the most this source's work is trusted: stranger (the default) or collaborator")
	address := flags.String("factory", "", "the factory's address, such as https://factory.example.com")
	token := flags.String("token", "", "that factory's access token (codeaf factory token prints it)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	rest := flags.Args()
	switch verb {
	case "add", "revoke":
		if len(rest) != 1 {
			return wrongCall(factorySourceUsage)
		}
		if err := trigger.CheckName(rest[0]); err != nil {
			return wrongCall(err.Error())
		}
	case "list":
		if len(rest) != 0 {
			return wrongCall(factorySourceUsage)
		}
	default:
		return wrongCall(factorySourceUsage)
	}
	if verb == "add" {
		if _, err := trigger.CheckTrust(factory.Tier(*trust)); err != nil {
			return wrongCall(err.Error())
		}
		if _, err := trigger.CheckCallback(*callback); err != nil {
			return wrongCall(err.Error())
		}
	}
	target, err := factoryAddTarget(*address, *token)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch verb {
	case "add":
		var m trigger.Minted
		body := map[string]string{"name": rest[0], "callback": *callback, "trust": *trust}
		if err := sourceCall(ctx, target, http.MethodPost, serve.SourcesPath, body, &m); err != nil {
			return err
		}
		// The two secrets are the answer a script captures; the rest is said
		// beside it, on the aside.
		if _, err := fmt.Fprintf(out, "token: %s\nsigning secret: %s\n", m.Token, m.Secret); err != nil {
			return err
		}
		_, err = fmt.Fprintf(aside, "registered %s · trust %s\nKeep both now: neither is shown again. The token files, reads and messages only work from %s.\n",
			m.Name, m.Trust, m.Name)
		if err == nil && m.Callback != "" {
			_, err = fmt.Fprintf(aside, "Webhooks go to %s, signed with the secret.\n", m.Callback)
		}
		return err
	case "list":
		var list struct {
			Sources []trigger.Public `json:"sources"`
		}
		if err := sourceCall(ctx, target, http.MethodGet, serve.SourcesPath, nil, &list); err != nil {
			return err
		}
		if len(list.Sources) == 0 {
			_, err = fmt.Fprintln(out, "no sources registered; add one with codeaf factory source add NAME")
			return err
		}
		for _, s := range list.Sources {
			line := s.Name + " · trust " + string(s.Trust)
			if s.Callback != "" {
				line += " · webhooks to " + s.Callback
			}
			if _, err := fmt.Fprintln(out, line); err != nil {
				return err
			}
		}
		return nil
	}
	if err := sourceCall(ctx, target, http.MethodDelete, serve.SourcesPath+"/"+url.PathEscape(rest[0]), nil, nil); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "revoked %s: its token stops working now\n", rest[0])
	return err
}

// sourceCall is one call to /v1/sources, its answer read into into.
func sourceCall(ctx context.Context, t factoryTarget, method, path string, body, into any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, t.url+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return fmt.Errorf("the factory at %s did not answer: %w", t.url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var refusal struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &refusal) == nil && strings.TrimSpace(refusal.Error) != "" {
			return fmt.Errorf("the factory refused it: %s", refusal.Error)
		}
		return fmt.Errorf("the factory refused it (%s)", resp.Status)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("the factory answered in a shape this codeaf cannot read")
	}
	return nil
}

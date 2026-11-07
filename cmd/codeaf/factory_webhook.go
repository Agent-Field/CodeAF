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

	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
)

// factoryWebhookUsage is the one-line shape `codeaf factory webhook` is refused with.
const factoryWebhookUsage = "use codeaf factory webhook add SOURCE URL, codeaf factory webhook list, or codeaf factory webhook remove SOURCE [--factory URL --token T]"

// runFactoryWebhook is `codeaf factory webhook add|list|remove`: the signed
// webhook a source's jobs are told about, on a running factory's
// /v1/webhooks. LIKE `codeaf factory add`, IT IS A CLIENT AND ONLY A CLIENT,
// with the operator's token: the factory this machine is attached to or
// serves, or --factory with --token.
func runFactoryWebhook(args []string, out io.Writer) error {
	if len(args) == 0 {
		return wrongCall(factoryWebhookUsage)
	}
	verb, args := args[0], args[1:]
	flags := commandFlags("factory webhook " + verb)
	address := flags.String("factory", "", "the factory's address, such as https://factory.example.com")
	token := flags.String("token", "", "that factory's access token (codeaf factory token prints it)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	rest := flags.Args()
	want := map[string]int{"add": 2, "list": 0, "remove": 1}
	n, known := want[verb]
	if !known || len(rest) != n {
		return wrongCall(factoryWebhookUsage)
	}
	if n > 0 {
		if err := trigger.CheckSource(rest[0]); err != nil {
			return wrongCall(err.Error())
		}
	}
	if verb == "add" {
		if _, err := trigger.CheckURL(rest[1]); err != nil {
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
		var added trigger.Added
		if err := webhookCall(ctx, target, http.MethodPost, serve.WebhooksPath, map[string]string{"source": rest[0], "url": rest[1]}, &added); err != nil {
			return err
		}
		// The secret is the answer a script captures; the rest is said beside it.
		if _, err := fmt.Fprintf(out, "secret: %s\n", added.Secret); err != nil {
			return err
		}
		_, err = fmt.Fprintf(aside, "%s's jobs are now told to %s, signed with this secret. Keep it now: it is not shown again.\n", added.Source, added.URL)
		return err
	case "list":
		var list struct {
			Webhooks []trigger.Public `json:"webhooks"`
		}
		if err := webhookCall(ctx, target, http.MethodGet, serve.WebhooksPath, nil, &list); err != nil {
			return err
		}
		if len(list.Webhooks) == 0 {
			_, err = fmt.Fprintln(aside, "no webhooks; add one with codeaf factory webhook add SOURCE URL")
			return err
		}
		for _, hook := range list.Webhooks {
			if _, err := fmt.Fprintln(out, hook.Source+" · "+hook.URL); err != nil {
				return err
			}
		}
		return nil
	}
	if err := webhookCall(ctx, target, http.MethodDelete, serve.WebhooksPath+"/"+url.PathEscape(rest[0]), nil, nil); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "removed the webhook of %s\n", rest[0])
	return err
}

// webhookCall is one call to /v1/webhooks, its answer read into into.
func webhookCall(ctx context.Context, t factoryTarget, method, path string, body, into any) error {
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

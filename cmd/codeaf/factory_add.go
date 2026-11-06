package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// factoryAddUsage is the one-line shape `codeaf factory add` is refused with.
const factoryAddUsage = "use codeaf factory add --source NAME --id ID --title TEXT [--body TEXT|-] [--repo owner/name] [--label L]... [--priority p0..p3] [--url URL] [--factory URL --token T]"

// labelFlags is --label, given once per label.
type labelFlags []string

func (l *labelFlags) String() string     { return strings.Join(*l, ",") }
func (l *labelFlags) Set(s string) error { *l = append(*l, s); return nil }

// runFactoryAdd is `codeaf factory add`: one piece of outside work posted to
// a running factory's POST /api/intake. IT IS A CLIENT AND ONLY A CLIENT: it
// reads the flags into an [inbox.Entry], checks it with the inbox's own
// check so a typo is refused before anything is sent, and prints what the
// factory did. Which factory: --factory (with --token), else the one this
// machine is attached to, else the one this machine serves.
func runFactoryAdd(args []string, stdin io.Reader, out io.Writer) error {
	flags := commandFlags("factory add")
	var e inbox.Entry
	var labels labelFlags
	flags.StringVar(&e.Source, "source", "", "the system the work comes from, such as inbox:ops")
	flags.StringVar(&e.ExternalID, "id", "", "the work's key on that system; the same source and id update one job")
	flags.StringVar(&e.Title, "title", "", "what the work is, in one line")
	body := flags.String("body", "", "the details, or - to read them from standard input")
	flags.StringVar(&e.Repo, "repo", "", "owner/name of the repository the work is on")
	flags.Var(&labels, "label", "a label, once per label")
	flags.StringVar(&e.Priority, "priority", "", "p0 (urgent) to p3 (low)")
	flags.StringVar(&e.URL, "url", "", "the work's address on its own system")
	address := flags.String("factory", "", "the factory's address, such as https://factory.example.com")
	token := flags.String("token", "", "that factory's access token (codeaf factory token prints it)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall(factoryAddUsage)
	}
	e.Body = *body
	if e.Body == "-" {
		data, err := io.ReadAll(io.LimitReader(stdin, inbox.MaxRequest+1))
		if err != nil {
			return err
		}
		e.Body = string(data)
	}
	e.Labels = labels
	clean, err := inbox.Validate(e)
	if err != nil {
		return wrongCall(err.Error())
	}
	target, err := factoryAddTarget(*address, *token)
	if err != nil {
		return err
	}
	res, err := postIntake(context.Background(), target, clean)
	if err != nil {
		return err
	}
	verb := "updated"
	if res.Created {
		verb = "filed"
	}
	_, err = fmt.Fprintf(out, "%s job %d from %s:%s\n", verb, res.ID, clean.Source, clean.ExternalID)
	return err
}

// factoryAddTarget is the factory `codeaf factory add` posts to.
func factoryAddTarget(address, token string) (factoryTarget, error) {
	if strings.TrimSpace(address) != "" {
		base, err := factoryURL(address)
		if err != nil {
			return factoryTarget{}, wrongCall(err.Error())
		}
		return factoryTarget{url: base, token: strings.TrimSpace(token)}, nil
	}
	if a, ok := readFactoryAttachment(); ok {
		t := factoryTarget{url: a.URL, token: a.Token}
		if strings.TrimSpace(token) != "" {
			t.token = strings.TrimSpace(token)
		}
		return t, nil
	}
	if t, ok := liveFactoryDiscovery(); ok {
		return t, nil
	}
	return factoryTarget{}, fmt.Errorf("no factory is running here; start one with codeaf factory serve, or name one with --factory URL --token T")
}

// postIntake sends e once and reads the factory's answer, or its refusal in
// its own words.
func postIntake(ctx context.Context, t factoryTarget, e inbox.Entry) (inbox.Result, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return inbox.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url+serve.IntakePath, bytes.NewReader(raw))
	if err != nil {
		return inbox.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return inbox.Result{}, fmt.Errorf("the factory at %s did not answer: %w", t.url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var refusal struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &refusal) == nil && refusal.Error != "" {
			return inbox.Result{}, fmt.Errorf("the factory refused it: %s", refusal.Error)
		}
		return inbox.Result{}, fmt.Errorf("the factory refused it (%s)", resp.Status)
	}
	var res inbox.Result
	if err := json.Unmarshal(data, &res); err != nil || res.ID < 1 {
		return inbox.Result{}, fmt.Errorf("the factory answered but did not name the job; read the queue before sending it again")
	}
	return res, nil
}

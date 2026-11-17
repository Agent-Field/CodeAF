package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ── factory.jira_connect, factory.jira_status, factory.jira_disconnect ─────
//
// A factory reads Jira Cloud tickets as work: every ticket the person's JQL
// finds becomes a job through the inbox, and the ticket gets a comment with
// the pull request's link when the job opens it and when it merges
// (internal/factory/jira). These three methods connect, read and forget that
// connection.
//
// factory.jira_connect PARAMS ARE {factory, site, email, token, jql}, every
// one required. The engine checks the email and API token against Jira's
// /myself and the JQL with one search BEFORE it keeps anything, and answers
// the connection's status; a refusal keeps nothing. THE TOKEN TRAVELS IN ONE
// DIRECTION ONLY: it is kept where the GitHub token is kept and no answer,
// event or error ever carries it back.
//
// While a factory is not connected, nothing reads Jira: no feed, no loop, no
// comment. Wave 1 connects only the factory on this machine.

// FeatureFactoryJira is announced in hello when the engine serves the three
// Jira methods.
const FeatureFactoryJira = "factory.jira"

// MaxJiraJQL is the longest JQL factory.jira_connect takes, in bytes.
const MaxJiraJQL = 2000

// FactoryJiraConnectParams is one connect.
type FactoryJiraConnectParams struct {
	Factory string `json:"factory"`
	// Site is the Jira Cloud site, such as https://acme.atlassian.net; a bare
	// host is read as https.
	Site string `json:"site"`
	// Email is the Jira account's sign-in email.
	Email string `json:"email"`
	// Token is an Atlassian API token for that account. It is never sent back.
	Token string `json:"token"`
	// JQL is which tickets become jobs, such as `project = FL AND labels = codeaf`.
	JQL string `json:"jql"`
}

// FactoryJiraStatus is a factory's Jira connection as a person reads it.
// Every field but factory and connected is absent when unknown.
type FactoryJiraStatus struct {
	Factory   string `json:"factory"`
	Connected bool   `json:"connected"`
	Site      string `json:"site,omitempty"`
	Email     string `json:"email,omitempty"`
	JQL       string `json:"jql,omitempty"`
	// Account is the connected account's display name.
	Account string `json:"account,omitempty"`
	// LastRead is when the factory last read Jira without an error.
	LastRead *time.Time `json:"last_read,omitempty"`
	// LastError is what the last read that failed said, cleared by the next
	// read that works.
	LastError string `json:"last_error,omitempty"`
}

// FactoryJira is the engine's half of the three methods.
type FactoryJira interface {
	FactoryJiraConnect(ctx context.Context, p FactoryJiraConnectParams) (FactoryJiraStatus, error)
	FactoryJiraStatus(ctx context.Context, id string) (FactoryJiraStatus, error)
	FactoryJiraDisconnect(ctx context.Context, id string) (FactoryJiraStatus, error)
}

func withFactoryJiraFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJira); ok {
		hello = withFeature(hello, FeatureFactoryJira)
	}
	return hello
}

// CheckJiraConnect bounds a connect without asking anyone. It never quotes the
// token in a refusal.
func CheckJiraConnect(p FactoryJiraConnectParams) (FactoryJiraConnectParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	p.Site, p.Email, p.Token, p.JQL = strings.TrimSpace(p.Site), strings.TrimSpace(p.Email), strings.TrimSpace(p.Token), strings.TrimSpace(p.JQL)
	switch {
	case p.Site == "":
		return p, Errorf(CodeBadParams, "site is required: the Jira site, such as https://acme.atlassian.net")
	case p.Email == "":
		return p, Errorf(CodeBadParams, "email is required: the Jira account's sign-in email")
	case p.Token == "":
		return p, Errorf(CodeBadParams, "token is required: an Atlassian API token")
	case strings.ContainsAny(p.Token, " \t\r\n\x00"):
		return p, Errorf(CodeBadParams, "an API token is one word")
	case p.JQL == "":
		return p, Errorf(CodeBadParams, "jql is required: which tickets become jobs, such as project = FL")
	case len(p.JQL) > MaxJiraJQL:
		return p, Errorf(CodeBadParams, "jql is at most %d bytes", MaxJiraJQL)
	case strings.ContainsAny(p.Site+p.Email, " \t\r\n\x00"):
		return p, Errorf(CodeBadParams, "site and email are one word each")
	}
	return p, nil
}

func init() {
	const connect = "factory.jira_connect"
	methods[connect] = shaped[FactoryJiraConnectParams, FactoryJiraStatus](func(e Engine, raw json.RawMessage) (*FactoryJiraStatus, error) {
		j, err := jiraEngine(e, connect)
		if err != nil {
			return nil, err
		}
		var p FactoryJiraConnectParams
		// A strict decode's message quotes the bad input, which may be the
		// token itself, so this one says only what shape was wanted.
		if err := decodeStrict(connect, raw, &p); err != nil {
			return nil, Errorf(CodeBadParams, "%s takes {factory, site, email, token, jql}, all strings", connect)
		}
		if p, err = CheckJiraConnect(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := j.FactoryJiraConnect(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	for name, call := range map[string]func(FactoryJira, context.Context, string) (FactoryJiraStatus, error){
		"factory.jira_status":     FactoryJira.FactoryJiraStatus,
		"factory.jira_disconnect": FactoryJira.FactoryJiraDisconnect,
	} {
		methods[name] = typed(func(e Engine, p FactoryRef) (*FactoryJiraStatus, error) {
			j, err := jiraEngine(e, name)
			if err != nil {
				return nil, err
			}
			id, err := factoryID(strings.TrimSpace(p.Factory))
			if err != nil {
				return nil, err
			}
			ctx, cancel := factoryCall()
			defer cancel()
			out, err := call(j, ctx, id)
			if err != nil {
				return nil, err
			}
			return &out, nil
		})
	}
}

func jiraEngine(e Engine, name string) (FactoryJira, error) {
	j, ok := e.(FactoryJira)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return j, nil
}

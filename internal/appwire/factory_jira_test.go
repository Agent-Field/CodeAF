package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type jiraWireEngine struct {
	factoryEngine
	got []FactoryJiraConnectParams
}

func (j *jiraWireEngine) FactoryJiraConnect(_ context.Context, p FactoryJiraConnectParams) (FactoryJiraStatus, error) {
	j.got = append(j.got, p)
	return FactoryJiraStatus{Factory: p.Factory, Connected: true, Site: p.Site, JQL: p.JQL}, nil
}

func (j *jiraWireEngine) FactoryJiraStatus(_ context.Context, id string) (FactoryJiraStatus, error) {
	return FactoryJiraStatus{Factory: id}, nil
}

func (j *jiraWireEngine) FactoryJiraDisconnect(_ context.Context, id string) (FactoryJiraStatus, error) {
	return FactoryJiraStatus{Factory: id}, nil
}

type onlyJira struct{ fakeEngine }

func (*onlyJira) FactoryJiraConnect(context.Context, FactoryJiraConnectParams) (FactoryJiraStatus, error) {
	return FactoryJiraStatus{}, nil
}
func (*onlyJira) FactoryJiraStatus(context.Context, string) (FactoryJiraStatus, error) {
	return FactoryJiraStatus{}, nil
}
func (*onlyJira) FactoryJiraDisconnect(context.Context, string) (FactoryJiraStatus, error) {
	return FactoryJiraStatus{}, nil
}

// The three Jira methods are served and announced only by a factory engine
// that has them; a connect is bounded before the engine is asked, and no
// refusal ever quotes the token.
func TestFactoryJiraMethodsAreRegisteredAnnouncedAndBounded(t *testing.T) {
	names := []string{"factory.jira_connect", "factory.jira_status", "factory.jira_disconnect"}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyJira{}} {
		for _, name := range names {
			if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f"}`)); wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%T %s: %v", e, name, err)
			}
		}
		if h := withFactoryJiraFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &jiraWireEngine{}
	if h := withFactoryJiraFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJira {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods["factory.jira_connect"].call(e, json.RawMessage(`{"factory":" factory_local ","site":" acme.atlassian.net ","email":"a@b.c","token":" s3cret ","jql":" project = FL "}`))
	if err != nil {
		t.Fatal(err)
	}
	if st := out.(*FactoryJiraStatus); !st.Connected || st.Site != "acme.atlassian.net" || st.JQL != "project = FL" {
		t.Fatalf("answered %+v", st)
	}
	if len(e.got) != 1 || e.got[0].Factory != "factory_local" || e.got[0].Token != "s3cret" {
		t.Fatalf("handed %+v", e.got)
	}
	answer, _ := json.Marshal(out)
	if strings.Contains(string(answer), "s3cret") || strings.Contains(string(answer), "token") {
		t.Fatalf("the answer carries the token: %s", answer)
	}
	for _, bad := range []string{
		`{"factory":"f","site":"s","email":"e","token":"","jql":"q"}`,
		`{"factory":"f","site":"","email":"e","token":"t","jql":"q"}`,
		`{"factory":"f","site":"s","email":"","token":"t","jql":"q"}`,
		`{"factory":"f","site":"s","email":"e","token":"t","jql":""}`,
		`{"factory":"","site":"s","email":"e","token":"t","jql":"q"}`,
		`{"factory":"f","site":"s","email":"e","token":"two words-s3cret","jql":"q"}`,
		`{"factory":"f","site":"s","email":"e","token":"t","jql":"` + strings.Repeat("x", MaxJiraJQL+1) + `"}`,
		`{"factory":"f","site":"s","email":"e","token":"s3cret-leak","jql":"q","extra":1}`,
		`{"factory":"f","site":"s","email":"e","token":7,"jql":"q"}`,
	} {
		_, err := methods["factory.jira_connect"].call(e, json.RawMessage(bad))
		if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
		if err != nil && strings.Contains(err.Error(), "s3cret") {
			t.Errorf("a refusal quoted the token: %v", err)
		}
	}
	if len(e.got) != 1 {
		t.Fatalf("a refused connect reached the engine: %+v", e.got)
	}
	for _, name := range names[1:] {
		out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`))
		if err != nil || out.(*FactoryJiraStatus).Factory != "factory_local" {
			t.Fatalf("%s: %v %v", name, out, err)
		}
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":""}`)); wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s with no factory: %v", name, err)
		}
	}
}

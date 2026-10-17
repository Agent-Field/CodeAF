package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// localEngine is a factory engine that is also a FactoryLocal and keeps what
// each call was handed.
type localEngine struct {
	factoryEngine
	acts    []FactorySetupAct
	callers []string
}

func (l *localEngine) FactoryLocalStart(_ context.Context, c string) (FactoryLocalStatus, error) {
	l.callers = append(l.callers, c)
	return FactoryLocalStatus{State: LocalRunning, Authority: LocalAuthorityApp}, nil
}
func (l *localEngine) FactoryLocalStatus(_ context.Context, c string) (FactoryLocalStatus, error) {
	l.callers = append(l.callers, c)
	return FactoryLocalStatus{State: LocalStopped}, nil
}
func (l *localEngine) FactoryLocalRun(_ context.Context, c string) (FactoryLocalStatus, error) {
	l.callers = append(l.callers, c)
	return FactoryLocalStatus{State: LocalRunning, Runner: true}, nil
}
func (l *localEngine) FactoryLocalAdopt(_ context.Context, c string) (FactoryLocalStatus, error) {
	l.callers = append(l.callers, c)
	return FactoryLocalStatus{State: LocalStopped}, nil
}
func (l *localEngine) FactorySetupRead(_ context.Context, c string) (FactorySetupView, error) {
	l.callers = append(l.callers, c)
	return FactorySetupView{State: "github"}, nil
}
func (l *localEngine) FactorySetupAct(_ context.Context, c string, a FactorySetupAct) (FactorySetupView, error) {
	l.callers = append(l.callers, c)
	l.acts = append(l.acts, a)
	return FactorySetupView{State: "repos"}, nil
}

// A VALID REQUEST FOR EACH METHOD.
var factoryLocalGood = map[string]string{
	"factory.local_start":      `{"caller":"w1"}`,
	"factory.local_status":     `{"caller":"w1"}`,
	"factory.local_adopt":      `{"caller":"w1"}`,
	"factory.local_run":        `{"caller":"w1"}`,
	"factory.setup_read":       `{"caller":"w1"}`,
	"factory.setup_discover":   `{"caller":"w1"}`,
	"factory.setup_back":       `{"caller":"w1"}`,
	"factory.setup_done":       `{"caller":"w1"}`,
	"factory.setup_connect":    `{"caller":"w1","source":"gh"}`,
	"factory.setup_select":     `{"caller":"w1","repos":["acme/api"]}`,
	"factory.setup_retry_read": `{"caller":"w1","repo":"acme/api"}`,
	"factory.setup_skip":       `{"caller":"w1","repo":"acme/api"}`,
	"factory.setup_clone":      `{"caller":"w1","repo":"acme/api","folder":"~/code/api"}`,
	"factory.setup_use_folder": `{"caller":"w1","repo":"acme/api","folder":"/work/api"}`,
}

func TestEveryLocalFactoryMethodHasAValidRequestHere(t *testing.T) {
	for name := range factoryLocalMethods {
		if _, ok := factoryLocalGood[name]; !ok {
			t.Errorf("%s has no request in factoryLocalGood", name)
		}
	}
	for name := range factoryLocalGood {
		if _, ok := methods[name]; !ok {
			t.Errorf("%s is in factoryLocalGood and not registered", name)
		}
	}
}

// AN ENGINE WITHOUT FactoryLocal ANSWERS unknown_method AND DOES NOT ANNOUNCE
// IT, EVEN ONE THAT SERVES factory.* AND factory.job_*.
func TestLocalFactoryAbsentWithoutIt(t *testing.T) {
	for name, params := range factoryLocalGood {
		for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &actorEngine{}} {
			if _, err := methods[name].call(e, json.RawMessage(params)); wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%s on %T: %v", name, e, err)
			}
		}
	}
	if got := withFactoryLocalFeature(Hello{}, &actorEngine{}); len(got.Features) != 0 {
		t.Fatalf("a non-local engine announced %v", got.Features)
	}
	got := withFactoryLocalFeature(Hello{}, &localEngine{})
	if len(got.Features) != 1 || got.Features[0] != FeatureFactoryLocal {
		t.Fatalf("a local engine announced %v", got.Features)
	}
}

func TestLocalFactoryRefusesBadRequestsBeforeTheEngine(t *testing.T) {
	eng := &localEngine{}
	long := strings.Repeat("x", 129)
	bad := []struct{ name, params string }{
		// No caller, and one too long.
		{"factory.local_start", `{}`},
		{"factory.local_run", `{}`},
		{"factory.local_run", `{"caller":"w","job":3}`},
		{"factory.local_start", `{"caller":"  "}`},
		{"factory.setup_read", `{"caller":"` + long + `"}`},
		{"factory.setup_done", `{}`},
		// A field the wire does not carry is refused, not ignored.
		{"factory.local_start", `{"caller":"w","url":"http://x"}`},
		{"factory.setup_clone", `{"caller":"w","repo":"a/b","folder":"/x","command":"rm"}`},
		{"factory.setup_discover", `{"caller":"w","path":"/v1/queue"}`},
		// connect: source is closed, and a token goes with token only.
		{"factory.setup_connect", `{"caller":"w"}`},
		{"factory.setup_connect", `{"caller":"w","source":"password"}`},
		{"factory.setup_connect", `{"caller":"w","source":"token"}`},
		{"factory.setup_connect", `{"caller":"w","source":"token","token":"two words"}`},
		{"factory.setup_connect", `{"caller":"w","source":"gh","token":"ghp_x"}`},
		{"factory.setup_connect", `{"caller":"w","source":"token","token":"` + strings.Repeat("t", MaxSetupToken+1) + `"}`},
		// select: owner/name, 1 to 100.
		{"factory.setup_select", `{"caller":"w","repos":[]}`},
		{"factory.setup_select", `{"caller":"w","repos":["not-a-repo"]}`},
		{"factory.setup_select", `{"caller":"w","repos":["a/b","../etc"]}`},
		{"factory.setup_retry_read", `{"caller":"w","repo":"a/b/c"}`},
		{"factory.setup_skip", `{"caller":"w"}`},
		// folder: a full path or ~/, one line.
		{"factory.setup_clone", `{"caller":"w","repo":"a/b"}`},
		{"factory.setup_clone", `{"caller":"w","repo":"a/b","folder":"code/b"}`},
		{"factory.setup_clone", `{"caller":"w","repo":"a/b","folder":"~other/b"}`},
		{"factory.setup_use_folder", `{"caller":"w","repo":"a/b","folder":"/x\ny"}`},
	}
	for _, c := range bad {
		if _, err := methods[c.name].call(eng, json.RawMessage(c.params)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	tooMany := make([]string, MaxFactorySetupRepo+1)
	for i := range tooMany {
		tooMany[i] = "a/r" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	raw, _ := json.Marshal(FactorySetupSelectParams{Caller: "w", Repos: tooMany})
	if _, err := methods["factory.setup_select"].call(eng, raw); wireCode(t, err) != CodeBadParams {
		t.Errorf("101 repos: %v", err)
	}
	if len(eng.callers) != 0 || len(eng.acts) != 0 {
		t.Fatalf("the engine was asked: %v %v", eng.callers, eng.acts)
	}
}

func TestLocalFactoryPassesTrimmedClosedActs(t *testing.T) {
	eng := &localEngine{}
	call := func(name, params string) {
		t.Helper()
		if _, err := methods[name].call(eng, json.RawMessage(params)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	call("factory.setup_discover", `{"caller":" w1 "}`)
	call("factory.setup_connect", `{"caller":"w1","source":"token","token":" ghp_secret "}`)
	call("factory.setup_select", `{"caller":"w1","repos":[" acme/api ","acme/api","acme/web"]}`)
	call("factory.setup_clone", `{"caller":"w1","repo":"acme/api","folder":" ~/code/api "}`)
	call("factory.setup_use_folder", `{"caller":"w1","repo":"acme/web","folder":"/work/web"}`)
	call("factory.setup_skip", `{"caller":"w1","repo":"acme/web"}`)
	want := []FactorySetupAct{
		{Verb: "discover"},
		{Verb: "connect", Source: "token", Token: "ghp_secret"},
		{Verb: "select_repos", Repos: []string{"acme/api", "acme/web"}},
		{Verb: "clone", Repo: "acme/api", Folder: "~/code/api"},
		{Verb: "checkout", Repo: "acme/web", Folder: "/work/web"},
		{Verb: "skip", Repo: "acme/web"},
	}
	if !reflect.DeepEqual(eng.acts, want) {
		t.Fatalf("acts %+v, want %+v", eng.acts, want)
	}
	if eng.callers[0] != "w1" {
		t.Fatalf("caller %q", eng.callers[0])
	}
}

// NO RESULT TYPE CAN CARRY A TOKEN, A BEARER, OR A PRIVATE ROOT.
func TestLocalFactoryResultsHaveNoSecretField(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[FactoryLocalStatus](), reflect.TypeFor[FactoryLocalAttachment](), reflect.TypeFor[FactorySetupView](), reflect.TypeFor[FactorySetupGitHub](), reflect.TypeFor[FactorySetupRepo]()} {
		for i := 0; i < typ.NumField(); i++ {
			tag := strings.ToLower(typ.Field(i).Tag.Get("json"))
			for _, word := range []string{"token", "secret", "bearer", "credential", "path", "checkout", "root", "session"} {
				if strings.Contains(tag, word) {
					t.Errorf("%s.%s is %q", typ.Name(), typ.Field(i).Name, tag)
				}
			}
		}
	}
}

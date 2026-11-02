package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type repoReadEngine struct {
	factoryEngine
	got []FactoryRepoReadParams
}

func (e *repoReadEngine) FactoryRepoRead(_ context.Context, p FactoryRepoReadParams) (FactoryRepoRead, error) {
	e.got = append(e.got, p)
	return FactoryRepoRead{Factory: p.Factory, Repo: p.Repo, Op: p.Op, At: "main", Ref: "origin/main", Commit: strings.Repeat("a", 40),
		Path: "src", Untrusted: true, Entries: []api.RepoEntry{{Path: "src/a.go", Type: "file", Size: 12}}}, nil
}

type onlyRepoReader struct{ fakeEngine }

func (*onlyRepoReader) FactoryRepoRead(context.Context, FactoryRepoReadParams) (FactoryRepoRead, error) {
	return FactoryRepoRead{}, nil
}

func TestRepoReadIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.repo_read"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyRepoReader{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","repo":"acme/api","op":"tree"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryRepoReadFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &repoReadEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":" f ","repo":"acme/api","op":"tree","path":"src"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0] != (FactoryRepoReadParams{Factory: "f", Repo: "acme/api", Op: "tree", Path: "src"}) {
		t.Fatalf("handed %+v", e.got)
	}
	want := `{"factory":"f","repo":"acme/api","op":"tree","at":"main","ref":"origin/main","commit":"` + strings.Repeat("a", 40) +
		`","path":"src","untrusted":true,"truncated":false,"entries":[{"path":"src/a.go","type":"file","size":12}]}`
	if got, _ := json.Marshal(out); string(got) != want {
		t.Fatalf("answered %s", got)
	}
	if h := withFactoryRepoReadFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryRepoRead {
		t.Fatalf("announced %v", h.Features)
	}
	for _, ok := range []string{
		`{"factory":"f","repo":"acme/api","op":"grep","pattern":"retry","path":"src"}`,
		`{"factory":"f","repo":"acme/api","op":"read","path":"src/a.go","from":3,"to":40,"job":7}`,
	} {
		if _, err := methods[name].call(e, json.RawMessage(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		`{"factory":"","repo":"acme/api","op":"tree"}`,
		`{"factory":"f","repo":"acme","op":"tree"}`,
		`{"factory":"f","repo":"acme/api","op":"write"}`,
		`{"factory":"f","repo":"acme/api","op":"grep"}`,
		`{"factory":"f","repo":"acme/api","op":"read"}`,
		`{"factory":"f","repo":"acme/api","op":"read","path":"a","from":-1}`,
		`{"factory":"f","repo":"acme/api","op":"read","path":"a","from":9,"to":2}`,
		`{"factory":"f","repo":"acme/api","op":"tree","job":-2}`,
		`{"factory":"f","repo":"acme/api","op":"grep","pattern":"` + strings.Repeat("x", MaxRepoReadPattern+1) + `"}`,
		`{"factory":"f","repo":"acme/api","op":"tree","path":"` + strings.Repeat("x", MaxRepoReadPath+1) + `"}`,
		`{"factory":"f","repo":"acme/api","op":"tree","ref":"HEAD"}`,
	} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(e.got) != 3 {
		t.Fatalf("a refused call reached the engine: %d", len(e.got))
	}
}

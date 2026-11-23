package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type renameEngine struct {
	factoryEngine
	got [][2]string
}

func (r *renameEngine) FactoryRename(_ context.Context, id, name string) error {
	r.got = append(r.got, [2]string{id, name})
	return nil
}

type onlyRenamer struct{ fakeEngine }

func (*onlyRenamer) FactoryRename(context.Context, string, string) error { return nil }

func TestFactoryRenameIsRegisteredAnnouncedAndFoldsTheName(t *testing.T) {
	const name = "factory.rename"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyRenamer{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","name":"x"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryRenameFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &renameEngine{}
	if _, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local","name":"  Ada's   desk  "}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local","name":""}`)); err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 2 || e.got[0] != [2]string{"factory_local", "Ada's desk"} || e.got[1] != [2]string{"factory_local", ""} {
		t.Fatalf("handed %v", e.got)
	}
	if h := withFactoryRenameFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryRename {
		t.Fatalf("announced %v", h.Features)
	}
	long := strings.Repeat("é", MaxFactoryName+1)
	for _, bad := range []string{`{"factory":"","name":"x"}`, `{"factory":"f","name":"` + long + `"}`, `{"factory":"f","name":"x","url":"y"}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

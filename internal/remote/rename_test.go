package remote

import "testing"

type renameAgent struct{ *fakeAgent }

func (a *renameAgent) Rename(name string) error { a.title = name; return nil }

func TestAppRenameUsesCanonicalEngineWire(t *testing.T) {
	far := &renameAgent{&fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{Version: Version, Launch: &LaunchShape{DeterministicNames: true}}, Options{Boot: func(hello Hello) (*Engine, error) {
		if !hello.Launch.DeterministicNames {
			t.Fatal("hello lost app naming posture")
		}
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/transcript.jsonl", Launch: hello.Launch}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().Rename("Billing timeout"); err != nil {
		t.Fatal(err)
	}
	if far.title != "Billing timeout" {
		t.Fatal("rename did not reach engine")
	}
}

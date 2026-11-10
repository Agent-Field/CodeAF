package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestHostedOptionsCarryTheAppliedOneModelLaunch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	for _, shape := range []*remote.LaunchShape{nil, {}, {OneModel: true}} {
		loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
			Boot: func(remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{Agent: &quietAgent{}, Launch: shape}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loop.Close() })
		options, _ := hostOptions(onePipeFleet("devbox", loop.Client), loop.Client.Welcome(), false)
		want := shape != nil && shape.OneModel
		if options.OneModel != want {
			t.Errorf("hosted options OneModel = %t, applied launch wants %t", options.OneModel, want)
		}
	}
}

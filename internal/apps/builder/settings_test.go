package builder

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestBuilderModelUsesTheRegisteredSetting(t *testing.T) {
	for _, tc := range []struct {
		name, saved, pin, want string
	}{
		{name: "default", want: DefaultModel},
		{name: "saved", saved: "saved/model", want: "saved/model"},
		{name: "pinned", saved: "saved/model", pin: " pinned/model ", want: "pinned/model"},
		{name: "blank pin", saved: "saved/model", pin: " \t ", want: "saved/model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(config.ProfileDirEnv, dir)
			t.Setenv(ModelEnv, "")
			row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyAppsBuilderModel)
			if !ok {
				t.Fatal("the Builder's model setting is missing")
			}
			if err := row.Apply(tc.saved); err != nil {
				t.Fatal(err)
			}
			t.Setenv(ModelEnv, tc.pin)
			service := New(svc.Config{Root: t.TempDir()}, &svc.Services{})
			build := service.newBuild("example", bridge.CreateAppParams{Model: "agent/model"})
			if build.model != tc.want || build.agentModel != "agent/model" {
				t.Fatalf("Builder model = %q, agent model = %q; want %q and agent/model",
					build.model, build.agentModel, tc.want)
			}
		})
	}
}

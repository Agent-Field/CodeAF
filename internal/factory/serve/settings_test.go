package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"testing"
)

func TestFactoryRunnerPolicyRoutesValidateAndRequireOperator(t *testing.T) {
	f := peopleV2Setup(t, "team")
	f.request(t, "GET", "/v1/settings", "operator-secret", nil, 200, nil)
	seconds := 90
	f.request(t, "PATCH", "/v1/settings", "operator-secret", api.PatchFactorySettings{RunnerTimeoutSeconds: &seconds}, 200, nil)
	saved, err := f.st.RunnerSettings()
	if err != nil || saved.RunnerTimeoutSeconds != 90 {
		t.Fatal(saved, err)
	}
	zero := 0
	f.request(t, "PATCH", "/v1/settings", "operator-secret", api.PatchFactorySettings{RunnerTimeoutSeconds: &zero}, 422, nil)
	saved, _ = f.st.RunnerSettings()
	if saved.RunnerTimeoutSeconds != 90 || saved.KeepOutputDays != store.DefaultKeepOutputDays {
		t.Fatal("invalid write changed policy", saved)
	}
	f.request(t, "PATCH", "/v1/settings", "", api.PatchFactorySettings{RunnerTimeoutSeconds: &seconds}, 401, nil)
}

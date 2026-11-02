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

// READ NEW ISSUES AS THEY ARRIVE is on until the operator turns it off: the
// settings answer it on a factory that never saved it, a patch turns it off,
// and the other policy stays as it was.
func TestReadOnArrivalIsOnUntilTheOperatorTurnsItOff(t *testing.T) {
	f := peopleV2Setup(t, "team")
	var got api.FactorySettings
	f.request(t, "GET", "/v1/settings", "operator-secret", nil, 200, &got)
	if !got.ReadOnArrival {
		t.Fatal("read on arrival is off on a new factory")
	}
	off := false
	f.request(t, "PATCH", "/v1/settings", "operator-secret", api.PatchFactorySettings{ReadOnArrival: &off}, 200, &got)
	saved, err := f.st.RunnerSettings()
	if err != nil || saved.ReadOnArrival || got.ReadOnArrival || saved.RunnerTimeoutSeconds != store.DefaultRunnerTimeoutSeconds {
		t.Fatal(saved, got, err)
	}
}

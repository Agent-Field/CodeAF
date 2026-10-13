package roles

import (
	"errors"
	"testing"
)

func TestAppShipsWithoutEnabledFeatures(t *testing.T) {
	if got := AppRoles(); len(got) != 0 {
		t.Fatalf("unexpected production app roles: %v", got)
	}
}
func TestAppRoleResolutionUsesPinTierAndDefault(t *testing.T) {
	role := Role("app.registry-test")
	registryMu.Lock()
	registry[role] = TierLow
	registryMu.Unlock()
	defer func() { registryMu.Lock(); delete(registry, role); registryMu.Unlock() }()
	values := map[string]string{TierKey(TierLow): "cheap:low", TierKey(TierWorker): "work:high"}
	source := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	if call, err := ResolveAppCall(source, role, "", "default"); err != nil || call.Model != "cheap" || call.Effort != "low" {
		t.Fatalf("default tier: %+v %v", call, err)
	}
	if call, err := ResolveAppCall(source, role, TierWorker, "default"); err != nil || call.Model != "work" || call.Effort != "high" {
		t.Fatalf("override: %+v %v", call, err)
	}
	values[PinKey(role)] = "pinned"
	if call, err := ResolveAppCall(source, role, TierWorker, "default"); err != nil || call.Model != "pinned" {
		t.Fatalf("pin: %+v %v", call, err)
	}
	if call, err := ResolveAppCall(nil, role, "", "default"); err != nil || call.Model != "default" {
		t.Fatalf("floor: %+v %v", call, err)
	}
	if _, err := ResolveAppCall(nil, RoleTitle, "", "default"); !errors.Is(err, ErrUnknownRole) {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		role Role
		tier Tier
	}{{"app.", TierLow}, {"title", TierLow}, {"app.test bad", TierLow}, {"app.bad-tier", TierHigh}} {
		if err := RegisterApp(bad.role, bad.tier); err == nil {
			t.Fatal(bad)
		}
	}
}

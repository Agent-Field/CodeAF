package config

import "testing"

func TestEveryCrewSeatRouteAndProvidersOffIsAPreference(t *testing.T) {
	for _, key := range []string{KeyCrewRouteWorker, KeyCrewRoutePlanner, KeyCrewRouteChecker, KeyCrewProvidersOff, KeyTierWorkerModel, KeyTierHighModel, KeyTierLowModel, KeyCrewAllowed, KeyCrewCap, KeyCrewTaskCap, KeyCrewFreeRoutes} {
		if !PreferenceKey(key) {
			t.Fatalf("crew choice %s stays local", key)
		}
	}
}

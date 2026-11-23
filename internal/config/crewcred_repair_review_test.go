package config

import "testing"

// Repair may be the first operation after upgrading: opening Settings must not
// be required to seed an old-key baseline before a real key change can work.
func TestLegacyRefusalCanBeRepairedBeforeTheFirstHealthRead(t *testing.T) {
	for _, appKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "profile-key", true: "launch-key"}[appKey], func(t *testing.T) {
			dir := crewProfile(t)
			t.Setenv(ProfileDirEnv, dir)
			pinWorkerOnOpenRouter(t, dir)
			refuseAsAnOldBuild(t, dir, "auth")
			// Deliberately no health/model/admission read after the old failure.
			if appKey {
				keys := NewAppCredentials()
				t.Cleanup(UseAppCredentials(keys))
				keys.Set("openrouter", crewNewKey)
			} else if err := WriteAPIKey(dir, crewNewKey); err != nil {
				t.Fatal(err)
			}
			reopen()
			if !pinnedWorkerAdmits(t, dir) {
				t.Fatal("a real credential change before the first health read left the old legacy refusal stuck")
			}
		})
	}
}

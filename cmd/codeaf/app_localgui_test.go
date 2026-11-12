package main

import (
	"os"
	"runtime"
	"strconv"
	"testing"
)

func TestAppLocalGUIRequiresExactParentBoundNativeMarker(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows", "other"} {
		for _, raw := range []string{"", "1", "v1:", "v1:abc", "v1:123", "v1:0123", "v1:124", "v1:123\n"} {
			want := raw == "v1:123" && (platform == "darwin" || platform == "linux")
			if appLocalGUI(raw, 123, platform) != want {
				t.Fatalf("marker parsing changed for %q/%q", platform, raw)
			}
		}
	}
	if appLocalGUI("v1:0", 0, "darwin") {
		t.Fatal("zero parent was trusted")
	}
}

func TestAppLocalGUIMarkerIsConsumedAndHasNoLegacyFallback(t *testing.T) {
	raw := "v1:" + strconv.Itoa(os.Getppid())
	t.Setenv(appLocalGUIEnv, raw)
	t.Setenv("AFORGE_APP_LOCAL_GUI", raw) // legacy-name: rejected provenance alias
	want := runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	if consumeAppLocalGUI() != want {
		t.Fatal("native parent marker was not recognized")
	}
	if _, ok := os.LookupEnv(appLocalGUIEnv); ok {
		t.Fatal("marker could leak to a child tool")
	}
	if consumeAppLocalGUI() {
		t.Fatal("legacy alias or consumed marker was accepted")
	}
}

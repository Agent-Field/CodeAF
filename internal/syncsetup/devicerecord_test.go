package syncsetup

import (
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// A device's record names the system it runs on, so the device list can say so.
func TestDeviceRecordCarriesThePlatform(t *testing.T) {
	id, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := deviceRecord(id, "spark")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Platform != runtime.GOOS {
		t.Fatalf("platform = %q, want %q", rec.Platform, runtime.GOOS)
	}
}

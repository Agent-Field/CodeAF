package placeorg

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStateRoundTripsAndStaysReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "places-org.json")
	st := NewState()
	st.Space = SpacePrimary
	st.Thresholds[SpacePrimary] = Thresholds{Floor: 0.45, Margin: 0.04, Delta: 0.03}
	st.Axes["p1"] = Axis{Kind: AxisMonth}
	st.Kinds["c1"] = KindProbe
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("the file is %v, want 0600", info.Mode().Perm())
	}
	got, recovery, err := LoadState(path, nil)
	if err != nil || recovery != "" {
		t.Fatalf("load: %v %q", err, recovery)
	}
	if !reflect.DeepEqual(got, st) {
		t.Errorf("read back %+v, wrote %+v", got, st)
	}
	if left, _ := filepath.Glob(path + ".tmp-*"); len(left) != 0 {
		t.Errorf("a temp file was left behind: %v", left)
	}
}

func TestAMissingStateIsANewOrganiser(t *testing.T) {
	st, recovery, err := LoadState(filepath.Join(t.TempDir(), "none.json"), nil)
	if err != nil || recovery != "" || st.Version != StateVersion || st.Thresholds == nil || st.Kinds == nil {
		t.Fatalf("got %+v %q %v", st, recovery, err)
	}
}

func TestADamagedStateIsSetAsideNotDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places-org.json")
	os.WriteFile(path, []byte("{ not json"), 0o600)
	st, recovery, err := LoadState(path, fixedClock())
	if err != nil || st == nil || !strings.Contains(recovery, ".corrupt-") {
		t.Fatalf("got %v %q %v", st, recovery, err)
	}
	aside, _ := filepath.Glob(path + ".corrupt-*")
	if len(aside) != 1 {
		t.Fatalf("the damaged copy is not kept: %v", aside)
	}
	if raw, _ := os.ReadFile(aside[0]); string(raw) != "{ not json" {
		t.Error("the damaged copy was changed")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the damaged file is still in place")
	}
}

func TestAStateFromANewerBuildIsSetAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places-org.json")
	os.WriteFile(path, []byte(`{"version":99}`), 0o600)
	_, recovery, err := LoadState(path, fixedClock())
	if err != nil || !strings.Contains(recovery, "newer") {
		t.Fatalf("got %q %v", recovery, err)
	}
}

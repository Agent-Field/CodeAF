package lawcheck

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// UpdateFlag registers -update for the test binary that calls it, unless the
// package already has a flag of that name. It is a call, not an init, because
// some packages that import lawcheck define their own -update.
func UpdateFlag() bool {
	if flag.Lookup("update") == nil {
		flag.Bool("update", false, "rewrite golden fixtures from their samples")
	}
	return true
}

func updating() bool { return flag.Lookup("update").Value.String() == "true" }

// Golden freezes one persisted schema. sample is a fully populated value (every
// optional field set) of the type the build writes; path is its fixture. The
// fixture is the schema's contract in bytes, and three things must hold:
//
//   - the build still writes exactly the fixture (the write side is stable),
//   - decoding the fixture and encoding it again yields the same bytes (the
//     read side loses and reorders nothing),
//   - V is the first key (L11).
//
// With -update the fixture is rewritten from the sample; a diff in review is
// then a visible schema change.
func Golden(t testing.TB, path string, sample any) {
	t.Helper()
	written := mustMarshal(t, sample)
	if updating() {
		writeFixture(t, path, written)
		return
	}
	golden := bytes.TrimRight(readFixture(t, path), "\n")
	if !bytes.Equal(written, golden) {
		t.Errorf("%s: the build writes\n%s\nbut the fixture is\n%s\n(rerun with -update if the schema change is intended)", path, written, golden)
	}
	if again := roundTrip(t, sample, golden); !bytes.Equal(again, golden) {
		t.Errorf("%s: decode then encode changed the bytes:\n%s\n%s", path, golden, again)
	}
	if !VersionFirst(golden) {
		t.Errorf("%s: V is not the first key", path)
	}
}

// roundTrip decodes doc into a fresh value of sample's type and encodes it.
func roundTrip(t testing.TB, sample any, doc []byte) []byte {
	t.Helper()
	fresh := reflect.New(reflect.TypeOf(sample))
	if err := json.Unmarshal(doc, fresh.Interface()); err != nil {
		t.Fatalf("decode %T: %v", sample, err)
	}
	return mustMarshal(t, fresh.Elem().Interface())
}

func mustMarshal(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readFixture(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	return b
}

func writeFixture(t testing.TB, path string, doc []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(doc, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

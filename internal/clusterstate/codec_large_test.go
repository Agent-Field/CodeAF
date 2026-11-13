package clusterstate_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R14/R25 retain the established JSON spelling for large opaque Unicode,
// including escapes and map keys that cross bounded serialization pieces.
func TestR14LargeOpaqueJSONMatchesLegacyCanonicalBytes(t *testing.T) {
	text := strings.Repeat("<>&\\\"\n\t\u2028🙂界", 10000)
	for _, value := range []map[string]string{{"title": text, "opaque": "saved"}, {text: "key", "other": text}} {
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := cs.Encode(value)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("large canonical JSON spelling changed", err)
		}
		var decoded map[string]string
		if err = cs.Decode(want, &decoded); err != nil || !reflect.DeepEqual(decoded, value) {
			t.Fatal("large Unicode round trip changed fields", err)
		}
	}
}

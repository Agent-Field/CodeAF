package schema

import (
	"encoding/json"
	"testing"
)

func TestKitShipsVersionedCommonTypes(t *testing.T) {
	var kit struct {
		CommonTypes struct {
			Version int
			Types   map[string]struct {
				Fields map[string]struct {
					Type, Role, To string
				}
			}
		} `json:"common_types"`
	}
	if err := json.Unmarshal(KitJSON(), &kit); err != nil {
		t.Fatal(err)
	}
	lib := kit.CommonTypes
	if lib.Version != 1 || len(lib.Types) != 10 {
		t.Fatalf("want v1 with ten types, got v%d with %d", lib.Version, len(lib.Types))
	}
	for _, key := range []string{"person", "organization", "address", "money", "document", "product", "location", "team", "event", "task"} {
		fields := lib.Types[key].Fields
		if len(fields) == 0 {
			t.Fatalf("missing common type %s", key)
		}
		for name, f := range fields {
			if f.Role == "" || f.Type == "" {
				t.Fatalf("%s.%s needs a type and role", key, name)
			}
			if (f.Type == "ref" || f.Type == "refs") && lib.Types[f.To].Fields == nil {
				t.Fatalf("%s.%s refers to missing type %q", key, name, f.To)
			}
		}
	}
	if f := lib.Types["person"].Fields["organization"]; f.Type != "ref" || f.To != "organization" {
		t.Fatalf("person.organization = %+v", f)
	}
}

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

func TestOffersCommonTypeAndAlreadyBoundSpaceType(t *testing.T) {
	types := TypeSet{
		"contact": {Common: "person", Owner: "crm"},
		"company": {Common: "organization", Owner: "crm"},
	}
	for _, tc := range []struct {
		name, explicit, key, existing string
	}{
		{"customer", "", "person", "contact"},
		{"companies", "", "organization", "company"},
		{"price", "", "money", ""},
		{"meeting", "", "event", ""},
		{"sponsor", "organization", "organization", "company"},
	} {
		offer, ok := types.OfferCommonType(tc.name, RecordType{Common: tc.explicit})
		if !ok || offer.Key != tc.key || offer.Existing != tc.existing {
			t.Errorf("%s: got %+v (%t), want %s and %s", tc.name, offer, ok, tc.key, tc.existing)
		}
	}
	if offer, ok := types.OfferCommonType("contact_notes", RecordType{}); ok {
		t.Fatalf("an unrelated type got a common offer: %+v", offer)
	}
}

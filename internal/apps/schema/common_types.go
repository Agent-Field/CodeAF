package schema

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PrivateRecordName is the storage name for an imported type whose app
// alias also names a different type in the receiving profile.
func PrivateRecordName(app, alias string) string {
	id := sha256.Sum256([]byte(app))
	return fmt.Sprintf("pkg_%x_%s", id[:6], alias)
}

// CommonTypeLibrary is the versioned data vocabulary shipped in kit 2.
type CommonTypeLibrary struct {
	Version int                   `json:"version"`
	Types   map[string]CommonType `json:"types"`
}

type CommonType struct {
	Label  string                 `json:"label"`
	Terms  []string               `json:"terms"`
	Fields map[string]CommonField `json:"fields"`
}

type CommonField struct {
	Type string `json:"type"`
	Role string `json:"role"`
	To   string `json:"to,omitempty"`
}

// CommonTypes reads the same library the kit sends to desk and the builder.
// Return a detached copy so clients can add presentation data of their own.
func CommonTypes() CommonTypeLibrary {
	lib := schemas().kits[2].CommonTypes
	out := CommonTypeLibrary{Version: lib.Version, Types: make(map[string]CommonType, len(lib.Types))}
	for key, typ := range lib.Types {
		typ.Fields = maps.Clone(typ.Fields)
		typ.Terms = slices.Clone(typ.Terms)
		out.Types[key] = typ
	}
	return out
}

// HasCommonType checks the portable key independently of any space's records.
func HasCommonType(key string) bool {
	_, ok := schemas().kits[2].CommonTypes.Types[key]
	return ok
}

// ForRecord resolves a portable common key before falling back to a local
// name. Explicit reuse must name a bound type, so a spelling match cannot
// accidentally give an app access to a different kind of record.
func (types TypeSet) ForRecord(name string, record RecordType) (ExistingType, bool) {
	if record.Common != "" {
		if _, typ, ok := types.boundCommonType(record.Common); ok {
			return typ, true
		}
		if record.Uses {
			return ExistingType{}, false
		}
	}
	typ, ok := types[name]
	return typ, ok
}

// CommonTypeOffer carries the portable key and the space's existing type
// name, when the person has already bound one.
type CommonTypeOffer struct {
	Key      string
	Existing string
}

// OfferCommonType recognizes a proposed record type through the catalog's
// vocabulary. Only a writer-backed binding qualifies as existing data.
func (types TypeSet) OfferCommonType(name string, record RecordType) (CommonTypeOffer, bool) {
	lib := schemas().kits[2].CommonTypes
	key := record.Common
	if key == "" {
		for _, candidate := range slices.Sorted(maps.Keys(lib.Types)) {
			if matchesTypeWord(name, candidate) || slices.ContainsFunc(lib.Types[candidate].Terms, func(term string) bool {
				return matchesTypeWord(name, term)
			}) {
				key = candidate
				break
			}
		}
	}
	if _, ok := lib.Types[key]; !ok {
		return CommonTypeOffer{}, false
	}
	existing, _, _ := types.boundCommonType(key)
	return CommonTypeOffer{Key: key, Existing: existing}, true
}

func (types TypeSet) boundCommonType(key string) (string, ExistingType, bool) {
	for name, typ := range types {
		if typ.Common == key {
			return name, typ, true
		}
	}
	return "", ExistingType{}, false
}

func matchesTypeWord(name, term string) bool {
	name = strings.ToLower(strings.NewReplacer("_", " ", "-", " ").Replace(name))
	term = strings.ReplaceAll(term, "_", " ")
	return name == term || name == term+"s" || name == term+"es" ||
		strings.HasSuffix(term, "y") && name == strings.TrimSuffix(term, "y")+"ies"
}

package schema

import (
	"maps"
	"slices"
)

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
		for _, typ := range types {
			if typ.Common == record.Common {
				return typ, true
			}
		}
		if record.Uses {
			return ExistingType{}, false
		}
	}
	typ, ok := types[name]
	return typ, ok
}

package appwire

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type BindCommonTypeParams struct {
	Common string `json:"common"`
	Type   string `json:"type"`
}

type BindCommonTypeResult struct {
	Common string `json:"common"`
	Type   string `json:"type"`
	TypeID string `json:"type_id"`
}

type commonTypeBinder interface {
	BindCommonAppType(BindCommonTypeParams) (*BindCommonTypeResult, error)
}

func init() {
	methods["apps.common_types"] = typed(func(_ Engine, _ Empty) (*schema.CommonTypeLibrary, error) {
		library := schema.CommonTypes()
		return &library, nil
	})
	methods["apps.bind_common_type"] = typed(func(e Engine, p BindCommonTypeParams) (*BindCommonTypeResult, error) {
		binder, ok := e.(commonTypeBinder)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not bind common types.")
		}
		if !schema.HasCommonType(p.Common) || p.Type == "" {
			return nil, Errorf(CodeBadParams, "Choose a common type key and an existing record type.")
		}
		return binder.BindCommonAppType(p)
	})
}

// RunCommonTypeCommand gives the local CLI the app wire's argument decoder,
// validation, and result shape.
func RunCommonTypeCommand(e Engine, verb string, raw json.RawMessage) (any, error) {
	switch verb {
	case "common_types", "bind_common_type":
		return methods["apps."+verb].call(e, raw)
	default:
		return nil, Errorf(CodeUnknownMethod, "Unknown common type command %q.", verb)
	}
}

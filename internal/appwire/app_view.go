package appwire

import "encoding/json"

// DataViewParams addresses a declared screen query. A caller sends the typed
// arguments rather than a new filter, projection, or record type.
type DataViewParams struct {
	DataScope
	Screen string                     `json:"screen"`
	View   string                     `json:"view"`
	Args   map[string]json.RawMessage `json:"args,omitempty"`
	Cursor string                     `json:"cursor,omitempty"`
	Limit  int                        `json:"limit,omitempty"`
}

type AppViewReader interface {
	DataViewQuery(DataViewParams) (*DataQueryResult, error)
}

func init() {
	methods["data.view.query"] = typed(func(e Engine, p DataViewParams) (*DataQueryResult, error) {
		reader, ok := e.(AppViewReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve declared view queries.")
		}
		return reader.DataViewQuery(p)
	})
}

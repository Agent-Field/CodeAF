package appwire

import (
	"reflect"
	"strings"
)

// FLUID APPS: THE PERSON'S DATA. Every record of every type
// any app made lives in one store per person, and these methods are its
// door: the Data place reads and writes it with no app (the person may do
// everything), and a screen, the app's agent or a run calls with its app,
// which the engine checks against that app's capabilities before anything
// else. A draft names a Builder draft's sample file, with the same methods.
//
// A ROW IS A FLAT OBJECT: its id, its field values by name, created_at,
// updated_at, type, and "_meta" (which values are masked, which came from AI,
// whether a freeze rule holds it). A refusal for a capability is not_allowed
// with detail {need: "records.<type>.rw"}; a refused value is bad_params with
// detail {field}.

// EventDataChanged is a live query's row diff after a committed write.
const EventDataChanged = "data.changed"

// FeatureData is hello's word for an engine serving the data.* methods.
const FeatureData = "data"

// The codes the data methods add to the wire's.
const (
	// CodeNotAllowed is a call an app's capabilities do not cover; its detail
	// names the capability it would take.
	CodeNotAllowed = "not_allowed"
	// CodeFrozen is a write to a record a freeze rule holds.
	CodeFrozen = "frozen"
	// CodeDuplicate is a value a unique field already holds elsewhere.
	CodeDuplicate = "duplicate"
)

// DataRow is one record as the wire carries it.
type DataRow = map[string]any

// DataScope is what every data call may carry: the app it is made for (none:
// the person in the Data place) and the draft whose sample it reads.
type DataScope struct {
	App   string `json:"app,omitempty"`
	Draft string `json:"draft,omitempty"`
}

// The params of the data methods.
type (
	DataQueryParams struct {
		DataScope
		Type   string `json:"type"`
		Query  string `json:"query,omitempty"`
		Cursor string `json:"cursor,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	DataCountParams struct {
		DataScope
		Type  string `json:"type"`
		Query string `json:"query,omitempty"`
		Group string `json:"group,omitempty"`
	}
	DataRecordParams struct {
		DataScope
		Type   string `json:"type"`
		ID     string `json:"id"`
		Reveal bool   `json:"reveal,omitempty"`
	}
	DataCreateParams struct {
		DataScope
		Type string           `json:"type"`
		Set  map[string]any   `json:"set,omitempty"`
		Rows []map[string]any `json:"rows,omitempty"`
	}
	DataUpdateParams struct {
		DataScope
		Type     string         `json:"type"`
		IDs      []string       `json:"ids"`
		Set      map[string]any `json:"set"`
		Unfreeze string         `json:"unfreeze,omitempty"`
	}
	DataDeleteParams struct {
		DataScope
		Type     string   `json:"type"`
		IDs      []string `json:"ids"`
		Unfreeze string   `json:"unfreeze,omitempty"`
	}
	DataUndoParams struct {
		DataScope
		Batch string `json:"batch"`
	}
	DataImportParams struct {
		DataScope
		Type    string            `json:"type"`
		Path    string            `json:"path"`
		Mapping map[string]string `json:"mapping,omitempty"`
		DryRun  bool              `json:"dry_run,omitempty"`
	}
	DataSubscribeParams struct {
		DataScope
		Sub   string `json:"sub"`
		Type  string `json:"type"`
		Query string `json:"query,omitempty"`
		ID    string `json:"id,omitempty"`
	}
	DataUnsubscribeParams struct {
		Sub string `json:"sub"`
	}
)

// The answers of the data methods.
type (
	DataTypesResult struct {
		Types []DataType `json:"types"`
	}
	DataType struct {
		Name     string      `json:"name"`
		Title    string      `json:"title,omitempty"`
		Label    string      `json:"label,omitempty"`
		OwnerApp string      `json:"owner_app"`
		UsedBy   []string    `json:"used_by"`
		Count    int         `json:"count"`
		Hidden   bool        `json:"hidden,omitempty"`
		Fields   []DataField `json:"fields"`
	}
	DataField struct {
		Name     string   `json:"name"`
		Type     string   `json:"type"`
		App      string   `json:"app"`
		Hidden   bool     `json:"hidden"`
		Label    string   `json:"label,omitempty"`
		Required bool     `json:"required,omitempty"`
		Unique   bool     `json:"unique,omitempty"`
		Masked   bool     `json:"masked,omitempty"`
		Values   []string `json:"values,omitempty"`
		To       string   `json:"to,omitempty"`
		Currency string   `json:"currency,omitempty"`
		Computed bool     `json:"computed,omitempty"`
		AI       bool     `json:"ai,omitempty"`
	}
	DataQueryResult struct {
		Rows  []DataRow `json:"rows"`
		Next  string    `json:"next,omitempty"`
		Total int       `json:"total"`
	}
	DataCountResult struct {
		Groups []DataGroup `json:"groups"`
		Total  int         `json:"total"`
	}
	DataGroup struct {
		Key   any `json:"key"`
		Count int `json:"count"`
	}
	DataRowResult struct {
		Row DataRow `json:"row"`
	}
	DataHistoryResult struct {
		History []DataHistoryEntry `json:"history"`
	}
	DataHistoryEntry struct {
		Seq     int64          `json:"seq"`
		Batch   string         `json:"batch"`
		Op      string         `json:"op"`
		App     string         `json:"app,omitempty"`
		Actor   string         `json:"actor"`
		Note    string         `json:"note,omitempty"`
		At      string         `json:"at"`
		Undone  bool           `json:"undone,omitempty"`
		Before  map[string]any `json:"before"`
		After   map[string]any `json:"after"`
		Changed []string       `json:"changed"`
	}
	DataWriteResult struct {
		Batch   string    `json:"batch"`
		Rows    []DataRow `json:"rows"`
		Removes []string  `json:"removes,omitempty"`
	}
	DataImportResult struct {
		// Preview is a dry run's account; Created and Batch a real import's.
		Preview  *DataImportPreview `json:"preview,omitempty"`
		Created  *int               `json:"created,omitempty"`
		Batch    string             `json:"batch,omitempty"`
		Failed   int                `json:"failed,omitempty"`
		Problems []DataImportIssue  `json:"problems,omitempty"`
	}
	DataImportPreview struct {
		Columns  []string          `json:"columns"`
		Mapping  map[string]string `json:"mapping"`
		Rows     []DataRow         `json:"rows"`
		Valid    int               `json:"valid"`
		Failed   int               `json:"failed"`
		Problems []DataImportIssue `json:"problems"`
	}
	DataImportIssue struct {
		Line    int    `json:"line"`
		Field   string `json:"field,omitempty"`
		Message string `json:"message"`
	}
	// DataChanged is a live query's diff: rows that now match (placed by the
	// client's own order), ids that no longer do, and the new total.
	DataChanged struct {
		Sub     string    `json:"sub"`
		Upserts []DataRow `json:"upserts"`
		Removes []string  `json:"removes"`
		Total   int       `json:"total"`
	}
)

// Dataer is the engine's half of the person's data.
type Dataer interface {
	DataTypes(DataScope) (*DataTypesResult, error)
	DataQuery(DataQueryParams) (*DataQueryResult, error)
	DataCount(DataCountParams) (*DataCountResult, error)
	DataGet(DataRecordParams) (*DataRowResult, error)
	DataHistory(DataRecordParams) (*DataHistoryResult, error)
	DataCreate(DataCreateParams) (*DataWriteResult, error)
	DataUpdate(DataUpdateParams) (*DataWriteResult, error)
	DataDelete(DataDeleteParams) (*DataWriteResult, error)
	DataUndo(DataUndoParams) (*DataWriteResult, error)
	DataImportCSV(DataImportParams) (*DataImportResult, error)
	DataSubscribe(DataSubscribeParams) error
	DataUnsubscribe(sub string) error
}

func init() {
	eventShapes[EventDataChanged] = reflect.TypeFor[DataChanged]()
	for name, m := range dataMethods {
		methods[name] = m
	}
}

// withDataFeature is hello carrying [FeatureData] for an engine with the
// door.
func withDataFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Dataer); ok {
		return withFeature(hello, FeatureData)
	}
	return hello
}

func dataer(e Engine, name string) (Dataer, error) {
	d, ok := e.(Dataer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return d, nil
}

// dataMethod is one data method: the engine's door, then fn.
func dataMethod[P, R any](name string, fn func(Dataer, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		d, err := dataer(e, name)
		if err != nil {
			return nil, err
		}
		return fn(d, p)
	})
}

// needType is the type every typed data call names.
func needType(typ string) error {
	if strings.TrimSpace(typ) == "" {
		return Errorf(CodeBadParams, "type is required")
	}
	return nil
}

var dataMethods = map[string]method{
	"data.types": dataMethod("data.types", func(d Dataer, p DataScope) (*DataTypesResult, error) {
		return d.DataTypes(p)
	}),
	"data.query": dataMethod("data.query", func(d Dataer, p DataQueryParams) (*DataQueryResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		return d.DataQuery(p)
	}),
	"data.count": dataMethod("data.count", func(d Dataer, p DataCountParams) (*DataCountResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		return d.DataCount(p)
	}),
	"data.get": dataMethod("data.get", func(d Dataer, p DataRecordParams) (*DataRowResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		return d.DataGet(p)
	}),
	"data.history": dataMethod("data.history", func(d Dataer, p DataRecordParams) (*DataHistoryResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		return d.DataHistory(p)
	}),
	"data.create": dataMethod("data.create", func(d Dataer, p DataCreateParams) (*DataWriteResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if (p.Set == nil) == (len(p.Rows) == 0) {
			return nil, Errorf(CodeBadParams, "data.create takes set (one record) or rows (several), one of them")
		}
		return d.DataCreate(p)
	}),
	"data.update": dataMethod("data.update", func(d Dataer, p DataUpdateParams) (*DataWriteResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if len(p.IDs) == 0 || len(p.Set) == 0 {
			return nil, Errorf(CodeBadParams, "data.update needs ids and set")
		}
		return d.DataUpdate(p)
	}),
	"data.delete": dataMethod("data.delete", func(d Dataer, p DataDeleteParams) (*DataWriteResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if len(p.IDs) == 0 {
			return nil, Errorf(CodeBadParams, "ids is required")
		}
		return d.DataDelete(p)
	}),
	"data.undo": dataMethod("data.undo", func(d Dataer, p DataUndoParams) (*DataWriteResult, error) {
		if _, err := required("batch", p.Batch); err != nil {
			return nil, err
		}
		return d.DataUndo(p)
	}),
	"data.import_csv": dataMethod("data.import_csv", func(d Dataer, p DataImportParams) (*DataImportResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if _, err := required("path", p.Path); err != nil {
			return nil, err
		}
		return d.DataImportCSV(p)
	}),
	"data.subscribe": dataMethod("data.subscribe", func(d Dataer, p DataSubscribeParams) (*Empty, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if _, err := required("sub", p.Sub); err != nil {
			return nil, err
		}
		if p.ID != "" && p.Query != "" {
			return nil, Errorf(CodeBadParams, "data.subscribe watches a query or one id, not both")
		}
		return done(d.DataSubscribe(p))
	}),
	"data.unsubscribe": dataMethod("data.unsubscribe", func(d Dataer, p DataUnsubscribeParams) (*Empty, error) {
		if _, err := required("sub", p.Sub); err != nil {
			return nil, err
		}
		return done(d.DataUnsubscribe(p.Sub))
	}),
}

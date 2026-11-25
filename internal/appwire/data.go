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
// updated_at, type, _rev (its revision, which a write may send back to refuse
// a stale edit), and "_meta" (which values are masked, who set each value,
// whether a freeze rule holds it). A refusal for a capability is not_allowed
// with detail {need: "records.<type>.rw"}; a refused value is bad_params with
// detail {field}.

// EventDataChanged is a live query's row diff after a committed write.
const EventDataChanged = "data.changed"

// EventDataReset tells a live query that what changed since its cursor cannot
// be sent as a diff: the client refetches, then carries on from the cursor.
const EventDataReset = "data.reset"

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
	// CodeLifecycle is a raw write to a lifecycle's state, or a transition
	// that cannot fire now (wrong state, guard not holding).
	CodeLifecycle = "lifecycle"
	// CodeLedger is a change, deletion or undo of a ledger entry; ledgers are
	// only reversed.
	CodeLedger = "ledger"
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
		Type   string     `json:"type"`
		Query  string     `json:"query,omitempty"`
		Where  []DataCond `json:"where,omitempty"`
		Sort   string     `json:"sort,omitempty"`
		Select []string   `json:"select,omitempty"`
		Cursor string     `json:"cursor,omitempty"`
		Limit  int        `json:"limit,omitempty"`
	}
	// DataCond is one condition of the query IR: field (or link.field) op
	// value, or free words with no field. Op is : > >= < <= is has.
	DataCond struct {
		Field string `json:"field,omitempty"`
		Op    string `json:"op,omitempty"`
		Value string `json:"value,omitempty"`
		Neg   bool   `json:"neg,omitempty"`
	}
	// DataAggregateParams asks for server aggregates over a query, grouped by
	// up to two fields (a link's field included).
	DataAggregateParams struct {
		DataScope
		Type      string     `json:"type"`
		Query     string     `json:"query,omitempty"`
		Where     []DataCond `json:"where,omitempty"`
		Group     []string   `json:"group,omitempty"`
		Aggregate []DataAgg  `json:"aggregate"`
	}
	DataAgg struct {
		Fn    string `json:"fn"`
		Field string `json:"field,omitempty"`
		As    string `json:"as,omitempty"`
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
		// Rev is the revision the caller saw (one id only); Reset hands
		// fields the person set back to AI.
		Rev   int64    `json:"rev,omitempty"`
		Reset []string `json:"reset,omitempty"`
	}
	DataDeleteParams struct {
		DataScope
		Type     string   `json:"type"`
		IDs      []string `json:"ids"`
		Unfreeze string   `json:"unfreeze,omitempty"`
		Rev      int64    `json:"rev,omitempty"`
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
		Type  string `json:"type,omitempty"`
		Query string `json:"query,omitempty"`
		ID    string `json:"id,omitempty"`
		// Table watches a work table (work_items, timers, signoffs,
		// comments) instead of a type; with type and id, one record's.
		Table string `json:"table,omitempty"`
		// Cursor resumes from the last cursor the client saw: one diff of
		// everything since, or a data.reset.
		Cursor *int64 `json:"cursor,omitempty"`
		// InvalidateOnly asks for a reset when a derived view must refetch.
		InvalidateOnly bool `json:"invalidate_only,omitempty"`
		DeferEvents    bool `json:"-"`
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
		Common string `json:"common,omitempty"`
		ID     string `json:"id"`
		Name   string `json:"name"`
		Title  string `json:"title,omitempty"`
		Label  string `json:"label,omitempty"`
		// CreatedByApp is where the type came from, only: a type is the
		// person's, and deleting that app never touches it.
		CreatedByApp string         `json:"created_by_app"`
		DefinedBy    string         `json:"defined_by,omitempty"`
		UsedBy       []string       `json:"used_by"`
		Count        int            `json:"count"`
		Hidden       bool           `json:"hidden,omitempty"`
		Ledger       bool           `json:"ledger,omitempty"`
		Lifecycle    *DataLifecycle `json:"lifecycle,omitempty"`
		Fields       []DataField    `json:"fields"`
	}
	DataLifecycle struct {
		Field       string           `json:"field"`
		Initial     string           `json:"initial"`
		Transitions []DataTransition `json:"transitions"`
	}
	DataTransition struct {
		ID      string           `json:"id"`
		Label   string           `json:"label,omitempty"`
		From    []string         `json:"from,omitempty"`
		To      string           `json:"to"`
		Guard   string           `json:"guard,omitempty"`
		By      []string         `json:"by"`
		Effects []map[string]any `json:"effects,omitempty"`
	}
	DataField struct {
		ID       string   `json:"id"`
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
		// Sequence numbers new records: prefix then a zero-padded counter.
		Sequence *DataSequence `json:"sequence,omitempty"`
	}
	DataSequence struct {
		Prefix string `json:"prefix,omitempty"`
		Pad    int    `json:"pad,omitempty"`
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
		ByKind  string         `json:"by_kind"`
		Cause   string         `json:"cause,omitempty"`
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
		Row     DataRow   `json:"row,omitempty"`
		Removes []string  `json:"removes,omitempty"`
		// Held is the values the write left alone because the person set
		// them (an AI write below a person's value).
		Held []DataHeld `json:"held,omitempty"`
	}
	DataHeld struct {
		ID    string `json:"id"`
		Field string `json:"field"`
	}
	DataAggregateResult struct {
		Group  []string        `json:"group"`
		Groups []DataAggregate `json:"groups"`
		Total  int             `json:"total"`
	}
	DataAggregate struct {
		Key    []any          `json:"key"`
		Values map[string]any `json:"values"`
	}
	DataSubscribeResult struct {
		Cursor        int64            `json:"cursor"`
		Snapshot      *DataQueryResult `json:"snapshot,omitempty"`
		ReleaseEvents func()           `json:"-"`
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
		Cursor  int64     `json:"cursor"`
	}
	// DataReset tells a live query to refetch, then carry on from Cursor.
	DataReset struct {
		Sub    string `json:"sub"`
		Cursor int64  `json:"cursor"`
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
	DataAggregate(DataAggregateParams) (*DataAggregateResult, error)
	DataSubscribe(DataSubscribeParams) (*DataSubscribeResult, error)
	DataUnsubscribe(sub string) error
	DataProcess
}

func init() {
	eventShapes[EventDataChanged] = reflect.TypeFor[DataChanged]()
	eventShapes[EventDataReset] = reflect.TypeFor[DataReset]()
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
		if len(p.IDs) == 0 || len(p.Set) == 0 && len(p.Reset) == 0 {
			return nil, Errorf(CodeBadParams, "data.update needs ids, and set or reset")
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
	"data.aggregate": dataMethod("data.aggregate", func(d Dataer, p DataAggregateParams) (*DataAggregateResult, error) {
		if err := needType(p.Type); err != nil {
			return nil, err
		}
		if len(p.Aggregate) == 0 {
			return nil, Errorf(CodeBadParams, "aggregate is required")
		}
		return d.DataAggregate(p)
	}),
	"data.subscribe": dataMethod("data.subscribe", func(d Dataer, p DataSubscribeParams) (*DataSubscribeResult, error) {
		if p.Table == "" {
			if err := needType(p.Type); err != nil {
				return nil, err
			}
		}
		if _, err := required("sub", p.Sub); err != nil {
			return nil, err
		}
		if p.ID != "" && p.Query != "" {
			return nil, Errorf(CodeBadParams, "data.subscribe watches a query or one id, not both")
		}
		p.DeferEvents = true
		return d.DataSubscribe(p)
	}),
	"data.unsubscribe": dataMethod("data.unsubscribe", func(d Dataer, p DataUnsubscribeParams) (*Empty, error) {
		if _, err := required("sub", p.Sub); err != nil {
			return nil, err
		}
		return done(d.DataUnsubscribe(p.Sub))
	}),
}

package appwire

// THE PROCESS HALF OF THE DATA DOOR: what moves work, not just values. A
// type's lifecycle moves only by data.transition; a ledger entry changes
// only by data.reverse; work items, timers, sign-offs and comments are the
// space's own tables, each with list and write methods and live queries
// (data.subscribe with table). Every call takes the optional app and draft of
// [DataScope], checked like the rest of the door.
//
// Work items, timers, sign-offs, comments and activity lines travel as flat
// objects (DataRow), the same as records: their fields by name.

// The params of the process methods.
type (
	DataTransitionParams struct {
		DataScope
		Type       string `json:"type"`
		ID         string `json:"id"`
		Transition string `json:"transition"`
		// Record names the record as {type, id}, the same as type and id.
		Record *DataRecordRef `json:"record,omitempty"`
		// Rev is the revision the caller saw (the row's _rev; expected_rev
		// is the same); a record that moved since is refused as stale.
		Rev         int64            `json:"rev,omitempty"`
		ExpectedRev ExpectedRevision `json:"expected_rev,omitempty"`
		Reason      string           `json:"reason,omitempty"`
		Unfreeze    string           `json:"unfreeze,omitempty"`
	}
	DataReverseParams struct {
		DataScope
		Type   string `json:"type"`
		ID     string `json:"id"`
		Reason string `json:"reason,omitempty"`
	}
	DataWorkCreateParams struct {
		DataScope
		Title    string `json:"title"`
		Type     string `json:"type,omitempty"`
		Record   string `json:"record,omitempty"`
		Assignee string `json:"assignee,omitempty"`
		Due      string `json:"due,omitempty"`
		SLAAt    string `json:"sla_at,omitempty"`
		Form     string `json:"form,omitempty"`
	}
	DataWorkUpdateParams struct {
		DataScope
		ID       string  `json:"id"`
		Title    *string `json:"title,omitempty"`
		Assignee *string `json:"assignee,omitempty"`
		Due      *string `json:"due,omitempty"`
		SLAAt    *string `json:"sla_at,omitempty"`
		Form     *string `json:"form,omitempty"`
		Status   *string `json:"status,omitempty"`
		Outcome  any     `json:"outcome,omitempty"`
	}
	DataWorkListParams struct {
		DataScope
		Assignee string `json:"assignee,omitempty"`
		Status   string `json:"status,omitempty"`
		Type     string `json:"type,omitempty"`
		Record   string `json:"record,omitempty"`
	}
	DataTimerCreateParams struct {
		DataScope
		Type    string `json:"type,omitempty"`
		Record  string `json:"record,omitempty"`
		FireAt  string `json:"fire_at"`
		Kind    string `json:"kind"`
		Payload any    `json:"payload,omitempty"`
		Every   string `json:"every,omitempty"`
	}
	DataTimerListParams struct {
		DataScope
		Record  string `json:"record,omitempty"`
		Pending bool   `json:"pending,omitempty"`
	}
	DataIDParams struct {
		DataScope
		ID string `json:"id"`
	}
	DataSignParams struct {
		DataScope
		Type      string `json:"type"`
		ID        string `json:"id"`
		Statement string `json:"statement"`
		Rev       int64  `json:"rev,omitempty"`
	}
	DataCommentParams struct {
		DataScope
		Type     string   `json:"type,omitempty"`
		Record   string   `json:"record,omitempty"`
		Node     string   `json:"node,omitempty"`
		Body     string   `json:"body"`
		Mentions []string `json:"mentions,omitempty"`
	}
	DataCommentListParams struct {
		DataScope
		Type   string `json:"type,omitempty"`
		Record string `json:"record,omitempty"`
		Node   string `json:"node,omitempty"`
	}
)

// DataRecordRef names one record.
type DataRecordRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// The answers of the process methods.
type (
	DataTransitionsResult struct {
		Transitions []DataTransitionState `json:"transitions"`
	}
	// DataTransitionState is a transition the record may take from its
	// state: ready when its guard holds now, allowed when the caller may
	// fire it.
	DataTransitionState struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		To      string `json:"to"`
		Ready   bool   `json:"ready"`
		Allowed bool   `json:"allowed"`
	}
	DataItemResult struct {
		Item DataRow `json:"item"`
	}
	DataItemsResult struct {
		Items []DataRow `json:"items"`
		// Viewer is the current person when listing comments.
		Viewer string `json:"viewer,omitempty"`
	}
)

// DataProcess is the engine's half of the process methods.
type DataProcess interface {
	DataTransition(DataTransitionParams) (*DataWriteResult, error)
	DataTransitions(DataRecordParams) (*DataTransitionsResult, error)
	DataReverse(DataReverseParams) (*DataWriteResult, error)
	DataWorkCreate(DataWorkCreateParams) (*DataItemResult, error)
	DataWorkUpdate(DataWorkUpdateParams) (*DataItemResult, error)
	DataWorkList(DataWorkListParams) (*DataItemsResult, error)
	DataTimerCreate(DataTimerCreateParams) (*DataItemResult, error)
	DataTimerCancel(DataIDParams) (*Empty, error)
	DataTimerList(DataTimerListParams) (*DataItemsResult, error)
	DataSign(DataSignParams) (*DataItemResult, error)
	DataSignoffs(DataRecordParams) (*DataItemsResult, error)
	DataCommentAdd(DataCommentParams) (*DataItemResult, error)
	DataCommentList(DataCommentListParams) (*DataItemsResult, error)
	DataActivity(DataRecordParams) (*DataItemsResult, error)
}

// needRecord checks the type and id every per-record call names.
func needRecord(typ, id string) error {
	if err := needType(typ); err != nil {
		return err
	}
	_, err := required("id", id)
	return err
}

func init() {
	for name, m := range processMethods {
		methods[name] = m
	}
}

var processMethods = map[string]method{
	"data.transition": dataMethod("data.transition", func(d Dataer, p DataTransitionParams) (*DataWriteResult, error) {
		if r := p.Record; r != nil {
			if (p.Type != "" && p.Type != r.Type) || (p.ID != "" && p.ID != r.ID) {
				return nil, Errorf(CodeBadParams, "record and type/id name different records")
			}
			p.Type, p.ID, p.Record = r.Type, r.ID, nil
		}
		if p.ExpectedRev != 0 {
			if p.Rev != 0 && p.Rev != int64(p.ExpectedRev) {
				return nil, Errorf(CodeBadParams, "rev and expected_rev differ")
			}
			p.Rev, p.ExpectedRev = int64(p.ExpectedRev), 0
		}
		if err := needRecord(p.Type, p.ID); err != nil {
			return nil, err
		}
		if _, err := required("transition", p.Transition); err != nil {
			return nil, err
		}
		return d.DataTransition(p)
	}),
	"data.transitions": dataMethod("data.transitions", func(d Dataer, p DataRecordParams) (*DataTransitionsResult, error) {
		if err := needRecord(p.Type, p.ID); err != nil {
			return nil, err
		}
		return d.DataTransitions(p)
	}),
	"data.reverse": dataMethod("data.reverse", func(d Dataer, p DataReverseParams) (*DataWriteResult, error) {
		if err := needRecord(p.Type, p.ID); err != nil {
			return nil, err
		}
		return d.DataReverse(p)
	}),
	"data.work.create": dataMethod("data.work.create", func(d Dataer, p DataWorkCreateParams) (*DataItemResult, error) {
		if _, err := required("title", p.Title); err != nil {
			return nil, err
		}
		return d.DataWorkCreate(p)
	}),
	"data.work.update": dataMethod("data.work.update", func(d Dataer, p DataWorkUpdateParams) (*DataItemResult, error) {
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		return d.DataWorkUpdate(p)
	}),
	"data.work.list": dataMethod("data.work.list", func(d Dataer, p DataWorkListParams) (*DataItemsResult, error) {
		return d.DataWorkList(p)
	}),
	"data.timers.create": dataMethod("data.timers.create", func(d Dataer, p DataTimerCreateParams) (*DataItemResult, error) {
		if _, err := required("fire_at", p.FireAt); err != nil {
			return nil, err
		}
		if _, err := required("kind", p.Kind); err != nil {
			return nil, err
		}
		return d.DataTimerCreate(p)
	}),
	"data.timers.cancel": dataMethod("data.timers.cancel", func(d Dataer, p DataIDParams) (*Empty, error) {
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		return d.DataTimerCancel(p)
	}),
	"data.timers.list": dataMethod("data.timers.list", func(d Dataer, p DataTimerListParams) (*DataItemsResult, error) {
		return d.DataTimerList(p)
	}),
	"data.signoffs.create": dataMethod("data.signoffs.create", func(d Dataer, p DataSignParams) (*DataItemResult, error) {
		if err := needRecord(p.Type, p.ID); err != nil {
			return nil, err
		}
		if _, err := required("statement", p.Statement); err != nil {
			return nil, err
		}
		return d.DataSign(p)
	}),
	"data.signoffs.list": dataMethod("data.signoffs.list", func(d Dataer, p DataRecordParams) (*DataItemsResult, error) {
		if err := needRecord(p.Type, p.ID); err != nil {
			return nil, err
		}
		return d.DataSignoffs(p)
	}),
	"data.comments.create": dataMethod("data.comments.create", func(d Dataer, p DataCommentParams) (*DataItemResult, error) {
		if _, err := required("body", p.Body); err != nil {
			return nil, err
		}
		if (p.Record == "") == (p.Node == "") {
			return nil, Errorf(CodeBadParams, "a comment is on a record (type and record) or a node, one of them")
		}
		return d.DataCommentAdd(p)
	}),
	"data.comments.list": dataMethod("data.comments.list", func(d Dataer, p DataCommentListParams) (*DataItemsResult, error) {
		if (p.Record == "") == (p.Node == "") {
			return nil, Errorf(CodeBadParams, "comments are listed for a record (type and record) or a node, one of them")
		}
		return d.DataCommentList(p)
	}),
	"data.activity": dataMethod("data.activity", func(d Dataer, p DataRecordParams) (*DataItemsResult, error) {
		if err := needRecord(p.Type, p.ID); err != nil {
			return nil, err
		}
		return d.DataActivity(p)
	}),
}

package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ── AN ORGANIZATION WORKSTREAM'S STANDING ORDERS (fix wave X1, E4 F0/F6) ──
//
// An organization's standing order is kept and run by its server, never by a
// person's computer (internal/automation refuses one there). These forward
// to it:
//
//	factory.org_workstream_orders        OrgOrdersParams      -> OrgOrders  GET    /v1/workstreams/{id}/orders
//	factory.org_workstream_order_create  OrgOrderCreateParams -> OrgOrder   POST   /v1/workstreams/{id}/orders
//	factory.org_workstream_order_update  OrgOrderUpdateParams -> OrgOrder   PATCH  /v1/workstreams/{id}/orders/{oid}  If-Match: rev
//	factory.org_workstream_order_delete  OrgOrderParams       -> Empty      DELETE /v1/workstreams/{id}/orders/{oid}
//
// The list says whether the server runs them (`runs`); a desk offers "Add a
// standing order…" only where it does. Each run is a task of the workstream,
// read and answered through the task methods. A change is relayed as
// factory.org_workstream_changed with part "orders".

// FeatureOrgWorkstreamOrders is the hello word for the methods above.
const FeatureOrgWorkstreamOrders = "org_workstream_orders"

// OrgOrdersParams names a workstream.
type OrgOrdersParams struct {
	OrgConn
	WS string `json:"ws"`
}

// OrgOrderParams names one of its orders.
type OrgOrderParams struct {
	OrgConn
	WS string `json:"ws"`
	ID string `json:"id"`
}

// OrgOrderCreateParams is a new order: what to do, and when ("every 0 9 *
// * 1", "every 1h") read in zone, the person's IANA time zone.
type OrgOrderCreateParams struct {
	OrgConn
	WS              string  `json:"ws"`
	Title           string  `json:"title,omitempty"`
	When            string  `json:"when"`
	Zone            string  `json:"zone,omitempty"`
	Does            string  `json:"does"`
	BudgetPerRunUSD float64 `json:"budget_per_run_usd,omitempty"`
	// WordsBy is "afi" when the desk's Afi wrote what the order does: it
	// only ever lowers trust in those words (OrgTaskCreateParams.WordsBy).
	WordsBy string `json:"words_by,omitempty"`
}

// OrgOrderUpdateParams switches an order on or off, at the rev it was read.
type OrgOrderUpdateParams struct {
	OrgConn
	WS  string `json:"ws"`
	ID  string `json:"id"`
	Rev int    `json:"rev"`
	On  *bool  `json:"on"`
}

// OrgOrders and OrgOrder are the server's shapes, passed through.
type (
	OrgOrders = json.RawMessage
	OrgOrder  = json.RawMessage
)

func orgOrdersPath(ws string) (string, error) {
	path, err := orgWorkstreamPath(ws)
	if err != nil {
		return "", err
	}
	return path + "/orders", nil
}

func orgOrderPath(ws, id string) (string, error) {
	path, err := orgOrdersPath(ws)
	if err != nil {
		return "", err
	}
	oid, err := orgSegment("id", id)
	if err != nil {
		return "", err
	}
	return path + "/" + url.PathEscape(oid), nil
}

func withFactoryOrgOrdersFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgWorkstreamOrders)
	}
	return hello
}

func init() {
	methods["factory.org_workstream_orders"] = orgMethod[OrgOrdersParams, OrgOrders]("factory.org_workstream_orders", func(p OrgOrdersParams) (OrgRequest, error) {
		path, err := orgOrdersPath(p.WS)
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	})
	methods["factory.org_workstream_order_create"] = orgMethod[OrgOrderCreateParams, OrgOrder]("factory.org_workstream_order_create", func(p OrgOrderCreateParams) (OrgRequest, error) {
		path, err := orgOrdersPath(p.WS)
		if err != nil {
			return OrgRequest{}, err
		}
		does, when := strings.TrimSpace(p.Does), strings.TrimSpace(p.When)
		switch {
		case does == "":
			return OrgRequest{}, Errorf(CodeBadParams, "a standing order needs something to do")
		case len(does) > orderTextMost:
			return OrgRequest{}, Errorf(CodeBadParams, "what a standing order does is at most %d characters", orderTextMost)
		case when == "":
			return OrgRequest{}, Errorf(CodeBadParams, "a standing order needs a when")
		case p.BudgetPerRunUSD < 0:
			return OrgRequest{}, Errorf(CodeBadParams, "a budget per run is above zero")
		}
		return OrgRequest{Method: http.MethodPost, Path: path, Body: struct {
			Title           string  `json:"title,omitempty"`
			When            string  `json:"when"`
			Zone            string  `json:"zone,omitempty"`
			Does            string  `json:"does"`
			BudgetPerRunUSD float64 `json:"budget_per_run_usd,omitempty"`
			WordsBy         string  `json:"words_by,omitempty"`
		}{strings.TrimSpace(p.Title), when, strings.TrimSpace(p.Zone), does, p.BudgetPerRunUSD, strings.TrimSpace(p.WordsBy)}}, nil
	})
	methods["factory.org_workstream_order_update"] = orgMethod[OrgOrderUpdateParams, OrgOrder]("factory.org_workstream_order_update", func(p OrgOrderUpdateParams) (OrgRequest, error) {
		path, err := orgOrderPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, false)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.On == nil {
			return OrgRequest{}, Errorf(CodeBadParams, "on says whether it runs")
		}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: map[string]bool{"on": *p.On}, IfMatch: rev}, nil
	})
	methods["factory.org_workstream_order_delete"] = orgMethod[OrgOrderParams, Empty]("factory.org_workstream_order_delete", func(p OrgOrderParams) (OrgRequest, error) {
		path, err := orgOrderPath(p.WS, p.ID)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
}

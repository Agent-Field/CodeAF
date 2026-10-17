package appwire

import (
	"context"
	"errors"
	"time"
)

// orders.made_in is the standing orders MADE IN one crew: the automations whose
// recorded creation team (internal/automation's Origin.Team) is exactly that
// crew's id. It is not orders.list, and not the crew an order belongs to
// ([Order.Crew]): it does not read who is in the crew, who manages it now, or
// which workspace it works in. A crew changing hands never moves a row.
//
// IT IS ADDITIVE. orders.list keeps its shape and its filter; an engine without
// FeatureOrdersMadeIn answers orders.made_in as unknown_method.

// FeatureOrdersMadeIn is the hello feature for orders.made_in.
const FeatureOrdersMadeIn = "orders.made_in"

// The bounds an orders.made_in ask is checked against before anything is read.
const (
	OrdersMadeLimitDefault = 50
	OrdersMadeLimitMax     = 100
	OrdersMadeCursorMax    = 128
	OrdersMadeReadTimeout  = 2 * time.Second
	// OrdersMadeReadsInFlight is how many of these reads run at once.
	OrdersMadeReadsInFlight = 4
)

// OrdersMadeAsk is one page of a crew's orders. Cursor is the previous page's
// Next, verbatim.
type OrdersMadeAsk struct {
	Crew   string `json:"crew"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// OrdersMadeResult is one page. Orders are in id order and carry no last_run
// and no asking: those are orders.list's. Next is absent ONLY when the crew's
// rows ended.
//
// UNKNOWN IS HOW MANY ORDERS THE STORE CANNOT PLACE IN ANY CREW OR IN NONE: saved
// before creation teams were recorded, made outside an accepted message, or
// unreadable. It counts the whole store, not this crew, and is real zero when
// zero: a surface must show it beside the rows, never read "none made here"
// from an empty page while it is above zero.
type OrdersMadeResult struct {
	Orders  []Order `json:"orders"`
	Next    string  `json:"next,omitempty"`
	Unknown int     `json:"unknown"`
}

// OrdersMadePager is the optional engine half. It honours ctx.
type OrdersMadePager interface {
	PageOrdersMadeIn(ctx context.Context, ask OrdersMadeAsk) (*OrdersMadeResult, error)
}

// CheckOrdersMadeAsk is the effective limit, or bad_params.
func CheckOrdersMadeAsk(p OrdersMadeAsk) (int, error) {
	if !validTeamID(p.Crew) {
		return 0, Errorf(CodeBadParams, "orders.made_in needs the crew's id")
	}
	if len(p.Cursor) > OrdersMadeCursorMax || (p.Cursor != "" && !plainText(p.Cursor, OrdersMadeCursorMax)) {
		return 0, Errorf(CodeBadParams, "orders.made_in cursor is not one this method gave")
	}
	switch {
	case p.Limit == 0:
		return OrdersMadeLimitDefault, nil
	case p.Limit < 0 || p.Limit > OrdersMadeLimitMax:
		return 0, Errorf(CodeBadParams, "orders.made_in limit is 1 to %d", OrdersMadeLimitMax)
	}
	return p.Limit, nil
}

// validTeamID is the teams directory-name law: letters, digits, _ and -, at
// most 64 bytes.
func validTeamID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

var ordersMadeReads = make(chan struct{}, OrdersMadeReadsInFlight)

func withOrdersMadeFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(OrdersMadePager); ok {
		return withFeature(hello, FeatureOrdersMadeIn)
	}
	return hello
}

func init() {
	methods["orders.made_in"] = typed(func(e Engine, p OrdersMadeAsk) (*OrdersMadeResult, error) {
		pager, ok := e.(OrdersMadePager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve orders.made_in")
		}
		if _, err := CheckOrdersMadeAsk(p); err != nil {
			return nil, err
		}
		// THE SLOT IS HELD UNTIL THE READ SETTLES, not until a timer gives up on
		// it: the read honours its context, so the deadline ends the work itself.
		select {
		case ordersMadeReads <- struct{}{}:
		default:
			return nil, Errorf(CodeBusy, "too many orders reads at once; try again")
		}
		defer func() { <-ordersMadeReads }()
		ctx, cancel := context.WithTimeout(context.Background(), OrdersMadeReadTimeout)
		defer cancel()
		res, err := pager.PageOrdersMadeIn(ctx, p)
		if err != nil {
			var wire *Error
			switch {
			case errors.As(err, &wire):
				return nil, err
			case errors.Is(err, context.DeadlineExceeded):
				return nil, Errorf(CodeEngine, "orders.made_in took longer than %s", OrdersMadeReadTimeout)
			case errors.Is(err, context.Canceled):
				return nil, Errorf(CodeEngine, "orders.made_in was cancelled")
			}
			return nil, Errorf(CodeEngine, "the crew's orders could not be read")
		}
		res.Orders = nonNil(res.Orders)
		return res, nil
	})
}

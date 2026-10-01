package executor

import "context"

// Settler is a seat that records the tree and can be asked to record it as it
// stands, after the last call: the transcript's tail is written once the calls
// of a turn are over.
type Settler interface {
	Settle(context.Context)
}

// Settle asks the seat to record the tree as it stands. A seat that keeps no
// record ignores it.
func Settle(ctx context.Context, s Seat) {
	if st, ok := s.(Settler); ok {
		st.Settle(ctx)
	}
}

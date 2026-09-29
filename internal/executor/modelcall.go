package executor

// ModelCall is one model call as a seat is told of it: who answered, for what,
// and what it cost. The seat that keeps a record puts it in the next receipt.
type ModelCall struct {
	Model        string
	Role         string
	TokensIn     int
	TokensOut    int
	TokensCached int
	CostMicroUSD int64
}

// ModelNoter is a seat that records model calls.
type ModelNoter interface {
	NoteModelCall(ModelCall)
}

// NoteModelCall tells the seat about a model call. A seat that keeps no record
// ignores it, the way ForSetup leaves a seat that cannot tell setup apart.
func NoteModelCall(s Seat, m ModelCall) {
	if n, ok := s.(ModelNoter); ok {
		n.NoteModelCall(m)
	}
}

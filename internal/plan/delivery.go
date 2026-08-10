package plan

// The delivery law: where the finished thing has to appear, and the one shape of
// ask for which the answer is different.
//
// It was stated in five places and every place knew a different amount. The
// instruction the deliverable owner receives and the working method written for
// it both demanded the deliverable written out in the final message and never
// filed; the leaf's own system prompt (exec/linear.go) told the same worker to
// keep that message under about three hundred words and put the long version in
// a file; and only the delivery gate knew the fact that reconciles them — that
// an ask which named a file, or asks for a change to material that already
// exists, makes the file the deliverable and the split correct. One task died
// over four gate rounds on that gap: the round that filed a 37KB document was
// failed for obeying the sane rule, and no prompt in the system knew that a
// deliverable has a size.
//
// So the law is one thing with two shapes, and which shape applies is a property
// of the ask rather than of whichever prompt happens to be speaking. The two
// constants are the law; DeliveryLaw picks between them; and the sites that
// commission a deliverable render the one the ask calls for.

// DeliverInMessage is the default shape, and it is the one that holds unless the
// ask itself is file-shaped.
//
// It states the split rather than a length, which is what lets it agree with the
// leaf's own budget instead of fighting it: the answer is never what gets filed,
// and the working always may be.
const DeliverInMessage = `The final message is the deliverable itself: the answer, the verdict, the
figures that carry it, written out there in full rather than filed somewhere and
named. A message that says where the answer lives instead of carrying it has
delivered nothing. Where there is more than the answer — the evidence, the
detail, the reasoning behind it — that working may live in a file: the split is
between the answer and its working, never between the answer and a pointer to
the answer.`

// DeliverToNamedFile is the carve-out, and it is the half nothing but the gate
// used to know.
//
// The wording follows the gate's own, because the gate is what judges the result
// and a worker held to a different sentence than the one it will be judged by is
// being set up to fail. Short is not thin here: a message beside an asked-for
// file is the correct shape, and its length convicts nothing.
const DeliverToNamedFile = `This ask is file-shaped — it named a file or document, or it asks for a change
to material that already exists — so the file IS the deliverable. Writing it
there is the work, not a way of avoiding the answer. The final message then
carries the answer itself, what was run and what came back, and the name of the
file; it never carries the file's whole contents, and its shortness convicts
nothing.`

// DeliveryLaw returns the half of the law that applies to an ask of this shape.
//
// It is exported because the bit is not the planner's to compute. Whoever holds
// the request holds the judgment — the chat session already makes it in the
// delivery gate, and `aforge run` can make it from the goal — and everything
// below this line only needs the answer. See Graph.FileShaped for the wiring.
func DeliveryLaw(fileShaped bool) string {
	if fileShaped {
		return DeliverToNamedFile
	}
	return DeliverInMessage
}

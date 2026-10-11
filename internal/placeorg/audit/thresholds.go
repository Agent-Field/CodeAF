package audit

// The figures are the reference implementation's, named once so a change to
// one of them is a decision and not a drift between the op and its comment.
// They are ratios of chats or of cosine similarity, never a tuned word list.
const (
	// dominantPercent is the share of a top-level place at which its child
	// is the place: 70 of every 100 chats. A child that holds all of them
	// (a chain of one) is the same rule, not a special case.
	dominantPercent = 70

	// interposeSharePercent rejects a grouping that would hold more than
	// 40 of every 100 chats still in a place. One bucket that large is a
	// second library, not a parent.
	interposeSharePercent = 40

	// interposeSepMax is the mean similarity above which the children are
	// not actually separate places. The reference uses 0.80.
	interposeSepMax = 0.80

	// interposeDistance is how close two place centroids must be, as
	// 1 − cosine, before average-linkage grouping puts them together.
	// 0.30 is the reference's distance threshold (similarity at least 0.70).
	interposeDistance = 0.30

	// anchorParentMax is the share of the parent that may already carry an
	// anchor before that anchor stops counting as the child's own subject.
	// The reference requires the parent's share to be under 0.80.
	anchorParentMax = 0.80

	// anchorMemberShare is the fraction of the child's own chats an anchor
	// must cover, besides the quorum, before the anchor decides on its own.
	anchorMemberShare = 0.25

	// splitK and splitFloor are the neighbour list a subdivision reads.
	// They match the reference's subgraph cut (k = 8, similarity 0.35).
	// The list is kept per chat; the stage never builds a chat-by-chat matrix.
	splitK     = 8
	splitFloor = 0.35

	// splitMinDepth is the shallowest place that may subdivide. The reference
	// walks the finest places, which sit three levels down, and leaves a flat
	// top-level place whole. Children a split creates are deeper and may
	// split again until MaxDepth.
	splitMinDepth = 3

	// facetTitles is how many chat titles the facet question shows. The
	// reference shows ten.
	facetTitles = 10
)

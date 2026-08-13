package head

import "github.com/Agent-Field/aforge-v2/internal/ctxbudget"

// One pot, computed once, for a prompt that is built again from nothing every
// single turn.
//
// Every block the head assembles used to carry a byte ceiling of its own,
// written as a literal and argued for in prose beside it. Added up they came to
// about twenty-two kilobytes, and that was the head's prompt whether the model
// behind it held a hundred and thirty thousand tokens or a million: the window
// grew tenfold and the thread was still cut at eight kilobytes, because nothing
// in this package had ever been told how big the window was. The head was not
// being frugal, it was being uninformed.
//
// So the literals stay exactly where they are and stop being ceilings. Each one
// is now the FALLBACK for its block — what that block gets when nobody told the
// head what model it speaks through — and the ceiling itself is a weighted share
// of one pot ctxbudget derives from the window. The weights are today's own
// proportions, so the shape of a turn — what matters more than what — is the
// one thing the window cannot change, and a window that prices a block at what
// it already had leaves it exactly there.
//
// Nothing shrinks. A share that lands under its own fallback loses to it, so a
// small window is exactly the head that shipped before this law and a large one
// is the same head with room. On the hundred-and-twenty-eight-thousand-token
// models most talk slots run, the reserve the law keeps for reasoning is most of
// the window and every block does floor to its literal — which is the honest
// answer, not a missed opportunity: there was never any room to hand out. The
// windows that have room are the ones that get it.
//
// And every number here is a CEILING rather than a purchase: the thread block is
// twenty messages long whatever the budget says, and a room that has not filled
// eight kilobytes does not start filling eighty. What the budget buys is that
// the truncation stops firing on a model that had room to spare.

const (
	// headFixedFloorTokens is the measured cost of everything in a head turn
	// that is not budgeted material: the orchestrator system prompt (11KB as
	// written) and the belt's tool schemas (14KB of JSON), with the clock, the
	// rail line and the hints on top. It is stated rather than measured per turn
	// because the pot is computed once, at construction — a budget that moved
	// between turns would move the front of the prompt with it, and the front of
	// this prompt holding still is what the whole cache-shape argument rests on.
	headFixedFloorTokens = 6656
	// headBudgetTotal is the denominator every weight below is out of, and the
	// thing being divided is ONE TURN rather than one prompt.
	//
	// That distinction is the whole of why the weights look small. A turn opens
	// with its ambient prompt — thread, board, depth, notebook, re-entry, about
	// twenty-two kilobytes of it — and then spends up to orchestratorToolCallCap
	// tool calls whose results land in the same window and stay there. Sixteen
	// reads at the size the read literals argue for come to well over a hundred
	// kilobytes, so the prompt the turn opens with is about a sixth of what the
	// turn actually costs, and the reads are the rest.
	//
	// Weighting the reads against the ambient prompt instead — a bash page as
	// seventy percent of "the pot" — reads plausibly and is a trap: it holds on
	// a window where every read floors to its literal, and on a large one it
	// prices sixteen reads at ten times the window they have to fit in. So the
	// denominator is the turn, every weight is that block's share of one, and a
	// turn that spends its whole belt on average-sized reads comes out at
	// exactly the pot on any window at all.
	headBudgetTotal = 1000
)

// The ambient weights: what the turn opens with, in the proportions the
// literals already had. They sum to 174 — a sixth of a turn — and that is the
// measured share, not a target.
const (
	threadWeight   = 62
	boardWeight    = 39
	deepWeight     = 46
	notebookWeight = 15
	reentryWeight  = 12
)

// The read weights, out of the same thousand: each read's own literal as a
// share of a whole turn. A belt of sixteen average reads is 826 of the
// thousand, which is what makes the ambient sixth a sixth.
const (
	resultWeight     = 31
	groundingWeight  = 15
	artifactWeight   = 46
	transcriptWeight = 31
	lensPageWeight   = 62
	bashWeight       = 124
	forkWeight       = 23
	correctionWeight = 9
)

// promptBudget is the whole of what the window buys, resolved to bytes and
// counts once. It is a value rather than a handle on the ctxbudget.Budget
// because the arithmetic is done at construction: a renderer asks for a number,
// never for a budget it would have to spend correctly on its own.
type promptBudget struct {
	thread         int
	threadMessage  int
	board          int
	boardRows      int
	deep           int
	deepResult     int
	deepJobs       int
	deepFiles      int
	notebook       int
	reentry        int
	reentryLine    int
	reentryJobs    int
	reentryFiles   int
	result         int
	grounding      int
	artifact       int
	artifactTail   int
	transcript     int
	transcriptLine int
	lensPage       int
	bash           int
	fork           int
	forkTurn       int
	correction     int
}

// newPromptBudget turns a window into every number the head renders under.
// Zero tokens means the window is unknown, and unknown is not small: every
// block falls back to the literal it shipped with, which is what this package
// did before it was told anything at all.
func newPromptBudget(contextTokens int) promptBudget {
	budget := ctxbudget.For(contextTokens).WithFloor(headFixedFloorTokens)
	// ctxbudget.Share falls back only when the window is unknown or the share
	// computes to nothing. The head asks for one more guarantee than that: a
	// window small enough to price a block under its old literal must not make
	// the head worse than the head that had no budget at all.
	share := func(weight, fallback int) int {
		if size := budget.Share(weight, headBudgetTotal, fallback); size > fallback {
			return size
		}
		return fallback
	}
	sized := promptBudget{
		thread:     share(threadWeight, maxThreadContextBytes),
		board:      share(boardWeight, maxGraphContextBytes),
		deep:       share(deepWeight, maxDeepContextBytes),
		notebook:   share(notebookWeight, notebookContextBytes),
		reentry:    share(reentryWeight, threadBriefBytes),
		result:     share(resultWeight, beltResultBytes),
		grounding:  share(groundingWeight, beltGroundingBytes),
		artifact:   share(artifactWeight, beltArtifactBytes),
		transcript: share(transcriptWeight, transcriptBytes),
		lensPage:   share(lensPageWeight, lensPageBytes),
		bash:       share(bashWeight, bashOutputBytes),
		fork:       share(forkWeight, forkContextBytes),
		correction: share(correctionWeight, correctionPreviousBytes),
	}
	// The sub-caps ride their own block rather than the pot. A per-message
	// allowance is a statement about how the thread block is spent, not about
	// how big the window is, and the two only agree because one is derived from
	// the other here.
	sized.threadMessage = portion(sized.thread, maxThreadContextBytes, threadMessageBytes)
	sized.boardRows = portion(sized.board, maxGraphContextBytes, BoardRowCap)
	sized.deepResult = portion(sized.deep, maxDeepContextBytes, deepResultBytes)
	sized.deepJobs = portion(sized.deep, maxDeepContextBytes, deepSliceLimit)
	sized.deepFiles = portion(sized.deep, maxDeepContextBytes, deepFileCap)
	sized.reentryLine = portion(sized.reentry, threadBriefBytes, threadBriefLineBytes)
	sized.reentryJobs = portion(sized.reentry, threadBriefBytes, threadBriefJobs)
	sized.reentryFiles = portion(sized.reentry, threadBriefBytes, threadBriefFiles)
	sized.artifactTail = sized.artifact / artifactTailDivisor
	sized.transcriptLine = portion(sized.transcript, transcriptBytes, transcriptLineBytes)
	sized.forkTurn = portion(sized.fork, forkContextBytes, forkTurnBytes)
	return sized
}

// portion scales a sub-cap by how much its block actually got. A thread block
// at twice its fallback allows twice as much of one message; a block at its
// fallback allows exactly what it always did. It floors at the fallback for the
// same reason share does — nothing this law touches may come out smaller than
// it was before the law existed.
func portion(block, blockFallback, fallback int) int {
	if block <= blockFallback || blockFallback <= 0 {
		return fallback
	}
	if scaled := fallback * block / blockFallback; scaled > fallback {
		return scaled
	}
	return fallback
}

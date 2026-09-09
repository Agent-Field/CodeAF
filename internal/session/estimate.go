package session

// EstimateTokens is THE estimator this program uses when nobody has told it the
// real figure: bytes, over [bytesPerToken].
//
// IT IS EXPORTED BECAUSE A SURFACE HAS THE SAME QUESTION AND MUST NOT ANSWER IT
// SEPARATELY. The engine's books move once a step, when a provider answers with
// what the step actually cost; a chat drawing a live figure between two of those
// readings has only the bytes on its own page to go on (internal/tui3's
// tokencol.go). The day that surface divides by a constant of its own is the day
// two parts of one program disagree about what a token weighs, and the person
// reading the screen has no way to tell which of them is lying.
//
// A negative or zero count is nothing rather than a negative estimate: the
// callers subtract one reading from another, and a subtraction that has not
// caught up yet is an absence of information, not a debt of tokens.
func EstimateTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return bytes / bytesPerToken
}

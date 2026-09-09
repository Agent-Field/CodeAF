package provider

// progressTokens estimates readable generation without confusing SSE frames
// with tokens. The controller compares these counts with a per-token rate:
// a frame carrying a paragraph must earn more progress than a single token.
// Counting accumulated bytes keeps fragmented and batched text equivalent.
// Billing still uses the provider's usage receipt, never this estimate.
type progressTokens struct{ bytes int }

func (p *progressTokens) add(text string) int {
	before := (p.bytes + charsPerToken - 1) / charsPerToken
	p.bytes += len(text)
	return (p.bytes+charsPerToken-1)/charsPerToken - before
}

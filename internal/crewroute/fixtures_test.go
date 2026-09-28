package crewroute

import "time"

// released is a catalog row's release date.
func released(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// Catalog rows as a live catalog lists them: per-token prices, context, open
// weights, the published indexes and design-arena Elo, and the release date.
var (
	glmFlash = Model{ID: "z-ai/glm-5.3-flash", Open: true, PromptPrice: 4.5e-8, CompletionPrice: 6e-7, CacheReadPrice: 2.85e-8,
		Intelligence: 41.8, Coding: 71.5, Agentic: 50.9, ArenaElo: 1348, Context: 1310720, Released: released(2026, 8, 26), Tools: true}
	kimiK3 = Model{ID: "moonshotai/kimi-k3", Open: true, PromptPrice: 3e-6, CompletionPrice: 1.5e-5, CacheReadPrice: 3e-7,
		Intelligence: 43.6, Coding: 76.2, Agentic: 50, ArenaElo: 1421, Context: 1048576, Released: released(2026, 7, 15), Tools: true}
	v4Flash = Model{ID: "deepseek/deepseek-v4-flash", Open: true, PromptPrice: 7.168e-8, CompletionPrice: 1.4336e-7, CacheReadPrice: 1.4336e-8,
		Intelligence: 24.2, Coding: 56.2, Agentic: 22.2, ArenaElo: 1216, Context: 1048576, Released: released(2026, 4, 23), Tools: true}
	glm53 = Model{ID: "z-ai/glm-5.3", Open: true, PromptPrice: 1.4e-6, CompletionPrice: 4.4e-6, CacheReadPrice: 2.6e-7,
		Intelligence: 44.8, Coding: 74.8, Agentic: 53.1, ArenaElo: 1387, Context: 1310720, Released: released(2026, 8, 16), Tools: true}
	opus5 = Model{ID: "anthropic/claude-opus-5", PromptPrice: 5e-6, CompletionPrice: 2.5e-5, CacheReadPrice: 5e-7,
		Intelligence: 50.8, Coding: 78, Agentic: 56.5, ArenaElo: 1372, Context: 1000000, Released: released(2026, 7, 23), Tools: true}
	fable51 = Model{ID: "anthropic/claude-fable-5.1", PromptPrice: 1e-5, CompletionPrice: 5e-5, CacheReadPrice: 2.5e-7,
		Intelligence: 53.4, Coding: 81.6, Agentic: 57.9, ArenaElo: 1421, Context: 1000000, Released: released(2026, 8, 31), Tools: true}
	astra6 = Model{ID: "openai/gpt-6-astra", PromptPrice: 1e-5, CompletionPrice: 5e-5, CacheReadPrice: 1e-6,
		Intelligence: 52.7, Coding: 76.9, Agentic: 51, Context: 1050000, Released: released(2026, 9, 3), Tools: true}
)

// candidateOf is a model reachable on the default service only.
func candidateOf(m Model) Candidate {
	return Candidate{Model: m, Routes: []Route{{Provider: "openrouter", Send: m.ID, Kind: Metered}}}
}

// catalogCandidates are three open models across the price range, each on the
// default service.
func catalogCandidates() []Candidate {
	return []Candidate{candidateOf(glmFlash), candidateOf(kimiK3), candidateOf(v4Flash)}
}

// frontierCandidates are the three open models with a mid-price model and the
// dearest frontier rows beside them.
func frontierCandidates() []Candidate {
	return append(catalogCandidates(), candidateOf(glm53), candidateOf(opus5), candidateOf(fable51), candidateOf(astra6))
}

// catalogRow is a model with published figures and no release date.
func catalogRow(id string, open bool, in, out float64, intel, coding, agentic float64) Candidate {
	return Candidate{
		Model: Model{ID: id, Open: open, PromptPrice: in / 1e6, CompletionPrice: out / 1e6, CacheReadPrice: in / 1e7,
			Intelligence: intel, Coding: coding, Agentic: agentic, Context: 1_000_000, Tools: true},
		Routes: []Route{{Provider: "openrouter", Send: id, Kind: Metered}},
	}
}

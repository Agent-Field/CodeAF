package ctxbudget

import "testing"

func TestUnknownWindowSpendsNothing(t *testing.T) {
	b := For(0)
	if b.Known() {
		t.Fatal("zero window must be unknown")
	}
	if got := b.BytesOr(4096); got != 4096 {
		t.Fatalf("fallback = %d, want 4096", got)
	}
	if got := b.TokensOr(600); got != 600 {
		t.Fatalf("fallback = %d, want 600", got)
	}
	if got := b.Share(1, 4, 512); got != 512 {
		t.Fatalf("share fallback = %d, want 512", got)
	}
}

func TestFillLaw(t *testing.T) {
	b := Budget{ContextTokens: 1_000_000, CompletionReserveTokens: 65536, FillPercent: 60}
	want := 1_000_000*60/100 - 65536
	if got := b.Tokens(); got != want {
		t.Fatalf("tokens = %d, want %d", got, want)
	}
	if got := b.Bytes(); got != want*BytesPerToken {
		t.Fatalf("bytes = %d, want %d", got, want*BytesPerToken)
	}
}

func TestFloorAndReserveCannotGoNegative(t *testing.T) {
	b := Budget{ContextTokens: 10_000, CompletionReserveTokens: 65536, FillPercent: 60}
	if got := b.Tokens(); got != 0 {
		t.Fatalf("tokens = %d, want 0", got)
	}
	if got := b.BytesOr(2048); got != 2048 {
		t.Fatalf("a starved budget must fall back, got %d", got)
	}
}

func TestShareSplitsThePot(t *testing.T) {
	b := Budget{ContextTokens: 200_000, CompletionReserveTokens: 20_000, FillPercent: 50}
	pot := b.Bytes()
	if got := b.Share(1, 4, 0); got != pot/4 {
		t.Fatalf("share = %d, want %d", got, pot/4)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", "95")
	if got := FillPercent(); got != 90 {
		t.Fatalf("fill clamp = %d, want 90", got)
	}
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", "5")
	if got := FillPercent(); got != 10 {
		t.Fatalf("fill clamp = %d, want 10", got)
	}
	t.Setenv("AFORGE_COMPLETION_RESERVE", "100000")
	if got := CompletionReserve(); got != 100000 {
		t.Fatalf("reserve = %d, want 100000", got)
	}
}

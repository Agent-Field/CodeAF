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

// A consumer whose reply has to carry what it was given may say so, and the
// budget takes room from the prompt to make room for it. The constant reserve is
// right for a call that answers and wrong for a call that reproduces.
func TestACompletionReserveMayBeRaisedByAConsumerThatCanSayWhy(t *testing.T) {
	base := For(200_000).WithFloor(8192)
	before := base.Tokens()

	// Below the law's reserve is not a request, it is a claim to need less, and
	// nothing gets to make it: the process-wide reserve is a floor.
	if got := base.WithCompletionReserve(1).Tokens(); got != before {
		t.Fatalf("a reserve below the law changed the budget: %d then %d", before, got)
	}

	// Above it, every token reserved is a token the prompt no longer has. That
	// exchange is the point: the material moves to a handle the consumer pulls.
	raised := base.WithCompletionReserve(base.CompletionReserveTokens + 20_000)
	if raised.CompletionReserveTokens != base.CompletionReserveTokens+20_000 {
		t.Fatalf("reserve = %d, want %d", raised.CompletionReserveTokens,
			base.CompletionReserveTokens+20_000)
	}
	if raised.Tokens() != before-20_000 {
		t.Fatalf("prompt room = %d, want %d", raised.Tokens(), before-20_000)
	}

	// The clamp is stated in the consumer's own units: the reserve stops where
	// the prompt's remaining room reaches the prompt's own fixed cost. A demand
	// for the whole window therefore leaves a usable prompt rather than none.
	greedy := base.WithCompletionReserve(1 << 30)
	if greedy.Tokens() != base.FixedFloorTokens {
		t.Fatalf("an unbounded demand left %d tokens of prompt, want the %d-token floor",
			greedy.Tokens(), base.FixedFloorTokens)
	}

	// An unknown window spends nothing and states nothing; the caller's named
	// fallback still carries the day.
	if got := For(0).WithCompletionReserve(1 << 20).BytesOr(4096); got != 4096 {
		t.Fatalf("an unknown window answered %d rather than falling back", got)
	}
}

func TestShareSplitsThePot(t *testing.T) {
	b := Budget{ContextTokens: 200_000, CompletionReserveTokens: 20_000, FillPercent: 50}
	pot := b.Bytes()
	if got := b.Share(1, 4, 0); got != pot/4 {
		t.Fatalf("share = %d, want %d", got, pot/4)
	}
}

// The working set is a clamp on the pot, applied before the fill percentage,
// and it only ever clamps down. A model that fits inside it is sized exactly as
// it was before the concept existed — which is what makes this additive rather
// than a re-tuning of every small model in the catalog.
func TestTheWorkingSetClampsOnlyTheModelsThatOverflowIt(t *testing.T) {
	ceiling := WorkingSetCeiling()

	huge := For(1 << 20)
	clamped := huge.WithinWorkingSet()
	if clamped.ContextTokens != ceiling {
		t.Fatalf("a 1M-token model kept %d tokens of pot, want the %d working set",
			clamped.ContextTokens, ceiling)
	}
	if clamped.Tokens() >= huge.Tokens() {
		t.Fatalf("the clamp did not reduce anything: %d then %d", huge.Tokens(), clamped.Tokens())
	}

	// A 128k model is under the ceiling, so nothing about it moves.
	small := For(128_000)
	if got := small.WithinWorkingSet(); got.ContextTokens != small.ContextTokens ||
		got.Tokens() != small.Tokens() {
		t.Fatalf("a 128k model was clamped from %d to %d tokens", small.Tokens(), got.Tokens())
	}

	// Exactly at the ceiling is also untouched: the clamp is a maximum, not a
	// target to be rounded to.
	exact := For(ceiling)
	if got := exact.WithinWorkingSet(); got.Tokens() != exact.Tokens() {
		t.Fatalf("a model the size of the working set was resized from %d to %d",
			exact.Tokens(), got.Tokens())
	}

	// An unknown window has nothing to clamp and says so.
	if got := For(0).WithinWorkingSet(); got.Known() {
		t.Fatal("an unknown window became known by being clamped")
	}
	if got := For(0).WithinWorkingSet().BytesOr(4096); got != 4096 {
		t.Fatalf("a clamped unknown window answered %d rather than falling back", got)
	}
}

// The reuse ceiling is the cumulative bound: how much prompt one loop may send
// in total. It is taken of the working set, so it stays sane on a small model
// where a fraction of the window would land an honest leaf at turn three.
func TestReuseCeilingIsTakenOfTheWorkingSetAndGoesInertWhenUnknown(t *testing.T) {
	// An unknown window is never governed — the whole point of the inertness
	// pattern: a bound derived from a guess would fire on evidence nobody has.
	if got := For(0).ReuseCeiling(); got != 0 {
		t.Fatalf("an unknown window produced a %d-token reuse ceiling", got)
	}

	ceiling := For(1 << 20).ReuseCeiling()
	want := WorkingSetCeiling() * FillPercent() / 100 * ReusePercent() / 100
	if ceiling != want {
		t.Fatalf("a 1M-token model may send %d tokens in total, want %d", ceiling, want)
	}
	// The measured threshold this default was set to reproduce: about a quarter
	// of a 1M window, which is where the trace ledgers put the landing that
	// would have caught every runaway node just after its real work.
	if quarter := (1 << 20) / 4; ceiling < quarter*8/10 || ceiling > quarter*12/10 {
		t.Fatalf("the 1M-model bound is %d tokens, nowhere near the measured %d", ceiling, quarter)
	}
	// And it is more than one whole context: a leaf must be able to send what
	// it is holding, and then some, before any cumulative bound may fire.
	if room := For(1 << 20).WithinWorkingSet().Tokens(); ceiling <= room {
		t.Fatalf("the bound is %d tokens against a %d-token working set — a leaf could not fill it once",
			ceiling, room)
	}

	// A small model's bound follows its own window rather than the ceiling.
	if got, big := For(64_000).ReuseCeiling(), For(1<<20).ReuseCeiling(); got >= big {
		t.Fatalf("a 64k model was given the same %d-token allowance as a 1M one", got)
	}
	if got, want := For(64_000).ReuseCeiling(), 64_000*FillPercent()/100*ReusePercent()/100; got != want {
		t.Fatalf("a 64k model may send %d tokens, want %d", got, want)
	}
}

// Both new knobs resolve the same way everything else here does, and the reuse
// clamp refuses to become a refusal.
func TestWorkingSetAndReuseEnvOverrides(t *testing.T) {
	t.Setenv("AFORGE_WORKING_SET", "48000")
	if got := WorkingSetCeiling(); got != 48_000 {
		t.Fatalf("working set = %d, want 48000", got)
	}
	if got := For(200_000).WithinWorkingSet().ContextTokens; got != 48_000 {
		t.Fatalf("the pinned ceiling did not reach the budget: %d tokens", got)
	}
	t.Setenv("AFORGE_CONTEXT_REUSE_PCT", "40")
	if got := ReusePercent(); got != 100 {
		t.Fatalf("reuse = %d, want the 100%% clamp — a loop must be able to send its context once", got)
	}
	t.Setenv("AFORGE_CONTEXT_REUSE_PCT", "400")
	if got := ReusePercent(); got != 400 {
		t.Fatalf("reuse = %d, want 400", got)
	}
}

// The settings sheet configures the law as one value, and configuring one field
// may not silently revert the others.
func TestConfigureCarriesTheWholeLaw(t *testing.T) {
	t.Cleanup(func() { Configure(Limits{}) })
	Configure(Limits{FillPercent: 40, CompletionReserveTokens: 8192,
		WorkingSetTokens: 90_000, ReusePercent: 300})
	if FillPercent() != 40 || CompletionReserve() != 8192 ||
		WorkingSetCeiling() != 90_000 || ReusePercent() != 300 {
		t.Fatalf("the configured law reads back as fill=%d reserve=%d working=%d reuse=%d",
			FillPercent(), CompletionReserve(), WorkingSetCeiling(), ReusePercent())
	}
	// A zero field is unset rather than zero: the default carries it.
	Configure(Limits{FillPercent: 40})
	if got := WorkingSetCeiling(); got != DefaultWorkingSetTokens {
		t.Fatalf("an unset working set read as %d, want the %d default", got, DefaultWorkingSetTokens)
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

package tokens

import "testing"

// TestResolveTokenIsTotal: the two axes come from a caller, so every pair —
// including values outside both enums — must produce a usable token and never a
// panic. Zero panics is the bar, and the render hot path is where it matters.
func TestResolveTokenIsTotal(t *testing.T) {
	for h := Hue(0); h < 40; h++ {
		for s := State(0); s < 40; s++ {
			tok := ResolveToken(h, s)
			if tok >= tokenCount {
				t.Fatalf("ResolveToken(%d, %d) returned an invalid token", h, s)
			}
			if tok.IsSurface() {
				t.Errorf("ResolveToken(%d, %d) = %s, a background", h, s, tok)
			}
			if tok.String() == "" {
				t.Errorf("ResolveToken(%d, %d) named nothing", h, s)
			}
		}
	}
}

// TestHueAlwaysWins is the first clause of the composition rule. A cell that
// means something keeps meaning it after the row settles — 5.16's green settled
// ✓ and coral ✕ are settled cells that are still coloured, and 8.1.6's "accent
// settles to plain text" governs the row's PROSE, which carries no hue.
func TestHueAlwaysWins(t *testing.T) {
	for _, c := range []struct {
		hue  Hue
		want Token
	}{
		{HueAttention, Amber},
		{HueAlive, Cyan},
		{HueMoney, Green},
		{HueBroken, Coral},
	} {
		for s := State(0); s < stateCount; s++ {
			if got := ResolveToken(c.hue, s); got != c.want {
				t.Errorf("%s at state %s resolved to %s, want %s", c.hue, s, got, c.want)
			}
		}
	}
	// The identity hue has no colour without a seed; it must still land on the
	// wheel rather than on the grey ramp, so a caller that forgot the seed sees
	// a visible placeholder rather than silently-plain text.
	for s := State(0); s < stateCount; s++ {
		if _, ok := IdentityIndex(ResolveToken(HueIdentity, s)); !ok {
			t.Errorf("HueIdentity at state %s did not resolve onto the wheel", s)
		}
	}
}

// TestStateAxisPicksTheTier is the second clause: with no hue, the state axis
// chooses the grey tier — and never reaches the SECONDARY tier, which a
// renderer names directly because a status line is secondary for what it is,
// not for how live it is.
func TestStateAxisPicksTheTier(t *testing.T) {
	cases := map[State]Token{
		StateLive:    Cyan,
		StateSettled: TextPrimary,
		StateChrome:  TextTertiary,
	}
	for s, want := range cases {
		if got := ResolveToken(HueNone, s); got != want {
			t.Errorf("unhued %s resolved to %s, want %s", s, got, want)
		}
	}
	for s := State(0); s < stateCount; s++ {
		if ResolveToken(HueNone, s) == TextSecondary {
			t.Errorf("state %s reached the secondary tier; that tier is named directly, never derived", s)
		}
	}
	// The zero value is settled, which is the safe default: a row wrongly drawn
	// settled is quiet, and a row wrongly drawn live lies about liveness.
	var zero State
	if zero != StateSettled {
		t.Error("the zero State must be settled")
	}
}

// TestPromoteAndDemoteWalkOnlyTheGreyRamp. Brightening a pastel would break the
// contrast pairs the gate measured, and a louder amber does not mean a more
// urgent question.
func TestPromoteAndDemoteWalkOnlyTheGreyRamp(t *testing.T) {
	if Promote(TextTertiary) != TextSecondary || Promote(TextSecondary) != TextPrimary {
		t.Error("Promote does not walk the ramp")
	}
	if Promote(TextPrimary) != TextPrimary {
		t.Error("Promote must saturate at the top")
	}
	if Demote(TextPrimary) != TextSecondary || Demote(TextSecondary) != TextTertiary {
		t.Error("Demote does not walk the ramp")
	}
	if Demote(TextTertiary) != TextTertiary {
		t.Error("Demote must saturate at the bottom")
	}
	for _, tok := range All() {
		switch tok {
		case TextPrimary, TextSecondary, TextTertiary:
			continue
		}
		if Promote(tok) != tok || Demote(tok) != tok {
			t.Errorf("%s moved on the grey ramp; only the three tiers do", tok)
		}
	}
	// The pair is an inverse on the interior of the ramp.
	if Demote(Promote(TextTertiary)) != TextTertiary || Promote(Demote(TextPrimary)) != TextPrimary {
		t.Error("Promote and Demote are not inverses in the middle of the ramp")
	}
	// 5.22's rule holds: a chrome control that brightens on focus leaves the
	// chrome contrast gate behind, so no interactive control lives permanently
	// at the weaker gate.
	if Promote(TextTertiary).Class() == ClassChrome {
		t.Error("a promoted chrome token is still gated as chrome")
	}
}

// TestCutMarkVocabulary is the truncation law's half of Wave 2 (12.5.2). The
// token layer does not decide WHERE a cut is drawn — that binds the head and
// the parts renderers — but nothing above can render one honestly if the
// vocabulary cannot say it.
func TestCutMarkVocabulary(t *testing.T) {
	if CutToken(CutNone) == Coral {
		t.Error("a turn that ended by finishing is not broken")
	}
	for _, k := range []CutKind{CutLengthCap, CutStreamDrop} {
		if got := CutToken(k); got != Coral {
			t.Errorf("%s renders %s, want coral — an unmarked half-artifact is a lie of omission (12.5)", k, got)
		}
		if got := CutHue(k); got != HueBroken {
			t.Errorf("%s is hue %s, want broken", k, got)
		}
		if ResolveToken(CutHue(k), StateSettled) != CutToken(k) {
			t.Errorf("%s disagrees between the hue axis and the token", k)
		}
	}
	// A user's own esc is not a fault, and amber is never the answer: amber
	// means a human is needed, and a cut turn is not a question.
	if got := CutToken(CutInterrupt); got != TextTertiary {
		t.Errorf("an interrupt renders %s, want the chrome tier", got)
	}
	for k := CutKind(0); k < 40; k++ {
		if got := CutToken(k); got == Amber {
			t.Errorf("cut kind %d renders amber; a cut turn is not a question (5.16)", k)
		}
		if CutToken(k) >= tokenCount || CutHue(k) >= hueCount {
			t.Fatalf("cut kind %d produced an invalid token or hue", k)
		}
	}
	for k := CutKind(0); k < cutKindCount; k++ {
		if k.String() == "invalid" {
			t.Errorf("cut kind %d has no name", k)
		}
	}
}

// TestEnumsAreNamed: every value the package exports on an enum prints as a
// word. Test failures, logs and the golden harness all key on these, so an
// unnamed value would show up as a number in a diff someone has to read.
func TestEnumsAreNamed(t *testing.T) {
	for h := Hue(0); h < hueCount; h++ {
		if h.String() == "invalid" {
			t.Errorf("hue %d has no name", h)
		}
	}
	for s := State(0); s < stateCount; s++ {
		if s.String() == "invalid" {
			t.Errorf("state %d has no name", s)
		}
	}
	for f := Focus(0); f < focusCount; f++ {
		if f.String() == "invalid" {
			t.Errorf("focus %d has no name", f)
		}
	}
	if Hue(200).String() != "invalid" || State(200).String() != "invalid" || Focus(200).String() != "invalid" {
		t.Error("an out-of-range enum must say so rather than guess")
	}
}

// TestEveryConstructibleTokenIsValid. The palette accessors panic on a Token
// outside the inventory, which is the right contract for a programming error —
// but it is only safe because no exported constructor can produce one. This
// walks all of them.
func TestEveryConstructibleTokenIsValid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a constructible token panicked: %v", r)
		}
	}()
	check := func(tok Token) {
		_ = tok.Color(FocusNormal)
		_ = tok.Color(FocusDimmed)
		_ = tok.Class()
		_ = tok.String()
		_ = tok.Hex(FocusNormal)
		_ = tok.IsSurface()
		for p := Profile(0); p < profileCount; p++ {
			_ = tok.Fg(p, FocusNormal)
			_ = tok.Bg(p, FocusDimmed)
			_ = tok.Index(p, FocusNormal)
		}
	}
	for _, tok := range All() {
		check(tok)
	}
	for i := -20; i < 40; i++ {
		check(Identity(i))
		check(BandFor(Identity(i)))
	}
	for _, id := range []string{"", "wisp-parity", "a", "01JD8Z9K2QW5X7YV3B4N6M8P0R"} {
		check(IdentityFor(id))
		check(IdentityNext(id, Identity0))
	}
	for h := Hue(0); h < 40; h++ {
		for s := State(0); s < 40; s++ {
			check(ResolveToken(h, s))
		}
	}
	for i := -5; i < 300; i++ {
		check(TokenForANSI16(i))
	}
	for k := CutKind(0); k < 40; k++ {
		check(CutToken(k))
	}
	check(Promote(TextTertiary))
	check(Demote(TextPrimary))
	check(ContextToken(1, 2))
	check(ElapsedToken(1, 2))
}

// TestPaletteAccessorsRejectInvalidTokens documents the one contract in this
// package that is enforced by a panic rather than by a clamp, and why that is
// the right choice HERE and nowhere else.
//
// A Token outside the inventory cannot be produced by any exported constructor
// (TestEveryConstructibleTokenIsValid walks all of them), so reaching these
// paths means someone cast an integer — a programming error that should stop at
// the first frame rather than paint a wrong colour for a release. The
// boundaries that take values from OTHER packages clamp instead: [NewStyler],
// [Styler.PaintToken] and [ResolveToken] are all total, because a render must
// never die because a caller passed a stale enum.
func TestPaletteAccessorsRejectInvalidTokens(t *testing.T) {
	cases := map[string]func(){
		"Token.Color":   func() { _ = Token(tokenCount).Color(FocusNormal) },
		"Token.Class":   func() { _ = Token(tokenCount).Class() },
		"Token.String":  func() { _ = Token(200).String() },
		"Token.Fg":      func() { _ = Token(200).Fg(TrueColor, FocusNormal) },
		"invalid Focus": func() { _ = Amber.Color(Focus(focusCount)) },
		"invalid Profile": func() {
			_ = Amber.Fg(Profile(profileCount), FocusNormal)
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("an out-of-inventory value was accepted silently")
				}
			}()
			fn()
		})
	}
}

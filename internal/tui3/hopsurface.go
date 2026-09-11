package tui3

import "github.com/Agent-Field/aforge-v2/internal/tui2/tokens"

// hopSurfacePalette keeps the ordinary ladder where it already separates the
// card and its rows. At black or white the quieter direction runs out of room;
// the card then moves inward and gets its own local row grounds. The page's
// palette is a value and remains untouched.
func (p palette) hopSurfacePalette() (palette, hue) {
	base := measuredGround{r: p.ramp.cursor.r, g: p.ramp.cursor.g, b: p.ramp.cursor.b}
	if p.measured {
		base = p.ground
	}
	up := groundIsDark(base)
	ground := groundStep(base, 1.15, !up)
	baseIndex := nearestGrey256(base.r, base.g, base.b)
	// Byte rounding may soften the intended ratio slightly. Only a collapsed
	// plane reverses direction; ordinary dark themes retain their existing look.
	reversed := contrastOn(ground, luminanceOf(base.r, base.g, base.b)) < 1.10 ||
		(p.profile == tokens.ANSI256 && ground.idx == baseIndex)
	if reversed {
		ground = groundStep(base, 1.15, up)
		ground.idx = greyApart(ground.idx, baseIndex, up)
	}
	if reversed || p.ramp.cursor.idx == ground.idx || p.ramp.selected.idx == p.ramp.cursor.idx {
		panel := measuredGround{r: ground.r, g: ground.g, b: ground.b}
		up = groundIsDark(panel)
		p.ramp.cursor = groundStep(panel, groundCursorRatio, up)
		p.ramp.selected = groundStep(panel, groundSelectedRatio, up)
		p.ramp.cursor.idx = greyApart(p.ramp.cursor.idx, ground.idx, up)
		p.ramp.selected.idx = greyApart(p.ramp.selected.idx, p.ramp.cursor.idx, up)
	}
	return p, ground
}

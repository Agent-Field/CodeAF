package tokens

import "math"

const WorkLogoCount = 10
const WorkLogoWidth, WorkLogoHeight = 9, 1

// WorkLogoCell is a native, single-column glyph. No background painting or
// raster blocks are needed, so the mark survives every terminal font and theme.
type WorkLogoCell struct {
	Glyph rune
	Gold  bool
}

// The ball and paddles share one baseline. Holds at contact, a compressed ball
// and changing paddle angles give the small mark weight without moving its label.
var workMotions = [WorkLogoCount][28]string{
	// Rally: elastic contact, held apex, soft return.
	{
		"|●", "|●", "›●", "> •", "> •", ">  •", ">   •",
		">    •", ">     •", ">     •", ">      •", ">      •", ">      •", ">      •",
		">      •", ">      •", ">     •", ">     •", ">    •", ">   •", ">  •",
		"> •", "> •", ">●", "›●", "›●", "›●", "›●",
	},
	// Ping-pong: each paddle straightens only at its own contact.
	{
		"|●     <", "|●     <", "|●     <", "> •    <", "> •    <", ">  •   <", ">   •  <",
		">    • <", ">    • <", ">     ●|", ">     ●|", ">     ●|", ">     ●|", ">     ●|",
		">     ●|", ">     ●|", ">     ●|", ">    • <", ">    • <", ">   •  <", ">  •   <",
		"> •    <", "> •    <", "|●     <", "|●     <", "|●     <", "|●     <", "|●     <",
	},
	// Dribble: low, middle and high dot forms make a vertical bounce within one row.
	{
		"| ●", "| ●", "› •", "> ·", "> •", "> ˙", "> ˙",
		"> ˙", "> ˙", "> ˙", "> ˙", "> ˙", "› •", "| ·",
		"| ●", "| ●", "› •", "> ·", "> •", "> ˙", "> ˙",
		"> ˙", "> ˙", "> ˙", "> •", "> ·", "› ●", "| ●",
	},
	// Slingshot: slow preload, quick release, eased return.
	{
		"> ●", "> ●", "› ●", "|●", "<●", "<●", "<●",
		"|●", "› •", ">  •", ">   •", ">    •", ">     •", ">      •",
		">      •", ">      •", ">      •", ">      •", ">      •", ">     •", ">     •",
		">    •", ">    •", ">   •", ">   •", ">  •", ">  •", "> •",
	},
	// Juggle: staggered dot heights with a small catching gesture.
	{
		"> · • ˙", "> · • ˙", "> • • ˙", "› • ˙ •", "› • ˙ •", "> ˙ ˙ ·", "> ˙ • ·",
		"> ˙ • ·", "> ˙ · •", "> • · •", "> • · •", "> · • ˙", "> · • ˙", "> · • ˙",
		"> • ˙ •", "> • ˙ •", "› ˙ • ·", "› ˙ • ·", "> ˙ · •", "> ˙ · •", "> • · ˙",
		"> • · ˙", "> · • ˙", "> · • ˙", "> • • ˙", "> • ˙ •", "> • • ˙", "> · • ˙",
	},
	// Backflip: eight paddle orientations around a tossed ball.
	{
		">  •", ">  •", ">   •", ">   ˙", "╱    ˙", "╱    ˙", "|     ˙",
		"|     ˙", "╲     •", "╲     •", "<     •", "<     •", "<     •", "<     •",
		"╱     •", "╱     •", "|    ˙", "|    ˙", "╲   ˙", "╲   ˙", ">  •",
		">  •", "> ●", "> ●", "›●", "|●", "› ●", ">  •",
	},
	// Cradle: the end balls pass motion through the still pair.
	{
		"> •●●", "> •●●", ">˙ ●●", ">˙ ●●", ">˙ ●●", ">˙ ●●", ">˙ ●●",
		"> •●●", "> •●●", ">  •●", ">  ●•", ">  ●●•", ">  ●● •", ">  ●●  ˙",
		">  ●●  ˙", ">  ●●  ˙", ">  ●●  ˙", ">  ●●  ˙", ">  ●● •", ">  ●●•", ">  ●•",
		">  •●", "> •●●", "> •●●", "> •●●", "> •●●", "> •●●", "> •●●",
	},
	// Ripple: two expanding echoes leave the central ball still.
	{
		">   ●", ">   ●", ">   ●", ">  (●)", ">  (●)", ">  (•)", ">  ‹•›",
		">  ‹•›", "> ‹ • ›", "> ‹ • ›", ">‹  ·  ›", ">‹  ·  ›", ">   ·", ">   ·",
		">   •", ">   •", ">   ●", ">   ●", ">  (●)", ">  (•)", ">  ‹•›",
		"> ‹ • ›", ">‹  ·  ›", ">   ·", ">   ·", ">   •", ">   •", ">   ●",
	},
	// Accordion: the compression travels through three chevrons.
	{
		">›› •", ">›› •", "›>› •", "›>›  •", "››>  •", "››>   •", "||>   •",
		"||>    •", "|›>    •", "|›>    •", "››>    •", "››>    •", "›>›   •", "›>›   •",
		">››  •", ">››  •", ">|| •", ">||●", ">|›●", ">›› •", ">›› •",
		"›>›  •", "›>›  •", ">››  •", ">››  •", ">›› •", ">›› •", ">›› •",
	},
	// Infinity: the ball passes high then low across the chevron.
	{
		" • >", " • >", "  ˙>", "  ˙>", "   ˙", "   >˙", "   > ˙",
		"   >  ˙", "   >   •", "   >   •", "   >   •", "   >   •", "   >  ·", "   > ·",
		"   >·", "   ·", "  ·>", " • >", " • >", " • >", " ˙ >",
		"  ˙>", "   ˙", "   >˙", "   •", "  •>", " • >", " • >",
	},
}

// WorkLogo samples time without mutating state. A fixed nine-column field
// keeps the status words still; all studies loop over 2.8 seconds.
func WorkLogo(style int, seconds float64) [WorkLogoWidth]WorkLogoCell {
	if style < 0 || style >= WorkLogoCount {
		style = 0
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		seconds = 0
	}
	frames := workMotions[style]
	phase := math.Mod(seconds, 2.8) / 2.8
	text := frames[min(int(phase*float64(len(frames))), len(frames)-1)]
	var out [WorkLogoWidth]WorkLogoCell
	for i := range out {
		out[i].Glyph = ' '
	}
	for i, r := range []rune(text) {
		if i >= len(out) {
			break
		}
		out[i] = WorkLogoCell{Glyph: r, Gold: r == '●' || r == '•' || r == '·' || r == '˙'}
	}
	return out
}

// WorkLogoMark is the resting brand signature, shared with the animated glyphs.
func WorkLogoMark() [2]WorkLogoCell {
	return [2]WorkLogoCell{{Glyph: '>'}, {Glyph: '●', Gold: true}}
}

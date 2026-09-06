package tui3

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The whole surface's palette lives here and nowhere else.
//
// This file is the CURATED PASTEL AUTHORITY of docs/CHAT-V3.md Decision 11.
// The colours are authored as hex, dark-terminal first, and there is one rule
// above every choice: if a colour could be described as "bright", it is wrong.
//
//	role    hex       what it paints
//	ink     #C6CDDA   the body — what was said, and every tool's TARGET
//	live    #D8DEE9   the body WHILE IT IS STILL BEING SAID: the growing edge of
//	                  a streaming reply, one lightness step up the reading
//	                  ladder, which drains back to the ink when the turn settles
//	accent  #9DC3E6   THE ONE LIVE OR CHOSEN THING ON THE SCREEN — the person's
//	                  › glyph, the rail, and whatever is currently moving or
//	                  currently picked. NOT headings (see the accent budget)
//	muted   #7FA6C9   the same hue one step back: tool names, the spinner, and
//	                  every heading, band label and wordmark this surface writes
//	dim     #6B7280   everything the surface says about itself — stats, notes,
//	                  hunk markers, the status line
//	add     #A3BE8C   a diff's + lines, and a write's line count
//	del     #C67173   a diff's − lines
//	bad     #D08770   the ✗ of a call that failed — soft orange-red, not fire
//	ask     #C08FE8   THE QUESTION HUE, and nothing else (see below)
//	data    #91C5D4   the payload rule's datum — a model id, a figure, a key
//	                  chord inside a quiet line (see [hueData])
//
// The three backgrounds are a ladder of their own and are stated under THE
// GROUND LADDER below, because a ground is not an ink and the rules that govern
// one do not govern the other.
//
// Why hex rather than internal/tui2/tokens (which this file used to delegate
// to): tokens is the v2 identity ramp, tuned for a rail of coloured cards, and
// D11 asks this surface for a quieter one. Detection is still tokens' — the
// profile question ("what can this terminal say") has one answer in this tree
// and it is [tokens.DetectProfile]; only the answer to "which colour" moved
// here. The spinner frames stay tokens' too: they are a glyph set, not a hue.
//
// The fallback ladder, and what each rung costs:
//
//	TrueColor  the palette exactly as authored — 38;2;r;g;b
//	ANSI256    the nearest member of the xterm cube+grey ramp, computed once at
//	           init by [nearest256]. Pastels land on soft indices; nothing in
//	           the table resolves into 0-15, which are whatever the user's theme
//	           says they are
//	ANSI16     NO HUE AT ALL. The sixteen are the terminal's own theme and its
//	           reds and greens are loud by definition, so this rung answers with
//	           weight instead: bold for what leads, faint for what recedes,
//	           plain for the body. Every distinction D11 draws in colour is also
//	           drawn in text (+/−, ✗, "exit 2"), so nothing is lost but the tint
//	NoColor    no SGR at all, weight included: a terminal told not to style is
//	           not styled halfway
//
// ── THE FIFTH COLOUR, AND WHY IT IS THE ONLY ONE ──
//
// Violet is the identity wheel's question hue, and D11 reserves it: it is spent
// on the moment the agent is WAITING FOR A PERSON and on nothing else — the
// consent question, its glyph, its choices, and the word in the status line.
// Nothing decorative may take it, because its whole value is that seeing it
// anywhere means exactly one thing.
//
// #C08FE8 rather than nord's own #B48EAD, which is the hue this table would
// otherwise have borrowed: at that saturation nord's purple sits within a few
// values of the body ink on a dark terminal, and the defect being fixed here is
// a person who could not tell the surface was waiting for them. A question hue
// that has to be looked for is not a question hue.
//
// It is also not the softer #C3A6E6 this wave first authored, and the reason is
// the second rung of the ladder: #C3A6E6's nearest xterm-256 neighbour is 146,
// WHICH IS THE ACCENT'S. On every 256-colour terminal the question would have
// been painted the same colour as the person's own › glyph — the exact failure
// this hue exists to prevent, arriving through the fallback nobody looked at.
// #C08FE8 resolves to 140, which is a violet and is nothing else on this
// surface. Any future change here owes the same check.
//
// Its sixteen-colour degradation is `heavy` (bold), which is the same answer
// this table gives every hue that LEADS. The question is never carried by
// colour alone: the glyph is a "?", the status line says "waiting · your call"
// in words, and the choices name their keys.
//
// ── THE SIGNAL BAND, AND THE TWO TIERS THAT ARE NOT IN IT ───────────────────
//
// The table divides in two, and the division is the reason a surface can carry
// seven colours and still read calm.
//
// The SIGNAL hues — accent, add, del, bad, ask, warn — answer "what KIND of
// thing is this". None of them outranks the others, so none of them may be
// LIGHTER than the others: the eye reads lightness as figure and ground and
// hue as identity, so a palette whose signals sit at one lightness reads as a
// single grey field at a glance and only resolves into colours when a person
// actually looks. THE SIGNAL HUES SIT INSIDE A FIFTEEN-POINT HSL LIGHTNESS
// BAND, on both ladders, and TestTheSignalHuesAreIsoluminant holds them to it.
// Dark: L 61–76. Light: L 39–53. Anything authored into this table from here
// on owes that check as well as the 256-neighbour one.
//
// The READING tiers — live, ink, muted, dim — answer "how loudly is this being
// said", and they are a LADDER by construction: the sentence still arriving,
// the body, the surface's second voice, and the surface talking about itself.
// Lightness is the whole of their meaning, so the band deliberately does not
// govern them and the test excludes them by name rather than by silence.
//
// #C67173 rather than nord's own #BF616A, which this table carried for four
// waves, is the one move that band cost. At L 56.5 the minus lines of a diff
// sat a clear five points under everything else in the signal set and read as a
// dimmer class of fact than the plus lines beside them, which is not what a
// diff means. The hue and the saturation are held (H 354→359, S 42); only the
// lightness rose, to L 61, and the 256 neighbour was re-checked: 167, a brick
// red, one clear step from [hueBad]'s 173. The obvious alternative, L 62, lands
// on 168 — a pink — and a diff whose minus lines went pink on every
// 256-colour terminal is the fallback nobody looked at, again.
//
// ── THE GLARE LAW ───────────────────────────────────────────────────────────
//
// THE BODY MAY NOT BE THE BRIGHTEST THING ON THE SCREEN. The reading tier's top
// rung is the one colour a person looks at for minutes at a time, and on a dark
// terminal a white much above 11:1 stops being legible and starts being a lamp:
// the strokes halate, the counters fill in, and everything quieter beside it
// reads as switched off. Comfortable long-read contrast on the assumed grounds
// is 8–11:1, and TestTheBodyInkDoesNotGlare holds BOTH ladders to it.
//
// THERE IS EXACTLY ONE EXCEPTION AND IT IS NAMED: [hueLive], the tier a reply
// wears WHILE IT IS STILL ARRIVING, stands above the ceiling on purpose. The law
// is about a colour somebody reads for MINUTES; the live tier is transient by
// construction — it exists for the seconds a turn is streaming and drains back
// to the body ink the moment the turn settles, which is the only reason a
// paragraph is allowed to lead at all (THE ACCENT BUDGET above). An exception
// with no bound of its own is a hole rather than an exception, so live carries a
// ceiling of its own: IT MAY NEVER CLIMB BACK INTO THE WHITE THIS WAVE TOOK
// AWAY. tokens' own body tier #E6E6F0 measures 13.79:1 against the middle of the
// assumed dark range and is the glare this whole law was written about;
// TestTheLiveTierIsTheGlareLawsOneException holds live under it, and holds the
// STEP itself — live over ink — inside adaptive.go's [liveStep], so a future
// retune cannot push the streaming text into glare by widening the gap either.
//
// #C6CDDA rather than the #D8DEE9 this table carried for five waves. At 14.05:1
// against #101014 the ink was half again as bright as the person's own accent
// (9.26:1 at the middle of the range), so the loudest thing on a surface whose
// accent budget is ONE LIT ELEMENT PER SCREEN was the paragraph — and the budget
// bought nothing, because whatever it was spent on was outshone by the text
// around it. The move is the smallest one that fixes that: THE HUE IS HELD at
// 219°, the lightness comes down L 88.0 → 81.6, and the saturation eases 28 →
// 21 because a body white is the one colour here with no identity to carry, and
// a tint nobody can name is a tint paid for in contrast.
//
//	ground    #101014  #1a1b26  #1e1e2e
//	#D8DEE9   14.05    12.65    12.14   was: a lamp
//	#C6CDDA   11.88    10.70    10.27   is:  a page
//
// The 256 neighbour was re-checked, because that is where an unchecked change
// silently becomes a different colour. #C6CDDA resolves to 252, and 252 is
// claimed by nothing on either ladder. The near misses are worth naming: the old
// ink's own 254 is the LIGHT ladder's selected ground and 255 is its cursor
// step, so a body ink that drifted back up a rung would be sharing an index with
// furniture. Any future change here owes the same check.
//
// THE LADDER STILL READS AS A LADDER, which is the other half of the law: ink
// 10.70, muted 6.67, dim 3.54 against the middle of the assumed range — three
// clear steps, ink still a wide step above the second voice. Coming down far
// enough to be comfortable without arriving on top of [hueMuted] is the whole
// width of the move.
//
// The LIGHT ladder's body ink is untouched at #3B4252 — 10.06:1 against #FFFFFF
// and 8.73:1 against nord's #ECEFF4. It was measured against the same band and
// was already inside it, and A VALUE IN BAND IS NOT TOUCHED, which is the rule
// THE GROUND LADDER's own retune stated. The law's test walks both ladders
// regardless, so the light side cannot drift out of band unnoticed either.
//
// ── AND THE TRANSCRIPT REACHES IT THROUGH A SEAM ──
//
// Authoring the ink here is only half the fix. A model's markdown is rendered by
// internal/tui2/prose, which resolves every colour on the row from
// internal/tui2/tokens, whose body tier is #E6E6F0 — brighter again than the
// white this table used to carry. Two whites shared one screen and the louder
// one painted the thing people read most.
//
// So markdown.go hands prose a Styler carrying THIS ink
// ([tokens.Styler.WithBodyInk]; [hue.tokenColor] below is the conversion), and a
// reply's paragraphs, its headings and its inline code spans all come back
// wearing the value above. The v2 surface keeps tokens' own white, because the
// override travels on the Styler and never touches the table.
//
// ── THE ACCENT BUDGET ───────────────────────────────────────────────────────
//
// ONE LIT ELEMENT PER SCREEN. The accent is the loudest thing this palette can
// say, and its whole worth is that a person's eye goes to it without being
// asked — which is a budget, not a colour. Spend it twice and it buys nothing.
//
// So the accent marks THE ONE LIVE OR CHOSEN THING and nothing else. HEADINGS
// ARE NOT THAT: a heading is furniture, it is in the same place every time, and
// a column of lit headings is a screen with no answer to "where am I". They
// wear [hueMuted] — the same hue one step back, which reads as structure rather
// than as a summons. The person's own › glyph and the rail keep the accent
// because they are where the eye starts and where the work is.
//
// docs/DESIGN-LANGUAGE.md states the budget in prose. No test can hold it: a
// screen is composed at fifty call sites and "how many lit things does this
// frame have" is not a question a unit test can ask. It is held by review, and
// by the fact that it is written down in two places on purpose.

// tier16 is what a sixteen-colour terminal draws instead of a hue.
type tier16 uint8

const (
	flat  tier16 = iota // no attribute: the body
	heavy               // SGR 1: what leads
	quiet               // SGR 2: what recedes
)

// hue is one authored colour and its two degradations.
type hue struct {
	r, g, b uint8
	// idx is the nearest xterm-256 index, computed once at init.
	idx uint8
	// tier is the weight a sixteen-colour terminal gets instead of the hue.
	tier tier16
}

// tokenColor is one authored hue said in internal/tui2/tokens' own vocabulary.
//
// It exists for the ONE seam that crosses — [tokens.Styler.WithBodyInk], which
// is how a model's markdown comes back in this palette's body ink rather than in
// tokens' brighter own (see THE GLARE LAW above, and markdown.go). Nothing else
// in this package hands a colour out; the palette is closed in both directions.
//
// The 256 index is deliberately NOT handed across with it. tokens resolves the
// neighbour itself, with its own metric, and the two answers have to agree or a
// 256-colour terminal is back to two whites — so the agreement is ASSERTED, by
// TestTheTranscriptBodyWearsTheSurfacesOwnInk, rather than papered over by
// shipping our answer to a question we were not asked.
func (h hue) tokenColor() tokens.Color { return tokens.Color{R: h.r, G: h.g, B: h.b} }

// The table. Changing a colour is changing one line here, and nothing else in
// the package holds an escape sequence.
var (
	// hueInk is the body, and it is held DOWN rather than up: see THE GLARE LAW
	// above for why 11:1 is a ceiling and not a target, and for the 256 check
	// that goes with any change to this line.
	hueInk = mustHue("#C6CDDA", flat)
	// hueLive is THE READING TIERS' ONE STEP ABOVE THE BODY, and it exists for a
	// single moment: the prose of an assistant reply while that reply is still
	// arriving (render.go's [app.assistantRows]).
	//
	// INK SETTLES WHEN THE TURN ENDS. A streaming answer is THE ONE LIVE THING ON
	// THE SCREEN, which is precisely what THE ACCENT BUDGET above says may lead —
	// and the budget also says the accent itself may not be spent on it, because
	// nothing the model writes is ever painted in the person's own hue (render.go
	// states that law over the user entry). So the live moment is drawn the only
	// way left that says "this is the growing edge" without saying "this is a
	// different kind of thing": ONE LIGHTNESS STEP, on the READING ladder, in the
	// body's own hue. When the turn settles the entry re-renders at the body ink
	// and the brightness drains away — no spinner, no checkmark, no glyph added
	// and none taken away, which is the emptiness law kept through a transition
	// rather than around it.
	//
	// It is a READING tier and not a signal, so the fifteen-point isoluminant
	// band above deliberately does not govern it, exactly as it does not govern
	// ink, muted and dim: lightness IS the whole of its meaning.
	//
	// ── WHY THE HEX IS THE INK'S OWN OLD VALUE ─────────────────────────────────
	//
	// #D8DEE9 is what [hueInk] carried before the readability wave calmed the
	// body down to #C6CDDA, and taking it here is not a coincidence: a tier
	// defined as "one step above the body" has to be authored RELATIVE to the
	// body, and the step the body just vacated is the step that was already
	// measured, already inside the palette's "nothing bright" law, and already
	// proven readable for four waves. Live is the ink the surface used to speak
	// in; the settled body is the calmer ink it speaks in now.
	//
	// THE DEPENDENCY IS STATED RATHER THAN HIDDEN. This hue is only ever ONE STEP
	// above whatever [hueInk] carries, and if the two ever meet the effect is
	// simply ABSENT — a streaming reply then looks exactly as it looked before
	// this existed. That is the right failure and the tests are written to allow
	// it (settle_test.go asserts live ≥ ink, not live > ink), because the pair is
	// one retune: a live tier that leapt above an un-calmed body would be the
	// "bright" this file's first rule forbids. It is also what the adaptive
	// derivation does on a ground with no headroom left (adaptive.go).
	//
	// ── THE 256 NEIGHBOUR, CHECKED ─────────────────────────────────────────────
	//
	// #D8DEE9 resolves to 254, on the GREY RAMP rather than into the colour cube,
	// which is what a reading tier owes: a body that rounded into a tint would be
	// prose that looked like it meant something. Against the dark ladder's other
	// roles 254 is clear by a wide margin — the nearest occupied index in the
	// whole table is [hueDim]'s 243, and every signal hue lands in the cube
	// (140, 144, 146, 167, 173, 186, 110). The ONE index it comes near is
	// [hueInk]'s own, which is 252 now that the body has settled at #C6CDDA —
	// one clear step down the same grey ramp, which is the collision check
	// passing exactly when the effect exists and failing into absence when it
	// does not. #C08FE8's note above is why this check is written down and not
	// merely done.
	//
	// ── DEGRADATION ────────────────────────────────────────────────────────────
	//
	// The tier is `flat`, and that is the whole of the ANSI16 answer: BELOW THE
	// 256 RUNG THE EFFECT IS SIMPLY ABSENT. Every other hue in this table falls
	// back to weight, and this one may not — WEIGHT BELONGS TO MARKDOWN
	// (render.go), so bolding a live reply would make a streaming answer
	// indistinguishable from one whose author opened with a bold lead-in, and
	// faint would say the opposite of what the tier means. With no hue to spend
	// there is nothing honest to degrade to, so nothing is drawn. NO_COLOR is the
	// same answer for the ordinary reason: a terminal told not to style is not
	// styled halfway.
	hueLive   = mustHue("#D8DEE9", flat)
	hueAccent = mustHue("#9DC3E6", heavy)
	hueMuted  = mustHue("#7FA6C9", flat)
	// hueNarr is DEMOTED PROSE — the model's own words one rung back
	// (hierarchy.go's narration) — and it exists because those words used to wear
	// [hueMuted], which is the ACCENT one step back: a blue. Muted is right for
	// what it paints elsewhere — a tool's name, a heading, a band label, each a
	// word or two of LABEL — but narration is paragraphs, and paragraphs of blue
	// prose over an accent-blue question read as one blue field with the answer
	// somewhere inside it. Prose is a READING role, and the reading ladder's law
	// is that lightness is the whole of its meaning — so narration takes the
	// BODY'S own hue family (H 221 against the ink's 219) at a saturation a
	// person cannot name (S 16 against muted's 41), one lightness step under the
	// second voice: 5.2:1 against the middle of the assumed dark range, between
	// muted's 6.67 and dim's 3.54, so the ladder still reads as a ladder.
	//
	// The 256 neighbour is 103, which is claimed by nothing on the dark ladder
	// (TestNoTwoRolesShareA256Index now walks this line too). The tier is `flat`
	// for [hueMuted]'s reason: below the 256 rung the demotion is simply absent,
	// because WEIGHT BELONGS TO MARKDOWN and faint would say "surface's own
	// murmur", which narration is not.
	hueNarr = mustHue("#848FA6", flat)
	hueDim  = mustHue("#6B7280", quiet)
	hueAdd  = mustHue("#A3BE8C", heavy)
	hueDel  = mustHue("#C67173", quiet)
	hueBad  = mustHue("#D08770", heavy)
	hueAsk  = mustHue("#C08FE8", heavy)
	// hueWarn is the SIXTH colour, and since the places wave it carries two
	// readings that are the same reading: A BOUND ABOUT TO BE REACHED, and A
	// PERSON BEING WAITED ON. Home used to say the second of those in two colours
	// — the `needs you` mark and the answer chips in the violet of [hueAsk],
	// the "finished, needs your look" glyph in this amber — which meant the
	// screen had two ways to say the one thing a person is meant to act on. They
	// are settled here, on the colour that was already right for the glyph, and
	// they are the same reading because both are the machine saying "this stops
	// unless you do something". The chat's own consent block keeps [hueAsk]: a
	// question inside a conversation is a different object from a row on a list,
	// and moving it is the owner's call rather than this wave's.
	//
	// A deadline thirty seconds out is not a failure and
	// must not wear the failure hue — the call may still land — but it is no
	// longer a fact you can leave in the dim tier either, because it is the one
	// thing on the row that is about to change what happens. Nord's yellow, one
	// clear step from the orange-red of [hueBad] on the 256 rung so the two
	// tiers of the same warning never collapse into one colour.
	hueWarn = mustHue("#EBCB8B", heavy)
	// hueData is the SEVENTH colour, and it is the payload rule's own ink
	// (payload.go): the datum inside a quiet line — a model id, a figure, a key
	// chord — steps up into it. It exists because the first rung this table
	// tried was [hueInk], and ink is the BODY's colour: a "lifted" datum two
	// rows under a paragraph of ink read as ordinary text, which is the exact
	// defect the payload rule was written against, arriving one rung later. The
	// eye reads lightness as loudness and HUE as identity, so a datum needs a
	// hue of its own — the syntax-highlighting contract every calm terminal
	// theme already keeps: greyscale for prose, colour for identifiers.
	//
	// Nord's frost cyan, at L 70.0 inside the fifteen-point signal band, so a
	// line full of data still reads as one quiet field until somebody looks.
	//
	// ── WHY L 70 AND NOT NORD'S OWN L 67.5 ─────────────────────────────────────
	//
	// #88C0D0 is the hex nord authors and this table carried for four waves, and
	// on the rung where hues are rounded it was not a cyan at all: it resolves to
	// xterm-256 110, which is #87afd7 — A STEEL BLUE, and the SAME INDEX
	// [hueMuted] rounds to. So on every 256-colour terminal the datum wore the
	// second voice's own colour, which is the one thing a hue given out for
	// IDENTITY may not do: the payload rule lifts a model id out of a dim line by
	// giving it a hue of its own, and a lifted datum painted in the tier it was
	// lifted out of is the rule doing nothing while appearing to work.
	//
	// The move is the smallest one that fixes it and it is THE MOVE THIS FILE
	// ALWAYS MAKES: the hue is held at 193°, the saturation at 43%, and only the
	// LIGHTNESS rises, L 67.5 → 70.0. That crosses onto 116 — #87d7d7, which is
	// an actual cyan and is claimed by nothing on either ladder — and it is the
	// first index above #88C0D0 that is: L 69.0 still rounds to 110 and L 73.0 has
	// gone on to 152. The signal band is unmoved, because the spread it measures
	// is [hueDel]'s L 61.0 to [hueAccent]'s L 75.9 and this value sits inside
	// both ends. Contrast against the middle of the assumed dark range goes 8.54
	// → 9.07, which leaves it under the accent's 9.26 — a datum may not outrank
	// the person's own hue.
	//
	// 116 is also one clear step from the accent's 146, which was the collision
	// this line was originally checked against and remains the one that would
	// matter most: a datum painted the person's own colour would spend the accent
	// budget forty times a minute by rounding. Both checks are now written down
	// rather than merely done — TestNoTwoRolesShareA256Index walks the whole
	// table on both ladders, which is what would have caught this one.
	hueData = mustHue("#91C5D4", heavy)
	// hueViolet was the SHELL OPERATOR's hue until the transcript restraint
	// greyed shell grammar down to the reading tiers (shellx.go) — it is held
	// in the table, currently unspent, and it is deliberately NOT the question
	// hue above.
	//
	// The fifth colour's law is that seeing #C08FE8 means one thing — a person
	// is being waited on — so a pipe in a command line may not wear it. This is
	// a dimmer, greyer violet a whole tier below it: 97 rather than 140 in the
	// 256 fallback, so the two never collapse into each other on the rung where
	// hues get rounded. It is the one hue both ladders share, because it is
	// mid-tone by construction and reads on a dark terminal and a white page
	// alike.
	hueViolet = mustHue("#8F6FA8", quiet)
	// hueMoney is the EIGHTH colour, and it is the only one this table has ever
	// added for a single reading: what a thing cost.
	//
	// Until the places wave a figure in dollars was painted `dim` and lifted to
	// `warn` near the day's ceiling (pulse.go), which says "money is telemetry
	// until it is nearly a problem" — fine on one line of a status bar, wrong on
	// a page whose whole subject is spend, and wrong on a row where the cost sits
	// beside a project tag and an age and has to be findable among them. It could
	// not borrow [hueAdd]: that green means IT LANDED, and a table where the
	// finished tick and the bill are one colour is a table that says finishing
	// and paying are the same event.
	//
	// The hue is the mint the design's own token table names for money — H 144,
	// where [hueAdd]'s olive is H 92 — held at THIS table's lightness rather than
	// the token table's: L 69.0 sits inside the fifteen-point signal band beside
	// the accent's 75.9 and the diff-minus' 61.0, where the token green's 76.1
	// would have widened the band past its law. 115 on the 256 rung, one clear
	// step off [hueData]'s 116 and far from [hueAdd]'s 144, so the three greens
	// and blue-greens of this table never collapse into one another where hues
	// get rounded.
	hueMoney = mustHue("#90D0AA", heavy)
)

// ── THE GROUND LADDER ───────────────────────────────────────────────────────
//
// Everything above this line is an INK. What follows is the other kind of
// colour this surface draws: the GROUND under a row. There are four steps,
// they are the only four there will ever be, and they are named here once.
//
//	rest      NO GROUND AT ALL — the row is the terminal's own background
//	cursor    the row the pointer is over, or the row the cursor is on
//	selected  the chosen thing: the current row, the current chip
//	mark      a marked span: copy mode's selection, and the text a yank takes
//
// REST IS NOT A COLOUR, AND THAT IS THE LAW — ON THE CONVERSATION AND ON EVERY
// PLACE ALIKE. It has no entry in the tables below because there is nothing to
// author: an unremarkable row is painted by not painting it. This is the
// emptiness law wearing its background clothes — a surface that tinted every row
// would be a surface where a tint said nothing, and the three steps that DO say
// something are only legible because the fourth state is empty.
//
// ── THE PAINTED PAGE THAT WAS TRIED, AND WHAT THE OWNER SAID ABOUT IT ───────
//
// FIDELITY.md item 13 asked the places to bring their own ground — #12121A over
// every cell of home and the six places — on the reasoning that a surface which
// paints its own page no longer has to guess what is under it. It was built, and
// on 2026-08-25 the owner tested it and reversed it in one sentence: "i want bg
// color and text color to be same as in inside chat please this new bg looks
// weird i think we were taking user terminal stuff or something previously."
//
// So THE TERMINAL'S OWN BACKGROUND SHOWS THROUGH EVERYWHERE, and the reason the
// law originally gave for itself turns out to be the reading a person actually
// has: a ground this program authored is a ground laid over somebody's theme,
// and it looks like one. A place raises the cursor and selection steps of THIS
// ladder and paints nothing else. FIDELITY item 13 carries the reversal note.
//
// ── THE EMPHASIS LAW ────────────────────────────────────────────────────────
//
// A ROW IS EMPHASIZED BY RAISING ITS GROUND AND TURNING ITS LEADING TEXT
// ACCENT. NOTHING ELSE EVER CHANGES.
//
// No new colour arrives for the emphasized state, no run of bolding spreads
// across the row, and above all NO OUTLINE IS ADDED — emphasis is a step UP
// this ladder, never a ring drawn around a thing. The ground says which band
// of pixels is being spoken about; the accent on the leading glyph or word
// says what it is. Two moves, both already in this file, and a lane that finds
// itself reaching for a third has found a state this ladder does not have
// rather than a colour this table is missing.
//
// ── WHY THE STEPS ARE AUTHORED, AND WHEN THEY ARE DERIVED INSTEAD ───────────
//
// THE STEPS BELOW ARE DERIVED WHEN THE TERMINAL ANSWERS AND AUTHORED WHEN IT
// DOES NOT, and both halves exist because the two are answers to two different
// questions rather than a good way and a bad way of doing one thing.
//
// The honest way to build this ladder is the way a compositor builds it: take
// the background, move it away from itself at a stated ratio, and let every step
// inherit the theme's own hue for free. That needs a background, and for four
// waves this file had none. There is no variable that states it, and the one
// query that would — OSC 11 — is a round trip on a terminal that may never
// answer, which is not something a CONSTRUCTOR may wait on (see [detectTheme]
// for the same wall, met from the other side).
//
// What changed is not the wall, it is the door beside it. A background reply is
// an EVENT: [app.Init] asks with tea.RequestBackgroundColor and the answer, if
// there is one, arrives as a tea.BackgroundColorMsg on the same lane as every
// keystroke. Nothing blocks, there is no timer, and there is no deadline to get
// wrong. adaptive.go turns that one colour into this whole ladder —
// [adaptRamp] — and the surface repaints.
//
// So the values below are THE PERMANENT FALLBACK, and they are load-bearing:
// they are what a terminal that stays silent paints, which is every terminal
// that does not implement the query, every pipe, every recording, and every
// frame drawn between startup and the reply. A silent terminal is not a degraded
// one — it gets exactly the surface four waves of authorship aimed at it — and
// that is the whole reason the question can be asked at all.
//
// What is authored is aimed rather than guessed, and the aim is what adaptive.go
// derives AGAINST: it is the same three ratios either way, measured against a
// real ground when there is one and against an assumed range when there is not.
// The assumed ground is the range real dark terminals actually sit in, #101014
// through #1e1e2e, and the aim is the band a wide reading of calm terminal
// palettes converges on: the cursor step at ≈1.1–1.2:1 against that ground, the
// selected step at ≈1.35–1.5:1, and the marked span louder again because it is
// transient and spans many rows at once. Measured, at the middle of the assumed
// range:
//
//	step      dark      #101014  #1a1b26  #1e1e2e   256
//	cursor    #242932    1.30     1.17     1.09     235
//	selected  #2E3440    1.52     1.37     1.31     237
//	mark      #434C5E    2.20     1.98     1.90     239
//
//	step      light     #FFFFFF  #ECEFF4            256
//	cursor    #E5E9F0    1.22     1.06              255
//	selected  #D8DEE9    1.35     1.17              254
//	mark      #B7C0D1    1.83     1.59              251
//
// The cost of authoring is stated rather than hidden, and it is exactly the cost
// a reply pays off: a fixed ground reads one notch louder on a blacker terminal
// and one notch quieter on a lighter one, and on a tinted page like nord's own
// #ECEFF4 the whole light ladder drops close to invisible. That is the price of
// not knowing. It is still cheaper than a query that hangs — which is why the
// query does not hang, and why these values remain what a terminal that will not
// say gets.
//
// Two things survived this retune unchanged and both were deliberate. #2E3440
// is the value this file has drawn under the pointer since the day it first
// drew a background — it moves DOWN one rung to become the selected step, and
// the row a person has been looking at for four waves keeps its exact weight
// while the pointer's own step gets quieter. And the light ladder's first two
// steps are byte-identical to what they were, because they were already inside
// the band; a value in band is not touched.
//
// The last COLUMN of each table is what keeps the ladder honest below
// truecolor: all six steps resolve into the 256 palette's GREY RAMP — 235, 237,
// 239 climbing away from black and 255, 254, 251 descending off the page —
// rather than into its colour cube. A ground that rounded into a hue would be a
// tint that looked like it meant something, and no step on this ladder means
// anything by itself.
var (
	// hueCursor is the cursor step: the row the pointer is over, and the row a
	// keyboard cursor sits on. Nord's own polar night pulled toward the void —
	// the same H 220 / S 16 as [hueSelected], four points of lightness under it.
	hueCursor = mustHue("#242932", flat)
	// hueSelected is the selected step, and it is the value the pointer used to
	// wear. Two states of one row have to read as two states of one row, so the
	// three steps are one hue at three lightnesses and never three colours.
	hueSelected = mustHue("#2E3440", flat)
	// hueMark is the marked span: copy mode's selection. It is the loudest step
	// because it is the only one that is TRANSIENT and the only one that covers
	// many rows at once — a person holding a selection open is looking for its
	// two ends, and an end that has to be hunted for is not an end.
	hueMark = mustHue("#434C5E", flat)
)

// ── THE IDENTITY RING — NOT SPENT ANY MORE ──────────────────────────────────
//
// Six hues that mean NOTHING, which was once their design and is now the reason
// nothing on this surface paints with them.
//
// They were spent on one cell: the marker at the head of a task row, hashed off
// the id, so that ◆ teal was task 3 everywhere it appeared. The argument was that
// a person running four tasks needs to know WHICH ONE a row belongs to. What that
// cost was a private alphabet of eight shapes in six colours which had to be
// learned, was relearned every session because ids restart, and put arbitrary
// colour on a surface whose whole colour law is that colour means something —
// while the row already carried the id in a form a person can say out loud and
// the state in its own mark and its own word. So the marker is one shape, dim,
// and the same for every task (taskident.go states the change and why).
//
// THE HUES ARE LEFT HERE RATHER THAN DELETED because the palettes below are
// built as complete ramps and every tier declares one; an empty ring would be a
// hole in three tables to save six lines, and a structural test walks those
// tables field by field to prove the light ladder is derived from the dark one
// (adaptive_test.go). THE DOOR IS GONE, which is the half that mattered:
// `palette.ringPaint` was the only way to spend one of these and it has been
// removed, so nothing can paint an identity hue by reaching for it.
var taskRing = []hue{
	mustHue("#8FBCBB", flat), // teal
	mustHue("#81A1C1", flat), // steel
	mustHue("#B48EAD", flat), // mauve
	mustHue("#9CC49B", flat), // sage
	mustHue("#E0A96D", flat), // amber
	mustHue("#D08C9B", flat), // rose
}

// lightTaskRing is the same ring for a page: the same six angles, saturated and
// darkened, by the move the whole light ladder makes.
var lightTaskRing = []hue{
	mustHue("#3E7C7B", flat),
	mustHue("#4C6E92", flat),
	mustHue("#7E5A79", flat),
	mustHue("#4F7A4E", flat),
	mustHue("#A06A2C", flat),
	mustHue("#97505F", flat),
}

// ── THE LIGHT LADDER ────────────────────────────────────────────────────────
//
// The table above is dark-terminal first and was, for four waves, the only
// table there was. A person on a white terminal got soft pastels authored
// against black: #C6CDDA body ink on #FFFFFF is very nearly invisible, and the
// dim tier below it is invisible outright.
//
// So there is a second ladder, authored the same way and against the same law —
// nothing bright — but for a page rather than for a void. The moves are the
// obvious ones and they are all the same move: what carried by being LIGHTER
// than the background now carries by being DARKER than it.
//
//	role    dark      light     what changed
//	ink     #C6CDDA   #3B4252   the body inverts: near-black on the page. Both
//	                            ends sit inside THE GLARE LAW's 8–11:1 band
//	live    #D8DEE9   #2E3440   the streaming step travels the other way too:
//	                            a growing edge LEADS by having more contrast
//	                            against the ground, which is lighter on a void
//	                            and DARKER on a page
//	accent  #9DC3E6   #5E81AC   the pastel blue saturates; a pastel on white
//	                            is a smudge
//	muted   #7FA6C9   #8098B8   accent, one step back, on both ladders
//	dim     #6B7280   #9AA3B2   the meta tier goes LIGHTER, not darker: it
//	                            recedes toward the page
//	add     #A3BE8C   #7BA23F   nord's green has no contrast on white
//	del     #C67173   #B55B64   already dark enough; barely moves
//	bad     #D08770   #C57A3C   soft orange-red, one step down
//	ask     #C08FE8   #6F3FA8   THE QUESTION HUE, inverted rather than dimmed:
//	                            it has to lead on a page too
//	warn    #EBCB8B   #A6791F   a pale yellow is nothing on white; the page
//	                            wants the same warning as dark amber
//	data    #91C5D4   #2C8A9E   the datum's cyan, deepened for the page the
//	                            way the accent was
//	violet  #8F6FA8   #8F6FA8   the shared one (above)
//
// The grounds invert the same way and are stated with the rest of THE GROUND
// LADDER above, not here: they are one ladder read from both ends, and a
// ladder split across two comments is a ladder that drifts.
//
// Every light index was checked against its neighbours the way #C08FE8 was:
// no two roles in this ladder resolve to the same xterm-256 index, because the
// 256 rung is where an unchecked pair silently becomes one colour. bundle_test
// asserts it, and any future change here owes the same check.
var (
	lightInk = mustHue("#3B4252", flat)
	// lightLive is [hueLive] on a page, and it makes the move the whole light
	// ladder makes: what led by being LIGHTER than a void leads by being DARKER
	// than a page. Nord's polar night 0 under the body's polar night 1 — the same
	// hue at the next authored step, so the settling reads as one ink drying and
	// never as two colours.
	//
	// 237 on the 256 rung, one clear step off [lightInk]'s 238, and on the grey
	// ramp where every reading tier belongs. Nothing else on this ladder is near
	// it: the light grounds climb the other end of the ramp (251, 254, 255) and
	// every light signal lands in the cube. The dark ladder's [hueSelected] is
	// also 237, and that is not a collision — it is a GROUND on the other ladder,
	// and the two ladders meet nowhere (see THE GROUND LADDER).
	lightLive   = mustHue("#2E3440", flat)
	lightAccent = mustHue("#5E81AC", heavy)
	lightMuted  = mustHue("#8098B8", flat)
	// lightNarr is [hueNarr] on a page, and it makes the reading ladder's move:
	// receding on white is going LIGHTER, so it sits between the light muted
	// (2.96:1 on white) and the light dim (2.55:1) at 2.7:1 — the same rung of
	// the same ladder, read from the other end. The near-neutral grey is
	// deliberate: this zone of the cube is crowded (103 and 109 are already the
	// light muted's and the light dim's), and a slate with any more blue in it
	// rounds onto one of them; the grey ramp's 247 is claimed by nothing.
	lightNarr = mustHue("#9E9EA4", flat)
	lightDim  = mustHue("#9AA3B2", quiet)
	lightAdd  = mustHue("#7BA23F", heavy)
	lightDel  = mustHue("#B55B64", quiet)
	lightBad  = mustHue("#C57A3C", heavy)
	lightAsk  = mustHue("#6F3FA8", heavy)
	lightWarn = mustHue("#A6791F", heavy)
	// The data hue inverted for the page, the same way the accent was: deeper
	// and less saturated rather than pale. L 39.6 sits inside the light signal
	// band (warn's 38.6 is its floor), and 31 on the 256 rung collides with
	// nothing — the near miss was #31859C, whose neighbour is 67, one step
	// from the light accent's own index.
	lightData = mustHue("#2C8A9E", heavy)
	// The money hue inverted for the page, the same way the data hue was: the
	// same mint at H 144, deepened rather than paled, because on white what leads
	// is what is DARKER. L 45.1 sits in the middle of the light signal band —
	// warn's 38.6 is its floor and del's 53.3 its ceiling — and 71 on the 256
	// rung is claimed by nothing (the light accent's own index is 67).
	lightMoney = mustHue("#34B266", heavy)

	// The light ladder's own three grounds. THE STEPS ARE THE SAME THREE STEPS
	// (see THE GROUND LADDER above) and they carry the same names — what changes
	// is only the direction of travel: a step up on a dark terminal is a step
	// DOWN off the page here, and the two ladders meet nowhere.
	lightCursor   = mustHue("#E5E9F0", flat)
	lightSelected = mustHue("#D8DEE9", flat)
	// The marked span goes one further off the page than the selected step, and
	// it is the only light ground this wave had to author: the first two were
	// already inside the band they were aimed at.
	lightMark = mustHue("#B7C0D1", flat)
)

// ── THE PLACE LADDER ────────────────────────────────────────────────────────
//
// Everything above this line is the CONVERSATION's palette. Home and the six
// places beside it (pages.go) PAINT FROM IT TOO, and this block is the record of
// why there is no second table any more.
//
// FIDELITY.md item 1 asked for the design's own nine hexes on the places: three
// greys (#E6E6F0 / #A0A6BB / #7C8296), three colours (amber #EECE96, cyan
// #A4D7EA, green #A2E2BC) and three grounds (#12121A / #1D1D28 / #262633). It
// was built exactly, with place-scoped variants of the glare law, the isoluminant
// band and the ground ladder to admit it. Then the owner ran it, on 2026-08-25:
//
//	"i want bg color and text color to be same as in inside chat please this new
//	 bg looks weird i think we were taking user terminal stuff or something
//	 previously"
//
// THE INKS ARE THE CHAT'S, MAPPED BY ROLE. That is the whole of what a place
// palette is now, and it is why [placeRampFrom] is nine lines rather than a
// table: every role a place paints has a reading the conversation already
// authored for the same reading, so the design's own semantics survive without a
// single new colour.
//
//	the design's role   what a place asks for   the hue it gets
//	the subject         p.ink (bold for tier 1) [hueInk]     #C6CDDA
//	what is true of it  p.muted                 [hueMuted]   #7FA6C9
//	demoted prose       p.narr                  [hueNarr]    #848FA6
//	the margin          p.dim                   [hueDim]     #6B7280
//	NEEDS A HUMAN       p.warn / p.ask          [hueWarn]    #EBCB8B
//	ALIVE               p.accent                [hueAccent]  #9DC3E6
//	MONEY               p.money                 [hueMoney]   #90D0AA
//
// THE DESIGN'S THREE MEANINGS ARE KEPT AND ONLY THE HEXES ARE THE CHAT'S: amber
// is a person being waited on, the accent is work in flight this instant, and
// green is a figure in dollars. Home already warned in [hueWarn] before the
// fidelity wave, so the amber reading did not move at all — only the two points
// of lightness the design's own token table carried.
//
// ── THE ONE-ACCENT LAW, AS THE DESIGN'S OWN PREAMBLE STATES IT ──────────────
//
// The designer wrote it down and then flagged it as unresolved: "one accent per
// screen vs. two live states. I read the law as one accent per meaning — amber
// for waiting on a person, cyan for in flight, and nothing else in colour."
//
// That reading survives the reversal, because it is arithmetic about how many
// meanings a screen carries rather than a claim about which hexes carry them. So
// a place still RETIRES two of the conversation's roles rather than merely
// re-pointing them ([placeRampFrom]):
//
//   - [hueData]'s cyan — the payload rule's identity ink — becomes the body ink
//     on a place, because a lifted datum on a screen with three meanings lifts by
//     being the SUBJECT tier, which is what SCREEN 2a says every hierarchy step
//     past the fourth must do.
//   - [hueAsk]'s violet becomes the amber, because home used to say "a person is
//     needed" in two colours and the design says it in one.
//   - [hueAdd]'s olive — the tick on work that landed — becomes the second voice,
//     because the glyph already says it landed and a hue saying it again spends
//     the screen's colour budget on the least urgent fact on it.
//
// The conversation keeps all three, untouched: a question inside a conversation
// is a different object from a row on a list.
//
// The ONE hue that survives outside the design's three is [hueBad]'s orange-red.
// It is a deviation and it is flagged rather than hidden: the design's own token
// table has a Coral for broken, its preamble does not name it, and a failed row
// stripped of every non-glyph signal is a row whose failure is carried by one
// cell of punctuation. It stays until the owner says otherwise.
//
// ── AND THE GROUND IS THE TERMINAL'S, ON A PLACE AS IN A CHAT ───────────────
//
// The painted page is gone with the inks that were authored against it. THE
// GROUND LADDER above is the only ladder, on every surface: rest is not a colour,
// and a place lifts a row off the terminal's own background only where a person
// put a pointer or a cursor on it ([hueCursor], [hueSelected], [hueMark]).
//
// The NoColor and plain profiles therefore need no special case here at all —
// there is nothing to suppress — and TestEveryPlaceHoldsAtThePlainFloor still
// walks every place with both floors on, because "legible as text with every
// escape removed" is a law about the layout rather than about the ground.

// ramp is one whole ladder: every role this surface paints, resolved once.
//
// The palette holds a ramp rather than reading the package vars directly, which
// is the entire mechanism of the light theme — every p.ink(), p.dim() and
// p.cursor() call site in the package was already going through the palette, so
// the second ladder cost the call sites nothing.
type ramp struct {
	ink, accent, muted, dim hue
	// narr is the reading ladder's rung for DEMOTED PROSE — the model's own
	// narration, one step under the second voice ([hueNarr]). It is a reading
	// tier, not a signal: the isoluminant band does not govern it.
	narr hue
	// live is the reading ladder's one step ABOVE the body: the prose of a reply
	// that is still arriving ([hueLive]). It sits beside ink rather than in a
	// table of its own because it is the same ladder — the body, said louder for
	// as long as it is still being said.
	live               hue
	add, del, bad, ask hue
	warn               hue
	data               hue
	violet             hue
	// money is what a thing cost ([hueMoney]). It is named apart from `add` for
	// the reason that hue's own note gives: landing and paying are two events.
	money hue
	// The three drawable steps of THE GROUND LADDER. The fourth step, rest, is
	// not here and cannot be: it is the absence of a paint, not a colour.
	cursor, selected, mark hue
	fade                   [3]hue
	// ring is the identity ring (above): not a role, and the only thing on the
	// ladder that is a list rather than a colour. It is named apart from the
	// ladder's `mark` step deliberately — a ring hue says WHICH work a row
	// belongs to and a marked ground says a person has selected a span, and the
	// day those two words meant the same thing on this surface is the day one of
	// them stopped meaning anything.
	ring []hue
}

var darkRamp = ramp{
	ink: hueInk, live: hueLive, accent: hueAccent, muted: hueMuted, narr: hueNarr, dim: hueDim,
	add: hueAdd, del: hueDel, bad: hueBad, ask: hueAsk, warn: hueWarn,
	data: hueData, violet: hueViolet, money: hueMoney, fade: thoughtFade,
	cursor: hueCursor, selected: hueSelected, mark: hueMark,
	ring: taskRing,
}

var lightRamp = ramp{
	ink: lightInk, live: lightLive, accent: lightAccent, muted: lightMuted, narr: lightNarr, dim: lightDim,
	add: lightAdd, del: lightDel, bad: lightBad, ask: lightAsk, warn: lightWarn,
	data: lightData, violet: hueViolet, money: lightMoney, fade: lightFade,
	cursor: lightCursor, selected: lightSelected, mark: lightMark,
	ring: lightTaskRing,
}

// lightFade is the thinking window's gradient on a page. It fades toward WHITE
// rather than toward black — the gradient's whole job is "this line is on its
// way out", and on a light terminal the way out is up, not down.
// placeRampFrom is what home and the six places paint from: THE LADDER THE
// PALETTE IS ALREADY HOLDING, with the three roles THE ONE-ACCENT LAW retires
// pointed at readings that table already carries.
//
// IT AUTHORS NO COLOUR AND IT TAKES NO TABLE OF ITS OWN, and both halves of that
// are the point. The owner asked for the chat's inks on the places, and the
// shortest way to keep that true forever is for there to be nothing here to
// retune — every role a place does not retire (the failure hue, the diff pair,
// the identity ring, the three ground steps, the thinking fade) is the same
// colour on both surfaces because it is LITERALLY the same value.
//
// It takes the ramp rather than naming [darkRamp] or [lightRamp] so that a
// MEASURED terminal reaches the places too: adaptive.go derives a whole ladder
// from the background the terminal reported and hands it to the palette, and a
// place that reached past it for the authored table would be the one surface in
// the program that ignored the answer.
//
// `ask` AND `warn` BECOME ONE COLOUR. On a place, "a person is being waited on"
// and "a bound is about to be reached" are the same sentence — something stops
// here unless you do something — and the design paints them with one hue. The
// amber is the one the conversation already warns in, which is the hue home
// already used for the needs-your-look mark before any of this. The conversation
// keeps its violet question and its yellow warning as two.
func placeRampFrom(base ramp) ramp {
	out := base
	out.ask = base.warn
	// `data` becomes the body ink: the payload rule lifts a datum out of a quiet
	// line, and on a screen with three meanings it lifts by being the SUBJECT
	// rather than by taking a fourth colour.
	out.data = base.ink
	// `add` becomes the second voice: the tick already says the work landed.
	out.add = base.muted
	return out
}

var lightFade = [3]hue{
	liftOf(lightDim, fadeOldest),
	liftOf(lightDim, fadeMiddle),
	liftOf(lightDim, fadeNewest),
}

// liftOf is [fadeOf]'s mirror: one hue at pct opacity over WHITE.
func liftOf(h hue, pct int) hue {
	mix := func(c uint8) uint8 { return uint8((int(c)*pct + 255*(100-pct) + 50) / 100) }
	r, g, b := mix(h.r), mix(h.g), mix(h.b)
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: h.tier}
}

// ── THE THEME SEAM ──────────────────────────────────────────────────────────
//
// theme is which ladder a surface paints from.
type theme uint8

const (
	// themeAuto asks the terminal, and falls back to dark. See [detectTheme].
	themeAuto theme = iota
	themeDark
	themeLight
)

// themeFromRow turns a settings row's value into a theme. It is THE SEAM, and
// it is a seam rather than a wire because the registry row does not exist yet:
// internal/config owns the rows, this package owns the ladders, and the day the
// row lands (a Display tab entry beside the nerd-font tier) it is one call —
// `newThemedPalette(profile, ascii, themeFromRow(settings.Get("display.theme")))`
// — and nothing else in this package moves.
//
// Anything unrecognized is auto, which is the honest answer to a row somebody
// spelled wrong: ask the terminal rather than pin the wrong ladder.
func themeFromRow(row string) theme {
	switch strings.ToLower(strings.TrimSpace(row)) {
	case "dark":
		return themeDark
	case "light":
		return themeLight
	default:
		return themeAuto
	}
}

// detectTheme is the auto answer: COLORFGBG, and nothing else.
//
// There is exactly one thing a terminal will tell you about its background
// without being interrogated, and it is this variable — "15;0" is light-on-dark,
// "0;15" is dark-on-light. The field that matters is the LAST one (some
// terminals send three, with the cursor colour in the middle), read as an ANSI
// index: 0-6 and 8 are the dark half of the sixteen, everything else is light.
//
// The other way to ask — OSC 11, a query and a reply parsed off the input
// stream — is still deliberately not done HERE, and for the reason it never was:
// it is a round trip on a terminal that may never answer, and this is a
// constructor. It is asked one layer out instead, where an answer is an EVENT
// and silence costs nothing (adaptive.go's [app.groundReply]), and this function
// is what paints until it lands and what keeps painting if it never does. Unset
// means dark, which is what this surface has always assumed and what most
// terminals are.
func detectTheme(env func(string) string) theme {
	if env == nil {
		return themeDark
	}
	value := strings.TrimSpace(env("COLORFGBG"))
	if value == "" {
		return themeDark
	}
	fields := strings.Split(value, ";")
	background, err := strconv.Atoi(strings.TrimSpace(fields[len(fields)-1]))
	if err != nil {
		return themeDark
	}
	if background >= 0 && background <= 6 || background == 8 {
		return themeDark
	}
	return themeLight
}

func rampFor(t theme, env func(string) string) ramp {
	if t == themeAuto {
		t = detectTheme(env)
	}
	if t == themeLight {
		return lightRamp
	}
	return darkRamp
}

// ── THE THINKING WINDOW'S FADE ──────────────────────────────────────────────
//
// While a model reasons, the last three lines of its working are on screen and
// nothing else (thinking.go). Three lines of identical dim text is a paragraph
// that has to be READ to learn which end of it is new, so the window is painted
// as an OPACITY GRADIENT instead: the oldest visible line furthest toward the
// background, the newest at the dim tier it will keep when it settles.
//
// The stops are the dim ink at three opacities over black — for a terminal that
// has not said what its background is, and every terminal this palette was
// authored for is dark, so black is the honest anchor. Where one DOES say, the
// same three opacities are composited over the colour it named instead
// (adaptive.go's [fadeToward]), and the gradient ends on the real page rather
// than near it. Naming the opacities rather than the colours is what makes that
// swap one line: change hueDim, or measure a ground, and the fade follows either
// way, which is what stops the gradient drifting off the tier it belongs to.
//
//	35%  #25282D  the oldest line — read already, on its way out
//	60%  #40444D  the middle
//	85%  #5B616D  the newest, one step under the settled block's own dim
//
// Below ANSI256 there is no hue to fade — the sixteen are the user's theme —
// and the window falls back to the dim tier's weight, which is what the block
// has always worn. NO_COLOR gets three plain lines: the newest is still last,
// which is the fact the gradient was drawing.
const (
	fadeOldest = 35
	fadeMiddle = 60
	fadeNewest = 85
)

// thoughtFade is the ramp, oldest first. It is derived at init rather than
// authored so that the hexes in the table above can be ASSERTED (thinking_test)
// instead of maintained by hand.
var thoughtFade = [3]hue{
	fadeOf(hueDim, fadeOldest),
	fadeOf(hueDim, fadeMiddle),
	fadeOf(hueDim, fadeNewest),
}

// fadeOf is one hue at pct opacity over black, rounded rather than truncated:
// truncation loses a whole value on two of the three stops and the ramp's job
// is that its steps are even.
func fadeOf(h hue, pct int) hue {
	mix := func(c uint8) uint8 { return uint8((int(c)*pct + 50) / 100) }
	r, g, b := mix(h.r), mix(h.g), mix(h.b)
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: h.tier}
}

// mustHue parses an authored "#RRGGBB" and resolves its 256-colour neighbour.
// It panics on a malformed literal, which is a compile-time mistake caught at
// init rather than a colour that silently renders as black.
func mustHue(hex string, tier tier16) hue {
	r, g, b, ok := parseHex(hex)
	if !ok {
		panic("tui3: malformed palette colour " + hex)
	}
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: tier}
}

func parseHex(hex string) (r, g, b uint8, ok bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(value >> 16), uint8(value >> 8), uint8(value), true
}

// cubeLevels are the six values of the xterm 6×6×6 colour cube.
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// nearest256 is the closest xterm-256 index to an authored colour, searched
// across the cube (16-231) and the 24-step grey ramp (232-255) and never the
// first sixteen — those are the user's theme, not a colour we chose.
//
// The metric is plain squared RGB distance. A perceptual one (CIE76 and up)
// would be defensible, but the table is seven low-saturation colours that are
// each far from their runners-up, and a colour-science dependency for a
// distance that does not change the answer is a dependency for nothing.
func nearest256(r, g, b uint8) uint8 {
	best, bestDist := 0, 1<<30
	consider := func(index, cr, cg, cb int) {
		dr, dg, db := int(r)-cr, int(g)-cg, int(b)-cb
		if d := dr*dr + dg*dg + db*db; d < bestDist {
			best, bestDist = index, d
		}
	}
	for ri, rv := range cubeLevels {
		for gi, gv := range cubeLevels {
			for bi, bv := range cubeLevels {
				consider(16+36*ri+6*gi+bi, rv, gv, bv)
			}
		}
	}
	for i := 0; i < 24; i++ {
		v := 8 + 10*i
		consider(232+i, v, v, v)
	}
	return uint8(best)
}

// palette paints one terminal's worth of this table.
type palette struct {
	profile tokens.Profile
	// ascii is the glyph floor: a terminal that cannot be trusted with box
	// drawing gets "+-> " where the rail would be. It gates GLYPHS only —
	// colour is the profile's business, and the two questions are independent
	// (a truecolor terminal in a C locale is a real terminal).
	ascii bool
	// ramp is the ladder this palette paints from — dark, or light.
	ramp ramp
	// pin is the theme this palette was CONSTRUCTED with, kept rather than
	// discarded so that a measured background can be told apart from a person.
	// A reply from the terminal re-derives every value on the ladder either way,
	// but it may only choose WHICH ladder when the answer was auto — somebody who
	// said "light" out loud outranks a terminal that reports otherwise
	// (adaptive.go's [app.groundReply]).
	pin theme
	// ground is the terminal's own background once it has said what it is, and
	// measured says it has. They are the seam between the authored palette and
	// the derived one: unset is the honest state of every terminal that has not
	// answered and of every terminal that never will, and it is the state the
	// whole of styles.go was written for.
	ground   measuredGround
	measured bool
	// light is which of the two authored ladders this palette started from, kept
	// so [palette.onPlaces] can answer the same question without re-detecting.
	light bool
	// linear is the screen-reader tier (Options.Linear): no motion, no pointer.
	// It gates the two paints that mean neither of those things to a reader —
	// the thinking window's gradient and the hover background — because a
	// gradient is an animation frozen in space and a hover is a pointer's
	// shadow, and a surface being read aloud has neither.
	linear bool
}

func newPalette(p tokens.Profile, ascii bool) palette {
	return palette{profile: p, ascii: ascii, ramp: darkRamp}
}

// newThemedPalette is [newPalette] with the ladder said out loud. It is what
// the settings row will call through [themeFromRow]; detection is the default
// and pins are the exception, which is the same shape every other display
// question on this surface has.
func newThemedPalette(p tokens.Profile, ascii bool, t theme, env func(string) string) palette {
	pal := newPalette(p, ascii)
	pal.ramp = rampFor(t, env)
	pal.pin = t
	pal.light = resolveTheme(t, env) == themeLight
	return pal
}

// resolveTheme is [rampFor]'s question without its answer: which of the two
// authored ladders a theme setting actually lands on. It is split out so the
// ladder and the flag that remembers WHICH ladder can never disagree.
func resolveTheme(t theme, env func(string) string) theme {
	if t == themeAuto {
		return detectTheme(env)
	}
	return t
}

// onPlaces is THE PLACE LADDER'S ONE DOOR: the same terminal, the same profile,
// the same glyph floor, the same inks, with the three roles THE ONE-ACCENT LAW
// retires pointed elsewhere ([placeRampFrom]).
//
// It is called once per frame, by the two functions that draw a whole place —
// pages.go's [placeFrameWithBar] and home.go's [app.homeFrame] — and it is a
// VALUE returned rather than a field mutated, so a caller that forgets to put
// the old palette back gets a compile-time nothing rather than a conversation
// painted in home's colours.
//
// A MEASURED GROUND REACHES THE PLACES EXACTLY AS IT REACHES THE CONVERSATION,
// and that is a consequence of the reversal rather than a special case: this
// swap re-points three roles of whatever ladder the palette is already holding
// — adaptive.go's derived one included — and touches nothing else, so a place
// stands on the terminal's own background wearing the terminal's own ladder.
func (p palette) onPlaces() palette {
	p.ramp = placeRampFrom(p.ramp)
	return p
}

// detectPalette reads the terminal the way the rest of the tree does — through
// the environment the app was given ([Options.Env]), never the process's own,
// so the profile, the glyph veto and the theme all come from the same table as
// every other fact the surface takes from the shell.
func detectPalette(env func(string) string) palette {
	return newThemedPalette(
		tokens.DetectProfile(env), detectASCII(env), themeAuto, env)
}

// detectASCII decides whether this surface may draw box-drawing characters.
//
// Like tokens' glyph detection it may only VETO: there is no escape sequence
// that answers "can you draw U+251C", so the answer is yes unless something
// says otherwise. The two vetoes are the ones that are actually knowable — a
// terminal that made no capability claim at all, and a locale that is not
// UTF-8, where a multi-byte rune arrives as mojibake rather than as a rail.
//
// There is deliberately no environment pin here. A human override of a terminal
// veto is a Display setting (internal/config's registry already fronts the
// nerd-font tier that way, and the repo's completeness gate says any new pin
// arrives as a row); inventing an AFORGE_* variable for this one surface would
// be a second door onto the same question.
func detectASCII(env func(string) string) bool {
	if env == nil {
		return true
	}
	term := strings.ToLower(strings.TrimSpace(env("TERM")))
	if term == "" || term == "dumb" {
		return true
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.TrimSpace(env(key)); v != "" {
			return !strings.Contains(strings.ToUpper(v), "UTF-8") &&
				!strings.Contains(strings.ToUpper(v), "UTF8")
		}
	}
	// No locale set at all is the POSIX C locale in every shell that matters,
	// and the C locale is not UTF-8.
	return true
}

// paint wraps text in the escape sequence one hue asks for on this terminal.
func (p palette) paint(s string, h hue) string {
	if s == "" || p.profile == tokens.NoColor {
		return s
	}
	switch p.profile {
	case tokens.TrueColor:
		return "\x1b[38;2;" + itoa(int(h.r)) + ";" + itoa(int(h.g)) + ";" + itoa(int(h.b)) + "m" +
			s + "\x1b[39m"
	case tokens.ANSI256:
		return "\x1b[38;5;" + itoa(int(h.idx)) + "m" + s + "\x1b[39m"
	default:
		switch h.tier {
		case heavy:
			return "\x1b[1m" + s + "\x1b[22m"
		case quiet:
			return "\x1b[2m" + s + "\x1b[22m"
		default:
			return s
		}
	}
}

func (p palette) ink(s string) string { return p.paint(s, p.ramp.ink) }

// live is the body ink for as long as the body is still being written: the
// growing edge of a streaming reply, one lightness step above where the same
// words will sit the moment the turn settles ([hueLive]). Below the 256 rung it
// paints nothing at all, which is deliberate and is stated at [hueLive].
func (p palette) live(s string) string { return p.paint(s, p.ramp.live) }

func (p palette) accent(s string) string { return p.paint(s, p.ramp.accent) }
func (p palette) muted(s string) string  { return p.paint(s, p.ramp.muted) }

// narr is demoted prose — the model's own narration, one reading rung under the
// second voice and in the body's own hue family rather than the accent's
// ([hueNarr]). hierarchy.go's working tier is the only thing that wears it.
func (p palette) narr(s string) string { return p.paint(s, p.ramp.narr) }

func (p palette) dim(s string) string { return p.paint(s, p.ramp.dim) }
func (p palette) add(s string) string { return p.paint(s, p.ramp.add) }
func (p palette) del(s string) string { return p.paint(s, p.ramp.del) }
func (p palette) bad(s string) string { return p.paint(s, p.ramp.bad) }

// warn is the tier below bad: something is about to go wrong rather than has
// (styles.go's [hueWarn]). The only thing that wears it today is a timeout with
// seconds left on it (toolview.go).
func (p palette) warn(s string) string { return p.paint(s, p.ramp.warn) }

// warnBold is [palette.askBold]'s opposite number now that WAITING ON A PERSON
// is amber on home and its places: the hue and the weight together, so the mark
// on a row that has stopped on you leads on a truecolor terminal and on a
// sixteen-colour one alike. The chat's own question block keeps [palette.ask].
func (p palette) warnBold(s string) string { return p.bold(p.warn(s)) }

// money is what a thing cost ([hueMoney]) — a figure in dollars, and nothing
// else. It is deliberately not [palette.add]: that green is the tick on work
// that landed, and a bill is not an achievement.
func (p palette) money(s string) string { return p.paint(s, p.ramp.money) }

// data is the payload rule's ink (payload.go): the datum inside a quiet line —
// a model id, a figure, a key chord — one hue of its own so it reads as a
// KIND and not merely a loudness. See [hueData] for why ink could not do this.
func (p palette) data(s string) string { return p.paint(s, p.ramp.data) }

// violet is the shell operator's tier and nothing else on this surface — see
// [hueViolet] for why it is not the question hue.
func (p palette) violet(s string) string { return p.paint(s, p.ramp.violet) }

// THE IDENTITY RING HAS NO DOOR ANY MORE. `ringPaint` stood here and painted a
// task's marker in a hue hashed off its id; the alphabet it served is gone
// (taskident.go states the change and why), and a painter with no caller is a
// capability that cannot work left standing where somebody would reach for it.
// The HUES survive one rung below because the ramps are complete tables that
// every tier declares and a structural test walks (adaptive_test.go checks the
// light theme carries every field of the dark one through) — data with no door
// is inert; a door with no reason is an invitation.

// underline is the third bare attribute, and it has one job: a PATH inside a
// highlighted command (shellx.go). A path is the one token in a command line
// that names a thing you could go and open, and underline is how every terminal
// on earth has said "this is a location" since before there were hyperlinks.
//
// "When the terminal allows" is the profile question and not the glyph one: a
// terminal told to draw no SGR at all (NO_COLOR) is not underlined either, and
// everything above that rung can do SGR 4 — it is in the original ECMA-48 set.
func (p palette) underline(s string) string {
	if p.profile == tokens.NoColor || s == "" {
		return s
	}
	return "\x1b[4m" + s + "\x1b[24m"
}

// fade paints one line of the streaming thinking window: stop 0 is the oldest
// and faintest, the last stop the newest. See [thoughtFade] for the ramp.
//
// The gradient is a COLOUR question and so it asks the profile and not the
// glyph tier: a truecolor terminal in a C locale is still a truecolor terminal,
// and the ascii flag has exactly one job in this file (box drawing). Where
// there is no hue — the sixteen, and NO_COLOR — the whole window comes back at
// the dim tier, unfaded, which is what it wore before this ramp existed.
func (p palette) fade(s string, stop int) string {
	if !p.fading() {
		return p.dim(s)
	}
	if stop < 0 {
		stop = 0
	}
	if stop >= len(p.ramp.fade) {
		stop = len(p.ramp.fade) - 1
	}
	return p.paint(s, p.ramp.fade[stop])
}

// fading reports whether this terminal has a gradient to spend at all, so a
// caller that draws its OWN ramp degrades exactly where [palette.fade] does
// rather than guessing at the same two conditions a second time.
//
// The gradient is a COLOUR question and so it asks the profile and not the
// glyph tier — a truecolor terminal in a C locale is still a truecolor
// terminal — and the linear tier takes the same answer the sixteen do: a
// gradient is an animation held still, and it says nothing to a reader.
func (p palette) fading() bool {
	switch p.profile {
	case tokens.TrueColor, tokens.ANSI256:
		return !p.linear
	}
	return false
}

// ask is the question hue: the consent block, and nothing else on this surface.
func (p palette) ask(s string) string { return p.paint(s, p.ramp.ask) }

// askBold is what the question's own marker takes — the hue and the weight
// together, so the row a person has to answer leads on a truecolor terminal and
// on a sixteen-colour one alike.
func (p palette) askBold(s string) string { return p.bold(p.ask(s)) }

// cursor paints THE GROUND LADDER's cursor step under one row: the pointer is
// here, or the cursor is.
//
// The text arrives already painted, and that is fine — every foreground
// sequence in this file closes with SGR 39, which resets the ink and leaves the
// background alone. The row is padded to width first, because a highlight that
// stops where the text stops reads as a smudge rather than as a row.
//
// A terminal below ANSI256 gets the row back untouched: see the note at the top
// of this file for why there is no weight-tier fallback here.
//
// THE LINEAR GATE IS THE POINTER'S, NOT THE STEP'S, AND IT IS IN THE WRONG
// PLACE. A pointer's shadow means nothing to somebody who is not looking at the
// screen, so linear mode drops it — but a KEYBOARD cursor on this same step is
// a position in a list, and a position is a fact for every reader. Today every
// caller of this method is a pointer, so the gate sitting here is correct by
// accident; the day the first keyboard cursor arrives on this step the gate
// moves out to the pointer's own call sites, and the two states merge onto one
// ground exactly as the ladder says they should.
func (p palette) cursor(s string, width int) string {
	if p.linear {
		return s
	}
	return p.background(s, width, p.ramp.cursor)
}

// selected paints the ladder's selected step: this is the chosen thing.
//
// It is NOT gated on the linear tier the way the cursor step is, and the reason
// is the note above: what linear mode drops is motion and pointers, not the
// answer to "which row am I on".
func (p palette) selected(s string, width int) string {
	return p.background(s, width, p.ramp.selected)
}

// mark paints the ladder's loudest step under a MARKED SPAN: the rows copy mode
// is holding, and the text a yank would take (copymode.go).
//
// Like the selected step it is ungated: a span a person is building is not a
// pointer's shadow, and a reader who cannot see it still has the status line's
// count. Below ANSI256 the span is unpainted and the count is all there is,
// which is the same trade every other ground on this ladder makes.
func (p palette) mark(s string, width int) string {
	return p.background(s, width, p.ramp.mark)
}

// chip paints an INLINE chip: a background behind exactly the cells the text
// already occupies, with the accent ink on top of it. It is what a recognized
// slash command wears, in the message box and in the sent message alike
// (slashchip.go).
//
// It is [palette.background] with the padding left out, and the missing padding
// is the law rather than an omission: the composer counts the caret's column off
// the draft's own runes, so a chip that added so much as a space would put the
// caret in the wrong column on every row that held one.
//
// The tint is THE GROUND LADDER's selected step ([hueSelected]) and not a
// seventh colour. A background on this surface is not a role — the ladder's
// steps say "the pointer is here", "this is the chosen one" and "you have this
// marked", none of which is a kind of thing — so a run of cells lifted off the
// page reads as "this is not prose" wherever it appears, and a slash command is
// exactly that.
//
// Below the 256 rung there is no background to draw and what is left is the
// accent's own weight: a command reads as bold where it cannot read as a tinted
// run, which is the same trade the person's own words already make (render.go).
func (p palette) chip(s string) string { return p.tint(s, p.accent) }

// tint is [palette.chip] with the ink NAMED rather than assumed: the same lifted
// run of cells, in whichever hue the caller is already saying this thing in.
//
// The one caller that needs it is the composer's room segment (room.go's
// [app.roomLead]), which wears the state hue of the task it names — the hue the
// roster paints the same node's glyph with — because a chip that said "you are
// typing to this" in a seventh colour would be a chip whose colour meant
// nothing. Below the 256 rung the background is dropped and the ink is what is
// left, which is the trade [palette.chip] already makes.
func (p palette) tint(s string, ink func(string) string) string {
	if s == "" {
		return s
	}
	// Width zero, so [palette.background] pads nothing.
	return p.background(ink(s), 0, p.ramp.selected)
}

// holdGround re-lays the step's ground under every cell of s: an inner
// background set becomes the step's ground, an inner background clear becomes
// the step's ground, and a full reset keeps its reset and re-lays the ground
// behind it. Inks and attributes are untouched, so a marked code span keeps its
// colour, its bold and its italic — only the plane under it is made one.
// ground is the step's own background as an SGR parameter list ("48;2;r;g;b"
// or "48;5;n"), without the ESC[…m wrapper.
//
// THE SELECTION WINS OVER THE CHIP, and that is the ruling rather than a side
// effect. The mark step is the loudest rung of the ground ladder; a raised
// plane inside it says "this cell is a different kind of thing" at exactly the
// moment the person is being told "these cells are the ones". Every terminal's
// own selection replaces the background it sweeps over, and this one reads the
// same. Before this walker existed, a background was one code, the text, one
// reset — true only of text carrying no background of its own — so a sweep
// across a row with an inline code span lit the cells before the span and
// nothing after it: the span's own ground took the plane, and its compound
// `49;39` on the way out switched the step's ground off for the rest of the
// row (#294).
//
// Every form the styler can emit is answered, not just the obvious one:
// `48;5;n`, `48;2;r;g;b`, the sixteen-colour `40`–`47` and `100`–`107`, the
// bare `49`, the compound `49;39` that prose actually emits, and `0` — which a
// terminal also spells as no parameters at all. Foreground and underline
// colours (`38…`, `39`, `58…`) and every attribute keep their sub-parameters
// and pass through untouched.
func holdGround(s, ground string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != '\x1b' || i+1 >= len(s) || s[i+1] != '[' {
			out.WriteByte(s[i])
			i++
			continue
		}
		// The sequence's end: an SGR is ESC [ <params> m, and anything else
		// with an escape in it is not this walker's to rewrite.
		j := i + 2
		for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
			j++
		}
		if j >= len(s) || s[j] != 'm' {
			// Not an SGR sequence: pass the escape through verbatim.
			out.WriteString(s[i:min(j+1, len(s))])
			i = min(j+1, len(s))
			continue
		}
		params := s[i+2 : j]
		if params == "" {
			// No parameters is the same as 0: a full reset, kept exactly as
			// it came, with the ground re-laid behind it.
			out.WriteString(s[i : j+1])
			out.WriteString("\x1b[" + ground + "m")
		} else if rebuilt, relaid := regroundParams(params, ground); !relaid {
			out.WriteString("\x1b[" + rebuilt + "m")
		} else {
			out.WriteString("\x1b[" + rebuilt + "m")
			out.WriteString("\x1b[" + ground + "m")
		}
		i = j + 1
	}
	return out.String()
}

// regroundParams rewrites one SGR parameter list so no parameter inside it can
// set or clear a background: a background parameter becomes the step's ground,
// and a full reset (0) is kept and asks for the ground to be re-laid after the
// sequence. Everything else — inks, underline colours, attributes, and the
// sub-parameters of the colour forms — passes through untouched.
func regroundParams(params, ground string) (string, bool) {
	parts := strings.Split(params, ";")
	out := make([]string, 0, len(parts))
	relaid := false
	for k := 0; k < len(parts); k++ {
		switch p := parts[k]; {
		case p == "38" || p == "58":
			// A foreground or underline colour keeps its sub-parameters.
			out = append(out, p)
			if n := colourSubs(parts, k+1); n > 0 {
				out = append(out, parts[k+1:k+1+n]...)
				k += n
			}
		case p == "48":
			// A background colour of any form becomes the step's ground.
			if n := colourSubs(parts, k+1); n > 0 {
				k += n
			}
			out = append(out, ground)
		case p == "49" || isBg16(p):
			// A background clear, and the sixteen-colour backgrounds plain
			// and bright, become the step's ground.
			out = append(out, ground)
		case p == "0":
			// A full reset keeps its reset and asks for the ground behind it.
			out = append(out, p)
			relaid = true
		default:
			out = append(out, p)
		}
	}
	return strings.Join(out, ";"), relaid
}

// isBg16 is the sixteen-colour background parameters, plain (40–47) and
// bright (100–107).
func isBg16(p string) bool {
	if len(p) == 2 && p[0] == '4' && p[1] >= '0' && p[1] <= '7' {
		return true
	}
	return len(p) == 3 && p[:2] == "10" && p[2] >= '0' && p[2] <= '7'
}

// colourSubs is how many parameters follow a 38/48/58 at parts[i] as that
// colour's own: two for the 256-colour form (5;n), four for the truecolour one
// (2;r;g;b), none for anything else — a malformed colour is left as it stands.
func colourSubs(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	switch parts[i] {
	case "5":
		if i+1 < len(parts) {
			return 2
		}
		return 1
	case "2":
		if i+3 < len(parts) {
			return 4
		}
		return len(parts) - i
	default:
		return 0
	}
}

// background is the one place this file draws a background: the row padded to
// the full width, wrapped in the colour, closed with SGR 49. A terminal below
// ANSI256 gets the row back untouched — there is no weight that means "this
// row", and the callers each carry a text-side marker anyway (the lead glyph,
// the bold label).
//
// THE GROUND IS HELD OVER EVERY CELL IT COVERS (holdGround): the wrapped text
// may carry a background of its own — an inline code span on the raised plane,
// a chip — and one code, text, one reset is true only of text that does not.
// An inner set would take the plane off the step, and the compound `49;39`
// prose closes its spans with would switch it off for the rest of the row
// (#294).
func (p palette) background(s string, width int, h hue) string {
	if s == "" {
		return s
	}
	if pad := width - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	switch p.profile {
	case tokens.TrueColor:
		ground := "48;2;" + itoa(int(h.r)) + ";" + itoa(int(h.g)) + ";" + itoa(int(h.b))
		return "\x1b[" + ground + "m" + holdGround(s, ground) + "\x1b[49m"
	case tokens.ANSI256:
		ground := "48;5;" + itoa(int(h.idx))
		return "\x1b[" + ground + "m" + holdGround(s, ground) + "\x1b[49m"
	default:
		return s
	}
}

// bold is the one attribute this file draws without a hue behind it: weight is
// what carries the user/assistant distinction on a monochrome terminal.
func (p palette) bold(s string) string {
	if p.profile == tokens.NoColor || s == "" {
		return s
	}
	return "\x1b[1m" + s + "\x1b[22m"
}

// italic is the second attribute, and it has exactly one job: the model's own
// reasoning (thinking.go), which is text that has to read as a tier below the
// answer even where the dim hue lands close to it. A terminal that ignores SGR 3
// loses nothing — the block is dim and behind its own marker either way.
func (p palette) italic(s string) string {
	if p.profile == tokens.NoColor || s == "" {
		return s
	}
	return "\x1b[3m" + s + "\x1b[23m"
}

// rail is the marker that opens a tool line: the elbow for the last call of a
// cluster, the tee for every call above it, and one ASCII arrow for a terminal
// that cannot draw either. All three are four cells wide, so a cluster's names
// start in one column whatever the terminal can say.
func (p palette) rail(last bool) string {
	if p.ascii {
		return railASCII
	}
	if last {
		return railLast
	}
	return railMid
}

// railWidth is what any of those three measure, which is the point of them all
// being four cells: a tool row's arithmetic starts from a constant instead of
// measuring one on every frame, and it was measuring one per row per frame.
// TestRailFormsAreOneWidth holds the three to it.
const railWidth = 4

// railCont is the stem an expanded call's detail rows hang from.
func (p palette) railCont() string {
	if p.ascii {
		return railContASCII
	}
	return railCont
}

// product is what this surface calls itself, everywhere it speaks: the pulse
// line, the welcome box's wordmark, the first-run wordmark, /help, the OAuth
// consent line and the desktop notification's title. It is written down ONCE
// because a product name spelled out at four call sites is a product name that
// gets renamed at three of them — which is exactly what had happened: this
// constant said `openaf` while pulse.go held a second spelling of the same fact
// (`pulseName = "aforge"`), so a fresh install met a wordmark naming one
// product and, three rows under it, prose naming another. There is one name and
// it is the one a person types.
const product = "aforge"

// The glyph vocabulary of this surface.
//
// There is NO success glyph, deliberately and permanently (D11): a quiet line
// is a success, and a column of ✓ is a column that has to be read to learn
// nothing. Only failure speaks.
const (
	glyphYou      = "› "
	glyphTool     = "↳ " // the fold line's marker, and only the fold line's
	glyphBad      = "✗"
	glyphMore     = "…"
	railMid       = "├─▶ "
	railLast      = "╰─▶ "
	railCont      = "│ "
	railASCII     = "+-> "
	railContASCII = "| "
	// glyphThought opens the reasoning block (thinking.go). It is the spinner's
	// own alphabet at rest — the full braille cell — because a thought is the
	// same machine the spinner is drawing, stopped.
	glyphThought = "⠿"
	// glyphIdle marks a call that was still running when its turn ended. A
	// frozen spinner would claim the call is alive; a dot claims nothing.
	glyphIdle = "·"
	// glyphQueued marks a call the model has asked for and nothing has started:
	// an EMPTY circle, dim, deliberately not a spinner. A spinner is a claim
	// that something is turning, and the whole point of this state is that
	// nothing is.
	glyphQueued = "◌"
	// glyphAsk marks the call a person is being asked about. It is the only
	// glyph on this surface that takes the question hue.
	glyphAsk = "?"
	// glyphAdd and glyphDel spell the diffstat. The minus is U+2212, which is
	// the width of the plus; ASCII '-' is not, and a stat is a pair of numbers
	// read side by side. The diff BODY keeps ASCII +/- — a diff is a diff, and
	// its first column is quoted, copied and pasted.
	glyphAdd = "+"
	glyphDel = "−"
)

// ── THE LINEAR TIER (Options.Linear) ────────────────────────────────────────
//
// Linear mode is the SCREEN-READER tier, and it is one question: what does this
// surface look like to somebody who is not looking at it? Three answers, and
// all three are subtractions:
//
//	no animation   a spinner read aloud is a word repeated forever
//	no hover       a pointer's shadow is nothing to a reader
//	no glyphs      "╰─▶" is announced as three characters nobody named
//
// So every marker that carries meaning by SHAPE gets an ASCII stand-in that
// carries it by NAME, and every marker that carries it by motion stops moving.
// The colours stay: a screen reader ignores SGR, and a person using linear mode
// on a terminal that has hues loses nothing by keeping them.
//
// The stand-ins are the obvious ones. `*` is running because it is what every
// installer that ever printed a progress line used, and `o` is queued because
// it is the empty circle spelled in one byte.
const (
	glyphYouASCII    = "> "
	glyphToolASCII   = "-> "
	glyphBadASCII    = "x"
	glyphIdleASCII   = "."
	glyphQueuedASCII = "o"
	glyphRunASCII    = "*"
)

// youGlyph and toolGlyph are the two markers the transcript opens rows with.
// Everything else on this surface is either inside a tool line (toolview.go
// asks the palette for its own marks) or is a word.
func (p palette) youGlyph() string {
	if p.linear {
		return glyphYouASCII
	}
	return glyphYou
}

func (p palette) toolGlyph() string {
	if p.linear {
		return glyphToolASCII
	}
	return glyphTool
}

// badGlyph is the one glyph a failure is allowed to spend.
func (p palette) badGlyph() string {
	if p.linear {
		return glyphBadASCII
	}
	return glyphBad
}

// spinnerStep is how many frame ticks one braille frame lasts. The frame clock
// runs at [frameInterval] (33ms) because that is the repaint ceiling, but the
// spinner turns on the house grid — 4 × 33ms ≈ tokens.MotionInterval — because
// below about 100ms a braille cycle stops reading as rotation and starts
// reading as shimmer.
const spinnerStep = 4

// pulseStep is the same idea for the quiet ellipsis: nine ticks ≈ 300ms per
// step, a breath rather than a spin.
const pulseStep = 9

// ellipsisFrames is the sign of life while a turn is silent. No spinner here —
// a session that is thinking is not a progress bar; the spinners belong to the
// tool lines, which are the things actually running.
var ellipsisFrames = [3]string{"·", "··", "···"}

func itoa(n int) string { return strconv.Itoa(n) }

// glyphHarness marks a sub-harness — a saved SHAPE of work rather than a piece
// of it (harnesspanel.go). It is the roster's own identity diamond deliberately:
// what a harness and a task node have in common is that both are things this
// session is carrying, and the difference between them is said by the word
// beside the mark rather than by a second alphabet of symbols nobody was taught.
const (
	glyphHarness      = "◆"
	glyphHarnessASCII = "#"
)

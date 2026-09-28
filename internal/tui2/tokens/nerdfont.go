package tokens

// The Nerd Font tier's data (12.7 B.1), as one table.
//
// PROVENANCE. Every codepoint below was verified against the glyphnames.json
// published by ryanoasis/nerd-fonts at **v3.2.1** (2024-04-12), a trimmed
// extract of which is vendored at testdata/nerdfont_glyphnames.json and walked
// by glyphset_test.go. The NAME is the contract and the codepoint is a binding:
// if a future release moves one, the test fails and the table is corrected
// rather than the surface quietly drawing the wrong shape. Every name in B.1
// verified at its documented address, so none of the named alternates
// (nf-fa-cube U+F1B2 for the model mark, nf-fa-sign_in U+F090 for the steer
// prompt, nf-oct-git_branch U+F418 for the branch) was needed.
//
// THREE SLOTS WERE REPICKED after B.1 shipped, by the glyph audit, and each
// carries its argument at its own binding rather than here: NeedsHuman
// (nf-fa-question_circle -> nf-fa-question_circle_o), Folder (nf-fa-folder ->
// nf-fa-folder_o) and Cut (nf-fa-scissors -> no icon at all). The first two are
// one rule applied twice — an icon inherits the INK WEIGHT of the plain glyph
// it stands in for, not only its meaning, its tint and its cell — and the third
// is the geometry rule catching a slot B.1 filed on the wrong side of it.
//
// MEASUREMENT. Every glyph on both sides measures one cell under both shipping
// rulers (ansi.StringWidth, grapheme; ansi.StringWidthWc, wcwidth), and every
// NF codepoint is BMP private use and therefore East_Asian_Width=Ambiguous —
// asserted positively by the gates, because a PUA glyph that reported otherwise
// would mean this table had drifted.
//
// The codepoints are written as \u escapes rather than as the characters
// themselves for the plainest reason there is: a private-use character is
// invisible in an unpatched editor, a terminal and a diff, and a table nobody
// can read in review is a table that drifts.
//
// TARGET. The tier targets the **Mono** Nerd Font variants, whose icons are
// drawn to one cell by construction. The plain "Nerd Font" and "Nerd Font
// Propo" variants draw many icons at roughly two cells over a one-cell advance:
// the grid still advances one, so layout is safe either way, but legibility is
// not. That is a font choice, recorded here so the symptom is diagnosable, and
// named in the settings row's hint.
// NFFailedCell pins the Font Awesome vocabulary spelling for a failed run share.
const NFFailedCell = "nf-fa-times_circle_o"

var vocabulary = []GlyphBinding{
	// -- state (card line 1, rail card, agent row) ---------------------------
	{
		ID: GQueued, Name: "Queued", Meaning: "queued",
		Plain: GlyphQueued, NerdFont: "\uF10C", NFName: "nf-fa-circle_o",
		ASCII:     "o",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GWorking, Name: "Working", Meaning: "working",
		// A half-filled circle for a half-filled circle: the shape language is
		// the same one, which is what makes the swap invisible as a change of
		// meaning and visible only as a change of typeface.
		Plain: GlyphWorking, NerdFont: "\uF042", NFName: "nf-fa-adjust",
		ASCII:     "*",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GSettled, Name: "Settled", Meaning: "settled",
		Plain: GlyphSettled, NerdFont: "\uF00C", NFName: "nf-fa-check",
		ASCII:     "+",
		UsualTint: Green, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFailed, Name: "Failed", Meaning: "failed",
		Plain: GlyphFailed, NerdFont: "\uF00D", NFName: "nf-fa-times",
		ASCII:     "x",
		UsualTint: Coral, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStopped, Name: "Stopped", Meaning: "stopped by the person",
		// THE ICON IS THE TRANSPORT STOP and the plain side is the filled
		// square that transport bar has always been drawn as, so the two tiers
		// are the same shape at two weights — which is the whole test of a
		// binding. nf-fa-stop sits one address along from nf-fa-pause, which
		// this table already ships and which the gate has already verified, so
		// the pair a person reads as "held" and "ended" comes from one family.
		Plain: GlyphStopped, NerdFont: "\uF04D", NFName: "nf-fa-stop",
		ASCII:     "/",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GPaused, Name: "Paused", Meaning: "paused",
		// ASCII plain side: "=" is a character a user types, so this slot is
		// adopted explicitly and never rewritten out from under a line.
		Plain: GlyphPaused, NerdFont: "\uF04C", NFName: "nf-fa-pause",
		ASCII:     "=",
		UsualTint: TextTertiary, NFAmbiguous: true,
	},

	// -- attention -----------------------------------------------------------
	{
		ID: GNeedsHuman, Name: "NeedsHuman", Meaning: "waiting on a human (always amber)",
		// ASCII plain side, and the sharpest case for the carve-out: 5.20 rule
		// 3 makes "?" a thing a user types.
		//
		// REPICK against 12.7 B.1, which named nf-fa-question_circle U+F059: that
		// is a SOLID disc with the mark knocked out of it, and it is the loudest
		// shape in the whole table. The register 12 states is calm and geometric,
		// and it refuses a filled icon wherever an outline one exists. One does:
		// nf-fa-question_circle_o is the same glyph drawn as a ring, it is the
		// same FA4.7 era as nf-fa-microchip which this table already ships, and
		// it puts the attention mark in the same outline-circle family as
		// nf-fa-circle_o, which is the shape sitting beside it on a rail card.
		// The plain side is a bare "?", a stroke and not a blob, so the ring is
		// also the side that inherits its ink weight.
		Plain: GlyphNeedsHuman, NerdFont: "\uF29C", NFName: "nf-fa-question_circle_o",
		ASCII:     "?",
		UsualTint: Amber, NFAmbiguous: true,
	},
	{
		ID: GWaitsOn, Name: "WaitsOn", Meaning: "waiting on a sibling (waits-on edge)",
		Plain: GlyphWaitsOn, NerdFont: "\uF024", NFName: "nf-fa-flag",
		ASCII:     "!",
		UsualTint: Amber, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GWithdrawn, Name: "Withdrawn", Meaning: "the question no longer needs answering",
		// nf-fa-ban is the same shape one weight heavier — a ring with a stroke
		// through it — so the two tiers are the geometric and the pictographic
		// spelling of one mark, which is the whole test of a binding. It sits in
		// the same FA4.7 outline family as nf-fa-question_circle_o above it,
		// which is the mark it retires on the row it lands on.
		//
		// The tint is TERTIARY and never Amber. Amber is a person being waited
		// on, and this mark's entire meaning is that nobody is being waited on
		// any more; a withdrawn line drawn in the attention hue would be the
		// screen asking for a decision it has just said it does not need.
		Plain: GlyphWithdrawn, NerdFont: "\uF05E", NFName: "nf-fa-ban",
		ASCII:     "-",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GAssumed, Name: "Assumed", Meaning: "taken for granted and gone on with; strike it to change it",
		// nf-fa-lightbulb_o is the ASKER'S OWN IDEA standing in for a fact
		// nobody supplied, which is exactly what an assumption is — and the
		// FA4.7 outline family is the one this table already ships beside
		// nf-fa-question_circle_o and nf-fa-circle_o. The tier has no icon for
		// `≈` itself: nf-md-approximately_equal lives above the BMP, which
		// TestNerdFontIsBMPPrivateUse refuses, so the two sides are the
		// mathematical and the pictographic spelling of one meaning rather than
		// one shape at two weights.
		//
		// The tint is TERTIARY and never Amber, for [GWithdrawn]'s reason said
		// about the other end of the same card: amber is a person being waited
		// on, and an assumption is the asker NOT waiting.
		Plain: GlyphAssumed, NerdFont: "\uF0EB", NFName: "nf-fa-lightbulb_o",
		ASCII:          "~",
		UsualTint:      TextTertiary,
		PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- disclosure and navigation -------------------------------------------
	//
	// NOT UPGRADED, any of them, and this is a REPICK against the tier's first
	// draft (user-reported, 2026-08-11: "these big >"). The rule, which GCut's
	// register note already implied: NF may upgrade PICTOGRAPHIC slots - tool
	// marks, state icons - never punctuation-shaped navigation marks. The
	// plain chevron and triangle marks are typographic characters the font renders at
	// text size and weight; the FA chevrons are private-use ICONS that most
	// patched fonts draw a size up and a weight heavier, so every prompt,
	// fold mark and scope header shouted. Both tiers now share the byte.
	{
		ID: GCollapsed, Name: "Collapsed", Meaning: "collapsed",
		Plain: GlyphCollapsed, UsualTint: TextTertiary, Geometry: true,
	},
	{
		ID: GExpanded, Name: "Expanded", Meaning: "expanded",
		Plain: GlyphExpanded, UsualTint: TextTertiary, Geometry: true,
	},
	{
		ID: GScopeUp, Name: "ScopeUp", Meaning: "scope header / go up",
		// Tinted with the scope's identity where there is one, tertiary where
		// there is not.
		Plain: GlyphScopeUp, UsualTint: TextTertiary, Geometry: true,
	},
	{
		// THE POINTER IS A NAVIGATION MARK AND SHARES ITS BYTE IN BOTH TIERS, for
		// the fold marks' reason above: an FA caret is an icon a size up and a
		// weight heavier, and a pointer that shouted would be the loudest cell on
		// a question whose words are meant to be read.
		ID: GPointer, Name: "Pointer", Meaning: "the answer your enter takes (always amber)",
		Plain: GlyphPointer, UsualTint: Amber, Geometry: true,
	},
	{
		// THE RECOMMENDED MARK IS A PICTOGRAPH, so the tier may draw it: an
		// outline star is the one icon every reader already takes for "this is
		// the suggested one", and the outline and not the filled star is this
		// table's register (the attention mark's own repick says why). Its word
		// `recommended` stands beside it wherever there is room, so the ASCII
		// spelling can be the character a reader names as a star.
		ID: GRecommended, Name: "Recommended", Meaning: "the asker's pick (always amber)",
		Plain: GlyphRecommended, NerdFont: "\uF006", NFName: "nf-fa-star_o",
		ASCII:     "*",
		UsualTint: Amber, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- the one frame (geometry: box drawing IS the right character) --------
	//
	// The frame's ASCII run is the frame's own (internal/tui3/frame.go), as
	// every grid's is: two plain rules and no sides.
	{
		ID: GFrameTopLeft, Name: "FrameTopLeft", Meaning: "the frame: its top-left corner",
		Plain: GlyphFrameTopLeft, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameTopRight, Name: "FrameTopRight", Meaning: "the frame: its top-right corner",
		Plain: GlyphFrameTopRight, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameBottomLeft, Name: "FrameBottomLeft", Meaning: "the frame: its bottom-left corner",
		Plain: GlyphFrameBottomLeft, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameBottomRight, Name: "FrameBottomRight", Meaning: "the frame: its bottom-right corner",
		Plain: GlyphFrameBottomRight, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameEdge, Name: "FrameEdge", Meaning: "the frame: its top and bottom edge",
		Plain: GlyphFrameEdge, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameSide, Name: "FrameSide", Meaning: "the frame: its sides",
		Plain: GlyphFrameSide, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameTeeDown, Name: "FrameTeeDown", Meaning: "the frame: where the rule above two panes meets the seam between them",
		Plain: GlyphFrameTeeDown, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GFrameTeeUp, Name: "FrameTeeUp", Meaning: "the frame: where the rule below two panes closes the seam between them",
		Plain: GlyphFrameTeeUp, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GTarget, Name: "Target", Meaning: "where the next thing goes — the destination of a draft",
		// Tinted with the identity of the room the draft will land in, exactly as
		// the steer prompt is: both of them are about somewhere that does not
		// exist yet.
		Plain: GlyphTarget, UsualTint: Identity0, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GTruncated, Name: "Truncated", Meaning: "clickable overflow — there is more, ask for it",
		Plain: GlyphTruncated, NerdFont: "\uF141", NFName: "nf-fa-ellipsis_h",
		ASCII:     ">",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GEllipsis, Name: "Ellipsis", Meaning: "static overflow — the rest is off the edge (16.2)",
		// Deliberately not upgraded, for two reasons that each suffice on their
		// own. First, the tier's ellipsis icon is already spent on GTruncated,
		// and two slots upgrading to one icon would erase the distinction the
		// plain tier draws. Second, this mark lands INSIDE prose — it is what a
		// width-bounded renderer leaves at the end of a sentence somebody wrote — so a
		// repertoire swap here would read as an edit, which is the same finding
		// that dropped 12.7 D.2's rule (b).
		Plain: GlyphEllipsis, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GCut, Name: "Cut", Meaning: "the truncation law's mark — this stopped and should not have (12.5.2)",
		// NOT UPGRADED, and this is a REPICK against 12.7 B.1, which named
		// nf-fa-scissors U+F0C4 for this slot. Three reasons, in the order the
		// glyph audit found them:
		//
		//  1. The cut is drawn as the tier-blind [GlyphCut] geometry slot. The
		//     parity golden records the consequence in plain sight: the cut row
		//     is byte-identical in both tiers. An icon nothing draws would be a
		//     lie in the table and a row in the width gate that gates nothing.
		//  2. The mark is line geometry, which is B.2's own rule. `╌` is a
		//     dashed rule FRAGMENT that tiles with the `─` run it leads
		//     ("╌ cut off — output cap ──────"); a pictograph in that cell
		//     breaks the rule the row is built out of.
		//  3. A pair of scissors is the one emoji-adjacent shape in the set — ✂
		//     by another name — in a register that is otherwise geometric shapes
		//     for state, tool marks for kind and chevrons for navigation.
		//
		// What 12.5.2 asks for survives in full: a cut is `╌` and an overflow is
		// `⋯`, in BOTH tiers, and glyph_test.go still pins them apart. If a
		// caller ever wants a tiered cut mark the decision reopens — with a
		// shape that is a rule, not a tool.
		Plain:     GlyphCut,
		UsualTint: Coral, Geometry: true,
	},

	// -- composer ------------------------------------------------------------
	//
	// Prompt marks are punctuation, not pictographs \u2014 the navigation rule
	// above applies, so neither tier swaps the byte.
	{
		ID: GPromptChat, Name: "PromptChat", Meaning: "composer prompt (chat)",
		Plain: GlyphPromptChat, UsualTint: TextSecondary, Geometry: true,
	},
	{
		ID: GPromptSteer, Name: "PromptSteer", Meaning: "composer prompt (steer line): words that became work",
		// "Maps into", which is what the steer line does. Tinted with the
		// identity of the room the draft lands in, cyan at the commissioning
		// moment itself.
		Plain: GlyphPromptSteer, UsualTint: Identity0, Geometry: true,
	},
	{
		ID: GReplyIn, Name: "ReplyIn", Meaning: "the answer to a question put back to the asker, drawn under it",
		Plain: GlyphReplyIn, UsualTint: TextTertiary, Geometry: true,
	},
	// A composer's own mark that is NOT geometry: the unsent draft (a message
	// cleared before sending). Both sides are "being written", the outline
	// pencil on the floor and the compose icon where a patched font supplies
	// one — the same shape at two weights, which is the whole test of a binding.
	{
		ID: GDraftUnsent, Name: "DraftUnsent", Meaning: "a message typed and cleared before it was sent",
		Plain: GlyphDraftUnsent, NerdFont: "\uF044", NFName: "nf-fa-pencil_square_o",
		ASCII:     "w",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	// A crew seat a person pinned: the fixed point on the floor, the thumb tack
	// where a patched font supplies one — a pin at two weights.
	{
		ID: GPinned, Name: "Pinned", Meaning: "a crew seat a person pinned; the router leaves it where it is",
		Plain: GlyphPinned, NerdFont: "\uF08D", NFName: "nf-fa-thumb_tack",
		ASCII:     "p",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- the execution voices (5.5) ------------------------------------------
	{
		ID: GThought, Name: "Thought", Meaning: "the model's own words between calls",
		Plain: GlyphThought, NerdFont: "\uF069", NFName: "nf-fa-asterisk",
		ASCII:     "\"",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GShell, Name: "Shell", Meaning: "a shell call",
		// ASCII plain side, and the carve-out earns its keep here: a painted
		// cell that is exactly "$" is money on every other row of the surface
		// (GSpend), so this slot is adopted explicitly by the one renderer
		// that owns it and is never rewritten out from under a line.
		Plain: GlyphShell, NerdFont: "\uF120", NFName: "nf-fa-terminal",
		ASCII:     "$",
		UsualTint: Cyan, NFAmbiguous: true,
	},
	{
		ID: GSearch, Name: "Search", Meaning: "a call that went out to the world",
		Plain: GlyphSearch, NerdFont: "\uF002", NFName: "nf-fa-search",
		ASCII:     "?",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFilter, Name: "Filter", Meaning: "narrowing what is already on the page",
		Plain: GlyphFilter, NerdFont: "\uF002", NFName: "nf-fa-search",
		// THE ASCII IS `/` AND NOT `?`. A screen reader on this page hears `?`
		// for every row of work waiting on a person, and the filter box very
		// often sits directly above one.
		ASCII:     "/",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GWrite, Name: "Write", Meaning: "a call that wrote something down",
		Plain: GlyphWrite, NerdFont: "\uF040", NFName: "nf-fa-pencil",
		ASCII:     "*",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- the action families (internal/tui3's step gutter) -------------------
	//
	// One mark per FAMILY of work, and every one of them a REFUSAL to say how
	// it went: the gutter is a label, `test` draws a flask and never a
	// checkmark, and no slot here carries a verdict. Four families are not in
	// this block because the table already owns their slot — search is
	// [GSearch], editing is [GWrite], a command is [GShell], and a thing that
	// was not there reuses the plus.
	//
	// The addresses are Font Awesome 4.7, which is B.2's rule: those codepoints
	// have sat still since Nerd Fonts v1 and are in every patched font,
	// including a Powerline-only patch.
	{
		ID: GActionRead, Name: "ActionRead", Meaning: "a call that read something",
		Plain: GlyphActionRead, NerdFont: "\uF15C", NFName: "nf-fa-file_text",
		ASCII:     "<",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionCreate, Name: "ActionCreate", Meaning: "a call that made something that was not there",
		// ASCII plain side, and it is the diff-add byte under another slot's
		// name: a painted cell that is exactly "+" is a diffstat everywhere
		// else on the surface, so this one is adopted explicitly and never
		// rewritten out from under a line.
		Plain: GlyphActionCreate, NerdFont: "\uF067", NFName: "nf-fa-plus",
		ASCII:     "+",
		UsualTint: Cyan, NFAmbiguous: true,
	},
	{
		ID: GActionTest, Name: "ActionTest", Meaning: "a call that checked something",
		Plain: GlyphActionTest, NerdFont: "\uF0C3", NFName: "nf-fa-flask",
		ASCII:     "!",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionBrowse, Name: "ActionBrowse", Meaning: "a call that went out to a page somewhere else",
		Plain: GlyphActionBrowse, NerdFont: "\uF0AC", NFName: "nf-fa-globe",
		ASCII:     "^",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionTransfer, Name: "ActionTransfer", Meaning: "a call that moved bytes both ways",
		Plain: GlyphActionTransfer, NerdFont: "\uF0EC", NFName: "nf-fa-exchange",
		ASCII:     "&",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionCommunicate, Name: "ActionCommunicate", Meaning: "a call that said something to a person",
		// NOT AUTO-UPGRADED, and the carve-out is 12.7 D.3's rule read for what
		// it means rather than for the byte it names: the guillemet is
		// PUNCTUATION half of Europe quotes with, so a painted cell that is
		// exactly "»" is plausible content in the same way a bare "?" is. The
		// one surface that draws this family asks for the slot by name.
		Plain: GlyphActionCommunicate, NerdFont: "\uF075", NFName: "nf-fa-comment",
		ASCII:     "@",
		UsualTint: Cyan, NFAmbiguous: true,
	},
	{
		ID: GActionCoordinate, Name: "ActionCoordinate", Meaning: "a call that handed work out",
		Plain: GlyphActionCoordinate, NerdFont: "\uF126", NFName: "nf-fa-code_fork",
		ASCII:     "|",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionPlan, Name: "ActionPlan", Meaning: "a call that wrote a plan down",
		Plain: GlyphActionPlan, NerdFont: "\uF0AE", NFName: "nf-fa-tasks",
		ASCII:     "#",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionWait, Name: "ActionWait", Meaning: "a call that waited",
		// A quarter of a clock face, STILL. The hourglasses are banned — two
		// cells, and they lie about liveness on a row that is not moving.
		Plain: GlyphActionWait, NerdFont: "\uF017", NFName: "nf-fa-clock_o",
		ASCII:     ",",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GActionWork, Name: "ActionWork", Meaning: "a step, which is all this one knows",
		Plain: GlyphActionWork, NerdFont: "\uF013", NFName: "nf-fa-cog",
		ASCII:     ".",
		UsualTint: Cyan, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- meta ----------------------------------------------------------------
	{
		ID: GBoosted, Name: "Boosted", Meaning: "boosted: a transient escalation of the work-role binding",
		// The tier recovers 5.17's ORIGINAL intent. 5.17 asked for ⚡; glyph.go
		// had to refuse it because U+26A1 measures two cells and 5.17's own
		// width law forbids it. nf-fa-bolt is the bolt at one cell.
		Plain: GlyphBoosted, NerdFont: "\uF0E7", NFName: "nf-fa-bolt",
		ASCII:     "^",
		UsualTint: Amber, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GSeparator, Name: "Separator", Meaning: "telemetry separator",
		// Geometry, and the one the refusal is loudest about: the powerline
		// triangle separators stay banned in both tiers (12.7 G), · remains the
		// separator, and BannedGlyphs enforces it at build time.
		Plain: GlyphSeparator, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GMissing, Name: "Missing", Meaning: "missing data — never an estimate (10.2.8)",
		Plain: GlyphMissing, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GEstimate, Name: "Estimate", Meaning: "estimated number (10.2.8)",
		Plain: GlyphEstimate, UsualTint: TextTertiary, Geometry: true,
	},

	// -- structure -----------------------------------------------------------
	{
		ID: GAccentRail, Name: "AccentRail", Meaning: "the accent rail grouping a card's lines in its identity hue",
		Plain: GlyphAccentRail, UsualTint: Identity0, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GDragHandle, Name: "DragHandle", Meaning: "a reorderable pending row (5.22)",
		Plain: GlyphDragHandle, NerdFont: "\uF142", NFName: "nf-fa-ellipsis_v",
		ASCII:     ":",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- plan progress (5.21 step dots) --------------------------------------
	{
		ID: GStepDone, Name: "StepDone", Meaning: "plan step done",
		Plain: GlyphStepDone, NerdFont: "\uF111", NFName: "nf-fa-circle",
		ASCII:     "#",
		UsualTint: Green, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStepRunning, Name: "StepRunning", Meaning: "plan step running",
		// The same rune as Working, by design: one shape for one state.
		Plain: GlyphStepRunning, NerdFont: "\uF042", NFName: "nf-fa-adjust",
		ASCII:     "*",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStepPending, Name: "StepPending", Meaning: "plan step pending",
		Plain: GlyphStepPending, NerdFont: "\uF10C", NFName: "nf-fa-circle_o",
		ASCII:     "o",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStepBlocked, Name: "StepBlocked", Meaning: "plan step blocked",
		Plain: GlyphStepBlocked, NerdFont: "\uF024", NFName: "nf-fa-flag",
		ASCII:     "!",
		UsualTint: Amber, NFAmbiguous: true, AutoUpgrade: true,
	},

	{ID: GDoneCell, Name: "DoneCell", Meaning: "a completed share of a run", Plain: GlyphDoneCell, NerdFont: "\uF111", NFName: "nf-fa-circle", ASCII: "#", UsualTint: Green, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true},
	{ID: GRunningCell, Name: "RunningCell", Meaning: "the partly completed share at a run frontier", Plain: GlyphRunningCell, NerdFont: "\uF042", NFName: "nf-fa-adjust", ASCII: ">", UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true},
	{ID: GEmptyCell, Name: "EmptyCell", Meaning: "a not-started share of a run", Plain: GlyphEmptyCell, NerdFont: "\uF10C", NFName: "nf-fa-circle_o", ASCII: ".", UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true},
	{ID: GFailedCell, Name: "FailedCell", Meaning: "a share of a run holding a failure", Plain: GlyphFailedCell, NerdFont: "\uF05C", NFName: NFFailedCell, ASCII: "x", UsualTint: Coral, NFAmbiguous: true, AutoUpgrade: true},

	// -- queue pills (10.3.13) -----------------------------------------------
	{
		ID: GQueuePill, Name: "QueuePill", Meaning: "one queued item, capped",
		Plain: GlyphQueuePill, NerdFont: "\uF0DA", NFName: "nf-fa-caret_right",
		ASCII:     ">",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- diff micro-stats (geometry: a sign is not an icon) ------------------
	{
		ID: GDiffAdd, Name: "DiffAdd", Meaning: "lines added",
		Plain: GlyphDiffAdd, UsualTint: Green, Geometry: true,
	},
	{
		ID: GDiffDel, Name: "DiffDel", Meaning: "lines deleted",
		Plain: GlyphDiffDel, UsualTint: Coral, Geometry: true,
	},

	// -- the inline spawn tree (geometry: box drawing IS the right character) -
	{
		ID: GTreeBranch, Name: "TreeBranch", Meaning: "spawn tree: a branch",
		Plain: GlyphTreeBranch, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GTreeLast, Name: "TreeLast", Meaning: "spawn tree: the last child",
		Plain: GlyphTreeLast, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GTreeVert, Name: "TreeVert", Meaning: "spawn tree: a continuing trunk",
		Plain: GlyphTreeVert, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GTreeDash, Name: "TreeDash", Meaning: "spawn tree: a horizontal run",
		Plain: GlyphTreeDash, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},

	// -- the place line (5.19) -----------------------------------------------
	{
		ID: GHome, Name: "Home", Meaning: "home / the task workspace",
		Plain: GlyphHome, NerdFont: "\uF015", NFName: "nf-fa-home",
		ASCII:     "~",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFolder, Name: "Folder", Meaning: "a folder or region of the tree",
		// ASCII plain side: the slash already means "directory" everywhere, and
		// a painted cell that is exactly "/" is plainly content.
		//
		// REPICK against 12.7 B.1, which named nf-fa-folder U+F07B. A solid
		// folder is a filled trapezoid — the single heaviest blot of ink in the
		// table — and it lands on the place line, which is TextTertiary chrome
		// under 5.19 and is meant to sit under the eye rather than in it. The
		// plain side it inherits from is a bare "/". nf-fa-folder_o is the same
		// shape as an outline, is FA4.0-era and therefore exactly as well
		// covered as the solid one (no coverage is traded for the weight), and
		// it matches nf-fa-home's line weight two segments to its left.
		Plain: GlyphFolder, NerdFont: "\uF114", NFName: "nf-fa-folder_o",
		ASCII:     "/",
		UsualTint: TextTertiary, NFAmbiguous: true,
	},
	{
		ID: GGitBranch, Name: "GitBranch", Meaning: "the git branch the work sits on",
		// U+E0A0 is the single highest-coverage codepoint in the whole Nerd
		// Font repertoire — present even in Powerline-only patches. Note the
		// distinction the refusal draws: the powerline BRANCH SYMBOL is adopted
		// because it is an icon; the powerline SEPARATORS stay banned because
		// they are joining chrome that must tile pixel-exactly to look like
		// anything (12.7 G).
		Plain: GlyphGitBranch, NerdFont: "\uE0A0", NFName: "nf-pl-branch",
		ASCII:     ":",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- the status line (5.17's "K3 ▄ $8.65") -------------------------------
	{
		ID: GModel, Name: "Model", Meaning: "the model the answer came from",
		Plain: GlyphModel, NerdFont: "\uF2DB", NFName: "nf-fa-microchip",
		ASCII:     "o",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GSpend, Name: "Spend", Meaning: "money spent",
		// ASCII plain side, and the cleanest parity case in the set: one cell
		// swaps for one cell inside a run that already reads "$8.65".
		Plain: GlyphSpend, NerdFont: "\uF155", NFName: "nf-fa-dollar",
		ASCII:     "$",
		UsualTint: Green, NFAmbiguous: true,
	},

	// -- the file kinds --------------------------------------------------------
	//
	// Four kinds, and the set is closed because it is the set a person can hand
	// this program and the set this program can hand back: a document, an
	// image, a sound, a moving picture. A chip on a message names one; so does
	// the gutter beside a call that made one or opened one. They are one BLOCK
	// rather than four scattered slots for the reason the block exists at all —
	// a row of chips that upgraded three kinds and left the fourth on the plain
	// floor would draw the exact mixed-repertoire line the tier was built to
	// end.
	//
	// A KIND IS NOT AN ACTION. The step gutter's families say what a call was
	// DOING; these say what the thing on the end of the path IS, which is why
	// `generate_video` and a dragged-in `.mp4` draw the same mark.
	//
	// The addresses are Font Awesome 4's outlined file family, so the four sit
	// at one line weight beside each other.
	{
		ID: GFileDocument, Name: "FileDocument", Meaning: "a document — a page of text",
		// The read action's byte AND its icon. A page of text is a page of
		// text whether a call opened it or a person dragged it in, so the
		// tier collapses nothing the floor draws apart, which is the one
		// condition glyphvocab_test.go puts on a shared icon.
		Plain: GlyphFileDocument, NerdFont: "\uF15C", NFName: "nf-fa-file_text",
		ASCII:     "d",
		UsualTint: TextSecondary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFileImage, Name: "FileImage", Meaning: "an image",
		Plain: GlyphFileImage, NerdFont: "\uF1C5", NFName: "nf-fa-file_image_o",
		ASCII:     "i",
		UsualTint: TextSecondary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFileAudio, Name: "FileAudio", Meaning: "a sound",
		Plain: GlyphFileAudio, NerdFont: "\uF1C7", NFName: "nf-fa-file_audio_o",
		ASCII:     "a",
		UsualTint: TextSecondary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFileVideo, Name: "FileVideo", Meaning: "a moving picture",
		// U+25B7, the OUTLINE triangle, and the choice is forced twice over. One
		// plain byte may upgrade exactly one way, and the filled U+25B6 a chip
		// used to draw is already [GQueuePill]'s — so a whole-cell rewrite could
		// not tell a queue pill from a video chip. The outline is also the right
		// weight beside the three outlined file icons it stands with.
		Plain: GlyphFileVideo, NerdFont: "\uF1C8", NFName: "nf-fa-file_video_o",
		ASCII:     "v",
		UsualTint: TextSecondary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- the prose slots (code.go) -------------------------------------------
	//
	// Named in code.go because a slot is a MEANING and not a byte; bound HERE
	// because the vocabulary is one table or it is not a vocabulary. All three
	// are geometry: a list mark, a quote gutter and a code hairline are line
	// structure, and 5.13 asks structure to be the quietest thing on the row —
	// an icon in any of the three would be the loudest.
	{
		ID: GProseBullet, Name: "ProseBullet", Meaning: "an unordered list item",
		Plain: GlyphProseBullet, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GProseQuote, Name: "ProseQuote", Meaning: "the gutter bar down a blockquote",
		Plain: GlyphProseQuote, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
	{
		ID: GCodeGutter, Name: "CodeGutter", Meaning: "the hairline down a fenced code block",
		Plain: GlyphCodeGutter, UsualTint: TextTertiary, PlainAmbiguous: true, Geometry: true,
	},
}

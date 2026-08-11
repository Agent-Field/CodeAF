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
var vocabulary = []GlyphBinding{
	// -- state (card line 1, rail card, agent row) ---------------------------
	{
		ID: GQueued, Name: "Queued", Meaning: "queued",
		Plain: GlyphQueued, NerdFont: "\uF10C", NFName: "nf-fa-circle_o",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GWorking, Name: "Working", Meaning: "working",
		// A half-filled circle for a half-filled circle: the shape language is
		// the same one, which is what makes the swap invisible as a change of
		// meaning and visible only as a change of typeface.
		Plain: GlyphWorking, NerdFont: "\uF042", NFName: "nf-fa-adjust",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GSettled, Name: "Settled", Meaning: "settled",
		Plain: GlyphSettled, NerdFont: "\uF00C", NFName: "nf-fa-check",
		UsualTint: Green, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFailed, Name: "Failed", Meaning: "failed",
		Plain: GlyphFailed, NerdFont: "\uF00D", NFName: "nf-fa-times",
		UsualTint: Coral, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GPaused, Name: "Paused", Meaning: "paused",
		// ASCII plain side: "=" is a character a user types, so this slot is
		// adopted explicitly and never rewritten out from under a line.
		Plain: GlyphPaused, NerdFont: "\uF04C", NFName: "nf-fa-pause",
		UsualTint: TextTertiary, NFAmbiguous: true,
	},

	// -- attention -----------------------------------------------------------
	{
		ID: GNeedsHuman, Name: "NeedsHuman", Meaning: "waiting on a human (always amber)",
		// ASCII plain side, and the sharpest case for the carve-out: 5.20 rule
		// 3 makes "?" a thing a user types.
		Plain: GlyphNeedsHuman, NerdFont: "\uF059", NFName: "nf-fa-question_circle",
		UsualTint: Amber, NFAmbiguous: true,
	},
	{
		ID: GWaitsOn, Name: "WaitsOn", Meaning: "waiting on a sibling (waits-on edge)",
		Plain: GlyphWaitsOn, NerdFont: "\uF024", NFName: "nf-fa-flag",
		UsualTint: Amber, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- disclosure and navigation -------------------------------------------
	{
		ID: GCollapsed, Name: "Collapsed", Meaning: "collapsed",
		Plain: GlyphCollapsed, NerdFont: "\uF054", NFName: "nf-fa-chevron_right",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GExpanded, Name: "Expanded", Meaning: "expanded",
		Plain: GlyphExpanded, NerdFont: "\uF078", NFName: "nf-fa-chevron_down",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GScopeUp, Name: "ScopeUp", Meaning: "scope header / go up",
		// Tinted with the scope's identity where there is one, tertiary where
		// there is not; a thin chevron matches the guillemet it replaces.
		Plain: GlyphScopeUp, NerdFont: "\uF104", NFName: "nf-fa-angle_left",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GTruncated, Name: "Truncated", Meaning: "clickable overflow — there is more, ask for it",
		Plain: GlyphTruncated, NerdFont: "\uF141", NFName: "nf-fa-ellipsis_h",
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GCut, Name: "Cut", Meaning: "the truncation law's mark — this stopped and should not have (12.5.2)",
		// The cut mark stays DISTINCT from the overflow mark in both tiers:
		// scissors against an ellipsis, exactly as ╌ stands against ⋯.
		Plain: GlyphCut, NerdFont: "\uF0C4", NFName: "nf-fa-scissors",
		UsualTint: Coral, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- composer ------------------------------------------------------------
	{
		ID: GPromptChat, Name: "PromptChat", Meaning: "composer prompt (chat)",
		Plain: GlyphPromptChat, NerdFont: "\uF105", NFName: "nf-fa-angle_right",
		UsualTint: TextSecondary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GPromptSteer, Name: "PromptSteer", Meaning: "composer prompt (steer line): words that became work",
		// "Maps into", which is what the steer line does. Tinted with the
		// identity of the room the draft lands in, cyan at the commissioning
		// moment itself.
		Plain: GlyphPromptSteer, NerdFont: "\uF178", NFName: "nf-fa-long_arrow_right",
		UsualTint: Identity0, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- meta ----------------------------------------------------------------
	{
		ID: GBoosted, Name: "Boosted", Meaning: "boosted: a transient escalation of the work-role binding",
		// The tier recovers 5.17's ORIGINAL intent. 5.17 asked for ⚡; glyph.go
		// had to refuse it because U+26A1 measures two cells and 5.17's own
		// width law forbids it. nf-fa-bolt is the bolt at one cell.
		Plain: GlyphBoosted, NerdFont: "\uF0E7", NFName: "nf-fa-bolt",
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
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- plan progress (5.21 step dots) --------------------------------------
	{
		ID: GStepDone, Name: "StepDone", Meaning: "plan step done",
		Plain: GlyphStepDone, NerdFont: "\uF111", NFName: "nf-fa-circle",
		UsualTint: Green, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStepRunning, Name: "StepRunning", Meaning: "plan step running",
		// The same rune as Working, by design: one shape for one state.
		Plain: GlyphStepRunning, NerdFont: "\uF042", NFName: "nf-fa-adjust",
		UsualTint: Cyan, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStepPending, Name: "StepPending", Meaning: "plan step pending",
		Plain: GlyphStepPending, NerdFont: "\uF10C", NFName: "nf-fa-circle_o",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GStepBlocked, Name: "StepBlocked", Meaning: "plan step blocked",
		Plain: GlyphStepBlocked, NerdFont: "\uF024", NFName: "nf-fa-flag",
		UsualTint: Amber, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- queue pills (10.3.13) -----------------------------------------------
	{
		ID: GQueuePill, Name: "QueuePill", Meaning: "one queued item, capped",
		Plain: GlyphQueuePill, NerdFont: "\uF0DA", NFName: "nf-fa-caret_right",
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
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GFolder, Name: "Folder", Meaning: "a folder or region of the tree",
		// ASCII plain side: the slash already means "directory" everywhere, and
		// a painted cell that is exactly "/" is plainly content.
		Plain: GlyphFolder, NerdFont: "\uF07B", NFName: "nf-fa-folder",
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
		UsualTint: TextTertiary, NFAmbiguous: true, AutoUpgrade: true,
	},

	// -- the status line (5.17's "K3 ▄ $8.65") -------------------------------
	{
		ID: GModel, Name: "Model", Meaning: "the model the answer came from",
		Plain: GlyphModel, NerdFont: "\uF2DB", NFName: "nf-fa-microchip",
		UsualTint: TextTertiary, PlainAmbiguous: true, NFAmbiguous: true, AutoUpgrade: true,
	},
	{
		ID: GSpend, Name: "Spend", Meaning: "money spent",
		// ASCII plain side, and the cleanest parity case in the set: one cell
		// swaps for one cell inside a run that already reads "$8.65".
		Plain: GlyphSpend, NerdFont: "\uF155", NFName: "nf-fa-dollar",
		UsualTint: Green, NFAmbiguous: true,
	},
}

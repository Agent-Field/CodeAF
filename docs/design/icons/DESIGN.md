# Icons — one vocabulary, three tiers, one door

Owner ruling 2026-09-09. Every mark a person sees in the chat comes from ONE
table, in whichever of three spellings their terminal can draw. This page is the
copy of record: the law, the table as it landed, and how to add a mark.

## The law

1. **One vocabulary.** `internal/tui2/tokens` holds every mark: the task states,
   the action families in the step gutter, the chrome, the prose slots. A
   surface holds the map from ITS meanings to slots and nothing else.
2. **Three tiers, one door.** `tokens.GlyphSet.Glyph(id)` resolves a slot:
   `NerdFont` (a Font Awesome 4 icon), `Plain` (the geometric floor every
   terminal draws), `ASCII` (one character a screen reader can name). Flipping
   the tier moves no column — every spelling on every side measures one cell
   under both shipping rulers, and `glyph_test.go` proves it rather than
   assuming it.
3. **Never a literal in a surface.** A mark spelled as a character draws the
   plain floor forever, because a literal cannot know which repertoire the
   terminal is on. That is not a style point: it is how a person with a patched
   font came to see proper icons beside their tool calls and bare geometric
   shapes beside their tasks. `internal/tui3/iconvocab_test.go` fails the build
   on one, and it runs on every pull request through `make test-laws`.
4. **The shape says the state, with no colour.** Two readings may not share a
   cell in any tier. The roster is read by people who have turned colour off and
   by people who cannot see it; a vocabulary that needed its hues would have
   nothing to say to either. Hue is a second, independent axis (`tierInk`).
5. **Font Awesome 4 addresses only.** Those codepoints have sat still since Nerd
   Fonts v1 and are in every patched font, including a Powerline-only patch.
   `nf-md-*` (Material) is refused outright: Nerd Fonts v3 moved the whole set
   into plane 15, where a v2-era patched font has nothing. The NAME is the
   contract and the codepoint is a binding, verified against the pinned
   `testdata/nerdfont_glyphnames.json`.
6. **The ban list is measured, not argued.** `tokens.BannedGlyphs` refuses
   anything wider than one cell, anything in the emoji planes, anything carrying
   a variation selector, the media-control pictographs (`⏸ ⏵ ⏹`), the
   hourglasses, and the powerline separators. A rune not on the list still has
   to pass the ruler.
7. **The tier is chosen once.** `tokens.DetectGlyphSet` vetoes the terminals and
   locales that cannot be trusted with private use — no `TERM`, `TERM=linux`,
   Apple Terminal, a CJK locale, legacy conhost — and the Display row (`step
   icons`: `auto` · `rich` · `plain`) is the person's own say. `app.iconSet`
   folds the two together; `palette.glyph` and `app.icon` are the surface's only
   doors, and the ASCII tier is `palette.ascii`'s answer (the linear,
   screen-reader option sets it).

## The table as it landed

### Task states (internal/tui3/tasktier.go's `tierSlot`)

| State | Slot | Plain | Nerd font | ASCII |
| --- | --- | --- | --- | --- |
| queued, auto-starts in Ns | `GQueued` | `○` | nf-fa-circle_o | `o` |
| waiting on task N / on the machine | `GWaitsOn` | `⚑` | nf-fa-flag | `!` |
| working, finishing | `GWorking` | `◐` | nf-fa-adjust | `*` |
| done | `GSettled` | `✓` | nf-fa-check | `+` |
| stopped by the person | `GStopped` | `■` | nf-fa-stop | `/` |
| incomplete | `GFailed` | `✕` | nf-fa-times | `x` |
| your call | `GNeedsHuman` | `?` | nf-fa-question_circle_o | `?` |
| paused at the cap | `GPaused` | `=` | nf-fa-pause | `=` |

A live row draws the braille spinner instead of the working mark, on the one
shared animation clock — that is the surface's promise that something is
happening THIS INSTANT, and it is why a redrawn-on-change page (the roster, the
record) wears the still mark instead. The linear tier never animates.

### Action families (internal/tui3/actionicon.go's `actionMarks`)

| Family | Slot | Plain | Nerd font | ASCII |
| --- | --- | --- | --- | --- |
| search | `GSearch` | `⌕` | nf-fa-search | `?` |
| read | `GActionRead` | `▤` | nf-fa-file_text_o | `<` |
| edit | `GWrite` | `✎` | nf-fa-pencil | `*` |
| create | `GActionCreate` | `+` | nf-fa-plus | `+` |
| run | `GShell` | `$` | nf-fa-terminal | `$` |
| test | `GActionTest` | `◎` | nf-fa-flask | `!` |
| browse | `GActionBrowse` | `↗` | nf-fa-globe | `^` |
| transfer | `GActionTransfer` | `⇄` | nf-fa-exchange | `&` |
| communicate | `GActionCommunicate` | `»` | nf-fa-comment | `@` |
| coordinate | `GActionCoordinate` | `⇉` | nf-fa-code_fork | `\|` |
| plan | `GActionPlan` | `≡` | nf-fa-tasks | `#` |
| wait | `GActionWait` | `◷` | nf-fa-clock_o | `,` |
| work (the bucket) | `GActionWork` | `▪` | nf-fa-cog | `.` |

The gutter says what family a step is and NEVER how it went: `test` draws a
flask and never a checkmark, because the family is the act of checking and not
its verdict.

The rest of the vocabulary — disclosure, prompts, the place line, the status
line, the spawn tree, the gauges, the prose slots — is in
`internal/tui2/tokens/nerdfont.go`, one binding each, with its argument at its
own binding.

The prompt family is three slots and not two since the questions wave
(`docs/design/questions/DESIGN.md`), because an exchange has two voices in it:

| Meaning | Slot | Plain | Nerd font | ASCII |
| --- | --- | --- | --- | --- |
| the composer, chat | `GPromptChat` | `›` | geometry | `›` |
| the composer, a steer line | `GPromptSteer` | `↦` | geometry | `↦` |
| the answer to a question put back to the asker | `GReplyIn` | `↳` | geometry | `↳` |

`GReplyIn` is deliberately not `GPromptChat`: `›` is the person typing and `↳` is
what came back, and a page that drew both with one mark would make an exchange
unreadable at exactly the moment it matters. All three are punctuation rather
than pictographs, so they are geometry and neither tier swaps the byte.

## Adding a mark

1. Declare the constant in `glyph.go` and add it to `Glyphs()`. That list IS the
   width gate; a constant missing from it escapes every ruler.
2. Add a `GlyphID` in `glyphset.go` and a binding in `nerdfont.go` with
   `Plain`, `NerdFont`, `NFName`, `ASCII`, `UsualTint` and the ambiguity flags.
   A slot that is line GEOMETRY (a rail, a tree corner, a gauge step) takes
   `Geometry: true` and no icon: box drawing is already the right character for
   a grid, and an icon there would be strictly worse.
3. Add its Nerd Fonts name and code to `testdata/nerdfont_glyphnames.json` if it
   is not there. The provenance gate checks the name against the pinned release.
4. Run `go test ./internal/tui2/tokens/`. It will tell you if the mark is two
   cells anywhere, if it is banned, if it duplicates a meaning, if its ambiguity
   flags are wrong, if a non-ASCII slot forgot to auto-upgrade, or if the icon
   is outside BMP private use.
5. Draw it through `palette.glyph(id)` or `app.icon(id)`. Never as a character.

## What this replaced

Before this wave the chat had three vocabularies. `internal/tui2/tokens` held
one; `internal/tui3/actionicon.go` held a private three-tier table with ten
private-use codepoints spelled inline; and the task states were seven constants
scattered over six files with an ASCII twin beside each. They disagreed, and the
disagreement was visible: a stopped task drew `⊘` on the roster and `✗` on its
own page, a run held at its spend gate drew the banned `⏸`, and the fold mark
`▸` and the working mark were the same character on the page that used both.

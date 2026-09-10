# Icons — one vocabulary, three tiers, one door

Owner ruling 2026-09-09. Every mark a person sees in **every surface package**
comes from ONE table, in whichever of three spellings their terminal can draw.
This page is the copy of record: the law, the table as it landed, and how to add
a mark.

**Scope: every surface package.** `internal/tui3` (the live chat),
`internal/tui` (v1, and the visual north star), `internal/head` and
`internal/resident` (the resident). It landed covering `internal/tui3` alone,
which left the other three free to spell marks for themselves — and v1 did, with
its own dotted circle for work waiting on a sibling, its own cross for failure,
and its own flag for work waiting on a PERSON, which is the character this table
keeps for the other kind of waiting. `internal/iconlaw` walks all four on one
list now, and a fifth surface joins the law by being added to that list.

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
   shapes beside their tasks. `internal/iconlaw`'s
   `TestNoSurfaceSpellsAnIconItself` fails the build on one, in any surface
   package, and it runs on every pull request through `make test-laws`. Its one
   carve-out is `resident.NoteMark`: a job-board note is written with `⚑` and
   read back by prefix, so that byte is a PROTOCOL byte in the journal rather
   than a cell on a screen, and routing it through the tier would make stored
   bytes depend on the terminal that wrote them.
   `TestEveryExemptionIsStillReal` fails if that carve-out ever stops naming a
   real line.
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
7. **The tier is chosen once, and one terminal shows one tier everywhere.**
   `tokens.DetectGlyphSet` vetoes the terminals and locales that cannot be
   trusted with private use — no `TERM`, `TERM=linux`, Apple Terminal, a CJK
   locale, legacy conhost — and the Display row (`step icons`: `auto` · `rich` ·
   `plain`, read with `config.IconsAt`) is the person's own say. Every surface
   folds the same two facts the same way:

   | Surface | Folds them in | Draws through | Detects at |
   | --- | --- | --- | --- |
   | `internal/tui3` | `app.iconSet` | `palette.glyph`, `app.icon` | `newApp` |
   | `internal/tui` | `Model.iconSet` (icons.go) | `Model.icon` | `RunWithCommander` |

   v3's ASCII tier is `palette.ascii`'s answer, set by the linear screen-reader
   option; v1 has no linear option, so it resolves `Plain` or `NerdFont` and the
   third tier reaches it the day it grows one. v1 detects **at the door that
   opens a real terminal** rather than at construction, so a window an embedder
   or a test builds keeps the designed plain floor — the tier is on by default
   for anything `DetectGlyphSet` cannot rule out, and a suite that detected would
   assert against private-use codepoints it cannot print.
   `internal/head` and `internal/resident` draw no marks of their own at all
   (their one `⚑` is the protocol byte above), so they hold no tier: they are on
   the law's list to keep it that way.

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

### File kinds (chips, and the gutter beside a call that made or opened one)

| Kind | Slot | Plain | Nerd font | ASCII |
| --- | --- | --- | --- | --- |
| a document — a page of text | `GFileDocument` | `▤` | nf-fa-file_text | `d` |
| an image | `GFileImage` | `⌾` | nf-fa-file_image_o | `i` |
| a sound | `GFileAudio` | `♪` | nf-fa-file_audio_o | `a` |
| a moving picture | `GFileVideo` | `▷` | nf-fa-file_video_o | `v` |

Four kinds, and the set is closed because it is the set a person can hand this
program and the set this program can hand back. A KIND IS NOT AN ACTION: the
step gutter's families say what a call was DOING, these say what the thing on the
end of the path IS, which is why `generate_video` and a dragged-in `.mp4` draw
the same mark. They are one block rather than four scattered slots for the
reason the block exists at all: a row of chips that upgraded three kinds and left
the fourth on the plain floor would draw the exact mixed-repertoire line the tier
was built to end.

`GFileDocument` is `GActionRead`'s byte AND its icon on purpose — a page of text
is a page of text whether a call opened it or a person dragged it in — and
`glyphvocab_test.go`'s named-exceptions table carries the pair. The video mark is
the OUTLINE triangle and not the filled `▶` a chip used to spell: the filled one
is `GQueuePill`'s, and one plain glyph may upgrade exactly one way.

### Destinations

| Meaning | Slot | Plain | Nerd font | ASCII |
| --- | --- | --- | --- | --- |
| where the next thing goes | `GTarget` | `→` | — (geometry) | `→` |

One mark, added 2026-09-09 for home's rule
(`→ new conversation in ~/src/parser · glm-5.3-flash`, internal/tui3's
homedraft.go). It is the only mark in the table about a **destination** rather
than a state, and it is deliberately neither `GScopeUp` (a header pointing back
up a tree) nor `GPromptSteer` (a composer's own prompt).

It is **geometry**: an arrow is already the right character for a grid, and a
Font Awesome arrow would buy nothing and spend a private-use codepoint. Note the
consequence, because it is the one cost of taking a character this common —
U+2192 is now a vocabulary byte, so `internal/tui2/modelui/result.go` needed the
first entry the `literalExemptions` list has had in a while (a `String()` for a
log line). `internal/tui3` is held by its own `iconLawRunes` map, which
deliberately does NOT claim `→`: a dozen hint strings on that surface spell it as
the name of the **right arrow key** (`→ verbs`, `→ opens`, `→ lanes`), which is a
key and not a mark, and a gate that failed on those would be a gate people learn
to ignore.

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

### What a question is doing (`docs/design/questions/DESIGN.md`)

| Meaning | Slot | Plain | Nerd font | ASCII |
| --- | --- | --- | --- | --- |
| it is waiting on you | `GNeedsHuman` | `?` | nf-fa-question_circle_o | `?` |
| it took something for granted and went on | `GAssumed` | `≈` | nf-fa-lightbulb_o | `~` |
| it did the reversible thing and is telling you | `GSettled` | `✓` | nf-fa-check | `+` |
| it no longer needs answering | `GWithdrawn` | `⊘` | nf-fa-ban | `-` |

**Only the first is amber**, and that is the reason there are four slots rather
than one. An assumptions card, a ratify line and a withdrawn line are all things
the asker has already decided; drawing any of them with the attention mark tells
a person to answer something that is not asking them anything, which is how the
one hue that means "waiting on you" stops meaning it. `GAssumed` and `GWithdrawn`
are tertiary and `GSettled` keeps its own green.

The tier has no icon for `≈`: `nf-md-approximately_equal` is a plane-15 address
and B.2 refuses those, so `GAssumed` is the one binding in the table whose two
sides are the same MEANING rather than the same shape — the mathematical
spelling on the floor, the asker's own idea on the tier.

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

## What v1 was drawing before

The window kept as the visual north star had five marks of its own, and two of
them contradicted the table:

| v1 drew | For | Now |
| --- | --- | --- |
| `◌` | a pending node waiting on a sibling | `GWaitsOn` — `⚑`, the mark every other surface already used for that reading |
| `⚑` | a card or brief row waiting on a PERSON | `GNeedsHuman` — `?`, which is what the flag was standing in the way of |
| `✗` | failure, in five places | `GFailed` — `✕` |
| `–` | cancelled, so it would not read as broken | `GStopped` — `■`, which says stopped-by-the-person outright |
| `⚙ $ ✎ ⌕ ⌾ ♪ ▶ ▤` | the tool-call gutter in the activity feed | the action-family and file-kind slots, the bucket on `GActionWork` (whose icon is the cog it was spelling) |

Its hues did not move: the mint, rose, peach and butter styles under every call
site are exactly what they were, because hue is a second, independent axis.

## What this replaced

Before this wave the chat had three vocabularies. `internal/tui2/tokens` held
one; `internal/tui3/actionicon.go` held a private three-tier table with ten
private-use codepoints spelled inline; and the task states were seven constants
scattered over six files with an ASCII twin beside each. They disagreed, and the
disagreement was visible: a stopped task drew `⊘` on the roster and `✗` on its
own page, a run held at its spend gate drew the banned `⏸`, and the fold mark
`▸` and the working mark were the same character on the page that used both.

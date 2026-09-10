---
kind: changed
title: the icon law reaches every surface package, not just the chat
pr: 753
surface: [chat, resident, docs]
invalidates:
  - "The icon law covered internal/tui3 only, and internal/tui3/iconvocab_test.go was where it lived. It lives in internal/iconlaw now and walks internal/tui, internal/tui3, internal/head and internal/resident on one list; a fifth surface joins the law by being added to that list."
  - "internal/tui (v1) did not import internal/tui2/tokens at all and spelled its own marks. It draws every state, action and file-kind mark through Model.icon now, and internal/tui/icons.go folds the Display row `step icons` over tokens.DetectGlyphSet exactly as internal/tui3's app.iconSet does — one terminal, one tier, everywhere."
  - "v1 drew `⚑` for a card or brief row waiting on a PERSON and `◌` for a pending node waiting on a sibling, which is the opposite way round from the shared vocabulary. The flag is waits-on-a-sibling on every surface now, and work that needs a person wears `?`."
  - "v1 drew `✗` for failure and `–` for cancelled work. It draws the vocabulary's `✕` and `■`; the cancelled mark still reads as stopped-on-purpose rather than as broken, which is why it is the stop square and not the cross."
  - "v1's activity feed spelled its own tool-call gutter — `⚙ $ ✎ ⌕ ⌾ ♪ ▶ ▤`. Those are slots now, and the bucket is tokens.GActionWork, whose nerd-font icon is the cog that line was spelling by hand."
  - "tokens had no slot for a file kind, so internal/tui's attachment and media chips spelled four characters. It has GFileDocument, GFileImage, GFileAudio and GFileVideo, on Font Awesome 4's outlined file family."
  - "A video chip drew `▶`. It draws `▷`: the filled triangle is tokens.GlyphQueuePill's byte, and one plain glyph may upgrade exactly one way."
  - "tokens named U+F15C `nf-fa-file_text_o` in the pinned glyphnames extract. That name is U+F0F6 in ryanoasis/nerd-fonts 3.2.1 and U+F15C is `nf-fa-file_text`; the binding is renamed and the extract regenerated from the release. No drawn character changed."
  - "resident.NoteMark is the one carve-out from the law, and it is written down as one: `⚑ ` is a protocol byte a job-board note is written with and read back by prefix, not a cell on a screen, so it stays a literal. TestEveryExemptionIsStillReal fails if that carve-out ever stops naming a real line."
---

The law landed in #711 as one surface's test, and a law that covers one surface
of four is how the same shape came to say two different things in one product:
v1's flag meant "waiting on you" while the vocabulary's flag means "waiting on a
sibling". v1's restraint is intact — no hue moved, and a shape changed only where
the shared vocabulary already had a different character for the meaning the
window was drawing.

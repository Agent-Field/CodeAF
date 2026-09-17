---
kind: changed
title: The wordmark is drawn `CodeAF`, four rows tall, with the capitals on the `d`'s cap line
pr: 1106
surface: [chat]
invalidates:
  - "The wordmark was three rows. It is four: the x-height letters (`c`, `o`, `e`) sit in the lower three and leave the top row blank, and the capital `A`, the capital `F` and the `d`'s ascender start in the top row. `wordmarkRows` returns four rows and `wordmarkGlyphs` is `map[rune][4]string`; anything holding three rows, or a `[3]string`, is holding the old letterforms."
  - "The last two letters were lowercase forms, and the `a` was `┌─┐ / ├─┤ / └─┘`, which reads as an `8`. They are a capital `A` (`┌─┐ / │ │ / ├─┤ / ╵ ╵`) and a capital `F` with the longer top arm (`┌── / ├─  / │   / ╵  `). The whole word is `          ╷     ┌─┐ ┌──` over `┌─  ┌─┐ ┌─┤ ┌─┐ │ │ ├─ ` over `│   │ │ │ │ ├─╴ ├─┤ │  ` over `└─  └─┘ └─┘ └─┘ ╵ ╵ ╵  `."
  - "A bar in box-drawing sits at the middle of its cell and only a vertical reaches the edge, so the `d`'s ascender is now the half-stroke `╷` (its top is the capitals' top bar, not the edge above it) and every stem that ends on the baseline ends as `╵` (it stops where `└─┘` does). A `│` in the bottom row is a descender and nothing else. `d` is `  ╷ / ┌─┤ / │ │ / └─┘`, with a bowl the height of the `o`'s."
  - "The DRAWING is mixed case and the TEXT is not: `product` is still `codeaf`, the glyph table is still keyed by its lowercase letters, and a terminal that cannot draw boxes still gets the word `codeaf`. The lowercase spelling law over sentences, titles and `--help` is unchanged; only the letterforms under two of its letters are."
  - "`internal/manual/chat/empty-screen.md` drew the retired `aforge` letterforms, with the pre-terminal `e`, under the label \"the codeaf wordmark\". It draws the shipped letterforms now. The name law walks text and cannot see a name spelled in box-drawing, so that page had spelled the old name in pictures since the rename."
---

Two closed bowls of equal size stacked on each other are an `8`, whatever letter
they were drawn for, and that is what the second-to-last glyph of the product's
own name had been on the first screen of every fresh install. The owner's answer
was to have the two letters at the end stand up out of the word as capitals —
and a capital's top is the ascender's top, which three rows of box-drawing could
not give it: a bar is drawn at the middle of its cell, a stem reaches the edge,
so the `d` stood half a row over the `A` and the `F` however they were formed.
The fourth row is that half a row made whole, with the x-height letters a row
lower and the half-strokes `╷` and `╵` putting the ascender's top and every
capital's feet on the same two lines the bowls use.

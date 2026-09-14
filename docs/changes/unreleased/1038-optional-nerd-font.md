---
kind: fixed
title: unknown terminal font support keeps icons plain and Nerd Font is opt-in
pr: 1038
surface: [chat, docs]
invalidates:
  - "The default auto icon mode enabled Nerd Font private-use characters whenever terminal detection found no veto, so iTerm2 with an ordinary font drew boxed question marks across the surface. Unknown font coverage now stays plain; terminal identity, colour depth and locale never establish Nerd Font support."
  - "Installing a patched font was described as enough to get rich icons automatically. This build has no reliable cross-terminal glyph-coverage probe, so auto never promotes to Nerd Font; selecting rich under Display's step icons row explicitly enables it, and returning to auto restores standard symbols."
  - "The manual said both that icons needed no patched font anywhere and that the normal view used Nerd Font icons. It now distinguishes the plain default from the optional rich tier, and explains iTerm2's separate non-ASCII font setting."
---

A cursor-position probe cannot prove that an icon rendered: the missing-glyph box
can occupy the same cell. Terminal-specific font-name queries also do not provide
a portable guarantee of glyph coverage. The automatic choice therefore stays
conservative without adding terminal I/O or a startup wait. The existing rich,
plain and accessible renderings remain available through the same vocabulary.

PR validation also exposed a pre-existing folder-browser test timeout on unchanged
`dev`: its initial store read scanned the developer's real home before the test
delivered its own roots. That test now supplies a fresh store in an isolated state
root, preserving the same mid-browse assertions without a real-home scan.

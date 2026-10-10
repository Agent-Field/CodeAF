# Desktop colours and text selection

## Why does selected text use the place tint in Light and Dark?

Selecting text in the codeaf desktop app draws a soft accent background from
its current place tint. Light and Dark use their respective soft accent colours.
The highlight marks the text you selected; it does not indicate work status or
change the text. Buttons keep their text unselectable.

## Why is the background dimmer behind an overlay in Dark appearance?

Quick Look and the command palette dim the content behind them with a shared
black scrim: 20% in Light appearance and 40% in Dark. The stronger Dark scrim
is the current design assumption for legibility. It does not change the
underlying content or signal a work state.

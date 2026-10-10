# Desktop control appearance

## Why do desktop chrome buttons have the same keyboard focus ring?

Desktop chrome buttons use the shared controls. Keyboard focus draws a 2px accent ring with a 4px soft halo. Clicking with the pointer does not draw that ring. Disabled controls use 40% opacity. Menus and hover previews use the theme's surface and shadow, so they follow Light and Dark appearance.

## Why are added line counts different in Light and Dark appearance?

Added line counts in desktop history and previews use the same theme-aware success color. Changing appearance changes that color in both places together. Counts come from recorded changes; unknown counts render nothing.

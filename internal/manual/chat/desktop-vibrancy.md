# Desktop frame transparency

## Why is the desktop sidebar translucent or blurred on macOS?

Only the desktop frame uses translucency: the rail, tab strip and frame toolbar. On macOS the native sidebar material sits beneath the webview, with the native traffic lights in the overlay title bar. Windows requests Mica where supported; Windows versions before Windows 11 use a solid frame. Linux and browser previews use a tinted frame blur when available.

Conversation cards, panes, the composer, question tray, popovers, sheets and Settings use opaque paper. Content never shows the desktop behind it, and native frame material never adds a CSS blur above the native blur.

## Why does the desktop frame turn solid when inactive or with reduced transparency?

An inactive window uses a solid frame. Reduced transparency and increased contrast also use a solid frame. Opening a layer with its own backdrop blur makes the frame solid so two blurs do not stack. Every new or reopened desktop window inherits the main window's native material and title-bar configuration.

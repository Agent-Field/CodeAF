# CodeAF visual standard

An Arc-inspired quiet workspace: compact sidebar, generous content space, a single accent, native window chrome. Reference the restraint, not Arc branding.

## Tokens

All primitives live in `src/styles/tokens.css`. Components consume semantic names.

| Role | Light | Dark |
| --- | --- | --- |
| Canvas | #f8f7f5 | #1c1b20 |
| Sidebar | #f0eeeb | #232127 |
| Surface | #ffffff | #28262e |
| Text | #28262d | #eeecf2 |
| Secondary text | #6f6a76 | #b0aab9 |
| Primary accent | #6850b8 | #b7a0f2 |

## Typography

Use OS system sans: SF Pro on macOS, Segoe UI on Windows, the installed system sans on Linux. Never bundle Apple's fonts. Use system monospace for code. Body 14–15px, secondary 12px, labels 10px, headings 32–46px. Body line-height 1.6–1.8. Reserve tight tracking for headings; do not reduce body legibility.

## Geometry & interaction

Spacing scale: 4, 8, 12, 16, 24, 32, 48px. Corners: 8px controls, 12px panels, 20px major surfaces. Sidebar 248px; native window minimum 800×560. Use borders before shadows; avoid gradients and decorative blur. Primary actions use the accent; navigation uses its soft surface. Motion 140ms and disabled for reduced motion. Visible focus required; Cmd/Ctrl+K opens the command palette. Use OS window controls and dialogs for future filesystem actions.

## Appearance

System is the default; Light and Dark persist locally. Listen through CSS to OS appearance changes while System is selected. Native feel is a design goal, not a claim that React controls are platform-native widgets. Tauri uses the platform WebView; validate font metrics and behavior on each OS.

## Foundation components

Application shell, sidebar navigation, toolbar, primary button, empty state, status panel, theme selector, modal command palette. The Design system screen shows live tokens. Build future screens from these primitives and extract shared components as real reuse appears.

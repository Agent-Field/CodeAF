# CodeAF repository contract — all contributors and agents

These rules apply to every change in this repository, including nested features. Preserve the approved minimal Arc-like design. A new screen is not permission to introduce a new visual language.

## 1. Read before changing UI

Read `docs/DESIGN.md` and inspect the live Design system screen. Reuse existing primitives in `src/components/ui/`. Check the smallest supported desktop window and both Light and Dark appearance. Honor explicit user instructions; record intentional design changes in the standard rather than creating hidden exceptions.

## 2. Use the single sources

- `src/design/tokens.json` owns colors, spacing, dimensions, radii, type sizes/weights/leading/tracking, icon stroke/motion, shadows, native window geometry and the monochrome brand mark.
- `src/styles/tokens.css`, favicon/mask SVGs and native icon source are generated. Do not hand-edit them. Change the source and run `npm run design:generate`; run `npm run brand:icons` when changing the brand.
- `src/styles/ui.css` and `src/components/ui/` own reusable control appearance and behavior. Screen CSS owns layout only and uses existing token references.
- `src/design/ThemeProvider.tsx` owns appearance persistence, system-theme resolution, native-window appearance and reduced motion. Do not reimplement these per screen.
- `src/design/index.template.html` owns the HTML shell. Native Tauri config owns permissions/build/bundling; its main-window geometry is generated from design tokens.

## 3. Non-negotiable design rules

Use shared Button, IconButton, NavigationItem, SidebarAction, TextInput, Select, ThemeSelect, Typography, Surface, Separator and KeyboardShortcut components. Add or extend a shared primitive when needed; do not clone one into a feature. No inline styles or arbitrary CSS color/length/type/opacity values. The only fixed media-query thresholds are the declared breakpoints, checked against tokens. Proportional layout values such as percentages, fractions and viewport units are permitted.

Lucide outline through `src/components/ui/Icon.tsx` is the ONLY UI icon family. The implementation is `@animateicons/react` with explicit per-icon imports. Add semantic names in that central registry. No direct icon-package imports elsewhere; no mixed Heroicons/Tabler/Phosphor/Hugeicons, emoji icons, icon fonts, inline hand-drawn SVGs or per-screen stroke overrides. Icons use currentColor, the declared size roles and one central stroke weight. Only the shared BrandMark may render our own SVG artwork.

Motion must communicate a state change. Navigation, favorite, search, settings and plus glyphs stay still. Only explicitly opted-in directional arrows translate; disclosure chevrons rotate with open state. Durations, curves and keyframe distances come from tokens; shared keyframes live in ui.css. Sidebar collapse uses the central layout transition and makes hidden controls inert. Reduced motion removes interpolation while preserving instant state indicators. No idle loops, decorative morphs, per-screen keyframes or upstream default icon animations. Every interactive control must implement themed hover, pressed, focus-visible and disabled states; persistent selection uses aria-current or aria-pressed. Use shared Select for app menus; native HTML select and direct Radix imports outside its boundary are prohibited. Icon-only controls require an accessible name. Keep visible keyboard focus, modal focus restoration, native window controls and drag regions. Never simulate Mac traffic lights on Linux. Avoid promotional hero cards, gradients within content, large saturated navigation states, unnecessary borders or decorative UI. Preserve macOS native sidebar material and Linux's honest tinted fallback.

## 4. Required gate

Run `npm run check` before committing: it checks generated assets, design policy, TypeScript/frontend build, policy regression tests and Go contracts. Do not bypass, silence or weaken `design:check` to make a feature pass. A deliberate standard change must update tokens/components, documentation and the live specimen together; explain the reason in review.

For UI changes also run `npm run test:ui` in Chromium and WebKit. Extend the reusable theme/accessibility contracts for new controls; verify Light, Dark, System, reduced motion, keyboard navigation and control states. Do not suppress accessibility failures or treat these checks as a substitute for visual review.

For native changes also run `npm run engine:build` and `cargo fmt --check --manifest-path src-tauri/Cargo.toml`, then validate native compilation on macOS and Linux. Browser screenshots do not prove native materials or sidecar behavior. CI calls the same gate; unavailable CI must be reported honestly. The root `CLAUDE.md` imports this contract so Claude-based contributors follow the same rules.

## 5. Architecture and evidence

Stack: Tauri 2 shell, React/TypeScript UI, Go sidecar. Rust stays a narrow typed native bridge. Do not grant renderer arbitrary shell access. No model/agent execution exists yet: do not depict fixtures as live sessions. Keep screenshots and recordings outside the source tree; attach review evidence directly to the relevant PR or report the attachment blocker. Production brand icons are app assets, not review evidence. Keep private infrastructure details out of public review content.

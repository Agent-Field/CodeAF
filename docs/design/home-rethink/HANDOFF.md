# Home Rethink — local copy of the Claude Design project

Source: https://claude.ai/design/p/0b9b85a6-e381-4265-a10d-16a19ab8a8b9
Project: "Aforge home redesign discovery" · pulled 2026-08-25

**Complete.** All 6 files the project holds are here, byte-for-byte.

## Files

| Path | Bytes | What it is |
| --- | --- | --- |
| `Home Rethink.dc.html` | 108,666 | **The design.** One long dark-mode page of TUI mockups: screens 1a–1g, 2a–2f, 3a–3e. Loads `./support.js` relatively, so open it from this directory. |
| `support.js` | 69,151 | Claude Design canvas runtime (`x-dc` element, `DCLogic` base class). Generated — do not edit. Pulls React 18.3.1, ReactDOM and Babel standalone from unpkg at runtime, so viewing needs network. |
| `github.md` | 2,181 | The project's own sync note: which repo file each screen was built from. **Read this first** — it is the map from mockup to Go source. |
| `uploads/home-concept-inventory.md` | 17,415 | The brief the design answers. Identical to `docs/home-concept-inventory.md` in the repo. |
| `uploads/Screenshot 2026-08-25 at 1.11.41 PM.png` | 376,210 | Reference shot of the current home screen, 2626×1838. |
| `.thumbnail` | 24,416 | The project's gallery preview card, WebP 593×390. Hidden file. |

## Viewing

```sh
cd "docs/design/home-rethink" && python3 -m http.server 8765
# http://localhost:8765/Home%20Rethink.dc.html
```

`file://` also works, though some browsers block the relative script tag.

## Note for the implementing session

`github.md` says `path: internal/tui2` and every screen maps to `internal/tui2/...`.
Per this repo's CLAUDE.md, **`internal/tui3` is the live surface** and `internal/tui2`
is the older one. Settled 2026-08-25: the build targets `internal/tui3`; the tui2 paths
are only where the palette and glyph tokens live, which tui3 imports. The plan and the
decisions are in `LANES.md`; `RECON.md`, `DATA-AUDIT.md` and `PAGES.md` are what it rests
on; `SCREENS.txt` is every artboard's text for a session without a browser.

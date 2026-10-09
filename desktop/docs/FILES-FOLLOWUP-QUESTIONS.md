# File and diff follow-ups

Questions and gaps from the file/diff follow-up. Root consolidates these into `DESIGN-QUESTIONS.md`. This page does not edit that file.

The design's edge copy that this change could match is matched. What follows is what the product still cannot say, or says differently, and why.

| ID | What is still open | What the code does |
| --- | --- | --- |
| FF1 | `base.kind` is only `start` or `head`. `head` means both "no start was recorded" and "the start commit is gone". | The Changes view shows "Compared with the latest commit. The commit this conversation started on is no longer in history." whenever `base.kind` is `head`. A never-recorded start gets the stronger sentence. No new kind was invented. |
| FF2 | The Open in menu in the specimen is about 220px wide, and its highlight reads as field-2. | The shared menu stays 180–320px (`--menu-min-width`, `--menu-max-width`) and highlights with `--menu-highlight`. This change does not restyle every menu. |
| FF3 | The specimen's Open in row is "Copy path ⌘⇧C" and does not show "Copy relative path". | "Copy relative path" stays, under Copy path. It was already the handoff, and removing it would drop a shipped action. |
| FF4 | The outside-git example is "~/scratch · not in git". | The directory is the workspace-relative parent, then " · not in git" (middle dot). A file with no parent is "not in git". The specimen path is `internal/parse/lexer.go`, so that card reads "internal/parse · not in git". |
| FF5 | A headless or remote engine might want the reason drawn in the menu. | The menu does not draw a reason sentence. Remote: empty editor list, copy items only. Headless: the list may still be returned with `open: false`, and the menu does not offer to launch. The reason is on the API response for a caller that can show it. |
| FF6 | macOS should list every editor that can open the file, default first. | On macOS the list is the default application only, from a fixed `osascript` query. If that query fails the list is empty. The query was not run on a Mac in this change. Linux reads real `.desktop` handlers (Name, MimeType, TryExec) for the file's type and its shared-mime-info parents, so a `text/x-go` source finds the editors registered for `text/plain`. The default is `xdg-mime query default` for the first type in that family that has one. Hidden entries, anything that is not `Type=Application`, and an entry whose `TryExec` program is not installed are skipped. `Exec=` is never read and never run. `mimeapps.list` added associations are not read, so an editor a person associated by hand without a MimeType line is missing. |
| FF7 | The tab should refresh when the file on disk changes, including a save from the editor or a shell command. | Refresh follows the session stream: `edit`, `write`, `multiedit`, or `apply_patch` whose arguments name this path, or a finished turn whose change list names it. One read, after 1 second, so a burst is one read. The stream drops while the tab or the document is hidden, and it stops after four failed reconnects. There is no file-system watch and no model call. A shell command and a save in another program do not name the file, so the tab stays as it was until it is opened again or a named edit arrives. |
| FF8 | The file chip's full path should be announced with the tooltip. | The chip uses the shared tooltip (delay, warm, shadow). The shared tooltip does not set `aria-describedby`. `Tooltip.tsx` was not edited; that link is a root patch. |
| FF9 | One discovered editor versus several. | One launchable editor keeps the "Open in editor" button and starts that editor. Several become "Open in", default first, with the word "default" after the name. When the editor route is missing, the previous opener remains if this machine is the engine's machine, so an older bridge does not lose the handoff. |
| FF10 | Type icons and an image view for a file tab. | Not in this change. |
| FF12 | Refresh matches paths. | Two relative paths match only when equal; an absolute path matches the relative path it ends with. An edit of `cmd/a.go` does not refresh a tab showing the root `a.go`. |
| FF13 | The node unit tests under `src/features/files/*.test.ts` are not in `npm run check`. | They run with `node --test --experimental-strip-types`. Adding them to the gate is a root `package.json` change. |
| FF11 | Header wrap at 320 and 560, and the compact breakpoint. | The header wraps at the declared small breakpoint, 600px, so 320 and 560 both wrap. Nothing new happens at 850. The directory shrinks before the file name and fades with the existing mask. |

## Editor API

`GET /sessions/{id}/editors?path=` answers `{ editors: [{ id, name, default }], local, open, reason? }`.

`POST /sessions/{id}/editors/open` takes `{ path, id }`. The id must be one this process just listed for that path. The body has no command line. Launch is `gtk-launch` or `gio launch` on Linux, and `open -b` on macOS. A path outside the workspace is refused the same way file locate refuses it. Another machine answers 409 and does not list this machine's editors. No display answers 409 and does not start a program.

At most eight editors. The default is first. A machine that cannot list editors returns an empty list and says so. Nothing here is a made-up Photoshop or Preview.

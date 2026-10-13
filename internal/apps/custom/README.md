# Custom screens on the server

The Builder may include a single-level `custom/<name>.tsx` definition file.
The spec loader retains its source alongside the JSON, YAML and agent files.
A screen uses it with `{"part":"Custom","props":{"file":"custom/<name>.tsx"}}`.

Check reports `kit/custom` when the referenced file is missing or its path is
wrong. Every custom source file, including an unused one, must be at most
65,536 bytes and import only `react` and `@codeaf/bridge`. Reexports and
TypeScript-only imports obey the same restriction. Local files, URLs and
package subpaths are refused.

The source may not use `fetch`, `XMLHttpRequest`, `WebSocket`, `EventSource`,
`eval`, `Function(`, dynamic `import(`, `window.top`, `parent.`,
`localStorage` or `document.cookie`. Whitespace and comments between the
tokens do not exempt them. A source issue carries its file, `kit/custom`
rule and `line N:` in its message; a missing reference carries the screen's
JSON pointer. This preserves the wave-1 issue format.

`custom.bundle` accepts `{file}` in an app scope and returns `{js, hash}`.
It reads files and version from one `Store.Get` snapshot, validates again,
and compiles with the pinned Go esbuild API. The result is one browser ES
module with the two permitted imports external. JSX uses `React.createElement`;
the source imports React when using JSX. The hash is SHA-256 of the source.
Repeated calls reuse a bundle for the same app, version, file and hash.
Changing source replaces that file's cache entry; changing version drops
the app's previous entries.

Invalid paths return `invalid`, absent files return `not_found`, and rejected
or malformed source returns `check_failed` with `details.issues`. The usual
gateway capability check still precedes the service. The desktop's iframe
sandbox remains responsible for isolating the running module.

`Store.WriteFiles` accepts these canonical custom paths alongside the existing
definition formats. The same allowance preserves their source in draft reads
and kept versions, so `Store.Get` supplies the source to the bundler. Check
owns source validation; the store can save unfinished Builder files for a fix
round.

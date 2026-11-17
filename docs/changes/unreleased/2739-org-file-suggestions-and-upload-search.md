---
kind: fixed
pr: 2739
title: An organization's agent suggestions reach the document, and Files search reads uploads
surface: [engine, app]
invalidates:
  - "On an organization's server, ws_file_suggest kept Afi's suggested edits as page versions nobody was shown, while Afi said they were waiting; the workstream's suggestions list stayed empty. Each suggestion is now one of the workstream's suggestions (GET /v1/workstreams/{id}/suggestions), with each edit's exact find and replace words, and the desktop app draws it on the document with Keep and Drop. A whole-page rewrite Afi may not make still goes to the page's History as Kept · not applied."
  - "A Files search on an organization's server matched a file's name and a document's words only. It now also matches an uploaded text file's words (CSV, Markdown, plain text) and a PDF's text layer, read once per upload and remembered by its bytes' sha256. A picture is still found by its name alone."
  - "ws_files described its query as matching names only. It matches names and words."
---

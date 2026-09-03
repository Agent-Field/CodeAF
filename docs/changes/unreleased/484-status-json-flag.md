---
kind: added
title: /status --json prints the status note as one JSON object
pr: 484
surface: [chat]
---

`/status` answers a new argument: `--json` prints the same facts the text form prints as one
JSON object — the labels are the keys, the values are strings, and the keys come in the same
order the text form prints them — because a wire caller and a reader asking the same question
of the same session should get the same list, not two assemblies. The list is one function
([app.statusItems], split out of [app.statusText]) behind both forms, so they cannot drift;
the object is built from the ordered slice by hand, since a Go map randomizes key order and
the order IS the contract. Any other spelling after the command name falls through to the
bare text note, the route /status has always answered on.

The emptiness law survives the serialization: a fact this session does not have is not a
key — never `null`, never `""`, never a zero — so the object keeps exactly the silence the
aligned form keeps. And it is a NOTE and not a pipe. It prints into the conversation like
every other one, which means it wraps to the width of the terminal; there is no `--json`
that writes a file and nothing to redirect, and the manual page says so rather than
promising a stream that does not exist.

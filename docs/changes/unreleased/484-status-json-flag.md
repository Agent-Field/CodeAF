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

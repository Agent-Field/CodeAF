---
kind: fixed
title: Read short office documents without discarding their text
pr: 2401
surface: [chat, engine, docs]
invalidates:
  - "Short office text previously triggered a fallback parser and could be lost when that parser failed. Complete short office text is now returned directly."
---

`read_document` accepts one-line office text without applying the PDF text-length
heuristic. Invalid UTF-8 and binary control characters still fall through to the
next parser and are refused if no parser returns readable text. When all parsers
fail, the error names them and suggests `read` for local text extraction.

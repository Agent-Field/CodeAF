---
kind: fixed
title: a question with evidence under each answer reaches the person on the first ask
pr: 797
surface: [engine, chat]
invalidates:
  - "A tool argument that arrived as a JSON string holding the list or object it was meant to be (`\"options\":\"[{…}]\"`) was refused with `Invalid arguments: options takes a list`, and deepseek-v4-flash sent `ask` that way three calls running until the loop guard ended the turn. The decoder now reads a string holding the wanted shape as that shape, once, at every level the type asks for; a string holding anything else is refused in a sentence that says it arrived as text."
  - "`ask` was the one tool on the belt whose schema used `$ref` into `$defs` for its evidence block. The block is written out in full at both places it stands, and a structural test (`TestEveryBeltSchemaIsWellFormedAndCarriesNoReference`) fails the build on any belt schema that is not one well-formed JSON document or carries a reference."
  - "An answer sent with a key and no label reached the person as a row with nothing to read. It is refused before it is drawn: `every answer needs a label a person can read, and answer 2 has none: write one, or drop that answer`."
---

The measured defect: the owner asked for a room with a diagram under each answer
and saw nothing, because the model could not make the call. The e2e subtest
`ACardWithABlockUnderEachAnswerArrivesWhole` is steered in prose and reads the
journal for one `ask` and no refusal, so the fix is held to the model's own
shape and not to an argument object a test spelled out.

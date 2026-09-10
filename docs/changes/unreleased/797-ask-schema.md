---
kind: fixed
title: a question with evidence under each answer reaches the person on the first ask
pr: 797
surface: [engine, chat]
invalidates:
  - "A tool argument that arrived as a JSON string holding the list or object it was meant to be (`\"options\":\"[{…}]\"`) was refused with `Invalid arguments: options takes a list`, and deepseek-v4-flash sent `ask` that way three calls running until the loop guard ended the turn. The decoder now reads a string holding the wanted shape as that shape, once, at every level the type asks for; a string holding anything else is refused in a sentence that says it arrived as text."
  - "The same model, far more often, opens that quote and never closes it: the list AND every field after it (`pick`, `reason`, `scope`, `stakes`) arrive inside one string, and the outer object has three fields where nine were meant — eleven of eleven refused `ask` calls measured on 2026-09-10 had this shape. The decoder recognises an object's swallowed tail (the string is not JSON alone, but closes into an object when put after its own field name) and reads the object it was; what follows a complete value inside such a string (a stray `]`, a leaked `<｜…｜>` control token and prose) is the model's spill and is dropped, and nothing is read out of it; a backslash before a real newline inside such a string is read as the newline it meant, and no other bad escape is guessed at; what the model wrote at the top level is never overwritten by the copy inside."
  - "A number sent where text was wanted was refused (`Complexity takes text: send {\"Complexity\":\"1\"}, not 1`), and `ask`'s comparison axes invited exactly that. A number in a cell of a text table is now read as its own spelling — only there; a number where a named text argument was wanted (`bash {\"command\":17}`) is still refused. The `dimensions` schema declares its values as text."
  - "A pick written as nothing but its key — `\"pick\":1` or `\"pick\":\"1\"` — was refused with `pick takes an object`. `Pick` reads both bare forms as that key, and the object form goes back through the one decoder, so what is inside it is read and refused by the same rules as every other argument."
  - "`ask` was the one tool on the belt whose schema used `$ref` into `$defs` for its evidence block. The block is written out in full at both places it stands, and a structural test (`TestEveryBeltSchemaIsWellFormedAndCarriesNoReference`) fails the build on any belt schema that is not one well-formed JSON document or carries a reference."
  - "An answer sent with a key and no label reached the person as a row with nothing to read. It is refused before it is drawn: `every answer needs a label a person can read, and answer 2 has none: write one, or drop that answer`."
---

The measured defect: the owner asked for a room with a diagram under each answer
and saw nothing, because the model could not make the call. The e2e subtest
`ACardWithABlockUnderEachAnswerArrivesWhole` is steered in prose and reads the
journal for one `ask` and no refusal, so the fix is held to the model's own
shape and not to an argument object a test spelled out.

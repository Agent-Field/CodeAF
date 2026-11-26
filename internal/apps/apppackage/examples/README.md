# Kit examples

Package only acceptance-passing releases and executable Look goldens here.
The adjacent `<archive>.json` needs `"acceptance": "passed"` or
`"acceptance": "golden"`, and `"accepted_hash"` equal to the archived manifest's
hash. The archive's own `sample.json` must have at least 30 records. Recheck
replacements against the acceptance rubric before recording the new hash.

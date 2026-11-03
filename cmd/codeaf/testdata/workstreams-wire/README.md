# workstreams-wire

The examples the desk's contract carries; regenerate when the contract changes:

    python3 -I extract.py <the contract's markdown> .

`contract.json` holds the method index, the event, the error codes, the enums and every
field table (type, required). Each other file holds one method's or event's examples.
`appworkstreams_conformance_test.go` sends the examples through the wire handler and
compares each answer's shape with them. Values may differ; shapes may not.

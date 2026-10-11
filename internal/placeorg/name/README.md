# Place naming stage

`Stage` implements `placeorg.Namer` and `placeorg.Judged`. It reads the forest
in preorder and returns `OpName`; only the Runner changes the tree. A branch's
members include its descendants and parked chats. Probes and duplicates do not
contribute to frequencies, support, spelling or example titles.

`NewIndex` computes corpus document frequencies once. `Candidates` reproduces
the reference's lowercase tokenisation, support of at least two members,
maximum document frequency of 60%, score `tf * log(n / (1 + df))`, and stable
top-four order. Generic-word filtering happens after ranking, as in the reference.

Root charters use `RolePlaceCharter` (40 output tokens); names use
`RolePlaceName` (30). Names have one or two words, are unique case-insensitively
across the tree, and contain member words. The spelling most frequent in member
titles wins, with message spelling as a backstop and first occurrence breaking
ties. Code never capitalises names. A semantic pick may combine member words
outside the top-four candidates.

When judgment falls back or returns an invalid answer, the first usable untaken
candidate wins. If none exists, the stage returns no naming operation rather
than inventing a placeholder. The Judge receives a nonempty no-name sentinel
in that case, which never becomes an operation. Existing named and person-made
nodes are preserved. Cancellation returns no partial operations.

## Saved reference proof

The private corpus is never committed. Generate external input with:

```sh
PLACEORG_REF=/path/to/owner-algo/ref
/path/to/python-with-numpy internal/placeorg/name/testdata/reference_candidates.py "$PLACEORG_REF" > /tmp/name-reference.json
PLACEORG_NAME_FIXTURE=/tmp/name-reference.json make test-focus PKGS=./internal/placeorg/name RUN=TestSavedReference
```

The extractor evaluates the original reference's scoring functions without
executing its model or structure pipeline. Saved embedding row IDs and saved
assignments fix the population and memberships; no embeddings are fetched.
ARI belongs to the structure stages, while this stage compares candidate
lists exactly.

On the owner's saved run: all 23 candidate lists match. Four replay answers
violate member-word validation; three receive a supported candidate and one
produces no operation. There are 22 emitted names. Exact raw reference-name
parity is intentionally incompatible with the required member spelling and
with rejecting unsupported words. The replay test checks both paths.

The root builder and Runner are still stubs on this branch, so stage tests use
node snapshots and the public empty-tree interface. This lane changes no
user-facing door; the bridge/manual lanes own the eventual feature's manual.

## Integration gate still outside this lane

`make test-laws` currently fails
`cmd/codeaf-replay.TestEveryRoleWordIsDeclaredWhereTheVocabularyIs`: all six
role constants in the frozen `internal/placeorg/judge.go` belong in
`internal/roles/roles.go`. Both files are unchanged by this lane. The placeorg
contract laws, focused stage tests, whole-tree build and placeorg vet pass.
The role-door lane must resolve the vocabulary declarations before integration.

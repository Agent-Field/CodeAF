# Exact cosine neighbours

`Builder` implements `placeorg.Neighbours`. Its zero value uses K=10 and
floor=0.35. `New(8, 0.35)` selects the harness's parity settings;
`New(k, 0)` permits an explicit zero floor.

## Algorithm and boundaries

The stage validates and normalises an owned float32 copy of the vectors once.
Each block of 512 rows has one goroutine. Each row scans every other row and
keeps its strongest K cosine scores at or above the floor. Ties choose the lower
row index. A zero vector has no neighbours. Ragged, empty or nonfinite vectors,
invalid thresholds and mismatched ID counts return an error. Cancellation
returns an error and never a successful partial graph.

The union of the directed selections becomes a sorted undirected edge list,
with each pair appearing once and I < J. Anchor evidence is added **after**
selection. Its boost is `0.5 × sum(min(a, b)) / sum(max(a, b))` over the union
of anchor keys, where a and b are the upstream IDF weights. Negative and
nonfinite weights are ignored. Empty evidence adds zero; identical nonempty
sets add 0.5. Thus rare shared anchors count more than common shared anchors,
and the total boost never exceeds 0.5. Stable key order makes the sum
reproducible. No subject vocabulary is built into this package.

The working set contains the normalised n×dims vectors and n×K candidates and
edges. There is no n×n similarity matrix. Caller-owned vectors remain unchanged.

## Repeat the proof

From the repository root:

```sh
make test-focus PKGS=./internal/placeorg/knn RUN='Test' TEST_FLAGS='-count=1 -v'
GOMAXPROCS=8 make test-focus PKGS=./internal/placeorg/knn RUN='^TestTenThousandBudget$' TEST_FLAGS='-count=1 -v'
```

The budget test uses 10,000 synthetic 256-dimensional vectors with shared
latent directions and noise, plus group and row anchors weighted by IDF, so it
retains more than 50,000 edges. It records
wall time and `runtime.MemStats.TotalAlloc` across the whole stage, including
normalisation and the returned graph, and enforces <5 seconds and <50 MB.
The supplied vectors and anchors are excluded from that allocation delta. `-short` skips the
performance test. The independent float64 full-sort oracle covers 1,031 rows
across three blocks; the small saved NumPy fixture runs without Python.

For the owner's saved embeddings, first export the harness's W keys:

```sh
python3 internal/placeorg/knn/testdata/reference_edges.py "$PLACEORG_REF" /tmp/placeorg-knn-edges.json
PLACEORG_KNN_EDGES=/tmp/placeorg-knn-edges.json make test-focus PKGS=./internal/placeorg/knn RUN='^TestSavedReferenceEdgeParity$' TEST_FLAGS='-count=1 -v'
```

Set `PLACEORG_REF` to the private reference directory. Python needs NumPy;
it never loads chat text or calls a model. The Go parity test skips when
`PLACEORG_REF` is absent, and requires the exported keys when it is present.
The script reproduces the harness's matrix multiplication, argpartition,
floor and symmetric union. Replacing its anchor boost with the derived boost
cannot change W's keys because both are added after cosine selection; weight
behaviour is covered separately by the IDF overlap and selection tests.
Neither the reference corpus nor its exported edge keys belongs in git.

## Measured acceptance

On 2026-10-10, saved `emb_all.npy` (389×1536), K=8, floor=0.35:
reference **2,162** edges; port **2,162** edges; intersection **2,162**;
edge-set Jaccard **1.000000000** (required ≥0.9).

On the same machine with `GOMAXPROCS=8`, 10k×256:
**1.486 seconds**, **31.06 MB** allocated, **83,078** edges
(required <5 seconds and <50 MB). This is an allocation ceiling, stronger
than checking only the retained heap after garbage collection.

This lane changes a structural stage only, with no desktop or manual surface.
The wave's integration change owns its user-facing manual and change entry.
No desktop design-question ledger rows are needed for this package.

## Verification gap outside this lane

`make build`, `go build ./...`, `go vet ./internal/placeorg/...`, all
`internal/placeorg/...` tests with saved-reference parity, and focused race
checks passed. The whole-tree `make test-laws` found a baseline failure in
`cmd/codeaf-replay`'s `TestEveryRoleWordIsDeclaredWhereTheVocabularyIs`:
the six role constants in unchanged `internal/placeorg/judge.go` are declared
outside `internal/roles/roles.go`. Those files match the lane's starting commit
`5ff4278ae` and are outside `knn/` ownership. The placeorg contract laws pass.
The integration lane must resolve that vocabulary failure before the wave's
whole-tree law gate can pass.

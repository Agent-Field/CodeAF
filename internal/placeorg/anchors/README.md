# Derived anchors

`anchors.Derived{}` implements `placeorg.Anchorer` without a model, a disk
lookup, or a fixed subject dictionary. It reads the title and text supplied
by the caller and leaves those chats unchanged.

Keys are lowercase issue references (`#123`, `owner/repo#123`), a cleaned
`workspace:<family>`, an opaque trimmed `repo:<key>`, and lowercase word
tokens. Issue and pull URLs produce both the qualified reference and bare
number. Workspace paths must be absolute; only their basename survives,
decimal/long hexadecimal/UUID suffixes are stripped, and generic containers
and home directories supply no family. Repository keys preserve their case
because the caller supplies their identity.

Proper-token candidates have a capital letter or an internal hyphen. Once
identified anywhere in the corpus, they match lowercase occurrences too.
Their document frequency counts every spelling and must be at least three
and at most 5% of the corpus. Confirmed place names match whole tokens,
including internal hyphens, without that frequency gate. Repeated mentions
and a title repeated in text count only once. Each emitted anchor is weighted
by smoothed IDF: `1 + ln((n + 1) / (df + 1))`.

## Verification

```sh
make test-focus PKGS=./internal/placeorg/... RUN='^Test'
go vet ./internal/placeorg/...
go test ./internal/placeorg/anchors -run '^$' -bench BenchmarkTenThousandChats -benchtime=1x -benchmem
PLACEORG_REF=/path/to/owner-algo/ref make test-focus PKGS=./internal/placeorg/anchors RUN='^TestReferenceRecovery$'
```

The synthetic fixture is public and runs everywhere. Reference parity reads
the saved embedding row IDs, the unmodified harness's dictionary, and the
private corpus at runtime; none of those inputs are committed. Set
`PLACEORG_CORPUS` if the two source JSONL files are not two directories above
`PLACEORG_REF`. Missing reference configuration skips the parity test;
configured but missing or malformed reference inputs fail it.

## Acceptance gap in the supplied reference

On 2026-10-10, the supplied 389-row reference recovered **50 of 401 anchor
hits (12.47%)**, with no confirmed place names supplied. The 90% acceptance
test is deliberately red with this reference. Most reference subject hits
are too frequent for the required 5% cap; others are lowercase-only,
below three documents, or substring matches rather than tokens. Artificially
treating reference-generated names as person-confirmed names would change
the input contract, so this implementation does not do it.

The task therefore needs a decision on which requirement prevails: the
stated proper-token frequency range or the 90% reference recovery target.
The implementation retains the stated frequency range. Structural ARI is
not a metric for this stage; it is measured by the complete runner using
the saved embeddings.

The final 10,000-chat benchmark measured 59 ms and 16.8 MB of total allocations
in a single iteration on this lane's machine. `make build`, `go build ./...`,
`go vet ./internal/placeorg/...`, fresh `go test ./internal/placeorg/...`,
focused tests, TypeScript checking, and the desktop design check passed.
The package's contract and subject-dictionary laws passed. The full
`make test-laws` failed only on
`TestEveryRoleWordIsDeclaredWhereTheVocabularyIs`: six role declarations
in the frozen root `internal/placeorg/judge.go` violate the central role
vocabulary rule. The identical failure was reproduced on an unmodified
archive of base commit `5ff4278ae`; the anchors lane does not own that fix.

This stage is not yet wired
to a person-facing feature, so it adds no desktop design ledger row or
manual claim about a capability the app can use.

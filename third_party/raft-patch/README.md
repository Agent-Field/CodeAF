# Shared Raft dependency

Both native and WASI targets import the root module's local replacement
`third_party/raft`, an isolated `go.etcd.io/raft/v3` module at **v3.7.0**. Its
complete upstream source, tests, fixtures and Apache-2.0 license are retained.
Only `raft.go` is modified. `election-random.patch` is the complete upstream
diff, including the local-change comments; no election algorithm is rewritten.

The added optional `Config.ElectionRandom func(int) int` belongs to one node.
`ElectionRandomBound` optionally changes only that provider's bounded offset;
zero retains upstream's bound. `ElectionTick`, CheckQuorum, PreVote lease checks,
votes and transfer remain unchanged. codeaf uses bound eight and returns a
ranked width-two draw so anchors ask before a promoted personal voter. Rank
comes from the committed provisioned-anchor manifest. This timing extension
also modifies only `raft.go`; the complete recorded patch reverses to the same
independently hashed upstream bytes.
It is consulted only by `resetRandomizedElectionTimeout`. Nil preserves the
upstream behavior for upstream tests/users; invalid bounds panic. codeaf always
sets it to the core-owned SplitMix64 provider, so the upstream global RNG is
never reached by that path. No global RNG replacement or linker trick is used.
This patch is local and has not been submitted or accepted upstream.

Protobuf source is pinned to **google.golang.org/protobuf v1.36.11** through the
root and isolated module requirements and sums. It is the unmodified Go module,
not a second copy/fork; Go downloads exactly that checksummed source for either
target. `PROTOBUF-LICENSE` and `PROTOBUF-PATENTS` retain its notices. Do not
silently upgrade either source or apply a runtime-only replacement for wasm.
A future separately built ABI module must use these same requirements and local
Raft replacement; a replacement in a dependency's go.mod is not inherited.

`SOURCES.json` records upstream module sums, archive SHA-256 values, and hashes
of every upstream Raft file. `verify.py` checks that the checked-in tree is
exactly that source plus the reviewed patch, and checks the root pins/notices.
The source archive can be obtained by `go mod download -json
 go.etcd.io/raft/v3@v3.7.0` without using git or modifying the fork.

Replay compatibility comprises this patch, the raft/protobuf source versions,
the deterministic protobuf marshal mode, and the SplitMix64 algorithm and
consumption order in `internal/raftcore`. The fixed core trace and published
PRNG vectors intentionally fail if that behavior changes. P03 owns the actual
wasm runtime comparison; native replay alone does not prove it.

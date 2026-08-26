# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/aforge

.PHONY: all build debug demo-home embed test test-swepro test-remote vet check size clean

# What the shipped binary is allowed to weigh, in bytes, checked in beside the
# tree that produces it. See `size` below for why it is a file and not a number
# in this Makefile, and PERF.md for the policy around changing it.
BUDGET := SIZE-BUDGET

# The imported swe-pro engine (internal/swepro) arrived with fifteen tests
# already failing on macOS in a clean upstream checkout — /var-vs-/private/var,
# a case-insensitive filesystem, JS float-rounding parity — and fixing them was
# not on the way in. Until they are fixed they are not aforge's end-of-change
# ritual. The exclusion is only this narrow: `go build ./...` and
# `go vet ./...` still cover internal/swepro and both are green, and the
# embedding's own divergences are covered from outside the tree by
# cmd/aforge/swepro_test.go, which drives the real binary. This is a to-do,
# not a policy — see internal/swepro/EMBEDDING.md.
AFORGE_PKGS = $(shell go list ./... | grep -v '/internal/swepro/')

# The packed corpora — the two manuals, the baked agent roster, the engine's
# prompt assets. Each folder is the source of truth and the archive beside it is
# generated from it (internal/packed says why), so a build that skipped this
# could ship yesterday's manual. The packer is a pure function of the folder, so
# regenerating on every build rewrites identical bytes and leaves the tree
# clean — cheaper to trust than a freshness check, and the test beside each
# corpus asserts the same thing for `make test`.
PACKED_PKGS = ./internal/manual ./internal/swepro/internal/baked ./internal/swepro/internal/assets

all: build

embed:
	go generate $(PACKED_PKGS)

# The symbol table and DWARF are a third of the shipped binary and nothing at
# runtime reads them. Stripping costs symbolized panic traces, which is exactly
# what `debug` keeps — build that when a stack trace is what you need.
#
# -trimpath drops the build machine's absolute paths out of the binary and makes
# the build reproducible: the same tree gives the same bytes on any machine.
# What it costs is compiled-in repository roots — runtime.Caller in
# internal/swepro's furrow and weave lookups walks up from its own source file
# to find vendor/bin/<tool>. There is no vendor/ in this repository, and a
# shipped binary's compiled-in root never exists on the machine running it, so
# both lookups already fell through to the cwd copy and then to PATH.
build: embed
	go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/aforge

debug: embed
	go build -trimpath -o $(BINARY) ./cmd/aforge

test:
	go test $(AFORGE_PKGS)

# The engine's own suite. Run it when you change internal/swepro, and compare
# against `go test ./...` in a clean upstream checkout: at import, the two
# failure sets were equal, which is what proved the import changed nothing.
test-swepro:
	go test ./internal/swepro/...

# TWO MACHINES, ACTUALLY TWO. Three containers on one network — a scripted
# model, an engine with sshd, and a surface — sharing no path, no home and no
# credential, so a session that only works because both halves happen to be one
# filesystem fails here instead of passing by coincidence. It is deliberately
# NOT part of `make test`: it wants a docker daemon and about a minute, which
# is a nightly or pre-release gate rather than an every-change one.
#
# It SKIPS, green, wherever there is no docker — so it is safe in any pipeline
# on the day the pipeline cannot yet run it.
test-remote:
	go test -tags docker_e2e -count=1 -run TestRemoteTwoMachines -timeout 20m ./internal/e2e/

# ── the demo home ───────────────────────────────────────────────────────────
#
# A HOME WITH SOMETHING ON EVERY PLACE, FOR LOOKING AT. On a machine that has
# just started using aforge the standing store, the memory store and the
# spending ledger are empty, and every one of those pages correctly draws
# nothing — which is the emptiness law working and is also indistinguishable
# from a page that is broken. This builds a THROWAWAY home somewhere else and
# opens the real binary against it, so all of it can be seen full without a
# single invented row landing in ~/.aforge.
#
# It prints the directory it built and the command to open it again, so the same
# home can be returned to:
#
#   make demo-home                              a fresh one in a temp directory
#   make demo-home DEMO_HOME=/tmp/aforge-demo    build it somewhere you can name
#   make demo-home DEMO_HOME=/tmp/aforge-demo KEEP=1
#                                               open the one already there,
#                                               with whatever the last look left
#
# The seeder is its own binary and NOT a hidden verb on aforge, because the
# shipped binary is on a checked-in byte budget (SIZE-BUDGET) and a developer
# target must not spend the product's weight. bin/aforge-demo-home is not a
# second copy of the product and cannot shadow it — it is a different program
# with a different name.
DEMO_BINARY := bin/aforge-demo-home

demo-home: build
	go build -o $(DEMO_BINARY) ./cmd/aforge-demo-home
	@$(DEMO_BINARY) $(if $(DEMO_HOME),--into "$(DEMO_HOME)") $(if $(KEEP),--keep) --launch "$(CURDIR)/$(BINARY)"

vet:
	go vet ./...

# ── the size ratchet ────────────────────────────────────────────────────────
#
# THE BINARY HAS A BUDGET AND THE BUDGET IS CHECKED IN. A megabyte of embedded
# prose, a dependency pulled in for one function, a corpus that stopped being
# packed: every one of them lands as bytes nobody measured, and the only moment
# anybody would have noticed is the moment it was added. So the number is read
# back out of the tree on every `check` and compared, which turns "the binary got
# fat somewhere over the last year" into "this change added it".
#
# IT GATES `check` AND NOT `build`. A developer iterating rebuilds twenty times
# an hour and must never be stopped by a byte count; the end-of-change ritual is
# where a size is a decision rather than an interruption.
#
# THE BUDGET IS A FILE BECAUSE A CHANGE TO IT MUST BE A DIFF. Raising it is
# allowed and sometimes right — a feature is worth its bytes — but it is a
# reviewable line in the same commit as the thing that spent them, never a flag
# somebody passed once on their own machine. PERF.md carries the same rule for
# every other cap in this repository.
#
# The measurement is bytes and not seconds, which is the doctrine PERF.md states
# for all of these gates: a work-based number is the same number on a loaded
# laptop and on idle CI, so red always means somebody changed something.
size: build
	@budget=$$(cat $(BUDGET)); \
	actual=$$(stat -c%s $(BINARY) 2>/dev/null || stat -f%z $(BINARY)); \
	if [ "$$actual" -gt "$$budget" ]; then \
		printf '\n%s is %s bytes. The budget in %s is %s. Over by %s.\n\n' \
			'$(BINARY)' "$$actual" '$(BUDGET)' "$$budget" "$$((actual - budget))"; \
		printf 'Shrink what you added — pack a corpus (internal/packed), drop a\n'; \
		printf 'dependency, stop embedding what can be fetched — or raise the number\n'; \
		printf 'in %s IN THIS COMMIT, so the extra weight is a decision somebody\n' '$(BUDGET)'; \
		printf 'signed for rather than a drift nobody saw. PERF.md states the policy.\n\n'; \
		printf 'The budget was set on linux/arm64 with the toolchain of the day. A\n'; \
		printf 'different platform or a new Go release moves this number on its own;\n'; \
		printf 'that too is a reason to reset it deliberately, never to ignore red.\n\n'; \
		exit 1; \
	fi; \
	printf '%s: %s bytes, under the %s budget of %s.\n' '$(BINARY)' "$$actual" '$(BUDGET)' "$$budget"

# The end-of-change ritual in one word: prove it, then ship the binary, then
# weigh it.
check: vet test size

clean:
	rm -rf bin

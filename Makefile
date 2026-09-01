# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/aforge

.PHONY: all build debug demo-home embed furrow test test-packed-manual test-remote vet check size clean \
        changelog changelog-new changelog-check changelog-preview

# What the shipped binary is allowed to weigh, in bytes, checked in beside the
# tree that produces it. See `size` below for why it is a file and not a number
# in this Makefile, and PERF.md for the policy around changing it.
BUDGET := SIZE-BUDGET

# These three words are the build's identity everywhere the program reports
# one. The timestamp is UTC at the seam and becomes local time only when a
# person reads it, so the same binary remains unambiguous across machines.
BUILD_REV := $(shell git rev-parse --short HEAD)
BUILD_DIRTY := $(shell if test -n "$$(git status --porcelain --untracked-files=normal)"; then printf true; else printf false; fi)
BUILD_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDINFO := github.com/Agent-Field/aforge-v2/internal/buildinfo
BUILD_STAMP := -X $(BUILDINFO).rev=$(BUILD_REV) -X $(BUILDINFO).dirty=$(BUILD_DIRTY) -X $(BUILDINFO).builtAt=$(BUILD_AT)
MANUAL_TAG := aforge_packed_manual

# The packed corpora — the two manuals. Each folder is the source of truth and
# the archive beside it is generated from it (internal/packed says why), so a
# build that skipped this could ship yesterday's manual. The packer is a pure
# function of the folder, so regenerating on every build rewrites identical
# bytes and leaves the tree clean — cheaper to trust than a freshness check, and
# the test beside each corpus asserts the same thing for `make test`.
PACKED_PKGS = ./internal/manual

all: build

embed:
	go generate $(PACKED_PKGS)

# ── the furrow that rides inside ────────────────────────────────────────────
#
# EVERY AFORGE IS AN AFORGE WITH FURROW, so the build fetches furrow before it
# can produce one. This step downloads the release pinned in
# internal/furrowbin/pin.json for whatever platform is being built for, checks
# it against the sha256 the pin names, keeps it in a gitignored third_party/
# cache, and stages it gzipped where go:embed picks it up. Six megabytes of
# binary is not committed — every clone would carry it forever — but the pin
# beside it is, so what shipped is auditable from the repository alone.
#
# It is a prerequisite of `build` and not a thing anybody remembers to run. A
# fetch that cannot happen STOPS THE BUILD, loudly, with the command to run: the
# failure mode this ordering exists to make impossible is a quietly successful
# build that produced an aforge without furrow inside it.
#
# On a machine with no network, point it at an artifact already on disk — the
# sha256 is checked either way, so this is an offline road and not a looser one:
#
#   make furrow FURROW_ARTIFACT=~/.agentfield/bin/furrow
#
# THE TARGET IS READ FIRST AND THEN UNSET. A cross-compiling build sets GOOS and
# GOARCH for aforge, and the fetcher has to know them — but it must not be built
# FOR them, or the build machine tries to run a Linux tool and reports an exec
# format error where it meant to report a download. So the two are captured as
# the platform to fetch and taken out of the environment the tool is built in.
FURROW_GOOS := $(shell go env GOOS)
FURROW_GOARCH := $(shell go env GOARCH)

furrow:
	@env -u GOOS -u GOARCH go run ./internal/furrowbin/cmd/fetch \
		-goos=$(FURROW_GOOS) -goarch=$(FURROW_GOARCH) \
		$(if $(FURROW_ARTIFACT),-from "$(FURROW_ARTIFACT)")

# The symbol table and DWARF are a third of the shipped binary and nothing at
# runtime reads them. Stripping costs symbolized panic traces, which is exactly
# what `debug` keeps — build that when a stack trace is what you need.
#
# -trimpath drops the build machine's absolute paths out of the binary. The
# build stamp deliberately gives separate builds separate bytes, but neither
# carries the machine-specific repository root. What trimming costs is
# compiled-in repository roots: a lookup that walks up from its own source file
# with runtime.Caller can no longer find a tool checked in beside the tree. A
# shipped binary's compiled-in root never exists on the machine running it
# anyway, so any such lookup has to fall through to the copy beside the cwd and
# then to PATH — which is what it does here.
build: furrow embed
	go build -tags=$(MANUAL_TAG) -trimpath -ldflags="-s -w $(BUILD_STAMP)" -o $(BINARY) ./cmd/aforge

debug: furrow embed
	go build -tags=$(MANUAL_TAG) -trimpath -ldflags="$(BUILD_STAMP)" -o $(BINARY) ./cmd/aforge

test:
	go test ./...

# Exercise the source mode the shipped binary uses. Ordinary Go commands embed
# the Markdown directly so a clean checkout compiles without generated files;
# this target proves the generated, compressed path reads the same pages.
test-packed-manual: embed
	go test -tags=$(MANUAL_TAG) ./internal/manual


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
check: vet test test-packed-manual size

# ── the changelog ───────────────────────────────────────────────────────────
#
# ONE FILE PER PULL REQUEST, ROLLED UP WHEN A VERSION IS CUT. The entries live
# loose in docs/changes/unreleased because several sessions work this tree at
# once and a shared file that every branch appends to conflicts on every merge —
# and a step that reliably produces a conflict is a step people reliably route
# around.
#
# What the entries carry is not what shipped. It is what somebody now believes
# WRONGLY: the branch that stopped existing, the default that moved, the refusal
# that became a capability. docs/rules/changelog.md says why that is the field
# the format is built around and why it cannot be generated.
CHANGES := ./cmd/aforge-changes

changelog-new:
	@test -n "$(PR)"   || { echo 'usage: make changelog-new PR=82 KIND=changed SLUG=branch-rules'; exit 1; }
	@test -n "$(KIND)" || { echo 'usage: make changelog-new PR=82 KIND=changed SLUG=branch-rules'; exit 1; }
	@test -n "$(SLUG)" || { echo 'usage: make changelog-new PR=82 KIND=changed SLUG=branch-rules'; exit 1; }
	@go run $(CHANGES) new $(KIND) $(PR) $(SLUG)

changelog-check:
	@go run $(CHANGES) check

changelog-preview:
	@test -n "$(VERSION)" || { echo 'usage: make changelog-preview VERSION=v0.2.0'; exit 1; }
	@go run $(CHANGES) render $(VERSION)

# Run this on a branch and land it through a pull request into `dev` BEFORE the
# promotion — never as a commit on `staging`, which would break the fast-forward
# the whole branch model rests on. docs/rules/promotion.md has the order.
changelog:
	@test -n "$(VERSION)" || { echo 'usage: make changelog VERSION=v0.2.0'; exit 1; }
	@go run $(CHANGES) roll $(VERSION)

clean:
	rm -rf bin

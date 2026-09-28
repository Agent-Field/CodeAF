# The one binary. Every build lands here — never at the repo root, never
# anywhere else — so a stale copy can't shadow a fresh one.
BINARY := bin/codeaf

.PHONY: all build build-check build-cross debug demo-home clean-run embed manual-pack-law furrow test test-focus test-report test-quick test-tooling test-touched test-touched-preflight pr-ready test-laws fmt-check test-packed-manual manual-gates test-remote test-e2e test-e2e-tui vet check size clean \
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
BUILDINFO := github.com/Agent-Field/codeaf/internal/buildinfo
BUILD_STAMP := -X $(BUILDINFO).rev=$(BUILD_REV) -X $(BUILDINFO).dirty=$(BUILD_DIRTY) -X $(BUILDINFO).builtAt=$(BUILD_AT)
MANUAL_TAG := codeaf_packed_manual

# The packed corpora — the two manuals. Each folder is the source of truth and
# the archive beside it is an IGNORED build product (internal/packed says why),
# so a build that skipped generation could ship yesterday's manual. The law
# below checks the index as well as .gitignore: an ignored file can still be
# force-added, which turns every otherwise-independent manual edit into a binary
# merge conflict.
PACKED_PKGS = ./internal/manual

all: build

manual-pack-law:
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		tracked=$$(git ls-files -- 'internal/manual/*.pack.gz'); \
		if test -n "$$tracked"; then \
			printf '%s\n' 'generated manual archives must not be tracked:' "$$tracked"; \
			printf '%s\n' 'remove them from the index; make build regenerates ignored copies'; \
			exit 1; \
		fi; \
	fi

embed: manual-pack-law
	# Generators execute on the build host, even when codeaf targets another OS.
	env -u GOOS -u GOARCH go generate $(PACKED_PKGS)

# ── the furrow that rides inside ────────────────────────────────────────────
#
# EVERY codeaf IS A codeaf WITH FURROW, so the build fetches furrow before it
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
# build that produced a codeaf without furrow inside it.
#
# On a machine with no network, point it at an artifact already on disk — the
# sha256 is checked either way, so this is an offline road and not a looser one:
#
#   make furrow FURROW_ARTIFACT=~/.agentfield/bin/furrow
#
# THE TARGET IS READ FIRST AND THEN UNSET. A cross-compiling build sets GOOS and
# GOARCH for codeaf, and the fetcher has to know them — but it must not be built
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
	go build -tags=$(MANUAL_TAG) -trimpath -ldflags="-s -w $(BUILD_STAMP)" -o $(BINARY) ./cmd/codeaf
	go build -trimpath -ldflags="-s -w $(BUILD_STAMP)" -o bin/plandb ./cmd/plandb

debug: furrow embed
	go build -tags=$(MANUAL_TAG) -trimpath -ldflags="$(BUILD_STAMP)" -o $(BINARY) ./cmd/codeaf

# ── the known-red ledger, read once ─────────────────────────────────────────
#
# .github/known-red.txt WAS the debt: tests that failed on a clean tree, skipped
# by name so that red still meant something. It burned to zero on 2026-09-12
# (#1012) and the file is gone; this read remains so that a ledger could not
# quietly return — IT IS READ HERE AND ONLY HERE, and an absent file skips
# nothing. `make test`, `make test-laws` and both workflows go through this one
# reading, so "green locally" and "green in CI" are one fact. Before 2026-09-02
# they were not: this target was a bare `go test ./...` that could not pass on a
# clean tree while the full run skipped the ledger, so `make check` — the ritual
# CLAUDE.md sends everybody to — stopped at its second step for everyone, every
# time (#372).
KNOWN_RED := $(shell grep -v -e '^\#' -e '^[[:space:]]*$$' .github/known-red.txt 2>/dev/null | paste -sd'|' -)
TEST_SKIP := $(if $(KNOWN_RED),-skip '^($(KNOWN_RED))$$')

# THE PER-SHARD TIMEOUT IS MEASURED, NOT GUESSED. On 2026-09-22 internal/tui3
# took 65 seconds as eight shards on the eight-core WSL2 box, down from 341
# seconds serial; the constrained runner's earlier serial measurement was 563
# seconds. Fifteen minutes remains per shard because a busy runner or a changed
# shard can still need the old headroom, and a real hang must name itself.
# Lower it only from a new uncached measurement, never from an expected
# optimization.
TEST_TIMEOUT := 15m
TEST_FLAGS ?=
PKGS ?= ./...
SHARDS ?= $(shell count=$$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 1); if ! test "$$count" -ge 1 2>/dev/null; then count=1; elif test "$$count" -gt 8; then count=8; fi; echo "$$count")

# A full run of the tree or either heavy package takes the box's one suite lock
# (scripts/one-suite.sh says why): each heavy package holds it for its whole
# sharded run, and a `./...` run holds it for the rest of the tree as well, so
# two sessions' tree runs still cannot stack. Focused and lighter runs stay
# independent.
HEAVY_PKGS := internal/tui3 internal/session
LOCKED_PKGS := ./... $(addprefix ./,$(HEAVY_PKGS)) $(addsuffix /,$(addprefix ./,$(HEAVY_PKGS)))
FRESH_FLAG := $(if $(filter $(LOCKED_PKGS),$(PKGS)),-count=1)
SUITE_LOCK := ./scripts/one-suite.sh
TREE_LOCK := $(if $(filter ./...,$(PKGS)),$(SUITE_LOCK))

test:
	@set -u; \
	module="$$(go list -m)" || exit 1; \
	packages="$$(go list $(PKGS))" || exit 1; \
	is_heavy() { \
		for heavy in $(HEAVY_PKGS); do \
			if test "$$1" = "$$module/$$heavy"; then return 0; fi; \
		done; \
		return 1; \
	}; \
	has_heavy=; \
	for package in $$packages; do \
		if is_heavy "$$package"; then has_heavy=yes; fi; \
	done; \
	if test -z "$$has_heavy"; then \
		exec go test -timeout $(TEST_TIMEOUT) $(TEST_FLAGS) $(TEST_SKIP) $(PKGS); \
	fi; \
	nonheavy=; \
	for package in $$packages; do \
		if ! is_heavy "$$package"; then nonheavy="$$nonheavy $$package"; fi; \
	done; \
	status=0; \
	if test -n "$$nonheavy"; then \
		$(TREE_LOCK) go test -timeout $(TEST_TIMEOUT) $(FRESH_FLAG) $(TEST_FLAGS) $(TEST_SKIP) $$nonheavy || status=1; \
	fi; \
	for package in $$packages; do \
		if is_heavy "$$package"; then \
			SHARDS='$(SHARDS)' $(SUITE_LOCK) ./scripts/shard-test.sh -timeout $(TEST_TIMEOUT) $(TEST_FLAGS) $(TEST_SKIP) "$$package" || status=1; \
		fi; \
	done; \
	exit "$$status"

# One named regression is the fastest trustworthy edit loop. RUN is required:
# an omitted selector must not silently turn a focused command into a full
# package run. The ordinary test target still owns the ledger and timeout.
test-focus:
	@test -n "$(RUN)" || { echo "usage: make test-focus PKGS=./internal/pkg RUN='^TestName$$'"; exit 2; }
	go test -timeout $(TEST_TIMEOUT) $(TEST_FLAGS) -run '$(RUN)' $(TEST_SKIP) $(PKGS)

# Keep the Go build cache warm while forcing the tests themselves to execute.
# REPORT is structured JSON; progress and the slowest completed tests remain
# visible on stderr during a long package run.
REPORT ?= test-report.json
test-report:
	./scripts/test-report.sh '$(REPORT)' $(MAKE) -s --no-print-directory test PKGS='$(PKGS)' TEST_FLAGS="$(TEST_FLAGS) -count=1 -json"

# This runs the deterministic light pull-request gates: build, vet, formatting,
# the packed corpus, well-formed change entries, the manual gates, and the laws.
# Off a pull request the changelog check can prove only that entries are well
# formed; whether this branch adds one needs the base commit that only CI has.
# It is fast feedback, NOT full acceptance: it does not run touched packages or
# whole suite. Use pr-ready before claiming pull-request acceptance.
build-check:
	go build ./...

test-quick: build-check vet fmt-check test-packed-manual changelog-check manual-gates test-laws

# The shell runners are executable infrastructure that Go's package walk cannot
# discover, so their acceptance scripts are named here. They are touched-only,
# like the Go packages: `pr-ready` runs them when the change touches scripts/ or
# this Makefile, because together they take about forty seconds and most pull
# requests never go near them.
test-tooling:
	bash scripts/one-suite_test.sh
	bash scripts/shard-test_test.sh

manual-gates:
	go test ./internal/manual/
	go test -run 'Manual' ./internal/tui3/ ./internal/session/

# THE TOUCHED SET IS THE PULL-REQUEST JOB'S SET. A module-file change reaches
# every package; otherwise each changed Go file contributes its directory, and
# a directory emptied by the change contributes nothing. BASE may name the
# pull request's exact base SHA; on a laptop it defaults to origin/dev. This
# refuses uncommitted Go or module files because the real gate sees BASE..HEAD:
# silently ignoring those files under-tests, while folding a shared checkout's
# unrelated edits into this run over-tests. Commit the candidate, then prove
# exactly what the pull request will send. The preflight is separate so
# `pr-ready` refuses before spending anything on its light checks. Tests still
# go through `make test`, the one door for the timeout, known-red ledger and
# full heavy-package lock.
test-touched-preflight:
	@set -eu; \
	base="$${BASE:-origin/dev}"; \
	if ! git rev-parse --verify "$$base^{commit}" >/dev/null 2>&1; then \
		printf '%s\n' "cannot resolve BASE '$$base'; fetch origin/dev or pass BASE=<commit>" >&2; \
		exit 2; \
	fi; \
	uncommitted="$$( \
		{ git diff --name-only HEAD -- '*.go' go.mod go.sum; \
		  git ls-files --others --exclude-standard -- '*.go' go.mod go.sum; } \
		| sort -u \
	)"; \
	if test -n "$$uncommitted"; then \
		printf '%s\n' 'test-touched cannot mirror the PR while Go or module files are uncommitted:' "$$uncommitted" \
			'Commit or remove them, then run this target again.' >&2; \
		exit 2; \
	fi

# A directory under its own go.mod is another module — a bench fixture the
# task door edits, not a package of this one — and `go test ./that/dir` from
# here answers "does not contain package". The walk skips those.
test-touched: test-touched-preflight
	@set -eu; \
	base="$${BASE:-origin/dev}"; \
	changed="$$(git diff --name-only "$$base" HEAD -- '*.go' go.mod go.sum | sort -u)"; \
	if printf '%s\n' "$$changed" | grep -qxE 'go\.(mod|sum)'; then \
		pkgs="./..."; \
	else \
		dirs="$$(printf '%s\n' "$$changed" | while IFS= read -r file; do \
			if test "$${file%.go}" != "$$file"; then dirname "$$file"; fi; \
		done | sort -u)"; \
		pkgs=""; \
		for dir in $$dirs; do \
			nested=0; walk="$$dir"; \
			while test "$$walk" != "." && test "$$walk" != "/"; do \
				if test -f "$$walk/go.mod"; then nested=1; break; fi; \
				walk="$$(dirname "$$walk")"; \
			done; \
			if test "$$nested" = 1; then continue; fi; \
			if ls "$$dir"/*.go >/dev/null 2>&1; then pkgs="$$pkgs ./$$dir"; fi; \
		done; \
	fi; \
	if test -z "$$pkgs"; then \
		echo 'No Go file and no module file changed; nothing to run.'; \
		exit 0; \
	fi; \
	echo "touched:$$pkgs"; \
	$(MAKE) --no-print-directory test TEST_FLAGS='$(TEST_FLAGS) -count=1 -p 1' PKGS="$$pkgs"

# One local spelling for the two jobs behind the pull request's required
# `check`: first the deterministic light gate, then the exact touched-package
# proof above. This deliberately omits the full tree, release binary, size
# ratchet, cross builds and remote containers; those remain staging/nightly
# work, with `make check` as the full-tree laptop/Spark spelling.
pr-ready: test-touched-preflight
	$(MAKE) --no-print-directory test-quick
	@if ! git diff --quiet "$${BASE:-origin/dev}" HEAD -- scripts Makefile; then \
		$(MAKE) --no-print-directory test-tooling; \
	fi
	$(MAKE) --no-print-directory test-touched

# The laws alone — every test that reads the tree itself — in under half a
# minute. This is what the pull-request gate runs on every change, and
# scripts/laws.sh says how they are found without anybody keeping a list.
test-laws:
	./scripts/laws.sh

# gofmt is not a preference here. A file gofmt would rewrite is a file the next
# editor's save rewrites, and that diff lands in somebody else's pull request.
fmt-check:
	@dirs="$$(go list -f '{{.Dir}}' ./...)" || exit 1; \
	test -n "$$dirs" || { echo 'go list found no packages'; exit 1; }; \
	files="$$(printf '%s\n' "$$dirs" | tr '\n' '\0' | xargs -0 gofmt -l)" || exit 1; \
	if test -n "$$files"; then \
		printf '%s\n' 'gofmt would rewrite:' "$$files"; \
		exit 1; \
	fi

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

# THE AMBIENT SURFACE, ALONE. TestTUIE2E fits in about seventeen minutes; the
# full tagged package does not fit in forty (ManualOnTheWire, QuestionsE2E and
# the roomfeed twins run first and eat the budget). This is the door for the
# seventeen-minute ambient proof. Needs OPENROUTER_API_KEY, tmux and bin/codeaf.
test-e2e-tui: build
	go test -tags e2e -count=1 -timeout 40m -v -run '^TestTUIE2E$$' ./internal/e2e/

# THE WHOLE TAGGED PACKAGE. Two hours is the measured fit on Spark once every
# live-model lane is included; prefer test-e2e-tui when only the ambient surface
# is under change.
test-e2e: build
	go test -tags e2e -count=1 -timeout 120m -v ./internal/e2e/

# ── the demo home ───────────────────────────────────────────────────────────
#
# A HOME WITH SOMETHING ON EVERY PLACE, FOR LOOKING AT. On a machine that has
# just started using codeaf the standing store, the memory store and the
# spending ledger are empty, and every one of those pages correctly draws
# nothing — which is the emptiness law working and is also indistinguishable
# from a page that is broken. This builds a THROWAWAY home somewhere else and
# opens the real binary against it, so all of it can be seen full without a
# single invented row landing in ~/.codeaf.
#
# It prints the directory it built and the command to open it again, so the same
# home can be returned to:
#
#   make demo-home                              a fresh one in a temp directory
#   make demo-home DEMO_HOME=/tmp/codeaf-demo    build it somewhere you can name
#   make demo-home DEMO_HOME=/tmp/codeaf-demo KEEP=1
#                                               open the one already there,
#                                               with whatever the last look left
#
# The seeder is its own binary and NOT a hidden verb on codeaf, because the
# shipped binary is on a checked-in byte budget (SIZE-BUDGET) and a developer
# target must not spend the product's weight. bin/codeaf-demo-home is not a
# second copy of the product and cannot shadow it — it is a different program
# with a different name.
DEMO_BINARY := bin/codeaf-demo-home

demo-home: build
	go build -o $(DEMO_BINARY) ./cmd/codeaf-demo-home
	@$(DEMO_BINARY) $(if $(DEMO_HOME),--into "$(DEMO_HOME)") $(if $(KEEP),--keep) --launch "$(CURDIR)/$(BINARY)"

# clean-run opens bin/codeaf on a fresh state root holding only your settings
# and keys (scripts/clean-run.sh), so a new build is tried from the same clean
# start every time: no conversations, projects or tasks from ~/.codeaf.
clean-run: build
	@scripts/clean-run.sh

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
# Read the release workflow so this gate follows the shipped platform list.
# The six builds share the normal GOCACHE, so a warm box pays far less than a
# cold one; the printed duration is the whole step, cold or warm.
build-cross:
	@set -eu; started=$$(date +%s%N); \
	targets="$$(awk '/^[[:space:]]*targets=\($$/ { in_targets=1; next } in_targets && /^[[:space:]]*\)/ { exit } in_targets { gsub(/"/, ""); if (NF == 2) print $$1 "/" $$2 }' .github/workflows/release.yml)"; \
	test -n "$$targets"; \
	for target in $$targets; do \
		goos=$${target%/*}; goarch=$${target#*/}; \
		printf 'build-cross: %s/%s\n' "$$goos" "$$goarch"; \
		if ! CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" go build ./...; then \
			printf 'build-cross: FAILED building %s/%s\n' "$$goos" "$$goarch" >&2; exit 1; \
		fi; \
	done; \
	finished=$$(date +%s%N); \
	printf 'build-cross: total duration %ss (rounded up, an upper bound on the added time)\n' "$$(( (finished - started + 999999999) / 1000000000))"

check: vet fmt-check test test-packed-manual size build-cross

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
CHANGES := ./cmd/codeaf-changes

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

# ── THE CALL CENSUS ─────────────────────────────────────────────────────────
#
# `make census` reads the model-call log this build always writes and prints
# docs/design/recovery/DESIGN.md §1 as markdown. It is the instrument that
# design's §8 asks for: five waves of recovery work each move a number in that
# table, and without a committed measurement every one of them is an argument
# about anecdotes.
#
#   make census                          this machine's own log
#   make census LOG=/path/to/calls.jsonl a log synced from somewhere else
#   make census OUT=/tmp/census.md       write it to a file instead of stdout
#   make census TOP=40 DAYS=7            widen the signature list and the window
#
# NIGHTLY IT IS THE SAME COMMAND. The Spark's cron syncs the laptop's log and
# runs `make census LOG=… OUT=…`; bench/README.md has the recipe and the one
# thing the cron owns that this target does not.
census:
	@go run ./cmd/codeaf-census \
	  $(if $(TOP),-top $(TOP)) $(if $(DAYS),-days $(DAYS)) \
	  $(if $(MIN),-min $(MIN)) $(if $(CHAINS),-chains $(CHAINS)) \
	  $(LOG) $(if $(OUT),> $(OUT))
	@$(if $(OUT),echo "the census is in $(OUT)")

# ── THE REPLAY ──────────────────────────────────────────────────────────────
#
# `make replay` judges a chooser the only way a chooser can honestly be judged:
# by the regret it would have paid over ten days of the model-call log this build
# already writes. Every change to the machine chooser in the week to 2026-09-11
# was argued from a screenshot and a ten-row grep; this is the instrument that
# ends that, and docs/design/recovery/DESIGN.md §8 is where a change is required
# to cite it.
#
#   make replay                             this machine's own log and journal
#   make replay LOG=/path/calls.jsonl SIGHTINGS=/path/lanes.log
#   make replay SINCE=2026-09-11T14:        one afternoon, for an incident
#   make replay OUT=/tmp/replay.md          write it to a file instead of stdout
#   make replay WINDOW=30m INCIDENTS=10     widen the estimator, show more moments
#
# It reads only; it never writes a belief file and never opens a connection.
replay:
	@go run ./cmd/codeaf-replay \
	  $(if $(LOG),-log $(LOG)) $(if $(SIGHTINGS),-sightings $(SIGHTINGS)) \
	  $(if $(SINCE),-since $(SINCE)) $(if $(WINDOW),-window $(WINDOW)) \
	  $(if $(MIN),-min $(MIN)) $(if $(INCIDENTS),-incidents $(INCIDENTS)) \
	  $(if $(OUT),-out $(OUT))

clean:
	rm -rf bin

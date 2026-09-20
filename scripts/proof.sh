#!/usr/bin/env bash
# PROOF STEPS THAT CAN FAIL. Source this; it defines functions and runs nothing.
#
# A proof chain is a sequence of lines somebody reads instead of watching the
# work. Every line in one has to answer a single question:
#
#   WHAT WOULD THIS PRINT IF THE THING IT CHECKS WERE BROKEN?
#
# If the answer is "the same thing it prints now", it is not a check. It is a
# line that spends the attention a real check would have earned, and it is worse
# than having no line at all, because a step nobody wrote is a step nobody
# trusts.
#
# Three shapes of that defect were found in one night's proof chains, each by a
# different person, and the three functions here are the three fixes. Each is
# small enough to inline, and each was inlined wrongly at least once, which is
# the argument for having them written down in one place with their failing arm
# tested. `internal/ci/prooflib_test.go` runs both arms of all three: the one
# where the thing is sound, and the one where it is broken. THE SECOND ARM IS
# THE ACCEPTANCE. A guard shipped without it joins the family it was written to
# prevent.
#
# THESE FUNCTIONS MAY HAVE NO CALLER IN THIS TREE, AND THAT IS NOT A REASON TO
# DELETE THE FILE. A survey for these three shapes across `Makefile`, `scripts/`
# and `.github/` found none of them, `fmt-check` having been written in the
# correct form already; every instance was in an ad-hoc chain script written
# outside the repository. That count covers those three shapes in those three
# places and claims nothing about any other way a failure can be discarded.
#
# So the functions are a convenience for whoever writes the next chain, and the
# file's standing value is its TEST: three assertions about how the tooling
# behaves that our proof chains depend on, which run on every pull request
# whether or not one line here is ever sourced. Delete the file and those three
# facts stop being checked by anything.
#
#   source "$(git rev-parse --show-toplevel)/scripts/proof.sh"
#   fmt_check ./cmd ./internal ./bench
#   step build go build ./...
#   run_tests ./internal/session/ TestOne TestTwo

# fmt_check prints what gofmt found and fails on a non-empty list.
#
# THE LIST IS THE SIGNAL AND THE EXIT CODE IS NOT. `gofmt -l` prints the files it
# would reformat and exits 0 whether it printed any or none, so a step written as
# `gofmt -l ./internal; echo "exit=$?"` reports green on every tree that has ever
# existed. That step ran for days over a const block nobody had formatted.
fmt_check() {
	local found
	found=$(gofmt -l "$@" 2>&1)
	if [ -n "$found" ]; then
		printf '%s\n' "$found"
		echo "gofmt=DIRTY"
		return 1
	fi
	echo "gofmt=clean"
	return 0
}

# step runs one command and reports THAT command's verdict.
#
# A PIPE REPORTS THE LAST COMMAND'S STATUS, WHICH IS USUALLY THE READER'S. The
# shape that hides is `cmd 2>&1 | tail -25; echo "exit=$?"`: it hands back tail's
# status, and tail succeeds at reading a failure. That line printed `exit=0`
# underneath a screen of FAIL. It does not merely hide a red; it manufactures a
# green directly below the evidence, and in a long chain the summary line is what
# gets read.
#
# So the status is captured before anything else can run, the output goes to a
# file rather than through a pipe, and the failures are counted from that file.
# THE COUNT IS A SECOND WITNESS SOURCED DIFFERENTLY FROM THE STATUS, which is
# what catches the case where the status itself is wrong.
step() {
	local name="$1"
	shift
	local log="${PROOF_LOG_DIR:-${TMPDIR:-/tmp}}/proof-${name//[^a-zA-Z0-9]/_}.log"
	echo "== $name =="
	"$@" >"$log" 2>&1
	local code=$?
	echo "${name}_exit=$code"
	echo "${name}_FAIL_lines=$(grep -c '^FAIL\|^--- FAIL' "$log")"
	grep -n '^--- FAIL\|^FAIL\|^ok ' "$log" | head -20
	return $code
}

# run_tests runs named tests and checks that every name it asked for RAN.
#
# A NAMED TEST THAT DID NOT RUN IS INDISTINGUISHABLE FROM ONE THAT PASSED, in
# every artifact anybody downstream reads. A `-run` pattern is a second copy of
# the list of tests in a file, and the two drift the moment somebody adds a test;
# `go test` answers a filter that matches nothing with `ok` and exit 0, because
# from its side nothing failed. There is no FAIL to count here, so the second
# witness has to be the enumeration itself.
#
# The failure names the count on both sides AND every name that reported nothing,
# in the same breath. A GUARD WHOSE SATISFACTION CAN ONLY BE SHOWN AS A TICK
# EVENTUALLY BECOMES A TICK, and the names are what make this one actionable at
# the moment it fires rather than after somebody goes looking.
run_tests() {
	local pkg="$1"
	shift
	local want=$#
	local pattern
	pattern=$(printf '%s|' "$@")
	pattern="^(${pattern%|})$"
	local log="${PROOF_LOG_DIR:-${TMPDIR:-/tmp}}/proof-tests.log"
	echo "== $want named tests in $pkg =="
	go test -count=1 -timeout "${PROOF_TEST_TIMEOUT:-10m}" -v -run "$pattern" "$pkg" >"$log" 2>&1
	local code=$?
	# Only the top-level result lines are counted. A subtest's line is indented,
	# so a table test with six subtests is one name and stays one name.
	local reported
	reported=$(grep -c '^--- PASS\|^--- FAIL\|^--- SKIP' "$log")
	echo "tests_exit=$code  asked=$want  reported=$reported"
	grep '^--- FAIL\|^--- SKIP' "$log" | head -20
	if [ "$reported" -ne "$want" ]; then
		echo "MISMATCH: the names asked for and the tests that ran have drifted."
		echo "These names reported nothing, so nothing about them was proved:"
		local name
		for name in "$@"; do
			grep -q -- "--- \(PASS\|FAIL\|SKIP\): $name" "$log" || echo "  $name"
		done
		return 1
	fi
	return $code
}

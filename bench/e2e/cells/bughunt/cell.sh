#!/usr/bin/env bash
# bughunt — a failing test whose cause is three packages away.
#
# The promoprice fixture prices a SKU by customer tier and memoises the result
# under a cache key that does not include the tier. The symptom appears in
# server/handler_test.go: quote gold, then silver, on one server, and the second
# quote comes back at the gold price. The cause is one line in cache/cache.go.
# Between them sit pricing/price.go, pricing/tier.go, and the memo itself.
#
# What this guards is the search, not the edit. The package-level suite in
# pricing/price_test.go is green on the broken code — every case there builds a
# fresh pricer, so no case ever crosses tiers — so a run that reads only the
# failing package and only the tests that fail finds a test that "looks wrong"
# and is one refactor away from deleting it. The assertions below are built to
# make that outcome a loud failure rather than a green row:
#
#   - the suite is run by this script afterwards, not trusted from the log
#   - both test files must be byte-identical to what the fixture wrote
#   - something on the key path must have changed
#
# Together those three say: the tests pass, and they pass because the code was
# fixed rather than because the test was.

CELL_GUARDS="distant-cause bug found and fixed on the key path, tests untouched"
CELL_BUDGET=1800

CELL_TASK='This Go module has a failing test suite. Run `go test ./...` to see it. Find the root cause and fix it so the whole suite passes. The cause is not in the test files — do not modify, weaken, or delete any test. Do not change what the tests assert. Fix the source.'

cell_fixture() {
  local dir="$1"
  "$E2E_ROOT/fixtures/promoprice.sh" "$dir" bug >/dev/null

  # The two test files are the thing that must not move. Their hashes are taken
  # before the run and checked after it.
  shasum -a 256 "$dir/pricing/price_test.go" | cut -d' ' -f1 > "$dir/.e2e-hash-price-test"
  shasum -a 256 "$dir/server/handler_test.go" | cut -d' ' -f1 > "$dir/.e2e-hash-handler-test"

  # A copy of the key path as it started, so "did the fix land where the cause
  # was" can be answered by comparison rather than by grepping for a keyword.
  cp "$dir/cache/cache.go" "$dir/.e2e-orig-cache.go"
  cp "$dir/pricing/price.go" "$dir/.e2e-orig-price.go"
}

cell_check() {
  local dir="$1" stdout="$2" stderr="$3" code="$4"

  check_eq "run exited cleanly" 0 "$code"

  # The suite is the judge, and this script runs it — not the harness.
  go_suite_green "go test ./... is green" "$dir" "$dir/.e2e-gotest.log"

  # Neither test file may have moved. This is the "did not weaken the tests"
  # check, and it is a hash rather than a diff because any change at all is a
  # failure here.
  check_unchanged "pricing/price_test.go untouched" \
    "$dir/pricing/price_test.go" "$(cat "$dir/.e2e-hash-price-test" 2>/dev/null)"
  check_unchanged "server/handler_test.go untouched" \
    "$dir/server/handler_test.go" "$(cat "$dir/.e2e-hash-handler-test" 2>/dev/null)"

  # The fix has to be on the key path. Either the key function itself changed or
  # its one caller did; anything else that makes the suite green is a fix
  # somewhere the cause is not, and worth seeing.
  local cache_changed=0 price_changed=0
  cmp -s "$dir/cache/cache.go" "$dir/.e2e-orig-cache.go" || cache_changed=1
  cmp -s "$dir/pricing/price.go" "$dir/.e2e-orig-price.go" || price_changed=1
  if [ "$cache_changed" = "1" ] || [ "$price_changed" = "1" ]; then
    pass "fix landed on the cache key path (cache.go=$cache_changed price.go=$price_changed)"
  else
    fail "fix did not touch cache/cache.go or pricing/price.go — the cause is there"
  fi
  record "changed_cache_go" "$cache_changed"
  record "changed_price_go" "$price_changed"

  # Behavioural confirmation, independent of the suite: the CLI must price the
  # same SKU differently at two tiers.
  local gold silver
  gold="$( (cd "$dir" && go run ./cmd/promoprice -sku=widget-1 -tier=gold) 2>/dev/null )"
  silver="$( (cd "$dir" && go run ./cmd/promoprice -sku=widget-1 -tier=silver) 2>/dev/null )"
  record "cli_gold" "${gold:-none}"
  record "cli_silver" "${silver:-none}"
  check "CLI prices gold and silver differently" \
    "$([ -n "$gold" ] && [ "$gold" != "$silver" ] && echo 1 || echo 0)"

  record "files_changed" "$(cd "$dir" && ls -1 2>/dev/null | wc -l | tr -d ' ')"
}

cell_shape() {
  local db="$1"
  check_eq "no failed nodes" 0 "$(failed_count "$db")"
  record "route" "$(scale_route "$db")"
  record "nodes" "$(node_count "$db")"
  record "growth_rounds" "$(growth_rounds "$db")"
  record "concurrent_starts_2s" "$(concurrent_starts "$db" 2)"
  record "delivery_gate" "$(delivery_pass "$db")"
}

CELL_WALL_CEILING=1800

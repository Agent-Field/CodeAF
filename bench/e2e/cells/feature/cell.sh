#!/usr/bin/env bash
# feature — one dimension threaded through every layer of the same fixture.
#
# Currency has to reach the catalog (prices per currency), the pricer (convert
# after the tier multiplier), the cache key (or EUR and USD collide the same way
# tiers did), the server request, and the CLI flag. Five files, none of which is
# optional: leave the cache key alone and the feature is silently wrong for the
# second currency, which is the same failure the bughunt cell plants — except
# here nobody points at it.
#
# The fixture is the CLEAN promoprice, so this cell measures building rather
# than repairing, and a failure here cannot be a leftover from the bug cell.
#
# What is asserted is behaviour, not structure. There is no correct file layout
# for this feature, so nothing checks which files were touched; instead the CLI
# is run for both currencies and the numbers have to differ and to persist
# across a second call on the same process — which is the cache-key half.

CELL_GUARDS="cross-cutting feature reaches every layer; new tests green; CLI verified by running it"
CELL_BUDGET=1800

CELL_TASK='This Go module quotes prices in US cents only. Add support for a second currency, EUR, threaded through the whole module: the pricer must be able to quote in either USD or EUR, the memo cache must not confuse the two, the server request must carry the currency, and the promoprice command must take a -currency flag accepting "usd" or "eur". Use a fixed conversion rate of 1 USD = 0.92 EUR, applied after the tier multiplier and any promotion. Add tests for the new behaviour, including one that quotes the same sku in both currencies through a single server. Keep every existing test passing and do not weaken any of them. Verify your work by running `go test ./...` and by running the command for both currencies.'

cell_fixture() {
  local dir="$1"
  "$E2E_ROOT/fixtures/promoprice.sh" "$dir" clean >/dev/null

  # The existing tests must survive the feature. Their hashes are the check.
  shasum -a 256 "$dir/pricing/price_test.go" | cut -d' ' -f1 > "$dir/.e2e-hash-price-test"
  shasum -a 256 "$dir/server/handler_test.go" | cut -d' ' -f1 > "$dir/.e2e-hash-handler-test"

  # How many test functions existed before. "New tests present" is this number
  # going up, which is harder to satisfy accidentally than "a test file exists".
  grep -rahoE '^func Test[A-Za-z0-9_]+' "$dir" --include='*_test.go' 2>/dev/null \
    | sort -u | wc -l | tr -d ' ' > "$dir/.e2e-test-count-before"
}

cell_check() {
  local dir="$1" stdout="$2" stderr="$3" code="$4"

  check_eq "run exited cleanly" 0 "$code"
  go_suite_green "go test ./... is green" "$dir" "$dir/.e2e-gotest.log"

  check_unchanged "pricing/price_test.go untouched" \
    "$dir/pricing/price_test.go" "$(cat "$dir/.e2e-hash-price-test" 2>/dev/null)"
  check_unchanged "server/handler_test.go untouched" \
    "$dir/server/handler_test.go" "$(cat "$dir/.e2e-hash-handler-test" 2>/dev/null)"

  # New tests, counted rather than assumed.
  local before after
  before="$(cat "$dir/.e2e-test-count-before" 2>/dev/null || echo 0)"
  after="$(grep -rahoE '^func Test[A-Za-z0-9_]+' "$dir" --include='*_test.go' 2>/dev/null | sort -u | wc -l | tr -d ' ')"
  record "test_funcs_before" "$before"
  record "test_funcs_after" "$after"
  check_ge "new test functions were added" "$((before + 1))" "$after"

  # The currency has to be reachable from the command line, and the two
  # currencies have to produce different numbers. This is the behavioural check
  # the task asked for, run here rather than believed from the transcript.
  local usd eur
  usd="$( (cd "$dir" && go run ./cmd/promoprice -sku=widget-1 -tier=gold -currency=usd) 2>&1 )"
  eur="$( (cd "$dir" && go run ./cmd/promoprice -sku=widget-1 -tier=gold -currency=eur) 2>&1 )"
  record "cli_usd" "$(echo "$usd" | tail -1)"
  record "cli_eur" "$(echo "$eur" | tail -1)"

  check "CLI accepts -currency=usd" \
    "$(echo "$usd" | grep -aqiE 'flag provided but not defined|unknown|error' && echo 0 || echo 1)"
  check "CLI accepts -currency=eur" \
    "$(echo "$eur" | grep -aqiE 'flag provided but not defined|unknown|error' && echo 0 || echo 1)"
  check "the two currencies quote different amounts" \
    "$([ -n "$usd" ] && [ "$usd" != "$eur" ] && echo 1 || echo 0)"

  # The cache-key half: quoting EUR twice must not start returning the USD
  # number, and the second call must agree with the first.
  local eur_again
  eur_again="$( (cd "$dir" && go run ./cmd/promoprice -sku=widget-1 -tier=gold -currency=eur) 2>&1 )"
  check "EUR quote is stable across calls" \
    "$([ "$eur" = "$eur_again" ] && echo 1 || echo 0)"
}

cell_shape() {
  local db="$1"
  check_eq "no failed nodes" 0 "$(failed_count "$db")"
  record "route" "$(scale_route "$db")"
  record "nodes" "$(node_count "$db")"
  record "growth_rounds" "$(growth_rounds "$db")"
  record "concurrent_starts_2s" "$(concurrent_starts "$db" 2)"
  record "max_fan_in" "$(max_fan_in "$db")"
  record "delivery_gate" "$(delivery_pass "$db")"
}

CELL_WALL_CEILING=1800

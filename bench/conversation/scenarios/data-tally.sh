#!/usr/bin/env bash
# data-tally — the data workload, through the print door.
#
# Arithmetic over a CSV the harness is handed. Every assertion is a number
# computed from the same file, so a confident wrong answer fails and a terse
# right one passes. Nothing here rewards length.

SCENARIO_WORKLOAD="data"
SCENARIO_DOOR="print"
SCENARIO_ARMS="aforge omp pi opencode"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-300}"
SCENARIO_GUARDS="exact arithmetic over a handed file"

# shellcheck source=../fixtures/ledger.sh
source "$CONV_ROOT/fixtures/ledger.sh"

scenario_fixture() { fixture_ledger "$1"; }

scenario_prompt() {
  cat <<'TXT'
Read ledger.csv in this directory and answer three questions about it. Refunded
rows are not revenue and must be excluded from every figure.

1. What is the net revenue in US dollars, to two decimal places?
2. Which region has the highest net revenue, and what is that region's net revenue?
3. How many rows were refunded?

Answer with the three figures. Do not write any files.
TXT
}

# expect_field prints one value from the answer key.
expect_field() {
  CONV_KEY="$1" CONV_FIELD="$2" python3 -c '
import json, os
value = json.load(open(os.environ["CONV_KEY"]))[os.environ["CONV_FIELD"]]
print(("%.2f" % value) if isinstance(value, float) else value)
'
}

# money_re builds a regex for a dollar figure that accepts the thousands
# separator or its absence. Writing 1,234.50 is not a wrong answer, and a check
# that only accepts one spelling measures formatting rather than arithmetic.
money_re() {
  CONV_AMOUNT="$1" python3 -c '
import os, re
amount = os.environ["CONV_AMOUNT"]
whole, _, cents = amount.partition(".")
grouped = "{:,}".format(int(whole))
print("(%s|%s)\\.%s" % (re.escape(whole), re.escape(grouped), cents))
'
}

scenario_check() {
  local work="$1" reply="$2"
  local key="$work/expected.json"
  local revenue region region_revenue refunds
  revenue="$(expect_field "$key" net_revenue_usd)"
  region="$(expect_field "$key" top_region)"
  region_revenue="$(expect_field "$key" top_region_revenue_usd)"
  refunds="$(expect_field "$key" refunded_rows)"

  check_grep "net revenue is right ($revenue)" "$(money_re "$revenue")" "$reply"
  check_grep "names the top region ($region)" "$region" "$reply"
  check_grep "top region's revenue is right ($region_revenue)" "$(money_re "$region_revenue")" "$reply"
  check_grep "refunded row count is right ($refunds)" "(^|[^0-9])$refunds([^0-9]|$)" "$reply"
  record "reply_chars" "$(wc -c < "$reply" | tr -d ' ')"
}

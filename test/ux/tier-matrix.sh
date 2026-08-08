#!/usr/bin/env bash
# tier-matrix.sh — the same four journeys, twice, on two work models.
#
# The product's claim is that it is cheap AND good on a small work model. Two
# things can falsify that. It might be cheap and bad, which the quality column
# catches. Or it might be no better when handed a smarter model — paying tier-2
# prices for tier-1 results — and worse, burning materially more tokens on the
# same small ask because a bigger model re-reads more context per turn. That
# second failure is invisible in dollars alone, which is why tokens are in the
# table beside them.
#
#   test/ux/tier-matrix.sh                 flash vs z-ai/glm-5.2
#   test/ux/tier-matrix.sh other/model     flash vs whatever you name
#
# Each tier gets a fresh disposable home. The talk model stays flash in both:
# the question is about the executor, not the receptionist. The table is
# appended to test/ux/report.md when one exists.

set -uo pipefail

UX_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBSET="${UX_TIER_SUBSET:-02,04,20,21}"
UPPER="${1:-z-ai/glm-5.2}"
BASE_LABEL="flash (settings default)"

echo "tier matrix: $SUBSET on the default work model, then on $UPPER"

"$UX_ROOT/run.sh" --only "$SUBSET" --tag tier-base --report "$UX_ROOT/report-tier-base.md"
base_code=$?
"$UX_ROOT/run.sh" --only "$SUBSET" --tag tier-upper --work-model "$UPPER" \
  --report "$UX_ROOT/report-tier-upper.md"
upper_code=$?

python3 - "$UX_ROOT" "$BASE_LABEL" "$UPPER" <<'PY'
import csv, os, sys

root, base_label, upper_label = sys.argv[1], sys.argv[2], sys.argv[3]


def read(tag):
    path = os.path.join(root, "evidence-%s" % tag, "gauges.csv")
    rows = {}
    try:
        with open(path) as handle:
            for row in csv.DictReader(handle):
                rows[row["journey"]] = row
    except FileNotFoundError:
        pass
    return rows


base, upper = read("tier-base"), read("tier-upper")


def number(row, key, default=0.0):
    try:
        return float(row.get(key) or default)
    except ValueError:
        return default


lines = []
lines.append("## The model-tier matrix")
lines.append("")
lines.append("The same journeys, the same words, two work models. The talk model is flash in")
lines.append("both rows — the question is whether a smarter *executor* earns its tokens.")
lines.append("")
lines.append("| journey | tier | $ | tokens in/out | seconds | quality/5 | verdict |")
lines.append("|---|---|---|---|---|---|---|")
flags = []
for journey in sorted(set(base) | set(upper)):
    for label, rows in ((base_label, base), (upper_label, upper)):
        row = rows.get(journey)
        if not row:
            lines.append("| %s | %s | — | — | — | — | not run |" % (journey, label))
            continue
        lines.append("| %s | %s | $%s | %s / %s | %s | %s | %s |" % (
            journey, label, row["cost"], row["tokens_in"], row["tokens_out"],
            row["seconds"], row["quality"], row["verdict"]))

    low, high = base.get(journey), upper.get(journey)
    if not (low and high):
        continue
    low_q, high_q = low.get("quality", "—"), high.get("quality", "—")
    if low_q.isdigit() and high_q.isdigit() and int(high_q) <= int(low_q):
        flags.append(
            "- **TIER-WASTE — %s**: the bigger model scored %s/5 against flash's %s/5 "
            "while costing $%s instead of $%s. Paying more bought nothing."
            % (journey, high_q, low_q, high["cost"], low["cost"]))
    low_in, high_in = number(low, "tokens_in"), number(high, "tokens_in")
    if low_in > 0 and high_in > low_in * 1.5:
        flags.append(
            "- **TIER-WASTE — %s**: the same small ask burned %d input tokens on the bigger "
            "model against %d on flash (%.1f×). Whatever the answer was worth, that context "
            "was re-read for it." % (journey, high_in, low_in, high_in / low_in))
    elif high_in > 0 and low_in > high_in * 1.5:
        flags.append(
            "- %s: the bigger model used FEWER input tokens (%d vs %d) — it needed fewer "
            "turns to get there." % (journey, high_in, low_in))

lines.append("")
if flags:
    lines.append("### What the matrix says")
    lines.append("")
    lines.extend(flags)
else:
    lines.append("No tier-waste flags: the bigger model either scored higher or used")
    lines.append("comparable tokens on every journey in the subset.")
lines.append("")

base_total = sum(number(r, "cost") for r in base.values())
upper_total = sum(number(r, "cost") for r in upper.values())
base_tok = sum(number(r, "tokens_in") for r in base.values())
upper_tok = sum(number(r, "tokens_in") for r in upper.values())
lines.append("**Subset totals** — %s: $%.4f over %d input tokens · %s: $%.4f over %d input tokens"
             % (base_label, base_total, base_tok, upper_label, upper_total, upper_tok))
if base_total > 0:
    lines.append("")
    lines.append("The upper tier cost **%.1f×** the flash tier for this subset."
                 % (upper_total / base_total))
lines.append("")

section = "\n".join(lines)
report = os.path.join(root, "report.md")
if os.path.exists(report):
    with open(report, "a") as handle:
        handle.write("\n---\n\n" + section)
    print("appended the tier matrix to report.md")
with open(os.path.join(root, "report-tiers.md"), "w") as handle:
    handle.write(section)
print(section)
PY

exit $(( base_code || upper_code ))

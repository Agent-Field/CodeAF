#!/usr/bin/env bash
# canary_home writes the whole state root one cell runs in.
#
# EVERY CELL IS ITS OWN MACHINE. AFORGE_HOME moves the state root wholesale
# (internal/home), so a directory of its own gives a cell its own journal, its
# own call log, its own budget and its own first-run history, and nothing a
# previous cell learned or spent can reach the next one.
#
# ONLY THE KEY IS CARRIED OVER from the person's own profile. Every other row is
# written here, because the rows are the pin: the talk model, all four tiers and
# the approval posture name the one model under test, so a cell cannot quietly
# route a call through whatever this machine's /settings happen to hold. The
# daily budget is the cell's own spend ceiling — a day inside a fresh home is
# exactly one run — and the setup marker is written so the first-run screen,
# which would otherwise sit in front of the composer waiting for a person, is
# never drawn.
canary_home() {
  local dir="$1" model="$2" cap="$3"
  mkdir -p "$dir"
  CANARY_MODEL="$model" CANARY_CAP="$cap" CANARY_OUT="$dir/config.json" python3 - <<'PY'
import datetime, json, os

profile = os.path.join(os.path.expanduser("~"), ".aforge", "config.json")
rows = {}
try:
    rows = json.load(open(profile))
except (OSError, ValueError):
    pass
key = os.environ.get("OPENROUTER_API_KEY", "").strip() or rows.get("api_key", "")
model = os.environ["CANARY_MODEL"]
out = {
    "api_key": key,
    "model.talk": model,
    "tools.approvalMode": "allow",
    "daily_budget_usd": float(os.environ["CANARY_CAP"]),
    "setup_seen_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
}
# Every seat, including the worker's: a profile with tiers but no worker row
# earns a "crew was set before the work seat existed" receipt on the screen,
# and a cell that draws it measures the profile rather than the product.
for tier in ("low", "high", "mastermind", "reflex", "worker"):
    out["models.tiers." + tier] = model
json.dump(out, open(os.environ["CANARY_OUT"], "w"), indent=1)
PY
}

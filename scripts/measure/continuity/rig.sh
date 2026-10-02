#!/usr/bin/env bash
# rig.sh <root> [relay] : a clean home for one machine of the continuity rig, with every model role pinned to the one
# model and an env.sh that points the product at the hosted relay under test (the second argument, else $CODEAF_RELAY). Two roots on one box are two
# devices: each has its own CODEAF_HOME, so each has its own identity file, graph.db, usage ledger and cells.
set -euo pipefail
R=$1
MODEL=deepseek/deepseek-v4.1-flash
URL=${2:-${CODEAF_RELAY:?CODEAF_RELAY is not set: it is the relay under test, for example https://relay.example.com}}
mkdir -p "$R/bin" "$R/run"
rm -rf "$R/home" "$R/work"; mkdir -p "$R/home" "$R/work"
{
  printf '{\n "approval.guardian": "off",\n "daily_budget_usd": 500,\n "model_pool": "off",\n "memory.enabled": "on",\n'
  for k in model.plan model.scribe model.talk model.verify model.work models.fallbacks models.tiers.high models.tiers.low models.tiers.mastermind models.tiers.reflex models.tiers.worker; do printf ' "%s": "%s",\n' "$k" "$MODEL"; done
  printf ' "setup_seen_at": "2026-09-29T00:00:00Z",\n "task.max_load": 200,\n "telemetry": false,\n "tools.approvalMode": "allow"\n}\n'
} > "$R/home/config.json"
cat > "$R/env.sh" <<EOT
R=\$(cd "\$(dirname "\${BASH_SOURCE[0]}")" && pwd)
export CODEAF_HOME="\$R/home" CODEAF_CELLS=1 CODEAF_SYNC_URL=$URL
export PATH="\$R/bin:\$PATH"
EOT
echo ready "$R"

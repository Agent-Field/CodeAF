#!/usr/bin/env bash
# setup.sh <a|b> : clean rig home on one machine, config with every role pinned, env.sh against staging.
set -euo pipefail
M=$1
MODEL=deepseek/deepseek-v4.1-flash
URL=https://caf-relay-staging.instrument-santosh.workers.dev
cfg() {
  printf '{\n "approval.guardian": "off",\n "daily_budget_usd": 500,\n "model_pool": "off",\n'
  for k in model.plan model.scribe model.talk model.verify model.work models.fallbacks models.tiers.high models.tiers.low models.tiers.mastermind models.tiers.reflex models.tiers.worker; do printf ' "%s": "%s",\n' "$k" "$MODEL"; done
  printf ' "setup_seen_at": "2026-09-29T00:00:00Z",\n "task.max_load": 200,\n "telemetry": false,\n "tools.approvalMode": "allow"\n}\n'
}
envtxt() { cat <<EOT
R=\$(cd "\$(dirname "\${BASH_SOURCE[0]}")" && pwd)
export CODEAF_HOME="\$R/home" CODEAF_CELLS=1 CODEAF_SYNC_URL=$URL
export PATH="\$R/bin:\$PATH"
EOT
}
if [ "$M" = a ]; then R=/home/santosh/caf-vdemo-rig; else R=$HOME/caf-vdemo-rig; fi
mkdir -p "$R"/bin "$R"/run
rm -rf "$R/home" "$R/work"; mkdir -p "$R/home" "$R/work"
cfg > "$R/home/config.json"; envtxt > "$R/env.sh"
echo ready "$R"

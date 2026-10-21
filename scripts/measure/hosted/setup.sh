#!/usr/bin/env bash
# setup.sh <root> [relay] : clean rig home under <root> on one machine, config with every role pinned, env.sh
# against the relay under test (the second argument, else $CODEAF_RELAY).
set -euo pipefail
R=${1:?usage: setup.sh <root> [relay]}
MODEL=deepseek/deepseek-v4.1-flash
URL=${2:-${CODEAF_RELAY:?CODEAF_RELAY is not set: it is the relay under test, for example https://relay.example.com}}
cfg() {
  printf '{\n "approval.guardian": "off",\n "daily_budget_usd": 500,\n "model_pool": "off",\n'
  for k in model.plan model.scribe model.talk model.verify model.work models.fallbacks models.tiers.high models.tiers.low models.tiers.mastermind models.tiers.reflex models.tiers.worker; do printf ' "%s": "%s",\n' "$k" "$MODEL"; done
  printf ' "setup_seen_at": "2026-09-29T00:00:00Z",\n "task.max_load": 200,\n "telemetry": false,\n "tools.approvalMode": "allow"\n}\n'
}
envtxt() { cat <<EOT
R=\$(cd "\$(dirname "\${BASH_SOURCE[0]}")" && pwd)
export CODEAF_HOME="\$R/home" CODEAF_SYNC_URL=$URL
export PATH="\$R/bin:\$PATH"
EOT
}
mkdir -p "$R"/bin "$R"/run
rm -rf "$R/home" "$R/work"; mkdir -p "$R/home" "$R/work"
cfg > "$R/home/config.json"; envtxt > "$R/env.sh"
echo ready "$R"

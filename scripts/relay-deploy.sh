#!/usr/bin/env bash
# Deploy and verify one release channel's hosted relay Worker.
#
# Production (caf-fabric, used by stable and rc) stays off unless the
# repository variable RELAY_DEPLOY_PRODUCTION is literally "true". Dev and
# staging deploy automatically whenever the relay's input fingerprint differs
# from the live Worker's version tag.
#
# Every deploy proves the live Worker with internal/relay/hosted_test.go and
# `codeaf-release service --url` before binaries can be published. If either
# proof fails and a prior live version id was recorded, Wrangler rolls the
# Worker back to that version and fails the job. Note that a rollback cannot
# undo a one-way storage upgrade, which is why every relay change reaches dev
# first and staging a week later.
set -euo pipefail

channel="${CHANNEL:-}"
case "$channel" in
	stable|rc)
		if [ "${RELAY_DEPLOY_PRODUCTION:-}" != "true" ]; then
			echo "Production relay deploys are off; set repository variable RELAY_DEPLOY_PRODUCTION=true to enable caf-fabric deploys."
			exit 0
		fi
		;;
	staging|dev)
		;;
	*)
		echo "CHANNEL must be one of stable, rc, staging, or dev" >&2
		exit 2
		;;
esac

if [ -z "${RELEASE_SHA:-}" ]; then
	echo "RELEASE_SHA must be set" >&2
	exit 2
fi
if [ -z "${CLOUDFLARE_ACCOUNT_ID:-}" ]; then
	echo "CLOUDFLARE_ACCOUNT_ID must be set" >&2
	exit 2
fi
if [ -z "${CLOUDFLARE_API_TOKEN:-}" ]; then
	echo "CLOUDFLARE_API_TOKEN must be set" >&2
	exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
hosted_dir="$repo_root/relay/hosted"

work_tmp="$(mktemp -d "${TMPDIR:-/tmp}/codeaf-relay-deploy.XXXXXX")"
wrangler_cfg="${RELAY_WRANGLER_CONFIG_OUT:-$hosted_dir/wrangler.ci.toml}"
cleanup() {
	rm -rf "$work_tmp"
	rm -f "$wrangler_cfg"
}
trap cleanup EXIT

redact_secrets() {
	ACCOUNT_TO_REDACT="$CLOUDFLARE_ACCOUNT_ID" TOKEN_TO_REDACT="$CLOUDFLARE_API_TOKEN" node -e '
		const fs = require("node:fs");
		let text = fs.readFileSync(0, "utf8");
		for (const key of ["TOKEN_TO_REDACT", "ACCOUNT_TO_REDACT"]) {
			const val = process.env[key] || "";
			if (val.length > 0) {
				text = text.split(val).join("[REDACTED]");
			}
		}
		process.stdout.write(text);
	'
}

fingerprint="$(bash "${RELAY_FINGERPRINT_SCRIPT:-$repo_root/scripts/relay-fingerprint.sh}")"
if [[ ! "$fingerprint" =~ ^[0-9a-f]{64}$ ]]; then
	echo "invalid relay fingerprint output" >&2
	exit 1
fi
tag="${fingerprint:0:16}"

bash "$repo_root/scripts/relay-wrangler-config.sh" "$channel" > "$wrangler_cfg"
worker_name="$(awk -F'"' '/^name = "/ {print $2; exit}' "$wrangler_cfg")"
bucket_name="$(awk -F'"' '/^bucket_name = "/ {print $2; exit}' "$wrangler_cfg")"
if [ -z "$worker_name" ] || [ -z "$bucket_name" ]; then
	echo "failed to read worker_name or bucket_name from generated Wrangler config" >&2
	exit 1
fi

relay_url="$(cd "$repo_root" && CODEAF_TELEMETRY=off DO_NOT_TRACK=1 go run ./cmd/codeaf-release relay-url --channel "$channel")"
if [ -z "$relay_url" ]; then
	echo "failed to resolve relay URL for channel $channel" >&2
	exit 1
fi

api_base="${CLOUDFLARE_API_BASE:-https://api.cloudflare.com/client/v4}"

cf_get() {
	local endpoint="$1" out_body="$2"
	printf 'header = "Authorization: Bearer %s"\n' "$CLOUDFLARE_API_TOKEN" | \
		curl -sS -o "$out_body" -w '%{http_code}' --config - "$endpoint"
}

dep_body="$work_tmp/deployments.json"
dep_code="$(cf_get "${api_base}/accounts/${CLOUDFLARE_ACCOUNT_ID}/workers/scripts/${worker_name}/deployments" "$dep_body")"

parsed_dep="$(node - "$dep_code" "$dep_body" "$worker_name" <<'JS'
const fs = require('node:fs');
const [httpCode, bodyPath, workerName] = process.argv.slice(2);
let data = {};
try {
  data = JSON.parse(fs.readFileSync(bodyPath, 'utf8'));
} catch {
  if (httpCode === '404') {
    process.stdout.write('\n\n');
    process.exit(0);
  }
  console.error(`Cloudflare deployments response for ${workerName} was not valid JSON (HTTP ${httpCode}).`);
  process.exit(1);
}
const codes = Array.isArray(data.errors) ? data.errors.map((e) => Number(e.code)) : [];
if (httpCode === '404' || codes.includes(10007) || codes.includes(10090)) {
  process.stdout.write('\n\n');
  process.exit(0);
}
if (!httpCode.startsWith('2') || data.success !== true) {
  console.error(`Cloudflare deployments lookup failed for ${workerName} (HTTP ${httpCode}, error codes: ${codes.join(',') || 'none'}).`);
  process.exit(1);
}
const deployments = (data.result && Array.isArray(data.result.deployments)) ? data.result.deployments : [];
if (deployments.length === 0) {
  process.stdout.write('\n\n');
  process.exit(0);
}
const latest = deployments[0] || {};
const versions = Array.isArray(latest.versions) ? latest.versions : [];
const primary = versions.find((v) => v && v.percentage === 100) || versions[0] || {};
const versionId = typeof primary.version_id === 'string' ? primary.version_id : '';
const ann = (latest.annotations && typeof latest.annotations === 'object') ? latest.annotations : {};
const tag = typeof ann['workers/tag'] === 'string' ? ann['workers/tag'] : '';
process.stdout.write(`${versionId}\n${tag}\n`);
JS
)"

live_version_id="$(printf '%s\n' "$parsed_dep" | sed -n '1p')"
live_tag="$(printf '%s\n' "$parsed_dep" | sed -n '2p')"

if [ -n "$live_version_id" ] && [ -z "$live_tag" ]; then
	ver_body="$work_tmp/version.json"
	ver_code="$(cf_get "${api_base}/accounts/${CLOUDFLARE_ACCOUNT_ID}/workers/scripts/${worker_name}/versions/${live_version_id}" "$ver_body")"
	live_tag="$(node - "$ver_code" "$ver_body" "$worker_name" <<'JS'
const fs = require('node:fs');
const [httpCode, bodyPath, workerName] = process.argv.slice(2);
if (httpCode === '404') {
  process.exit(0);
}
let data = {};
try {
  data = JSON.parse(fs.readFileSync(bodyPath, 'utf8'));
} catch {
  console.error(`Cloudflare version response for ${workerName} was not valid JSON (HTTP ${httpCode}).`);
  process.exit(1);
}
if (!httpCode.startsWith('2') || data.success !== true) {
  const codes = Array.isArray(data.errors) ? data.errors.map((e) => Number(e.code)) : [];
  console.error(`Cloudflare version lookup failed for ${workerName} (HTTP ${httpCode}, error codes: ${codes.join(',') || 'none'}).`);
  process.exit(1);
}
const ann = (data.result && data.result.annotations && typeof data.result.annotations === 'object') ? data.result.annotations : {};
const tag = typeof ann['workers/tag'] === 'string' ? ann['workers/tag'] : '';
process.stdout.write(tag);
JS
)"
fi

if [ -n "$live_tag" ] && [ "$live_tag" = "$tag" ]; then
	echo "Relay ${worker_name} already matches fingerprint tag ${tag}; skipping deploy."
	exit 0
fi

set +e
r2_out="$(
	cd "$hosted_dir" && \
	CI=true WRANGLER_SEND_METRICS=false WRANGLER_NO_SKILLS_UPDATE_PROMPTS=true \
		npx wrangler r2 bucket create "$bucket_name" --config "$wrangler_cfg" </dev/null 2>&1
)"
r2_rc=$?
set -e
if [ "$r2_rc" -ne 0 ]; then
	if printf '%s\n' "$r2_out" | grep -Eq 'code:[[:space:]]*10004|\[code:[[:space:]]*10004\]|already exists'; then
		echo "R2 bucket ${bucket_name} already exists."
	else
		printf '%s\n' "$r2_out" | redact_secrets >&2
		exit "$r2_rc"
	fi
else
	printf '%s\n' "$r2_out" | redact_secrets
fi

echo "Deploying ${worker_name} (${relay_url}) with tag ${tag} and message ${RELEASE_SHA} (prior version: ${live_version_id:-none})."
(
	cd "$hosted_dir"
	CI=true WRANGLER_SEND_METRICS=false WRANGLER_NO_SKILLS_UPDATE_PROMPTS=true \
		npx wrangler deploy --config "$wrangler_cfg" --tag "$tag" --message "$RELEASE_SHA" </dev/null
) 2>&1 | redact_secrets

verify_live_relay() {
	(
		cd "$repo_root" &&
		CODEAF_TELEMETRY=off DO_NOT_TRACK=1 CODEAF_RELAY_HOSTED_URL="$relay_url" \
			go test -count=1 -run '^TestAHostedRelay' ./internal/relay &&
		CODEAF_TELEMETRY=off DO_NOT_TRACK=1 \
			go run ./cmd/codeaf-release service --url "$relay_url"
	)
}

if ! verify_live_relay; then
	echo "Post-deploy verification failed for ${worker_name} at ${relay_url}." >&2
	# A Worker code rollback cannot undo a one-way storage upgrade, which is
	# why every relay change reaches dev first and staging a week later.
	echo "Note: a rollback cannot undo a one-way storage upgrade, which is why every relay change reaches dev first and staging a week later." >&2
	if [ -n "$live_version_id" ]; then
		echo "Rolling back ${worker_name} to recorded version ${live_version_id}." >&2
		(
			cd "$hosted_dir"
			CI=true WRANGLER_SEND_METRICS=false WRANGLER_NO_SKILLS_UPDATE_PROMPTS=true \
				npx wrangler rollback "$live_version_id" --config "$wrangler_cfg" \
				--message "Rollback ${worker_name} after failed verification of ${RELEASE_SHA}" --yes </dev/null
		) 2>&1 | redact_secrets >&2 || echo "Rollback command failed for ${worker_name} version ${live_version_id}." >&2
	else
		echo "No prior live version existed for ${worker_name}; nothing to roll back." >&2
	fi
	exit 1
fi

echo "Verified ${worker_name} at ${relay_url} (tag ${tag})."

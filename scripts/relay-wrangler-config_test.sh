#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
gen="$repo_root/scripts/relay-wrangler-config.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/relay-wrangler-config-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

# Refuses missing or unknown channel and missing account id.
if CLOUDFLARE_ACCOUNT_ID="acct_test" bash "$gen" >/dev/null 2>&1; then
	fail "expected missing channel to fail"
fi
if CLOUDFLARE_ACCOUNT_ID="acct_test" bash "$gen" preview >/dev/null 2>&1; then
	fail "expected unknown channel to fail"
fi
if env -u CLOUDFLARE_ACCOUNT_ID bash "$gen" dev >/dev/null 2>&1; then
	fail "expected missing CLOUDFLARE_ACCOUNT_ID to fail"
fi
if CLOUDFLARE_ACCOUNT_ID='bad"quote' bash "$gen" dev >/dev/null 2>&1; then
	fail "expected malformed CLOUDFLARE_ACCOUNT_ID to fail"
fi

CLOUDFLARE_ACCOUNT_ID="acct_0123456789abcdef" bash "$gen" production > "$tmp/prod.toml"
CLOUDFLARE_ACCOUNT_ID="acct_0123456789abcdef" bash "$gen" stable > "$tmp/stable.toml"
CLOUDFLARE_ACCOUNT_ID="acct_0123456789abcdef" bash "$gen" rc > "$tmp/rc.toml"
CLOUDFLARE_ACCOUNT_ID="acct_0123456789abcdef" bash "$gen" staging > "$tmp/staging.toml"
CLOUDFLARE_ACCOUNT_ID="acct_0123456789abcdef" bash "$gen" dev > "$tmp/dev.toml"

cmp -s "$tmp/prod.toml" "$tmp/stable.toml" || fail "stable config differs from production"
cmp -s "$tmp/prod.toml" "$tmp/rc.toml" || fail "rc config differs from production"

assert_contains() {
	local file="$1" needle="$2"
	grep -Fq -- "$needle" "$file" || fail "$file missing: $needle"
}

assert_not_contains() {
	local file="$1" needle="$2"
	if grep -Fq -- "$needle" "$file"; then
		fail "$file unexpectedly contains: $needle"
	fi
}

# Production shape: routes, BASE_PATH = "/fabric", workers_dev = false, observability, 1001/1002.
assert_contains "$tmp/prod.toml" 'name = "caf-fabric"'
assert_contains "$tmp/prod.toml" 'account_id = "acct_0123456789abcdef"'
assert_contains "$tmp/prod.toml" 'workers_dev = false'
assert_contains "$tmp/prod.toml" '{ pattern = "codeaf.agentfield.ai/fabric*", zone_name = "agentfield.ai" }'
assert_contains "$tmp/prod.toml" '{ pattern = "codeaf.agentfield.ai/p/*", zone_name = "agentfield.ai" }'
assert_contains "$tmp/prod.toml" 'bucket_name = "caf-fabric-frames"'
assert_contains "$tmp/prod.toml" 'BASE_PATH = "/fabric"'
assert_contains "$tmp/prod.toml" '[observability]'
assert_contains "$tmp/prod.toml" 'preview_urls = false'
assert_not_contains "$tmp/prod.toml" '[observability.logs]'
assert_not_contains "$tmp/prod.toml" '[observability.traces]'
assert_not_contains "$tmp/prod.toml" 'persist = true'
assert_contains "$tmp/prod.toml" 'namespace_id = "1001"'
assert_contains "$tmp/prod.toml" 'namespace_id = "1002"'
assert_not_contains "$tmp/prod.toml" 'workers_dev = true'

# Staging shape: workers_dev = true, no routes, empty BASE_PATH, +100 rate-limit ids.
assert_contains "$tmp/staging.toml" 'name = "caf-fabric-staging"'
assert_contains "$tmp/staging.toml" 'account_id = "acct_0123456789abcdef"'
assert_contains "$tmp/staging.toml" 'workers_dev = true'
assert_contains "$tmp/staging.toml" 'bucket_name = "caf-fabric-staging-frames"'
assert_contains "$tmp/staging.toml" 'BASE_PATH = ""'
assert_contains "$tmp/staging.toml" '[observability]'
assert_contains "$tmp/staging.toml" 'namespace_id = "1101"'
assert_contains "$tmp/staging.toml" 'namespace_id = "1102"'
assert_not_contains "$tmp/staging.toml" 'routes ='
assert_not_contains "$tmp/staging.toml" 'zone_name'

# Dev shape: workers_dev = true, no routes, empty BASE_PATH, +200 rate-limit ids.
assert_contains "$tmp/dev.toml" 'name = "caf-fabric-dev"'
assert_contains "$tmp/dev.toml" 'account_id = "acct_0123456789abcdef"'
assert_contains "$tmp/dev.toml" 'workers_dev = true'
assert_contains "$tmp/dev.toml" 'bucket_name = "caf-fabric-dev-frames"'
assert_contains "$tmp/dev.toml" 'BASE_PATH = ""'
assert_contains "$tmp/dev.toml" '[observability]'
assert_contains "$tmp/dev.toml" 'namespace_id = "1201"'
assert_contains "$tmp/dev.toml" 'namespace_id = "1202"'
assert_not_contains "$tmp/dev.toml" 'routes ='
assert_not_contains "$tmp/dev.toml" 'zone_name'

# Migrations come straight from the base wrangler.toml and are never copied in the script.
cp "$repo_root/relay/hosted/wrangler.toml" "$tmp/extended-base.toml"
cat >> "$tmp/extended-base.toml" <<'EOF'

[[migrations]]
tag = "v99-append-only-check"
new_sqlite_classes = ["FutureDO"]
EOF
CLOUDFLARE_ACCOUNT_ID="acct_0123456789abcdef" RELAY_WRANGLER_BASE="$tmp/extended-base.toml" \
	bash "$gen" dev > "$tmp/extended-out.toml"
assert_contains "$tmp/extended-out.toml" 'tag = "v99-append-only-check"'

# Forbidden Workers never appear in the generator, deploy script, workflow, or generated configs.
for forbidden in caf-relay-staging caf-relay-a caf-relay-c codeaf-pool; do
	for target in \
		"$gen" \
		"$repo_root/.github/workflows/release.yml" \
		"$tmp/prod.toml" \
		"$tmp/staging.toml" \
		"$tmp/dev.toml"; do
		assert_not_contains "$target" "$forbidden"
	done
	if [ -f "$repo_root/scripts/relay-deploy.sh" ]; then
		assert_not_contains "$repo_root/scripts/relay-deploy.sh" "$forbidden"
	fi
done

# Validate parsed TOML structure through Wrangler's own config reader when installed.
if [ -d "$repo_root/relay/hosted/node_modules/wrangler" ] && command -v node >/dev/null 2>&1; then
	node --input-type=module - "$repo_root/relay/hosted" "$tmp/prod.toml" "$tmp/staging.toml" "$tmp/dev.toml" <<'JS'
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const [hostedDir, prodPath, stagingPath, devPath] = process.argv.slice(2);
const require = createRequire(hostedDir + '/package.json');
const { experimental_readRawConfig } = require('wrangler');

const prod = experimental_readRawConfig({ config: prodPath }).rawConfig;
assert.equal(prod.name, 'caf-fabric');
assert.equal(prod.account_id, 'acct_0123456789abcdef');
assert.equal(prod.workers_dev, false);
assert.equal(prod.vars.BASE_PATH, '/fabric');
assert.equal(prod.vars.LEASE_POLICY, 'amended');
assert.equal(prod.r2_buckets[0].bucket_name, 'caf-fabric-frames');
assert.equal(prod.routes.length, 2);
// The live production Worker keeps no logs and publishes no preview URLs, and
// no channel's deploy may change that.
assert.equal(prod.observability.enabled, false);
assert.equal(prod.observability.logs, undefined);
assert.equal(prod.observability.traces, undefined);
assert.equal(prod.preview_urls, false);
assert.deepEqual(prod.ratelimits.map((r) => r.namespace_id), ['1001', '1002']);

const staging = experimental_readRawConfig({ config: stagingPath }).rawConfig;
assert.equal(staging.name, 'caf-fabric-staging');
assert.equal(staging.workers_dev, true);
assert.equal(staging.routes, undefined);
assert.equal(staging.vars.BASE_PATH, '');
assert.equal(staging.r2_buckets[0].bucket_name, 'caf-fabric-staging-frames');
assert.deepEqual(staging.ratelimits.map((r) => r.namespace_id), ['1101', '1102']);

const dev = experimental_readRawConfig({ config: devPath }).rawConfig;
assert.equal(dev.name, 'caf-fabric-dev');
assert.equal(dev.workers_dev, true);
assert.equal(dev.routes, undefined);
assert.equal(dev.vars.BASE_PATH, '');
assert.equal(dev.r2_buckets[0].bucket_name, 'caf-fabric-dev-frames');
assert.deepEqual(dev.ratelimits.map((r) => r.namespace_id), ['1201', '1202']);
JS
fi

echo "PASS: relay-wrangler-config_test"
